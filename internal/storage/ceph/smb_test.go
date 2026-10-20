package ceph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const smbSentinel = "smb-sentinel-Qv8z31"

func smbObject(name string, pairs ...any) api.Object {
	return childOf(api.StorageSMBResource, name, pairs...)
}

func smbUsers() api.Object {
	users := api.ListValue(requestMap("name", "alice"), requestMap("name", "bob"))
	return smbObject("smb-users", "secrets", api.ListValue(requestMap("path", "/values/users/1/password", "secretRef", "bob-password"), requestMap("path", "/values/users/0/password", "secretRef", "alice-password")),
		"native", requestMap("resource_type", api.SMBUsersGroups, "users_groups_id", "users", "values", requestMap("users", users, "groups", api.ListValue())))
}

func smbJoin() api.Object {
	return smbObject("smb-join", "secrets", api.ListValue(requestMap("path", "/auth/password", "secretRef", "join-password")),
		"native", requestMap("resource_type", api.SMBJoinAuth, "auth_id", "join", "auth", requestMap("username", "Administrator")))
}

func smbFiles(placement api.Value, native ...any) api.Object {
	pairs := append([]any{"resource_type", api.SMBCluster, "cluster_id", "files", "auth_mode", "user",
		"user_group_settings", api.ListValue(requestMap("source_type", "resource", "ref", "users")), "placement", placement}, native...)
	return smbObject("smb-files", "resourceRefs", api.StringList("smb-users"), "native", requestMap(pairs...))
}

func smbDocs(backing ...any) api.Object {
	if len(backing) == 0 {
		backing = []any{"subvolumeRef", "docs"}
	}
	return smbObject("smb-docs", append([]any{"smbClusterRef", "smb-files", "native", requestMap("resource_type", api.SMBShare, "cluster_id", "files", "share_id", "docs", "name", "Docs",
		"cephfs", requestMap("path", "/"))}, backing...)...)
}

func smbCatalog(t *testing.T, subvolume api.Object, objects ...api.Object) api.Catalog {
	t.Helper()
	children := append([]api.Object{cephfsPool("fs-meta", "cephfs-metadata"), cephfsPool("fs-data", "cephfs-data"), filesystemOf(), subvolume}, objects...)
	return api.NewCatalog(append(pinnedCatalog(t, withMDS).Objects(), children...))
}

func smbGraph(t *testing.T) api.Catalog {
	t.Helper()
	return smbCatalog(t, subvolumeOf(), smbUsers(), smbFiles(requestMap("label", "mon", "count", 2)), smbDocs())
}

func smbBase(t *testing.T, catalog api.Catalog) Request {
	t.Helper()
	cluster, _ := catalog.Find(api.StorageCluster, "example")
	base, err := baseRequest(catalog, cluster, "synthetic")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return base
}

func builtSMB(t *testing.T, catalog api.Catalog, base Request, name string) SMBRequest {
	t.Helper()
	object, found := catalog.Find(api.StorageSMBResource, name)
	if !found {
		t.Fatalf("the catalog has no StorageSMBResource %s", name)
	}
	r, err := smbRequest(catalog, object, base, "controller")
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return r
}

func smbDefinitions(t *testing.T, catalog api.Catalog, verb reconciliation.Verb) map[string]reconciliation.BlockDefinition {
	t.Helper()
	plan, err := NewSMB(nil).Plan(context.Background(), lifecycle.PlanInput{Verb: verb, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	out := map[string]reconciliation.BlockDefinition{}
	for _, d := range plan.Definitions {
		out[d.ID] = d
	}
	return out
}

func smbPlan(t *testing.T, catalog api.Catalog) reconciliation.Plan {
	t.Helper()
	definitions := cephDefinitions(t, catalog)
	for _, d := range smbDefinitions(t, catalog, reconciliation.Apply) {
		definitions = append(definitions, d)
	}
	seen := map[reconciliation.ObjectRef]bool{}
	for _, definition := range slices.Clone(definitions) {
		for _, ref := range definition.Requires {
			if (ref.Kind == string(api.Machine) || ref.Kind == "MachineInstallProfile") && !seen[ref] {
				seen[ref] = true
				definitions = append(definitions, reconciliation.BlockDefinition{ID: strings.ToLower(ref.Kind) + "-" + ref.Object, Description: "synthetic " + ref.Object, Stage: reconciliation.StageMachines, Kind: ref.Kind, Object: ref.Object, Implementation: "synthetic", ContentDigest: strings.Repeat("f", 64), Request: json.RawMessage(`{}`)})
			}
		}
	}
	plan, err := reconciliation.NewPlan(reconciliation.Apply, definitions)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	return plan
}

func sambaAuxiliary() []AuxiliaryImage {
	images := []AuxiliaryImage{}
	for _, name := range []string{"haproxy", "samba", "samba_metrics"} {
		base := PinnedRegistry + "/synthetic/" + name
		images = append(images, AuxiliaryImage{Name: name, Base: base, Tag: "synthetic-1", Image: base + ":synthetic-1"})
	}
	return images
}

func sambaDigests() []string {
	return []string{"sha256:" + strings.Repeat("1", 64), "sha256:" + strings.Repeat("2", 64), "sha256:" + strings.Repeat("3", 64)}
}

func coreClusterProof(t *testing.T, r Request, change func(*Evidence)) lifecycle.BlockEvidence {
	t.Helper()
	data, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: ClusterBlockID(r.Cluster), Kind: string(api.StorageCluster), Object: r.Cluster, Implementation: Implementation, Request: data}, RequestDigest: "digest"}
	runtime := r
	runtime.Owner, runtime.FSID = "apply-original", FSIDFor("apply-original", r.Cluster)
	e := readyEvidence(runtime)
	if change != nil {
		change(&e)
	}
	return lifecycle.BlockEvidence{Block: block, Kind: block.Kind, Object: block.Object, Implementation: Implementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: evidenceBytes(t, e)}
}

