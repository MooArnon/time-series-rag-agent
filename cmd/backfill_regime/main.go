package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/adshao/go-binance/v2/futures"

	"time-series-rag-agent/config"
	"time-series-rag-agent/internal/database"
	"time-series-rag-agent/internal/market"
	markettrend "time-series-rag-agent/internal/market_trend"
	"time-series-rag-agent/pkg"
)

type BackfillTarget struct {
	Symbol   string
	Interval string
	Lookback time.Duration
}

type regimeRow struct {
	t      time.Time
	result markettrend.RegimeResult
}

var targets = []BackfillTarget{
	// {"ETHUSDT", "15m", 30 * 24 * time.Hour}, // 30 วัน = 2,880 candles
	{"ETHUSDT", "4h", 90 * 24 * time.Hour}, // 90 วัน = 540 candles ✓
	// {"ETHUSDT", "1d", 365 * 24 * time.Hour}, // 1 ปี   = 365 candles ✓
}

func main() {
	logger := pkg.SetupLogger()
	cfg := config.LoadConfig()

	binanceClient := futures.NewClient(cfg.Market.ApiKey, cfg.Market.ApiSecret)

	// Run ครั้งแรกทันที
	logger.Info("Starting initial backfill...")
	runBackfill(binanceClient, cfg)

	// Schedule ทุก 1 ชั่วโมง
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	logger.Info("Scheduler started — running every 1 hour")
	for range ticker.C {
		logger.Info("=== Scheduled backfill triggered ===")
		runBackfill(binanceClient, cfg)
	}
}

func newConnString(cfg *config.AppConfig) string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?pool_max_conns=3&pool_min_conns=1&pool_max_conn_lifetime=5m&pool_max_conn_idle_time=1m",
		cfg.Database.DBUser,
		cfg.Database.DBPassword,
		cfg.Database.DBHost,
		cfg.Database.DBPort,
		cfg.Database.DBName,
	)
}

func runBackfill(binanceClient *futures.Client, cfg *config.AppConfig) {
	logger := pkg.SetupLogger()

	// เปิด connection ใหม่ทุกรอบ
	pg, err := database.NewPostgresDB(newConnString(cfg))
	if err != nil {
		logger.Error(fmt.Sprintf("DB connection failed: %v", err))
		return
	}
	// ปิด connection ทันทีที่ runBackfill จบ ไม่มี zombie
	defer pg.Close()

	for _, target := range targets {
		logger.Info(fmt.Sprintf("=== Backfilling %s %s ===", target.Symbol, target.Interval))

		startTime := getLastRegimeTime(pg, target.Symbol, target.Interval, target.Lookback)
		endTime := time.Now().UTC()

		logger.Info(fmt.Sprintf("Fetching from %s to %s",
			startTime.Format("2006-01-02 15:04 UTC"),
			endTime.Format("2006-01-02 15:04 UTC"),
		))

		candles, err := market.FetchHistoryByTime(
			binanceClient,
			target.Symbol,
			target.Interval,
			startTime,
			endTime,
		)
		if err != nil {
			logger.Error(fmt.Sprintf("Fetch failed for %s %s: %v", target.Symbol, target.Interval, err))
			continue
		}
		logger.Info(fmt.Sprintf("Got %d candles for %s %s", len(candles), target.Symbol, target.Interval))

		inserted, skipped := processCandles(pg, candles, target.Symbol, target.Interval, cfg)
		logger.Info(fmt.Sprintf("Done %s %s — inserted: %d, skipped: %d", target.Symbol, target.Interval, inserted, skipped))

		time.Sleep(500 * time.Millisecond)
	}
}

func getLastRegimeTime(pg *database.PostgresDB, symbol, interval string, fallback time.Duration) time.Time {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var lastTime time.Time
	err := pg.Pool.QueryRow(ctx, `
		SELECT COALESCE(MAX(time), NOW() - $3::interval)
		FROM market_regime
		WHERE symbol = $1 AND "interval" = $2
	`, symbol, interval, fallback.String()).Scan(&lastTime)

	if err != nil {
		logger := pkg.SetupLogger()
		logger.Error(fmt.Sprintf("getLastRegimeTime failed: %v, using fallback", err))
		return time.Now().UTC().Add(-fallback)
	}

	// buffer 101 candles ย้อนหลังเพื่อให้ ADX คำนวณถูกต้อง
	intervalDuration := map[string]time.Duration{
		"15m": 15 * time.Minute,
		"4h":  4 * time.Hour,
		"1d":  24 * time.Hour,
	}
	if d, ok := intervalDuration[interval]; ok {
		return lastTime.Add(-d * 101)
	}
	return lastTime
}

