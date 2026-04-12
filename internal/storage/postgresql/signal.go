package postgresql

import (
	"context"
	"fmt"
)

const insertTradeSignalSQL = `
INSERT INTO trade_signal_log (
    time, symbol, interval,
    signal, confidence,
    regime_read, pattern_read, price_action_read,
    synthesis, risk_note, invalidation,
    ws_close, executed, skip_reason
) VALUES (
    $1, $2, $3,
    $4, $5,
    $6, $7, $8,
    $9, $10, $11,
    $12, $13, $14
)
`

func (s *PatternStore) InsertTradeSignal(ctx context.Context, l TradeSignalLog) error {
	_, err := s.db.Exec(ctx, insertTradeSignalSQL,
		l.Time.Unix(), l.Symbol, l.Interval,
		l.Signal, l.Confidence,
		l.RegimeRead, l.PatternRead, l.PriceActionRead,
		l.Synthesis, l.RiskNote, l.Invalidation,
		l.WsClose, l.Executed, l.SkipReason,
	)
	if err != nil {
		return fmt.Errorf("InsertTradeSignal: %w", err)
	}
	return nil
}
