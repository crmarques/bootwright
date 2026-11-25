package lifecycle

import (
	"fmt"
	"math/bits"
	"strconv"
	"time"
)

type ByteProgress struct {
	Completed int64
	Total     int64
	Note      string
	ToolLog   string
}

func (p ByteProgress) Known() bool {
	return p.Total > 0 && p.Completed >= 0 && p.Completed <= p.Total
}

func (p ByteProgress) Percent() int {
	if !p.Known() {
		return 0
	}
	high, low := bits.Mul64(uint64(p.Completed), 100)
	percent, _ := bits.Div64(high, low, uint64(p.Total))
	return int(percent)
}

const (
	MeterWarmup      = 10 * time.Second
	MeterLogInterval = time.Minute
	meterLogStep     = 5
)

type ByteMeter struct {
	started   time.Time
	first     int64
	last      ByteProgress
	logged    time.Time
	loggedPct int
	seen      bool
	logSeen   bool
}

func (m *ByteMeter) Observe(now time.Time, progress ByteProgress) (string, bool) {
	if !progress.Known() {
		return "", false
	}
	if !m.seen || progress.Completed < m.last.Completed || progress.Total != m.last.Total {
		m.started, m.first, m.seen = now, progress.Completed, true
	}
	m.last = progress
	percent := progress.Percent()
	due := !m.logSeen || now.Sub(m.logged) >= MeterLogInterval || percent/meterLogStep > m.loggedPct/meterLogStep
	if due {
		m.logged, m.loggedPct, m.logSeen = now, percent, true
	}
	return m.text(now), due
}

func (m *ByteMeter) text(now time.Time) string {
	progress := m.last
	text := fmt.Sprintf("%d%% (%s of %s)", progress.Percent(), FormatBytes(progress.Completed), FormatBytes(progress.Total))
	elapsed := now.Sub(m.started)
	moved := progress.Completed - m.first
	if progress.Completed == progress.Total || elapsed < MeterWarmup || moved <= 0 {
		return text
	}
	rate := int64(float64(moved) / elapsed.Seconds())
	if rate <= 0 {
		return text
	}
	left := time.Duration(float64(progress.Total-progress.Completed) / float64(rate) * float64(time.Second))
	return text + ", " + FormatBytes(rate) + "/s, about " + formatLeft(left) + " left at the observed rate"
}

func FormatBytes(size int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB", "EiB"}
	unit, scaled := 0, float64(size)
	for scaled >= 1024 && unit < len(units)-1 {
		scaled /= 1024
		unit++
	}
	if unit == 0 {
		return strconv.FormatInt(size, 10) + " B"
	}
	return strconv.FormatFloat(scaled, 'f', 1, 64) + " " + units[unit]
}

func formatLeft(left time.Duration) string {
	minutes := int((left + 30*time.Second) / time.Minute)
	switch {
	case minutes < 1:
		return "under a minute"
	case minutes < 60:
		return strconv.Itoa(minutes) + "m"
	case minutes < 48*60:
		return fmt.Sprintf("%dh%02dm", minutes/60, minutes%60)
	}
	return strconv.Itoa(minutes/(24*60)) + " days"
}
