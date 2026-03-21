package cooldown

import "time"

type Manager struct {
	interval          time.Duration
	consecutiveLosses int
	resumeAfter       time.Time
	lastSLTime        time.Time // ← เพิ่ม
}
