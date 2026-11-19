package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// credentialVersions records, for each test binder, the Grafana candidates
// it generated and every credential version write, in order. The binder's
// own fields belong to the journey suite.
var credentialVersions = struct {
	sync.Mutex
	generated map[*testBinder]int
	writes    map[*testBinder][]string
	refused   map[*testBinder]error
}{generated: map[*testBinder]int{}, writes: map[*testBinder][]string{}, refused: map[*testBinder]error{}}

func (b *testBinder) GenerateCredential(_ context.Context, _, secret string) (secrets.Material, error) {
	credentialVersions.Lock()
	credentialVersions.generated[b]++
	count := credentialVersions.generated[b]
	refused := credentialVersions.refused[b]
	credentialVersions.Unlock()
	if refused != nil {
		return secrets.Material{}, refused
	}
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(fmt.Sprintf("%s-candidate-%020d", secret, count))}), nil
}

func (b *testBinder) versionWrites() []string {
	credentialVersions.Lock()
	defer credentialVersions.Unlock()
	return slices.Clone(credentialVersions.writes[b])
}

// PrepareCredentialVersion models the custody store: the pinned version must
// be current or the current one must already hold the material; a new
// version is written only when the material differs, then the binding is
// rebound at the Secret's current version.
func (b *testBinder) PrepareCredentialVersion(_ context.Context, request custody.RebindRequest) (custody.ScopedCredentialVersion, error) {
	publication := func(rebind bool) custody.CredentialPublication {
		return func(ctx context.Context, _ secretstore.Context, area secretstore.Area, pinned string, material secrets.Material) (secretstore.Binding, error) {
			if err := publishLent(area); err != nil {
				return secretstore.Binding{}, err
			}
			name := request.Names[0]
			current := b.versions[name]
			value, _ := material.Part(secrets.ValuePart)
			held, _ := b.versionMaterial[current].Part(secrets.ValuePart)
			equal := string(value) == string(held)
			defer clear(value)
			defer clear(held)
			switch {
			case !equal && (current != pinned || rebind):
				return secretstore.Binding{}, errors.Join(custody.ErrCredentialMoved, errors.New("moved"))
			case rebind:
				return b.Rebind(ctx, request)
			case !equal:
				version := fmt.Sprintf("%s-v%d", name, len(b.versionMaterial)+1)
				b.versionMaterial[version] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: value})
				b.versions[name] = version
				credentialVersions.Lock()
				credentialVersions.writes[b] = append(credentialVersions.writes[b], version)
				credentialVersions.Unlock()
			}
			return secretstore.Binding{}, nil
		}
	}
	return custody.ScopedCredentialVersion{Write: publication(false), Rebind: publication(true)}, nil
}

func (b *testBinder) ReleaseInArea(ctx context.Context, _ secretstore.Context, area secretstore.Area, binding string) (bool, error) {
	if err := publishLent(area); err != nil {
		return false, err
	}
	return b.Release(ctx, custody.BindingRequest{ContextName: testContextName, BindingID: binding})
}

func (b *testBinder) DiscardProduced(ctx context.Context, selected secretstore.Context, area secretstore.Area, block, name string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if selected.Name != testContextName {
		return false, errors.New("produced material was discarded from another context")
	}
	key := block + "/" + name
	if _, held := b.produced[key]; !held {
		return false, nil
	}
	if err := publishLent(area); err != nil {
		return false, err
	}
	delete(b.produced, key)
	delete(b.unproved, key)
	delete(b.identities, key)
	b.journal = append(b.journal, "discard "+key)
	return true, nil
}

const grafanaSecret = "grafana-admin"

var (
	dashboardLogin    = AdminLogin{Name: "dashboard", User: "admin", Output: "admin-password", Seed: "dashboard", Custody: AdminProducedPassword}
	prometheusLogin   = AdminLogin{Name: "prometheus", Output: "monitoring", Custody: AdminProducedLogin}
	alertmanagerLogin = AdminLogin{Name: "alertmanager", Output: "monitoring", Custody: AdminProducedLogin}
	grafanaLogin      = AdminLogin{Name: "grafana", User: "admin", Secret: grafanaSecret, Custody: AdminBoundSecret}
)

