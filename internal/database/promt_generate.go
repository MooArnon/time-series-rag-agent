package database

import (
	"context"
	"fmt"
)

type PnLData struct {
	PositionOpenAt string
	NetPnL         float64
	SignalSide     string
	Regime4H       string
	Regime1D       string
	DurationHours  string
}

type HoldPositionData struct {
	Date     string
	Position string
}

// Query PnL data for the past n days
func (db *PostgresDB) QueryPnLData(context context.Context, numLookBack int) ([]PnLData, error) {
	fmt.Print("Query data")
	query := GetQueryPnLData(numLookBack)

	result, err := db.Pool.Query(context, query)
	if err != nil {
		fmt.Printf("Error executing query: %v\n", err)
		return nil, err
	}
	defer result.Close()

	var pnlData []PnLData
	for result.Next() {
		var data PnLData
		err := result.Scan(
			&data.PositionOpenAt,
			&data.NetPnL,
			&data.SignalSide,
			&data.Regime4H,
			&data.Regime1D,
			&data.DurationHours,
		)
		if err != nil {
			fmt.Printf("Error scanning row: %v\n", err)
			return nil, err
		}
		pnlData = append(pnlData, data)
	}
	return pnlData, nil
}

// Get Query
func GetQueryPnLData(days int) string {
	query := fmt.Sprintf(
		`
		SELECT
			p.open_timestamp::text as position_open_at
			, p.net_pnl
			, s.side AS sig_side
			, COALESCE(mr_4h.regime, 'UNKNOWN') AS regime_4h
			, COALESCE(mr_1d.regime, 'UNKNOWN') AS regime_1d
			, ROUND(EXTRACT(EPOCH FROM (current_timestamp - p.open_timestamp)) / 3600.0, 2) AS hour_ago
		FROM trading.position_history AS p
		JOIN trading.signal_log AS s ON p.symbol = s.symbol
			AND s.recorded_at < p.open_timestamp
			AND s.recorded_at > p.open_timestamp - INTERVAL '15 minutes'
		LEFT JOIN market_regime AS mr_4h
			ON p.symbol = mr_4h.symbol AND mr_4h.interval = '4h'
			AND mr_4h.time < p.open_timestamp
			AND mr_4h.time > p.open_timestamp - INTERVAL '4 hours'
		LEFT JOIN market_regime AS mr_1d
			ON p.symbol = mr_1d.symbol AND mr_1d.interval = '1d'
			AND mr_1d.time < p.open_timestamp
			AND mr_1d.time > p.open_timestamp - INTERVAL '1 day'
		WHERE s.side <> 'HOLD'
		ORDER BY p.recorded_at DESC
		LIMIT %d
		`,
		days,
	)
	return query
}
