package lifecycle

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore/areadouble"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

const (
	testContextName = "lab"
	testRevision    = "rev-0123456789abcdef0123456789abcdef"
	testAutomaton   = "aaaa0123456789abcdef0123456789abcdef0123456789abcdef0123456789ab"
)

// memoryArea is an in-memory operation area that areacontract.Verify holds to
// the same clauses as contextfs's area (TestJourneyAreaHonoursTheAreaContract).
// The real area serializes concurrent blocks through the filesystem, so this
// one holds a mutex: without it the race detector reports the fake rather than
// the engine. It refuses a cancelled context, as the contract requires, so a
// test can prove which records an interrupted invocation still writes. It
// keeps the directories it was asked to create, because a claimed operation
// directory holds no file.
type memoryArea struct {
	mutex       sync.Mutex
	files       map[string][]byte
	directories map[string]bool
	fail        map[string]error
	// failAfter fails a write once, after its bytes landed, as a rename that
	// succeeded before its read-back or sync failed does, and a directory's
	// creation once it exists, as one whose parent sync failed does.
	failAfter map[string]error
	// landing, when set, runs as each write is about to land: after the area
	// is held and before anything changes, with the live files. It may copy
	// them but must not take the area again, and an error it returns fails
	// that write, so a test can stop an invocation exactly at a durable write.
	landing func(operation, target string, files map[string][]byte) error
	// bound, when set, refuses a write that would take the bytes the area's
	// records hold past it, counting a replaced record's current bytes as the
	// real area does until its replacement lands.
	bound int
}

// held is the bytes the area's records hold.
func (a *memoryArea) held() int {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	return a.stored()
}

func (a *memoryArea) stored() int {
	total := 0
	for _, data := range a.files {
		total += len(data)
	}
	return total
}

func (a *memoryArea) fits(data []byte) error {
	if a.bound > 0 && a.stored()+len(data) > a.bound {
		return errors.New("the operation area has reached its bytes")
	}
	return nil
}

func newArea() *memoryArea {
	return &memoryArea{files: map[string][]byte{}, directories: map[string]bool{}, fail: map[string]error{}, failAfter: map[string]error{}}
}

// written reports whether this area holds anything under one identity, so a
// test can prove a bounded run left the operation records alone.
func (a *memoryArea) written(identity string) bool {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	for name := range a.files {
		if strings.HasPrefix(name, identity+"/") {
			return true
		}
	}
	return false
}

func (a *memoryArea) clone() *memoryArea {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	copied := newArea()
	for name, data := range a.files {
		copied.files[name] = slices.Clone(data)
	}
	maps.Copy(copied.directories, a.directories)
	return copied
}

// landed fails a write whose bytes just landed, once, when a test asked for it.
func (a *memoryArea) landed(target string) error {
	err := a.failAfter[target]
	delete(a.failAfter, target)
	return err
}

func (a *memoryArea) admit(ctx context.Context, target string, record bool) error {
	return areadouble.Admit(ctx, a.files, a.directories, target, record)
}

func (a *memoryArea) Read(ctx context.Context, target string, maximum int) ([]byte, bool, error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := a.admit(ctx, target, true); err != nil {
		return nil, false, err
	}
	if err := a.fail["read "+target]; err != nil {
		return nil, false, err
	}
	data, ok := a.files[target]
	if ok && maximum > 0 && len(data) > maximum {
		return nil, false, errors.New("record exceeds its maximum")
	}
	return slices.Clone(data), ok, nil
}

func (a *memoryArea) Entries(ctx context.Context, target string) ([]operationstore.Entry, error) {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := a.admit(ctx, target, false); err != nil {
		return nil, err
	}
	return areadouble.Entries[operationstore.Entry](a.files, a.directories, target), nil
}

func (a *memoryArea) EnsureDirectory(ctx context.Context, target string) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := a.admit(ctx, target, false); err != nil {
		return err
	}
	for current := target; current != "." && current != ""; current = path.Dir(current) {
		a.directories[current] = true
	}
	return a.landed("ensure " + target)
}

func (a *memoryArea) WriteExclusive(ctx context.Context, target string, data []byte) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := a.admit(ctx, target, true); err != nil {
		return err
	}
	if a.landing != nil {
		if err := a.landing("write", target, a.files); err != nil {
			return err
		}
	}
	if err := a.fail["write "+target]; err != nil {
		return err
	}
	if _, exists := a.files[target]; exists {
		return errors.New("exists")
	}
	if err := a.fits(data); err != nil {
		return err
	}
	a.files[target] = slices.Clone(data)
	return a.landed("write " + target)
}

func (a *memoryArea) Replace(ctx context.Context, target string, data, expected []byte) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := a.admit(ctx, target, true); err != nil {
		return err
	}
	if a.landing != nil {
		if err := a.landing("replace", target, a.files); err != nil {
			return err
		}
	}
	if err := a.fail["replace "+target]; err != nil {
		return err
	}
	current, exists := a.files[target]
	if expected == nil {
		if exists {
			return errors.New("exists")
		}
	} else if !exists || !slices.Equal(current, expected) {
		return errors.New("expectation")
	}
	if err := a.fits(data); err != nil {
		return err
	}
	a.files[target] = slices.Clone(data)
	return a.landed("replace " + target)
}

func (a *memoryArea) Append(ctx context.Context, target string, data []byte) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := a.admit(ctx, target, true); err != nil {
		return err
	}
	if a.landing != nil {
		if err := a.landing("append", target, a.files); err != nil {
			return err
		}
	}
	if err := a.fail["append "+target]; err != nil {
		return err
	}
	if err := a.fits(data); err != nil {
		return err
	}
	a.files[target] = append(a.files[target], data...)
	return nil
}