func processCandles(
	pg *database.PostgresDB,
	candles []market.InputData,
	symbol string,
	interval string,
	cfg *config.AppConfig,
) (int, int) {
	minRequired := 101

	var rows []regimeRow

	for i := minRequired; i < len(candles); i++ {
		window := candles[:i+1]

		adx := markettrend.CalcADX(window, 14)
		bandWidth := markettrend.CalcBandWidth(window, 20)
		atrRatio := markettrend.CalcATRRatio(window)

		result := markettrend.RegimeResult{
			ADX:       adx.ADX,
			PlusDI:    adx.PlusDI,
			MinusDI:   adx.MinusDI,
			ATRRatio:  atrRatio,
			BandWidth: bandWidth,
			Regime:    markettrend.RegimeUnknown,
		}

		switch {
		case atrRatio > cfg.Regime.ATRVolatileThreshold:
			result.Regime = markettrend.RegimeVolatile
		case adx.ADX > cfg.Regime.ADXTrendThreshold && atrRatio < cfg.Regime.ATRVolatileThreshold:
			result.Regime = markettrend.RegimeTrending
			if adx.PlusDI > adx.MinusDI {
				result.Direction = "BULL"
			} else {
				result.Direction = "BEAR"
			}
		case adx.ADX < cfg.Regime.ADXRangeThreshold && bandWidth < cfg.Regime.BandWidthThreshold:
			result.Regime = markettrend.RegimeRanging
		}

		rows = append(rows, regimeRow{
			t:      time.Unix(candles[i].Time, 0).UTC(),
			result: result,
		})
	}

	batchSize := 500
	inserted := 0
	skipped := 0

	for start := 0; start < len(rows); start += batchSize {
		end := start + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		batch := rows[start:end]

		err := batchUpsertRegime(pg, batch, symbol, interval)
		if err != nil {
			fmt.Printf("Batch upsert failed: %v\n", err)
			skipped += len(batch)
		} else {
			inserted += len(batch)
		}
	}

	return inserted, skipped
}

func batchUpsertRegime(
	pg *database.PostgresDB,
	rows []regimeRow,
	symbol string,
	interval string,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	valueStrings := make([]string, 0, len(rows))
	valueArgs := make([]interface{}, 0, len(rows)*9)

	for i, row := range rows {
		n := i * 9
		valueStrings = append(valueStrings, fmt.Sprintf(
			"($%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			n+1, n+2, n+3, n+4, n+5, n+6, n+7, n+8, n+9,
		))
		valueArgs = append(valueArgs,
			row.t, symbol, interval,
			row.result.ADX, row.result.PlusDI, row.result.MinusDI,
			row.result.BandWidth, row.result.ATRRatio, toRegimeString(row.result),
		)
	}

	query := fmt.Sprintf(`
		INSERT INTO market_regime (
			time, symbol, "interval",
			adx, plus_di, minus_di,
			bbw, atr_ratio, regime
		) VALUES %s
		ON CONFLICT (time, symbol, "interval")
		DO UPDATE SET
			adx        = EXCLUDED.adx,
			plus_di    = EXCLUDED.plus_di,
			minus_di   = EXCLUDED.minus_di,
			bbw        = EXCLUDED.bbw,
			atr_ratio  = EXCLUDED.atr_ratio,
			regime     = EXCLUDED.regime,
			updated_at = NOW()
	`, strings.Join(valueStrings, ","))

	_, err := pg.Pool.Exec(ctx, query, valueArgs...)
	return err
}

func toRegimeString(result markettrend.RegimeResult) string {
	switch result.Regime {
	case markettrend.RegimeTrending:
		if result.Direction == "BULL" {
			return "TRENDING_BULL"
		}
		return "TRENDING_BEAR"
	case markettrend.RegimeRanging:
		return "RANGING"
	case markettrend.RegimeVolatile:
		return "VOLATILE"
	default:
		return "UNKNOWN"
	}
}
