package ceph

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
	"github.com/crmarques/bootwright/internal/trust"
)

func hostPublicKey() []byte {
	blob := binary.BigEndian.AppendUint32(nil, uint32(len("ssh-ed25519")))
	blob = append(blob, []byte("ssh-ed25519")...)
	blob = binary.BigEndian.AppendUint32(blob, 32)
	blob = append(blob, bytes.Repeat([]byte{7}, 32)...)
	return []byte("ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob))
}
func withMaterial(e lifecycle.Execution, r Request) lifecycle.Execution {
	e.Material = map[string]secrets.Material{r.Hosts[0].SSHKeyRef: secrets.NewMaterial(map[secrets.Part][]byte{secrets.PrivateKeyPart: []byte("fleet-private")}), r.ClusterKeyRef: secrets.NewMaterial(map[secrets.Part][]byte{secrets.PrivateKeyPart: []byte("cluster-private"), secrets.PublicKeyPart: hostPublicKey()}), r.RegistryRef: secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte("registry-user"), secrets.PasswordPart: []byte("registry-password")})}
	for _, h := range r.Hosts {
		e.Material[h.HostKeyRef] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.PublicKeyPart: hostPublicKey(), secrets.PrivateKeyPart: []byte("host-private-never-lent")})
	}
	return e
}

func TestMaterialProjectionUsesProvedPublicKeysAndPrivateFiles(t *testing.T) {
	r := coreRequest()
	e := withMaterial(executionFor(t, r), r)
	bound, files, cleanup, err := boundMaterial(e, r, "apply", dependencyProofs{})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 5 {
		t.Fatalf("unexpected material files %+v", files)
	}
	registry, ok := bound.Material[registryMaterial].Part(secrets.ValuePart)
	if !ok || !bytes.Contains(registry, []byte("registry-password")) {
		t.Fatal("private registry JSON missing")
	}
	clear(registry)
	known, _ := bound.Material[knownHostsMaterial].Part(secrets.ValuePart)
	if bytes.Contains(known, []byte("host-private")) || !bytes.Contains(known, []byte("192.0.2.1 ssh-ed25519")) {
		t.Fatal("wrong host trust projection")
	}
	clear(known)
	cleanup()
	value, _ := bound.Material[registryMaterial].Part(secrets.ValuePart)
	if bytes.Contains(value, []byte("registry-password")) {
		t.Fatal("temporary registry material retained")
	}
	clear(value)
	original, _ := e.Material[r.RegistryRef].Part(secrets.PasswordPart)
	if string(original) != "registry-password" {
		t.Fatal("caller material was cleared")
	}
	clear(original)
}

func TestObservationUsesOnlyFleetIdentityAndKnownHostsAndFrozenDigest(t *testing.T) {
	r := coreRequest()
	e := withMaterial(executionFor(t, r), r)
	runtime := r
	runtime.Owner = e.ApplyOperation
	runtime.FSID = FSIDFor(e.ApplyOperation, r.Cluster)
	evidence := readyEvidence(runtime)
	evidence.Request = e.Block.RequestDigest
	runner := &recordingRunner{result: lifecycle.RunResult{Evidence: evidenceBytes(t, evidence), Produced: dashboardProduced(dashboardPassword)}}
	observation, err := New(api.StorageCluster, runner).Observe(context.Background(), e)
	if err != nil || observation.Effect != reconciliation.EffectCompleted || len(observation.Produced) != 1 || observation.Produced[0].Name != DashboardOutput {
		t.Fatalf("observation failed: %+v %v", observation, err)
	}
	lifecycle.ClearProduced(observation.Produced)
	call := runner.request
	if !call.NativeErrors {
		t.Fatal("Ceph invocation did not opt in to typed native errors")
	}
	if !reflect.DeepEqual(call.Outputs, []lifecycle.OutputFile{{Name: "dashboard", Variable: "dashboard"}}) {
		t.Fatalf("the cluster observation does not recapture its dashboard password: %+v", call.Outputs)
	}
	if call.Operation != "observe" || !call.Placement.Local() || call.Digest != e.Block.RequestDigest || len(call.Materials) != 2 {
		t.Fatalf("observation escaped read boundary: %+v", call)
	}
	if bytes.Contains(call.Canonical, []byte("private")) || bytes.Contains(call.Canonical, []byte("password")) {
		t.Fatal("secret in canonical request")
	}
	if _, ok := call.Material[r.RegistryRef]; ok {
		t.Fatal("observation received registry credential")
	}
	if _, ok := call.Material[r.ClusterKeyRef]; ok {
		t.Fatal("observation received native cluster private key")
	}
}