// RemoveDirectory removes only an empty directory, as the kernel does, and
// never the area itself. It lands like a write, so a test can stop an
// invocation at a removal.
func (a *memoryArea) RemoveDirectory(ctx context.Context, target string) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := a.admit(ctx, target, false); err != nil {
		return err
	}
	if target == "" {
		return errors.New("the area itself is not removable")
	}
	if a.landing != nil {
		if err := a.landing("remove", target, a.files); err != nil {
			return err
		}
	}
	if err := a.fail["remove "+target]; err != nil {
		return err
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

// RemoveRecord removes a record only while it holds exactly expected, and
// keeps the directories above it, which the real area leaves in place. It
// lands like a write.
func (a *memoryArea) RemoveRecord(ctx context.Context, target string, expected []byte) error {
	a.mutex.Lock()
	defer a.mutex.Unlock()
	if err := a.admit(ctx, target, true); err != nil {
		return err
	}
	if a.landing != nil {
		if err := a.landing("unlink", target, a.files); err != nil {
			return err
		}
	}
	if err := a.fail["unlink "+target]; err != nil {
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
	return a.admit(ctx, target, false)
}

func (a *memoryArea) Location() string { return "/var/lib/bootwright/contexts/lab/state/operations" }

func (a *memoryArea) Reference() string { return "contexts/lab/state/operations" }

// testWorkspace is one context's durable state: its evidence, reservations and
// operation records, with the same read and mutate boundaries the store has.
type testWorkspace struct {
	area         *memoryArea
	runArea      *memoryArea
	evidence     []byte
	reservations []prerequisites.HostReservation
	controller   prerequisites.StorageView
	inputs       desiredstate.Sources
	mutations    int
	runs         int
	binds        int
	areas        map[string]bool
	retained     []prerequisites.DependencySource
	resolutions  int
	failPublish  error
	// landThenFail fails the next evidence publication once, after its bytes
	// landed, as a publication whose read-back or sync failed does.
	landThenFail error
	// opened counts how many times this operation asked for the approved
	// execution bundle, which an operation does once however many blocks run.
	opened int
	// beforeMutation runs as the exclusive lock is taken, so a test can advance
	// durable state exactly between a decision and the effects it authorized.
	beforeMutation func()
	// revision names the input the context holds now. A test that publishes
	// another revision replaces it together with inputs, as an import does.
	revision string
	// kill runs first in every durable publication this workspace performs
	// outside its operation area, named by the point it would publish, so a
	// test can stop an invocation there; an error it returns fails it.
	kill func(point string) error
	// transacting is true while a MutateLifecycle callback runs, the only time
	// a secret area may be lent.
	transacting bool
	// clientFiles is what the client areas hold, by area and path; areas
	// names each client area opened, true once it is sealed.
	clientFiles map[string][]byte
}

func (w *testWorkspace) view() *testView {
	return &testView{workspace: w}
}

// held is the view one callback holds. Its operation area writes only in a
// transaction and its run area only in a bounded run, and both refuse every
// call once the callback returns, as the store's areas do.
func (w *testWorkspace) held(operations, runs bool) (*testView, func()) {
	closed := &atomic.Bool{}
	view := &testView{
		workspace:  w,
		operations: &heldArea{memoryArea: w.area, writable: operations, closed: closed, reference: "contexts/lab/state/operations"},
		runs:       &heldArea{memoryArea: w.runArea, writable: runs, closed: closed, reference: "contexts/lab/state/runs"},
		closed:     closed,
	}
	return view, func() { closed.Store(true) }
}

// admit refuses, as the store does before it reads anything, a context this
// workspace does not hold and a missing callback.
func (w *testWorkspace) admit(name string, called bool) error {
	if name == "" {
		return errors.New("explicit context required")
	}
	if name != testContextName {
		return errors.New("the selected context does not exist")
	}
	if !called {
		return errors.New("lifecycle callback is missing")
	}
	return nil
}

func (w *testWorkspace) ReadLifecycle(ctx context.Context, name string, callback func(View) error) error {
	if err := w.admit(name, callback != nil); err != nil {
		return err
	}
	view, release := w.held(false, false)
	defer release()
	return callback(view)
}

func (w *testWorkspace) RunLifecycle(ctx context.Context, name string, callback func(RunView) error) error {
	if err := w.admit(name, callback != nil); err != nil {
		return err
	}
	w.runs++
	view, release := w.held(false, true)
	defer release()
	return callback(view)
}

func (w *testWorkspace) MutateLifecycle(ctx context.Context, name string, callback func(Transaction) error) error {
	if err := w.admit(name, callback != nil); err != nil {
		return err
	}
	// The real store takes the exclusive lock here, so this is the moment
	// another invocation's committed work becomes visible to a decision that
	// was taken under the shared one.
	if w.beforeMutation != nil {
		w.beforeMutation()
	}
	w.mutations++
	w.transacting = true
	defer func() { w.transacting = false }()
	view, release := w.held(true, false)
	defer release()
	return callback(view)
}

// heldArea is one callback's hold on an area: it refuses every call once that
// callback returned, and every write while it is read-only.
type heldArea struct {
	*memoryArea
	writable bool
	closed   *atomic.Bool
	// reference tells the two areas apart, which the shared double cannot.
	reference string
}

func (a *heldArea) Reference() string { return a.reference }

func (a *heldArea) usable(ctx context.Context, write bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.closed.Load() {
		return errors.New("the lifecycle operation capability has closed")
	}
	if write && !a.writable {
		return errors.New("read-only lifecycle access refuses mutation")
	}
	return nil
}

func (a *heldArea) Read(ctx context.Context, target string, maximum int) ([]byte, bool, error) {
	if err := a.usable(ctx, false); err != nil {
		return nil, false, err
	}
	return a.memoryArea.Read(ctx, target, maximum)
}

func (a *heldArea) Entries(ctx context.Context, target string) ([]operationstore.Entry, error) {
	if err := a.usable(ctx, false); err != nil {
		return nil, err
	}
	return a.memoryArea.Entries(ctx, target)
}

func (a *heldArea) EnsureDirectory(ctx context.Context, target string) error {
	if err := a.usable(ctx, true); err != nil {
		return err
	}
	return a.memoryArea.EnsureDirectory(ctx, target)
}

func (a *heldArea) WriteExclusive(ctx context.Context, target string, data []byte) error {
	if err := a.usable(ctx, true); err != nil {
		return err
	}
	return a.memoryArea.WriteExclusive(ctx, target, data)
}

func (a *heldArea) Replace(ctx context.Context, target string, data, expected []byte) error {
	if err := a.usable(ctx, true); err != nil {
		return err
	}
	return a.memoryArea.Replace(ctx, target, data, expected)
}

func (a *heldArea) Append(ctx context.Context, target string, data []byte) error {
	if err := a.usable(ctx, true); err != nil {
		return err
	}
	return a.memoryArea.Append(ctx, target, data)
}

func (a *heldArea) Sync(ctx context.Context, target string) error {
	if err := a.usable(ctx, true); err != nil {
		return err
	}
	return a.memoryArea.Sync(ctx, target)
}

func (a *heldArea) RemoveDirectory(ctx context.Context, target string) error {
	if err := a.usable(ctx, true); err != nil {
		return err
	}
	return a.memoryArea.RemoveDirectory(ctx, target)
}

func (a *heldArea) RemoveRecord(ctx context.Context, target string, expected []byte) error {
	if err := a.usable(ctx, true); err != nil {
		return err
	}
	return a.memoryArea.RemoveRecord(ctx, target, expected)
}

// testView is a view of the workspace. One a callback holds carries the areas
// it holds; one a test builds to inspect state reaches the areas directly.
type testView struct {
	workspace        *testWorkspace
	operations, runs operationstore.Area
	closed           *atomic.Bool
}

func (v *testView) Identity() ContextIdentity {
	return ContextIdentity{Name: testContextName, Revision: v.workspace.revision, Mode: "ready"}
}
func (v *testView) Inputs() desiredstate.Sources { return v.workspace.inputs }

// Controller mirrors the reservations this workspace holds into the controller
// record, as the store's controller record carries them.
func (v *testView) Controller() prerequisites.StorageView {
	view := v.workspace.controller
	view.State.Reservations = slices.Clone(v.workspace.reservations)
	return view
}
func (v *testView) Evidence() []byte { return slices.Clone(v.workspace.evidence) }

func (v *testView) Operations() operationstore.Area {
	if v.operations != nil {
		return v.operations
	}
	return v.workspace.area
}

func (v *testView) Runs() operationstore.Area {
	if v.runs != nil {
		return v.runs
	}
	return v.workspace.runArea
}

func (v *testView) PublishEvidence(_ context.Context, data []byte) error {
	if len(data) == 0 {
		return errors.New("context mutation evidence exceeds its bounds")
	}
	if err := v.killed("publish evidence"); err != nil {
		return err
	}
	if v.workspace.failPublish != nil {
		return v.workspace.failPublish
	}
	v.workspace.evidence = slices.Clone(data)
	failed := v.workspace.landThenFail
	v.workspace.landThenFail = nil
	return failed
}

// Bind records the relationship the way the store does: a first apply
// establishes it, a later one revalidates exactly it, and a different Machine
// or host refuses instead of replacing it. It names a Machine, and binds only
// the host the controller record identifies.
func (v *testView) Bind(_ context.Context, machine string, host controller.InstalledHostIdentity) error {
	if machine == "" {
		return errors.New("a controller binding requires the selected Machine name")
	}
	if !v.workspace.controller.State.Host.Equal(host) {
		return failure("controller.identity", "this host is not the host this controller state belongs to", "")
	}
	if err := v.killed("publish binding"); err != nil {
		return err
	}
	digest, err := host.PrivateDigest()
	if err != nil {
		return err
	}
	v.workspace.binds++
	for _, binding := range v.workspace.controller.State.Bindings {
		if binding.Context != v.Identity().Name {
			continue
		}
		if binding.Machine != machine || binding.HostDigest != digest {
			return failure("controller.identity", "this context is already bound to another controller Machine or host", "")
		}
		return nil
	}
	v.workspace.controller.State.Bindings = append(v.workspace.controller.State.Bindings,
		prerequisites.ControllerBinding{Context: v.Identity().Name, Machine: machine, HostDigest: digest})
	return nil
}

func (v *testView) Reserve(_ context.Context, reservations []prerequisites.HostReservation) error {
	for _, reservation := range reservations {
		if reservation.Context != v.Identity().Name {
			return errors.New("a lifecycle reservation must belong to its own context")
		}
	}
	if err := v.killed("publish reservations"); err != nil {
		return err
	}
	v.workspace.reservations = slices.Clone(reservations)
	return nil
}

func (v *testView) ReleaseReservations(context.Context) error {
	if err := v.killed("release reservations"); err != nil {
		return err
	}
	v.workspace.reservations = nil
	return nil
}

// killed asks the workspace's kill hook whether this publication may land.
func (v *testView) killed(point string) error {
	if v.workspace.kill == nil {
		return nil
	}
	return v.workspace.kill(point)
}

// ClientArea models the shared host area the controller stage publishes into:
// only a closure's digest names one and never the approved setup bundle, the
// reservation is recorded before the area exists, and a sealed closure reopens
// read-only.
func (v *testView) ClientArea(_ context.Context, id string) (prerequisites.BundleArea, error) {
	if decoded, err := hex.DecodeString(id); err != nil || len(decoded) != 32 {
		return nil, errors.New("controller client area identity is invalid")
	}
	if id == v.workspace.controller.State.Receipt.CatalogDigest {
		return nil, errors.New("the approved setup bundle is not a client publication area")
	}
	if v.workspace.areas == nil {
		v.workspace.areas = map[string]bool{}
	}
	if v.workspace.clientFiles == nil {
		v.workspace.clientFiles = map[string][]byte{}
	}
	if _, exists := v.workspace.areas[id]; !exists {
		v.workspace.areas[id] = false
		v.workspace.controller.Areas = append(slices.Clone(v.workspace.controller.Areas), prerequisites.HeldArea{ID: id})
	}
	return &clientArea{workspace: v.workspace, id: id, closed: v.closed}, nil
}

// clientArea is one client closure a view opened. It refuses a write once the
// closure is sealed, and every call once the view's callback returned, as the
// store's area does; a file, once written, is never replaced.
type clientArea struct {
	workspace *testWorkspace
	id        string
	closed    *atomic.Bool
}

func (a *clientArea) usable(ctx context.Context, write bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if a.closed != nil && a.closed.Load() || write && a.workspace.areas[a.id] {
		return errors.New("controller bundle capability is closed or read-only")
	}
	return nil
}

func (a *clientArea) Read(ctx context.Context, name string, maximum int) ([]byte, error) {
	if err := a.usable(ctx, false); err != nil {
		return nil, err
	}
	data, exists := a.workspace.clientFiles[a.id+"/"+name]
	if !exists || len(data) > maximum {
		return nil, errors.New("controller bundle read failed or exceeded its bound")
	}
	return slices.Clone(data), nil
}

func (a *clientArea) Write(ctx context.Context, name string, data []byte, _ bool) error {
	if err := a.usable(ctx, true); err != nil {
		return err
	}
	if _, exists := a.workspace.clientFiles[a.id+"/"+name]; exists {
		return errors.New("controller bundle file exists")
	}
	a.workspace.clientFiles[a.id+"/"+name] = slices.Clone(data)
	return nil
}

func (a *clientArea) EnsureDirectory(ctx context.Context, _ string) error { return a.usable(ctx, true) }
func (a *clientArea) Verify(ctx context.Context) error                    { return a.usable(ctx, false) }

func (a *clientArea) Entries(ctx context.Context) ([]prerequisites.BundleEntry, error) {
	if err := a.usable(ctx, false); err != nil {
		return nil, err
	}
	entries := []prerequisites.BundleEntry{}
	for _, name := range slices.Sorted(maps.Keys(a.workspace.clientFiles)) {
		if rest, ok := strings.CutPrefix(name, a.id+"/"); ok {
			entries = append(entries, prerequisites.BundleEntry{Path: rest, Size: int64(len(a.workspace.clientFiles[name]))})
		}
	}
	return entries, nil
}

func (a *clientArea) Location(ctx context.Context) (prerequisites.BundleLocation, error) {
	if err := a.usable(ctx, false); err != nil {
		return prerequisites.BundleLocation{}, err
	}
	sealed := a.workspace.areas[a.id]
	return prerequisites.BundleLocation{Writable: !sealed, Sealed: sealed}, nil
}

func (v *testView) SealClientArea(_ context.Context, id string) error {
	sealed, exists := v.workspace.areas[id]
	if !exists {
		return errors.New("area is not attributed")
	}
	if !sealed {
		v.workspace.areas[id] = true
	}
	return nil
}

// Secrets lends the context's secret area only inside a transaction, as the
// store does; the binder these tests use keeps its produced entries itself, so
// the area it is handed is nil.
func (v *testView) Secrets(ctx context.Context, callback func(secretstore.Context, secretstore.Area) error) error {
	if callback == nil {
		return errors.New("lifecycle secret callback is missing")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !v.workspace.transacting {
		return errors.New("a secret area is lent only inside a lifecycle transaction")
	}
	return callback(secretstore.Context{Name: testContextName, Mode: "ready", Revision: v.workspace.revision}, nil)
}

// RetainDependencies records what it was asked to retain and keeps it in the
// controller record as the store does: a source or a resolution is never
// replaced under its identity, a resolution is kept only complete and beside
// its exact sources, and the resolutions it supersedes are retired only beside
// it, never the receipt's own.
func (v *testView) RetainDependencies(_ context.Context, definition *prerequisites.Definition, sources []prerequisites.DependencySource, superseded []string) error {
	state := v.workspace.controller.State
	retained, definitions := slices.Clone(state.RetainedSources), slices.Clone(state.RetainedDefinitions)
	if definition == nil && len(superseded) != 0 {
		return errors.New("a controller stage retires resolutions only beside the one that supersedes them")
	}
	for _, digest := range superseded {
		if len(digest) != 64 || strings.Trim(digest, "0123456789abcdef") != "" || definition != nil && digest == definition.ResolutionDigest ||
			state.Receipt.Definition != nil && digest == state.Receipt.Definition.ResolutionDigest {
			return errors.New("a controller stage may not retire that resolution")
		}
	}
	definitions = slices.DeleteFunc(definitions, func(item prerequisites.Definition) bool { return slices.Contains(superseded, item.ResolutionDigest) })
	for _, source := range sources {
		index := slices.IndexFunc(retained, func(item prerequisites.DependencySource) bool { return item.ID == source.ID })
		if index >= 0 && retained[index] != source {
			return errors.New("a retained controller dependency source cannot be replaced")
		}
		if index < 0 {
			retained = append(retained, source)
		}
	}
	if definition != nil {
		index := slices.IndexFunc(definitions, func(item prerequisites.Definition) bool { return item.ResolutionDigest == definition.ResolutionDigest })
		if index >= 0 && !prerequisites.SameDefinition(definitions[index], *definition) {
			return errors.New("a controller stage would replace immutable resolution evidence")
		}
		if index < 0 {
			if err := prerequisites.ValidateResolvedDefinition(*definition); err != nil {
				return err
			}
			if slices.ContainsFunc(definition.Sources, func(source prerequisites.DependencySource) bool { return !slices.Contains(retained, source) }) {
				return errors.New("retained controller resolution lacks its exact sources")
			}
			definitions = append(definitions, prerequisites.CloneDefinition(*definition))
		}
		v.workspace.resolutions++
	}
	v.workspace.retained = append(v.workspace.retained, sources...)
	v.workspace.controller.State.RetainedSources, v.workspace.controller.State.RetainedDefinitions = retained, definitions
	return nil
}

// testCapability records what the engine asked it to do and replays scripted
// outcomes, so every orchestration rule is observable without an adapter. The
// engine calls it from each running block's own goroutine, so what it records
// is guarded.
type testCapability struct {
	mutex        sync.Mutex
	definitions  []reconciliation.BlockDefinition
	reservations []prerequisites.HostReservation
	sshClaims    []SSHReservation
	secrets      []string
	unsupported  []Refusal
	applies      []string
	destroys     []string
	observes     []string
	// calls is every attempt and observation in the order the engine made
	// them, so a test can prove what ran before what without reading two
	// lists that each know only their own half.
	calls        []string
	outcomes     []Result
	observations []Observation
	planErr      error
	applyErr     error
	material     []map[string]secrets.Material
	executions   []Execution
	extraGroup   string
	probes       []string
	quiescence   map[string]Quiescence
	quiescentErr error
	observeErr   error
	removals     []string
	removalErr   error
	consumes     map[string][]string
	// hold runs at the start of an attempt and released at its end, so a test
	// can keep blocks in flight and observe exactly which of them overlap.
	// observeHold is the same moment of an observation.
	hold        func(string)
	released    func(string)
	observeHold func(string)
	destroyHold func(string)
	// outcomeFor and errorFor answer per block. Blocks running together finish
	// in no fixed order, so a queue of scripted outcomes would be handed out by
	// a race rather than by the test.
	outcomeFor map[string]Result
	errorFor   map[string]error
	// verbose, when set, is how many more bytes an attempt prints on its
	// adapter output, asked as it prints them.
	verbose func() int
}

// print writes what a verbose adapter prints beside its fixed lines.
func (c *testCapability) print(execution Execution) {
	c.mutex.Lock()
	verbose := c.verbose
	c.mutex.Unlock()
	if verbose != nil && execution.Output != nil {
		_, _ = execution.Output.Write([]byte(strings.Repeat("x", verbose())))
	}
}

// record appends what one call saw under the fixture's own lock, so blocks
// running at the same time never race what it remembers. The lock is never
// held across the call itself, because a test that holds one block until
// another starts would otherwise deadlock on the bookkeeping.
func (c *testCapability) record(target *[]string, value string) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	*target = append(*target, value)
}

func (c *testCapability) recordExecution(execution Execution) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.executions = append(c.executions, execution)
}

// Removal answers from the fixture. It records which frozen blocks a removal
// read, so a test can prove a plan came from them rather than from input.
func (c *testCapability) Removal(_ context.Context, block reconciliation.Block) (Removal, error) {
	c.record(&c.removals, block.ID)
	if c.removalErr != nil {
		return Removal{}, c.removalErr
	}
	return Removal{
		Description: "remove " + block.Object,
		Impacts:     []string{"remove-" + block.Object},
		Consumes:    c.consumes[block.ID],
		Groups:      []reconciliation.Group{{ID: "remove", Description: "remove it", Machines: []string{block.Object}}},
	}, nil
}

// Quiescent answers from the fixture, and settles by default so a removal that
// is not exercising the gate is not written as though it were.
func (c *testCapability) Quiescent(_ context.Context, probe Probe) (Quiescence, error) {
	c.record(&c.probes, probe.Block.ID)
	if c.quiescentErr != nil {
		return Quiescence{}, c.quiescentErr
	}
	if state, found := c.quiescence[probe.Block.ID]; found {
		return state, nil
	}
	return Quiescence{State: Quiescent, Reason: "nothing it owns is in use"}, nil
}

func (c *testCapability) Plan(_ context.Context, input PlanInput) (CapabilityPlan, error) {
	if c.planErr != nil {
		return CapabilityPlan{}, c.planErr
	}
	return CapabilityPlan{Definitions: c.definitions, Reservations: c.reservations, SSHReservations: c.sshClaims, Secrets: c.secrets}, nil
}

func (c *testCapability) Unsupported(*compilation.State) []Refusal { return c.unsupported }

func (c *testCapability) next(outcomes *[]Result) Result {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if len(*outcomes) == 0 {
		return Result{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"ok":true}`)}
	}
	value := (*outcomes)[0]
	*outcomes = (*outcomes)[1:]
	return value
}

func (c *testCapability) Apply(ctx context.Context, execution Execution) (Result, error) {
	c.mutex.Lock()
	c.applies = append(c.applies, execution.Block.ID)
	c.calls = append(c.calls, "apply:"+execution.Block.ID)
	c.material = append(c.material, execution.Material)
	c.executions = append(c.executions, execution)
	hold, released, scripted, failure := c.hold, c.released, c.outcomeFor[execution.Block.ID], c.errorFor[execution.Block.ID]
	c.mutex.Unlock()
	if hold != nil {
		hold(execution.Block.ID)
	}
	if released != nil {
		defer released(execution.Block.ID)
	}
	if scripted.Outcome != "" || failure != nil {
		if failure != nil {
			return Result{Outcome: reconciliation.OutcomeFailed}, failure
		}
		return scripted, nil
	}
	if execution.Progress != nil {
		execution.Progress(ctx, "pull-image", "running")
		execution.Progress(ctx, "pull-image", "ok")
	}
	if c.extraGroup != "" {
		execution.Progress(ctx, c.extraGroup, "ok")
	}
	if execution.Output != nil {
		_, _ = execution.Output.Write([]byte("TASK [acquire the image]\nok: [controller]\n"))
	}
	c.print(execution)
	if c.applyErr != nil {
		return Result{Outcome: reconciliation.OutcomeFailed}, c.applyErr
	}
	result := c.next(&c.outcomes)
	if result.Outcome == reconciliation.OutcomeFailed {
		return result, errors.New("capability failed")
	}
	return result, nil
}

func (c *testCapability) Destroy(_ context.Context, execution Execution) (Result, error) {
	c.record(&c.destroys, execution.Block.ID)
	c.recordExecution(execution)
	c.mutex.Lock()
	hold := c.destroyHold
	c.mutex.Unlock()
	if hold != nil {
		hold(execution.Block.ID)
	}
	c.print(execution)
	return c.next(&c.outcomes), nil
}

func (c *testCapability) Observe(_ context.Context, execution Execution) (Observation, error) {
	return c.observe("observe:", execution)
}

// ObserveRemoval answers from the same script as Observe, and names itself in
// calls, so a test proves which of the two a resolution asked.
func (c *testCapability) ObserveRemoval(_ context.Context, execution Execution) (Observation, error) {
	return c.observe("observe-removal:", execution)
}

func (c *testCapability) observe(call string, execution Execution) (Observation, error) {
	c.record(&c.observes, execution.Block.ID)
	c.record(&c.calls, call+execution.Block.ID)
	c.recordExecution(execution)
	c.mutex.Lock()
	hold := c.observeHold
	c.mutex.Unlock()
	if hold != nil {
		hold(execution.Block.ID)
	}
	c.mutex.Lock()
	defer c.mutex.Unlock()
	if c.observeErr != nil {
		return Observation{}, c.observeErr
	}
	if len(c.observations) == 0 {
		return Observation{Effect: reconciliation.EffectUnknown}, nil
	}
	value := c.observations[0]
	c.observations = c.observations[1:]
	return value, nil
}

type testResolver struct {
	capability Capability
	bindings   []CapabilityBinding
	missing    bool
}

func (r testResolver) Bindings() []CapabilityBinding {
	if len(r.bindings) != 0 {
		return r.bindings
	}
	return []CapabilityBinding{{Kind: "ArtifactServer", Implementation: "artifact-server-nginx-v1"}}
}

func (r testResolver) Resolve(kind, implementation string) (Capability, bool) {
	if r.missing || !slices.Contains(r.Bindings(), CapabilityBinding{Kind: kind, Implementation: implementation}) {
		return nil, false
	}
	return r.capability, true
}

type testCompiler struct {
	state *compilation.State
	err   error
}

func (c testCompiler) Compile(context.Context, desiredstate.Sources) (*compilation.State, *compilation.Report, error) {
	if c.err != nil {
		return nil, nil, c.err
	}
	return c.state, &compilation.Report{}, nil
}

type testBinder struct {
	bound    []string
	released []string
	issued   int
	material map[string]secrets.Material
	bindErr  error
	// releaseErr fails every Release, as a custody store that cannot drop a
	// binding does, before anything is released.
	releaseErr error
	// bindingsErr fails every listing, as a custody store that cannot be read
	// does.
	bindingsErr error
	// lost names bindings the store no longer lists although no Release took
	// them, as an earlier defect or an operator's edit of the keyring leaves;
	// unreadable fails the Reopen of a binding it still lists with the error the
	// store gives for material it cannot read.
	lost       []string
	unreadable map[string]error
	// kill runs first in every Bind and Release, and in every Produce and
	// Withdraw that publishes, named by the point it would publish, so a test
	// can stop an invocation there; an error it returns fails it before
	// anything is bound, released, produced or withdrawn.
	kill func(point string) error
	// produced holds each produced entry's bytes by "<block>/<name>", as the
	// custody store keys it; journal lists every produced publication and
	// withdrawal in order. produceErr and withdrawErr fail every Produce and
	// Withdraw before anything changes.
	produced    map[string]string
	journal     []string
	produceErr  error
	withdrawErr error
}

// Produce models the custody store: one publication for the block, which an
// output equal to its entry does not need.
func (b *testBinder) Produce(ctx context.Context, selected secretstore.Context, _ secretstore.Area, request custody.ProduceRequest) ([]secretstore.Produced, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if selected.Name != testContextName || selected.Mode != "ready" {
		return nil, errors.New("produced material was offered for another context")
	}
	next := maps.Clone(b.produced)
	if next == nil {
		next = map[string]string{}
	}
	result := make([]secretstore.Produced, 0, len(request.Outputs))
	for _, output := range request.Outputs {
		value, _ := output.Material.Part(secrets.ValuePart)
		next[request.Block+"/"+output.Name] = string(value)
		clear(value)
		result = append(result, secretstore.Produced{Block: request.Block, Name: output.Name, Version: "ver-" + request.Block + "-" + output.Name})
	}
	if maps.Equal(next, b.produced) {
		return result, nil
	}
	if b.kill != nil {
		if err := b.kill("publish produced material"); err != nil {
			return nil, err
		}
	}
	if b.produceErr != nil {
		return nil, b.produceErr
	}
	b.produced = next
	b.journal = append(b.journal, "produce "+request.Block)
	return result, nil
}

// producedEntries names every produced entry with its bytes, sorted.
func (b *testBinder) producedEntries() []string {
	entries := make([]string, 0, len(b.produced))
	for key, value := range b.produced {
		entries = append(entries, key+"="+value)
	}
	slices.Sort(entries)
	return entries
}

// Withdraw removes every entry in one publication, and publishes nothing when
// there is none.
func (b *testBinder) Withdraw(ctx context.Context, selected secretstore.Context, _ secretstore.Area) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if selected.Name != testContextName {
		return false, errors.New("produced material was withdrawn from another context")
	}
	if len(b.produced) == 0 {
		return false, nil
	}
	if b.kill != nil {
		if err := b.kill("withdraw produced material"); err != nil {
			return false, err
		}
	}
	if b.withdrawErr != nil {
		return false, b.withdrawErr
	}
	b.produced = nil
	b.journal = append(b.journal, "withdraw")
	return true, nil
}

func (b *testBinder) Bind(_ context.Context, request custody.BindRequest) (secretstore.Binding, error) {
	if b.kill != nil {
		if err := b.kill("publish secret binding"); err != nil {
			return secretstore.Binding{}, err
		}
	}
	if b.bindErr != nil {
		return secretstore.Binding{}, b.bindErr
	}
	b.bound = append(b.bound, request.Names...)
	b.issued++
	return secretstore.Binding{ID: fmt.Sprintf("bind-%d", b.issued)}, nil
}

// Reopen models the store: a released binding is gone, so an operation that
// still needs its material can no longer acquire it.
func (b *testBinder) Reopen(_ context.Context, request custody.BindingRequest) ([]secretstore.BoundMaterial, error) {
	if slices.Contains(b.released, request.BindingID) {
		return nil, errors.New("secret binding does not exist")
	}
	if slices.Contains(b.lost, request.BindingID) {
		return nil, secretstore.Failure("input", "secret binding does not exist")
	}
	if err := b.unreadable[request.BindingID]; err != nil {
		return nil, err
	}
	out := []secretstore.BoundMaterial{}
	for name, value := range b.material {
		out = append(out, secretstore.BoundMaterial{
			Version:  secretstore.Version{Declaration: secrets.VersionDeclaration{Name: name}},
			Material: value,
		})
	}
	return out, nil
}

// Bindings lists what the store still holds: every binding issued and not yet
// released.
func (b *testBinder) Bindings(context.Context, custody.BindingsRequest) ([]string, error) {
	if b.bindingsErr != nil {
		return nil, b.bindingsErr
	}
	held := []string{}
	for issued := 1; issued <= b.issued; issued++ {
		if binding := fmt.Sprintf("bind-%d", issued); !slices.Contains(b.released, binding) && !slices.Contains(b.lost, binding) {
			held = append(held, binding)
		}
	}
	return held, nil
}

// Release refuses a cancelled context before anything else, as the custody
// store does.
func (b *testBinder) Release(ctx context.Context, request custody.BindingRequest) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if b.kill != nil {
		if err := b.kill("release secret binding"); err != nil {
			return false, err
		}
	}
	if b.releaseErr != nil {
		return false, b.releaseErr
	}
	b.released = append(b.released, request.BindingID)
	return true, nil
}

type testHost struct {
	identity controller.InstalledHostIdentity
}

func (h testHost) Identity(context.Context) (controller.InstalledHostIdentity, error) {
	return h.identity, nil
}

type testAutomation struct{ digest string }

func (a testAutomation) CatalogDigest() string { return a.digest }

// testGuard stands in for the private execution boundary. Blocks running at
// the same time each enter it, exactly as they do the real one, so what it
// counts is guarded.
type testGuard struct {
	mutex sync.Mutex
	calls int
}

func (g *testGuard) WithPython(ctx context.Context, area prerequisites.BundleArea, _ prerequisites.ExecutionRequirement, use func(prerequisites.PythonLaunch, func() error) error) error {
	g.mutex.Lock()
	g.calls++
	g.mutex.Unlock()
	return use(prerequisites.PythonLaunch{Loader: "/loader"}, func() error { return nil })
}

func (g *testGuard) entered() int {
	g.mutex.Lock()
	defer g.mutex.Unlock()
	return g.calls
}

type testBundle struct{}

func (testBundle) Read(context.Context, string, int) ([]byte, error) { return nil, nil }
func (testBundle) Write(context.Context, string, []byte, bool) error { return nil }
func (testBundle) EnsureDirectory(context.Context, string) error     { return nil }
func (testBundle) Entries(context.Context) ([]prerequisites.BundleEntry, error) {
	return nil, nil
}
func (testBundle) Verify(context.Context) error { return nil }
func (testBundle) Location(context.Context) (prerequisites.BundleLocation, error) {
	return prerequisites.BundleLocation{Path: "/bundle"}, nil
}

type testConfirmer struct {
	asked   int
	decline bool
}

func (c *testConfirmer) Confirm(context.Context, string, string) error {
	c.asked++
	if c.decline {
		return errors.New("declined")
	}
	return nil
}

type testPresenter struct{ presented []PlanResult }

func (p *testPresenter) PresentLifecyclePlan(_ context.Context, result PlanResult) error {
	p.presented = append(p.presented, result)
	return nil
}

// testClock advances a second per reading so records carry distinct stamps.
// Blocks running at the same time stamp their own records through it, so it is
// guarded: the production clock is the wall clock, which needs no guard.
type testClock struct {
	mutex  sync.Mutex
	moment time.Time
}

func (c *testClock) Now() time.Time {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.moment = c.moment.Add(time.Second)
	return c.moment
}

type harness struct {
	service    Service
	workspace  *testWorkspace
	capability *testCapability
	binder     *testBinder
	confirmer  *testConfirmer
	presenter  *testPresenter
	guard      *testGuard
	entropy    []byte
}

func definition(id string) reconciliation.BlockDefinition {
	return reconciliation.BlockDefinition{
		ID: id, Description: "serve " + id, Stage: reconciliation.StageInfraComponents,
		Kind: "ArtifactServer", Object: id,
		Implementation: "artifact-server-nginx-v1", ContentDigest: strings.Repeat("c", 64),
		Request: json.RawMessage(`{"name":"` + id + `"}`),
		Groups:  []reconciliation.Group{{ID: "pull-image", Description: "acquire the pinned server image", Machines: []string{"controller"}}},
	}
}

func newHarness(t *testing.T, blocks ...string) *harness {
	t.Helper()
	definitions := make([]reconciliation.BlockDefinition, 0, len(blocks))
	for _, id := range blocks {
		definitions = append(definitions, definition(id))
	}
	return newPlannedHarness(t, definitions)
}

func newPlannedHarness(t *testing.T, definitions []reconciliation.BlockDefinition) *harness {
	t.Helper()
	host, err := controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1,
		"0123456789abcdef0123456789abcdef", "12345678-1234-5678-9abc-def012345678", "fedcba98-7654-3210-fedc-ba9876543210")
	if err != nil {
		t.Fatal(err)
	}
	digest, err := host.PrivateDigest()
	if err != nil {
		t.Fatal(err)
	}
	catalog := api.NewCatalog([]api.Object{api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
		api.FieldValue{Name: "controller", Value: api.MapValue(api.FieldValue{Name: "machineRef", Value: api.StringValue("controller")})},
	))})
	pristine, err := reconciliation.PristineEvidence().Bytes()
	if err != nil {
		t.Fatal(err)
	}
	workspace := &testWorkspace{
		area: newArea(), runArea: newArea(), evidence: pristine, revision: testRevision,
		inputs: desiredstate.Sources{Roots: []string{"/synthetic"}, Files: []desiredstate.SourceFile{
			desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte("kind: Environment\n")),
		}},
		controller: prerequisites.StorageView{
			Exists: true, Initialized: true,
			State: prerequisites.HostState{
				Host: host,
				Receipt: prerequisites.SetupReceipt{
					Status: "complete", CatalogDigest: strings.Repeat("b", 64),
					Definition: &prerequisites.Definition{Bootstrap: testBootstrap()},
				},
				Bindings: []prerequisites.ControllerBinding{{Context: testContextName, Machine: "controller", HostDigest: digest}},
			},
		},
	}
	workspace.controller.OpenBundle = func(context.Context, string) (prerequisites.BundleArea, error) {
		workspace.opened++
		return testBundle{}, nil
	}
	capability := &testCapability{definitions: definitions, secrets: []string{"artifact-server-tls"}}
	binder := &testBinder{material: map[string]secrets.Material{"artifact-server-tls": secrets.NewMaterial(nil)}}
	confirmer := &testConfirmer{}
	presenter := &testPresenter{}
	guard := &testGuard{}
	clock := &testClock{moment: time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)}
	h := &harness{workspace: workspace, capability: capability, binder: binder, confirmer: confirmer, presenter: presenter, guard: guard}
	h.entropy = []byte{1}
	h.service = New(workspace, nil, testCompiler{state: compilation.NewState(catalog, catalog, nil)}, binder,
		testHost{identity: host}, testAutomation{digest: testAutomaton}, guard, testResolver{capability: capability},
		Options{
			Confirmer: confirmer, Presenter: presenter, Clock: clock,
			Entropy: func(buffer []byte) (int, error) {
				for index := range buffer {
					buffer[index] = h.entropy[0]
				}
				h.entropy[0]++
				return len(buffer), nil
			},
			// One block at a time, so a journey asserting an exact order reads
			// one schedule rather than a race. The scheduler's own suite raises
			// the bound and asserts what running blocks together proves.
			Concurrency: 1,
			Selection:   func(context.Context) (string, error) { return testContextName, nil },
			Executable:  Executable{Version: "devel", Commit: "abcdef1"},
			Operations:  func(area operationstore.Area) OperationStore { return operationstore.New(area, clock.Now) },
		})
	return h
}

func TestFreshApplyRegistersExecutesAndProjectsEvidence(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil || result == nil {
		t.Fatalf("apply = %+v (%v)", result, err)
	}
	if result.Receipt.State != "done" || result.Receipt.Next != "none" || !reconciliation.ValidOperationID(result.Receipt.Operation) {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
	if !slices.Equal(h.capability.applies, []string{"artifact-server-lab"}) {
		t.Fatalf("applied blocks = %v", h.capability.applies)
	}
	if !slices.Equal(h.binder.bound, []string{"artifact-server-tls"}) {
		t.Fatalf("bound secrets = %v", h.binder.bound)
	}
	applied, err := reconciliation.EvidenceFor(reconciliation.Apply, reconciliation.OperationDone)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := applied.Bytes()
	if string(h.workspace.evidence) != string(want) {
		t.Fatalf("evidence = %q, want %q", h.workspace.evidence, want)
	}
	if len(h.workspace.reservations) == 0 && len(h.capability.reservations) != 0 {
		t.Fatal("the operation did not publish its host reservations")
	}
	if h.guard.calls != 1 {
		t.Fatalf("execution guard calls = %d", h.guard.calls)
	}
	if len(result.Logs) == 0 {
		t.Fatal("the operation created no private log")
	}
}

// Setup prepares a host without claiming any context, so the first apply is
// what records this context against it. A later apply revalidates exactly that
// relationship instead of publishing another one.
func TestFirstApplyBindsTheContextToItsControllerHost(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.workspace.controller.State.Bindings = nil
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	bindings := h.workspace.controller.State.Bindings
	if len(bindings) != 1 || bindings[0].Context != testContextName || bindings[0].Machine != "controller" {
		t.Fatalf("bindings = %#v", bindings)
	}
	if h.workspace.binds != 1 {
		t.Fatalf("binds = %d", h.workspace.binds)
	}
	// Destroying and applying again revalidates the same binding.
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if len(h.workspace.controller.State.Bindings) != 1 {
		t.Fatalf("a repeated apply republished the binding: %#v", h.workspace.controller.State.Bindings)
	}
}

// A context already bound to another controller Machine is never silently
// rebound, and the refusal happens before the operation registers.
func TestApplyRefusesToRebindAnEstablishedContext(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.workspace.controller.State.Bindings = []prerequisites.ControllerBinding{
		{Context: testContextName, Machine: "replacement", HostDigest: h.workspace.controller.State.Bindings[0].HostDigest},
	}
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if diagnostics.Of(err)[0].Code != "controller.identity" {
		t.Fatalf("rebind error = %v", err)
	}
	if len(h.capability.applies) != 0 {
		t.Fatalf("a refused binding still executed %v", h.capability.applies)
	}
}

// An unprepared host refuses before any effect and names setup, because no
// context can claim a host that has none of the shared prerequisites.
func TestApplyRefusesAnUnpreparedControllerHost(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.workspace.controller.State.Receipt.Status = "pending"
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if diagnostics.Of(err)[0].Code != "controller.identity" || !strings.Contains(diagnostics.Of(err)[0].Remediation, "bootwright setup") {
		t.Fatalf("unprepared host error = %v", err)
	}
	if len(h.capability.applies) != 0 || h.workspace.binds != 0 {
		t.Fatalf("a refused host still executed %v", h.capability.applies)
	}
}

// testProgress collects the stream the presenter would render. Blocks running
// at the same time report from their own goroutines, exactly as they do to the
// real presenter, so this one is guarded the same way.
type testProgress struct {
	mutex          sync.Mutex
	rows           []string
	location       string
	rowsAtLocation int
}

func (p *testProgress) ReportProgress(_ context.Context, event ProgressEvent) {
	row := event.Description + ":" + event.Detail + ":" + event.Status
	if event.Total != 0 {
		row += ":" + strconv.Itoa(event.Position) + "/" + strconv.Itoa(event.Total)
	}
	if event.Phase != EffectPhase {
		row = event.Phase + ":" + row
	}
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.rows = append(p.rows, row)
}

func (p *testProgress) ReportLogLocation(_ context.Context, location string) {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.location = location
	p.rowsAtLocation = len(p.rows)
}

// reported copies the stream so an assertion reads a stable list.
func (p *testProgress) reported() []string {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	return slices.Clone(p.rows)
}

// Progress names each block by its description and each group by the frozen
// plan's own description of it, so the operator never reads an identifier.
func TestProgressNamesBlocksAndGroupsFromTheFrozenPlan(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	progress := &testProgress{}
	h.service.options.Progress = progress
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"serve artifact-server-lab::running:1/1",
		"serve artifact-server-lab:acquire the pinned server image:running:1/1",
		"serve artifact-server-lab:acquire the pinned server image:ok:1/1",
		"serve artifact-server-lab::done:1/1",
	}
	if !slices.Equal(progress.rows, want) {
		t.Fatalf("progress = %q, want %q", progress.rows, want)
	}
}

func TestDeclinedConfirmationRegistersNothing(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.confirmer.decline = true
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab"}); err == nil {
		t.Fatal("a declined apply succeeded")
	}
	if h.confirmer.asked != 1 || len(h.presenter.presented) != 1 {
		t.Fatalf("confirmation asked=%d presented=%d", h.confirmer.asked, len(h.presenter.presented))
	}
	if len(h.workspace.area.files) != 0 {
		t.Fatalf("a declined apply wrote operation records: %v", slices.Collect(maps.Keys(h.workspace.area.files)))
	}
	if len(h.binder.bound) != 0 || len(h.workspace.reservations) != 0 || h.workspace.mutations != 0 {
		t.Fatal("a declined apply bound, reserved or mutated")
	}
	pristine, _ := reconciliation.PristineEvidence().Bytes()
	if string(h.workspace.evidence) != string(pristine) {
		t.Fatal("a declined apply changed the context evidence")
	}
}

func TestAuthorizationTokenAndBorrowedCredentialsRefuseBeforeAnyRead(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", Authorizations: []string{"data-loss"}, SkipConfirmation: true})
	if code := firstCode(err); code != "lifecycle.authorization" {
		t.Fatalf("authorization refusal = %q", code)
	}
	_, err = h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true, SSH: borrowedOptions()})
	if code := firstCode(err); code != "lifecycle.state" {
		t.Fatalf("borrowed credential refusal = %q", code)
	}
	if len(h.presenter.presented) != 0 || h.workspace.mutations != 0 {
		t.Fatal("a refused request presented a plan or mutated")
	}
}

// Each object a capability refuses is its own diagnostic, carrying the object,
// the reason the capability gave and its remedy, so plan and apply say why a
// cluster or Machine is refused and what to change rather than only naming it.
// An object two capabilities refuse alike is reported once, and a refusal that
// names no remedy directs the operator to the object itself.
func TestUnsupportedObjectsRefuseBeforeRegistration(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	cluster := Refusal{
		Kind: "ContainerCluster", Name: "sno",
		Reason:      "a declared node selects an install profile, so two installations would write its disk",
		Remediation: "remove spec.os.installProfileRef from Machine/sno-01 or drop it from ContainerCluster/sno",
	}
	h.capability.unsupported = []Refusal{
		{Kind: "Machine", Name: "guest", Reason: "the install profile selects spec.subscription"},
		cluster, cluster,
	}
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	want := []diagnostics.Diagnostic{
		{
			Severity: "error", Code: "lifecycle.unsupported", Message: cluster.Reason, Remediation: cluster.Remediation,
			Object: &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: "ContainerCluster", Name: "sno"},
		},
		{
			Severity: "error", Code: "lifecycle.unsupported", Message: "the install profile selects spec.subscription", Remediation: "correct Machine/guest",
			Object: &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: "Machine", Name: "guest"},
		},
	}
	if reported := diagnostics.Of(err); !reflect.DeepEqual(reported, want) {
		t.Fatalf("unsupported refusal = %+v, want %+v", reported, want)
	}
	if len(h.workspace.area.files) != 0 || h.workspace.mutations != 0 {
		t.Fatal("an unsupported graph registered an operation")
	}
}

// An object of a kind no capability claims is refused by the engine itself,
// naming the object, that no capability realizes it, and the supported shape.
func TestAnUnclaimedObjectRefusesNamingItsReasonAndRemedy(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.service.compiler = testCompiler{state: withEnabledPlaybook()}
	_, err := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab"})
	want := []diagnostics.Diagnostic{{
		Severity: "error", Code: "lifecycle.unsupported", Message: "no capability of this executable runs an enabled CustomPlaybook",
		Remediation: "remove CustomPlaybook/tune from the selected Environment, or use an example within the supported shape such as " + supportedExample,
		Object:      &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: "CustomPlaybook", Name: "tune"},
	}}
	if reported := diagnostics.Of(err); !reflect.DeepEqual(reported, want) {
		t.Fatalf("unclaimed refusal = %+v, want %+v", reported, want)
	}
}

// Repeating a completed apply over the same desired state is the ordinary way
// an operator asks whether anything is left to do. It settles without an
// effect, so a script may repeat the verb, and the destroy that follows still
// removes exactly what the first apply created.
func TestAppliedContextRepeatsWithoutEffectAndDestroysInstead(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	first, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	applies, mutations, bound := len(h.capability.applies), h.workspace.mutations, len(h.binder.bound)
	repeated, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatalf("a repeated apply failed: %v", err)
	}
	if !repeated.Settled || repeated.Receipt.State != "done" || repeated.Receipt.Next != "none" {
		t.Fatalf("repeated apply = %+v", repeated.Receipt)
	}
	if repeated.Receipt.Operation != first.Receipt.Operation {
		t.Fatalf("a repeated apply named operation %q, want the completed %q", repeated.Receipt.Operation, first.Receipt.Operation)
	}
	if len(h.capability.applies) != applies {
		t.Fatalf("a repeated apply ran %d blocks", len(h.capability.applies)-applies)
	}
	if h.workspace.mutations != mutations {
		t.Fatal("a repeated apply opened a mutation transaction")
	}
	if len(h.binder.bound) != bound {
		t.Fatal("a repeated apply bound Secret material")
	}
	if len(repeated.Blocks) != 1 || repeated.Blocks[0].State != "done" {
		t.Fatalf("repeated apply blocks = %+v", repeated.Blocks)
	}
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil || result.Receipt.State != "done" || result.Receipt.Next != "none" {
		t.Fatalf("destroy = %+v (%v)", result, err)
	}
	if !slices.Equal(h.capability.destroys, []string{"artifact-server-lab"}) {
		t.Fatalf("destroyed blocks = %v", h.capability.destroys)
	}
	pristine, _ := reconciliation.PristineEvidence().Bytes()
	if string(h.workspace.evidence) != string(pristine) {
		t.Fatalf("a completed destroy left evidence %q", h.workspace.evidence)
	}
	if len(h.workspace.reservations) != 0 {
		t.Fatal("a completed destroy retained its host reservations")
	}
	if len(h.binder.released) == 0 {
		t.Fatal("a completed destroy retained its Secret bindings")
	}
}

// A context that owns nothing is already in the state a removal would leave
// it in, so the verb succeeds having done nothing rather than refusing.
func TestDestroyWithNothingOwnedSucceedsWithoutEffects(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatalf("destroy without apply = %v", err)
	}
	if !result.Settled || result.Receipt.State != "done" || result.Receipt.Next != "none" {
		t.Fatalf("destroy without apply = %+v", result.Receipt)
	}
	if result.Receipt.Operation != "none" || len(result.Blocks) != 0 {
		t.Fatalf("destroy without apply named %q over %d blocks", result.Receipt.Operation, len(result.Blocks))
	}
	if len(h.capability.destroys) != 0 || h.workspace.mutations != 0 {
		t.Fatal("destroy without apply performed work")
	}
	if h.confirmer.asked != 0 {
		t.Fatal("destroy without apply asked for confirmation")
	}
}

// A completed removal leaves nothing to remove, so repeating it settles too.
func TestDestroyOverACompletedDestroySucceedsWithoutEffects(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	removals := len(h.capability.destroys)
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil || !result.Settled || result.Receipt.State != "done" {
		t.Fatalf("repeated destroy = %+v (%v)", result, err)
	}
	if len(h.capability.destroys) != removals {
		t.Fatal("a repeated destroy removed something again")
	}
}

// Desired state changes only at rest, and a completed apply is not at rest
// until what it owns is removed: a changed input names the destroy it needs
// rather than being silently realized over the old one.
func TestApplyOverACompletedApplyWithChangedInputRefuses(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	h.workspace.inputs = desiredstate.Sources{Roots: []string{"/synthetic"}, Files: []desiredstate.SourceFile{
		desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte("kind: Environment\n# edited\n")),
	}}
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if code := firstCode(err); code != "lifecycle.state" {
		t.Fatalf("apply over a changed input = %q (%+v)", code, result)
	}
	if len(h.capability.applies) != 1 {
		t.Fatalf("a refused apply ran %d blocks", len(h.capability.applies))
	}
}

func TestFailedBlockLeavesTheOperationContinuable(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err == nil || result == nil || result.Receipt.State != "failed" || result.Receipt.Next != "continue-apply" {
		t.Fatalf("failed apply = %+v (%v)", result, err)
	}
	applied, _ := reconciliation.EvidenceFor(reconciliation.Apply, reconciliation.OperationFailed)
	want, _ := applied.Bytes()
	if string(h.workspace.evidence) != string(want) {
		t.Fatalf("evidence = %q", h.workspace.evidence)
	}
	retried, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil || retried.Receipt.State != "done" {
		t.Fatalf("retry = %+v (%v)", retried, err)
	}
	if len(h.capability.applies) != 2 {
		t.Fatalf("retry ran %d attempts", len(h.capability.applies))
	}
	if h.presenter.presented[1].Continuation != true {
		t.Fatal("a continuation was presented as a fresh plan")
	}
}

func TestUnknownOutcomeIsResolvedFromLiveEvidence(t *testing.T) {
	for name, tc := range map[string]struct {
		effect reconciliation.EffectState
		state  string
		next   string
	}{
		"completed": {reconciliation.EffectCompleted, "done", "none"},
		"no effect": {reconciliation.EffectNoEffect, "failed", "continue-apply"},
		"partial":   {reconciliation.EffectPartial, "failed", "continue-apply"},
		"unknown":   {reconciliation.EffectUnknown, "unknown", "resolve"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab")
			h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
			result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
			if err == nil || result.Receipt.State != "unknown" || result.Receipt.Next != "resolve" {
				t.Fatalf("unknown apply = %+v (%v)", result, err)
			}
			h.capability.observations = []Observation{{Effect: tc.effect}}
			resolved, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
			if resolved.Receipt.State != tc.state || resolved.Receipt.Next != tc.next {
				t.Fatalf("resolution = %+v (%v), want %s/%s", resolved.Receipt, err, tc.state, tc.next)
			}
			if len(h.capability.observes) != 1 {
				t.Fatalf("observations = %v", h.capability.observes)
			}
			if tc.effect != reconciliation.EffectCompleted && len(h.capability.applies) != 1 {
				t.Fatal("an unresolved block started another attempt")
			}
		})
	}
}

// An observation that could not be performed is not an inconclusive
// observation. The operator needs the reason it never ran, because the
// unresolved diagnosis sends them to restore a target that was already
// reachable, and repeating the operation reproduces the same silence.
func TestAnObservationThatNeverRanReportsWhy(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an unproved effect completed")
	}
	h.capability.observeErr = diagnostics.NewFailureWithRemediation(
		"controller.identity", "the approved execution bundle is unavailable", "", "run bootwright setup")
	resolved, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err == nil {
		t.Fatalf("a failed observation resolved cleanly = %+v", resolved.Receipt)
	}
	var named bool
	for _, reported := range diagnostics.Of(err) {
		if reported.Code == "controller.identity" && reported.Remediation == "run bootwright setup" {
			named = true
		}
		if strings.HasPrefix(reported.Message, "the outcome of artifact-server-lab is still unknown") {
			t.Fatalf("a failed observation was reported as an inconclusive one = %+v", reported)
		}
	}
	if !named {
		t.Fatalf("resolution discarded why the observation never ran = %+v", diagnostics.Of(err))
	}
	if resolved.Receipt.State != "unknown" || resolved.Receipt.Next != "resolve" {
		t.Fatalf("receipt = %+v", resolved.Receipt)
	}
}

// A target the capability proves is part way realized and its own is the
// ordinary outcome of an interrupted effect. Resolving it to failed is what
// lets the next invocation converge it: an unknown block would start no retry,
// no removal and no deletion, so the context would have nowhere to go.
func TestAPartlyRealizedBlockIsConvergedByRepeatingTheOperation(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an unproved effect completed")
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectPartial}}
	resolved, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err == nil || resolved.Receipt.State != "failed" {
		t.Fatalf("partial resolution = %+v (%v)", resolved.Receipt, err)
	}
	if code := firstCode(err); code != "lifecycle.state" {
		t.Fatalf("a partial resolution reported %q, want a failure an operator can retry", code)
	}
	converged, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil || converged.Receipt.State != "done" {
		t.Fatalf("convergence = %+v (%v)", converged.Receipt, err)
	}
	if len(h.capability.applies) != 2 || len(h.capability.observes) != 1 {
		t.Fatalf("applies = %v, observes = %v", h.capability.applies, h.capability.observes)
	}
}

// The same resolution frees a removal, because a failed operation admits a
// fresh destroy while an unknown one admits nothing at all.
func TestAPartlyRealizedBlockIsAlsoRemovable(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an unproved effect completed")
	}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an unknown block admitted a removal")
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectPartial}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("a partial resolution completed the operation")
	}
	removed, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil || removed.Receipt.State != "done" {
		t.Fatalf("removal after a partial resolution = %+v (%v)", removed, err)
	}
	if !slices.Equal(h.capability.destroys, []string{"artifact-server-lab"}) {
		t.Fatalf("destroyed blocks = %v", h.capability.destroys)
	}
}

// An interrupted removal is converged the same way, so a partial resolution is
// not an apply-only road out.
func TestAPartlyRemovedBlockIsConvergedByRepeatingTheRemoval(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an unproved removal completed")
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectPartial}}
	resolved, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err == nil || resolved.Receipt.State != "failed" || resolved.Receipt.Verb != "destroy" {
		t.Fatalf("partial removal resolution = %+v (%v)", resolved.Receipt, err)
	}
	converged, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil || converged.Receipt.State != "done" {
		t.Fatalf("removal convergence = %+v (%v)", converged.Receipt, err)
	}
}

// The gate runs while no operation exists, so it is none of the plan's
// effects: it reports one check step and makes every block it probes a
// sub-step of it. Reported as an effect row per block, it printed the whole
// removal once before the log location and again after it, so an operator read
// every step twice and the second reading was the only real one.
func TestTheQuiescenceGateReportsOneCheckAheadOfEveryEffect(t *testing.T) {
	h := newHarness(t, "artifact-server-lab", "machine-rhel-01")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	progress := &testProgress{rowsAtLocation: -1}
	h.service.options.Progress = progress
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	gate := []string{
		"check:prove nothing this removal takes back is still in use:ArtifactServer/artifact-server-lab:running",
		"check:prove nothing this removal takes back is still in use:ArtifactServer/machine-rhel-01:running",
		"check:prove nothing this removal takes back is still in use::ok",
	}
	if len(progress.rows) < len(gate) || !slices.Equal(progress.rows[:len(gate)], gate) {
		t.Fatalf("the gate reported %q, want %q", progress.rows, gate)
	}
	if progress.rowsAtLocation != len(gate) {
		t.Fatalf("the log location followed %d rows, want the %d the gate reported", progress.rowsAtLocation, len(gate))
	}
	for _, row := range progress.rows[len(gate):] {
		if strings.HasPrefix(row, CheckPhase+":") {
			t.Fatalf("a check reported after the log location: %q", row)
		}
	}
}

// A check closes with what it proved, exactly as a step does: the refusal is
// why the removal stopped, and a probe that never answered leaves the gate
// with no outcome to claim.
func TestTheQuiescenceGateClosesWithWhatItProved(t *testing.T) {
	for name, tc := range map[string]struct {
		arrange func(*harness)
		want    string
	}{
		"live": {
			arrange: func(h *harness) {
				h.capability.quiescence = map[string]Quiescence{
					"machine-rhel-01": {State: Live, Reason: "its domain is running"},
				}
			},
			want: "check:prove nothing this removal takes back is still in use::failed",
		},
		"faulted": {
			arrange: func(h *harness) { h.capability.quiescentErr = errors.New("the probe could not run") },
			want:    "check:prove nothing this removal takes back is still in use::unknown",
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab", "machine-rhel-01")
			if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
				t.Fatal(err)
			}
			progress := &testProgress{}
			h.service.options.Progress = progress
			tc.arrange(h)
			if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
				t.Fatal("the gate admitted the removal")
			}
			if len(progress.rows) == 0 || progress.rows[len(progress.rows)-1] != tc.want {
				t.Fatalf("the gate closed with %q, want %q", progress.rows, tc.want)
			}
		})
	}
}

// A removal takes dependents before dependencies, so a gate that only checked
// each effect as it ran would delete the quiescent leaves and then stop at the
// running machine, leaving a context that can only continue a destroy it
// should never have started. Nothing is registered until every owned asset is
// proved idle.
func TestFreshDestroyRefusesLiveStateBeforeRegistering(t *testing.T) {
	h := newHarness(t, "artifact-server-lab", "machine-rhel-01")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	registered := h.workspace.area.clone()
	h.capability.quiescence = map[string]Quiescence{
		"machine-rhel-01": {State: Live, Reason: "its domain is running", Stop: "bootwright machine stop --name rhel-01"},
	}
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if code := firstCode(err); code != "lifecycle.live" {
		t.Fatalf("destroy over a running machine = %q (%v)", code, err)
	}
	reported := diagnostics.Of(err)[0]
	if !strings.Contains(reported.Message, "its domain is running") {
		t.Fatalf("the refusal did not say what is in use: %q", reported.Message)
	}
	if !strings.Contains(reported.Remediation, "bootwright machine stop --name rhel-01") {
		t.Fatalf("the refusal did not name the command that stops it: %q", reported.Remediation)
	}
	if len(h.capability.destroys) != 0 {
		t.Fatalf("a refused removal destroyed %v", h.capability.destroys)
	}
	if !maps.EqualFunc(h.workspace.area.files, registered.files, slices.Equal) {
		t.Fatal("a refused removal changed durable operation state")
	}
	// A removal inherits its apply's binding rather than acquiring one, so a
	// refusal must leave it alone: releasing it would leave a context whose
	// effects no later removal could present the material for.
	if len(h.binder.released) != 0 {
		t.Fatalf("a refused removal released %v", h.binder.released)
	}
}

// Every block is probed, so an operator is told everything to stop rather than
// discovering the next obstacle each time they repeat the command.
func TestFreshDestroyProbesEveryBlockAndNamesEachLiveOne(t *testing.T) {
	h := newHarness(t, "artifact-server-lab", "machine-rhel-01")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	h.capability.quiescence = map[string]Quiescence{
		"artifact-server-lab": {State: Live, Reason: "a fetch is in flight"},
		"machine-rhel-01":     {State: Live, Reason: "its domain is running", Stop: "bootwright machine stop --name rhel-01"},
	}
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	message := diagnostics.Of(err)[0].Message
	for _, named := range []string{"a fetch is in flight", "its domain is running"} {
		if !strings.Contains(message, named) {
			t.Fatalf("the refusal omitted %q: %q", named, message)
		}
	}
	if len(h.capability.probes) != 2 {
		t.Fatalf("probed %v, want every block", h.capability.probes)
	}
}

// An environment that cannot prove it is idle is never assumed to be.
func TestUnprovedActivityCountsAsLive(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	h.capability.quiescence = map[string]Quiescence{
		"artifact-server-lab": {State: Unproved, Reason: "its state could not be read"},
	}
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if code := firstCode(err); code != "lifecycle.live" {
		t.Fatalf("destroy over an unprovable target = %q", code)
	}
}

// A removal that supersedes a failed one is a fresh removal, so it is gated
// exactly like any other: repairing an adapter never buys a way past the gate.
func TestASupersedingRemovalIsGatedToo(t *testing.T) {
	h := newHarness(t, "artifact-server-lab", "machine-rhel-01")
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged}, {Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("the seeded apply failure did not fire")
	}
	h.capability.quiescence = map[string]Quiescence{
		"machine-rhel-01": {State: Live, Reason: "its domain is running"},
	}
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if code := firstCode(err); code != "lifecycle.live" {
		t.Fatalf("a superseding removal over a running machine = %q", code)
	}
}

// A continuation is not gated before registration: its operation already
// exists, its effects were authorized when it registered, and each one
// revalidates in the adapter that performs it.
func TestContinuedDestroyIsNotGatedBeforeRegistration(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("the seeded unproved removal did not fire")
	}
	probes := len(h.capability.probes)
	h.capability.quiescence = map[string]Quiescence{
		"artifact-server-lab": {State: Live, Reason: "a fetch is in flight"},
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("a continued removal was gated: %v", err)
	}
	if len(h.capability.probes) != probes {
		t.Fatalf("a continued removal probed %v", h.capability.probes[probes:])
	}
}

func TestContinuationRefusesDriftedInputExecutableOrHost(t *testing.T) {
	for name, corrupt := range map[string]func(*harness){
		"changed automation": func(h *harness) {
			h.service.automation = testAutomation{digest: strings.Repeat("9", 64)}
		},
		"changed input": func(h *harness) {
			h.workspace.inputs.Files = []desiredstate.SourceFile{
				desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte("kind: Environment # edited\n")),
			}
		},
		"unbound host": func(h *harness) {
			h.workspace.controller.State.Bindings = nil
		},
		"incomplete setup": func(h *harness) {
			h.workspace.controller.State.Receipt.Status = "pending"
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab")
			h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
			if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
				t.Fatal("the first attempt should have failed")
			}
			before := len(h.capability.applies)
			corrupt(h)
			_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
			if err == nil {
				t.Fatal("a drifted continuation ran")
			}
			if len(h.capability.applies) != before {
				t.Fatal("a drifted continuation performed an effect")
			}
		})
	}
}

func TestMissingImplementationRefusesTheBlock(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	h.service.capabilities = testResolver{missing: true}
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err == nil {
		t.Fatal("a missing capability still ran")
	}
}

// A required-log failure is a durable fault of the operation, not of the
// invocation that met it: a later invocation starts nothing until it has
// proved the boundary writable again, and only that proof clears the fault.
func TestRequiredLogFaultStopsTheOperation(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	operationLog := "append " + path.Join("op-"+strings.Repeat("01", 16), "logs", "operation.jsonl")
	h.workspace.area.fail[operationLog] = errors.New("no space")
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if code := firstCode(err); code != "runtime.log" {
		t.Fatalf("log fault = %q (%v)", code, err)
	}
	if len(h.capability.applies) != 0 {
		t.Fatal("an effect ran after the logging boundary failed")
	}
	if record, _ := durableOperation(t, h); !record.LogFault {
		t.Fatal("the log fault was not recorded on the operation")
	}
	_, err = h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if code := firstCode(err); code != "runtime.log" {
		t.Fatalf("an unrestored boundary = %q (%v)", code, err)
	}
	if record, _ := durableOperation(t, h); len(h.capability.applies) != 0 || !record.LogFault {
		t.Fatalf("an unrestored boundary admitted work: applies=%v fault=%t", h.capability.applies, record.LogFault)
	}
	delete(h.workspace.area.fail, operationLog)
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("a restored boundary = %+v (%v)", result, err)
	}
	if record, _ := durableOperation(t, h); record.LogFault || len(h.capability.applies) != 1 {
		t.Fatalf("restoration left fault=%t after applies=%v", record.LogFault, h.capability.applies)
	}
}

// The first required-log failure latches: it requests cancellation of the work
// in flight and records the fault, and every later failure reports that fault.
func TestALogFaultLatchesCancellationAndItsRecord(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	store := operationstore.New(h.workspace.area, (&testClock{moment: time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)}).Now)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logging := newLogBoundary(store, currentOperation(t, h), cancel)
	first := logging.fail(ctx, errors.New("no space"))
	if code := firstCode(first); code != "runtime.log" || ctx.Err() == nil {
		t.Fatalf("fault = %q, cancellation requested %t", code, ctx.Err() != nil)
	}
	if record, _ := durableOperation(t, h); !record.LogFault {
		t.Fatal("the latched fault was not recorded")
	}
	if again := logging.fail(ctx, errors.New("denied")); again != first || !logging.faulted() {
		t.Fatalf("a second failure = %v, want the latched %v", again, first)
	}
}

// A fault the latch could not write when it met it is still recorded as the
// operation settles, so the record never depends on that one write.
func TestALogFaultTheLatchCouldNotRecordIsRecordedAsTheOperationSettles(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	operation := "op-" + strings.Repeat("01", 16)
	record := "replace " + path.Join(operation, "operation.json")
	h.capability.hold = func(block string) {
		failDuring(h, path.Join(operation, "logs", "blocks", block, "attempt-000001.jsonl"))(block)
		h.workspace.area.mutex.Lock()
		defer h.workspace.area.mutex.Unlock()
		h.workspace.area.fail[record] = errors.New("no space")
	}
	// The block's settled row comes after the latch tried and before the
	// operation settles.
	h.service.options.Progress = progressFunc(func(_ context.Context, event ProgressEvent) {
		if event.Block != "" && event.Group == "" && event.Status != "running" {
			h.workspace.area.mutex.Lock()
			defer h.workspace.area.mutex.Unlock()
			delete(h.workspace.area.fail, record)
		}
	})
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); firstCode(err) != "runtime.log" {
		t.Fatalf("apply = %v", err)
	}
	if settled, _ := durableOperation(t, h); !settled.LogFault {
		t.Fatal("the fault was lost when the latch could not record it")
	}
}

// failDuring makes one path's appends fail from the moment a capability call
// reaches it, so the log it names opened cleanly and fails part way through.
func failDuring(h *harness, target string) func(string) {
	return func(string) {
		h.workspace.area.mutex.Lock()
		defer h.workspace.area.mutex.Unlock()
		h.workspace.area.fail["append "+target] = errors.New("no space")
	}
}

// An attempt log that cannot be created runs no effect, and one that fails
// after its effect ran admits nothing further, even beside a block that
// completed. A typed failure it could not log proves less than completion, so
// it is recorded unknown for an observation to resolve.
func TestAFailedAttemptLogStopsTheInvocationAndLeavesTheAttemptUnknown(t *testing.T) {
	attemptLog := path.Join("op-"+strings.Repeat("01", 16), "logs", "blocks", "alpha", "attempt-000001.jsonl")
	for name, tc := range map[string]struct {
		arrange   func(*harness)
		reported  reconciliation.Outcome
		applies   []string
		operation reconciliation.OperationState
		alpha     reconciliation.BlockState
		recorded  reconciliation.Outcome
	}{
		"never created": {
			func(h *harness) { h.workspace.area.fail["append "+attemptLog] = errors.New("no space") },
			reconciliation.OutcomeFailed, nil, reconciliation.OperationUnknown, reconciliation.BlockUnknown, reconciliation.OutcomeUnknown,
		},
		"failed after a typed failure": {
			func(h *harness) { h.capability.hold = failDuring(h, attemptLog) },
			reconciliation.OutcomeFailed, []string{"alpha"}, reconciliation.OperationUnknown, reconciliation.BlockUnknown, reconciliation.OutcomeUnknown,
		},
		"failed after a completed effect": {
			func(h *harness) { h.capability.hold = failDuring(h, attemptLog) },
			reconciliation.OutcomeChanged, []string{"alpha"}, reconciliation.OperationRunning, reconciliation.BlockDone, reconciliation.OutcomeChanged,
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("alpha"), definition("bravo")})
			h.capability.outcomeFor = map[string]Result{"alpha": {Outcome: tc.reported}}
			tc.arrange(h)
			result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
			if code := firstCode(err); code != "runtime.log" || result == nil || result.Receipt.State != string(tc.operation) {
				t.Fatalf("apply = %+v (%q, %v)", result, code, err)
			}
			if !slices.Equal(h.capability.applies, tc.applies) {
				t.Fatalf("applies = %v, want %v", h.capability.applies, tc.applies)
			}
			record, states := durableOperation(t, h)
			if !record.LogFault || states["alpha"] != tc.alpha || states["bravo"] != reconciliation.BlockPending {
				t.Fatalf("durable operation = fault %t with %v", record.LogFault, states)
			}
			attempt := string(h.workspace.area.files[path.Join(record.ID, "blocks", "alpha", "attempt-000001.json")])
			if !strings.Contains(attempt, `"outcome":"`+string(tc.recorded)+`"`) {
				t.Fatalf("attempt record = %s", attempt)
			}
		})
	}
}

// A resolution first restores the operation's logging boundary and then
// creates its own log. Either failing observes nothing and leaves the block and
// the operation as they were; the resolution identity it allocated is never
// reused.
func TestAResolutionWithoutItsLogsObservesNothing(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an unknown outcome reported success")
	}
	operation := currentOperation(t, h)
	blocks := path.Join(operation, "blocks", "artifact-server-lab")
	for _, broken := range []string{
		path.Join(operation, "logs", "operation.jsonl"),
		path.Join(operation, "logs", "blocks", "artifact-server-lab", "attempt-000001-resolution-000001.jsonl"),
	} {
		h.workspace.area.fail["append "+broken] = errors.New("no space")
		_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
		if code := firstCode(err); code != "runtime.log" {
			t.Fatalf("%s: apply = %q (%v)", broken, code, err)
		}
		delete(h.workspace.area.fail, "append "+broken)
		record, states := durableOperation(t, h)
		if len(h.capability.observes) != 0 || !record.LogFault ||
			record.State != reconciliation.OperationUnknown || states["artifact-server-lab"] != reconciliation.BlockUnknown {
			t.Fatalf("%s: observes=%v fault=%t state=%s blocks=%v", broken, h.capability.observes, record.LogFault, record.State, states)
		}
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("a restored resolution = %v", err)
	}
	if _, found := h.workspace.area.files[path.Join(blocks, "attempt-000001-resolution-000002.json")]; !found {
		t.Fatal("the resolution reused the identity a failed one allocated")
	}
	if record, _ := durableOperation(t, h); record.LogFault || record.State != reconciliation.OperationDone {
		t.Fatalf("restored operation = %s, fault %t", record.State, record.LogFault)
	}
}

// A removal's resolution that proves its block still records the transition
// when its log fails, but the fault refuses the removal before it registers,
// and a later removal restores the boundary before it proceeds.
func TestALogFaultDuringARemovalsResolutionRefusesTheRemoval(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an unknown outcome reported success")
	}
	applied := currentOperation(t, h)
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	h.capability.observeHold = failDuring(h, path.Join(applied, "logs", "blocks", "artifact-server-lab", "attempt-000001-resolution-000001.jsonl"))
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if code := firstCode(err); code != "runtime.log" {
		t.Fatalf("destroy = %q (%v)", code, err)
	}
	record, states := durableOperation(t, h)
	if record.ID != applied || !record.LogFault || states["artifact-server-lab"] != reconciliation.BlockDone || len(h.capability.destroys) != 0 {
		t.Fatalf("after the fault: current %s fault %t blocks %v destroys %v", record.ID, record.LogFault, states, h.capability.destroys)
	}
	h.capability.observeHold = nil
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("a restored removal = %v", err)
	}
	if !slices.Equal(h.capability.destroys, []string{"artifact-server-lab"}) {
		t.Fatalf("destroys = %v", h.capability.destroys)
	}
	store := operationstore.New(h.workspace.area, func() time.Time { return time.Unix(0, 0) })
	if restored, err := store.ReadOperation(context.Background(), applied); err != nil || restored.LogFault {
		t.Fatalf("the replaced operation = %+v (%v)", restored, err)
	}
}

// A completed removal whose last log write failed holds the fault too: it
// still releases what it no longer needs, and the next apply restores its
// boundary before it registers anything.
func TestAFreshApplyRestoresTheBoundaryACompletedRemovalFaulted(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	removal := "op-" + strings.Repeat("02", 16)
	h.capability.destroyHold = failDuring(h, path.Join(removal, "logs", "blocks", "artifact-server-lab", "attempt-000001.jsonl"))
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if code := firstCode(err); code != "runtime.log" || result == nil || result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("destroy = %+v (%q, %v)", result, code, err)
	}
	if record, _ := durableOperation(t, h); record.ID != removal || !record.LogFault {
		t.Fatalf("completed removal = %s, fault %t", record.ID, record.LogFault)
	}
	if !slices.Equal(h.binder.released, []string{"bind-1"}) {
		t.Fatalf("released bindings = %v", h.binder.released)
	}
	operationLog := "append " + path.Join(removal, "logs", "operation.jsonl")
	h.workspace.area.fail[operationLog] = errors.New("no space")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); firstCode(err) != "runtime.log" {
		t.Fatalf("apply over an unrestored removal = %v", err)
	}
	if currentOperation(t, h) != removal || len(h.capability.applies) != 1 {
		t.Fatalf("an unrestored boundary registered %s after applies %v", currentOperation(t, h), h.capability.applies)
	}
	delete(h.workspace.area.fail, operationLog)
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("apply over a restored removal = %v", err)
	}
	store := operationstore.New(h.workspace.area, func() time.Time { return time.Unix(0, 0) })
	if restored, err := store.ReadOperation(context.Background(), removal); err != nil || restored.LogFault {
		t.Fatalf("restored removal = %+v (%v)", restored, err)
	}
}

// A removal restores the boundary of the operation it replaces before it
// proves, probes or registers anything, so a restoration that fails starts
// nothing and leaves the fault where it was, even when every effect of that
// operation is already proved.
func TestARemovalOverAnUnrestoredBoundaryStartsNothing(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	applied := currentOperation(t, h)
	store := operationstore.New(h.workspace.area, (&testClock{moment: time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)}).Now)
	if err := markLogFault(context.Background(), store, applied); err != nil {
		t.Fatal(err)
	}
	operationLog := "append " + path.Join(applied, "logs", "operation.jsonl")
	h.workspace.area.fail[operationLog] = errors.New("no space")
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if code := firstCode(err); code != "runtime.log" {
		t.Fatalf("destroy = %q (%v)", code, err)
	}
	record, _ := durableOperation(t, h)
	if record.ID != applied || !record.LogFault || len(h.capability.probes) != 0 || len(h.capability.destroys) != 0 {
		t.Fatalf("an unrestored boundary admitted work: current %s fault %t probes %v destroys %v",
			record.ID, record.LogFault, h.capability.probes, h.capability.destroys)
	}
	delete(h.workspace.area.fail, operationLog)
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("a restored removal = %v", err)
	}
	if !slices.Equal(h.capability.destroys, []string{"artifact-server-lab"}) {
		t.Fatalf("destroys = %v", h.capability.destroys)
	}
}

// Restoration clears the fault only by a durable write, so a clear that fails
// starts nothing, even for an operation that is already running and so needs
// no other write before its next block.
func TestARestorationWhoseClearFailsStartsNothing(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("alpha"), definition("bravo")})
	h.capability.outcomeFor = map[string]Result{"alpha": {Outcome: reconciliation.OutcomeChanged}}
	operation := "op-" + strings.Repeat("01", 16)
	attemptLog := path.Join(operation, "logs", "blocks", "alpha", "attempt-000001.jsonl")
	h.capability.hold = failDuring(h, attemptLog)
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); firstCode(err) != "runtime.log" {
		t.Fatalf("apply = %v", err)
	}
	if record, _ := durableOperation(t, h); record.State != reconciliation.OperationRunning || !record.LogFault {
		t.Fatalf("faulted operation = %s, fault %t", record.State, record.LogFault)
	}
	h.capability.hold = nil
	delete(h.workspace.area.fail, "append "+attemptLog)
	h.workspace.area.fail["replace "+path.Join(operation, "operation.json")] = errors.New("no space")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("a restoration that could not clear the fault reported success")
	}
	if record, _ := durableOperation(t, h); !record.LogFault || !slices.Equal(h.capability.applies, []string{"alpha"}) {
		t.Fatalf("an uncleared fault admitted work: fault %t, applies %v", record.LogFault, h.capability.applies)
	}
}

// loggingCapability records through the attempt's own log before it applies,
// exactly as the adapter records each group it settles.
type loggingCapability struct {
	*testCapability
	log func(context.Context, Execution)
}

func (c *loggingCapability) Apply(ctx context.Context, execution Execution) (Result, error) {
	c.log(ctx, execution)
	return c.testCapability.Apply(ctx, execution)
}

// A record the adapter logs through its execution is a required-log write like
// any other: one the attempt log cannot keep latches the fault and cancels the
// run that made it, rather than letting it go on changing the host unlogged.
func TestALogRecordTheAttemptLogCannotKeepCancelsTheRun(t *testing.T) {
	const block = "artifact-server-lab"
	h := newHarness(t, block)
	canceled := false
	h.service.capabilities = testResolver{capability: &loggingCapability{testCapability: h.capability, log: func(ctx context.Context, execution Execution) {
		err := execution.Log(ctx, operationstore.LogRecord{Event: "group", Group: "pull-image", Detail: "ok"})
		canceled = err != nil && ctx.Err() != nil
	}}}
	attemptLog := path.Join("op-"+strings.Repeat("01", 16), "logs", "blocks", block, "attempt-000001.jsonl")
	// The block's running row follows the attempt log's opening record.
	h.service.options.Progress = progressFunc(func(_ context.Context, event ProgressEvent) {
		if event.Block == block && event.Group == "" && event.Status == "running" {
			failDuring(h, attemptLog)(block)
		}
	})
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if code := firstCode(err); code != "runtime.log" || !canceled {
		t.Fatalf("apply = %q (%v), run canceled %t", code, err, canceled)
	}
	if record, _ := durableOperation(t, h); !record.LogFault {
		t.Fatal("the log fault was not recorded on the operation")
	}
}

// A log that outgrew its bound is finalized with an explicit truncation marker,
// so a reader never mistakes it for a complete one, and a marker that cannot be
// written is a log fault like any other write.
func TestALogThatCannotBeFinalizedIsALogFault(t *testing.T) {
	const block = "artifact-server-lab"
	h := newHarness(t, block)
	detail := strings.Repeat("x", 512)
	h.service.capabilities = testResolver{capability: &loggingCapability{testCapability: h.capability, log: func(ctx context.Context, execution Execution) {
		// Every record exceeds a kilobyte, so this many outgrow the bound.
		for range operationstore.MaxLogBytes/1024 + 1 {
			_ = execution.Log(ctx, operationstore.LogRecord{Event: "group", Group: detail, Detail: detail})
		}
	}}}
	attemptLog := path.Join("op-"+strings.Repeat("01", 16), "logs", "blocks", block, "attempt-000001.jsonl")
	// The block's settled row follows its outcome record and precedes the
	// finalize, so only the truncation marker meets the failure.
	h.service.options.Progress = progressFunc(func(_ context.Context, event ProgressEvent) {
		if event.Block == block && event.Group == "" && event.Status != "running" {
			failDuring(h, attemptLog)(block)
		}
	})
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if code := firstCode(err); code != "runtime.log" {
		t.Fatalf("apply = %q (%v)", code, err)
	}
	if record, _ := durableOperation(t, h); !record.LogFault {
		t.Fatal("a log that could not be finalized recorded no fault")
	}
}

// A log is recorded rather than performed, so an interrupt reaches none of its
// writes and is never a log fault.
func TestAnInterruptIsNotALogFault(t *testing.T) {
	store := operationstore.New(newArea(), (&testClock{moment: time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)}).Now)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	operation := "op-" + strings.Repeat("01", 16)
	logging := newLogBoundary(store, operation, nil)
	log, err := logging.open(ctx, operationstore.OperationLogPath(operation))
	if err != nil {
		t.Fatalf("an interrupted open = %v", err)
	}
	if err := logging.append(ctx, log, operationstore.LogRecord{Event: "outcome", Detail: "canceled"}); err != nil {
		t.Fatalf("an interrupted append = %v", err)
	}
	logging.close(ctx, log)
	if logging.faulted() {
		t.Fatalf("an interrupt latched %v", logging.err())
	}
}

// durableOperation reads the context's current operation record as the store
// holds it, including what no result reports, such as its log fault.
func durableOperation(t *testing.T, h *harness) (operationstore.Operation, map[string]reconciliation.BlockState) {
	t.Helper()
	ctx := context.Background()
	store := operationstore.New(h.workspace.area, func() time.Time { return time.Unix(0, 0) })
	index, err := store.Index(ctx)
	if err != nil || index.Current == "" {
		t.Fatalf("current operation = %q (%v)", index.Current, err)
	}
	operation, err := store.ReadOperation(ctx, index.Current)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := store.ReadPlan(ctx, operation.ID)
	if err != nil {
		t.Fatal(err)
	}
	states, err := store.BlockStates(ctx, operation.ID, plan)
	if err != nil {
		t.Fatal(err)
	}
	return operation, states
}

// A resolution that cannot start performs no observation, so it proves nothing
// and moves nothing: an operation an executor died in stays running with its
// running block, whether a continuation or a removal tried and whatever step
// stopped it. Only a resolution log that cannot be created is a log fault.
func TestAResolutionThatCannotStartLeavesTheOperationUnchanged(t *testing.T) {
	const block = "artifact-server-lab"
	repeats := map[string]func(*harness) error{
		"continuation": func(h *harness) error {
			_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
			return err
		},
		"removal": func(h *harness) error {
			_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
			return err
		},
	}
	for name, tc := range map[string]struct {
		arrange  func(t *testing.T, h *harness, operation string)
		logFault bool
	}{
		"its identity cannot be allocated": {arrange: func(_ *testing.T, h *harness, operation string) {
			h.workspace.area.fail["write "+path.Join(operation, "blocks", block, "attempt-000001-resolution-000001.json")] = errors.New("no space")
		}},
		"its log cannot be created": {arrange: func(_ *testing.T, h *harness, operation string) {
			h.workspace.area.fail["append "+path.Join(operation, "logs", "blocks", block, "attempt-000001-resolution-000001.jsonl")] = errors.New("no space")
		}, logFault: true},
		"its capability is missing": {arrange: func(_ *testing.T, h *harness, _ string) {
			h.service.capabilities = testResolver{missing: true}
		}},
		"its block records no attempt": {arrange: func(t *testing.T, h *harness, operation string) {
			target := path.Join(operation, "blocks", block, "state.json")
			h.workspace.area.mutex.Lock()
			defer h.workspace.area.mutex.Unlock()
			record := string(h.workspace.area.files[target])
			if !strings.Contains(record, `"attempts":1`) {
				t.Fatalf("block record = %s", record)
			}
			h.workspace.area.files[target] = []byte(strings.Replace(record, `"attempts":1`, `"attempts":0`, 1))
		}},
	} {
		for verb, repeat := range repeats {
			t.Run(name+"/"+verb, func(t *testing.T) {
				h := newHarness(t, block)
				h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
				if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
					t.Fatal("an unknown outcome reported success")
				}
				leaveExecutorDead(t, h, block)
				tc.arrange(t, h, currentOperation(t, h))
				err := repeat(h)
				if err == nil {
					t.Fatal("a resolution that could not start reported success")
				}
				if len(h.capability.observes) != 0 {
					t.Fatalf("observations = %v", h.capability.observes)
				}
				record, states := durableOperation(t, h)
				if record.State != reconciliation.OperationRunning || states[block] != reconciliation.BlockRunning {
					t.Fatalf("durable state = %s with %v, want it unchanged", record.State, states)
				}
				if record.LogFault != tc.logFault {
					t.Fatalf("log fault = %t, want %t", record.LogFault, tc.logFault)
				}
				if code := firstCode(err); tc.logFault && code != "runtime.log" {
					t.Fatalf("refusal = %q (%v)", code, err)
				}
			})
		}
	}
}

// An apply that started nothing owns nothing, so removing it takes nothing
// back: the removal completes without an effect or a probe and leaves the
// context at rest, from which a fresh apply starts.
func TestADestroyOverAnApplyThatStartedNothingCompletes(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	open := h.workspace.controller.OpenBundle
	h.workspace.controller.OpenBundle = func(context.Context, string) (prerequisites.BundleArea, error) {
		return nil, errors.New("bundle unavailable")
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an apply without its bundle reported success")
	}
	applied := currentOperation(t, h)
	if _, states := durableOperation(t, h); states["artifact-server-lab"] != reconciliation.BlockPending {
		t.Fatalf("the apply started %v", states)
	}
	h.workspace.controller.OpenBundle = open
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatalf("a removal of nothing refused: %+v", diagnostics.Of(err))
	}
	if result.Receipt.State != string(reconciliation.OperationDone) || result.Receipt.Operation == applied || len(result.Blocks) != 0 {
		t.Fatalf("receipt = %+v over %d blocks", result.Receipt, len(result.Blocks))
	}
	if len(h.capability.destroys) != 0 || len(h.capability.probes) != 0 || len(h.capability.observes) != 0 {
		t.Fatalf("a removal of nothing reached the host: destroys=%v probes=%v observes=%v",
			h.capability.destroys, h.capability.probes, h.capability.observes)
	}
	pristine, _ := reconciliation.PristineEvidence().Bytes()
	if !slices.Equal(h.workspace.evidence, pristine) || !slices.Equal(h.binder.released, []string{"bind-1"}) {
		t.Fatalf("evidence = %q, released bindings = %v", h.workspace.evidence, h.binder.released)
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatalf("the context was not left at rest: %v", err)
	}
	if !slices.Equal(h.capability.applies, []string{"artifact-server-lab"}) {
		t.Fatalf("applies = %v", h.capability.applies)
	}
}

// Only an apply still running or paused may have started nothing, and a failed
// removal left nothing to remove is finalized first. Any other operation whose
// records leave nothing to remove contradicts them, so its removal refuses
// before it registers, reaches a host, or releases the material the effects
// still on that host need.
func TestARemovalOverRecordsThatContradictThemselvesRefuses(t *testing.T) {
	const block = "artifact-server-lab"
	for name, arrange := range map[string]func(*testing.T, *harness){
		"a completed apply without its block record": func(t *testing.T, h *harness) {
			if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
				t.Fatal(err)
			}
			h.workspace.area.mutex.Lock()
			defer h.workspace.area.mutex.Unlock()
			delete(h.workspace.area.files, path.Join("op-"+strings.Repeat("01", 16), "blocks", block, "state.json"))
		},
		"a failed apply that records no started block": func(t *testing.T, h *harness) {
			open := h.workspace.controller.OpenBundle
			h.workspace.controller.OpenBundle = func(context.Context, string) (prerequisites.BundleArea, error) {
				return nil, errors.New("bundle unavailable")
			}
			if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
				t.Fatal("an apply without its bundle reported success")
			}
			h.workspace.controller.OpenBundle = open
			rewriteState(t, h, path.Join(currentOperation(t, h), "operation.json"), string(reconciliation.OperationFailed))
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, block)
			arrange(t, h)
			current := currentOperation(t, h)
			h.capability.destroys, h.capability.probes, h.capability.observes = nil, nil, nil
			_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
			reported := diagnostics.Of(err)
			if len(reported) == 0 || reported[0].Code != "lifecycle.state" || !strings.Contains(reported[0].Message, "records no block") {
				t.Fatalf("destroy = %+v (%v)", reported, err)
			}
			if currentOperation(t, h) != current || len(h.binder.released) != 0 {
				t.Fatalf("the refusal registered %s or released %v", currentOperation(t, h), h.binder.released)
			}
			if len(h.capability.destroys) != 0 || len(h.capability.probes) != 0 || len(h.capability.observes) != 0 {
				t.Fatalf("the refusal reached the host: destroys=%v probes=%v observes=%v",
					h.capability.destroys, h.capability.probes, h.capability.observes)
			}
		})
	}
}

func TestPlanPreviewsWithoutWritingAnything(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	result, err := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab"})
	if err != nil || result.Receipt.State != "preview" || result.Receipt.Next != "apply" || result.Receipt.Operation != "none" {
		t.Fatalf("plan = %+v (%v)", result, err)
	}
	if len(result.Steps) != 1 || result.Steps[0].ID != "artifact-server-lab" {
		t.Fatalf("steps = %+v", result.Steps)
	}
	if len(h.workspace.area.files) != 0 || h.workspace.mutations != 0 {
		t.Fatal("plan wrote durable state")
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	after, err := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab"})
	if err != nil || after.Receipt.Next != "destroy" || after.Verb != "destroy" {
		t.Fatalf("plan after apply = %+v (%v)", after, err)
	}
}

func TestStatusReportsDurableStateWithoutProbing(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	before, err := h.service.Status(context.Background(), StatusRequest{ContextName: "lab"})
	if err != nil || before.Lifecycle != nil {
		t.Fatalf("status before apply = %+v (%v)", before, err)
	}
	if before.Context != (ContextIdentity{Name: testContextName, Revision: testRevision, Mode: "ready"}) {
		t.Fatalf("status context = %+v, want the view's identity", before.Context)
	}
	if len(before.SetupChecks) != 2 || before.SetupChecks[0].Status != "ready" {
		t.Fatalf("setup checks = %+v", before.SetupChecks)
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	after, err := h.service.Status(context.Background(), StatusRequest{ContextName: "lab"})
	if err != nil || after.Lifecycle == nil || after.Lifecycle.State != "done" || after.Lifecycle.Next != "none" {
		t.Fatalf("status after apply = %+v (%v)", after, err)
	}
	// A finished context is offered no command, because every one it could be
	// offered would undo what it just proved.
	if len(after.NextSteps) != 0 {
		t.Fatalf("a completed apply still offers steps = %v", after.NextSteps)
	}
	if len(after.Lifecycle.Blocks) != 1 || after.Lifecycle.Blocks[0].State != "done" {
		t.Fatalf("status blocks = %+v", after.Lifecycle.Blocks)
	}
}

// A structured result names each log relative to the state root, so the
// operation area's own place under it leads every path, in the apply's result
// and in status alike.
func TestAnOperationNamesItsLogsRelativeToTheStateRoot(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	operations := "contexts/" + testContextName + "/state/operations/" + result.Receipt.Operation + "/logs/"
	if len(result.Logs) < 2 || result.Logs[0] != operations+"operation.jsonl" {
		t.Fatalf("the apply named its logs %v", result.Logs)
	}
	for _, named := range result.Logs {
		if !strings.HasPrefix(named, operations) {
			t.Fatalf("the apply named %q outside its operation's logs under the state root", named)
		}
	}
	status, err := h.service.Status(context.Background(), StatusRequest{ContextName: "lab"})
	if err != nil || status.Lifecycle == nil || !slices.Equal(status.Lifecycle.Logs, result.Logs) {
		t.Fatalf("status named %+v, not the apply's %v (%v)", status.Lifecycle, result.Logs, err)
	}
}

// Status reports each setup check in the controller's readiness vocabulary:
// what stored evidence proves is ready, and anything it does not prove, a
// missing record or an incomplete receipt alike, is not-ready. A binding the
// first apply publishes is pending while the context holds no operation, and
// not-ready once one exists without it.
func TestStatusSetupChecksUseTheReadinessVocabulary(t *testing.T) {
	for name, test := range map[string]struct {
		applied         bool
		prepare         func(*prerequisites.StorageView)
		binding, bundle string
	}{
		"a bound context with a complete receipt": {prepare: func(*prerequisites.StorageView) {}, binding: "ready", bundle: "ready"},
		"an unbound context that holds no operation": {
			prepare: func(view *prerequisites.StorageView) { view.State.Bindings = nil },
			binding: "pending", bundle: "ready",
		},
		"an unbound context holding an operation": {
			applied: true,
			prepare: func(view *prerequisites.StorageView) { view.State.Bindings = nil },
			binding: "not-ready", bundle: "ready",
		},
		"no controller record, so no bundle and no binding yet": {
			prepare: func(view *prerequisites.StorageView) { view.Exists, view.Initialized = false, false },
			binding: "pending", bundle: "not-ready",
		},
		"an incomplete receipt": {
			prepare: func(view *prerequisites.StorageView) { view.State.Receipt.Status = "pending" },
			binding: "ready", bundle: "not-ready",
		},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, "artifact-server-lab")
			if test.applied {
				completeApply(t, h)
			}
			test.prepare(&h.workspace.controller)
			status, err := h.service.Status(context.Background(), StatusRequest{ContextName: "lab"})
			if err != nil {
				t.Fatal(err)
			}
			want := []SetupCheck{{ID: "controller-binding", Status: test.binding}, {ID: "execution-bundle", Status: test.bundle}}
			if !slices.Equal(status.SetupChecks, want) {
				t.Fatalf("setup checks = %+v, want %+v", status.SetupChecks, want)
			}
		})
	}
}

// A block retried by another invocation that failed again reads the state it
// found, so status reports the attempts its record counts.
func TestStatusReportsEachBlocksAttempts(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.capability.outcomeFor = map[string]Result{"artifact-server-lab": {Outcome: reconciliation.OutcomeFailed}}
	for attempt := range 2 {
		if result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil || result.Receipt.State != "failed" {
			t.Fatalf("apply %d = %+v (%v)", attempt+1, result, err)
		}
	}
	status, err := h.service.Status(context.Background(), StatusRequest{ContextName: "lab"})
	if err != nil || status.Lifecycle == nil {
		t.Fatalf("status = %+v (%v)", status, err)
	}
	blocks := status.Lifecycle.Blocks
	if len(blocks) != 1 || blocks[0].State != "failed" || blocks[0].Attempts != 2 {
		t.Fatalf("status blocks = %+v, want one failed block attempted twice", blocks)
	}
}

func TestEmptyCurrentSelectionRefuses(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.service.options.Selection = func(context.Context) (string, error) { return "", nil }
	if _, err := h.service.Plan(context.Background(), PlanRequest{}); firstCode(err) != "context.state" {
		t.Fatalf("empty selection = %v", err)
	}
}

func TestCancellationBeforeAnyEffectRegistersNothing(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err == nil {
		t.Fatalf("a canceled apply reported success: %+v", result)
	}
	if len(h.capability.applies) != 0 || len(h.workspace.area.files) != 0 {
		t.Fatalf("a canceled apply reached a host: applies=%v files=%d", h.capability.applies, len(h.workspace.area.files))
	}
}

// An interrupt arrives while a block is in flight, which is the only moment it
// can strand an effect. The records that say so are written under a boundary
// the interrupt does not reach, because a durable `running` block stays
// unproved until a later observation resolves it.
func TestAnInterruptedBlockIsRecordedUnknown(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	ctx, cancel := context.WithCancel(context.Background())
	h.capability.hold = func(string) { cancel() }
	h.capability.errorFor = map[string]error{"artifact-server-lab": context.Canceled}
	result, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err == nil {
		t.Fatalf("an interrupted apply reported success: %+v", result)
	}
	if result == nil || result.Receipt.State != "unknown" || result.Receipt.Next != "resolve" {
		t.Fatalf("receipt = %+v", result)
	}
	// The interrupt reaches no log write, so it is never a log fault.
	if record, _ := durableOperation(t, h); record.LogFault || firstCode(err) == "runtime.log" {
		t.Fatalf("an interrupt read as a log fault: fault %t (%v)", record.LogFault, err)
	}
	status, err := h.service.Status(context.Background(), StatusRequest{ContextName: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	if status.Lifecycle == nil || status.Lifecycle.State != "unknown" || status.Lifecycle.Next != "resolve" {
		t.Fatalf("durable operation = %+v", status.Lifecycle)
	}
	if len(status.Lifecycle.Blocks) != 1 || status.Lifecycle.Blocks[0].State != "unknown" {
		t.Fatalf("durable block = %+v", status.Lifecycle.Blocks)
	}
	unknown, err := reconciliation.EvidenceFor(reconciliation.Apply, reconciliation.OperationUnknown)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := unknown.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(h.workspace.evidence, expected) {
		t.Fatalf("evidence = %q, want %q", h.workspace.evidence, expected)
	}
	h.capability.hold, h.capability.errorFor = nil, nil
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted}}
	resolved, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil || resolved.Receipt.State != "done" {
		t.Fatalf("resolution = %+v (%v)", resolved, err)
	}
	if !slices.Equal(h.capability.observes, []string{"artifact-server-lab"}) {
		t.Fatalf("observations = %v", h.capability.observes)
	}
}

// Two blocks in flight both lose their outcome to one interrupt, so both are
// recorded rather than only the one whose goroutine noticed first.
func TestEveryInterruptedBlockIsRecordedUnknown(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("alpha"), definition("bravo")})
	h.service.options.Concurrency = 2
	ctx, cancel := context.WithCancel(context.Background())
	var started sync.WaitGroup
	started.Add(2)
	h.capability.hold = func(string) {
		started.Done()
		started.Wait()
		cancel()
	}
	h.capability.errorFor = map[string]error{"alpha": context.Canceled, "bravo": context.Canceled}
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an interrupted apply reported success")
	}
	status, err := h.service.Status(context.Background(), StatusRequest{ContextName: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	if status.Lifecycle == nil || status.Lifecycle.State != "unknown" {
		t.Fatalf("durable operation = %+v", status.Lifecycle)
	}
	for _, block := range status.Lifecycle.Blocks {
		if block.State != "unknown" {
			t.Fatalf("durable blocks = %+v", status.Lifecycle.Blocks)
		}
	}
}

// An observation interrupted part way leaves the block unknown, which is what
// it already was; what must not survive is a resolution record still claiming
// to be running, because the next resolution is allocated against it.
func TestAnInterruptedResolutionIsRecordedUnknown(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.capability.outcomeFor = map[string]Result{"artifact-server-lab": {Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an unknown outcome reported success")
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.capability.observeHold = func(string) { cancel() }
	h.capability.observeErr = context.Canceled
	_, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err == nil {
		t.Fatal("an interrupted resolution reported success")
	}
	if record, _ := durableOperation(t, h); record.LogFault || firstCode(err) == "runtime.log" {
		t.Fatalf("an interrupt read as a log fault: fault %t (%v)", record.LogFault, err)
	}
	record := string(h.workspace.area.files[resolutionRecord(h, "artifact-server-lab")])
	if !strings.Contains(record, `"phase":"observed"`) || !strings.Contains(record, `"effect":"unknown"`) {
		t.Fatalf("resolution record = %q", record)
	}
}

// resolutionRecord names the durable resolution record of a block's attempt,
// which is where an interrupted observation is recorded. The attempt's log
// sits under the same block name, so the record is identified by its own
// subtree and extension rather than by the first match.
func resolutionRecord(h *harness, block string) string {
	var found []string
	for name := range h.workspace.area.files {
		if strings.Contains(name, "/logs/") || !strings.HasSuffix(name, ".json") {
			continue
		}
		if strings.Contains(name, "/blocks/"+block+"/attempt-") && strings.Contains(name, "-resolution-") {
			found = append(found, name)
		}
	}
	slices.Sort(found)
	if len(found) == 0 {
		return ""
	}
	return found[0]
}

func TestBlocksExecuteInFrozenOrderAndSkipDoneWork(t *testing.T) {
	h := newHarness(t, "alpha", "bravo")
	h.capability.outcomes = []Result{
		{Outcome: reconciliation.OutcomeChanged, Evidence: json.RawMessage(`{"ok":true}`)},
		{Outcome: reconciliation.OutcomeFailed},
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("the second block should have failed")
	}
	if !slices.Equal(h.capability.applies, []string{"alpha", "bravo"}) {
		t.Fatalf("execution order = %v", h.capability.applies)
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(h.capability.applies, []string{"alpha", "bravo", "bravo"}) {
		t.Fatalf("continuation re-ran a completed block: %v", h.capability.applies)
	}
}

func TestOperationRecordsSurviveAnInterruptedRegistration(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.workspace.area.fail["replace index.json"] = errors.New("interrupted")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an interrupted registration reported success")
	}
	if len(h.capability.applies) != 0 {
		t.Fatal("an effect ran before registration committed")
	}
	delete(h.workspace.area.fail, "replace index.json")
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil || result.Receipt.State != "done" {
		t.Fatalf("recovery = %+v (%v)", result, err)
	}
}

func borrowedOptions() machine.SSHOptions { return machine.SSHOptions{User: "operator"} }

func firstCode(err error) string {
	reported := diagnostics.Of(err)
	if len(reported) == 0 {
		return ""
	}
	return reported[0].Code
}

func stagedDefinition(id string, stage reconciliation.Stage, dependencies ...string) reconciliation.BlockDefinition {
	block := definition(id)
	block.Stage = stage
	slices.Sort(dependencies)
	block.Dependencies = dependencies
	return block
}

// nestedDefinitions are the shape that makes stages a graph rather than
// strata: the KubeVirt substrate of a hub cluster cannot exist before the
// hosting cluster's virtualization add-on has been installed.
func nestedDefinitions() []reconciliation.BlockDefinition {
	return []reconciliation.BlockDefinition{
		stagedDefinition("artifacts", reconciliation.StageInfraComponents),
		stagedDefinition("provider-metal", reconciliation.StageSubstrates),
		stagedDefinition("host-node", reconciliation.StageMachines, "provider-metal"),
		stagedDefinition("host-cluster", reconciliation.StageClusters, "host-node"),
		stagedDefinition("host-virtualization", reconciliation.StageAddOns, "host-cluster"),
		stagedDefinition("provider-kubevirt", reconciliation.StageSubstrates, "host-virtualization"),
		stagedDefinition("hub-node", reconciliation.StageMachines, "provider-kubevirt"),
	}
}

func TestStagedApplyPausesAtTheStageBoundary(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	result, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"infra-components"},
	})
	if err != nil {
		t.Fatalf("a stage boundary reported a failure: %v", err)
	}
	if result.Receipt.State != string(reconciliation.OperationPaused) || result.Receipt.Next != "continue-apply" {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
	if !slices.Equal(h.capability.applies, []string{"artifacts"}) {
		t.Fatalf("applied blocks = %v", h.capability.applies)
	}
	paused, err := reconciliation.EvidenceFor(reconciliation.Apply, reconciliation.OperationPaused)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := paused.Bytes()
	if string(h.workspace.evidence) != string(want) {
		t.Fatalf("evidence = %q, want %q", h.workspace.evidence, want)
	}
}

// A stage selection gates which blocks start; it never narrows the frozen plan,
// so a block whose dependency belongs to an unselected stage simply waits.
func TestStagedApplyDefersBlocksBehindUnselectedStages(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	if _, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"substrates"},
	}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(h.capability.applies, []string{"provider-metal"}) {
		t.Fatalf("applied blocks = %v", h.capability.applies)
	}
	presented := h.presenter.presented[0]
	if len(presented.Steps) != len(nestedDefinitions()) {
		t.Fatalf("the presented plan was narrowed to %d steps", len(presented.Steps))
	}
	marks := map[string]string{}
	waits := map[string]string{}
	for _, step := range presented.Steps {
		marks[step.ID], waits[step.ID] = step.Selection, step.WaitsOn
	}
	if marks["provider-metal"] != StepStart || marks["artifacts"] != StepNotSelected {
		t.Fatalf("selection markers = %v", marks)
	}
	if marks["provider-kubevirt"] != StepWaiting || waits["provider-kubevirt"] != "host-virtualization" {
		t.Fatalf("the nested substrate is not deferred behind its add-on: %v %v", marks, waits)
	}
	if presented.Startable != 1 || presented.Deferred != len(nestedDefinitions())-1 {
		t.Fatalf("startable = %d, deferred = %d", presented.Startable, presented.Deferred)
	}
}

func TestContinuationAcceptsAWiderStageSetAndCompletes(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	if _, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"infra-components"},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.State != string(reconciliation.OperationDone) || result.Receipt.Next != "none" {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
	if !slices.Equal(h.capability.applies, []string{
		"artifacts", "provider-metal", "host-node", "host-cluster", "host-virtualization", "provider-kubevirt", "hub-node",
	}) {
		t.Fatalf("applied blocks = %v", h.capability.applies)
	}
}

// A pause owns exactly what it completed, so its removal covers those blocks
// and nothing the operation never started.
func TestDestroyFromAPausedApplyRemovesOnlyDoneBlocks(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	if _, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"infra-components", "substrates"},
	}); err != nil {
		t.Fatal(err)
	}
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.State != string(reconciliation.OperationDone) || result.Receipt.Next != "none" {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
	if !slices.Equal(h.capability.destroys, []string{"artifacts", "provider-metal"}) {
		t.Fatalf("destroyed blocks = %v", h.capability.destroys)
	}
}

// A failed block was permitted to change its target, so the context owns it.
// A removal covers every block the apply started and nothing it never did.
func TestDestroyOverAFailedApplyRemovesEveryStartedBlock(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	h.capability.outcomes = []Result{
		{Outcome: reconciliation.OutcomeChanged},
		{Outcome: reconciliation.OutcomeChanged},
		{Outcome: reconciliation.OutcomeFailed},
	}
	applied, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err == nil || applied.Receipt.State != string(reconciliation.OperationFailed) {
		t.Fatalf("apply = %+v (%v)", applied.Receipt, err)
	}
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatalf("a failed apply refused its removal: %v", err)
	}
	if result.Receipt.State != string(reconciliation.OperationDone) || result.Receipt.Next != "none" {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
	if !slices.Equal(h.capability.destroys, []string{"artifacts", "host-node", "provider-metal"}) {
		t.Fatalf("destroyed blocks = %v, applied = %v", h.capability.destroys, h.capability.applies)
	}
	pristine, _ := reconciliation.PristineEvidence().Bytes()
	if string(h.workspace.evidence) != string(pristine) {
		t.Fatalf("evidence = %q", h.workspace.evidence)
	}
}

// Repairing the adapter that failed an operation changes the automation its
// continuation is frozen to. The removal is the road out of that, so the
// refusal names it and the context returns to rest under the new executable.
func TestRepairedAutomationLeavesAFailedApplyDestroyable(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("a failed block reported success")
	}
	h.service.automation = testAutomation{digest: strings.Repeat("9", 64)}
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	remediation := ""
	for _, reported := range diagnostics.Of(err) {
		if reported.Code == "lifecycle.state" {
			remediation = reported.Remediation
		}
	}
	if !strings.Contains(remediation, "destroy") {
		t.Fatalf("the drift refusal does not name the road out: %q", remediation)
	}
	removed, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatalf("a repaired executable could not remove what it owns: %v", err)
	}
	if removed.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("removal receipt = %+v", removed.Receipt)
	}
	if !slices.Equal(h.capability.destroys, []string{"artifact-server-lab"}) {
		t.Fatalf("destroyed blocks = %v", h.capability.destroys)
	}
	fresh, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil || fresh.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("the context did not return to rest: %+v (%v)", fresh.Receipt, err)
	}
}

// A removal can fail the same way, so a fresh one supersedes it over exactly
// what it never proved gone. Both removals inherit the apply's own binding
// rather than minting one, so the replacement releases exactly that.
func TestDestroyOverAFailedDestroyCoversOnlyWhatRemains(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	h.capability.secrets = []string{"lab-bmc-credentials"}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeChanged}, {Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("a failed removal reported success")
	}
	h.capability.destroys = nil
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatalf("a failed removal refused its replacement: %v", err)
	}
	if result.Receipt.State != string(reconciliation.OperationDone) || result.Receipt.Next != "none" {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
	want := []string{"hub-node", "provider-kubevirt", "host-virtualization", "host-cluster", "host-node", "provider-metal"}
	if !slices.Equal(h.capability.destroys, want) {
		t.Fatalf("destroyed blocks = %v", h.capability.destroys)
	}
	if !slices.Equal(h.binder.released, []string{"bind-1"}) {
		t.Fatalf("released bindings = %v", h.binder.released)
	}
}

// An effect no observation can prove still admits no removal, because nothing
// says what it owns. The removal proves what it can and registers nothing.
func TestDestroyOverAnUnprovableBlockRefusesBeforeRegistration(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.capability.outcomeFor = map[string]Result{"artifact-server-lab": {Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an unknown outcome reported success")
	}
	current := currentOperation(t, h)
	h.capability.observations = []Observation{{Effect: reconciliation.EffectUnknown}}
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if len(reported) == 0 || reported[len(reported)-1].Code != "lifecycle.unknown" {
		t.Fatalf("refusal = %+v", reported)
	}
	if !strings.Contains(reported[len(reported)-1].Message, "artifact-server-lab") {
		t.Fatalf("the refusal does not name the effect it could not prove: %+v", reported)
	}
	if len(h.capability.destroys) != 0 {
		t.Fatalf("an unprovable block was destroyed: %v", h.capability.destroys)
	}
	if !slices.Equal(h.capability.observes, []string{"artifact-server-lab"}) {
		t.Fatalf("observations = %v", h.capability.observes)
	}
	if after := currentOperation(t, h); after != current {
		t.Fatalf("a refused removal registered an operation: %q became %q", current, after)
	}
	status, err := h.service.Status(context.Background(), StatusRequest{ContextName: "lab"})
	if err != nil || status.Lifecycle == nil || status.Lifecycle.State != "unknown" {
		t.Fatalf("durable operation = %+v (%v)", status.Lifecycle, err)
	}
	if len(h.binder.released) != 0 {
		t.Fatalf("a refused removal released the apply's binding: %v", h.binder.released)
	}
}

// An interrupted apply owns every block it started, including the one whose
// outcome it lost. The removal proves that outcome first, which is what admits
// the removal, and then takes back the whole set.
func TestDestroyOverAnInterruptedApplyResolvesThenRemoves(t *testing.T) {
	for _, tc := range []struct {
		name   string
		effect reconciliation.EffectState
	}{
		{"partly realized", reconciliation.EffectPartial},
		{"completed", reconciliation.EffectCompleted},
		{"never performed", reconciliation.EffectNoEffect},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("alpha"), definition("bravo")})
			h.service.options.Concurrency = 1
			h.capability.outcomeFor = map[string]Result{
				"alpha": {Outcome: reconciliation.OutcomeChanged},
				"bravo": {Outcome: reconciliation.OutcomeUnknown},
			}
			if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
				t.Fatal("an unknown outcome reported success")
			}
			h.capability.outcomeFor = nil
			h.capability.observations = []Observation{{Effect: tc.effect}}
			result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
			if err != nil {
				t.Fatalf("an interrupted apply refused its removal: %v", err)
			}
			if result.Receipt.State != string(reconciliation.OperationDone) || result.Receipt.Next != "none" {
				t.Fatalf("receipt = %+v", result.Receipt)
			}
			if !slices.Equal(h.capability.observes, []string{"bravo"}) {
				t.Fatalf("observations = %v", h.capability.observes)
			}
			if !slices.Equal(h.capability.destroys, []string{"alpha", "bravo"}) {
				t.Fatalf("destroyed blocks = %v", h.capability.destroys)
			}
			pristine, _ := reconciliation.PristineEvidence().Bytes()
			if !slices.Equal(h.workspace.evidence, pristine) {
				t.Fatalf("evidence = %q", h.workspace.evidence)
			}
			if !slices.Equal(h.binder.released, []string{"bind-1"}) {
				t.Fatalf("released bindings = %v", h.binder.released)
			}
		})
	}
}

// An executor that died mid-attempt leaves its block durably running, which is
// an unproved effect and not work in progress. A removal resolves it exactly as
// it resolves one the interrupt recorded.
func TestDestroyOverADeadExecutorResolvesTheRunningBlock(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	ctx, cancel := context.WithCancel(context.Background())
	h.capability.hold = func(string) { cancel() }
	h.capability.errorFor = map[string]error{"artifact-server-lab": context.Canceled}
	if _, err := h.service.Apply(ctx, ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an interrupted apply reported success")
	}
	leaveRunning(t, h, "artifact-server-lab")
	h.capability.hold, h.capability.errorFor = nil, nil
	h.capability.observations = []Observation{{Effect: reconciliation.EffectPartial}}
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatalf("a dead executor refused its removal: %v", err)
	}
	if result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
	if !slices.Equal(h.capability.observes, []string{"artifact-server-lab"}) {
		t.Fatalf("observations = %v", h.capability.observes)
	}
	if !slices.Equal(h.capability.destroys, []string{"artifact-server-lab"}) {
		t.Fatalf("destroyed blocks = %v", h.capability.destroys)
	}
}

// A removal observes only what is unproved. An apply stopped with nothing in
// flight is removed without reaching the host to ask about it.
func TestDestroyOverAnIncompleteApplyWithNothingUnprovedObservesNothing(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	h.capability.outcomeFor = map[string]Result{"provider-metal": {Outcome: reconciliation.OutcomeChanged}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"substrates"},
	}); err != nil {
		t.Fatal(err)
	}
	status, err := h.service.Status(context.Background(), StatusRequest{ContextName: "lab"})
	if err != nil || status.Lifecycle == nil || status.Lifecycle.State != "paused" {
		t.Fatalf("durable operation = %+v (%v)", status.Lifecycle, err)
	}
	if !slices.Contains(status.NextSteps, "bootwright destroy") {
		t.Fatalf("next steps = %v", status.NextSteps)
	}
	result, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatalf("a paused apply refused its removal: %v", err)
	}
	if result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
	if len(h.capability.observes) != 0 {
		t.Fatalf("a proved apply was observed: %v", h.capability.observes)
	}
	if !slices.Equal(h.capability.destroys, []string{"provider-metal"}) {
		t.Fatalf("destroyed blocks = %v", h.capability.destroys)
	}
}

// Resolution and the quiescence gate compose: the removal proves every outcome
// first and then refuses because what it would take back is in use. The proof
// is durable, so repeating the removal does not observe the same effect again.
func TestDestroyOverAnInterruptedApplyIsStillGated(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.capability.outcomeFor = map[string]Result{"artifact-server-lab": {Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an unknown outcome reported success")
	}
	h.capability.outcomeFor = nil
	h.capability.observations = []Observation{{Effect: reconciliation.EffectPartial}}
	h.capability.quiescence = map[string]Quiescence{
		"artifact-server-lab": {State: Live, Reason: "rhel-01 is running", Stop: "bootwright machine stop --name rhel-01"},
	}
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	if code := firstCode(err); code != "lifecycle.live" {
		t.Fatalf("refusal = %q", code)
	}
	if len(h.capability.destroys) != 0 {
		t.Fatalf("a gated removal destroyed something: %v", h.capability.destroys)
	}
	status, err := h.service.Status(context.Background(), StatusRequest{ContextName: "lab"})
	if err != nil || status.Lifecycle == nil || status.Lifecycle.State != "failed" {
		t.Fatalf("the resolution was not recorded: %+v (%v)", status.Lifecycle, err)
	}
	h.capability.quiescence = nil
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(h.capability.observes, []string{"artifact-server-lab"}) {
		t.Fatalf("the second removal observed again: %v", h.capability.observes)
	}
}

// The decision is read under the shared lock and the effects run under the
// exclusive one. An operation that moved in between invalidates the frozen plan
// waiting to register, so the removal refuses instead of applying it.
func TestDestroyRefusesWhenTheOperationItReplacesMoved(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.capability.outcomeFor = map[string]Result{"artifact-server-lab": {Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("a failed block reported success")
	}
	h.workspace.beforeMutation = func() {
		h.workspace.beforeMutation = nil
		leaveRunning(t, h, "artifact-server-lab")
	}
	_, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" {
		t.Fatalf("refusal = %+v", reported)
	}
	if !strings.Contains(reported[0].Message, "no longer the one the context holds") {
		t.Fatalf("refusal message = %q", reported[0].Message)
	}
	if len(h.capability.destroys) != 0 {
		t.Fatalf("a stale removal destroyed something: %v", h.capability.destroys)
	}
}

// A continuation is decided under the shared lock too. A removal another
// invocation completed in between supersedes the operation it would continue,
// so it refuses rather than resume work that removal already took back.
func TestContinuationRefusesWhenTheContextChangedBeforeMutation(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.capability.outcomeFor = map[string]Result{"artifact-server-lab": {Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("a failed block reported success")
	}
	h.capability.outcomeFor = nil
	continued := currentOperation(t, h)
	h.workspace.beforeMutation = func() {
		h.workspace.beforeMutation = nil
		if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	superseding := currentOperation(t, h)
	requireContextChanged(t, err, "apply", "it was planned from operation "+continued+" (failed), "+
		"and the context now holds operation "+superseding+" (done)")
	if len(h.capability.applies) != 1 {
		t.Fatalf("a stale continuation performed an effect: %v", h.capability.applies)
	}
	if superseding == continued {
		t.Fatal("the removal that superseded the operation is not the one the context holds")
	}
	operation, err := h.service.store(h.workspace.view()).ReadOperation(context.Background(), continued)
	if err != nil {
		t.Fatal(err)
	}
	if operation.State != reconciliation.OperationFailed {
		t.Fatalf("a stale continuation reopened the superseded operation: %s", operation.State)
	}
}

// A fresh apply is decided under the shared lock as well. An apply another
// invocation registered in between now holds the context, and registering this
// one over it would leave that operation's effects owned by nothing.
func TestFreshApplyRefusesWhenTheContextChangedBeforeMutation(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	applied := ""
	h.workspace.beforeMutation = func() {
		h.workspace.beforeMutation = nil
		if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
			t.Fatal(err)
		}
		applied = currentOperation(t, h)
	}
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	requireContextChanged(t, err, "apply", "it was planned from no operation, "+
		"and the context now holds operation "+applied+" (done)")
	if len(h.capability.applies) != 1 || h.workspace.binds != 1 {
		t.Fatalf("a stale apply performed work: applies %v, binds %d", h.capability.applies, h.workspace.binds)
	}
	if current := currentOperation(t, h); applied == "" || current != applied {
		t.Fatalf("current operation = %q, want the one the other invocation registered, %q", current, applied)
	}
	if h.workspace.area.written("op-" + strings.Repeat("02", 16)) {
		t.Fatal("a stale apply registered an operation")
	}
	// The refused apply refuses in the transaction that protects the context,
	// before it binds anything, so the only binding is the one the registered
	// operation owns, and it is kept, as is that operation's evidence.
	if h.binder.issued != 1 || len(h.binder.released) != 0 {
		t.Fatalf("issued %d, released %v", h.binder.issued, h.binder.released)
	}
	if applied := evidenceBytes(t, reconciliation.Apply, reconciliation.OperationDone); string(h.workspace.evidence) != string(applied) {
		t.Fatalf("evidence = %s, want the registered apply's %s", h.workspace.evidence, applied)
	}
}

// A continuation re-proves the block states it was planned from, not only
// which operation is current and its state. Another invocation that continued
// the same operation in between leaves it failed again, but at another block,
// so the retry this command presented is no longer the one it would run.
func TestContinuationRefusesWhenTheBlockStatesChangedBeforeMutation(t *testing.T) {
	h := newHarness(t, "alpha", "bravo")
	h.capability.outcomeFor = map[string]Result{"alpha": {Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("a failed block reported success")
	}
	continued := currentOperation(t, h)
	h.workspace.beforeMutation = func() {
		h.workspace.beforeMutation = nil
		h.capability.outcomeFor = map[string]Result{"bravo": {Outcome: reconciliation.OutcomeFailed}}
		if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
			t.Fatal("a failed block reported success")
		}
	}
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	requireContextChanged(t, err, "apply", "it was planned from operation "+continued+" (failed), "+
		"and the context now holds operation "+continued+" (failed) with different block states")
	if !slices.Equal(h.capability.applies, []string{"alpha", "alpha", "bravo"}) {
		t.Fatalf("a stale continuation performed an effect: %v", h.capability.applies)
	}
	store := h.service.store(h.workspace.view())
	frozen, err := store.ReadPlan(context.Background(), continued)
	if err != nil {
		t.Fatal(err)
	}
	states, err := store.BlockStates(context.Background(), continued, frozen)
	if err != nil {
		t.Fatal(err)
	}
	if states["alpha"] != reconciliation.BlockDone || states["bravo"] != reconciliation.BlockFailed {
		t.Fatalf("a stale continuation changed the block states: %v", states)
	}
}

// A fresh apply compiles its plan from the input the decision read. A revision
// published in between is input that plan was never compiled from, so the
// apply refuses rather than register it under the new revision and digest.
func TestFreshApplyRefusesWhenTheInputChangedBeforeMutation(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	published := "rev-fedcba9876543210fedcba9876543210"
	h.workspace.beforeMutation = func() {
		h.workspace.beforeMutation = nil
		h.workspace.revision = published
		h.workspace.inputs = desiredstate.Sources{Roots: []string{"/synthetic"}, Files: []desiredstate.SourceFile{
			desiredstate.NewSourceFile("/synthetic/environment.yaml", []byte("kind: Environment\nmetadata: {name: changed}\n")),
		}}
	}
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	requireContextChanged(t, err, "apply", "it was planned from no operation, "+
		"and the context now holds no operation at input revision "+published+" rather than "+testRevision)
	if len(h.capability.applies) != 0 || h.workspace.binds != 0 || len(h.workspace.reservations) != 0 {
		t.Fatalf("a stale apply performed work: applies %v, binds %d, reservations %v",
			h.capability.applies, h.workspace.binds, h.workspace.reservations)
	}
	if current := currentOperation(t, h); current != "" {
		t.Fatalf("a stale apply registered operation %q", current)
	}
	if h.workspace.area.written("op-" + strings.Repeat("01", 16)) {
		t.Fatal("a stale apply wrote operation records")
	}
	pristine, err := reconciliation.PristineEvidence().Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(h.workspace.evidence) != string(pristine) {
		t.Fatalf("a stale apply projected evidence: %s", h.workspace.evidence)
	}
	if h.binder.issued != 0 || len(h.binder.released) != 0 {
		t.Fatalf("a stale apply bound: issued %d, released %v", h.binder.issued, h.binder.released)
	}
}

// An incomplete removal is continued, never replaced by an apply.
func TestApplyOverAnIncompleteDestroyStillRefuses(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("a failed removal reported success")
	}
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" {
		t.Fatalf("refusal = %+v", reported)
	}
	if !strings.Contains(reported[0].Message, "an incomplete destroy must be continued") {
		t.Fatalf("refusal message = %q", reported[0].Message)
	}
}

// currentOperation names the operation the context holds, read the way the
// engine reads it.
// requireContextChanged asserts the refusal a transition returns when the
// context moved after the command read it, naming what it was planned from and
// what the context holds now.
func requireContextChanged(t *testing.T, err error, verb, moved string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
		reported[0].Remediation != "repeat bootwright "+verb+" to plan from what the context holds now" {
		t.Fatalf("refusal = %+v", reported)
	}
	if want := "the context changed after this command read it: " + moved; reported[0].Message != want {
		t.Fatalf("refusal message = %q, want %q", reported[0].Message, want)
	}
}

func currentOperation(t *testing.T, h *harness) string {
	t.Helper()
	status, err := h.service.Status(context.Background(), StatusRequest{ContextName: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	if status.Lifecycle == nil {
		return ""
	}
	return status.Lifecycle.Operation
}

// leaveRunning rewrites one block's durable state as an executor that died
// mid-attempt leaves it, which no interrupt this engine survives can produce.
func leaveRunning(t *testing.T, h *harness, block string) {
	t.Helper()
	rewriteState(t, h, path.Join(currentOperation(t, h), "blocks", block, "state.json"), string(reconciliation.BlockRunning))
}

// leaveExecutorDead rewrites the current operation and one of its blocks as an
// executor that died mid-attempt leaves both: running, with nothing recorded.
func leaveExecutorDead(t *testing.T, h *harness, block string) {
	t.Helper()
	leaveRunning(t, h, block)
	rewriteState(t, h, path.Join(currentOperation(t, h), "operation.json"), string(reconciliation.OperationRunning))
}

func rewriteState(t *testing.T, h *harness, target, state string) {
	t.Helper()
	h.workspace.area.mutex.Lock()
	defer h.workspace.area.mutex.Unlock()
	current, ok := h.workspace.area.files[target]
	if !ok {
		t.Fatalf("%s has no durable record", target)
	}
	const field = `"state":"`
	start := strings.Index(string(current), field)
	if start < 0 {
		t.Fatalf("record has no state: %q", current)
	}
	start += len(field)
	end := strings.Index(string(current[start:]), `"`)
	if end < 0 {
		t.Fatalf("record has no state: %q", current)
	}
	h.workspace.area.files[target] = []byte(string(current[:start]) + state + string(current[start+end:]))
}

func TestStageSelectionWithNothingStartableRefuses(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	_, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"clusters"},
	})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.stage" {
		t.Fatalf("fresh refusal = %+v", reported)
	}
	if !strings.Contains(reported[0].Remediation, "infra-components") {
		t.Fatalf("the refusal does not name a stage that would unblock work: %+v", reported[0])
	}
	if len(h.workspace.area.files) != 0 || h.workspace.mutations != 0 {
		t.Fatal("a refused stage selection registered an operation")
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"infra-components"},
	}); err != nil {
		t.Fatal(err)
	}
	_, err = h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"clusters"},
	})
	if code := firstCode(err); code != "lifecycle.stage" {
		t.Fatalf("continuation refusal = %q", code)
	}
}

