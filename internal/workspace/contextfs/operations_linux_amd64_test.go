//go:build linux && amd64

package contextfs

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

// An operation that ran its blocks holds what FirstPassEntries counts for its
// plan, and admission counts the area as its entry bound does. Copies of a
// one-block operation that ran its block fill the area to where it still
// admits one more such apply beside its removal's first pass and
// ReservedEntries, but not one of two blocks. The one-block apply claims,
// registers and runs its block, whose later attempt still writes, and the next
// claim refuses at the retained-operation bound (lifecycle.state) before it
// creates anything, rather than at the area's storage (context.state). The
// removal of that last apply fits beside what it left, and filled until it has
// exactly what admission needs, it registers, runs its block and completes,
// leaving one entry free.
func TestTheOperationAreaAdmitsEveryRetainedOperation(t *testing.T) {
	ctx := context.Background()
	store, record := lifecycleFixture(t)
	block := func(id string) reconciliation.BlockDefinition {
		return reconciliation.BlockDefinition{
			ID: id, Description: "serve " + id, Stage: reconciliation.StageInfraComponents,
			Kind: "ArtifactServer", Object: id, Implementation: "artifact-server-nginx-v1",
			ContentDigest: strings.Repeat("a", 64), Request: json.RawMessage(`{"name":"` + id + `"}`),
		}
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{block("alpha")})
	if err != nil {
		t.Fatal(err)
	}
	wider, err := reconciliation.NewPlan(reconciliation.Apply, []reconciliation.BlockDefinition{block("alpha"), block("beta")})
	if err != nil {
		t.Fatal(err)
	}
	removal, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	identity := func(index int) string { return fmt.Sprintf("op-%032x", index) }
	clock := func() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) }
	attempt := func(operations *operationstore.Store, id string, done bool) error {
		number, err := operations.StartAttempt(ctx, id, "alpha")
		if err != nil {
			return err
		}
		target, err := operationstore.AttemptLogPath(id, "alpha", number, 0)
		if err != nil {
			return err
		}
		log, err := operations.OpenLog(ctx, target)
		if err != nil {
			return err
		}
		output, err := operationstore.AdapterOutputPath(target)
		if err != nil {
			return err
		}
		retained := operations.OpenAdapterOutput(ctx, output)
		if _, err := retained.Write([]byte("PLAY [serve alpha]\n")); err != nil {
			return err
		}
		if err := retained.Close(ctx); err != nil {
			return err
		}
		if err := log.Close(ctx); err != nil {
			return err
		}
		if done {
			return operations.CompleteAttempt(ctx, id, "alpha", number, reconciliation.OutcomeChanged, reconciliation.EffectCompleted, reconciliation.BlockDone, json.RawMessage(`{"postcondition":true}`))
		}
		return operations.CompleteAttempt(ctx, id, "alpha", number, reconciliation.OutcomeFailed, reconciliation.EffectUnknown, reconciliation.BlockFailed, nil)
	}
	register := func(operations *operationstore.Store, id, source string, plan reconciliation.Plan) (operationstore.Operation, error) {
		digest, err := plan.Digest()
		if err != nil {
			return operationstore.Operation{}, err
		}
		operation := operationstore.Operation{
			Version: operationstore.OperationVersion, ID: id, Verb: plan.Verb, Source: source, Context: record.Name, Revision: "rev-" + strings.Repeat("ef", 16),
			InputDigest: strings.Repeat("1", 64), PlanDigest: digest, AutomationDigest: strings.Repeat("2", 64),
			Executable: operationstore.Executable{Version: "devel", Commit: "abcdef1"},
			Closure:    &operationstore.Closure{Digest: strings.Repeat("3", 64), Python: "3.14.7", Ansible: "2.21.4"},
			Bindings:   []string{}, State: reconciliation.OperationRunning,
			Created: "2026-09-30T12:00:00Z", Updated: "2026-09-30T12:00:00Z",
		}
		if err := operations.Register(ctx, operation, plan); err != nil {
			return operation, err
		}
		log, err := operations.OpenLog(ctx, operationstore.OperationLogPath(id))
		if err != nil {
			return operation, err
		}
		return operation, log.Close(ctx)
	}
	run := func(tx lifecycle.Transaction, id string, done bool) (*operationstore.Store, error) {
		operations := operationstore.New(tx.Operations(), clock)
		if _, err := operations.Index(ctx); err != nil {
			return nil, err
		}
		if err := operations.Claim(ctx, id, plan); err != nil {
			return nil, err
		}
		if _, err := register(operations, id, "", plan); err != nil {
			return nil, err
		}
		return operations, attempt(operations, id, done)
	}
	first := identity(0)
	if err := store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		_, err := run(tx, first, true)
		return err
	}); err != nil {
		t.Fatal(causeText(err))
	}
	area := filepath.Join(store.options.Root, "contexts", record.Name, "state", "operations")
	template := filepath.Join(area, first)
	directories, files := []string{"."}, map[string][]byte{}
	if err := filepath.WalkDir(template, func(name string, entry fs.DirEntry, err error) error {
		if err != nil || name == template {
			return err
		}
		relative, err := filepath.Rel(template, name)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			directories = append(directories, relative)
			return nil
		}
		files[relative], err = os.ReadFile(name)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	cost := operationstore.FirstPassEntries(plan)
	if held := len(directories) + len(files); held != cost || cost != 13 {
		t.Fatalf("a one-block operation that ran its block holds %d entries, and FirstPassEntries counts %d, want 13: %v %v", held, cost, directories, slices.Sorted(maps.Keys(files)))
	}
	retained := (operationstore.MaxEntries - operationstore.AdmissionEntries(plan) - 1) / cost
	for index := 1; index < retained; index++ {
		copied := filepath.Join(area, identity(index))
		for _, directory := range directories {
			if err := os.Mkdir(filepath.Join(copied, directory), 0700); err != nil {
				t.Fatal(err)
			}
		}
		for relative, data := range files {
			if err := os.WriteFile(filepath.Join(copied, relative), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	last, wide, beyond, removed := identity(retained), identity(retained+1), identity(retained+2), identity(retained+3)
	held := func() int {
		count := 0
		if err := filepath.WalkDir(area, func(name string, _ fs.DirEntry, err error) error {
			if name != area {
				count++
			}
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return count
	}
	refusedAtTheBound := func(operations *operationstore.Store, id string, plan reconciliation.Plan) {
		refused := diagnostics.Of(operations.Claim(ctx, id, plan))
		if len(refused) != 1 || refused[0].Code != "lifecycle.state" || !strings.Contains(refused[0].Message, "retained the maximum number of lifecycle operations") {
			t.Fatalf("a claim for %d blocks beyond the retained-operation bound reported %+v, want its lifecycle.state refusal", len(plan.Blocks), refused)
		}
		if _, err := os.Stat(filepath.Join(area, id)); !os.IsNotExist(err) {
			t.Fatalf("the refused claim left its directory (%v)", err)
		}
	}
	err = store.MutateLifecycle(ctx, "example", func(tx lifecycle.Transaction) error {
		refusedAtTheBound(operationstore.New(tx.Operations(), clock), wide, wider)
		operations, err := run(tx, last, false)
		if err != nil {
			t.Fatalf("the last operation the area admits did not run its block beside %d others: %s", retained, causeText(err))
		}
		if err := attempt(operations, last, true); err != nil {
			t.Fatalf("the last operation the area admits could not run a later attempt: %s", causeText(err))
		}
		refusedAtTheBound(operations, beyond, plan)
		padding := operationstore.MaxEntries - held() - operationstore.AdmissionEntries(removal) - 1
		if padding < 0 {
			t.Fatalf("the removal of the last apply the area admits needs %d entries beside the %d it holds", operationstore.AdmissionEntries(removal), held())
		}
		if err := os.Mkdir(filepath.Join(area, "padding"), 0700); err != nil {
			t.Fatal(err)
		}
		for index := range padding {
			if err := os.WriteFile(filepath.Join(area, "padding", fmt.Sprintf("record-%d", index)), []byte("{}\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}
		operation, err := register(operations, removed, last, removal)
		if err != nil {
			t.Fatalf("the removal of the last apply the area admits did not register: %s", causeText(err))
		}
		if err := attempt(operations, removed, true); err != nil {
			t.Fatalf("the removal of the last apply the area admits could not run its block: %s", causeText(err))
		}
		operation.State = reconciliation.OperationDone
		if err := operations.UpdateOperation(ctx, operation); err != nil {
			t.Fatalf("the removal of the last apply the area admits could not complete: %s", causeText(err))
		}
		if free := operationstore.MaxEntries - held(); free != 1 {
			t.Fatalf("the completed removal left %d entries free, want the one it was admitted with", free)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
