package ceph

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/trust"
)

const tieAddress = "198.51.100.10"

// tiebreakerCatalog adds a provided RHEL host at a third site to the
// four-host package catalog and names it the stretch tiebreaker. Its Machine
// names only the cluster SSH user, its pinned host key and an operator
// identity; the Environment's fleet key is the one Ceph dials it with.
func tiebreakerCatalog(change func(machine, cluster api.Value) (api.Value, api.Value)) (api.Catalog, api.Object) {
	objects := packageCatalogOf(4).Objects()
	tie := requestMap("os", requestMap("provided", true),
		"network", requestMap("addresses", api.ListValue(requestMap("name", "ip", "address", tieAddress))),
		"access", requestMap("ssh", requestMap("addressRef", "ip", "user", "operator", "knownHostsRef", "tie-known-hosts", "auth", requestMap("operatorIdentity", api.MapValue()))))
	objects = append(objects, api.NewObject(api.Environment, "environment", api.Value{}, requestMap("remoteMachinesAccessKey", requestMap("keyRef", "fleet-key"))),
		api.NewObject(api.Secret, "fleet-key", api.Value{}, requestMap("type", "sshKeyPair")))
	for i, object := range objects {
		if object.Kind() != api.CephCluster {
			continue
		}
		spec := object.Spec()
		nodes := spec.Get("topology", "nodes").Items()
		for i := range nodes {
			nodes[i] = nodes[i].With("site", api.StringValue([]string{"site-a", "site-b"}[i*2/len(nodes)]))
		}
		nodes = append(nodes, requestMap("name", "tie", "machineRef", "tie", "site", "site-c", "labels", api.StringList("mon")))
		spec = spec.WithPath(api.ListValue(nodes...), "topology", "nodes").WithPath(requestMap("failureDomain", "datacenter", "ruleName", "stretch-rule", "dataSites", api.StringList("site-a", "site-b"),
			"tiebreaker", requestMap("node", "tie", "site", "site-c")), "topology", "stretch").
			WithPath(api.StringList("192.0.2.0/24", "198.51.100.0/24"), "networks", "publicCIDRs")
		if change != nil {
			tie, spec = change(tie, spec)
		}
		objects[i] = object.WithSpec(spec)
	}
	objects = append(objects, api.NewObject(api.Machine, "tie", api.Value{}, tie))
	catalog := api.NewCatalog(objects)
	cluster, _ := catalog.Find(api.CephCluster, "example")
	return catalog, cluster
}

func boundTiebreaker(catalog api.Catalog, cluster api.Object) (Request, error) {
	spec := cluster.Spec()
	r := Request{Cluster: cluster.Name(), ClusterSSHUser: spec.Get("cephadm", "clusterSSH", "user").Text(), ClusterKeyRef: spec.Get("cephadm", "clusterSSH", "keyRef").Text(), Hosts: []Host{}}
	if spec.Get("security", "fips", "enabled").Bool() {
		r.Security = &Security{FIPS: true, CephXKeyType: "aes"}
	}
	err := bindHosts(&r, catalog, cluster, "synthetic")
	return r, err
}

func tieHost(t *testing.T, r Request) Host {
	t.Helper()
	for _, host := range r.Hosts {
		if host.Machine == "tie" {
			return host
		}
	}
	t.Fatalf("no tiebreaker host in %+v", r.Hosts)
	return Host{}
}

func TestAProvidedTiebreakerBindsAsAMONOnlyHostWithItsPinnedKeyAndTheBootstrapDVD(t *testing.T) {
	catalog, cluster := tiebreakerCatalog(nil)
	if _, err := baseRequest(catalog, cluster, "synthetic"); err != nil {
		t.Fatalf("a stretch cluster with a provided tiebreaker did not plan: %+v", diagnostics.Of(err))
	}
	r, err := boundTiebreaker(catalog, cluster)
	if err != nil {
		t.Fatal(err)
	}
	tie := tieHost(t, r)
	if !tie.Provided || tie.KnownHostsRef != "tie-known-hosts" || tie.HostKeyRef != "" || len(tie.Devices) != 0 || tie.Physical || tie.Address != tieAddress || tie.SSHKeyRef != "fleet-key" {
		t.Fatalf("tiebreaker host = %+v", tie)
	}
	for _, ref := range hostRequirements(r.Hosts...) {
		if ref.Object == "tie" {
			t.Fatal("a provided Machine, which no block realizes, became a block requirement")
		}
	}
	if len(hostRequirements(r.Hosts...)) != 4 || !strings.Contains(strings.Join(scopeOf(r).secretReferences(), ","), "tie-known-hosts") {
		t.Fatalf("requirements %v, references %v", hostRequirements(r.Hosts...), scopeOf(r).secretReferences())
	}
	packages, err := packageSources(catalog, r, "synthetic")
	if err != nil {
		t.Fatalf("%s %+v", r.Bootstrap, diagnostics.Of(err))
	}
	found := false
	for _, source := range packages.Sources {
		if source.Host == tie.Name {
			found = source.Profile == "rhel" && source.Media == "rhel-dvd.iso" && source.URL == "https://192.0.2.10:8443/os/rhel/tree"
		}
	}
	if !found || len(packages.Sources) != 5 {
		t.Fatalf("the tiebreaker did not take the bootstrap host's DVD source: %+v", packages.Sources)
	}
}

