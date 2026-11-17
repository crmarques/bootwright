package cli

import (
	"sync"
	"time"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type byteMeters struct {
	mu     sync.Mutex
	meters map[string]*lifecycle.ByteMeter
}

func (m *byteMeters) text(now time.Time, event lifecycle.ProgressEvent) string {
	key := event.Block + "\x00" + event.Group
	m.mu.Lock()
	defer m.mu.Unlock()
	if event.Group == "" || event.Status != "running" || !event.Bytes.Known() {
		delete(m.meters, key)
		return ""
	}
	if m.meters == nil {
		m.meters = map[string]*lifecycle.ByteMeter{}
	}
	meter := m.meters[key]
	if meter == nil {
		meter = &lifecycle.ByteMeter{}
		m.meters[key] = meter
	}
	text, _ := meter.Observe(now, event.Bytes)
	return text
}