func sambaCore() Request {
	r := coreRequest()
	r.Auxiliary = authoredReferences(sambaAuxiliary())
	return r
}

func sambaProofs(t *testing.T) []lifecycle.BlockEvidence {
	t.Helper()
	r := sambaCore()
	return []lifecycle.BlockEvidence{coreClusterProof(t, r, nil), auxiliaryProof(t, r.Image, sambaAuxiliary(), sambaDigests()...)}
}

func smbBlock(t *testing.T, r SMBRequest) reconciliation.Block {
	t.Helper()
	canonical, err := r.Canonical()
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	definition := reconciliation.BlockDefinition{ID: SMBBlockID(r.Object), Kind: string(api.StorageSMBResource), Object: r.Object, Implementation: SMBImplementation, ContentDigest: SMBContentDigest(), Request: canonical}
	digest, err := reconciliation.RequestDigest(definition)
	if err != nil {
		t.Fatal(err)
	}
	return reconciliation.Block{BlockDefinition: definition, RequestDigest: digest}
}

func smbExecution(t *testing.T, r SMBRequest, value string) lifecycle.Execution {
	t.Helper()
	e := lifecycle.Execution{ApplyOperation: "apply-original", Operation: "apply-original", Context: "example", Block: smbBlock(t, r), ApplyProved: sambaProofs(t)}
	e = withMaterial(e, sambaCore())
	for _, b := range r.Bindings {
		e.Material[b.SecretRef] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(value)})
	}
	return e
}

