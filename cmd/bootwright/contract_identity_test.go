//go:build linux && amd64

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/addons/clusterobjects"
	"github.com/crmarques/bootwright/internal/addons/instance"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/containercluster/networking"
	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/dnsserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/infrastructureservices/ntpserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/proxy"
	"github.com/crmarques/bootwright/internal/machine/power"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/storage/ceph"
	"github.com/crmarques/bootwright/internal/substrate"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

// The role each value a content digest hashes plays in it: a domain tag that
// separates digests, the implementation identity, the request version the
// frozen request carries, another declared version, or an effect pin, a value
// Go owns outside the frozen request that fixes what an effect is.
const (
	digestDomain         = "domain"
	digestImplementation = "implementation"
	digestRequest        = "request"
	digestVersion        = "version"
	digestPin            = "pin"
)

type digestPart struct{ role, value string }

// digestRecipe is what one content digest hashes, in order, joined by its
// separator. TestEveryImplementationMovesItsContentDigestWithItsContract holds
// it to the digest the implementation computes, so its effect pins are
// exactly the ones the contract golden lists.
type digestRecipe struct {
	separator string
	parts     []digestPart
}

func digestOf(parts ...digestPart) digestRecipe { return digestRecipe{separator: "\x00", parts: parts} }

func partDomain(value string) digestPart         { return digestPart{digestDomain, value} }
func partImplementation(value string) digestPart { return digestPart{digestImplementation, value} }
func partRequest(value string) digestPart        { return digestPart{digestRequest, value} }
func partVersion(value string) digestPart        { return digestPart{digestVersion, value} }
func partPin(value string) digestPart            { return digestPart{digestPin, value} }

func (r digestRecipe) digest() string {
	values := make([]string, 0, len(r.parts))
	for _, part := range r.parts {
		values = append(values, part.value)
	}
	sum := sha256.Sum256([]byte(strings.Join(values, r.separator)))
	return hex.EncodeToString(sum[:])
}

// requestVersionOf is the version the frozen request carries: the request
// part, or the implementation identity where the request is versioned by it.
func (r digestRecipe) requestVersionOf() string {
	named := ""
	for _, part := range r.parts {
		switch part.role {
		case digestRequest:
			return part.value
		case digestImplementation:
			named = part.value
		}
	}
	return named
}

func (r digestRecipe) pins() []string {
	var pins []string
	for _, part := range r.parts {
		if part.role == digestPin {
			pins = append(pins, part.value)
		}
	}
	return pins
}

