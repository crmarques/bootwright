package lifecycle

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/crmarques/bootwright/internal/reconciliation"
)

// journeyKeptRuns is how many runs the journey double's run area keeps: few
// enough that a test reaches its retention in a handful of runs.
const journeyKeptRuns = 3

// JourneyKeptRuns is that count, which the workspace contract holds the
// journey double to.
const JourneyKeptRuns = journeyKeptRuns

// runLedger is what one run area of the journey double opened, oldest first,
// with the closed flag of the view holding each, so its retention tells a run
// still in progress from one whose callback returned.
type runLedger struct {
	opened  []string
	holders map[string]*atomic.Bool
}

// runLedgers keeps each area's ledger beside the double rather than in it, by
// area, so the shared double needs no field for a capability only bounded runs
// use.
var runLedgers = struct {
	sync.Mutex
	byArea map[*memoryArea]*runLedger
}{byArea: map[*memoryArea]*runLedger{}}

func ledgerOf(area *memoryArea) *runLedger {
	ledger := runLedgers.byArea[area]
	if ledger == nil {
		ledger = &runLedger{holders: map[string]*atomic.Bool{}}
		runLedgers.byArea[area] = ledger
	}
	return ledger
}

// openedRuns lists, oldest first, the runs one area opened that it still
// keeps.
func openedRuns(area *memoryArea) []string {
	runLedgers.Lock()
	defer runLedgers.Unlock()
	return append([]string{}, ledgerOf(area).opened...)
}

// OpenRun is the double's run opening: only a bounded run's live view opens a
// run, under a run identity no entry of the area holds. It first retires the
// oldest runs no live view holds until fewer than journeyKeptRuns remain, then
// creates the directory, and one whose creation fails once it exists is
// removed again.
func (v *testView) OpenRun(ctx context.Context, identity string) error {
	held, ok := v.runs.(*heldArea)
	if !ok {
		return errors.New("a run opens only inside a bounded run")
	}
	if err := held.usable(ctx, true); err != nil {
		return err
	}
	if !reconciliation.ValidRunID(identity) {
		return errors.New("a bounded run's directory requires a run identity")
	}
	area := held.memoryArea
	area.mutex.Lock()
	defer area.mutex.Unlock()
	if err := area.admit(ctx, identity, false); err != nil {
		return err
	}
	if area.directories[identity] || runHolds(area, identity) {
		return errors.New("the run directory exists")
	}
	runLedgers.Lock()
	defer runLedgers.Unlock()
	ledger := ledgerOf(area)
	ledger.retire(area)
	area.directories[identity] = true
	if err := area.landed("open " + identity); err != nil {
		delete(area.directories, identity)
		return err
	}
	ledger.opened = append(ledger.opened, identity)
	ledger.holders[identity] = v.closed
	return nil
}

// retire forgets runs already gone, then removes the oldest a closed view held
// until fewer than journeyKeptRuns remain, each only while it holds nothing
// but its own output.
func (l *runLedger) retire(area *memoryArea) {
	present := []string{}
	for _, identity := range l.opened {
		if area.directories[identity] {
			present = append(present, identity)
		} else {
			delete(l.holders, identity)
		}
	}
	kept, remaining := []string{}, len(present)
	for _, identity := range present {
		if remaining >= journeyKeptRuns && l.holders[identity].Load() && removeRun(area, identity) {
			delete(l.holders, identity)
			remaining--
			continue
		}
		kept = append(kept, identity)
	}
	l.opened = kept
}

// runHolds reports whether anything lies beneath one run's directory.
func runHolds(area *memoryArea, identity string) bool {
	for name := range area.files {
		if strings.HasPrefix(name, identity+"/") {
			return true
		}
	}
	for name := range area.directories {
		if strings.HasPrefix(name, identity+"/") {
			return true
		}
	}
	return false
}

func removeRun(area *memoryArea, identity string) bool {
	output := identity + "/" + RunOutputName
	for name := range area.files {
		if strings.HasPrefix(name, identity+"/") && name != output {
			return false
		}
	}
	for name := range area.directories {
		if strings.HasPrefix(name, identity+"/") {
			return false
		}
	}
	delete(area.files, output)
	delete(area.directories, identity)
	return true
}
