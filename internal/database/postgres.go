package database
package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
	"time-series-rag-agent/internal/ai"
)

type PostgresDB struct {
	Pool *pgxpool.Pool
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
		INSERT INTO market_patterns (time, symbol, interval, close_price, embedding)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (symbol, interval, time) 
		DO UPDATE SET embedding = $5, close_price = $4;
	`
	// Convert []float64 to pgvector.Vector
	vec := pgvector.NewVector(feature.Embedding)
	
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
				"UPDATE market_patterns SET %s = $1 WHERE time = $2 AND symbol = $3 AND interval = $4", 
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