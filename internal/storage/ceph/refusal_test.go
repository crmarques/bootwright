package ceph

import (
	"context"
	"encoding/json"
	"maps"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/crmarques/bootwright/ansible"
	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// refusingRunner answers as the lifecycle runner does when the adapter
// publishes a refused record: with the failure its caller gave that reason,
// and a reason its caller did not name is an invalid protocol.
type refusingRunner struct {
	reason  string
	request lifecycle.RunRequest
}

func (r *refusingRunner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.request = request
	if named := request.Refusals[r.reason]; named != nil {
		return lifecycle.RunResult{}, named
	}
	return lifecycle.RunResult{}, failure("lifecycle.unknown", "the adapter capability protocol was invalid", "")
}

func TestAHostRefusalReachesTheOperatorWithItsRemedy(t *testing.T) {
	e, _, proofs := hostExecution(t, "node-2")
	runner := &refusingRunner{reason: "image-pull"}
	result, err := NewHost(runner).WithProofs(proofs).Apply(context.Background(), e)
	d := diagnostics.Of(err)
	if result.Outcome == reconciliation.OutcomeChanged || len(d) != 1 || d[0].Code != "lifecycle.unknown" ||
		d[0].Message != "the frozen Ceph image or an auxiliary image digest could not be pulled within its three bounded attempts" ||
		d[0].Remediation != "inspect the attempt's adapter output, then apply again; host preparation resumes from its journal" {
		t.Fatalf("an image-pull refusal reached the operator as %+v %+v", result, d)
	}
}

func TestAPackageRefusalReachesTheOperatorWithWhatToCheck(t *testing.T) {
	for reason, want := range map[string]string{"package-license": "Entitlement.spec.license.accept", "package-signature": "publisher keys", "package-resolution": "IBM Ceph repository", "package-transaction": "dnf log"} {
		t.Run(reason, func(t *testing.T) {
			e, _, proofs := hostExecution(t, "node-1")
			_, err := NewHost(&refusingRunner{reason: reason}).WithProofs(proofs).Apply(context.Background(), e)
			if d := diagnostics.Of(err); len(d) != 1 || d[0].Message == "the adapter capability protocol was invalid" || !strings.Contains(d[0].Remediation, want) {
				t.Fatalf("a %s refusal reached the operator as %+v", reason, d)
			}
		})
	}
}

// A named refusal ends the attempt with its entry's code: an unknown-coded one
// leaves the block's effect unknown, for the next apply to observe first, and
// an unsupported-coded one fails it.
func TestANamedHostRefusalsCodeDecidesTheAttemptOutcome(t *testing.T) {
	for reason, want := range map[string]reconciliation.Outcome{"image-pull": reconciliation.OutcomeUnknown, "package-license": reconciliation.OutcomeFailed} {
		t.Run(reason, func(t *testing.T) {
			e, _, proofs := hostExecution(t, "node-1")
			result, err := NewHost(&refusingRunner{reason: reason}).WithProofs(proofs).Apply(context.Background(), e)
			if err == nil || result.Outcome != want || lifecycle.AttemptOutcome(err) != want {
				t.Fatalf("a %s refusal ended the attempt %q (%v), want %q", reason, result.Outcome, diagnostics.Of(err), want)
			}
		})
	}
}

func TestAClusterRefusalReachesTheOperatorWithItsRemedy(t *testing.T) {
	r := coreRequest()
	e := withMaterial(executionFor(t, r), r)
	runner := &refusingRunner{reason: "dashboard-drift"}
	observation, err := New(api.StorageCluster, runner).Observe(context.Background(), e)
	d := diagnostics.Of(err)
	if observation.Effect != reconciliation.EffectUnknown || len(d) != 1 ||
		d[0].Message != "the Ceph dashboard user admin no longer matches the password this context's custody holds" ||
		!strings.Contains(d[0].Remediation, "bootwright cluster info --secrets --name <cluster>") {
		t.Fatalf("a dashboard-drift refusal reached the operator as %+v %+v", observation, d)
	}
}

func TestADashboardManagerRefusalNamesTheManagerContainerNotTheAdminUser(t *testing.T) {
	r := coreRequest()
	e := withMaterial(executionFor(t, r), r)
	runner := &refusingRunner{reason: "dashboard-manager"}
	observation, err := New(api.StorageCluster, runner).Observe(context.Background(), e)
	d := diagnostics.Of(err)
	if observation.Effect != reconciliation.EffectUnknown || len(d) != 1 || d[0].Code != "lifecycle.unknown" ||
		d[0].Message != "a Ceph MGR's container or dashboard port no longer matches the deployment this operation recorded" ||
		!strings.Contains(d[0].Remediation, "ceph orch ps --daemon_type mgr") || strings.Contains(d[0].Message, "admin") {
		t.Fatalf("a dashboard-manager refusal reached the operator as %+v %+v", observation, d)
	}
}

func TestACallHomeUnqualifiedRefusalReachesTheOperatorAsUnsupportedWithItsRemedy(t *testing.T) {
	r := coreRequest()
	e := withMaterial(executionFor(t, r), r)
	runner := &refusingRunner{reason: "call-home-unqualified"}
	_, err := New(api.StorageCluster, runner).Observe(context.Background(), e)
	d := diagnostics.Of(err)
	if len(d) != 1 || d[0].Code != "lifecycle.unsupported" || d[0].Message != "IBM Call Home intent has no qualified path on the selected Ceph build" ||
		d[0].Remediation != "the apply froze this input, so take back what it owns with bootwright destroy --context <context>, "+
			"correct ceph.ibm.callHome for the selected build with bootwright context update, then apply; see .agents/knowledge/version-specializations.md" {
		t.Fatalf("a call-home-unqualified refusal reached the operator as %+v", d)
	}
}

func TestANativeOptionUndeclaredRefusalNamesTheDestroyItsInputChangeNeeds(t *testing.T) {
	r := coreRequest()
	e := withMaterial(executionFor(t, r), r)
	runner := &refusingRunner{reason: "native-option-undeclared"}
	_, err := New(api.StorageCluster, runner).Observe(context.Background(), e)
	d := diagnostics.Of(err)
	if len(d) != 1 || d[0].Code != "lifecycle.unsupported" ||
		!strings.HasPrefix(d[0].Remediation, "the apply froze this input, so take back what it owns with bootwright destroy --context <context>, ") ||
		!strings.Contains(d[0].Remediation, "with bootwright context update, then apply") {
		t.Fatalf("a native-option-undeclared refusal reached the operator as %+v", d)
	}
}

func TestAForeignImageRefusalNamesTheSettingAndNeverOverwrites(t *testing.T) {
	r := coreRequest()
	r.Action, r.Settings = "settings", &Settings{Modules: []string{"nfs"}}
	e := withMaterial(executionFor(t, r), r)
	observation, err := New(api.StorageCluster, &refusingRunner{reason: "foreign-image"}).Observe(context.Background(), e)
	d := diagnostics.Of(err)
	if observation.Effect != reconciliation.EffectUnknown || len(d) != 1 || d[0].Code != "lifecycle.unknown" ||
		d[0].Message != "a cephadm container_image setting of the Ceph cluster holds an image this operation did not freeze, is set outside the mgr section, or no longer holds the frozen image a declared service needs" ||
		d[0].Remediation != "find the setting the native error names in ceph config dump, then remove it with ceph config rm or set it to the frozen image outside Bootwright and apply again; "+
			"Bootwright never overwrites a container_image setting it did not write" {
		t.Fatalf("a foreign-image refusal reached the operator as %+v %+v", observation, d)
	}
}

func nfsUnitRequest() Request {
	r := coreRequest()
	document := json.RawMessage(`{"placement":{"count":1,"hosts":["node-1"]},"service_id":"files","service_type":"nfs"}`)
	r.Action, r.Service = "service", &ServiceUnit{Documents: []json.RawMessage{document}, ID: "files", Type: "nfs"}
	return r
}

func TestANFSModuleDisabledRefusalNamesTheSettingsBlock(t *testing.T) {
	r := nfsUnitRequest()
	e := withMaterial(executionFor(t, r), r)
	observation, err := New(api.StorageCluster, &refusingRunner{reason: "nfs-module-disabled"}).Observe(context.Background(), e)
	d := diagnostics.Of(err)
	if observation.Effect != reconciliation.EffectUnknown || len(d) != 1 || d[0].Code != "lifecycle.unknown" ||
		d[0].Message != "the Ceph cluster's nfs manager module is disabled, although the cluster's settings block enabled it for the declared NFS service" ||
		d[0].Remediation != "enable it again outside Bootwright with ceph mgr module enable nfs, then apply again; the settings block does not run again once it completed, "+
			"and an NFS service block never enables a manager module" {
		t.Fatalf("an nfs-module-disabled refusal reached the operator as %+v %+v", observation, d)
	}
}

func TestANFSPoolForeignRefusalNamesThePool(t *testing.T) {
	r := nfsUnitRequest()
	e := withMaterial(executionFor(t, r), r)
	observation, err := New(api.StorageCluster, &refusingRunner{reason: "nfs-pool-foreign"}).Observe(context.Background(), e)
	d := diagnostics.Of(err)
	if observation.Effect != reconciliation.EffectUnknown || len(d) != 1 || d[0].Code != "lifecycle.unknown" ||
		d[0].Message != "a Ceph pool named .nfs exists that this operation's NFS services did not create, or it is not the pool they recorded" ||
		d[0].Remediation != "find the .nfs pool with ceph osd pool ls detail; if nothing uses it, remove it outside Bootwright and apply again, "+
			"or take back what this context owns with bootwright destroy --context <context>, then apply; Bootwright never adopts a .nfs pool it did not create" {
		t.Fatalf("an nfs-pool-foreign refusal reached the operator as %+v %+v", observation, d)
	}
}

func gatewayRefusal(t *testing.T, reason string) (reconciliation.EffectState, []diagnostics.Diagnostic) {
	t.Helper()
	r := coreRequest()
	r.Action, r.Gateway = "gateway", gatewayRuntimeRequest().Gateway
	e := withMaterial(executionFor(t, r), r)
	e.Block.ID, e.Block.Kind, e.Block.Object = GatewayBlockID("objects"), string(api.StorageObjectGateway), "objects"
	observation, err := New(api.StorageObjectGateway, &refusingRunner{reason: reason}).Observe(context.Background(), e)
	return observation.Effect, diagnostics.Of(err)
}

func TestAnObjectRealmForeignRefusalNamesItsNextStep(t *testing.T) {
	effect, d := gatewayRefusal(t, "object-realm-foreign")
	if effect != reconciliation.EffectUnknown || len(d) != 1 || d[0].Code != "lifecycle.unknown" ||
		d[0].Message != "the cluster holds RGW configuration this operation did not create: a .rgw.root pool without this operation's claim, "+
			"or a realm, zone group or zone of the StorageObjectGateway's names without its creation record, or one it created that is gone or changed" ||
		d[0].Remediation != "find what the native error names with ceph osd pool ls and radosgw-admin realm list, zonegroup list and zone list; "+
			"for a foreign name, rename spec.realm, spec.zoneGroup or spec.zone of the StorageObjectGateway, or remove the foreign configuration outside Bootwright, "+
			"then apply again; when the native error says one this operation created is gone or changed, take back what this context owns with "+
			"bootwright destroy --context <context>, then apply; "+
			"Bootwright never adopts RGW configuration it did not create" {
		t.Fatalf("an object-realm-foreign refusal reached the operator as %s %+v", effect, d)
	}
}

func TestAnObjectZonePoolsRefusalNamesItsNextStep(t *testing.T) {
	effect, d := gatewayRefusal(t, "object-zone-pools")
	if effect != reconciliation.EffectUnknown || len(d) != 1 || d[0].Code != "lifecycle.unknown" ||
		d[0].Message != "the zone a StorageObjectGateway created names a pool Bootwright did not plan, or its default placement does not hold the pools the gateway froze" ||
		d[0].Remediation != "inspect the zone with radosgw-admin zone get --rgw-realm <realm> --rgw-zone <zone>; the native error names the pool; "+
			"take back what this context owns with bootwright destroy --context <context>, then apply; "+
			"Bootwright creates every zone pool before RGW starts and never lets RGW create a pool it did not plan" {
		t.Fatalf("an object-zone-pools refusal reached the operator as %s %+v", effect, d)
	}
}

func TestAnObjectEndpointCertificateRefusalNamesItsNextStep(t *testing.T) {
	effect, d := gatewayRefusal(t, "object-endpoint-certificate")
	if effect != reconciliation.EffectUnknown || len(d) != 1 || d[0].Code != "lifecycle.unknown" ||
		d[0].Message != "a TLS service of a StorageObjectGateway holds a certificate or private key other than the one its bound Secret holds" ||
		d[0].Remediation != "find the service the native error names with ceph orch ls; Bootwright never replaces the certificate of a deployed gateway service: "+
			"store the deployed certificate and key in the Secret again with bootwright secret set --name <secret> --certificate-file <path> --private-key-file <path>, "+
			"or take back what this context owns with bootwright destroy --context <context>, then apply" {
		t.Fatalf("an object-endpoint-certificate refusal reached the operator as %s %+v", effect, d)
	}
}

func nfsExportRefusal(t *testing.T, reason string) (reconciliation.EffectState, []diagnostics.Diagnostic) {
	t.Helper()
	r := coreRequest()
	r.Action, r.NFSExport = "nfs-export", &NFSExport{Filesystem: "files", Name: "docs", Service: "shares", Native: NFSExportNative{AccessType: "RW", Clients: []NFSExportClient{},
		FSAL: NFSExportFSAL{CmountPath: "/", FSName: "files", Name: "CEPH"}, Path: "/", Protocols: []int{4}, Pseudo: "/docs", SecurityLabel: true, Squash: "root_squash", Transports: []string{"TCP"}}}
	observation, err := New(api.StorageNFSExport, &refusingRunner{reason: reason}).Observe(context.Background(), withMaterial(exportExecution(t, r), r))
	return observation.Effect, diagnostics.Of(err)
}

func TestANFSExportForeignRefusalNamesItsNextStep(t *testing.T) {
	effect, d := nfsExportRefusal(t, "nfs-export-foreign")
	if effect != reconciliation.EffectUnknown || len(d) != 1 || d[0].Code != "lifecycle.unknown" ||
		d[0].Message != "an NFS export exists at the StorageNFSExport's pseudo path on its NFS service that this operation did not create, "+
			"or the export this operation created is gone or now has another export ID or CephFS principal" ||
		d[0].Remediation != "find the export the native error names with ceph nfs export info <service> <pseudo>; for an export this operation did not create, "+
			"set another spec.native.pseudo on the StorageNFSExport or remove the foreign export outside Bootwright, then apply again; "+
			"when the native error says the export this operation created is gone or changed, take back what this context owns with "+
			"bootwright destroy --context <context>, then apply; Bootwright never adopts or overwrites an NFS export it did not create" {
		t.Fatalf("an nfs-export-foreign refusal reached the operator as %s %+v", effect, d)
	}
}

func TestANFSExportApplyRefusalNamesItsNextStep(t *testing.T) {
	effect, d := nfsExportRefusal(t, "nfs-export-apply")
	if effect != reconciliation.EffectUnknown || len(d) != 1 || d[0].Code != "lifecycle.unknown" ||
		d[0].Message != "the NFS manager did not apply the StorageNFSExport's export cleanly: its NFS service is not listed, the apply failed or reported a warning, "+
			"or the stored export differs from the one applied" ||
		d[0].Remediation != "read the native error, which holds the NFS manager's answer; check the service with ceph nfs cluster ls and the export with "+
			"ceph nfs export info <service> <pseudo>, correct what the error names outside Bootwright (a missing directory, a path another protocol claims, "+
			"or NFS daemons that need ceph orch restart nfs.<service>), then apply again" {
		t.Fatalf("an nfs-export-apply refusal reached the operator as %s %+v", effect, d)
	}
}

func TestANFSExportPathEscapeRefusalNamesItsNextStep(t *testing.T) {
	effect, d := nfsExportRefusal(t, "nfs-export-path-escape")
	if effect != reconciliation.EffectUnknown || len(d) != 1 || d[0].Code != "lifecycle.unknown" ||
		d[0].Message != "the StorageNFSExport's spec.native.path does not lie within the StorageSubvolume it binds, "+
			"or its spec.native.fsal.cmount_path is neither / nor the export path or a directory above it" ||
		d[0].Remediation != "omit spec.native.path so the export serves the subvolume's own path, or set it to the path ceph fs subvolume getpath reports "+
			"for the subvolume or a directory below it, and set spec.native.fsal.cmount_path to / or a directory that contains the export path, "+
			"then apply again; Bootwright wrote nothing for this export" {
		t.Fatalf("an nfs-export-path-escape refusal reached the operator as %s %+v", effect, d)
	}
}

func TestAnIngressFirewallRefusalNamesItsNextStep(t *testing.T) {
	const message = "a host that runs the ingress of a Ceph NFS service or StorageObjectGateway has an active firewalld that does not permit " +
		"VRRP (IP protocol 112) or the ingress's client port on the interface that carries its virtual IP"
	const remedy = "permit VRRP (IP protocol 112) and the port the native error names in the firewalld zone it names on the host it names, " +
		"for example with firewall-cmd --permanent --zone=<zone> --add-protocol=vrrp --add-port=<port>/tcp and firewall-cmd --reload, " +
		"or install that host without an active firewall, then apply again; Bootwright never changes a host's firewall, and this apply changed nothing before the check"
	r := nfsUnitRequest()
	observation, err := New(api.StorageCluster, &refusingRunner{reason: "ingress-firewall"}).Observe(context.Background(), withMaterial(executionFor(t, r), r))
	unit := diagnostics.Of(err)
	effect, gateway := gatewayRefusal(t, "ingress-firewall")
	for _, d := range [][]diagnostics.Diagnostic{unit, gateway} {
		if len(d) != 1 || d[0].Code != "lifecycle.unknown" || d[0].Message != message || d[0].Remediation != remedy {
			t.Fatalf("an ingress-firewall refusal reached the operator as %+v", d)
		}
	}
	if observation.Effect != reconciliation.EffectUnknown || effect != reconciliation.EffectUnknown {
		t.Fatalf("an ingress-firewall refusal left the unit %s and the gateway %s", observation.Effect, effect)
	}
}

// The module publishes only a reason its block names, and a token the block
// does not name as unknown with the token in its detail, so each table names
// exactly what ceph_core.py may publish for its block, unknown among them.
func TestEachCephBlockNamesExactlyTheRefusalsItsModuleMayPublish(t *testing.T) {
	const path = "collections/ansible_collections/bootwright/core/plugins/module_utils/ceph_core.py"
	source := string(ansible.Assets()[path])
	token := regexp.MustCompile(`"([a-z0-9-]+)"`)
	for name, refusals := range map[string]map[string]error{"HOST_REASONS": hostRefusals(), "CLUSTER_REASONS": adapterRefusals()} {
		_, tuple, found := strings.Cut(source, "\n"+name+" = (\n")
		tuple, _, closed := strings.Cut(tuple, "\n)\n")
		if !found || !closed {
			t.Fatalf("%s declares no %s tuple", path, name)
		}
		published := []string{}
		for _, match := range token.FindAllStringSubmatch(tuple, -1) {
			published = append(published, match[1])
		}
		if slices.Sort(published); !slices.Equal(published, slices.Sorted(maps.Keys(refusals))) || !slices.Contains(published, "unknown") {
			t.Fatalf("%s publishes %q, its block names %q", name, published, slices.Sorted(maps.Keys(refusals)))
		}
		for reason, named := range refusals {
			if d := diagnostics.Of(named); len(d) != 1 || d[0].Remediation == "" {
				t.Fatalf("%s names %q with no remedy: %+v", name, reason, d)
			}
		}
	}
}

func TestEachSMBBlockNamesExactlyTheRefusalsItsModuleMayPublish(t *testing.T) {
	const root = "collections/ansible_collections/bootwright/core/plugins/"
	source := string(ansible.Assets()[root+"module_utils/ceph_core.py"])
	_, tuple, found := strings.Cut(source, "\nSMB_REASONS = (\n")
	tuple, _, closed := strings.Cut(tuple, "\n)\n")
	if !found || !closed {
		t.Fatal("ceph_core.py declares no SMB_REASONS tuple")
	}
	published := []string{}
	for _, match := range regexp.MustCompile(`"([a-z0-9-]+)"`).FindAllStringSubmatch(tuple, -1) {
		published = append(published, match[1])
	}
	named := slices.Sorted(maps.Keys(smbRefusals()))
	if slices.Sort(published); !slices.Equal(published, named) || !slices.Contains(published, "unknown") {
		t.Fatalf("SMB_REASONS publishes %q, the SMB block names %q", published, named)
	}
	for _, reason := range []string{"smb-identity", "smb-result"} {
		refusal := smbRefusals()[reason]
		if d := diagnostics.Of(refusal); len(d) != 1 || d[0].Remediation == "" {
			t.Fatalf("the SMB block names %q with no remedy: %+v", reason, d)
		}
	}
	var documentation struct {
		Options struct {
			Reason struct {
				Choices []string `yaml:"choices"`
			} `yaml:"reason"`
		} `yaml:"options"`
	}
	_, text, _ := strings.Cut(string(ansible.Assets()[root+"modules/ceph_smb_protocol.py"]), `DOCUMENTATION = r"""`)
	text, _, _ = strings.Cut(text, `"""`)
	if err := yaml.Unmarshal([]byte(text), &documentation); err != nil || !slices.Equal(slices.Sorted(slices.Values(documentation.Options.Reason.Choices)), named) {
		t.Fatalf("ceph_smb_protocol documents the reasons %q (%v), the SMB block names %q", documentation.Options.Reason.Choices, err, named)
	}
}

func TestResourceRefusalsNameTheirNextStep(t *testing.T) {
	refusals := adapterRefusals()
	generic := diagnostics.Of(refusals["native-output"])[0].Remediation
	for _, reason := range []string{"foreign-pool", "foreign-profile", "foreign-rule", "crush-capacity", "pg-budget", "foreign-rbd-image", "rbd-image-identity", "foreign-subvolume", "subvolume-identity", "pool-drift", "nfs-module-disabled", "nfs-pool-foreign", "object-realm-foreign", "object-zone-pools", "object-endpoint-certificate", "nfs-export-foreign", "nfs-export-apply", "nfs-export-path-escape", "ingress-firewall"} {
		d := diagnostics.Of(refusals[reason])
		if len(d) != 1 || d[0].Code != "lifecycle.unknown" || d[0].Remediation == generic || strings.Contains(d[0].Message, "could not prove its") {
			t.Fatalf("%s reaches the operator as %+v", reason, d)
		}
	}
}

func TestAPoolDriftRefusalNamesTheSettingsAndBothRemedies(t *testing.T) {
	d := diagnostics.Of(adapterRefusals()["pool-drift"])
	const message = "a setting this operation completed on the Ceph pool (size, min_size, a quota, a compression option, erasure-coded overwrites or its application) no longer has the value its StoragePool or StorageObjectGateway froze"
	if len(d) != 1 || d[0].Code != "lifecycle.unknown" || d[0].Message != message ||
		!strings.Contains(d[0].Remediation, "outside Bootwright") || !strings.Contains(d[0].Remediation, "bootwright destroy --context <context>") {
		t.Fatalf("a pool-drift refusal reached the operator as %+v", d)
	}
}

func smbRefusal(t *testing.T, reason string) []diagnostics.Diagnostic {
	t.Helper()
	catalog := smbGraph(t)
	r := builtSMB(t, catalog, sambaBase(t, catalog), "smb-docs")
	_, err := NewSMB(&refusingRunner{reason: reason}).Apply(context.Background(), smbExecution(t, r, "pw"))
	return diagnostics.Of(err)
}

func TestSMBRefusalsNameOnlyNextStepsAFailedApplyCanTake(t *testing.T) {
	for reason, want := range map[string][2]string{
		"smb-identity": {"an SMB resource of the StorageSMBResource's type and ID exists that this operation did not create, or the one it created is gone or no longer matches its frozen document",
			"find the resource the native error names with ceph smb show <name> --password-filter hidden; for a resource this operation did not create, remove it outside Bootwright, " +
				"then apply again with the same input; when the one this operation created is gone or changed, or the StorageSMBResource must change, take back what this context owns with " +
				"bootwright destroy --context <context>, which retires the cluster with its Machines, then apply; Bootwright never adopts an SMB resource it did not create"},
		"smb-result": {"the SMB manager did not apply the StorageSMBResource's resource: its answer was not one successful result for that resource",
			"check the SMB manager's state with ceph smb show --password-filter hidden, correct what blocks the resource (an unknown cluster, a share name in use, a path that is not a " +
				"directory in the volume, or a resource still in use) outside Bootwright, then apply again with the same input; when the StorageSMBResource itself must change, take back " +
				"what this context owns with bootwright destroy --context <context>, which retires the cluster with its Machines, then apply"},
	} {
		d := smbRefusal(t, reason)
		if len(d) != 1 || d[0].Code != "lifecycle.unknown" || d[0].Message != want[0] || d[0].Remediation != want[1] {
			t.Errorf("a %s refusal reached the operator as %+v", reason, d)
		}
	}
	for _, reason := range []string{"smb-identity", "smb-result"} {
		refusal := smbRefusals()[reason]
		remedy := diagnostics.Of(refusal)[0].Remediation
		for _, edit := range []string{"in the StorageSMBResource", "another ID"} {
			if strings.Contains(remedy, edit) {
				t.Errorf("the %s remedy asks for an input edit a failed apply refuses: %q", reason, remedy)
			}
		}
	}
}