const dashboardPassword = "Xq7-Lm2_Rv9.Tk4-Wz8_Hn3.Pb6-Jd5_"

func dashboardProduced(values ...string) []lifecycle.Produced {
	produced := []lifecycle.Produced{}
	for _, value := range values {
		produced = append(produced, lifecycle.Produced{Name: DashboardOutput, Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(value)})})
	}
	return produced
}

func TestProducedDashboardPasswordIsBoundedToTheGeneratorsShape(t *testing.T) {
	r := coreRequest()
	good := dashboardProduced(dashboardPassword)
	defer lifecycle.ClearProduced(good)
	if err := validateProduced(good, r, true); err != nil {
		t.Fatal(err)
	}
	renamed := dashboardProduced(dashboardPassword)
	renamed[0].Name = "client"
	extraPart := []lifecycle.Produced{{Name: DashboardOutput, Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(dashboardPassword), secrets.PasswordPart: []byte(dashboardPassword)})}}
	for name, bad := range map[string][]lifecycle.Produced{
		"missing": nil, "short": dashboardProduced(dashboardPassword[:31]), "long": dashboardProduced(dashboardPassword + "x"),
		"outside the alphabet": dashboardProduced("Xq7-Lm2_Rv9.Tk4-Wz8_Hn3.Pb6-Jd5!"), "two outputs": dashboardProduced(dashboardPassword, dashboardPassword),
		"another name": renamed, "another part": extraPart,
	} {
		err := validateProduced(bad, r, true)
		lifecycle.ClearProduced(bad)
		if err == nil {
			t.Fatalf("%s dashboard output accepted", name)
		}
		if strings.Contains(err.Error(), dashboardPassword[:8]) {
			t.Fatal("password in error")
		}
	}
	unoffered := dashboardProduced(dashboardPassword)
	defer lifecycle.ClearProduced(unoffered)
	if validateProduced(unoffered, r, false) == nil {
		t.Fatal("a dashboard password accepted from an unproved run")
	}
	pool := coreRequest()
	pool.Action = "pool"
	if validateProduced(unoffered, pool, true) == nil {
		t.Fatal("a pool block offered a dashboard password")
	}
}

func TestProducedClientMaterialRejectsAdminOrForeignCaps(t *testing.T) {
	r := coreRequest()
	r.Action = "client"
	r.Client = &Client{Name: "consumer", Pool: "volumes", Access: "read-only"}
	caps := map[string]string{"mon": "profile rbd", "mgr": "profile rbd-read-only pool=volumes", "osd": "profile rbd-read-only pool=volumes"}
	entries := []clientCredential{{Entity: "client.consumer", Key: base64.StdEncoding.EncodeToString([]byte("private-key")), Caps: caps}}
	encode := func() []lifecycle.Produced {
		data, _ := json.Marshal(entries)
		defer clear(data)
		return []lifecycle.Produced{{Name: "client", Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: data})}}
	}
	good := encode()
	defer lifecycle.ClearProduced(good)
	if err := validateProduced(good, r, true); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []func(){func() { entries[0].Entity = "client.admin" }, func() { entries[0].Entity = "client.consumer"; caps["osd"] = "allow *" }, func() { caps["osd"] = "profile rbd-read-only pool=volumes"; entries[0].Key = "not-base64" }} {
		mutation()
		bad := encode()
		err := validateProduced(bad, r, true)
		lifecycle.ClearProduced(bad)
		if err == nil {
			t.Fatal("foreign or excessive credential accepted")
		}
		if strings.Contains(err.Error(), "private-key") {
			t.Fatal("credential in error")
		}
	}
}

