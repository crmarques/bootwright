package lifecycle

import (
	"context"
	"errors"
	"path"
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

const liveRoot = "contexts/lab/operations/"

func retainOutput(t *testing.T, h *harness, operation, block, name, content string) {
	t.Helper()
	area := h.workspace.logArea
	area.mutex.Lock()
	defer area.mutex.Unlock()
	area.files[path.Join(operation, "blocks", block, name)] = []byte(content)
}

func appliedWithRunning(t *testing.T, running ...string) (*harness, string) {
	t.Helper()
	h := newHarness(t, "alpha", "bravo", "charlie")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	operation := currentOperation(t, h)
	for _, block := range running {
		leaveRunning(t, h, block)
	}
	return h, operation
}

// The records say which blocks are running and the log area says what each one
// has retained, so status names, in plan order, the newest output of the
// attempt each running block is on: the highest resolution, relative to the log
// root, a block whose attempt retained nothing yet naming the file it will
// write, and a block that is not running or an attempt it is not on never.
func TestStatusListsEachRunningBlocksNewestOutputFromRecords(t *testing.T) {
	h, operation := appliedWithRunning(t)
	idle, err := h.service.Status(context.Background(), StatusRequest{ContextName: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	if idle.Lifecycle.Live == nil || len(idle.Lifecycle.Live) != 0 {
		t.Fatalf("a context with nothing running reports %#v, want an empty list", idle.Lifecycle.Live)
	}
	h, operation = appliedWithRunning(t, "charlie", "alpha")
	retainOutput(t, h, operation, "alpha", "attempt-000001.output", "first")
	retainOutput(t, h, operation, "alpha", "attempt-000001-resolution-000001.output", "second")
	retainOutput(t, h, operation, "alpha", "attempt-000001-resolution-000002.output", "the newest")
	retainOutput(t, h, operation, "alpha", "attempt-000002.output", "an attempt the record is not on")
	retainOutput(t, h, operation, "bravo", "attempt-000001.output", "bravo is done")
	h.workspace.logArea.mutex.Lock()
	delete(h.workspace.logArea.files, path.Join(operation, "blocks", "charlie", "attempt-000001.output"))
	h.workspace.logArea.mutex.Unlock()
	status, err := h.service.Status(context.Background(), StatusRequest{ContextName: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	want := []LiveBlock{
		{Block: "alpha", Attempt: 1, Resolution: 2, Output: liveRoot + operation + "/blocks/alpha/attempt-000001-resolution-000002.output", Present: true, Bytes: int64(len("the newest"))},
		{Block: "charlie", Attempt: 1, Output: liveRoot + operation + "/blocks/charlie/attempt-000001.output"},
	}
	if !slices.Equal(status.Lifecycle.Live, want) {
		t.Fatalf("live = %+v, want %+v", status.Lifecycle.Live, want)
	}
}

// Status names what the records and the log area hold and proves nothing about
// any process: listing a running block reads, writes and runs nothing.
func TestStatusLiveReadsNoHost(t *testing.T) {
	h, operation := appliedWithRunning(t, "alpha")
	retainOutput(t, h, operation, "alpha", "attempt-000001.output", "partial")
	before := countWrites(h)
	status, err := h.service.Status(context.Background(), StatusRequest{ContextName: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Lifecycle.Live) != 1 || status.Lifecycle.Live[0].Block != "alpha" {
		t.Fatalf("live = %+v", status.Lifecycle.Live)
	}
	assertNothingWritten(t, h, before)
}

type pathsSource struct {
	asked []string
	paths []ContextPath
	err   error
}

func (p *pathsSource) Paths(_ context.Context, name string) ([]ContextPath, error) {
	p.asked = append(p.asked, name)
	return slices.Clone(p.paths), p.err
}

// The places a context keeps its content are asked of the host only when the
// request asks for them, in the order the port gave them, and a build wired
// with no source for them refuses rather than answering an empty list.
func TestStatusPathsOnlyWhenAsked(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "alpha")
	source := &pathsSource{paths: []ContextPath{
		{Kind: "store", Path: "/var/lib/bootwright/contexts/lab", Present: true},
		{Kind: "logs", Path: "/var/log/bootwright/contexts/lab", Present: true},
		{Kind: "hosted", Path: "/var/lib/bootwright-hosted/lab"},
		{Kind: "libvirt-images", Path: "/var/lib/libvirt/images/bootwright/lab"},
	}}
	h.service.options.Paths = source
	plain, err := h.service.Status(ctx, StatusRequest{ContextName: "lab"})
	if err != nil || plain.Paths != nil || len(source.asked) != 0 {
		t.Fatalf("a plain status carried %+v and asked %q (%v)", plain.Paths, source.asked, err)
	}
	asked, err := h.service.Status(ctx, StatusRequest{ContextName: "lab", Paths: true})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(asked.Paths, source.paths) || !slices.Equal(source.asked, []string{"lab"}) {
		t.Fatalf("paths = %+v after asking %q", asked.Paths, source.asked)
	}
	source.err = errors.New("the host answered no")
	if _, err := h.service.Status(ctx, StatusRequest{ContextName: "lab", Paths: true}); !errors.Is(err, source.err) {
		t.Fatalf("a failing source reported %v", err)
	}
	h.service.options.Paths = nil
	_, err = h.service.Status(ctx, StatusRequest{ContextName: "lab", Paths: true})
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "runtime.internal" {
		t.Fatalf("a status with no paths source reported %+v (%v), want an internal failure", reported, err)
	}
	if _, err := h.service.Status(ctx, StatusRequest{ContextName: "lab"}); err != nil {
		t.Fatalf("a plain status needed the paths source: %v", err)
	}
}

// An operation names where it writes as the path, the context and the
// operation, and no run: it is told once, before its first effect.
func TestProgressNamesTheOperationContextAndRun(t *testing.T) {
	h := newHarness(t, "alpha")
	progress := &testProgress{rowsAtLocation: -1}
	h.service.options.Progress = progress
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	want := LogLocation{Path: "/var/log/bootwright/contexts/lab/operations/" + firstOperation, Context: "lab", Operation: firstOperation}
	if progress.named != want {
		t.Fatalf("named %+v, want %+v", progress.named, want)
	}
	if got := currentOperation(t, h); got != firstOperation {
		t.Fatalf("the current operation is %q, not the named %q", got, firstOperation)
	}
}
