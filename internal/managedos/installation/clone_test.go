package installation

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

func cloneCatalog(cloud api.Value) api.Catalog {
	m := guest()
	m = m.WithSpec(m.Spec().WithPath(api.StringValue("metal-01-host-key"), "os", "install", "hostKeyRef").WithPath(api.StringValue("lab-vsphere"), "hardware", "providerRef"))
	hardware := api.MapValue(text("name", "rhel"), number("diskGiB", "60"), text("template", "/dc/vm/templates/rhel"), text("placementRef", "site"), field("tpm", api.MapValue()))
	vs := api.MapValue(field("vcenters", api.ListValue(api.MapValue(text("server", "vc.example.test"), text("credentialsRef", "vc-account")))),
		field("placements", api.ListValue(api.MapValue(text("name", "site"), text("server", "vc.example.test"), text("datacenter", "/dc"), text("datastore", "/dc/datastore/store")))),
		field("machineProfiles", api.ListValue(hardware)))
	p := api.NewObject(api.InfraProvider, "lab-vsphere", api.Value{}, api.MapValue(field("vsphere", vs)))
	profile := installProfile(field("customizations", api.MapValue()), field("installer", api.MapValue(field("templateClone", api.MapValue(field("seed", api.MapValue(field("cloudInit", cloud))))))))
	catalog := labCatalog(m, p, profile, sshKeySecret(api.MapValue(field("generated", api.MapValue(text("keyType", "ed25519"))))))
	var objects []api.Object
	for _, o := range catalog.Objects() {
		if o.Kind() != api.MachineImage && o.Kind() != api.ArtifactServer {
			objects = append(objects, o)
		}
	}
	return api.NewCatalog(objects)
}

func TestCloneSeedGolden(t *testing.T) {
	catalog := cloneCatalog(api.MapValue())
	req, needs, err := Requests(catalog, "controller", testContext)
	if err != nil || len(req) != 1 {
		t.Fatalf("clone request: %v (%d)", err, len(req))
	}
	r := req[0]
	if len(r.Target.PortMACs) == 0 || !r.CloudInit.GrowRootFilesystem || !r.Target.TPM2 || len(r.MediaNames()) != 0 || len(needs[0].ArtifactServers) != 0 {
		t.Fatalf("unexpected clone requirements: %+v, %+v", r, needs)
	}
	canonical, err := r.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "request-vsphere-clone", canonical, false)
	if _, err = DecodeRequest(canonical); err != nil {
		t.Fatal(err)
	}
	passesTheTemplateGuard(t, canonical)
	input := planInput(reconciliation.Apply)
	input.State = compilation.NewState(catalog, catalog, nil)
	if _, err := New(nil).Plan(context.Background(), input); err != nil {
		t.Fatalf("clone-only plan without a media reader: %v", err)
	}
	evidence := completeEvidence(r, "digest", "marker")
	var e Evidence
	if err := json.Unmarshal(evidence, &e); err != nil {
		t.Fatal(err)
	}
	e.Image = false
	e.OS = OSEvidence{ID: "rhel", VersionID: "9.8"}
	e.TPM2 = true
	data, _ := json.Marshal(e)
	if err := ValidatePresence(data, r, "digest", "marker"); err == nil {
		t.Fatal("clone without cloud-init completion was accepted")
	}
	e.CloudInit = true
	data, _ = json.Marshal(e)
	if err := ValidatePresence(data, r, "digest", "marker"); err != nil {
		t.Fatal(err)
	}
	e.TPM2 = false
	data, _ = json.Marshal(e)
	if err := ValidatePresence(data, r, "digest", "marker"); err == nil {
		t.Fatal("clone without its required TPM proof was accepted")
	}
	e.TPM2 = true
	e.OS.VersionID = "9.7"
	data, _ = json.Marshal(e)
	if err := ValidatePresence(data, r, "digest", "marker"); err == nil {
		t.Fatal("clone on the wrong release was accepted")
	}
}

