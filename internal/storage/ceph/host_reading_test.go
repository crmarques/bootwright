package ceph

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type readingRunner struct {
	mutex       sync.Mutex
	evidence    map[string][]byte
	readings    map[string]int
	replayed    map[string]string
	ownReads    int
	refuse      bool
	foreign     bool
	unreachable bool
	reading     chan struct{}
}

func (r *readingRunner) Run(ctx context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	if request.MaterialValues["reading"] == hostsReading {
		return r.readHosts(ctx, request)
	}
	r.mutex.Lock()
	defer r.mutex.Unlock()
	lent, shared := request.MaterialValues[hostsReading]
	r.replayed[request.Digest] = lent
	if !shared {
		r.ownReads++
	}
	if r.unreachable && (!shared || strings.Contains(lent, `"refused"`)) {
		return lifecycle.RunResult{}, request.Refusals["transport"]
	}
	return lifecycle.RunResult{Outcome: "unchanged", Evidence: r.evidence[request.Digest]}, nil
}

func (r *readingRunner) readHosts(ctx context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	decoded, err := DecodeRequest(request.Canonical)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	r.mutex.Lock()
	r.readings[decoded.Cluster]++
	held := r.reading
	r.mutex.Unlock()
	if held != nil {
		close(held)
		<-ctx.Done()
		return lifecycle.RunResult{}, ctx.Err()
	}
	if r.refuse {
		return lifecycle.RunResult{}, errors.New("the host reading run failed")
	}
	digest := request.Digest
	if r.foreign {
		digest = strings.Repeat("f", 64)
	}
	var hosts []string
	for _, host := range decoded.Hosts {
		if r.unreachable && host.Name == "node-2" {
			hosts = append(hosts, `{"name":"node-2","refused":{"detail":"transport@ceph_core._call:600","reason":"transport","unsettled":false}}`)
			break
		}
		hosts = append(hosts, `{"name":"`+host.Name+`","state":{"exists":true,"name":"`+host.Name+`","owned":true}}`)
	}
	return lifecycle.RunResult{Outcome: "unchanged", Evidence: []byte(`{"hosts":[` + strings.Join(hosts, ",") + `],"request":"` + digest + `","scope":"` + strings.Repeat("a", 64) + `","version":"` + HostReadingVersion + `"}`)}, nil
}

func clientExecutions(t *testing.T, cluster string, count int, runner *readingRunner, shared *lifecycle.SharedReadings) []lifecycle.Execution {
	t.Helper()
	var executions []lifecycle.Execution
	for i := range count {
		r := coreRequest()
		r.Cluster = cluster
		name := fmt.Sprintf("consumer-%d", i)
		r.Action, r.Client = "client", &Client{Name: name, Pool: "volumes", Access: "readOnly"}
		e := executionFor(t, r)
		e.Block.ID, e.Block.Kind, e.Block.Object = ClientBlockID(name), string(api.CephClient), name
		e.Block.RequestDigest = fmt.Sprintf("%s%02d%s", cluster[:1], i, strings.Repeat("0", 61))
		e = withMaterial(e, r)
		e.ApplyProved = []lifecycle.BlockEvidence{imageProof(t, r.Cluster, r.Image, resolvedDigest)}
		e.Shared = shared
		runtime := r
		runtime.Owner, runtime.FSID = e.ApplyOperation, FSIDFor(e.ApplyOperation, r.Cluster)
		evidence := readyEvidence(runtime)
		evidence.Request, evidence.Client = e.Block.RequestDigest, "client."+name
		runner.evidence[e.Block.RequestDigest] = evidenceBytes(t, evidence)
		executions = append(executions, e)
	}
	return executions
}

func newReadingRunner() *readingRunner {
	return &readingRunner{evidence: map[string][]byte{}, readings: map[string]int{}, replayed: map[string]string{}}
}

func observeAll(t *testing.T, runner *readingRunner, executions []lifecycle.Execution) []lifecycle.Observation {
	t.Helper()
	observations := make([]lifecycle.Observation, len(executions))
	failures := make([]error, len(executions))
	var group sync.WaitGroup
	for i, e := range executions {
		group.Go(func() {
			observations[i], failures[i] = New(api.CephClient, runner).Observe(context.Background(), e)
		})
	}
	group.Wait()
	for i, err := range failures {
		if err != nil {
			t.Fatalf("observation %d: %v", i, diagnostics.Of(err))
		}
	}
	return observations
}