func TestAProvidedHostRefusesOutsideTheTiebreakerOrWithoutItsPinnedFleetAccess(t *testing.T) {
	for name, change := range map[string]func(machine, cluster api.Value) (api.Value, api.Value){
		"not the tiebreaker": func(m, c api.Value) (api.Value, api.Value) {
			return m, c.WithPath(api.StringValue("node-4"), "topology", "stretch", "tiebreaker", "node")
		},
		"the bootstrap host": func(m, c api.Value) (api.Value, api.Value) {
			return m, c.WithPath(api.StringValue("tie"), "cephadm", "bootstrap", "nodeRef")
		},
		"no stretch": func(m, c api.Value) (api.Value, api.Value) {
			return m, c.WithPath(api.MapValue(), "topology").WithPath(c.Get("topology", "nodes"), "topology", "nodes")
		},
		"no pinned host key": func(m, c api.Value) (api.Value, api.Value) {
			return m.WithPath(requestMap("addressRef", "ip", "user", "operator", "auth", requestMap("operatorIdentity", api.MapValue())), "access", "ssh"), c
		},
		"the fleet key as its access key": func(m, c api.Value) (api.Value, api.Value) {
			return m.WithPath(requestMap("privateKeyRef", "fleet-key"), "access", "ssh", "auth"), c
		},
		"the cluster key as its access key": func(m, c api.Value) (api.Value, api.Value) {
			return m.WithPath(requestMap("privateKeyRef", c.Get("cephadm", "clusterSSH", "keyRef").Text()), "access", "ssh", "auth"), c
		},
		"another user": func(m, c api.Value) (api.Value, api.Value) {
			return m.WithPath(api.StringValue("root"), "access", "ssh", "user"), c
		},
		"no SSH access": func(m, c api.Value) (api.Value, api.Value) {
			return m.WithPath(api.Value{}, "access"), c
		},
	} {
		t.Run(name, func(t *testing.T) {
			catalog, cluster := tiebreakerCatalog(change)
			if _, err := boundTiebreaker(catalog, cluster); err == nil || diagnostics.Of(err)[0].Code != "lifecycle.unsupported" {
				t.Fatalf("admitted: %v", err)
			}
		})
	}
}

// The tiebreaker's own access arm serves operator sessions only: a key of its
// own binds as well as an operator identity, and Ceph dials it with the fleet
// key either way, so without a declared fleet key it refuses.
func TestAProvidedTiebreakerIsDialedWithTheFleetKeyWhateverItsOwnAccessArm(t *testing.T) {
	catalog, cluster := tiebreakerCatalog(func(m, c api.Value) (api.Value, api.Value) {
		return m.WithPath(requestMap("privateKeyRef", "tie-operator-key"), "access", "ssh", "auth"), c
	})
	r, err := boundTiebreaker(catalog, cluster)
	if err != nil {
		t.Fatal(diagnostics.Of(err))
	}
	if tie := tieHost(t, r); tie.SSHKeyRef != "fleet-key" || slices.Contains(scopeOf(r).secretReferences(), "tie-operator-key") {
		t.Fatalf("the tiebreaker's operator key reached the Ceph request: %+v", tie)
	}
	for name, change := range map[string]func(m, c api.Value) (api.Value, api.Value){
		"a password arm": func(m, c api.Value) (api.Value, api.Value) {
			return m.WithPath(requestMap("passwordRef", "tie-operator-password"), "access", "ssh", "auth"), c
		},
		"the tiebreaker listed first": func(m, c api.Value) (api.Value, api.Value) {
			nodes := c.Get("topology", "nodes").Items()
			return m, c.WithPath(api.ListValue(append([]api.Value{nodes[len(nodes)-1]}, nodes[:len(nodes)-1]...)...), "topology", "nodes")
		},
	} {
		catalog, cluster := tiebreakerCatalog(change)
		r, err := boundTiebreaker(catalog, cluster)
		if err != nil || tieHost(t, r).SSHKeyRef != "fleet-key" || slices.ContainsFunc(r.Hosts, func(h Host) bool { return h.SSHKeyRef != "fleet-key" }) {
			t.Fatalf("%s: %+v %v", name, r.Hosts, diagnostics.Of(err))
		}
	}
	objects := slices.DeleteFunc(catalog.Objects(), func(o api.Object) bool { return o.Kind() == api.Environment })
	_, err = boundTiebreaker(api.NewCatalog(objects), cluster)
	if d := diagnostics.Of(err); len(d) != 1 || !strings.Contains(d[0].Message, "the Environment declares none") || !strings.Contains(d[0].Remediation, "spec.remoteMachinesAccessKey.keyRef") {
		t.Fatalf("no fleet key: %v", err)
	}
}