func smbReady(t *testing.T, e lifecycle.Execution, r SMBRequest, change func(*SMBEvidence)) json.RawMessage {
	t.Helper()
	evidence := SMBEvidence{Version: SMBEvidenceVersion, Request: e.Block.RequestDigest, Owner: "apply-original", FSID: FSIDFor("apply-original", r.Cluster), Owned: true, Exists: true, Ready: true,
		Resource: smbResourceName(r), Daemons: []SMBDaemon{}}
	if r.ResourceType == api.SMBShare {
		evidence.Daemons = []SMBDaemon{{Host: r.Service.Hosts[0], Name: "smb.files.0.0.node-1.abcdef", Service: r.Service.Name}}
	}
	if change != nil {
		change(&evidence)
	}
	data, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func sambaBase(t *testing.T, catalog api.Catalog) Request {
	t.Helper()
	base := smbBase(t, catalog)
	base.Auxiliary = authoredReferences(sambaAuxiliary())
	return base
}

func TestSMBRequestGolden(t *testing.T) {
	catalog := smbCatalog(t, subvolumeOf("group", "csi"), smbUsers(), smbJoin(),
		smbFiles(requestMap("label", "mon", "count", 2), "clustering", "always", "public_addrs", api.ListValue(requestMap("address", "192.0.2.80/24"))), smbDocs())
	base := smbBase(t, catalog)
	base.Auxiliary = []AuxiliaryReference{{Name: "samba", Image: PinnedRegistry + "/synthetic/samba@sha256:" + strings.Repeat("a", 64)}, {Name: "samba_metrics", Image: PinnedRegistry + "/synthetic/samba-metrics@sha256:" + strings.Repeat("b", 64)}}
	for golden, name := range map[string]string{"request-smb-usersgroups": "smb-users", "request-smb-join-auth": "smb-join", "request-smb-cluster": "smb-files", "request-smb-share": "smb-docs"} {
		t.Run(golden, func(t *testing.T) {
			canonical, err := builtSMB(t, catalog, base, name).Canonical()
			if err != nil {
				t.Fatal(diagnostics.Of(err))
			}
			matchesGolden(t, golden, canonical, false)
			decoded, err := DecodeSMBRequest(canonical)
			if err != nil {
				t.Fatal(diagnostics.Of(err))
			}
			if again, err := decoded.Canonical(); err != nil || string(again) != string(canonical) {
				t.Fatalf("the request does not round-trip: %s (%v)", again, err)
			}
		})
	}
}

func withNativeLeaf(t *testing.T, native json.RawMessage, name, value string) json.RawMessage {
	t.Helper()
	members := map[string]any{}
	if err := json.Unmarshal(native, &members); err != nil {
		t.Fatal(err)
	}
	members[name] = value
	data, err := json.Marshal(members)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSMBRequestDecodeRefusesInconsistentFrozenRequests(t *testing.T) {
	catalog := smbCatalog(t, subvolumeOf(), smbUsers(), smbJoin(), smbFiles(requestMap("label", "mon", "count", 2)), smbDocs())
	base := sambaBase(t, catalog)
	cases := map[string]SMBRequest{}
	users := builtSMB(t, catalog, base, "smb-users")
	share := builtSMB(t, catalog, base, "smb-docs")
	join := builtSMB(t, catalog, base, "smb-join")
	add := func(name string, r SMBRequest, change func(*SMBRequest)) {
		r.Bindings, r.Machines, r.ClusterIDs, r.Images = slices.Clone(r.Bindings), slices.Clone(r.Machines), slices.Clone(r.ClusterIDs), slices.Clone(r.Images)
		if r.Service != nil {
			service := *r.Service
			service.Hosts = slices.Clone(r.Service.Hosts)
			r.Service = &service
		}
		change(&r)
		cases[name] = r
	}
	add("a scope", users, func(r *SMBRequest) { r.Scope = json.RawMessage(`{"cluster":"example"}`) })
	add("another type", users, func(r *SMBRequest) { r.ResourceType = api.SMBJoinAuth })
	add("another id", users, func(r *SMBRequest) { r.ID = "other" })
	add("no machines", users, func(r *SMBRequest) { r.Machines = []string{} })
	add("unsorted machines", users, func(r *SMBRequest) { slices.Reverse(r.Machines) })
	add("unsorted bindings", users, func(r *SMBRequest) { slices.Reverse(r.Bindings) })
	add("a foreign binding path", users, func(r *SMBRequest) { r.Bindings[0].Path = "/values/users/0/name" })
	add("a service on users", users, func(r *SMBRequest) { r.Service = share.Service })
	add("a share binding", share, func(r *SMBRequest) { r.Bindings = users.Bindings })
	add("a foreign service", share, func(r *SMBRequest) { r.Service.Name = "smb.other" })
	add("unsorted service hosts", share, func(r *SMBRequest) { slices.Reverse(r.Service.Hosts) })
	add("reversed images", share, func(r *SMBRequest) { slices.Reverse(r.Images) })
	add("one image", share, func(r *SMBRequest) { r.Images = r.Images[:1] })
	add("a long object", users, func(r *SMBRequest) { r.Object = strings.Repeat("a", 55) })
	add("a service naming another listed cluster", share, func(r *SMBRequest) { r.ClusterIDs, r.Service.Name = []string{"files", "other"}, "smb.other" })
	add("a cluster list without its own cluster", share, func(r *SMBRequest) { r.ClusterIDs = []string{"other"} })
	add("a join auth at another path", join, func(r *SMBRequest) { r.Bindings[0].Path = "/auth/username" })
	add("a join auth with two bindings", join, func(r *SMBRequest) {
		r.Bindings = append(r.Bindings, SMBBinding{Path: "/auth/password", SecretRef: "other-password"})
	})
	add("a join auth without a Secret", join, func(r *SMBRequest) { r.Bindings[0].SecretRef = "" })
	add("users without a binding", users, func(r *SMBRequest) { r.Bindings = []SMBBinding{} })
	add("another native resource type", users, func(r *SMBRequest) { r.Native = withNativeLeaf(t, r.Native, "resource_type", api.SMBJoinAuth) })
	for name, r := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := reconciliation.Freeze(r, "Ceph SMB")
			if err != nil {
				t.Fatal(err)
			}
			_, err = DecodeSMBRequest(data)
			refusedWith(t, err, "lifecycle.state", "the frozen Ceph SMB request is inconsistent", "")
		})
	}
}

