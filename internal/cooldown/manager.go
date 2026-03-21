package cooldown

import (
	"fmt"
	"time"
)

// Rules:
//   1 SL            → wait 2 bars
//   consecutive SL  → wait 4 bars
//   1 win           → reset all (unlock immediately)

func New(interval time.Duration) *Manager {
	return &Manager{interval: interval}
}

// --- State transitions ---

func (m *Manager) LastSLTime() time.Time { return m.lastSLTime }

func (m *Manager) RecordStopLoss(barTime time.Time, slTime time.Time) {
	m.consecutiveLosses++
	m.lastSLTime = slTime // ← เก็บ timestamp จาก Binance
	bars := m.barsToWait()
	m.resumeAfter = barTime.Add(time.Duration(bars) * m.interval)
}

func (m *Manager) RecordWin() {
	m.consecutiveLosses = 0
	m.resumeAfter = time.Time{} // clear immediately
}

// --- Query ---

func (m *Manager) IsInCooldown(barTime time.Time) bool {
	return !m.resumeAfter.IsZero() && barTime.Before(m.resumeAfter)
}

func (m *Manager) BarsRemaining(barTime time.Time) int {
	if !m.IsInCooldown(barTime) {
		return 0
	}
	diff := m.resumeAfter.Sub(barTime)
	n := int(diff / m.interval)
	if diff%m.interval > 0 {
		n++ // round up
	}
	return n
}

func (m *Manager) ConsecutiveLosses() int { return m.consecutiveLosses }

func (m *Manager) Status(barTime time.Time) string {
	if m.IsInCooldown(barTime) {
		return fmt.Sprintf(
			"🔴 COOLDOWN  consecutive_sl=%d  bars_remaining=%d  resume_after=%s",
			m.consecutiveLosses,
			m.BarsRemaining(barTime),
			m.resumeAfter.Format("2006-01-02 15:04:05 MST"),
		)
	}
	return fmt.Sprintf("🟢 READY  consecutive_sl=%d", m.consecutiveLosses)
}

// --- internal ---

func (m *Manager) barsToWait() int {
	if m.consecutiveLosses <= 1 {
		return 2
	}
	return 4
}
