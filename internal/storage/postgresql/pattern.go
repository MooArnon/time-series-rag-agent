package postgresql

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"

	"time-series-rag-agent/internal/embedding"

	"log/slog"
)

const upsertPatternSQL = `
INSERT INTO market_pattern_go (
    time, symbol, interval,
    embedding,
    close_price, next_return, next_slope_3, next_slope_5
)
VALUES ($1, $2, $3, $4::vector, $5, $6, $7, $8)
ON CONFLICT (time, symbol, interval) DO UPDATE SET
    embedding   = EXCLUDED.embedding,
    close_price = EXCLUDED.close_price,
	next_return  = COALESCE(EXCLUDED.next_return,  market_pattern_go.next_return),
	next_slope_3 = COALESCE(EXCLUDED.next_slope_3, market_pattern_go.next_slope_3),
	next_slope_5 = COALESCE(EXCLUDED.next_slope_5, market_pattern_go.next_slope_5)
`

type PatternStore struct {
	db     *pgxpool.Pool
	logger slog.Logger
}

func NewPostgresDB(ctx context.Context, connString string, logger slog.Logger) (*PatternStore, error) {
	pool, err := pgxpool.New(ctx, connString)
	if err != nil {
		return nil, err
	}
	return &PatternStore{db: pool, logger: logger}, nil
}

// UpsertFeature inserts or updates embedding + close_price for a given candle time.
func (s *PatternStore) UpsertFeature(ctx context.Context, f embedding.PatternFeature) error {
	vec := make([]float32, len(f.Embedding))
	for i, v := range f.Embedding {
		vec[i] = float32(v)
	}

	_, err := s.db.Exec(ctx, upsertPatternSQL,
		f.Time.Unix(),
		f.Symbol,
		f.Interval,
		pgvector.NewVector(vec),
		f.ClosePrice,
		nil, nil, nil,
	)
	if err != nil {
		return fmt.Errorf("UpsertFeature: %w", err)
	}
	return nil
}

// UpsertLabels updates label columns for past candles.
// Each LabelUpdate targets a specific (TargetTime, symbol, interval) row.
func (s *PatternStore) UpsertLabels(ctx context.Context, symbol, interval string, labels []embedding.LabelUpdate) error {
	if len(labels) == 0 {
		return nil
	}

	// group by column เพราะแต่ละ column ต้องใช้ SQL ต่างกัน
	grouped := map[string][]embedding.LabelUpdate{}
	for _, l := range labels {
		if _, err := validateLabelColumn(l.Column); err != nil {
			return err
		}
		grouped[l.Column] = append(grouped[l.Column], l)
	}

	for col, group := range grouped {
		if err := s.bulkUpsertLabelColumn(ctx, symbol, interval, col, group); err != nil {
			return err
		}
	}
	return nil
}

func (s *PatternStore) bulkUpsertLabelColumn(ctx context.Context, symbol, interval, col string, labels []embedding.LabelUpdate) error {
	const batchSize = 1000

	for i := 0; i < len(labels); i += batchSize {
		end := i + batchSize
		if end > len(labels) {
			end = len(labels)
		}
		batch := labels[i:end]

		times := make([]int64, len(batch))
		values := make([]float64, len(batch))
		symbols := make([]string, len(batch))
		intervals := make([]string, len(batch))

		for j, l := range batch {
			times[j] = l.TargetTime
			values[j] = l.Value
			symbols[j] = symbol
			intervals[j] = interval
		}

		sql := fmt.Sprintf(`
            INSERT INTO market_pattern_go (time, symbol, interval, %s)
            SELECT
                UNNEST($1::bigint[]),
                UNNEST($2::text[]),
                UNNEST($3::text[]),
                UNNEST($4::float8[])
            ON CONFLICT (time, symbol, interval) DO UPDATE SET
                %s = EXCLUDED.%s
        `, col, col, col)

		if _, err := s.db.Exec(ctx, sql, times, symbols, intervals, values); err != nil {
			return fmt.Errorf("bulkUpsertLabelColumn [%s batch %d-%d]: %w", col, i, end, err)
		}
	}
	return nil
}

