package pipeline

import (
	"context"
	"fmt"
	"log/slog"

	"time-series-rag-agent/config"
	"time-series-rag-agent/internal/embedding"
	"time-series-rag-agent/internal/exchange"
	"time-series-rag-agent/internal/storage/postgresql"

	"golang.org/x/sync/errgroup"
)

func RestIngestVectorFlow(logger *slog.Logger, symbol string, interval string, vectorSize int) error {
	logger.Info("[RestIngestVectorFlow] Start multiple ingest data")

	cfg := config.LoadConfig()
	connString := fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
		cfg.Database.DBUser, cfg.Database.DBPassword,
		cfg.Database.DBHost, cfg.Database.DBPort, cfg.Database.DBName,
	)

	ctx := context.Background()

	// ── Phase 1: DB connect + Fetch candles (concurrent) ──
	var (
		dbIngest     *postgresql.PatternStore
		wsRestCandle []exchange.WsRestCandle
	)

	g1, ctx1 := errgroup.WithContext(ctx)

	g1.Go(func() error {
		var err error
		dbIngest, err = postgresql.NewPostgresDB(ctx1, connString, *logger)
		if err != nil {
			return fmt.Errorf("connect db: %w", err)
		}
		logger.Info("[RestIngestVectorFlow] DB connected")
		return nil
	})

	g1.Go(func() error {
		binanceClient, err := exchange.NewBinanceClient(ctx1, cfg)
		if err != nil {
			return fmt.Errorf("new binance client: %w", err)
		}
		adapter := exchange.NewBinanceAdapter(binanceClient)

		restCandle, err := exchange.FetchLatestCandles(ctx1, adapter, symbol, interval, vectorSize+1+99)
		if err != nil {
			return fmt.Errorf("fetch candles: %w", err)
		}

		wsRestCandle = make([]exchange.WsRestCandle, len(restCandle))
		for i, c := range restCandle {
			wsRestCandle[i] = exchange.WsRestCandle{
				Time:   c.Time,
				Open:   c.Open,
				High:   c.High,
				Low:    c.Low,
				Close:  c.Close,
				Volume: c.Volume,
			}
		}
		logger.Info("[RestIngestVectorFlow] Candles fetched", "count", len(wsRestCandle))
		return nil
	})

	if err := g1.Wait(); err != nil {
		return fmt.Errorf("[RestIngestVectorFlow] phase 1: %w", err)
	}
	defer dbIngest.Close()

	// ── Phase 2: Calculate feature + label (concurrent) ──
	var (
		feature *embedding.PatternFeature // adjust to actual return type
		label   []embedding.LabelUpdate   // adjust to actual return type
	)

	g2, _ := errgroup.WithContext(ctx)

	g2.Go(func() error {
		fc := embedding.NewFeatureCalculator(symbol, interval, vectorSize)
		feature = fc.Calculate(wsRestCandle)
		logger.Info("[RestIngestVectorFlow] Feature calculated")
		return nil
	})

	g2.Go(func() error {
		lb := embedding.NewLabelCalculator()
		label = lb.CalculateFromHistory(wsRestCandle)
		logger.Info("[RestIngestVectorFlow] Label calculated")
		return nil
	})

	if err := g2.Wait(); err != nil {
		return fmt.Errorf("[RestIngestVectorFlow] phase 2: %w", err)
	}

	// ── Phase 3: Upsert to DB (sequential — same table, avoid row lock conflict) ──
	if err := dbIngest.UpsertFeature(ctx, *feature); err != nil {
		return fmt.Errorf("[RestIngestVectorFlow] upsert feature: %w", err)
	}
	logger.Info("[RestIngestVectorFlow] Ingested feature")

	if err := dbIngest.UpsertLabels(ctx, symbol, interval, label); err != nil {
		return fmt.Errorf("[RestIngestVectorFlow] upsert labels: %w", err)
	}
	logger.Info("[RestIngestVectorFlow] Ingested label")

	logger.Info("[RestIngestVectorFlow] Success ingested")
	return nil
}
