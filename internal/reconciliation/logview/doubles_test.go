package logview

import (
	"context"
	"encoding/json"
	"errors"
	"path"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore/areacontract"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore/areadouble"
)

type memoryArea struct {
	mutex       *sync.Mutex
	files       map[string][]byte
	directories map[string]bool
	reference   string
}

func newArea(mutex *sync.Mutex, reference string) *memoryArea {
	return &memoryArea{mutex: mutex, files: map[string][]byte{}, directories: map[string]bool{"": true}, reference: reference}
}

func TestMemoryAreaHonoursTheAreaContract(t *testing.T) {
	areacontract.Verify(t, func(t *testing.T, use func(operationstore.Area)) {
		use(newArea(&sync.Mutex{}, ""))
	})
}

func (a *memoryArea) Read(ctx context.Context, target string, maximum int) ([]byte, bool, error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := areadouble.Admit(ctx, a.files, a.directories, target, true); err != nil {
		return nil, false, err
	}
	data, ok := a.files[target]
	if ok && maximum > 0 && len(data) > maximum {
		return nil, false, errors.New("bounds")
	}
	return slices.Clone(data), ok, nil
}

func (a *memoryArea) Entries(ctx context.Context, target string) ([]operationstore.Entry, error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := areadouble.Admit(ctx, a.files, a.directories, target, false); err != nil {
		return nil, err
	}
	return areadouble.Entries[operationstore.Entry](a.files, a.directories, target), nil
}

func (a *memoryArea) EnsureDirectory(ctx context.Context, target string) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := areadouble.Admit(ctx, a.files, a.directories, target, false); err != nil {
		return err
	}
	for current := target; current != "." && current != ""; current = path.Dir(current) {
		a.directories[current] = true
	}
	return nil
}

func (a *memoryArea) WriteExclusive(ctx context.Context, target string, data []byte) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := areadouble.Admit(ctx, a.files, a.directories, target, true); err != nil {
		return err
	}
	if _, exists := a.files[target]; exists {
		return errors.New("exists")
	}
	a.files[target] = slices.Clone(data)
	return nil
}

func (a *memoryArea) Replace(ctx context.Context, target string, data, expected []byte) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := areadouble.Admit(ctx, a.files, a.directories, target, true); err != nil {
		return err
	}
	current, exists := a.files[target]
	if expected == nil && exists || expected != nil && (!exists || !slices.Equal(current, expected)) {
		return errors.New("expectation")
	}
	a.files[target] = slices.Clone(data)
	return nil
}

func (a *memoryArea) Append(ctx context.Context, target string, data []byte) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := areadouble.Admit(ctx, a.files, a.directories, target, true); err != nil {
		return err
	}
	a.files[target] = append(a.files[target], data...)
	return nil
}

func (a *memoryArea) RemoveDirectory(ctx context.Context, target string) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := areadouble.Admit(ctx, a.files, a.directories, target, false); err != nil {
		return err
	}
	if target == "" {
		return errors.New("the area itself is not removable")
	}
	for name := range a.directories {
		if strings.HasPrefix(name, target+"/") {
			return errors.New("directory not empty")
		}
	}
	for name := range a.files {
		if strings.HasPrefix(name, target+"/") {
			return errors.New("directory not empty")
		}
	}
	delete(a.directories, target)
	return nil
}

func (a *memoryArea) RemoveRecord(ctx context.Context, target string, expected []byte) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := areadouble.Admit(ctx, a.files, a.directories, target, true); err != nil {
		return err
	}
	current, exists := a.files[target]
	if !exists {
		return nil
	}
	if !slices.Equal(current, expected) {
		return errors.New("the record changed before its removal")
	}
	delete(a.files, target)
	for parent := path.Dir(target); parent != "."; parent = path.Dir(parent) {
		a.directories[parent] = true
	}
	return nil
}

func (a *memoryArea) Sync(ctx context.Context, target string) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return areadouble.Admit(ctx, a.files, a.directories, target, false)
}

func (a *memoryArea) Location() string { return "" }

func (a *memoryArea) Reference() string { return a.reference }

type workspace struct {
	mutex      sync.Mutex
	records    *memoryArea
	logs       *memoryArea
	locked     bool
	busy       int
	mutating   bool
	polls      int
	beforeRead func(poll int)
	lockedRead int
	unlocked   int
	runs       map[string]*stream
	setups     []*stream
}

