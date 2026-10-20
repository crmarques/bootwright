package inspection

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
)

const testFSID = "6b8f1c2e-4d7a-5e3f-9a1b-0c2d4e6f8a1b"

const testPassword = "Xq7-Lm2_Rv9.Tk4-Wz8_Hn3.Pb6-Jd5_"

func testDashboard() Dashboard {
	return Dashboard{Status: DashboardReady, User: "admin", CertificateSHA256: strings.Repeat("c", 64), URLs: []DashboardURL{
		{Host: "rhel-01", URL: "https://rhel-01.lab.test:8443/", Role: DashboardActive}, {Host: "rhel-02", URL: "https://rhel-02.lab.test:8443/", Role: DashboardStandby}}}
}

type fixture struct {
	t         *testing.T
	catalog   api.Catalog
	asked     []string
	cephCalls int
	cephFor   map[string]CephProof
	installed map[string]bool
	installs  int
	proved    map[string]bool
	keyReads  int
	failure   error
	passwords map[string]Custodied
	custody   []string
	keyrings  map[string]Custodied
	clients   []string
	clientErr error
	readError error
	rows      map[string][]Resource
	rowCalls  int
	rowError  error
}

func text(value string) api.Value { return api.StringValue(value) }

func field(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}

func node(name, fqdn, machineRef string) api.Value {
	return api.MapValue(field("name", text(name)), field("fqdn", text(fqdn)), field("machineRef", text(machineRef)))
}

func container(name, distribution string) api.Object {
	return api.NewObject(api.ContainerCluster, name, api.Value{}, api.MapValue(
		field("distribution", api.MapValue(field("type", text(distribution))))))
}

func storageCluster(name, management string, bootstrap string, nodes ...api.Value) api.Object {
	ceph := api.MapValue(
		field("release", text("9.9.1.2")),
		field("cephadm", api.MapValue(field("bootstrap", api.MapValue(field("node", text(bootstrap)))))),
		field("topology", api.MapValue(field("nodes", api.ListValue(nodes...)))),
	)
	return api.NewObject(api.StorageCluster, name, api.Value{}, api.MapValue(
		field("type", text("ceph")), field("management", text(management)), field("ceph", ceph)))
}

func newFixture(t *testing.T) *fixture {
	nodes := []api.Value{
		node("rhel-03", "rhel-03.lab.test", "machine-c"),
		node("rhel-01", "rhel-01.lab.test", "machine-a"),
		node("rhel-02", "rhel-02.lab.test", "machine-b"),
	}
	return &fixture{
		t: t,
		catalog: api.NewCatalog([]api.Object{
			container("okd-lab", "okd"), container("edge", "openshift"),
			storageCluster("storage", "managed", "rhel-01", nodes...),
			storageCluster("archive", "external", ""),
		}),
		cephFor: map[string]CephProof{"storage": {
			FSID: testFSID, Image: Image{Reference: "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9:v9.9.1-25074", Digest: "sha256:" + strings.Repeat("a", 64), Platform: "linux/amd64"},
			Monitors: []Monitor{{Host: "rhel-01", FQDN: "rhel-01.lab.test", Address: "192.0.2.11"}}, Dashboard: testDashboard(),
		}},
		passwords: map[string]Custodied{"storage": {Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(testPassword)})}},
		installed: map[string]bool{"edge": true},
		proved:    map[string]bool{"machine-a": true},
	}
}

func (f *fixture) Effective(_ context.Context, contextName string) (api.Catalog, error) {
	f.asked = append(f.asked, contextName)
	return f.catalog, f.failure
}

func (f *fixture) ManagedCeph(_ context.Context, _ string, _ api.Catalog, cluster api.Object) (CephProof, error) {
	f.cephCalls++
	proof, found := f.cephFor[cluster.Name()]
	if !found {
		return CephProof{Reason: "no apply has completed the cluster's bootstrap"}, nil
	}
	return proof, nil
}