// contractRecipes is every implementation's digest recipe. The values Go keeps
// unexported are spelled out, so moving one fails the recipe until it is
// restated here.
func contractRecipes() map[string]digestRecipe {
	recipes := map[string]digestRecipe{
		instance.Implementation: digestOf(partImplementation(instance.Implementation), partRequest("addon-instance-request-v2"), partVersion("addon-instance-evidence-v3"),
			partVersion(clusterobjects.Version), partPin(clusterobjects.OwnerDomain), partPin(clusterobjects.ObjectDomain), partPin(clusterobjects.ExactRule),
			partPin(clusterobjects.OperatorManagedRule), partPin(clusterobjects.DependentExactRule)),
		networking.Implementation: digestOf(partImplementation(networking.Implementation), partRequest("cluster-network-request-v2"), partVersion("cluster-network-evidence-v3"),
			partVersion(clusterobjects.Version)),
		clients.Implementation: digestOf(partDomain("bootwright.controller.client-stage-v1"), partImplementation(clients.Implementation), partRequest(clients.Version)),
		libvirt.HostImplementation: digestOf(partDomain("bootwright.substrate.libvirt.host-v1"), partImplementation(libvirt.HostImplementation), partRequest("substrate-host-libvirt-v6"),
			partPin(strings.Join(libvirt.HypervisorPackages(), ",")), partPin(substrate.ImagePrefix), partPin(substrate.Prefix), partPin(strings.Join(libvirt.ServiceUnits(), ",")), partPin("bridge-use-claim")),
		libvirt.MachineImplementation: digestOf(partDomain("bootwright.substrate.libvirt.machine-v1"), partImplementation(libvirt.MachineImplementation), partRequest("machine-libvirt-v8"),
			partPin("quay.io/metal3-io/sushy-tools@sha256:f760343718e1175343f230ec43496a4f0d850f1d0a3139b92db1e475ac91d11e"),
			partPin(substrate.ImagePrefix), partPin(substrate.Prefix), partPin("data-media-ssd-scsi-v1"), partPin("shared-bmc-claim")),
		baremetal.Implementation: digestOf(partDomain("bootwright.substrate.baremetal.machine-v1"), partImplementation(baremetal.Implementation), partRequest("machine-baremetal-v2"),
			partPin("identity+exact-addresses+power+system-claim")),
		installation.TreeImplementation:  digestOf(partImplementation(installation.TreeImplementation), partVersion(installation.TreeEvidenceVersion)),
		installation.EraseImplementation: {separator: ":", parts: []digestPart{partImplementation(installation.EraseImplementation), partPin("descriptor-diskseq-full-readback-receipt-v1")}},
		installation.Implementation: digestOf(partDomain("bootwright.managedos.install-anaconda-v1"), partImplementation(installation.Implementation), partRequest("os-install-anaconda-v19"),
			partVersion("kickstart-anaconda-v15"), partPin("os"), partPin("private"), partPin("public"), partPin(installation.MarkerPath), partPin(installation.HostKeyPath),
			partPin(installation.NetworkDocumentPath), partPin(installation.NetworkMarkerPath), partPin(strconv.Itoa(300)), partPin("lorax,xorriso"), partPin("proved-installation-disk-boot,replay-safe-unread-registration")),
		agentinstall.MediaImplementation: digestOf(partDomain("bootwright.containercluster.media-agent-v1"), partImplementation(agentinstall.MediaImplementation), partRequest("cluster-media-agent-v9"),
			partPin("clusters"), partPin(agentinstall.ImageFile), partPin("/var/lib/bootwright-clusters"), partPin("openshift-install")),
		agentinstall.InstallImplementation: digestOf(partDomain("bootwright.containercluster.install-agent-v1"), partImplementation(agentinstall.InstallImplementation), partRequest("cluster-install-agent-v8"),
			partPin("clusters"), partPin(agentinstall.ImageFile), partPin("/var/lib/bootwright-clusters"), partPin("openshift-install"), partPin("openshift-clients")),
		artifactserver.Implementation: digestOf(partDomain("bootwright.infrastructureservices.artifact-server-v1"), partImplementation(artifactserver.Implementation), partRequest("artifact-server-nginx-v2"),
			partPin("registry.access.redhat.com/ubi9/nginx-124@sha256:bff0f204cfef8af0b21a2e683a9d1231da98a429d856ee29ebd0e2691d919b56"),
			partPin("/var/lib/bootwright-services"), partPin("bootwright")),
		ceph.Implementation: digestOf(partImplementation(ceph.Implementation), partRequest(ceph.Version), partVersion(ceph.EvidenceVersion), partVersion(ceph.PackageVersion),
			partPin("selected-tunnel-image-v3")),
		ceph.ImageImplementation: digestOf(partImplementation(ceph.ImageImplementation), partRequest(ceph.ImageVersion), partPin("selected-artifact-manifest-v2")),
		ceph.HostImplementation: digestOf(partImplementation(ceph.HostImplementation), partRequest(ceph.HostVersion), partVersion(ceph.HostEvidenceVersion),
			partPin("selected-packages-pull-profile-route-devices-v3")),
		ceph.PhysicalRetirementImplementation: {separator: ":", parts: []digestPart{partImplementation(ceph.PhysicalRetirementImplementation), partPin("original-host-and-completed-erase-proof-v1")}},
		ceph.SMBImplementation: digestOf(partImplementation(ceph.SMBImplementation), partRequest(ceph.SMBVersion), partVersion(ceph.SMBEvidenceVersion),
			partPin(ceph.PhysicalRetirementImplementation)),
		ceph.ReplicationImplementation: digestOf(partImplementation(ceph.ReplicationImplementation), partRequest(ceph.ReplicationVersion), partVersion(ceph.ReplicationEvidenceVersion),
			partPin(ceph.PhysicalRetirementImplementation)),
		ceph.ConsumerImplementation: digestOf(partImplementation(ceph.ConsumerImplementation), partRequest(ceph.ConsumerVersion), partVersion(ceph.ConsumerEvidenceVersion)),
	}
	for _, definition := range []managedservice.Definition{proxy.Definition(), dnsServerDefinition(), ntpserver.Definition()} {
		recipes[definition.Implementation] = digestOf(partDomain("bootwright.infrastructureservices.managed-service-v1"), partImplementation(definition.Implementation),
			partRequest(definition.Version), partPin(definition.Image), partPin(managedservice.ContentRootPrefix), partPin(managedservice.UnitPrefix))
	}
	return recipes
}