var grafanaDeclaration = api.NewObject(api.Secret, grafanaSecret, api.Value{}, api.MapValue(api.FieldValue{Name: "type", Value: api.StringValue("token")}))

const activeMonitoring = `{"alertmanager":{"password":"am-active-password","user":"am-admin"},"prometheus":{"password":"pr-active-password","user":"pr-admin"}}`

// cephAdminHarness applies a managed CephCluster edge of three blocks: the
// cluster block owns the dashboard, the gateway the two basic-auth logins and
// the monitoring unit Grafana's bound password. extra blocks join the plan.
func cephAdminHarness(t *testing.T, extra ...reconciliation.BlockDefinition) (*harness, *credentialCapability) {
	t.Helper()
	var blocks []reconciliation.BlockDefinition
	for _, id := range []string{"edge-cluster", "edge-gateway", "edge-monitoring"} {
		block := definition(id)
		block.Kind, block.Object, block.Stage = string(api.CephCluster), "edge", reconciliation.StageClusters
		blocks = append(blocks, block)
	}
	blocks[2].Request = json.RawMessage(`{"grafanaRef":"` + grafanaSecret + `","name":"edge-monitoring"}`)
	for _, block := range extra {
		block.Kind, block.Object, block.Stage = string(api.CephCluster), "edge", reconciliation.StageClusters
		blocks = append(blocks, block)
	}
	h := newPlannedHarness(t, blocks)
	h.capability.secrets = append(h.capability.secrets, grafanaSecret)
	h.service.options.PasswordMatches = func(password, hash []byte) bool { return true }
	old := h.service.compiler.(testCompiler).state.Effective()
	catalog := api.NewCatalog(append(old.Objects(), api.NewObject(api.CephCluster, "edge", api.Value{}, api.MapValue()), grafanaDeclaration))
	h.service.compiler = testCompiler{state: compilation.NewState(catalog, catalog, nil)}
	capability := &credentialCapability{testCapability: h.capability, logins: map[string][]AdminLogin{
		"edge-cluster": {dashboardLogin}, "edge-gateway": {alertmanagerLogin, prometheusLogin}, "edge-monitoring": {grafanaLogin},
	}, failing: map[string]bool{}}
	for _, block := range extra {
		capability.logins[block.ID] = nil
	}
	h.service.capabilities = testResolver{capability: capability, bindings: []CapabilityBinding{{Kind: string(api.CephCluster), Implementation: blocks[0].Implementation}}}
	h.binder.versions = map[string]string{grafanaSecret: grafanaSecret + "-v1"}
	h.binder.versionMaterial = map[string]secrets.Material{grafanaSecret + "-v1": secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("grafana-bound-password")})}
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatal(err)
	}
	if h.binder.produced == nil {
		h.binder.produced = map[string]string{}
	}
	h.binder.produced["edge-gateway/monitoring"] = activeMonitoring
	return h, capability
}

func updateLogins(h *harness, logins ...string) (*UpdateAdminCredentialsResult, error) {
	return h.service.AdminCredentials().UpdateAdminCredentials(context.Background(), UpdateAdminCredentialsRequest{ContextName: testContextName, Name: "edge", Logins: logins, SkipConfirmation: true})
}

func loginNames(result *UpdateAdminCredentialsResult) []string {
	var names []string
	for _, login := range result.Logins {
		names = append(names, login.Login+"="+login.User)
	}
	return names
}

func monitoringLogins(t *testing.T, h *harness) map[string]monitoringEntry {
	t.Helper()
	logins, ok := decodeLogins([]byte(h.binder.produced["edge-gateway/monitoring"]))
	if !ok {
		t.Fatalf("the custodied monitoring logins are invalid: %s", h.binder.produced["edge-gateway/monitoring"])
	}
	return logins
}

