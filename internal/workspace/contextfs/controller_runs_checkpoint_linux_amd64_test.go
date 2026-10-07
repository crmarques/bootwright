//go:build linux && amd64

package contextfs

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// checkpointSetupRunPrinted is what the scenario's Ansible prints.
const checkpointSetupRunPrinted = "the setup run the harness interrupts\n"

// checkpointSetupRunFixture is a host whose receipt holds a durable intent,
// which is where a setup starts its Ansible, already keeping its newest runs,
// so the next run retires the oldest before it is created.
func checkpointSetupRunFixture(t *testing.T) *Store {
	t.Helper()
	store, _ := intendedControllerFixture(t)
	for number := 1; number <= maxSetupRuns; number++ {
		plantRun(t, store, setupRunName(number), []byte(fmt.Sprintf("run %d\n", number)))
	}
	return store
}

func checkpointSetupRunScenario() checkpointScenario {
	open := func(_ *testing.T, _ context.Context, store *Store) error {
		_, err := keepRun(store, checkpointSetupRunPrinted)
		return err
	}
	return checkpointControllerScenario(checkpointScenario{
		name:    "setup-run",
		prepare: checkpointSetupRunFixture,
		operate: open,
		// specs/contexts/controller-record.md, Setup runs: a run is created
		// exclusively and never reuses a name, and a killed run's shape stays
		// admitted, so the next setup opens the next run, retiring the oldest
		// first, whatever an interrupted one left.
		retry: open,
		settled: func(_ *testing.T, ctx context.Context, store *Store) error {
			numbers, err := checkpointSetupRuns(store)
			if err != nil {
				return err
			}
			if len(numbers) != maxSetupRuns || numbers[len(numbers)-1] <= maxSetupRuns {
				return fmt.Errorf("the host keeps runs %v, want the newest %d past setup-%06d", numbers, maxSetupRuns, maxSetupRuns)
			}
			for index := 1; index < len(numbers); index++ {
				if numbers[index] != numbers[index-1]+1 {
					return fmt.Errorf("the kept runs %v are not the newest", numbers)
				}
			}
			newest, err := os.ReadFile(filepath.Join(runsPath(store), setupRunName(numbers[len(numbers)-1]), setupRunOutputName))
			if err != nil || string(newest) != checkpointSetupRunPrinted {
				return fmt.Errorf("the newest run holds %q (%v)", newest, err)
			}
			return nil
		},
	}, "")
}

// checkpointSetupRuns numbers the runs the host keeps, in order; the scenario's
// reads already passed them through the store's admission.
func checkpointSetupRuns(store *Store) ([]int, error) {
	runs, err := os.ReadDir(runsPath(store))
	if err != nil {
		return nil, err
	}
	var numbers []int
	for _, run := range runs {
		number, valid := setupRunNumber(run.Name())
		if !valid {
			return nil, fmt.Errorf("the runs directory holds %s", run.Name())
		}
		numbers = append(numbers, number)
	}
	return numbers, nil
}