func (f *fixture) Resources(_ context.Context, _ string, _ api.Catalog) (map[string][]Resource, error) {
	f.rowCalls++
	return f.rows, f.rowError
}

func (f *fixture) Installed(_ context.Context, _, cluster string) (bool, error) {
	f.installs++
	return f.installed[cluster], nil
}

func (f *fixture) HostKeyProved(_ context.Context, _, machine string) (bool, error) {
	f.keyReads++
	return f.proved[machine], nil
}

func (f *fixture) DashboardPassword(_ context.Context, contextName, cluster string) (Custodied, bool, error) {
	f.custody = append(f.custody, contextName+"/"+cluster)
	found, ok := f.passwords[cluster]
	return found, ok, f.readError
}

func (f *fixture) ClientKeyring(_ context.Context, contextName, client string) (Custodied, bool, error) {
	f.clients = append(f.clients, contextName+"/"+client)
	found, ok := f.keyrings[client]
	return found, ok, f.clientErr
}

func (f *fixture) service(current string) Service {
	return New(f, f, f, f, func(context.Context) (string, error) { return current, nil }, f)
}

func (f *fixture) untouched() {
	f.t.Helper()
	if f.cephCalls != 0 || f.rowCalls != 0 || f.installs != 0 || f.keyReads != 0 || len(f.custody) != 0 || len(f.clients) != 0 {
		f.t.Fatalf("a refusal read evidence: ceph %d, resources %d, installs %d, host keys %d, custody %v, client custody %v", f.cephCalls, f.rowCalls, f.installs, f.keyReads, f.custody, f.clients)
	}
}

func commandStatuses(commands []AccessCommand) map[string]string {
	statuses := map[string]string{}
	for _, command := range commands {
		statuses[command.Command] = command.Status
	}
	return statuses
}

func commandNames(commands []AccessCommand) []string {
	names := make([]string, 0, len(commands))
	for _, command := range commands {
		names = append(names, command.Command)
	}
	return names
}

func TestListClustersReportsEverySelectedClusterInCanonicalOrder(t *testing.T) {
	for _, explicit := range []string{"", "lab"} {
		f := newFixture(t)
		current := "lab"
		if explicit == "" {
			current = "current"
		}
		result, err := f.service(current).ListClusters(context.Background(), ListClustersRequest{ContextName: explicit})
		if err != nil {
			t.Fatal(err)
		}
		want := []ClusterRef{
			{"archive", KindStorageCluster}, {"edge", KindContainerCluster}, {"okd-lab", KindContainerCluster}, {"storage", KindStorageCluster},
		}
		if result.Context != current || !slices.Equal(result.Clusters, want) {
			t.Fatalf("list = %+v, want %s %v", result, current, want)
		}
		if !slices.Equal(f.asked, []string{current}) {
			t.Fatalf("rendered %v, want %s once", f.asked, current)
		}
		f.untouched()
	}
}