func newWorkspace() *workspace {
	ws := &workspace{runs: map[string]*stream{}}
	ws.records = newArea(&ws.mutex, "")
	ws.logs = newArea(&ws.mutex, "contexts/lab/operations")
	return ws
}

func (w *workspace) ReadLogs(ctx context.Context, name string, callback func(View) error) error {
	w.mutex.Lock()
	w.polls++
	poll, hook := w.polls, w.beforeRead
	w.mutex.Unlock()
	if hook != nil {
		hook(poll)
	}
	w.mutex.Lock()
	if w.busy > 0 {
		w.busy--
		w.mutex.Unlock()
		return diagnostics.NewFailureWithRemediation("lifecycle.lease", "context storage is busy", "", "retry")
	}
	w.locked = true
	w.mutex.Unlock()
	defer func() {
		w.mutex.Lock()
		w.locked = false
		w.mutex.Unlock()
	}()
	if name != "lab" {
		return diagnostics.NewFailure("context.state", "context "+name+" does not exist", "")
	}
	return callback(view{w})
}

func (w *workspace) OpenRun(ctx context.Context, name, run string) (Stream, bool, error) {
	found, ok := w.runs[run]
	return found, ok, nil
}

func (w *workspace) OpenSetup(ctx context.Context, setup string) (Stream, bool, error) {
	for index := len(w.setups) - 1; index >= 0; index-- {
		if setup == "" || w.setups[index].name == setup {
			return w.setups[index], true, nil
		}
	}
	return nil, false, nil
}

type view struct{ w *workspace }

func (v view) Records() operationstore.Area { return v.w.records }

func (v view) Logs() operationstore.Area { return v.w.logs }

func (v view) Open(ctx context.Context, target string) (LogFile, bool, error) {
	v.w.mutex.Lock()
	defer v.w.mutex.Unlock()
	data, found := v.w.logs.files[target]
	if !found {
		return nil, false, nil
	}
	return &file{w: v.w, target: target, kept: slices.Clone(data)}, true, nil
}

func (v view) Mutating(ctx context.Context) (bool, error) {
	v.w.mutex.Lock()
	defer v.w.mutex.Unlock()
	return v.w.mutating, nil
}

type file struct {
	w      *workspace
	target string
	kept   []byte
	closed bool
}

func (f *file) content() []byte {
	if data, found := f.w.logs.files[f.target]; found {
		f.kept = slices.Clone(data)
	}
	return f.kept
}

func (f *file) ReadRange(offset int64, maximum int) ([]byte, error) {
	f.w.mutex.Lock()
	defer f.w.mutex.Unlock()
	if f.w.locked {
		f.w.lockedRead++
	} else {
		f.w.unlocked++
	}
	return window(f.content(), offset, maximum), nil
}

func window(content []byte, offset int64, maximum int) []byte {
	if offset >= int64(len(content)) {
		return nil
	}
	return slices.Clone(content[offset:min(int64(len(content)), offset+int64(maximum))])
}

func (f *file) Size() (int64, error) {
	f.w.mutex.Lock()
	defer f.w.mutex.Unlock()
	return int64(len(f.content())), nil
}

func (f *file) Close() error {
	f.closed = true
	return nil
}

type stream struct {
	mutex   sync.Mutex
	name    string
	data    []byte
	held    bool
	probes  int
	closeAt int
}

func (s *stream) ReadRange(offset int64, maximum int) ([]byte, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return window(s.data, offset, maximum), nil
}

func (s *stream) Size() (int64, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return int64(len(s.data)), nil
}

func (s *stream) Close() error { return nil }

func (s *stream) Name() string { return s.name }

func (s *stream) Reference() string {
	if strings.HasPrefix(s.name, "setup-") {
		return "host/" + s.name + ".output"
	}
	return "contexts/lab/runs/" + s.name + ".output"
}

func (s *stream) Held(context.Context) (bool, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.probes++
	if s.probes >= s.closeAt {
		s.held = false
	}
	return s.held, nil
}

func (s *stream) write(text string) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.data = append(s.data, text...)
}

const operationID = "op-0123456789abcdef0123456789abcdef"

var build = operationstore.Build{
	AutomationDigest: strings.Repeat("2", 64), ContentDigest: strings.Repeat("a", 64),
	Executable: operationstore.Executable{Version: "devel", Commit: "abcdef1"},
}

type fixture struct {
	t     *testing.T
	ctx   context.Context
	ws    *workspace
	store *operationstore.Store
	plan  reconciliation.Plan
}