func TestOneDiffReadsTheHostsOfAClusterScopeOnceForEveryResourceBlock(t *testing.T) {
	runner := newReadingRunner()
	shared := lifecycle.NewSharedReadings()
	executions := append(clientExecutions(t, "example", 5, runner, shared), clientExecutions(t, "second", 3, runner, shared)...)
	observations := observeAll(t, runner, executions)
	if want := map[string]int{"example": 1, "second": 1}; !reflect.DeepEqual(runner.readings, want) {
		t.Fatalf("host readings per cluster %v, want %v", runner.readings, want)
	}
	byCluster := map[string]string{}
	for _, e := range executions {
		r, _ := DecodeRequest(e.Block.Request)
		value := runner.replayed[e.Block.RequestDigest]
		if value == "" || byCluster[r.Cluster] != "" && byCluster[r.Cluster] != value {
			t.Fatalf("block %s of %s replayed %q", e.Block.ID, r.Cluster, value)
		}
		byCluster[r.Cluster] = value
	}
	if byCluster["example"] == byCluster["second"] {
		t.Fatal("two clusters shared one host reading")
	}
	alone := newReadingRunner()
	for i, e := range clientExecutions(t, "example", 5, alone, nil) {
		observation, err := New(api.CephClient, alone).Observe(context.Background(), e)
		if err != nil || !reflect.DeepEqual(observation, observations[i]) {
			t.Fatalf("block %d without a shared reading observed %+v (%v), with one %+v", i, observation, err, observations[i])
		}
	}
	if len(alone.readings) != 0 || alone.replayed[executions[0].Block.RequestDigest] != "" {
		t.Fatalf("an observation outside a diff read %v and replayed %q", alone.readings, alone.replayed[executions[0].Block.RequestDigest])
	}
}

func TestAFailedOrForeignHostReadingLeavesEachBlockItsOwnRead(t *testing.T) {
	for name, damage := range map[string]func(*readingRunner){
		"a failed run":    func(r *readingRunner) { r.refuse = true },
		"another request": func(r *readingRunner) { r.foreign = true },
	} {
		t.Run(name, func(t *testing.T) {
			runner := newReadingRunner()
			damage(runner)
			executions := clientExecutions(t, "example", 4, runner, lifecycle.NewSharedReadings())
			for i, observation := range observeAll(t, runner, executions) {
				if observation.Effect != reconciliation.EffectCompleted || runner.replayed[executions[i].Block.RequestDigest] != "" {
					t.Fatalf("block %d observed %+v and replayed %q", i, observation, runner.replayed[executions[i].Block.RequestDigest])
				}
			}
			if runner.readings["example"] != 1 {
				t.Fatalf("the host reading ran %d times", runner.readings["example"])
			}
		})
	}
}

func TestOnlyADiffsResourceObservationSharesAHostReading(t *testing.T) {
	runner := newReadingRunner()
	shared := lifecycle.NewSharedReadings()
	e := clientExecutions(t, "example", 1, runner, shared)[0]
	recorded := e
	recorded.Attempt = 1
	cluster := executionFor(t, coreRequest())
	cluster.Shared = shared
	for name, run := range map[string]func() map[string]string{
		"a recorded observation": func() map[string]string {
			return New(api.CephClient, runner).sharedHosts(context.Background(), recorded, mustPrepare(t, recorded), false)
		},
		"a removal observation": func() map[string]string {
			return New(api.CephClient, runner).sharedHosts(context.Background(), e, mustPrepare(t, e), true)
		},
		"the cluster block": func() map[string]string {
			return New(api.CephCluster, runner).sharedHosts(context.Background(), cluster, mustPrepare(t, cluster), false)
		},
	} {
		if values := run(); values != nil || len(runner.readings) != 0 {
			t.Fatalf("%s shared %v after %v readings", name, values, runner.readings)
		}
	}
}