func TestSMBBlocksOrderAuthThenClusterThenShare(t *testing.T) {
	plan := smbPlan(t, smbGraph(t))
	removal, err := plan.Inverse()
	if err != nil {
		t.Fatal(err)
	}
	apply, inverse := cephIDs(plan), cephIDs(removal)
	order := []string{"ceph-smb-smb-users", "ceph-smb-smb-files", "ceph-smb-smb-docs"}
	for i := 1; i < len(order); i++ {
		if slices.Index(apply, order[i-1]) > slices.Index(apply, order[i]) || slices.Index(inverse, order[i-1]) < slices.Index(inverse, order[i]) {
			t.Fatalf("%s and %s are misordered: apply %v, removal %v", order[i-1], order[i], apply, inverse)
		}
	}
	if slices.Index(apply, "ceph-subvolume-docs") > slices.Index(apply, "ceph-smb-smb-docs") || slices.Index(inverse, "ceph-subvolume-docs") < slices.Index(inverse, "ceph-smb-smb-docs") {
		t.Fatalf("the share is not ordered after its subvolume: apply %v, removal %v", apply, inverse)
	}
}

func TestSMBShareRequiresItsSubvolumeOrFilesystemBlock(t *testing.T) {
	smbCluster := reconciliation.ObjectRef{Kind: string(api.StorageSMBResource), Object: "smb-files"}
	for name, test := range map[string]struct {
		share api.Object
		want  reconciliation.ObjectRef
	}{
		"subvolume":  {smbDocs(), reconciliation.ObjectRef{Kind: string(api.StorageSubvolume), Object: "docs"}},
		"filesystem": {smbDocs("filesystemRef", "files"), reconciliation.ObjectRef{Kind: string(api.StorageFilesystem), Object: "files"}},
	} {
		t.Run(name, func(t *testing.T) {
			catalog := smbCatalog(t, subvolumeOf(), smbUsers(), smbFiles(requestMap("label", "mon", "count", 2)), test.share)
			share := smbDefinitions(t, catalog, reconciliation.Apply)["ceph-smb-smb-docs"]
			want := []reconciliation.ObjectRef{smbCluster, test.want}
			slices.SortFunc(want, func(a, b reconciliation.ObjectRef) int {
				return strings.Compare(a.Kind+"/"+a.Object, b.Kind+"/"+b.Object)
			})
			if !slices.Equal(share.Requires, want) {
				t.Fatalf("the share requires %v, want %v", share.Requires, want)
			}
		})
	}
}

func TestSMBBlocksDependOnSettingsBlockByID(t *testing.T) {
	catalog := smbGraph(t)
	definitions := smbDefinitions(t, catalog, reconciliation.Apply)
	if len(definitions) != 3 {
		t.Fatalf("planned %d SMB blocks", len(definitions))
	}
	for id, d := range definitions {
		if !slices.Equal(d.Dependencies, []string{"ceph-cluster-example", "ceph-image-example", "ceph-settings-example"}) {
			t.Errorf("%s depends on %v", id, d.Dependencies)
		}
		for _, ref := range d.Requires {
			if ref.Kind == string(api.StorageCluster) {
				t.Errorf("%s requires StorageCluster %s", id, ref.Object)
			}
		}
		if d.Stage != reconciliation.StageClusters || !slices.Equal(d.Exclusive, []string{"ceph/example"}) || d.Kind != string(api.StorageSMBResource) || d.Implementation != SMBImplementation || d.ContentDigest != SMBContentDigest() {
			t.Errorf("%s is %+v", id, d)
		}
	}
	settings := blockOf(t, smbPlan(t, catalog), "ceph-settings-example")
	if r, err := DecodeRequest(settings.Request); err != nil || r.Settings == nil || !slices.Contains(r.Settings.Modules, SMBManagerModule) {
		t.Fatalf("the settings request is %+v (%v)", r, err)
	}
}

func TestSMBDerivesTheSMBManagerModule(t *testing.T) {
	modules := func(catalog api.Catalog) []string {
		found := settingsDefinitions(cephDefinitions(t, catalog))
		if len(found) == 0 {
			return nil
		}
		r, err := DecodeRequest(found[0].Request)
		if err != nil || r.Settings == nil {
			t.Fatalf("the settings request is %+v (%v)", r, err)
		}
		return r.Settings.Modules
	}
	if got := modules(settingsCatalog(t, nil, smbJoin())); !slices.Equal(got, []string{"smb"}) {
		t.Fatalf("a join-auth-only cluster derives %v", got)
	}
	if got := modules(settingsCatalog(t, []api.Value{nfsService("files")}, smbJoin())); !slices.Equal(got, []string{"nfs", "smb"}) {
		t.Fatalf("a cluster with nfs and SMB derives %v", got)
	}
	if got := modules(settingsCatalog(t, nil)); got != nil {
		t.Fatalf("a cluster without SMB derives %v", got)
	}
	if got := modules(settingsCatalog(t, []api.Value{nfsService("files")})); !slices.Equal(got, []string{"nfs"}) {
		t.Fatalf("a cluster with nfs alone derives %v", got)
	}
}