func TestCloneHonorsExplicitNoGrowAndBootstrapsBondedVLAN(t *testing.T) {
	catalog := cloneCatalog(api.MapValue(field("growRootFilesystem", api.BoolValue(false))))
	r, _, err := Requests(catalog, "controller", testContext)
	if err != nil {
		t.Fatal(err)
	}
	cloud := r[0].CloudInit
	if cloud.GrowRootFilesystem || cloud.UserData["resize_rootfs"] != false || cloud.UserData["growpart"].(map[string]any)["mode"] != "off" {
		t.Fatal("false did not disable both partition and filesystem growth")
	}
	i := Installation{Address: "192.0.2.4", Prefix: 24, Interface: "vlan40", VLANBase: "bond0", VLANID: 40, BondPorts: []string{"lan0", "lan1"}, BondOptions: []string{"mode=active-backup", "miimon=100"}}
	net := cloneNetwork(i, []string{"ifname=lan0:00:50:56:00:00:01", "ifname=lan1:00:50:56:00:00:02"})
	b, _ := json.Marshal(net)
	for _, want := range []string{`"bond_interfaces":["lan0","lan1"]`, `"bond-mode":"active-backup"`, `"vlan_link":"bond0"`, `"vlan_id":40`, `"address":"192.0.2.4/24"`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("%s missing %s", b, want)
		}
	}
}

func TestPushedInstallerRequiresControllerLocalISO(t *testing.T) {
	base := cloneCatalog(api.MapValue())
	remote := artifactServer(text("machineRef", "services"))
	catalog := labCatalog(append(base.Objects(), servicesHost(), installProfile(), remote)...)
	_, _, err := Requests(catalog, "controller", testContext)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Message != "pushed installer media must be built on the controller" {
		t.Fatalf("remote private ISO placement = %+v", reported)
	}
	catalog = labCatalog(append(base.Objects(), installProfile(), artifactServer())...)
	req, _, err := Requests(catalog, "controller", testContext)
	if err != nil || len(req) != 1 || req[0].Private == nil || len(req[0].Target.PortMACs) == 0 || !req[0].Target.TPM2 {
		t.Fatalf("pushed installer did not freeze its private placement, MACs and TPM: %+v, %v", req, diagnostics.Of(err))
	}
}

func TestCloneOverwritesTheDeclaredRepositoryAndReleasePin(t *testing.T) {
	repository := Repository{ID: "packages", Name: "Packages", BaseURL: "https://packages.example.test/rhel", Enabled: true, GPGCheck: true, GPGKeyURL: "https://packages.example.test/key", Proxy: "_none_"}
	cloud := cloneUserData(Installation{Repositories: []Repository{repository}, Release: "9.8"}, CloudInit{}, "/etc/ssh/ssh_host_ed25519_key")
	files := cloud["write_files"].([]map[string]string)
	if len(files) != 3 || files[1]["path"] != repositoryPath(repository) || files[1]["owner"] != "root:root" || files[1]["permissions"] != "0644" ||
		files[1]["content"] != strings.Join(repositoryContents(repository), "\n")+"\n" || files[2]["path"] != ReleaseVariablePath || files[2]["content"] != "9.8\n" {
		t.Fatalf("clone repository and release declarations = %+v", files)
	}
}