func TestClusterInfoReportsEachKindFromDesiredStateAndEvidence(t *testing.T) {
	f := newFixture(t)
	result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Context != "lab" || len(result.Clusters) != 2 || len(result.Storage) != 2 {
		t.Fatalf("info = %+v", result)
	}
	names := []string{result.Clusters[0].Name, result.Clusters[1].Name, result.Storage[0].Name, result.Storage[1].Name}
	if !slices.Equal(names, []string{"edge", "okd-lab", "archive", "storage"}) {
		t.Fatalf("order = %v", names)
	}
	order := []string{CommandExec, CommandKeyring, CommandKubeconfig, CommandKubectl, CommandOC, CommandRsh}
	for _, entry := range result.Clusters {
		if !slices.Equal(commandNames(entry.AccessCommands), order) || entry.Kind != KindContainerCluster {
			t.Fatalf("%s access = %+v", entry.Name, entry.AccessCommands)
		}
	}
	edge, okd := commandStatuses(result.Clusters[0].AccessCommands), commandStatuses(result.Clusters[1].AccessCommands)
	if edge[CommandKubeconfig] != StatusReady || okd[CommandKubeconfig] != StatusUnavailable ||
		edge[CommandRsh] != StatusNotImplemented || edge[CommandOC] != StatusNotImplemented {
		t.Fatalf("container statuses edge %v okd %v", edge, okd)
	}
	if reason := result.Clusters[1].AccessCommands[2].Reason; !strings.Contains(reason, "bootwright apply --context lab") {
		t.Fatalf("unavailable kubeconfig reason = %q", reason)
	}
	archive := result.Storage[0]
	if archive.Ceph != nil || !slices.Equal(commandNames(archive.AccessCommands), order) {
		t.Fatalf("external entry = %+v", archive)
	}
	for _, command := range archive.AccessCommands {
		if command.Status != StatusNotApplicable || command.Reason == "" || command.Node != "" {
			t.Fatalf("external command = %+v", command)
		}
	}
	managed := result.Storage[1]
	statuses := commandStatuses(managed.AccessCommands)
	if statuses[CommandExec] != StatusReady || statuses[CommandRsh] != StatusReady || statuses[CommandKubeconfig] != StatusNotApplicable ||
		statuses[CommandOC] != StatusNotApplicable || statuses[CommandKubectl] != StatusNotApplicable || statuses[CommandKeyring] != StatusUnavailable {
		t.Fatalf("managed statuses = %v", statuses)
	}
	if managed.AccessCommands[0].Node != "rhel-01" || managed.AccessCommands[5].Node != "rhel-01" || managed.AccessCommands[0].Reason != "" {
		t.Fatalf("managed sessions = %+v", managed.AccessCommands)
	}
	wantCommand := "bootwright cluster exec --context lab --name storage --node rhel-01 -- sudo -n ceph --conf /var/lib/bootwright/ceph/" + testFSID +
		"/ceph.conf --keyring /var/lib/bootwright/ceph/" + testFSID + "/ceph.client.admin.keyring"
	want := ManagedCeph{
		Management: ManagementManaged, Release: "9.9.1.2", FSID: testFSID, Status: StatusReady, AdminNode: "rhel-01", CephCommand: wantCommand,
		Image:     &Image{Reference: "cp.icr.io/cp/ibm-ceph/ceph-9-rhel9:v9.9.1-25074", Digest: "sha256:" + strings.Repeat("a", 64), Platform: "linux/amd64"},
		Monitors:  []Monitor{{Host: "rhel-01", FQDN: "rhel-01.lab.test", Address: "192.0.2.11"}},
		Dashboard: &Dashboard{Status: DashboardReady, User: "admin", CertificateSHA256: strings.Repeat("c", 64), URLs: testDashboard().URLs},
		Resources: []Resource{}, Endpoints: []Endpoint{},
	}
	if !reflect.DeepEqual(*managed.Ceph, want) {
		t.Fatalf("ceph = %+v, want %+v", *managed.Ceph, want)
	}
	if f.cephCalls != 1 {
		t.Fatalf("the external cluster read Ceph evidence: %d calls", f.cephCalls)
	}
	if len(f.custody) != 0 || len(f.clients) != 0 || managed.Ceph.Dashboard.Revealed || len(managed.Ceph.Dashboard.Password.Parts()) != 0 {
		t.Fatalf("cluster info without --secrets read custody %v", f.custody)
	}
}