func TestSMBRequestCarriesDescriptorsNeverSecretBytes(t *testing.T) {
	catalog := smbCatalog(t, subvolumeOf(), smbUsers(), smbJoin())
	base := smbBase(t, catalog)
	for _, name := range []string{"smb-users", "smb-join"} {
		t.Run(name, func(t *testing.T) {
			r := builtSMB(t, catalog, base, name)
			e := smbExecution(t, r, smbSentinel)
			runner := &recordingRunner{}
			runner.result = lifecycle.RunResult{Outcome: "changed", Evidence: smbReady(t, e, r, nil)}
			result, err := NewSMB(runner).Apply(context.Background(), e)
			if err != nil || result.Outcome != reconciliation.OutcomeChanged || runner.calls != 1 {
				t.Fatalf("apply = %+v (%v), calls %d", result, diagnostics.Of(err), runner.calls)
			}
			for label, data := range map[string][]byte{"frozen": e.Block.Request, "sent": runner.request.Canonical} {
				if strings.Contains(string(data), smbSentinel) {
					t.Fatalf("the %s request holds the secret value: %s", label, data)
				}
				for _, b := range r.Bindings {
					if !strings.Contains(string(data), `"secretRef":"`+b.SecretRef+`"`) {
						t.Fatalf("the %s request does not name %s", label, b.SecretRef)
					}
				}
			}
			want := []lifecycle.MaterialFile{{Name: "fleet-key", Secret: "fleet-key", Part: secrets.PrivateKeyPart, Variable: "sshKey"}, {Name: "known-hosts", Secret: knownHostsMaterial, Part: secrets.ValuePart, Variable: "knownHosts"}}
			for i, b := range r.Bindings {
				index := string(rune('0' + i))
				want = append(want, lifecycle.MaterialFile{Name: "smb-secret-" + index, Secret: b.SecretRef, Part: secrets.ValuePart, Variable: "smbSecret" + index})
			}
			if got := runner.request.Materials; !slices.Equal(got, want) {
				t.Fatalf("material files %+v, want %+v", got, want)
			}
			if runner.request.Implementation != SMBImplementation || runner.request.Variable != "bootwright_ceph_smb" || runner.request.Operation != "apply" || !runner.request.NativeErrors || runner.request.Placement.Machine != "controller" {
				t.Fatalf("the invocation is %+v", runner.request)
			}
		})
	}
}

func TestSMBSecretValueRefusesBeforeTheRun(t *testing.T) {
	catalog := smbCatalog(t, subvolumeOf(), smbJoin())
	r := builtSMB(t, catalog, smbBase(t, catalog), "smb-join")
	for name, value := range map[string]string{"carriage return": "pass\rword", "line feed": "pass\nword", "nul": "pass\x00word", "invalid UTF-8": "pass\xffword", "empty": "", "too long": strings.Repeat("p", 1025)} {
		t.Run(name, func(t *testing.T) {
			runner := &recordingRunner{}
			result, err := NewSMB(runner).Apply(context.Background(), smbExecution(t, r, value))
			refusedWith(t, err, "lifecycle.unsupported", "the Secret join-password that StorageSMBResource smb-join binds at /auth/password is not one line of 1 to 1024 bytes of UTF-8 text",
				"store the password in Secret join-password as one line of 1 to 1024 bytes with bootwright secret set, then apply again")
			if runner.calls != 0 || result.Outcome == reconciliation.OutcomeChanged || value != "" && strings.Contains(diagnostics.Of(err)[0].Message, value) {
				t.Fatalf("the refusal ran the adapter or names the value: %+v calls %d", result, runner.calls)
			}
		})
	}
	runner := &recordingRunner{}
	e := smbExecution(t, r, strings.Repeat("p", 1024))
	runner.result = lifecycle.RunResult{Outcome: "unchanged", Evidence: smbReady(t, e, r, nil)}
	if result, err := NewSMB(runner).Apply(context.Background(), e); err != nil || result.Outcome != reconciliation.OutcomeUnchanged || runner.calls != 1 {
		t.Fatalf("a 1024-byte password = %+v (%v)", result, diagnostics.Of(err))
	}
}

