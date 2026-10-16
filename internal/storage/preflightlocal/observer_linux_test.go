package preflightlocal

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/storage/preflight"
)

func TestMemoryFactsRequireOneBoundedQuantity(t *testing.T) {
	for _, row := range []struct {
		input string
		value int64
		ok    bool
	}{{"MemAvailable: 12 kB\n", 12288, true}, {"MemFree: 12 kB\n", 0, false}, {"MemAvailable: 12 MB\n", 0, false}, {"MemAvailable: -1 kB\n", 0, false}, {"MemAvailable: 9223372036854775807 kB\n", 0, false}, {"MemAvailable: 1 kB\nMemAvailable: 2 kB\n", 0, false}} {
		got, ok := parseMemory([]byte(row.input))
		if got != row.value || ok != row.ok {
			t.Fatalf("%q: %d %t", row.input, got, ok)
		}
	}
}
func TestRemoteTargetNeverReadsLocalFacts(t *testing.T) {
	facts, err := (Observer{}).Observe(context.Background(), preflight.Target{Host: "remote", Pool: "/ignored"})
	if err != nil || facts.CPUKnown || facts.MemoryKnown || facts.DiskKnown || facts.Reason == "" {
		t.Fatalf("%+v %v", facts, err)
	}
}