// requestIdentities names, for each implementation, the members of its frozen
// request that fix an effect's identity, place, ownership mark or claimed host
// resource: what a removal reads from the request its apply froze rather than
// from the build in hand. Each must be a member of the request schema.
func requestIdentities() map[string][]string {
	service := []string{"identity.context", "identity.service", "identity.block", "contentRoot", "unit", "bindAddress", "port", "placement.machine"}
	identities := map[string][]string{
		instance.Implementation:   {"cluster", "package", "binding", "placement.machine"},
		networking.Implementation: {"cluster", "placement.machine"},
		clients.Implementation:    {"packages", "tools[].kind"},
		libvirt.HostImplementation: {"identity.context", "identity.object", "identity.block", "uri", "poolName", "poolPath", "networks[].name",
			"networks[].bridge", "firewallReadyUnit"},
		libvirt.MachineImplementation: {"identity.context", "identity.object", "identity.block", "uri", "domain", "uuid", "directory", "poolName",
			"poolPath", "disks[].name", "disks[].path", "interfaces[].macAddress", "controller.unit", "controller.port"},
		baremetal.Implementation:        {"identity.context", "identity.object", "identity.block", "provider", "controller.endpoint", "hardware[].macAddress"},
		installation.TreeImplementation: {"identity.context", "identity.profile", "media.name", "publication.path", "placement.machine"},
		installation.EraseImplementation: {"install.identity.context", "install.identity.object", "install.identity.block", "installDigest",
			"targets[].deviceName", "targets[].serialNumber", "targets[].wwn"},
		installation.Implementation: {"identity.context", "identity.object", "identity.block", "identity.profile", "hostname", "markerPath",
			"hostKeyPath", "network.documentPath", "network.markerPath", "private.path", "image.path", "target.domain", "target.uri",
			"target.hardware.rootDevice", "target.hardware.serialNumber", "target.hardware.wwn", "placement.machine"},
		agentinstall.MediaImplementation: {"identity.context", "identity.cluster", "identity.block", "workRoot", "image.path", "placement.machine"},
		agentinstall.InstallImplementation: {"identity.context", "identity.cluster", "identity.block", "workRoot", "image.path", "nodes[].name",
			"nodes[].machine", "vips[].address", "placement.machine"},
		artifactserver.Implementation: {"identity.context", "identity.service", "identity.block", "contentRoot", "unit", "bindAddress",
			"listeners[].port", "placement.machine"},
		ceph.Implementation: {"cluster", "fsid", "owner", "clusterSSHUser", "cephadm.path", "hosts[].name", "hosts[].machine",
			"hosts[].devices[].path", "hosts[].devices[].serial", "hosts[].devices[].wwn", "pool.name", "rbd.name", "filesystem.name",
			"subvolume.name", "client.name", "service.id", "objectUser.name", "objectBucket.name", "nvmeof.name", "nfsExport.name", "policy.name"},
		ceph.ImageImplementation: {"cluster", "image", "registry", "auxiliary[].image"},
		ceph.HostImplementation: {"host", "cluster.cluster", "cluster.fsid", "cluster.owner", "cluster.clusterSSHUser", "cluster.cephadm.path",
			"cluster.hosts[].name", "cluster.hosts[].machine", "cluster.hosts[].devices[].path", "cluster.hosts[].devices[].serial",
			"cluster.hosts[].devices[].wwn"},
		ceph.PhysicalRetirementImplementation: {"cluster", "hosts[].name", "hosts[].machine", "hosts[].eraseWWNs", "hosts[].devices[].wwn"},
		ceph.SMBImplementation:                {"cluster", "object", "id", "clusterIDs", "scope", "resourceType", "service.name"},
		ceph.ReplicationImplementation: {"object", "primary.cluster", "primary.poolObject", "primary.scope", "secondary.cluster",
			"secondary.poolObject", "secondary.scope", "images[].name"},
		ceph.ConsumerImplementation: {"cluster", "name", "scope", "principals"},
	}
	for _, definition := range []managedservice.Definition{proxy.Definition(), dnsServerDefinition(), ntpserver.Definition()} {
		identities[definition.Implementation] = service
	}
	return identities
}