func TestSMBContentDigestMovesWithItsContract(t *testing.T) {
	if SMBImplementation != "storage-ceph-smb-v1" || SMBVersion != "ceph-smb-v1" || SMBEvidenceVersion != "ceph-smb-evidence-v1" || SMBManagerModule != "smb" {
		t.Fatalf("the SMB contract is %s %s %s %s", SMBImplementation, SMBVersion, SMBEvidenceVersion, SMBManagerModule)
	}
	d := sha256.Sum256([]byte("storage-ceph-smb-v1\x00ceph-smb-v1\x00ceph-smb-evidence-v1"))
	if SMBContentDigest() != hex.EncodeToString(d[:]) {
		t.Fatalf("the SMB content digest is %s", SMBContentDigest())
	}
	for _, other := range []string{ContentDigest(), HostContentDigest(), ImageContentDigest()} {
		if other == SMBContentDigest() {
			t.Fatal("the SMB content digest equals another Ceph digest")
		}
	}
}

func TestSMBRequestsCarryTheFrozenSambaDigests(t *testing.T) {
	catalog := smbCatalog(t, subvolumeOf(), smbUsers(), smbJoin(), smbFiles(requestMap("label", "mon", "count", 2)), smbDocs())
	base := sambaBase(t, catalog)
	want := []SMBImage{{Name: "samba", Image: PinnedRegistry + "/synthetic/samba:synthetic-1"}, {Name: "samba_metrics", Image: PinnedRegistry + "/synthetic/samba_metrics:synthetic-1"}}
	completed := []SMBImage{{Name: "samba", Image: PinnedRegistry + "/synthetic/samba@" + sambaDigests()[1]}, {Name: "samba_metrics", Image: PinnedRegistry + "/synthetic/samba_metrics@" + sambaDigests()[2]}}
	for _, name := range []string{"smb-files", "smb-docs"} {
		r := builtSMB(t, catalog, base, name)
		if !slices.Equal(r.Images, want) {
			t.Fatalf("%s froze images %+v", name, r.Images)
		}
		prepared, scope, err := NewSMB(nil).prepare(context.Background(), smbExecution(t, r, "pw"))
		if err != nil || !slices.Equal(prepared.Images, completed) || scope.Scope.Owner != "apply-original" || len(prepared.Scope) == 0 {
			t.Fatalf("%s prepared %+v (%v)", name, prepared.Images, diagnostics.Of(err))
		}
		unfrozen := r
		unfrozen.Images = nil
		_, _, err = NewSMB(nil).prepare(context.Background(), smbExecution(t, unfrozen, "pw"))
		refusedWith(t, err, "lifecycle.state", "StorageSMBResource "+name+" froze no samba image", "")
		foreign := r
		foreign.Images = []SMBImage{want[0], {Name: "samba_metrics", Image: PinnedRegistry + "/synthetic/other:synthetic-1"}}
		_, _, err = NewSMB(nil).prepare(context.Background(), smbExecution(t, foreign, "pw"))
		refusedWith(t, err, "lifecycle.state", "StorageSMBResource "+name+" froze a samba image its StorageCluster's settings did not freeze", "restore the original apply evidence")
	}
	for _, name := range []string{"smb-users", "smb-join"} {
		if r := builtSMB(t, catalog, base, name); len(r.Images) != 0 {
			t.Fatalf("%s froze images %+v", name, r.Images)
		}
	}
}

func TestSMBRefusesUntilSambaImagesAreSelected(t *testing.T) {
	plain := pinnedCatalog(t, nil)
	refused := map[api.Kind]string{api.StorageCluster: "example", api.StoragePool: "volumes", api.StorageClient: "consumer"}
	state := compilation.NewState(plain, plain, nil)
	for kind := range refused {
		if got := New(kind, nil).Unsupported(state); len(got) != 0 {
			t.Fatalf("%s without SMB refuses %+v", kind, got)
		}
	}
	catalog := api.NewCatalog(append(plain.Objects(), smbJoin(), smbUsers()))
	state = compilation.NewState(catalog, catalog, nil)
	for kind, name := range refused {
		want := []lifecycle.Refusal{{Kind: string(kind), Name: name,
			Reason:      "StorageSMBResource smb-join needs cephadm's container_image_samba and container_image_samba_metrics; no auxiliary image is selected for them, and Bootwright keeps no default",
			Remediation: "remove every StorageSMBResource whose spec.clusterRef is example from the selected Environment"}}
		if got := New(kind, nil).Unsupported(state); !slices.Equal(got, want) {
			t.Fatalf("%s refuses %+v, want %+v", kind, got, want)
		}
	}
}

func retiredSMB(t *testing.T, placement api.Value) (SMBRequest, lifecycle.Execution) {
	t.Helper()
	catalog := smbCatalog(t, subvolumeOf(), smbUsers(), smbFiles(placement))
	r := builtSMB(t, catalog, sambaBase(t, catalog), "smb-files")
	e := smbExecution(t, r, "pw")
	e.Operation, e.RetiredWithHosts, e.ApplyProved = "destroy-later", true, nil
	return r, e
}