func newFixture(t *testing.T, blocks ...string) *fixture {
	t.Helper()
	ws := newWorkspace()
	ws.mutating = true
	clock := func() time.Time { return time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC) }
	f := &fixture{t: t, ctx: context.Background(), ws: ws, store: operationstore.New(ws.records, ws.logs, clock)}
	definitions := make([]reconciliation.BlockDefinition, 0, len(blocks))
	for _, id := range blocks {
		definitions = append(definitions, reconciliation.BlockDefinition{
			ID: id, Description: "serve " + id, Stage: reconciliation.StageInfrastructureServices,
			Kind: "ArtifactServer", Object: id, Implementation: "artifact-server-nginx-v1", ContentDigest: strings.Repeat("a", 64),
			Request: json.RawMessage(`{"name":"` + id + `"}`),
		})
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := plan.Digest()
	if err != nil {
		t.Fatal(err)
	}
	f.plan = plan
	operation := operationstore.Operation{
		Version: operationstore.OperationVersion, ID: operationID, Verb: reconciliation.Apply,
		Context: "lab", Revision: "rev-" + strings.Repeat("ef", 16),
		InputDigest: strings.Repeat("1", 64), PlanDigest: digest, AutomationDigest: strings.Repeat("2", 64),
		Executable: operationstore.Executable{Version: "devel", Commit: "abcdef1"},
		Closure:    &operationstore.Closure{Digest: strings.Repeat("3", 64), Python: "3.14", Ansible: "2.21.4"},
		Bindings:   []string{}, State: reconciliation.OperationRunning,
		Created: "2026-11-08T09:00:00Z", Updated: "2026-11-08T09:00:00Z",
	}
	if _, err := f.store.Index(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.store.Register(f.ctx, operation, plan); err != nil {
		t.Fatal(err)
	}
	return f
}

func stores(records, logs operationstore.Area) OperationStore {
	return operationstore.New(records, logs, nil).Running(build.Executable)
}

func (f *fixture) service(pause Pause) Service {
	return New(f.ws, Options{Operations: stores, Pause: pause})
}

func record(clock, event, block, detail string) string {
	line, _ := json.Marshal(operationstore.LogRecord{Time: "2026-11-08T" + clock + "Z", Event: event, Block: block, Detail: detail})
	return string(line)
}

func (f *fixture) log(target string, lines ...string) {
	f.t.Helper()
	if err := f.ws.logs.Append(f.ctx, path.Join(operationID, target), []byte(strings.Join(lines, "\n")+"\n")); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) write(target, text string) {
	f.t.Helper()
	if err := f.ws.logs.Append(f.ctx, path.Join(operationID, target), []byte(text)); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) start(block string) int {
	f.t.Helper()
	number, err := f.store.StartAttempt(f.ctx, operationID, block, build)
	if err != nil {
		f.t.Fatal(err)
	}
	return number
}

func (f *fixture) complete(block string, attempt int, state reconciliation.BlockState) {
	f.t.Helper()
	if err := f.store.CompleteAttempt(f.ctx, operationID, block, attempt, reconciliation.OutcomeChanged, reconciliation.EffectCompleted, state, json.RawMessage(`{}`)); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) settle(state reconciliation.OperationState) {
	f.t.Helper()
	operation, err := f.store.ReadOperation(f.ctx, operationID)
	if err != nil {
		f.t.Fatal(err)
	}
	operation.State = state
	if err := f.store.UpdateOperation(f.ctx, operation); err != nil {
		f.t.Fatal(err)
	}
}

type recorder struct{ lines []string }

func (r *recorder) Event(line Line) error {
	r.lines = append(r.lines, "event "+line.Text)
	return nil
}

func (r *recorder) Output(line Line) error {
	r.lines = append(r.lines, "["+line.Source+"] "+line.Text)
	return nil
}

func scripted(steps ...func()) (Pause, *int) {
	pauses := 0
	return func(ctx context.Context, interval time.Duration) error {
		if interval != PollInterval {
			return errors.New("the follow polled at another interval")
		}
		if pauses >= len(steps) {
			return errors.New("the follow did not stop when it should have")
		}
		pauses++
		steps[pauses-1]()
		return nil
	}, &pauses
}

func codes(err error) []string {
	var found []string
	for _, reported := range diagnostics.Of(err) {
		found = append(found, reported.Code)
	}
	return found
}