func TestClusterInfoSelectsOneNameAndNeverAnExcludedOne(t *testing.T) {
	f := newFixture(t)
	service := f.service("lab")
	result, err := service.ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}})
	if err != nil || len(result.Clusters) != 0 || len(result.Storage) != 1 || result.Storage[0].Name != "storage" {
		t.Fatalf("selected = %+v (%v)", result, err)
	}
	result, err = service.ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"edge"}})
	if err != nil || len(result.Clusters) != 1 || len(result.Storage) != 0 {
		t.Fatalf("selected container = %+v (%v)", result, err)
	}
	for _, name := range []string{"excluded-cluster", "Storage"} {
		reads := f.cephCalls + f.installs + f.keyReads
		_, err := service.ClusterInfo(context.Background(), ClusterInfoRequest{ContextName: "other", Names: []string{name}})
		found := diagnostics.Of(err)
		if len(found) != 1 || found[0].Code != "access.target" || !strings.Contains(found[0].Message, "other") || !strings.Contains(found[0].Remediation, "bootwright cluster info --context other") {
			t.Fatalf("%s refusal = %v", name, err)
		}
		if f.cephCalls+f.installs+f.keyReads != reads {
			t.Fatalf("%s refusal read evidence", name)
		}
	}
}

func TestMissingEvidenceReportsUnavailableWithTheApplyRemedy(t *testing.T) {
	f := newFixture(t)
	delete(f.cephFor, "storage")
	delete(f.proved, "machine-a")
	f.installed = map[string]bool{}
	result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{})
	if err != nil {
		t.Fatal(err)
	}
	ceph := result.Storage[1].Ceph
	if ceph.Status != StatusUnavailable || ceph.Reason == "" || ceph.Remedy != "bootwright apply --context lab --stage clusters" ||
		ceph.FSID != "" || ceph.Image != nil || len(ceph.Monitors) != 0 || ceph.CephCommand != "" || ceph.AdminNode != "rhel-01" {
		t.Fatalf("unavailable ceph = %+v", *ceph)
	}
	for _, command := range result.Storage[1].AccessCommands {
		if command.Command == CommandExec || command.Command == CommandRsh {
			if command.Status != StatusUnavailable || command.Node != "rhel-01" || !strings.Contains(command.Reason, "machine-a") ||
				!strings.Contains(command.Reason, "bootwright apply --context lab --stage machines") {
				t.Fatalf("session without a host key = %+v", command)
			}
		}
	}
	if status := commandStatuses(result.Clusters[0].AccessCommands)[CommandKubeconfig]; status != StatusUnavailable {
		t.Fatalf("edge kubeconfig = %s", status)
	}
}

func TestIncompleteProofsNeverReportReady(t *testing.T) {
	for name, edit := range map[string]func(*fixture){
		"no fsid": func(f *fixture) { f.cephFor["storage"] = CephProof{} },
		"no bootstrap": func(f *fixture) {
			f.catalog = api.NewCatalog([]api.Object{storageCluster("storage", "managed", "", node("rhel-01", "a", "machine-a"))})
		},
	} {
		f := newFixture(t)
		edit(f)
		result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if ceph := result.Storage[0].Ceph; ceph.Status != StatusUnavailable || ceph.CephCommand != "" || ceph.Reason == "" {
			t.Fatalf("%s: ceph = %+v", name, *ceph)
		}
	}
}

func TestAClusterWithoutNodesOpensNoSession(t *testing.T) {
	f := newFixture(t)
	f.catalog = api.NewCatalog([]api.Object{storageCluster("storage", "managed", "rhel-01")})
	result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}})
	if err != nil {
		t.Fatal(err)
	}
	statuses := commandStatuses(result.Storage[0].AccessCommands)
	if statuses[CommandRsh] != StatusUnavailable || statuses[CommandExec] != StatusUnavailable || f.keyReads != 0 {
		t.Fatalf("statuses %v after %d host key reads", statuses, f.keyReads)
	}
}