func TestSMBBlocksRetireWithTheirHosts(t *testing.T) {
	_, e := retiredSMB(t, requestMap("label", "mon", "count", 2))
	runner := &recordingRunner{result: lifecycle.RunResult{Outcome: "changed", Evidence: json.RawMessage(`{}`)}}
	capability := NewSMB(runner)
	result, err := capability.Destroy(context.Background(), e)
	if err != nil || result.Outcome != reconciliation.OutcomeChanged || runner.calls != 0 {
		t.Fatalf("retired destroy = %+v (%v), calls %d", result, diagnostics.Of(err), runner.calls)
	}
	want, err := json.Marshal(Retirement{Action: "smb", Cluster: "example", FSID: FSIDFor("apply-original", "example"), RetiredWith: []string{"node-1", "node-2", "node-3"}})
	if err != nil || string(result.Evidence) != string(want) {
		t.Fatalf("retirement evidence %s, want %s", result.Evidence, want)
	}
	observed, err := capability.ObserveRemoval(context.Background(), e)
	if err != nil || observed.Effect != reconciliation.EffectCompleted || string(observed.Evidence) != string(want) || runner.calls != 0 {
		t.Fatalf("retired observation = %+v (%v), calls %d", observed, diagnostics.Of(err), runner.calls)
	}
	q, err := capability.Quiescent(context.Background(), lifecycle.Probe{Block: e.Block, RetiredWithHosts: true})
	if err != nil || q.State != lifecycle.Quiescent || q.Reason != retiredReason {
		t.Fatalf("retired quiescence = %+v (%v)", q, err)
	}
}

func TestSMBBlockStaysUnprovedWhenOnlyEligibleHostsRetire(t *testing.T) {
	r, e := retiredSMB(t, requestMap("hosts", api.StringList("node-1", "node-2")))
	if !slices.Equal(r.Service.Hosts, []string{"node-1", "node-2"}) {
		t.Fatalf("the SMB hosts are %v", r.Service.Hosts)
	}
	machines, err := NewSMB(nil).HostMachines(e.Block)
	if err != nil || !slices.Equal(machines, []string{"node-1", "node-2", "node-3"}) {
		t.Fatalf("host machines = %v (%v)", machines, err)
	}
	q, err := NewSMB(nil).Quiescent(context.Background(), lifecycle.Probe{Block: e.Block})
	if err != nil || q.State != lifecycle.Unproved || q.Reason != survivingReason || q.Remedy != survivingRemedy {
		t.Fatalf("surviving quiescence = %+v (%v)", q, err)
	}
}

func TestSMBOnlineRemovalRefusesWithB491(t *testing.T) {
	_, e := retiredSMB(t, requestMap("label", "mon", "count", 2))
	e.RetiredWithHosts, e.ApplyProved = false, sambaProofs(t)
	runner := &recordingRunner{}
	capability := NewSMB(runner)
	result, err := capability.Destroy(context.Background(), e)
	refusedWith(t, err, "lifecycle.unsupported", survivingReason, survivingRemedy)
	observed, observeErr := capability.ObserveRemoval(context.Background(), e)
	refusedWith(t, observeErr, "lifecycle.unsupported", survivingReason, survivingRemedy)
	if result.Outcome != reconciliation.OutcomeFailed || observed.Effect != reconciliation.EffectUnknown || runner.calls != 0 || !capability.RequiresOnlineRemoval(e.Block) {
		t.Fatalf("online removal = %+v / %+v, calls %d", result, observed, runner.calls)
	}
	removal, err := capability.Removal(context.Background(), e.Block)
	if err != nil || removal.Description != "remove Ceph SMB cluster files of smb-files" || !slices.Equal(removal.Impacts, []string{removal.Description}) || !slices.Equal(removal.Consumes, []string{reconciliation.AuthorizationDataLoss}) {
		t.Fatalf("removal = %+v (%v)", removal, err)
	}
}

func TestSMBIdentity(t *testing.T) {
	catalog := smbCatalog(t, subvolumeOf(), smbJoin())
	r := builtSMB(t, catalog, smbBase(t, catalog), "smb-join")
	const mismatch = "Ceph SMB request does not match its frozen resource identity"
	for name, change := range map[string]func(*lifecycle.Execution){
		"another kind":     func(e *lifecycle.Execution) { e.Block.Kind = string(api.StorageCluster) },
		"another object":   func(e *lifecycle.Execution) { e.Block.Object = "other" },
		"another block ID": func(e *lifecycle.Execution) { e.Block.ID = "ceph-smb-other" },
	} {
		t.Run(name, func(t *testing.T) {
			e := smbExecution(t, r, "pw")
			change(&e)
			_, err := NewSMB(nil).identity(context.Background(), e)
			refusedWith(t, err, "lifecycle.state", mismatch, "")
		})
	}
	e := smbExecution(t, r, "pw")
	e.ApplyOperation = ""
	_, err := NewSMB(nil).identity(context.Background(), e)
	refusedWith(t, err, "lifecycle.state", "Ceph SMB requires the immutable original apply identity", "")
	scoped := r
	scoped.Scope = json.RawMessage(`{"cluster":"example"}`)
	e = smbExecution(t, r, "pw")
	if e.Block.Request, err = reconciliation.Freeze(scoped, "Ceph SMB"); err != nil {
		t.Fatal(err)
	}
	_, err = NewSMB(nil).identity(context.Background(), e)
	refusedWith(t, err, "lifecycle.state", "the frozen Ceph SMB request is inconsistent", "")
	if _, err := NewSMB(nil).identity(context.Background(), smbExecution(t, r, "pw")); err != nil {
		t.Fatalf("the frozen identity is refused: %+v", diagnostics.Of(err))
	}
}