// rootBinding is the binding the cluster's apply names now.
func rootBinding(t *testing.T, h *harness) string {
	t.Helper()
	var binding string
	err := h.workspace.ReadLifecycle(context.Background(), testContextName, func(view View) error {
		store := h.service.store(view)
		index, err := store.Index(context.Background())
		if err != nil {
			return err
		}
		operation, err := store.ReadOperation(context.Background(), index.Current)
		binding = firstBinding(operation.Bindings)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return binding
}

func TestAdminCredsDefaultSelectsEveryLoginOfTheCluster(t *testing.T) {
	t.Parallel()
	h, capability := cephAdminHarness(t)
	result, err := updateLogins(h)
	if err != nil {
		t.Fatalf("%v %+v", err, diagnostics.Of(err))
	}
	want := []string{"dashboard=admin", "prometheus=", "alertmanager=", "grafana=admin"}
	if got := loginNames(result); !slices.Equal(got, want) || !slices.Equal(capability.order, []string{"dashboard", "prometheus", "alertmanager", "grafana"}) {
		t.Fatalf("logins %v in order %v, want %v", got, capability.order, want)
	}
}

func TestAdminCredsRefusesAnInapplicableLoginBeforeConfirmation(t *testing.T) {
	t.Parallel()
	h, capability := cephAdminHarness(t)
	reads := h.binder.producedReads
	result, err := h.service.AdminCredentials().UpdateAdminCredentials(context.Background(), UpdateAdminCredentialsRequest{ContextName: testContextName, Name: "edge", Logins: []string{"console", "grafana", "loki"}})
	d := diagnostics.Of(err)
	if result != nil || len(d) != 1 || d[0].Code != "engine.credential-login-inapplicable" || !strings.Contains(d[0].Message, "console, loki") || !strings.Contains(d[0].Message, "dashboard, prometheus, alertmanager, grafana") {
		t.Fatalf("update = %+v %+v", result, d)
	}
	if h.confirmer.asked != 0 || capability.prepares != 0 || h.binder.producedReads != reads || len(h.binder.versionWrites()) != 0 {
		t.Fatalf("an inapplicable login asked %d times, prepared %d and read %d custody entries", h.confirmer.asked, capability.prepares, h.binder.producedReads-reads)
	}
}

func TestAdminCredsStopsAtAFailedLoginAndNamesTheRemainingLogins(t *testing.T) {
	t.Parallel()
	h, capability := cephAdminHarness(t)
	capability.failing["prometheus"] = true
	result, err := updateLogins(h)
	d := diagnostics.Of(err)
	if result != nil || len(d) != 1 || !strings.HasSuffix(d[0].Remediation, "bootwright cluster update-admin-creds --context "+testContextName+" --name edge --login prometheus,alertmanager,grafana") {
		t.Fatalf("update = %+v %+v", result, d)
	}
	if !slices.Equal(capability.order, []string{"dashboard", "prometheus"}) || h.binder.produced["edge-cluster/admin-password"] == "" || len(h.binder.versionWrites()) != 0 {
		t.Fatalf("the run went on past the failed login: %v", capability.order)
	}
	if logins := monitoringLogins(t, h); logins["prometheus"].Password != "pr-active-password" {
		t.Fatal("an unproved login was published")
	}
}

func TestAMonitoringLoginReusesItsCandidateAfterALostReply(t *testing.T) {
	t.Parallel()
	h, capability := cephAdminHarness(t)
	capability.fail = true
	if _, err := updateLogins(h, "prometheus"); err == nil {
		t.Fatal("a lost reply was reported as an update")
	}
	first := capability.remote
	if monitoringLogins(t, h)["prometheus"].Password != "pr-active-password" || h.binder.produced["edge-gateway/prometheus-update"] == "" {
		t.Fatal("the candidate was not retained, or an unproved one was published")
	}
	if _, err := updateLogins(h, "prometheus"); err != nil {
		t.Fatal(err)
	}
	if capability.generated != 1 || capability.remote != first || monitoringLogins(t, h)["prometheus"].Password != first {
		t.Fatalf("the retry generated %d candidates or lost the first", capability.generated)
	}
	if _, err := updateLogins(h, "prometheus"); err != nil || capability.generated != 2 || capability.remote == first {
		t.Fatalf("a new invocation reused a finished candidate: %v", err)
	}
}

func TestAMonitoringLoginPublishesKeepingTheOtherService(t *testing.T) {
	t.Parallel()
	h, capability := cephAdminHarness(t)
	if _, err := updateLogins(h, "prometheus"); err != nil {
		t.Fatal(err)
	}
	logins := monitoringLogins(t, h)
	if logins["alertmanager"] != (monitoringEntry{Password: "am-active-password", User: "am-admin"}) || logins["prometheus"].User != "pr-admin" || logins["prometheus"].Password != capability.remote {
		t.Fatalf("published logins %+v", logins)
	}
	var lent map[string]json.RawMessage
	if err := json.Unmarshal([]byte(capability.lent["prometheus"][0]), &lent); err != nil || !slices.Equal(slices.Sorted(maps.Keys(lent)), []string{"active", "guard", "password", "user"}) ||
		string(lent["active"]) != `{"password":"pr-active-password","user":"pr-admin"}` || string(lent["guard"]) != `{"login":"prometheus"}` {
		t.Fatalf("lent update %s (%v)", capability.lent["prometheus"][0], err)
	}
}

func TestGrafanaUpdateWritesTheVersionAndRebindsTheApplysBinding(t *testing.T) {
	t.Parallel()
	h, capability := cephAdminHarness(t)
	replaced := rootBinding(t, h)
	if _, err := updateLogins(h, "grafana"); err != nil {
		t.Fatalf("%v %+v", err, diagnostics.Of(err))
	}
	writes := h.binder.versionWrites()
	issued := rootBinding(t, h)
	if len(writes) != 1 || h.binder.versions[grafanaSecret] != writes[0] || issued == replaced || h.binder.pins[issued][grafanaSecret] != writes[0] {
		t.Fatalf("writes %v, binding %s pins %v", writes, issued, h.binder.pins[issued])
	}
	stored, _ := h.binder.versionMaterial[writes[0]].Part(secrets.ValuePart)
	if string(stored) != capability.remote || !slices.Contains(h.binder.released, replaced) {
		t.Fatalf("stored %q for %q; released %v", stored, capability.remote, h.binder.released)
	}
	var lent map[string]json.RawMessage
	if err := json.Unmarshal([]byte(capability.lent["grafana"][0]), &lent); err != nil || !slices.Equal(slices.Sorted(maps.Keys(lent)), []string{"guard", "password", "user"}) || string(lent["user"]) != `"admin"` {
		t.Fatalf("lent update %s (%v)", capability.lent["grafana"][0], err)
	}
	if !strings.Contains(string(h.workspace.logArea.files[operationstore.OperationLogPath(operationOf(t, h))]), "rebound as "+issued) {
		t.Fatal("the rebind is not in the apply's operation log")
	}
}

func operationOf(t *testing.T, h *harness) string {
	t.Helper()
	var id string
	err := h.workspace.ReadLifecycle(context.Background(), testContextName, func(view View) error {
		index, err := h.service.store(view).Index(context.Background())
		id = index.Current
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestGrafanaUpdateRecoversAfterPutBeforeRebind(t *testing.T) {
	t.Parallel()
	h, capability := cephAdminHarness(t)
	replaced := rootBinding(t, h)
	h.binder.kill = func(point string) error {
		if point == "publish secret binding" {
			return errors.New("killed before the rebind")
		}
		return nil
	}
	if _, err := updateLogins(h, "grafana"); err == nil {
		t.Fatal("an interrupted rebind was reported as an update")
	}
	if len(h.binder.versionWrites()) != 1 || rootBinding(t, h) != replaced {
		t.Fatal("the interruption did not land between the write and the rebind")
	}
	h.binder.kill = nil
	if _, err := updateLogins(h, "grafana"); err != nil {
		t.Fatalf("%v %+v", err, diagnostics.Of(err))
	}
	issued := rootBinding(t, h)
	if len(h.binder.versionWrites()) != 1 || h.binder.pins[issued][grafanaSecret] != h.binder.versions[grafanaSecret] || len(capability.lent["grafana"]) != 2 || capability.lent["grafana"][0] != capability.lent["grafana"][1] {
		t.Fatalf("the repeat lent another candidate or rewrote: lent %v, writes %v", capability.lent["grafana"], h.binder.versionWrites())
	}
}

func TestGrafanaUpdateRecoversAfterRebindBeforeRecord(t *testing.T) {
	t.Parallel()
	h, capability := cephAdminHarness(t)
	replaced := rootBinding(t, h)
	record := operationOf(t, h) + "/operation.json"
	h.workspace.area.landing = func(_, target string, _ map[string][]byte) error {
		if target == record {
			return errors.New("killed before the record")
		}
		return nil
	}
	if _, err := updateLogins(h, "grafana"); err == nil {
		t.Fatal("an interrupted record was reported as an update")
	}
	if rootBinding(t, h) != replaced || len(h.binder.rebinds) != 1 {
		t.Fatal("the interruption did not land between the rebind and the record")
	}
	h.workspace.area.landing = nil
	if _, err := updateLogins(h, "grafana"); err != nil {
		t.Fatalf("%v %+v", err, diagnostics.Of(err))
	}
	issued := rootBinding(t, h)
	if len(h.binder.versionWrites()) != 1 || issued == replaced || h.binder.pins[issued][grafanaSecret] != h.binder.versions[grafanaSecret] || capability.lent["grafana"][0] != capability.lent["grafana"][1] {
		t.Fatalf("the repeat lent another candidate or rewrote: lent %v, writes %v", capability.lent["grafana"], h.binder.versionWrites())
	}
}

func TestGrafanaUpdateRefusesASecretAnotherBlockReferences(t *testing.T) {
	t.Parallel()
	sharing := definition("edge-sharing")
	sharing.Request = json.RawMessage(`{"name":"edge-sharing","passwordRef":"` + grafanaSecret + `"}`)
	h, capability := cephAdminHarness(t, sharing)
	_, err := updateLogins(h, "grafana")
	if d := diagnostics.Of(err); len(d) != 1 || d[0].Code != "engine.credential-login-inapplicable" || capability.prepares != 0 {
		t.Fatalf("a shared Secret gave %+v", d)
	}
	result, err := updateLogins(h)
	if err != nil || slices.Contains(capability.order, "grafana") || len(result.Logins) != 3 {
		t.Fatalf("the default ran %v (%v)", capability.order, err)
	}
}

func TestASettlingApplyAfterGrafanaUpdateBindsNothing(t *testing.T) {
	t.Parallel()
	h, _ := cephAdminHarness(t)
	if _, err := updateLogins(h, "grafana"); err != nil {
		t.Fatal(err)
	}
	issued, bound, applies := h.binder.issued, len(h.binder.bound), len(h.capability.applies)
	if _, err := h.service.Apply(context.Background(), ApplyRequest{ContextName: testContextName, SkipConfirmation: true}); err != nil {
		t.Fatalf("%v %+v", err, diagnostics.Of(err))
	}
	if h.binder.issued != issued || len(h.binder.bound) != bound || len(h.capability.applies) != applies {
		t.Fatalf("a settling apply bound %d and applied %d", h.binder.issued-issued, len(h.capability.applies)-applies)
	}
}

func TestSecretHoldsCoverTheReboundGrafanaVersion(t *testing.T) {
	t.Parallel()
	h, _ := cephAdminHarness(t)
	if _, err := updateLogins(h, "grafana"); err != nil {
		t.Fatal(err)
	}
	issued := rootBinding(t, h)
	holds, err := h.service.SecretHolds(context.Background(), testContextName)
	if err != nil {
		t.Fatal(err)
	}
	held := slices.ContainsFunc(holds.Bindings, func(hold custody.Hold) bool {
		return hold.Binding == issued && (hold.All || slices.Contains(hold.Secrets, grafanaSecret))
	})
	if !held || h.binder.pins[issued][grafanaSecret] != h.binder.versions[grafanaSecret] {
		t.Fatalf("holds %+v do not cover %s at the current version", holds, issued)
	}
}

func TestSecretsRefusesAPendingMonitoringOrGrafanaUpdate(t *testing.T) {
	t.Parallel()
	h, capability := cephAdminHarness(t)
	capability.failing["prometheus"], capability.failing["grafana"] = true, true
	service := h.service.AdminCredentials()
	read := func() []error {
		monitoring, _, monitoringErr := service.MonitoringLogin(context.Background(), testContextName, "edge", "edge-gateway", "monitoring", "prometheus")
		monitoring.Material.Clear()
		grafana, _, grafanaErr := service.GrafanaPassword(context.Background(), testContextName, "edge", "edge-monitoring", grafanaSecret)
		grafana.Clear()
		return []error{monitoringErr, grafanaErr}
	}
	for _, err := range read() {
		if err != nil {
			t.Fatalf("a login with no update refused: %+v", diagnostics.Of(err))
		}
	}
	for _, login := range []string{"prometheus", "grafana"} {
		if _, err := updateLogins(h, login); err == nil {
			t.Fatalf("the failing %s update succeeded", login)
		}
	}
	for i, login := range []string{"prometheus", "grafana"} {
		d := diagnostics.Of(read()[i])
		if len(d) != 1 || d[0].Code != "engine.credential-update-pending" || !strings.HasSuffix(d[0].Remediation, "--login "+login+" finishes the retained update") {
			t.Fatalf("an unfinished %s update disclosed: %+v", login, d)
		}
	}
	if _, err := updateLogins(h, "prometheus", "grafana"); err != nil {
		t.Fatal(err)
	}
	for _, err := range read() {
		if err != nil {
			t.Fatalf("a finished update refused: %+v", diagnostics.Of(err))
		}
	}
}

func TestALoginRemedyKeepsTheAdaptersTextBeforeTheRepeat(t *testing.T) {
	t.Parallel()
	rest := "bootwright cluster update-admin-creds --context " + testContextName + " --name edge --login prometheus,alertmanager,grafana"
	for _, test := range []struct{ remedy, want string }{
		{"set that service's login back with ceph orch prometheus set-credentials", "set that service's login back with ceph orch prometheus set-credentials, then run " + rest},
		{"inspect bootwright cluster update-admin-creds --help first", "inspect bootwright cluster update-admin-creds --help first, then run " + rest},
		{RepeatAdminCredentialUpdate, rest},
	} {
		h, capability := cephAdminHarness(t)
		capability.refusals = map[string]error{"prometheus": engineCredentialMoved.Fail("the prometheus login moved", test.remedy)}
		_, err := updateLogins(h)
		if d := diagnostics.Of(err); len(d) != 1 || d[0].Remediation != test.want {
			t.Fatalf("remedy %q became %+v, want %q", test.remedy, d, test.want)
		}
	}
}

func TestADriftBeforeAnyEffectDiscardsTheRetainedCandidateSoDisclosureWorks(t *testing.T) {
	t.Parallel()
	for _, login := range []string{"prometheus", "grafana"} {
		t.Run(login, func(t *testing.T) {
			t.Parallel()
			h, capability := cephAdminHarness(t)
			block := map[string]string{"prometheus": "edge-gateway", "grafana": "edge-monitoring"}[login]
			service := h.service.AdminCredentials()
			disclose := func() error {
				if login == "prometheus" {
					read, _, err := service.MonitoringLogin(context.Background(), testContextName, "edge", block, "monitoring", login)
					read.Material.Clear()
					return err
				}
				read, _, err := service.GrafanaPassword(context.Background(), testContextName, "edge", block, grafanaSecret)
				read.Clear()
				return err
			}
			capability.refusals = map[string]error{login: engineCredentialMoved.Fail("the "+login+" login drifted after an effect", "")}
			if _, err := updateLogins(h, login); err == nil || h.binder.produced[block+"/"+login+"-update"] == "" {
				t.Fatalf("a drift after an effect dropped the retained candidate (%v)", err)
			}
			if d := diagnostics.Of(disclose()); len(d) != 1 || d[0].Code != "engine.credential-update-pending" {
				t.Fatalf("a kept candidate disclosed: %+v", d)
			}
			capability.refusals = map[string]error{login: errors.Join(engineCredentialMoved.Fail("the "+login+" login drifted before any effect", "set it back"), ErrAdminCandidateUnapplied)}
			_, err := updateLogins(h, login)
			if d := diagnostics.Of(err); len(d) != 1 || !strings.HasPrefix(d[0].Remediation, "set it back, then run ") {
				t.Fatalf("the unapplied drift reported %+v", d)
			}
			if _, held := h.binder.produced[block+"/"+login+"-update"]; held || !slices.Contains(h.binder.journal, "discard "+block+"/"+login+"-update") {
				t.Fatalf("the unapplied candidate was kept: %v", h.binder.producedEntries())
			}
			if err := disclose(); err != nil {
				t.Fatalf("disclosure after an unapplied drift refused: %+v", diagnostics.Of(err))
			}
			if _, err := updateLogins(h, login); err != nil {
				t.Fatalf("the repeat after the drift was restored failed: %+v", diagnostics.Of(err))
			}
			if err := disclose(); err != nil || len(h.binder.versionWrites()) > 1 {
				t.Fatalf("the finished update refused disclosure (%v) or wrote %v", err, h.binder.versionWrites())
			}
		})
	}
}

func withGrafanaDeclaration(h *harness, spec api.Value) {
	var objects []api.Object
	for _, object := range h.service.compiler.(testCompiler).state.Effective().Objects() {
		if object.Kind() != api.Secret || object.Name() != grafanaSecret {
			objects = append(objects, object)
		}
	}
	catalog := api.NewCatalog(append(objects, api.NewObject(api.Secret, grafanaSecret, api.Value{}, spec)))
	h.service.compiler = testCompiler{state: compilation.NewState(catalog, catalog, nil)}
}

func generatedToken(bytes string) api.Value {
	generated := api.MapValue(api.FieldValue{Name: "bytes", Value: api.IntegerValue(bytes)})
	return api.MapValue(api.FieldValue{Name: "type", Value: api.StringValue("token")}, api.FieldValue{Name: "source", Value: api.MapValue(api.FieldValue{Name: "generated", Value: generated})})
}

func TestAGrafanaSecretNoUpdateCanGenerateIsInapplicableBeforeConfirmation(t *testing.T) {
	t.Parallel()
	for name, spec := range map[string]api.Value{
		"certificate":    api.MapValue(api.FieldValue{Name: "type", Value: api.StringValue("tlsCertificate")}),
		"oversize token": generatedToken("97"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h, capability := cephAdminHarness(t)
			withGrafanaDeclaration(h, spec)
			reads := h.binder.producedReads
			_, err := h.service.AdminCredentials().UpdateAdminCredentials(context.Background(), UpdateAdminCredentialsRequest{ContextName: testContextName, Name: "edge", Logins: []string{"grafana"}})
			d := diagnostics.Of(err)
			if len(d) != 1 || d[0].Code != "engine.credential-login-inapplicable" || !strings.Contains(d[0].Message, "grafana is inapplicable because a credential update generates no value of at most 128 visible characters for the Secret "+grafanaSecret) {
				t.Fatalf("an ungenerable Grafana Secret gave %+v", d)
			}
			if h.confirmer.asked != 0 || capability.prepares != 0 || capability.updates != 0 || h.binder.producedReads != reads {
				t.Fatalf("the refusal asked %d times, prepared %d, updated %d and read %d custody entries", h.confirmer.asked, capability.prepares, capability.updates, h.binder.producedReads-reads)
			}
			result, err := updateLogins(h)
			if err != nil || slices.Contains(capability.order, "grafana") || len(result.Logins) != 3 {
				t.Fatalf("the default ran %v (%v)", capability.order, err)
			}
		})
	}
	h, _ := cephAdminHarness(t)
	withGrafanaDeclaration(h, generatedToken("96"))
	if _, err := updateLogins(h, "grafana"); err != nil {
		t.Fatalf("a 96-byte token, 128 characters, refused: %+v", diagnostics.Of(err))
	}
}

func TestACandidatePreparationFailureKeepsItsOwnRemedy(t *testing.T) {
	t.Parallel()
	h, capability := cephAdminHarness(t)
	credentialVersions.Lock()
	credentialVersions.refused[h.binder] = secretstore.Failure("store.conflict", "the keyring is busy")
	credentialVersions.Unlock()
	_, err := updateLogins(h)
	d := diagnostics.Of(err)
	if len(d) != 1 || strings.Contains(d[0].Remediation, "--login") || capability.updates != 0 || capability.prepares != 0 {
		t.Fatalf("a failure before any login ran became %+v after %d updates", d, capability.updates)
	}
}