// QueryTopN returns the N most similar rows to the given embedding using cosine distance.
func (s *PatternStore) QueryTopN(ctx context.Context, symbol, interval string, queryEmbedding []float64, topN int) ([]embedding.PatternLabel, error) {
	sql := `
		SELECT
			time, symbol, interval,
			close_price, next_return, next_slope_3, next_slope_5,
			embedding,
			embedding <=> $1 AS distance
		FROM market_pattern_go
		WHERE symbol   = $2
			AND interval = $3
			AND embedding IS NOT NULL
		ORDER BY embedding <=> $1
		LIMIT $4
	`

	s.logger.Info(fmt.Sprintf("Querying with param: symbol=%s, interval=%s, topN=%d", symbol, interval, topN))
	rows, err := s.db.Query(ctx, sql, toVectorLiteral(queryEmbedding), symbol, interval, topN)

	if err != nil {
		return nil, fmt.Errorf("QueryTopN: %w", err)
	}
	defer rows.Close()

	var results []embedding.PatternLabel
	for rows.Next() {
		var (
			unixTime   int64
			sym        string
			intv       string
			closePrice float64
			nextReturn *float64
			nextSlope3 *float64
			nextSlope5 *float64
			Embedding  pgvector.Vector
			distance   float64
		)
		if err := rows.Scan(&unixTime, &sym, &intv, &closePrice, &nextReturn, &nextSlope3, &nextSlope5, &Embedding, &distance); err != nil {
			return nil, fmt.Errorf("QueryTopN scan: %w", err)
		}
		results = append(results, embedding.PatternLabel{
			Time:       time.Unix(unixTime, 0),
			Symbol:     sym,
			Interval:   intv,
			ClosePrice: closePrice,
			NextReturn: derefOr(nextReturn, 0),
			NextSlope3: derefOr(nextSlope3, 0),
			NextSlope5: derefOr(nextSlope5, 0),
			Embedding:  Embedding,
			Distance:   distance,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("QueryTopN rows: %w", err)
	}

	return results, nil
}

// --- helpers ---

// toVectorLiteral converts []float64 to pgvector literal e.g. "[0.1,0.2,0.3]"
func toVectorLiteral(v []float64) string {
	if len(v) == 0 {
		return "[]"
	}
	s := "["
	for i, f := range v {
		if i > 0 {
			s += ","
		}
		s += fmt.Sprintf("%.10f", f)
	}
	s += "]"
	return s
}

// validateLabelColumn whitelists allowed column names to prevent SQL injection.
func validateLabelColumn(col string) (string, error) {
	allowed := map[string]bool{
		"next_return":  true,
		"next_slope_3": true,
		"next_slope_5": true,
	}
	if !allowed[col] {
		return "", fmt.Errorf("invalid label column: %q", col)
	}
	return col, nil
}

func derefOr(v *float64, fallback float64) float64 {
	if v == nil {
		return fallback
	}
	return *v
}

func (s *PatternStore) BulkUpsertFeature(ctx context.Context, features []embedding.PatternFeature) error {
	if len(features) == 0 {
		return nil
	}

	const batchSize = 1000

	for i := 0; i < len(features); i += batchSize {
		s.logger.Info(fmt.Sprintln("Upserting feature: ", i))
		end := i + batchSize
		if end > len(features) {
			end = len(features)
		}
		batch := features[i:end]

		if err := s.upsertFeatureBatch(ctx, batch); err != nil {
			return fmt.Errorf("BulkUpsertFeature batch %d-%d: %w", i, end, err)
		}
	}
	return nil
}

func (s *PatternStore) upsertFeatureBatch(ctx context.Context, features []embedding.PatternFeature) error {
	times := make([]int64, len(features))
	symbols := make([]string, len(features))
	intervals := make([]string, len(features))
	embeddings := make([]string, len(features))
	closePrices := make([]float64, len(features))

	for i, f := range features {
		times[i] = f.Time.Unix()
		symbols[i] = f.Symbol
		intervals[i] = f.Interval
		embeddings[i] = toVectorLiteral(f.Embedding)
		closePrices[i] = f.ClosePrice
	}

	_, err := s.db.Exec(ctx, `
        INSERT INTO market_pattern_go (time, symbol, interval, embedding, close_price)
        SELECT
            UNNEST($1::bigint[]),
            UNNEST($2::text[]),
            UNNEST($3::text[]),
            UNNEST($4::text[])::vector,
            UNNEST($5::float8[])
        ON CONFLICT (time, symbol, interval) DO UPDATE SET
            embedding   = EXCLUDED.embedding,
            close_price = EXCLUDED.close_price
    `, times, symbols, intervals, embeddings, closePrices)
	if err != nil {
		return err
	}
	return nil
}

func (s *PatternStore) Close() {
	s.db.Close()
}