func mustPrepare(t *testing.T, e lifecycle.Execution) Request {
	t.Helper()
	r, err := DecodeRequest(e.Block.Request)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestAHostReadingKeyIgnoresOnlyTheBlocksOwnResource(t *testing.T) {
	base := coreRequest()
	pool, client := base, base
	pool.Action, pool.Pool = "pool", &Pool{Name: "volumes"}
	client.Action, client.Client = "client", &Client{Name: "consumer"}
	first, _ := hostReadingKey(pool)
	second, _ := hostReadingKey(client)
	if first != second {
		t.Fatal("two resource blocks of one scope keyed apart")
	}
	for name, change := range map[string]func(*Request){
		"delegated": func(r *Request) { r.Delegated = []DelegatedService{{Name: "crash", Hosts: []string{"node-1"}}} },
		"image":     func(r *Request) { r.Image += "-other" },
		"owner":     func(r *Request) { r.Owner = "apply-other" },
		"host":      func(r *Request) { r.Hosts = r.Hosts[:2] },
		"cluster":   func(r *Request) { r.Cluster = "other" },
	} {
		changed := client
		changed.Hosts = append([]Host(nil), client.Hosts...)
		change(&changed)
		if key, _ := hostReadingKey(changed); key == first {
			t.Fatalf("a changed %s kept the host reading key", name)
		}
	}
}

func TestAHostReadingKeyZeroesExactlyTheMembersTheCollectionCallsResources(t *testing.T) {
	source := string(ansible.Assets()["collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_native.py"])
	_, literal, found := strings.Cut(source, "\nRESOURCE_MEMBERS = frozenset((")
	literal, _, closed := strings.Cut(literal, "))\n")
	if !found || !closed {
		t.Fatal("the collection's RESOURCE_MEMBERS is missing")
	}
	var python []string
	for _, item := range strings.Split(literal, ",") {
		if item = strings.Trim(strings.TrimSpace(item), `"`); item != "" {
			python = append(python, item)
		}
	}
	slices.Sort(python)
	if !slices.Equal(python, hostReadingResourceMembers) {
		t.Fatalf("Go zeroes %v, the collection's RESOURCE_MEMBERS is %v", hostReadingResourceMembers, python)
	}
	members := map[string]reflect.StructField{}
	for _, field := range reflect.VisibleFields(reflect.TypeFor[Request]()) {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		members[name] = field
	}
	for _, name := range hostReadingResourceMembers {
		if field, ok := members[name]; !ok || field.Type.Kind() != reflect.Pointer {
			t.Fatalf("resource member %s is no pointer member of Request", name)
		}
	}
}

func TestAWaiterWhoseReaderWasCanceledReadsItsOwnHosts(t *testing.T) {
	runner := newReadingRunner()
	runner.reading = make(chan struct{})
	executions := clientExecutions(t, "example", 2, runner, lifecycle.NewSharedReadings())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leader := make(chan error, 1)
	go func() {
		_, err := New(api.CephClient, runner).Observe(ctx, executions[0])
		leader <- err
	}()
	<-runner.reading
	waiter := make(chan lifecycle.Observation, 1)
	go func() {
		observation, err := New(api.CephClient, runner).Observe(context.Background(), executions[1])
		if err != nil {
			t.Errorf("the waiter: %v", diagnostics.Of(err))
		}
		waiter <- observation
	}()
	cancel()
	if observation := <-waiter; observation.Effect != reconciliation.EffectCompleted || runner.replayed[executions[1].Block.RequestDigest] != "" {
		t.Fatalf("the waiter observed %+v, replaying %q", observation, runner.replayed[executions[1].Block.RequestDigest])
	}
	<-leader
	if runner.readings["example"] != 1 {
		t.Fatalf("the hosts were read %d times", runner.readings["example"])
	}
}

func TestAReplayedHostRefusalIsTheBlocksOwnRefusal(t *testing.T) {
	alone := newReadingRunner()
	alone.unreachable = true
	own := clientExecutions(t, "example", 3, alone, nil)
	runner := newReadingRunner()
	runner.unreachable = true
	shared := clientExecutions(t, "example", 3, runner, lifecycle.NewSharedReadings())
	for i := range shared {
		expected, ownErr := New(api.CephClient, alone).Observe(context.Background(), own[i])
		observed, err := New(api.CephClient, runner).Observe(context.Background(), shared[i])
		if ownErr == nil || err == nil || !reflect.DeepEqual(diagnostics.Of(err), diagnostics.Of(ownErr)) || !reflect.DeepEqual(observed, expected) {
			t.Fatalf("block %d replayed %+v (%v), its own read %+v (%v)", i, observed, diagnostics.Of(err), expected, diagnostics.Of(ownErr))
		}
		if !strings.Contains(runner.replayed[shared[i].Block.RequestDigest], `"refused"`) {
			t.Fatalf("block %d did not replay the refused reading", i)
		}
	}
	if runner.readings["example"] != 1 || runner.ownReads != 0 {
		t.Fatalf("hosts read %d times, %d own reads", runner.readings["example"], runner.ownReads)
	}
}