// A provided host's first fleet call is where its authorization is proved, so
// a connection that fails there names the fleet key, the user and the pin.
func TestAProvidedHostsFailedFleetConnectionNamesTheKeyToAuthorize(t *testing.T) {
	host := Host{Name: "tie", Machine: "tie", Address: tieAddress, Port: 22, User: "bootwright", SSHKeyRef: "fleet-key", KnownHostsRef: "tie-known-hosts", Provided: true, Devices: []Device{}}
	r := HostRequest{Version: HostVersion, Host: "tie", Cluster: ClusterScope{Controller: "controller", Hosts: []Host{host}}}
	runner := &refusingRunner{reason: "transport"}
	_, err := NewHost(runner).run(context.Background(), lifecycle.Execution{}, "destroy", r)
	d := diagnostics.Of(err)
	if len(d) != 1 || !strings.Contains(d[0].Message, "Machine/tie at "+tieAddress) || !strings.Contains(d[0].Remediation, "inspect the retained operation evidence") ||
		!strings.Contains(d[0].Remediation, "public half of the fleet key Secret fleet-key (bootwright secret show --name fleet-key --part public-key) for user bootwright on Machine/tie") || !strings.Contains(d[0].Remediation, "tie-known-hosts") {
		t.Fatalf("transport = %+v", d)
	}
	r.Cluster.Hosts[0].Provided = false
	if _, err := NewHost(runner).run(context.Background(), lifecycle.Execution{}, "destroy", r); strings.Contains(diagnostics.Of(err)[0].Message, "provided tiebreaker") {
		t.Fatal("an installed host's transport failure was read as a provided host's")
	}
}

func TestAFIPSClusterBindsAProvidedTiebreakerWithoutAnInstallProfile(t *testing.T) {
	catalog, cluster := tiebreakerCatalog(func(m, c api.Value) (api.Value, api.Value) {
		return m, c.WithPath(api.BoolValue(true), "security", "fips", "enabled")
	})
	_, err := boundTiebreaker(catalog, cluster)
	if d := diagnostics.Of(err); len(d) != 1 || !strings.Contains(d[0].Message, "FIPS Ceph requires an installed FIPS profile on Machine/node-1") {
		t.Fatalf("installed hosts lost their FIPS profile requirement, or the tiebreaker needed one: %v", err)
	}
}

func wire(parts ...[]byte) string {
	var blob bytes.Buffer
	for _, part := range parts {
		_ = binary.Write(&blob, binary.BigEndian, uint32(len(part)))
		blob.Write(part)
	}
	return base64.StdEncoding.EncodeToString(blob.Bytes())
}

func rsaKey(bits int) trust.HostKey {
	modulus := append([]byte{0}, bytes.Repeat([]byte{0xff}, bits/8)...)
	return trust.HostKey{Type: "ssh-rsa", PublicKey: wire([]byte("ssh-rsa"), []byte{1, 0, 1}, modulus)}
}

func TestAFIPSProvidedHostKeyIsECDSAOrRSAOfAtLeast2048Bits(t *testing.T) {
	host := Host{Machine: "tie", KnownHostsRef: "tie-known-hosts"}
	for _, test := range []struct {
		key      trust.HostKey
		admitted bool
	}{
		{trust.HostKey{Type: "ecdsa-sha2-nistp256", PublicKey: wire([]byte("ecdsa-sha2-nistp256"), []byte("nistp256"), bytes.Repeat([]byte{4}, 65))}, true},
		{rsaKey(2048), true},
		{rsaKey(1024), false},
		{trust.HostKey{Type: "ssh-ed25519", PublicKey: wire([]byte("ssh-ed25519"), bytes.Repeat([]byte{1}, 32))}, false},
	} {
		if err := fipsHostKey(host, test.key); (err == nil) != test.admitted {
			t.Fatalf("%s %d bytes: %v", test.key.Type, len(test.key.PublicKey), err)
		}
	}
}