func TestSecretsRefusalsSettleTheTargetBeforeAnyEvidenceRead(t *testing.T) {
	tests := []struct {
		name, code string
		contains   []string
	}{
		{"edge", "cluster.not-applicable", []string{"bootwright cluster kubeconfig", "ContainerCluster"}},
		{"okd-lab", "cluster.not-applicable", []string{"bootwright cluster kubeconfig"}},
		{"archive", "cluster.not-applicable", []string{"external Ceph StorageCluster", "managed Ceph StorageClusters"}},
		{"missing", "access.target", []string{"selects no ContainerCluster or StorageCluster named missing"}},
		{"", "access.target", []string{"requires one cluster name"}},
	}
	for _, tt := range tests {
		f := newFixture(t)
		result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{tt.name}, Secrets: true})
		found := diagnostics.Of(err)
		if result != nil || len(found) != 1 || found[0].Code != tt.code {
			t.Fatalf("%q: result %+v, err %v, want %s", tt.name, result, err, tt.code)
		}
		for _, want := range tt.contains {
			if !strings.Contains(found[0].Message, want) {
				t.Fatalf("%q: message %q lacks %q", tt.name, found[0].Message, want)
			}
		}
		f.untouched()
	}
}

func TestEvidenceFailuresPropagateInsteadOfReportingUnavailable(t *testing.T) {
	f := newFixture(t)
	f.failure = errors.New("the graph cannot be compiled")
	if _, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{}); !errors.Is(err, f.failure) {
		t.Fatalf("graph failure = %v", err)
	}
	f.untouched()
	none := New(newFixture(t), nil, nil, nil, nil, nil)
	if _, err := none.ListClusters(context.Background(), ListClustersRequest{ContextName: "lab"}); err == nil {
		t.Fatal("a service missing its evidence ports listed clusters")
	}
	missing := newFixture(t)
	if _, err := New(missing, missing, missing, missing, nil, missing).ListClusters(context.Background(), ListClustersRequest{}); diagnostics.Of(err) == nil || diagnostics.Of(err)[0].Code != "context.state" {
		t.Fatalf("no selection = %v", err)
	}
}

func revealed(t *testing.T, d *Dashboard) string {
	t.Helper()
	value, ok := d.Password.Part(secrets.ValuePart)
	if !ok || !d.Revealed {
		t.Fatalf("no revealed password in %+v", d)
	}
	return string(value)
}

func TestSecretsRevealTheCustodiedDashboardPasswordAfterTheTargetSettles(t *testing.T) {
	f := newFixture(t)
	result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, Secrets: true})
	if err != nil {
		t.Fatal(err)
	}
	dashboard := result.Storage[0].Ceph.Dashboard
	if revealed(t, dashboard) != testPassword || dashboard.Warn || dashboard.Status != DashboardReady || !slices.Equal(f.custody, []string{"lab/storage"}) {
		t.Fatalf("reveal = %+v after custody reads %v", dashboard, f.custody)
	}
	if len(result.Clusters) != 0 || len(result.Storage) != 1 {
		t.Fatalf("--secrets reported more than the named cluster: %+v", result)
	}
}

func TestSecretsRevealAnUnprovedCopyWithAWarning(t *testing.T) {
	for name, edit := range map[string]func(*fixture){
		"custody copy never proved": func(f *fixture) {
			copied := f.passwords["storage"]
			copied.Unproved = true
			f.passwords["storage"] = copied
		},
		"dashboard never reached": func(f *fixture) {
			proof := f.cephFor["storage"]
			proof.Dashboard.Status, proof.Dashboard.Reason = DashboardUnproved, "the controller-side TLS connect failed"
			f.cephFor["storage"] = proof
		},
		"cluster evidence missing": func(f *fixture) { delete(f.cephFor, "storage") },
	} {
		f := newFixture(t)
		edit(f)
		result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, Secrets: true})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		dashboard := result.Storage[0].Ceph.Dashboard
		if revealed(t, dashboard) != testPassword || !dashboard.Warn || dashboard.Status != DashboardUnproved || dashboard.Reason == "" || dashboard.User != "admin" {
			t.Fatalf("%s: reveal = %+v", name, dashboard)
		}
	}
}

