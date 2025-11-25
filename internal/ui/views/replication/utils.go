package replication

import (
	"fmt"
	"time"

	"github.com/mattn/go-runewidth"

	"github.com/willibrandon/steep/internal/db/models"
)

func truncateWithEllipsis(s string, maxLen int) string {
	if runewidth.StringWidth(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return runewidth.Truncate(s, maxLen-1, "…")
}

func clamp(val, minVal, maxVal int) int {
	if val < minVal {
		return minVal
	}
	if val > maxVal {
		return maxVal
	}
	return val
}

func formatDuration(d time.Duration) string {
	if d == 0 {
		return "-"
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	return fmt.Sprintf("%.1fm", d.Minutes())
}

func sortByFunc(replicas []models.Replica, less func(a, b models.Replica) bool) {
	for i := 0; i < len(replicas)-1; i++ {
		for j := i + 1; j < len(replicas); j++ {
			if less(replicas[j], replicas[i]) {
				replicas[i], replicas[j] = replicas[j], replicas[i]
			}
		}
	}
}
