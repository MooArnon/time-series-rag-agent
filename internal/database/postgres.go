package database

import (
	"context"
	"fmt"
	"time"

	"time-series-rag-agent/internal/ai"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

type PostgresDB struct {
	Pool *pgxpool.Pool
}

type TradingLog struct {
	Signal     string
	Reason     string
	CandleKey  string
	ChartKey   string
	Symbol     string
	RecordedAt string
}

func NewPostgresDB(connString string) (*PostgresDB, error) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, err
	}
	return &PostgresDB{Pool: pool}, nil
}

// IngestPattern handles the "Parallel Flow":
// 1. Saves the NEW pattern (T)
// 2. Updates the OLD patterns (T-1, T-3, etc) with new labels
func (db *PostgresDB) IngestPattern(ctx context.Context, feature *ai.PatternFeature, labels []ai.LabelUpdate) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// --- 1. Insert/Upsert the Current Feature (T) ---
	// We save the Embedding NOW. The labels (next_return, slope) are NULL for now.
	q1 := `
		INSERT INTO market_pattern_go (time, symbol, interval, close_price, embedding)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (symbol, interval, time) 
		DO UPDATE SET embedding = $5, close_price = $4;
	`
	// FIXED: Convert []float64 -> []float32 for pgvector
	embedding32 := make([]float32, len(feature.Embedding))
	for i, v := range feature.Embedding {
		embedding32[i] = float32(v)
	}

	// Convert []float64 to pgvector.Vector
	vec := pgvector.NewVector(embedding32)

	_, err = tx.Exec(ctx, q1,
		feature.Time.Unix(),
		feature.Symbol,
		feature.Interval,
		feature.ClosePrice,
		vec,
	)
	if err != nil {
		return fmt.Errorf("failed to insert feature: %w", err)
	}

	// --- 2. Update Past Labels (T-1, T-3, T-5) ---
	// "Labels" contains the computed truth for past timestamps.
	if len(labels) > 0 {
		batch := &pgx.Batch{}

		for _, lbl := range labels {
			// Dynamic update query based on which column we calculated
			// (next_return, next_slope_3, etc)
			query := fmt.Sprintf(
				"UPDATE market_pattern_go SET %s = $1 WHERE time = $2 AND symbol = $3 AND interval = $4",
				lbl.Column, // Safe because we control the column names in AI package
			)

			batch.Queue(query, lbl.Value, lbl.TargetTime, feature.Symbol, feature.Interval)
		}

		br := tx.SendBatch(ctx, batch)
		err = br.Close()
		if err != nil {
			return fmt.Errorf("failed to update batch labels: %w", err)
		}
	}

	return tx.Commit(ctx)
}

func (db *PostgresDB) Close() {
	db.Pool.Close()
}

// BulkSave inserts many patterns at once (optimized for Backfill)
func (db *PostgresDB) BulkSave(ctx context.Context, results []ai.BulkResult) error {
	batch := &pgx.Batch{}

	query := `
        INSERT INTO market_pattern_go (
            time, symbol, interval, close_price, embedding, 
            next_return, next_slope_3, next_slope_5
        ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
        ON CONFLICT (time, symbol, interval) DO UPDATE SET
            embedding = EXCLUDED.embedding,
            next_return = EXCLUDED.next_return,
            next_slope_3 = EXCLUDED.next_slope_3,
            next_slope_5 = EXCLUDED.next_slope_5;
    `

	for _, res := range results {
		// 1. Prepare Vector
		embedding32 := make([]float32, len(res.Features.Embedding))
		for i, v := range res.Features.Embedding {
			embedding32[i] = float32(v)
		}
		vec := pgvector.NewVector(embedding32)

		// 2. Map Labels to Columns
		// BulkResult.Labels contains dynamic rows, we need to flatten them to columns
		var nextRet, slope3, slope5 *float64 // Use pointers to handle NULLs

		for _, lbl := range res.Labels {
			val := lbl.Value
			switch lbl.Column {
			case "next_return":
				nextRet = &val
			case "next_slope_3":
				slope3 = &val
			case "next_slope_5":
				slope5 = &val
			}
		}

		// 3. Queue the Batch
		// res.Features.Time is time.Time, convert to Unix() for BIGINT schema
		batch.Queue(query,
			res.Features.Time.Unix(),
			res.Features.Symbol,
			res.Features.Interval,
			res.Features.ClosePrice,
			vec,
			nextRet, slope3, slope5,
		)
	}

	// 4. Execute
	br := db.Pool.SendBatch(ctx, batch)
	defer br.Close()

	_, err := br.Exec()
	return err
}

func (db *PostgresDB) SearchPatterns(ctx context.Context, queryVec []float64, k int, currentSymbol string) ([]ai.PatternLabel, error) {
	embedding32 := make([]float32, len(queryVec))
	for i, v := range queryVec {
		embedding32[i] = float32(v)
	}
	qVec := pgvector.NewVector(embedding32)

	// UPDATE QUERY: Add "embedding <=> $1" to SELECT list
	sql := `
        SELECT 
            time, symbol, interval, 
            next_return, next_slope_3, next_slope_5, 
            embedding,
            (embedding <=> $1) as distance  -- <--- Fetch Distance
        FROM market_pattern_go
        WHERE next_return IS NOT NULL
        ORDER BY distance ASC
        LIMIT $2
    `

	rows, err := db.Pool.Query(ctx, sql, qVec, k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ai.PatternLabel

	for rows.Next() {
		var r ai.PatternLabel
		var rawTime int64
		var slope3, slope5 *float64
		var vec pgvector.Vector

		// UPDATE SCAN: Add &r.Distance at the end
		err := rows.Scan(
			&rawTime, &r.Symbol, &r.Interval,
			&r.NextReturn, &slope3, &slope5,
			&vec,
			&r.Distance, // <--- Scan into struct
		)
		if err != nil {
			return nil, err
		}

		r.Time = time.Unix(rawTime, 0).UTC()
		if slope3 != nil {
			r.NextSlope3 = *slope3
		}
		if slope5 != nil {
			r.NextSlope5 = *slope5
		}

		r.Embedding = make([]float64, len(vec.Slice()))
		for i, v := range vec.Slice() {
			r.Embedding[i] = float64(v)
		}

		results = append(results, r)
	}

	return results, nil
}

func (db *PostgresDB) IngestTradingLog(ctx context.Context, tradingLog TradingLog) error {
	fmt.Print("Processing Ingetion")
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// --- 1. Insert/Upsert the Current Feature (T) ---
	// We save the Embedding NOW. The labels (next_return, slope) are NULL for now.
	q1 := `
    INSERT INTO trading.signal_log (
        recorded_at,
        created_at,
        market,
        symbol,
        side,
        reason,
        candle_prefix,
        chart_prefix
    )
    VALUES (
        $1,                -- Map to tradingLog.RecordedAt
        current_timestamp, 
        0, 
        $2,                -- Map to tradingLog.Symbol
        $3,                -- Map to tradingLog.Signal
        $4,                -- Map to tradingLog.Reason
        $5,                -- Map to tradingLog.CandleKey
        $6                 -- Map to tradingLog.ChartKey
    )
    ON CONFLICT (recorded_at, symbol)
    DO NOTHING
`
	_, err = tx.Exec(ctx, q1,
		tradingLog.RecordedAt,
		tradingLog.Symbol,
		tradingLog.Signal,
		tradingLog.Reason,
		tradingLog.CandleKey,
		tradingLog.ChartKey,
	)
	if err != nil {
		return fmt.Errorf("failed to insert feature: %w", err)
	}

	return tx.Commit(ctx)
}
