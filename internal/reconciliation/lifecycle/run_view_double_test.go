package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
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
	next    int
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
// run. It first retires the oldest runs no live view holds until fewer than
// journeyKeptRuns remain, then creates the file numbered one past the highest
// it ever opened, and one whose creation fails is removed again.
func (v *testView) OpenRun(ctx context.Context) (string, error) {
	held, ok := v.runs.(*heldArea)
	if !ok {
		return "", errors.New("a run opens only inside a bounded run")
	}
	if err := held.usable(ctx, true); err != nil {
		return "", err
	}
	area := held.memoryArea
	area.mutex.Lock()
	defer area.mutex.Unlock()
	runLedgers.Lock()
	defer runLedgers.Unlock()
	ledger := ledgerOf(area)
	ledger.retire(area)
	ledger.next++
	name := fmt.Sprintf("run-%06d.output", ledger.next)
	if err := area.admit(ctx, name, true); err != nil {
		return "", err
	}
	if _, exists := area.files[name]; exists {
		return "", errors.New("the run file exists")
	}
	area.files[name] = []byte{}
	if err := area.landed("open " + name); err != nil {
		delete(area.files, name)
		return "", err
	}
	ledger.opened = append(ledger.opened, name)
	ledger.holders[name] = v.closed
	return name, nil
}

// retire forgets runs already gone, then removes the oldest a closed view held
// until fewer than journeyKeptRuns remain.
func (l *runLedger) retire(area *memoryArea) {
	present := []string{}
	for _, name := range l.opened {
		if _, found := area.files[name]; found {
			present = append(present, name)
		} else {
			delete(l.holders, name)
		}
	}
	kept, remaining := []string{}, len(present)
	for _, name := range present {
		if remaining >= journeyKeptRuns && l.holders[name].Load() {
			delete(area.files, name)
			delete(l.holders, name)
			remaining--
			continue
		}
		kept = append(kept, name)
	}
	l.opened = kept
}