func TestSecretsWithoutCustodyFailUnavailableAndReadFailuresPropagate(t *testing.T) {
	f := newFixture(t)
	delete(f.passwords, "storage")
	_, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, Secrets: true})
	found := diagnostics.Of(err)
	if len(found) != 1 || found[0].Code != "access.unavailable" || !strings.Contains(found[0].Message, "StorageCluster storage") ||
		found[0].Remediation != "bootwright apply --context lab --stage clusters" || len(f.custody) != 1 {
		t.Fatalf("missing custody = %v after %v", err, f.custody)
	}
	f = newFixture(t)
	f.readError = errors.New("the custody store is unreadable")
	if _, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, Secrets: true}); !errors.Is(err, f.readError) || len(f.custody) != 1 {
		t.Fatalf("read failure = %v", err)
	}
	unwired := newFixture(t)
	service := New(unwired, unwired, unwired, unwired, func(context.Context) (string, error) { return "lab", nil }, nil)
	if _, err := service.ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage"}, Secrets: true}); err == nil {
		t.Fatal("a service without custody revealed a password")
	}
}

func TestClusterInfoSelectsExactlyTheListedClusters(t *testing.T) {
	tests := []struct {
		name              string
		names             []string
		containers, store []string
	}{
		{name: "omitted selects every cluster", containers: []string{"edge", "okd-lab"}, store: []string{"archive", "storage"}},
		{name: "a list of both kinds", names: []string{"storage", "edge"}, containers: []string{"edge"}, store: []string{"storage"}},
		{name: "a list keeps canonical order whatever its own", names: []string{"storage", "archive", "okd-lab", "edge"}, containers: []string{"edge", "okd-lab"}, store: []string{"archive", "storage"}},
		{name: "members are trimmed and deduplicated", names: []string{" storage ", "storage", ""}, store: []string{"storage"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: tt.names})
			if err != nil {
				t.Fatal(err)
			}
			var containers, store []string
			for _, entry := range result.Clusters {
				containers = append(containers, entry.Name)
			}
			for _, entry := range result.Storage {
				store = append(store, entry.Name)
			}
			if !slices.Equal(containers, tt.containers) || !slices.Equal(store, tt.store) {
				t.Fatalf("selected %v and %v, want %v and %v", containers, store, tt.containers, tt.store)
			}
			if !slices.Equal(f.asked, []string{"lab"}) {
				t.Fatalf("rendered %v, want once", f.asked)
			}
		})
	}
}

func TestClusterInfoNamesEveryUnknownMemberInOneRefusal(t *testing.T) {
	tests := []struct {
		names   []string
		message string
	}{
		{names: []string{"zed", "storage", "missing"}, message: "the context lab selects no ContainerClusters or StorageClusters named missing, zed"},
		{names: []string{"missing", "storage"}, message: "the context lab selects no ContainerCluster or StorageCluster named missing"},
	}
	for _, tt := range tests {
		for _, reveal := range []bool{false, true} {
			f := newFixture(t)
			result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: tt.names, Secrets: reveal})
			found := diagnostics.Of(err)
			if result != nil || len(found) != 1 || found[0].Code != "access.target" || found[0].Message != tt.message {
				t.Fatalf("%v (secrets %v): result %+v, err %v, want access.target %q", tt.names, reveal, result, err, tt.message)
			}
			f.untouched()
		}
	}
}

func TestSecretsRevealsOneClusterNeverSeveral(t *testing.T) {
	f := newFixture(t)
	result, err := f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{"storage", "archive"}, Secrets: true})
	found := diagnostics.Of(err)
	if result != nil || len(found) != 1 || found[0].Code != "cli.usage" || !strings.Contains(found[0].Message, "name one cluster") {
		t.Fatalf("several with --secrets: result %+v, err %v", result, err)
	}
	f.untouched()
	result, err = f.service("lab").ClusterInfo(context.Background(), ClusterInfoRequest{Names: []string{" storage", "storage"}, Secrets: true})
	if err != nil || len(result.Storage) != 1 || !result.Storage[0].Ceph.Dashboard.Revealed || !slices.Equal(f.custody, []string{"lab/storage"}) {
		t.Fatalf("one member twice with --secrets: %+v (%v), custody %v", result, err, f.custody)
	}
}