func TestAProvidedHostKeyIsTheOnePinnedKnownHostsEntryForItsAddress(t *testing.T) {
	host := Host{Machine: "tie", Address: tieAddress, Port: 22, KnownHostsRef: "tie-known-hosts", Provided: true}
	key := rsaKey(2048)
	material := func(value string) lifecycle.Execution {
		return lifecycle.Execution{Material: map[string]secrets.Material{"tie-known-hosts": secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(value)})}}
	}
	got, err := provedHostKey(host, material(key.Line(tieAddress, 22)), nil)
	if err != nil || got != key {
		t.Fatalf("pinned key = %+v, %v", got, err)
	}
	if _, err := provedHostKey(host, material(key.Line("198.51.100.11", 22)), nil); err == nil {
		t.Fatal("a known-hosts entry for another address was admitted")
	}
}

func TestAProvidedHostOfAFIPSClusterIsPreparedOnlyWithItsObservedFIPSMode(t *testing.T) {
	r := HostRequest{Host: "tie", Cluster: ClusterScope{Security: &Security{FIPS: true, CephXKeyType: "aes"}, Hosts: []Host{{Name: "tie", Machine: "tie", Provided: true, Devices: []Device{}}}}}
	digest := strings.Repeat("a", 64)
	evidence := HostEvidence{Ready: true, Route: true, ImageID: digest, UID: 167, GID: 167, Label: SharedLabel, PolicySHA256: digest, CephadmSHA256: digest, PlanDigest: digest, Devices: []HostDevice{}}
	for _, test := range []struct {
		fips     *HostFIPS
		prepared bool
	}{
		{&HostFIPS{Kernel: "1", Policy: "FIPS"}, true},
		{&HostFIPS{Kernel: "1", Policy: "FIPS:OSPP"}, false},
		{&HostFIPS{Kernel: "0", Policy: "FIPS"}, false},
		{nil, false},
	} {
		evidence.FIPS = test.fips
		if evidence.prepared(r) != test.prepared {
			t.Fatalf("fips %+v: prepared %v", test.fips, !test.prepared)
		}
	}
	r.Cluster.Security = nil
	evidence.FIPS = &HostFIPS{Kernel: "1", Policy: "FIPS"}
	if evidence.prepared(r) {
		t.Fatal("a non-FIPS cluster's host evidence carried a FIPS observation")
	}
}

func TestAClusterBlockReadsAProvidedHostsFIPSModeFromItsHostBlock(t *testing.T) {
	scope := ClusterScope{Cluster: "example", Version: Version, Security: &Security{FIPS: true, CephXKeyType: "aes"}, Packages: &PackageRequest{Sources: []PackageSource{{Host: "tie"}}},
		Hosts: []Host{{Name: "tie", Machine: "tie", Provided: true, Devices: []Device{}}}}
	request, err := HostRequest{Version: HostVersion, Host: "tie", Cluster: scope}.canonical()
	if err != nil {
		t.Fatal(err)
	}
	proof := func(fips *HostFIPS) []lifecycle.BlockEvidence {
		evidence := []byte(`{"version":"` + HostEvidenceVersion + `","request":"` + strings.Repeat("b", 64) + `","host":"tie","devices":[]}`)
		if fips != nil {
			evidence = []byte(`{"version":"` + HostEvidenceVersion + `","request":"` + strings.Repeat("b", 64) + `","host":"tie","devices":[],"fips":{"kernel":"` + fips.Kernel + `","policy":"` + fips.Policy + `"}}`)
		}
		block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{Kind: string(api.CephCluster), Object: "example", Implementation: HostImplementation, Request: request}, RequestDigest: strings.Repeat("b", 64)}
		return []lifecycle.BlockEvidence{{Block: block, Kind: block.Kind, Object: block.Object, Implementation: block.Implementation, Verb: reconciliation.Apply, State: reconciliation.BlockDone, Evidence: evidence}}
	}
	if err := observedFIPS(proof(&HostFIPS{Kernel: "1", Policy: "FIPS"}), "example", "tie"); err != nil {
		t.Fatal(err)
	}
	for _, fips := range []*HostFIPS{nil, {Kernel: "1", Policy: "FIPS:OSPP"}} {
		err := observedFIPS(proof(fips), "example", "tie")
		if d := diagnostics.Of(err); len(d) != 1 || !strings.Contains(d[0].Message, "does not report FIPS mode") || !strings.Contains(d[0].Remediation, "never switches") {
			t.Fatalf("fips %+v: %v", fips, err)
		}
	}
}