func TestFailedBlockOutsideTheSelectionRefusesRetry(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"infra-components"},
	}); err == nil {
		t.Fatal("a failed block reported success")
	}
	_, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"substrates"},
	})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.stage" || !strings.Contains(reported[0].Remediation, "infra-components") {
		t.Fatalf("retry refusal = %+v", reported)
	}
}

// Resolution is read-only, so an unproved effect is observed whatever stages
// the invocation selects; nothing else may start until it is resolved.
func TestUnknownBlockIsResolvedRegardlessOfStage(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"infra-components"},
	}); err == nil {
		t.Fatal("an unknown effect reported success")
	}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"ok":true}`)}}
	result, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"substrates"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(h.capability.observes, []string{"artifacts"}) {
		t.Fatalf("observed blocks = %v", h.capability.observes)
	}
	if !slices.Equal(h.capability.applies, []string{"artifacts", "provider-metal"}) {
		t.Fatalf("applied blocks = %v", h.capability.applies)
	}
	if result.Receipt.State != string(reconciliation.OperationPaused) {
		t.Fatalf("receipt = %+v", result.Receipt)
	}
}

func TestPlanPreviewMarksStartDeferredAndNotSelected(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	result, err := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab", Stages: []string{"substrates"}})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(result.Stages, []string{"substrates"}) || result.Startable != 1 {
		t.Fatalf("preview = %+v", result)
	}
	if result.Receipt.State != "preview" || h.workspace.mutations != 0 {
		t.Fatalf("a preview allocated state: %+v", result.Receipt)
	}
	plain, err := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range plain.Steps {
		if step.Selection != "" {
			t.Fatalf("a preview without a selection marked %s as %q", step.ID, step.Selection)
		}
		if step.Stage == "" {
			t.Fatalf("step %s carries no stage", step.ID)
		}
	}
}

// Every plan block needs a resolved capability, so an object of a kind no
// capability claims refuses the whole operation before it registers.
func TestUnclaimedKindsRefuseBeforeRegistration(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	catalog := api.NewCatalog([]api.Object{
		api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
			api.FieldValue{Name: "controller", Value: api.MapValue(api.FieldValue{Name: "machineRef", Value: api.StringValue("controller")})},
		)),
		api.NewObject(api.Proxy, "lab-proxy", api.Value{}, api.MapValue(
			api.FieldValue{Name: "management", Value: api.StringValue("managed")},
		)),
		api.NewObject(api.Proxy, "upstream", api.Value{}, api.MapValue(
			api.FieldValue{Name: "management", Value: api.StringValue("external")},
		)),
		api.NewObject(api.StorageCluster, "ceph", api.Value{}, api.MapValue()),
		api.NewObject(api.ClusterAddon, "gitops", api.Value{}, api.MapValue()),
		api.NewObject(api.Machine, "bare", api.Value{}, api.MapValue(
			api.FieldValue{Name: "os", Value: api.MapValue(api.FieldValue{Name: "provided", Value: api.BoolValue(false)})},
		)),
		api.NewObject(api.Machine, "installed", api.Value{}, api.MapValue(
			api.FieldValue{Name: "os", Value: api.MapValue(api.FieldValue{Name: "provided", Value: api.BoolValue(true)})},
		)),
	})
	h.service.compiler = testCompiler{state: compilation.NewState(catalog, catalog, nil)}
	_, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	// The external proxy is an input and a Machine whose operating system is
	// provided needs no installation, so neither is refused. Each other object
	// is its own diagnostic, naming itself, the kind or shape no capability
	// realizes, and the remedy of leaving it out.
	unclaimed := func(kind, name, reason string) diagnostics.Diagnostic {
		return diagnostics.Diagnostic{
			Severity: "error", Code: "lifecycle.unsupported", Message: reason,
			Remediation: "remove " + kind + "/" + name + " from the selected Environment, or use an example within the supported shape such as " + supportedExample,
			Object:      &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: kind, Name: name},
		}
	}
	want := []diagnostics.Diagnostic{
		unclaimed("ClusterAddon", "gitops", "no capability of this executable realizes the ClusterAddon kind"),
		unclaimed("Machine", "bare", "no capability of this executable realizes a Machine whose operating system is not provided"),
		unclaimed("Proxy", "lab-proxy", "no capability of this executable manages the Proxy kind"),
		unclaimed("StorageCluster", "ceph", "no capability of this executable realizes the StorageCluster kind"),
	}
	if reported := diagnostics.Of(err); !reflect.DeepEqual(reported, want) {
		t.Fatalf("unclaimed refusal = %+v, want %+v", reported, want)
	}
}

// The clients a controller block installs are what every other block's adapter
// runs, so the engine makes each of them wait for it. The edge is a real block
// dependency, frozen with the plan, not a rule about stage order.
func TestControllerBlockPrecedesEveryOtherBlock(t *testing.T) {
	definitions := append([]reconciliation.BlockDefinition{
		stagedDefinition("controller-prerequisites", reconciliation.StageController),
	}, nestedDefinitions()...)
	h := newPlannedHarness(t, definitions)
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if len(h.capability.applies) == 0 || h.capability.applies[0] != "controller-prerequisites" {
		t.Fatalf("applied blocks = %v", h.capability.applies)
	}
}

// Selecting only a later stage therefore starts nothing at all, and the refusal
// names the stage that would unblock the operation.
func TestSelectingALaterStageWaitsForTheControllerBlock(t *testing.T) {
	definitions := append([]reconciliation.BlockDefinition{
		stagedDefinition("controller-prerequisites", reconciliation.StageController),
	}, nestedDefinitions()...)
	h := newPlannedHarness(t, definitions)
	_, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"infra-components"},
	})
	if diagnostics.Of(err)[0].Code != "lifecycle.stage" || !strings.Contains(diagnostics.Of(err)[0].Remediation, "--stage controller") {
		t.Fatalf("selection error = %v", err)
	}
	if len(h.capability.applies) != 0 {
		t.Fatalf("a refused selection still executed %v", h.capability.applies)
	}
}

// A plan without a controller block keeps exactly the dependencies its
// capabilities declared.
func TestPlanWithoutAControllerBlockGainsNoDependency(t *testing.T) {
	h := newPlannedHarness(t, nestedDefinitions())
	result, err := h.service.Plan(context.Background(), PlanRequest{ContextName: "lab"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Steps) != len(nestedDefinitions()) {
		t.Fatalf("steps = %+v", result.Steps)
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{
		ContextName: "lab", SkipConfirmation: true, Stages: []string{"infra-components"},
	}); err != nil {
		t.Fatalf("an unrelated selection was refused: %v", err)
	}
}

// A registered operation owns its binding until a completed destroy releases
// it. Releasing it when an effect fails would strand the operation: every
// continuation reopens that exact binding.
func TestFailedExecutionKeepsItsBindingAndFailedRegistrationReleasesIt(t *testing.T) {
	failed := newHarness(t, "artifact-server-lab")
	failed.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeFailed}}
	if _, err := failed.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("a failed block reported success")
	}
	if len(failed.binder.released) != 0 {
		t.Fatalf("a registered operation released its binding: %v", failed.binder.released)
	}

	refused := newHarness(t, "artifact-server-lab")
	refused.workspace.area.fail["replace index.json"] = errors.New("interrupted")
	if _, err := refused.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an interrupted registration reported success")
	}
	if !slices.Equal(refused.binder.released, []string{"bind-1"}) {
		t.Fatalf("a failed registration retained its binding: %v", refused.binder.released)
	}
}

// The terminal state alone says only that the operation did not complete. A
// block that refused for a nameable reason must carry that reason out with it.
func TestFailedBlockReportsItsOwnCauseBesideTheTerminalState(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	h.capability.applyErr = diagnostics.NewFailureWithRemediation(
		"secret.part", "the serving certificate does not cover every address its HTTPS endpoints answer on",
		"", "add 192.0.2.1 to the certificate's subject alternative names and regenerate it")
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err == nil || result == nil || result.Receipt.State != "failed" {
		t.Fatalf("failed apply = %+v (%v)", result, err)
	}
	codes := map[string]string{}
	for _, reported := range diagnostics.Of(err) {
		codes[reported.Code] = reported.Remediation
	}
	if _, ok := codes["lifecycle.state"]; !ok {
		t.Fatalf("the terminal state was dropped: %v", diagnostics.Of(err))
	}
	if codes["secret.part"] != "add 192.0.2.1 to the certificate's subject alternative names and regenerate it" {
		t.Fatalf("the block cause did not reach the caller: %v", diagnostics.Of(err))
	}
}

// A controller block is the one block that extends the host's shared
// prerequisites, so the engine hands it the publication boundary that work
// needs: the retained setup evidence, the shared client area and its sealing,
// the durable identity record, the before-state publication and the native
// package lock it has to hand back before its own transaction.
func TestControllerBlockReceivesItsPublicationBoundary(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{
		stagedDefinition("controller-prerequisites", reconciliation.StageController),
	})
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if len(h.capability.executions) != 1 {
		t.Fatalf("executions = %d", len(h.capability.executions))
	}
	execution := h.capability.executions[0]
	if execution.Attempt != 1 || execution.Resolution != 0 {
		t.Fatalf("attempt identity = %d/%d", execution.Attempt, execution.Resolution)
	}
	if execution.Stage == nil {
		t.Fatal("the controller block received no publication boundary")
	}
	if !execution.Stage.Setup.Exists || execution.Stage.Setup.State.Receipt.Status != "complete" {
		t.Fatalf("setup evidence = %+v", execution.Stage.Setup.State.Receipt)
	}
	for name, supplied := range map[string]bool{
		"ClientArea":         execution.Stage.ClientArea != nil,
		"SealClientArea":     execution.Stage.SealClientArea != nil,
		"RetainDependencies": execution.Stage.RetainDependencies != nil,
		"Prepare":            execution.Stage.Prepare != nil,
		"ReleaseFoundation":  execution.Stage.ReleaseFoundation != nil,
	} {
		if !supplied {
			t.Fatalf("the controller block received no %s capability", name)
		}
	}
	if err := execution.Stage.RetainDependencies(context.Background(), nil, nil, nil); err != nil {
		t.Fatalf("retention refused: %v", err)
	}
	if _, err := execution.Stage.ClientArea(context.Background(), strings.Repeat("d", 64)); err != nil {
		t.Fatalf("client area refused: %v", err)
	}
	if err := execution.Stage.SealClientArea(context.Background(), strings.Repeat("d", 64)); err != nil {
		t.Fatalf("sealing refused: %v", err)
	}
}

// Every block outside the controller stage receives no publication boundary at
// all, so one capability's specifics cannot reach another's attempt.
func TestOnlyTheControllerBlockReceivesThePublicationBoundary(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{
		stagedDefinition("artifact-server-lab", reconciliation.StageInfraComponents),
	})
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if len(h.capability.executions) != 1 {
		t.Fatalf("executions = %d", len(h.capability.executions))
	}
	if h.capability.executions[0].Stage != nil {
		t.Fatal("a block outside the controller stage reached that stage's boundary")
	}
}

// An observation is read-only. It may never authorize a host effect, so the
// before-state publication that precedes one is refused during a resolution.
func TestObservationCannotAuthorizeAHostEffect(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{
		stagedDefinition("controller-prerequisites", reconciliation.StageController),
	})
	h.capability.outcomes = []Result{{Outcome: reconciliation.OutcomeUnknown}}
	h.capability.observations = []Observation{{Effect: reconciliation.EffectCompleted, Evidence: json.RawMessage(`{"ok":true}`)}}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an unknown attempt reported success")
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if len(h.capability.observes) != 1 {
		t.Fatalf("observations = %v", h.capability.observes)
	}
	observation := h.capability.executions[len(h.capability.executions)-1]
	if observation.Resolution == 0 {
		t.Fatalf("the observation carries no resolution identity: %+v", observation)
	}
	if err := observation.Stage.Prepare(context.Background(), prerequisites.NativePreparation{}); err == nil {
		t.Fatal("an observation published a before-state")
	}
}

// A run that completes retains what its adapter printed, exactly as a failed
// one does, and its attempt log names the file and what it holds. Nothing about
// that retention reaches the outcome the operation records.
func TestASucceededAttemptRetainsWhatItsAdapterPrinted(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Receipt.State != string(reconciliation.OperationDone) {
		t.Fatalf("apply state = %q", result.Receipt.State)
	}
	var output, attempt string
	for name := range h.workspace.area.files {
		if strings.HasSuffix(name, "attempt-000001.output") {
			output = name
		}
		if strings.HasSuffix(name, "attempt-000001.jsonl") {
			attempt = name
		}
	}
	if output == "" {
		t.Fatalf("a completed run retained no adapter output: %v", slices.Sorted(maps.Keys(h.workspace.area.files)))
	}
	if !strings.Contains(string(h.workspace.area.files[output]), "TASK [acquire the image]") {
		t.Fatalf("retained output = %q", h.workspace.area.files[output])
	}
	record := string(h.workspace.area.files[attempt])
	if !strings.Contains(record, `"event":"adapter-output"`) || !strings.Contains(record, "attempt-000001.output, 42 bytes") {
		t.Fatalf("the attempt log did not record the retained output: %q", record)
	}
}

// Completion is measured against what the block froze, so a group the plan
// never declared cannot push a step past the work it declared.
func TestOnlyDeclaredGroupsAdvanceCompletion(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	var reported []ProgressEvent
	h.service.options.Progress = progressFunc(func(_ context.Context, event ProgressEvent) {
		reported = append(reported, event)
	})
	h.capability.extraGroup = "never-declared"
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	injected := false
	for _, event := range reported {
		if event.Group == "never-declared" {
			injected = true
		}
		if event.Declared > 0 && event.Completed > event.Declared {
			t.Fatalf("an undeclared group advanced completion to %d of %d", event.Completed, event.Declared)
		}
	}
	if !injected {
		t.Fatal("the undeclared group never reached the presenter, so nothing was proved")
	}
}

type progressFunc func(context.Context, ProgressEvent)

func (f progressFunc) ReportProgress(ctx context.Context, event ProgressEvent) { f(ctx, event) }

func (progressFunc) ReportLogLocation(context.Context, string) {}

// The log location is named before the first effect runs, because an operator
// who reads it afterwards cannot follow the work it was written for.
func TestLogLocationIsNamedBeforeTheFirstEffect(t *testing.T) {
	h := newHarness(t, "artifact-server-lab")
	progress := &testProgress{rowsAtLocation: -1}
	h.service.options.Progress = progress
	result, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true})
	if err != nil {
		t.Fatal(err)
	}
	want := "/var/lib/bootwright/contexts/lab/state/operations/" + result.Receipt.Operation + "/logs"
	if progress.location != want {
		t.Fatalf("reported location = %q, want %q", progress.location, want)
	}
	if progress.rowsAtLocation != 0 {
		t.Fatalf("the location followed %d progress rows, want none", progress.rowsAtLocation)
	}
	if result.LogLocation != want {
		t.Fatalf("result location = %q, want %q", result.LogLocation, want)
	}
}

// Every block of one operation runs inside the same approved bundle, so the
// operation opens it once before its first effect rather than once per block.
// Opening it per attempt asks the transaction to record what it has open while
// its own blocks are running, which is exactly what must not happen.
func TestAnOperationOpensItsApprovedBundleOnce(t *testing.T) {
	h := newHarness(t, "artifact-server-lab", "artifact-server-spare")
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if h.workspace.opened != 1 {
		t.Fatalf("the approved bundle was opened %d times", h.workspace.opened)
	}
	if len(h.capability.applies) != 2 {
		t.Fatalf("applied %v", h.capability.applies)
	}
}

// A removal proves what it is about to take back and then takes it back, and
// each of those runs inside one approved bundle however many blocks it covers.
func TestARemovalOpensItsApprovedBundleOncePerPhase(t *testing.T) {
	h := newPlannedHarness(t, []reconciliation.BlockDefinition{definition("alpha"), definition("bravo")})
	h.service.options.Concurrency = 2
	h.capability.outcomeFor = map[string]Result{
		"alpha": {Outcome: reconciliation.OutcomeUnknown},
		"bravo": {Outcome: reconciliation.OutcomeUnknown},
	}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: "lab", SkipConfirmation: true}); err == nil {
		t.Fatal("an unknown outcome reported success")
	}
	applied := h.workspace.opened
	h.capability.outcomeFor = nil
	h.capability.observations = []Observation{{Effect: reconciliation.EffectPartial}, {Effect: reconciliation.EffectPartial}}
	if _, err := h.service.Destroy(context.Background(), DestroyRequest{ContextName: "lab", SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if opened := h.workspace.opened - applied; opened != 2 {
		t.Fatalf("the removal opened the approved bundle %d times, want one to prove and one to remove", opened)
	}
	if len(h.capability.observes) != 2 || len(h.capability.destroys) != 2 {
		t.Fatalf("observed %v and destroyed %v", h.capability.observes, h.capability.destroys)
	}
}
