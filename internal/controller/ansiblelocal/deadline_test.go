package ansiblelocal

import (
	"testing"
	"time"
)

func TestAnAcquisitionDeadlineScalesWithItsSourceBytes(t *testing.T) {
	for bytes, want := range map[int64]time.Duration{
		1:          121 * time.Second,
		524_288:    121 * time.Second,
		524_289:    122 * time.Second,
		44_433_552: 205 * time.Second,
		1 << 30:    2_168 * time.Second,
	} {
		if got := acquisitionDeadline(bytes); got != want {
			t.Errorf("acquisitionDeadline(%d) = %s, want %s", bytes, got, want)
		}
	}
}