func TestSMBPlanRefusesANameTooLongForItsBlock(t *testing.T) {
	long := smbObject(strings.Repeat("s", 55), "secrets", api.ListValue(requestMap("path", "/auth/password", "secretRef", "join-password")),
		"native", requestMap("resource_type", api.SMBJoinAuth, "auth_id", "join", "auth", requestMap("username", "Administrator")))
	catalog := smbCatalog(t, subvolumeOf(), long)
	_, err := NewSMB(nil).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"})
	refusedWith(t, err, "api.invariant", "StorageSMBResource "+strings.Repeat("s", 55)+" has a name too long to identify its block, whose identity holds at most 63 characters", "rename the StorageSMBResource to at most 54 characters")
}

func TestSMBPlanNamesItsSecretsAndSubjects(t *testing.T) {
	catalog := smbGraph(t)
	input := lifecycle.PlanInput{Verb: reconciliation.Destroy, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"}
	plan, err := NewSMB(nil).Plan(context.Background(), input)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if want := []string{"alice-password", "bob-password", "fleet-key"}; !slices.Equal(plan.Secrets, want) {
		t.Fatalf("the SMB plan binds %v, want %v", plan.Secrets, want)
	}
	descriptions := []string{}
	for _, d := range plan.Definitions {
		descriptions = append(descriptions, d.Description)
		if !slices.Equal(d.Consumes, []string{reconciliation.AuthorizationDataLoss}) {
			t.Fatalf("%s consumes %v", d.ID, d.Consumes)
		}
	}
	slices.Sort(descriptions)
	want := []string{"remove Ceph SMB cluster files of smb-files", "remove Ceph SMB share docs of smb-docs", "remove Ceph SMB users and groups users of smb-users"}
	if !slices.Equal(descriptions, want) {
		t.Fatalf("descriptions %v, want %v", descriptions, want)
	}
}

func TestSMBPlanBindsEveryHostKeySecret(t *testing.T) {
	objects := smbGraph(t).Objects()
	for i, object := range objects {
		if object.Kind() == api.Machine {
			objects[i] = object.WithSpec(object.Spec().WithPath(api.StringValue(object.Name()+"-host-key"), "os", "install", "hostKeyRef"))
		}
	}
	catalog := api.NewCatalog(objects)
	want := []string{"alice-password", "bob-password", "fleet-key"}
	for _, h := range smbBase(t, catalog).Hosts {
		want = append(want, h.Machine+"-host-key")
	}
	slices.Sort(want)
	if len(want) != 6 {
		t.Fatalf("the cluster binds %v", want)
	}
	plan, err := NewSMB(nil).Plan(context.Background(), lifecycle.PlanInput{Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: "synthetic"}, State: compilation.NewState(catalog, catalog, nil), Controller: "controller"})
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if !slices.Equal(plan.Secrets, want) {
		t.Fatalf("the SMB plan binds %v, want %v", plan.Secrets, want)
	}
}

func TestSMBDeadline(t *testing.T) {
	catalog := smbGraph(t)
	base := sambaBase(t, catalog)
	for name, want := range map[string][2]time.Duration{"smb-users": {2040 * time.Second, 720 * time.Second}, "smb-files": {2040 * time.Second, 720 * time.Second}, "smb-docs": {3600 * time.Second, 1380 * time.Second}} {
		r := builtSMB(t, catalog, base, name)
		apply, err := smbDeadline(r, "apply")
		observe, observeErr := smbDeadline(r, "observe")
		if err != nil || observeErr != nil || apply != want[0] || observe != want[1] || apply > lifecycle.MaxDeadline {
			t.Fatalf("%s deadlines %s %s (%v %v)", name, apply, observe, err, observeErr)
		}
		if _, err := smbDeadline(r, "destroy"); err == nil {
			t.Fatalf("%s states a destroy deadline", name)
		}
	}
}