func TestHostTrustComesFromOriginalCompletedInstallation(t *testing.T) {
	r := coreRequest()
	h := r.Hosts[0]
	h.HostKeyRef = ""
	frozen := installation.Request{Version: "os-install-anaconda-v10", Address: h.Address, Identity: installation.Identity{Context: "original", Object: h.Machine, Profile: "profile"}}
	request, err := frozen.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	marker, err := installation.MarkerFor(frozen, "digest")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(installation.Evidence{Address: h.Address, HostKey: string(hostPublicKey()), Image: true, Marker: string(marker), Network: true, NetworkReboot: true, Postcondition: true, Power: "On", Reachable: true, Request: "digest"})
	proof := lifecycle.BlockEvidence{Kind: "Machine", Object: h.Machine, Implementation: installation.Implementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: raw, Block: reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Kind: "Machine", Object: h.Machine, Implementation: installation.Implementation, Request: request}, RequestDigest: "digest"}}
	e := lifecycle.Execution{ApplyOperation: "original", Operation: "destroy", Context: "renamed", ApplyProved: []lifecycle.BlockEvidence{proof}}
	if _, err := provedHostKey(h, e, dependencyProofs{}); err != nil {
		t.Fatal(err)
	}
	e.ApplyProved[0].Verb = reconciliation.Destroy
	if _, err := provedHostKey(h, e, dependencyProofs{}); err == nil {
		t.Fatal("destroy observation replaced installation identity")
	}
	e.ApplyProved = []lifecycle.BlockEvidence{proof}
	other := h
	other.Address = "192.0.2.200"
	if _, err := provedHostKey(other, e, dependencyProofs{}); err == nil {
		t.Fatal("host key reused at unproved address")
	}
	e.ApplyProved = nil
	if _, err := provedHostKey(h, e, dependencyProofs{}); err == nil {
		t.Fatal("missing original evidence replaced by live trust")
	}
}

func TestProvedHostKeyRequiresConvergedNetwork(t *testing.T) {
	h := coreRequest().Hosts[0]
	h.HostKeyRef = ""
	frozen := installation.Request{Version: "os-install-anaconda-v10", Address: h.Address, Identity: installation.Identity{Context: "original", Object: h.Machine, Profile: "profile"}}
	request, err := frozen.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	marker, err := installation.MarkerFor(frozen, "digest")
	if err != nil {
		t.Fatal(err)
	}
	execution := func(network, reboot bool) lifecycle.Execution {
		raw, _ := json.Marshal(installation.Evidence{Address: h.Address, HostKey: string(hostPublicKey()), Image: true, Marker: string(marker), Network: network, NetworkReboot: reboot, Postcondition: true, Power: "On", Reachable: true, Request: "digest"})
		proof := lifecycle.BlockEvidence{Kind: "Machine", Object: h.Machine, Implementation: installation.Implementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: raw, Block: reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Kind: "Machine", Object: h.Machine, Implementation: installation.Implementation, Request: request}, RequestDigest: "digest"}}
		return lifecycle.Execution{ApplyOperation: "original", Operation: "destroy", Context: "renamed", ApplyProved: []lifecycle.BlockEvidence{proof}}
	}
	if _, err := provedHostKey(h, execution(true, true), dependencyProofs{}); err != nil {
		t.Fatal(err)
	}
	if _, err := provedHostKey(h, execution(false, true), dependencyProofs{}); err == nil {
		t.Fatal("a host key was trusted before its network converged")
	}
	if _, err := provedHostKey(h, execution(true, false), dependencyProofs{}); err == nil {
		t.Fatal("a host key was trusted before its network was proved after a restart")
	}
}

type dependencyProofs struct{}

func (dependencyProofs) DataDisk(block reconciliation.Block, evidence []byte, path, serial string, size int64) bool {
	return libvirt.ProvedDataDisk(block, evidence, path, serial, size)
}
func (dependencyProofs) HostKey(proofs []lifecycle.BlockEvidence, machine, address string) (trust.HostKey, error) {
	return installation.ProvedHostKey(proofs, machine, address)
}

func (dependencyProofs) PackageTree(proofs []lifecycle.BlockEvidence, profile string) (PackageTree, error) {
	request, err := installation.ProvedPackageTree(proofs, profile)
	if err != nil {
		return PackageTree{}, err
	}
	return PackageTree{Context: request.Identity.Context, Media: request.Media.Name, MediaSHA256: request.Media.SHA256, MediaSize: request.Media.Size, Path: request.Publication.Path}, nil
}