func TestCloneRemovalUsesTheRegisteredVMPinBeforeContactingItsPort(t *testing.T) {
	req := onlyClone(t)
	raw, err := req.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	call, _ := execution(t, "digest")
	call.Block.Request = raw
	call.Material[req.Target.HostKeyRef] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.PublicKeyPart: []byte("ssh-ed25519 AAAAHOST")})
	call.Proved = []lifecycle.BlockEvidence{{Object: "different", Operation: "unrelated"}}
	call.ApplyProved = []lifecycle.BlockEvidence{{Object: req.Identity.Object, Operation: "original"}}
	gone, _ := json.Marshal(Evidence{Absent: true, Postcondition: true, Request: call.Block.RequestDigest})
	runner := &fakeRunner{result: lifecycle.RunResult{Outcome: reconciliation.OutcomeChanged, Evidence: gone}}
	proved := &pinReader{pinned: true, identity: machine.HardwareIdentity{UUID: "vcenter-instance", Serial: `{"moRef":"vm-17","instanceUUID":"original-vm"}`}}
	capability := New(runner).WithIdentities(proved)
	if _, err := capability.Destroy(t.Context(), call); err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if len(runner.requests) != 1 || !reflect.DeepEqual(proved.handed, [][]lifecycle.BlockEvidence{call.ApplyProved}) {
		t.Fatalf("destroy used another operation's proof: %+v", proved.handed)
	}
	for key, value := range proved.identity.PinValues("") {
		if runner.requests[0].MaterialValues[key] != value {
			t.Fatalf("destroy did not bind the original VM %s", key)
		}
	}
	call.ApplyProved = nil
	call.Proved = []lifecycle.BlockEvidence{{Object: req.Identity.Object, Operation: "current"}}
	proved.identity.Serial = ""
	if _, err := capability.ObserveRemoval(t.Context(), call); err == nil || len(runner.requests) != 1 {
		t.Fatalf("a clone missing its inventory identity contacted the Machine port: %v", err)
	}
	if !reflect.DeepEqual(proved.handed[1], call.Proved) {
		t.Fatalf("observation did not consult its available Machine proof: %+v", proved.handed[1])
	}
}

func onlyClone(t *testing.T) Request {
	t.Helper()
	requests, _, err := Requests(cloneCatalog(api.MapValue()), "controller", testContext)
	if err != nil || len(requests) != 1 || requests[0].CloudInit == nil {
		t.Fatalf("clone request: %v (%d)", err, len(requests))
	}
	return requests[0]
}

func TestCloneRefusesTamperedFrozenSeedRequests(t *testing.T) {
	r := onlyClone(t)
	for name, alter := range map[string]func(*Request){
		"mixed installer media":            func(r *Request) { r.BootMedia.Name = "installer.iso" },
		"unverified management connection": func(r *Request) { r.Target.Controller.TLSVerify = false },
		"unproved virtual interfaces":      func(r *Request) { r.Target.PortMACs = nil },
		"remote private files":             func(r *Request) { r.Placement.Connection = machine.ConnectionSSH },
	} {
		t.Run(name, func(t *testing.T) {
			altered := r
			alter(&altered)
			data, err := altered.Canonical()
			if err != nil {
				t.Fatal(err)
			}
			_, err = DecodeRequest(data)
			expectRefusal(t, err, "install.request-invalid")
		})
	}
}

func TestClonePlanningRefusesUnusableAccessKeysBeforeCreatingARequest(t *testing.T) {
	base := cloneCatalog(api.MapValue())
	env := base.OfKind(api.Environment)[0]
	host, ok := base.Find(api.Secret, "metal-01-host-key")
	if !ok {
		t.Fatal("clone fixture has no host key")
	}
	for _, tc := range []struct {
		object        api.Object
		code, message string
	}{
		{env.WithSpec(env.Spec().Without("remoteMachinesAccessKey")), "api.required", "fleet access key"},
		{host.WithSpec(host.Spec().WithPath(api.StringValue("unsupported-type"), "source", "generated", "keyType")), "api.value", "SSH host key type"},
	} {
		t.Run(tc.object.Identity(), func(t *testing.T) {
			catalog := labCatalog(append(base.Objects(), tc.object)...)
			requests, _, err := Requests(catalog, "controller", testContext)
			reported := diagnostics.Of(err)
			if len(requests) != 0 || len(reported) != 1 || reported[0].Code != tc.code || !strings.Contains(reported[0].Message, tc.message) {
				t.Fatalf("unexpected clone access decision: %+v, %d requests", reported, len(requests))
			}
		})
	}
}