// TestCheckCommitsContractPathsCoverEveryImplementation holds the paths on
// which scripts/check-commits asks for a Contract line to every package that
// defines an implementation's request, evidence, managed-service definition or
// power content digest, and to the collection's meta, playbooks, plugins and
// roles, and holds each listed path to a directory that exists.
func TestCheckCommitsContractPathsCoverEveryImplementation(t *testing.T) {
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "check-commits"))
	if err != nil {
		t.Fatal(err)
	}
	match := regexp.MustCompile(`(?s)\ncontract_paths='\n(.*?)'\n`).FindSubmatch(script)
	if match == nil {
		t.Fatal("scripts/check-commits declares no contract_paths")
	}
	prefixes := strings.Fields(string(match[1]))
	for _, prefix := range prefixes {
		if info, err := os.Stat(filepath.Join("..", "..", filepath.FromSlash(prefix))); err != nil || !info.IsDir() || !strings.HasSuffix(prefix, "/") {
			t.Errorf("contract path %s is not a directory of this repository ending in /", prefix)
		}
	}
	const module = "github.com/crmarques/bootwright/"
	required := map[string]string{
		"ansible/" + automationCollection + "meta/":      "the collection's meta",
		"ansible/" + automationCollection + "playbooks/": "the collection's playbooks",
		"ansible/" + automationCollection + "plugins/":   "the collection's plugins",
		"ansible/" + automationCollection + "roles/":     "the collection's roles",
	}
	for _, version := range contractVersions() {
		for _, defined := range []reflect.Type{version.request, version.evidence} {
			required[strings.TrimPrefix(defined.PkgPath(), module)+"/"] = version.implementation
		}
	}
	for _, definition := range []any{proxy.Definition, dnsserver.Definition, ntpserver.Definition, power.ContentDigest, power.ReadContentDigest} {
		name := runtime.FuncForPC(reflect.ValueOf(definition).Pointer()).Name()
		required[strings.TrimPrefix(name[:strings.LastIndex(name, ".")], module)+"/"] = name
	}
	for directory, owner := range required {
		if !slices.ContainsFunc(prefixes, func(prefix string) bool { return strings.HasPrefix(directory, prefix) }) {
			t.Errorf("scripts/check-commits asks no Contract line for %s, which defines %s: add it to contract_paths", directory, owner)
		}
	}
}
