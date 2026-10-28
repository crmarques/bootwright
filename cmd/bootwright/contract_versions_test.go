//go:build linux && amd64

package main

import (
	"encoding"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/addons/clusterobjects/driver"
	"github.com/crmarques/bootwright/internal/addons/instance"
	"github.com/crmarques/bootwright/internal/containercluster/agentinstall"
	"github.com/crmarques/bootwright/internal/containercluster/networking"
	"github.com/crmarques/bootwright/internal/controller/clients"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/controller/runtimelock"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/infrastructureservices/ntpserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/proxy"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/storage/ceph"
	"github.com/crmarques/bootwright/internal/substrate/baremetal"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

// contractVersion is one implementation's contract: its content digest and
// the Go types its request and evidence encode from.
type contractVersion struct {
	implementation string
	digest         string
	request        reflect.Type
	evidence       reflect.Type
}

func contractVersions() []contractVersion {
	versions := []contractVersion{
		{instance.Implementation, instance.ContentDigest(), reflect.TypeFor[instance.Request](), reflect.TypeFor[instance.Evidence]()},
		{networking.Implementation, networking.ContentDigest(), reflect.TypeFor[networking.Request](), reflect.TypeFor[driver.Evidence]()},
		{clients.Implementation, clients.ContentDigest(), reflect.TypeFor[clients.Request](), reflect.TypeFor[clients.Evidence]()},
		{libvirt.HostImplementation, libvirt.HostContentDigest(), reflect.TypeFor[libvirt.HostRequest](), reflect.TypeFor[libvirt.HostEvidence]()},
		{libvirt.MachineImplementation, libvirt.MachineContentDigest(), reflect.TypeFor[libvirt.MachineRequest](), reflect.TypeFor[libvirt.MachineEvidence]()},
		{baremetal.Implementation, baremetal.ContentDigest(), reflect.TypeFor[baremetal.Request](), reflect.TypeFor[baremetal.Evidence]()},
		{installation.TreeImplementation, installation.TreeContentDigest(), reflect.TypeFor[installation.TreeRequest](), reflect.TypeFor[installation.TreeEvidence]()},
		{installation.EraseImplementation, installation.EraseContentDigest(), reflect.TypeFor[installation.EraseRequest](), reflect.TypeFor[installation.EraseEvidence]()},
		{installation.Implementation, installation.ContentDigest(), reflect.TypeFor[installation.Request](), reflect.TypeFor[installation.Evidence]()},
		{agentinstall.MediaImplementation, agentinstall.MediaContentDigest(), reflect.TypeFor[agentinstall.MediaRequest](), reflect.TypeFor[agentinstall.MediaEvidence]()},
		{agentinstall.InstallImplementation, agentinstall.InstallContentDigest(), reflect.TypeFor[agentinstall.InstallRequest](), reflect.TypeFor[agentinstall.InstallEvidence]()},
		{artifactserver.Implementation, artifactserver.ContentDigest(), reflect.TypeFor[artifactserver.Request](), reflect.TypeFor[artifactserver.Evidence]()},
		{ceph.Implementation, ceph.ContentDigest(), reflect.TypeFor[ceph.Request](), reflect.TypeFor[ceph.Evidence]()},
		{ceph.ImageImplementation, ceph.ImageContentDigest(), reflect.TypeFor[ceph.ImageRequest](), reflect.TypeFor[ceph.ImageEvidence]()},
		{ceph.HostImplementation, ceph.HostContentDigest(), reflect.TypeFor[ceph.HostRequest](), reflect.TypeFor[ceph.HostEvidence]()},
		{ceph.PhysicalRetirementImplementation, ceph.PhysicalRetirementContentDigest(), reflect.TypeFor[ceph.PhysicalRetirementRequest](), reflect.TypeFor[ceph.PhysicalRetirementEvidence]()},
		{ceph.SMBImplementation, ceph.SMBContentDigest(), reflect.TypeFor[ceph.SMBRequest](), reflect.TypeFor[ceph.SMBEvidence]()},
		{ceph.ReplicationImplementation, ceph.ReplicationContentDigest(), reflect.TypeFor[ceph.ReplicationRequest](), reflect.TypeFor[ceph.ReplicationEvidence]()},
		{ceph.ConsumerImplementation, ceph.ConsumerContentDigest(), reflect.TypeFor[ceph.ConsumerRequest](), reflect.TypeFor[ceph.ConsumerEvidence]()},
	}
	for _, definition := range []managedservice.Definition{proxy.Definition(), dnsServerDefinition(), ntpserver.Definition()} {
		versions = append(versions, contractVersion{definition.Implementation, definition.ContentDigest(),
			reflect.TypeFor[managedservice.Request](), reflect.TypeFor[managedservice.Evidence]()})
	}
	return versions
}

// Automation literal classes: what a versioned literal, or a literal naming a
// Bootwright host path, name prefix or mark, in the embedded collection names.
// A persisted format or a fixed value belongs to the contract of every
// implementation it names; the others are already a content-digest input,
// carried by the frozen request, or gated by the automation digest.
//
//   - persisted: a record format the automation writes on a host and a later
//     run reads back;
//   - mirrored: a version Go also holds, which the role refuses unless the
//     frozen request or evidence names it;
//   - transient: a value that lives within one run and is never read across
//     runs, such as a probe's scratch or a temporary name;
//   - controller: the controller stage's own format or store, not an effect's;
//   - fixed: a host path, name prefix, ownership mark or proof identifier the
//     automation fixes outside the frozen request, so a removal under another
//     build reads it from the build in hand (the exceptions to the rule that a
//     removal reads its target's identity from the frozen request);
//   - validated: a value that only bounds what a frozen request may carry, so
//     another build's value refuses the request rather than relocating it;
//   - documentation: text that runs nothing.
const (
	formatPersisted     = "persisted"
	formatMirrored      = "mirrored"
	formatTransient     = "transient"
	formatController    = "controller"
	formatFixed         = "fixed"
	formatValidated     = "validated"
	formatDocumentation = "documentation"
)

// The kinds of a persisted or fixed literal, each its own contract golden
// section: a mark that proves an object this context's own, the identifier of
// what an observation proves, and a place, the host path or name prefix where
// an effect lives.
const (
	kindMark  = "mark"
	kindProof = "proof"
	kindPlace = "place"
)

// automationFormat classifies one literal; a persisted or fixed one names
// every implementation whose effects it versions, places, marks or proves,
// and every class but persisted and mirrored records its reason.
type automationFormat struct {
	class           string
	kind            string
	implementations []string
	reason          string
}

func persistedBy(implementations ...string) automationFormat {
	return automationFormat{class: formatPersisted, implementations: implementations}
}

func fixedBy(kind, reason string, implementations ...string) automationFormat {
	return automationFormat{class: formatFixed, kind: kind, implementations: implementations, reason: reason}
}

func transient(reason string) automationFormat {
	return automationFormat{class: formatTransient, reason: reason}
}

func validated(reason string) automationFormat {
	return automationFormat{class: formatValidated, reason: reason}
}

// automationFormats classifies every literal the embedded collection carries
// that ends in a version or names a Bootwright host path, name prefix or
// mark, by where each is written and read.
func automationFormats() map[string]automationFormat {
	host, core := persistedBy(ceph.HostImplementation), persistedBy(ceph.Implementation)
	formats := map[string]automationFormat{
		"addon-instance-request-v2":  {class: formatMirrored},
		"ceph-export-request-v2":     {class: formatMirrored},
		"ceph-export-evidence-v1":    {class: formatMirrored},
		"cluster-network-request-v2": {class: formatMirrored},
		"cluster-objects-v2":         {class: formatMirrored},
		// The host block writes them; the core block's workers read them back
		// for the manager account, the cephadm cache and the OSD identities.
		"ceph-runtime-profile-v3":         persistedBy(ceph.HostImplementation, ceph.Implementation),
		"ceph-physical-device-binding-v1": persistedBy(ceph.HostImplementation, ceph.Implementation),
		"ceph-device-identities-v2":       persistedBy(ceph.HostImplementation, ceph.Implementation),
		"ceph-host-route-v3":              host,
		"ceph-package-decision-v1":        host,
		"ceph-package-artifacts-v1":       host,
		"ceph-package-complete-v1":        host,
		"ceph-crypto-policy-v1":           host,
		// The resolved package plan the host's package decision record holds.
		"bootwright.native-plan-v1": host,

		"ceph-tunnel-v3":                    core,
		"ceph-tunnel-custody-v1":            core,
		"ceph-replication-custody-v1":       persistedBy(ceph.ReplicationImplementation),
		"ceph-tunnel-relabel-v1":            core,
		"ceph-tunnel-stage-v1":              core,
		"ceph-tunnel-redeploy-v1":           core,
		"ceph-bootstrap-recovery-v1":        core,
		"ceph-bootstrap-state-v1":           core,
		"ceph-registry-v1":                  core,
		"ceph-native-teardown-v1":           core,
		"ceph-pool-attempt-v2":              core,
		"ceph-policy-attempt-v1":            core,
		"ceph-rbd-image-attempt-v1":         core,
		"ceph-filesystem-attempt-v1":        core,
		"ceph-subvolume-group-attempt-v1":   core,
		"ceph-subvolume-attempt-v1":         core,
		"ceph-client-auth-v1":               core,
		"ceph-dashboard-redeploy-v1":        core,
		"ceph-dashboard-port-repair-v1":     core,
		"ceph-dashboard-port-attempt-v1":    core,
		"ceph-dashboard-user-v1":            core,
		"ceph-cluster-device-identities-v2": core,
		"ceph-osd-device-intent-v1":         core,
		"ceph-service-inputs-v1":            core,
		// The core block's cache phase publishes cephadm on each host under it.
		"ceph-host-cache-v1": core,
		// The client and dashboard credential custody records, by kind.
		"-custody-v1": persistedBy(ceph.Implementation, ceph.ConsumerImplementation),

		// The namespace of the ownership metadata a libvirt network, pool or
		// domain carries.
		"https://bootwright.io/substrate/v1": {class: formatPersisted, kind: kindMark,
			implementations: []string{libvirt.HostImplementation, libvirt.MachineImplementation}},

		"ceph-host-image-v1":                         transient("the image facts a contained probe prints and the same call reads back"),
		"ceph-host-device-id-v1":                     transient("the device identity a contained probe prints and the same call reads back"),
		"bootwright-ceph-route-probe-v1":             transient("the result schema of the route probe, read back by the call that ran it"),
		"bootwright-ceph-route-prepare-v1":           transient("the result schema of the route probe's preparation, read back by the call that ran it"),
		"bootwright-ceph-route-synthetic-v1":         transient("the synthetic account the route probe creates and checks within one call"),
		"bootwright-ceph-metadata-v1":                transient("the registry client's User-Agent"),
		"bootwright-pull-broker-v1":                  transient("the pull broker's User-Agent"),
		"bootwright-contained-public-label-probe-v1": transient("the bytes the label probe writes in its own scratch and reads back"),
		"ceph-packages-v2":                           transient("the guest package transaction policy ceph_packages.transaction_request builds and the shared resolver reads within one run"),

		"ceph-host-v9":                 {class: formatMirrored},
		"ceph-host-evidence-v4":        {class: formatMirrored},
		"ceph-core-v34":                {class: formatMirrored},
		"ceph-core-evidence-v12":       {class: formatMirrored},
		"ceph-packages-v3":             {class: formatMirrored},
		"ceph-image-v4":                {class: formatMirrored},
		"ceph-smb-v1":                  {class: formatMirrored},
		"ceph-smb-evidence-v1":         {class: formatMirrored},
		"os-package-tree-evidence-v1":  {class: formatMirrored},
		"ceph-replication-v1":          {class: formatMirrored},
		"ceph-replication-evidence-v1": {class: formatMirrored},
		// The request versions each role refuses unless its request names.
		"substrate-host-libvirt-v6": {class: formatMirrored},
		"machine-libvirt-v8":        {class: formatMirrored},
		"machine-baremetal-v2":      {class: formatMirrored},
		"os-package-tree-v1":        {class: formatMirrored},
		"os-install-anaconda-v21":   {class: formatMirrored},
		"physical-erase-v6":         {class: formatMirrored},
		"cluster-media-agent-v9":    {class: formatMirrored},
		"cluster-install-agent-v10": {class: formatMirrored},
		"artifact-server-nginx-v2":  {class: formatMirrored},
		"managed-service-v4":        {class: formatMirrored},
		// Power is no plan block: each run binds its request to the power
		// content digest these implementations feed, and nothing is retained.
		"machine-power-v3":      {class: formatMirrored},
		"machine-power-read-v3": {class: formatMirrored},

		"controller-prerequisites-v6": {class: formatController, reason: "the controller stage's request, which setup applies and no plan block retains"},
	}
	for literal, format := range automationIdentities() {
		if _, twice := formats[literal]; twice {
			panic(literal + " is classified twice")
		}
		formats[literal] = format
	}
	return formats
}

// automationIdentities classifies the literals naming a Bootwright host path,
// name prefix or mark (TestEveryAutomationVersionLiteralIsClassified scans
// them): the exact list of values a removal may read from the build in hand
// rather than from its frozen request, and why every other one is safe.
func automationIdentities() map[string]automationFormat {
	core, host := ceph.Implementation, ceph.HostImplementation
	media, install := agentinstall.MediaImplementation, agentinstall.InstallImplementation
	addon, network := instance.Implementation, networking.Implementation
	anaconda, tree, erase := installation.Implementation, installation.TreeImplementation, installation.EraseImplementation
	scratch := transient("a temporary name the call that creates it removes or renames before it returns")
	inProcess := transient("a name the plugin gives a module it loads into its own process")
	eraseRuntime := transient("the erase environment's RAM-only runtime, which exists only while that boot runs")
	scriptName := transient("the $0 word of a fixed shell probe")
	identities := map[string]automationFormat{
		"bootwright.cluster-objects.owner-v1":     fixedBy(kindMark, "the domain an applied object's owner annotation is derived under", addon, network),
		"bootwright.io/owner":                     fixedBy(kindMark, "the annotation key of an applied object's owner mark", addon, network),
		"bootwright-":                             fixedBy(kindPlace, "the prefix of the server-side apply field manager and of the install's wildcard DNS label", addon, network, install),
		"bootwright-cluster-install":              fixedBy(kindPlace, "the field manager of the JSON patch that places a completed cluster's node labels and taints", install),
		"bootwright-owner":                        fixedBy(kindMark, "the key of the owner mark on a Ceph resource", core, ceph.ConsumerImplementation, ceph.SMBImplementation, ceph.ReplicationImplementation),
		"bootwright:":                             fixedBy(kindMark, "the comment prefix that marks the cluster's authorized SSH key line as this owner's", core),
		".bootwright-registry-":                   fixedBy(kindMark, "the anchor naming the registry credential this owner placed", core),
		"bootwright-managed-export":               fixedBy(kindMark, "the argument the published handoff ConfigMap names the export by", ceph.ConsumerImplementation),
		"bootwright-bound:":                       fixedBy(kindProof, "the placeholder a stored service spec holds where a bound secret goes", core),
		"bootwright-bound:/spec/ssl_cert":         fixedBy(kindProof, "the placeholder a stored gateway spec holds for its bound certificate", core),
		"bootwright-bound:/spec/ssl_key":          fixedBy(kindProof, "the placeholder a stored gateway spec holds for its bound key", core),
		"bootwright.storage-export.principal-v1":  fixedBy(kindPlace, "the domain the export's principal name is derived under", ceph.ConsumerImplementation),
		"/var/lib/bootwright/ceph":                fixedBy(kindPlace, "the root of the cluster's tunnel, custody and native records on each host, which the export, SMB and replication workers read and write under", core, ceph.ConsumerImplementation, ceph.SMBImplementation, ceph.ReplicationImplementation),
		"/var/lib/bootwright/ceph-host":           fixedBy(kindPlace, "the root of the host's runtime profile", host, core),
		"/var/lib/bootwright/ceph-packages":       fixedBy(kindPlace, "the root of the host's package records", host),
		"/run/bootwright-ceph-ssh":                fixedBy(kindPlace, "the tunnel's mount point inside the cluster's containers", core),
		".bootwright-cephadm-":                    fixedBy(kindPlace, "the prefix of the staged cephadm name the host cache record holds", core),
		"05-bootwright-ceph-":                     fixedBy(kindPlace, "the prefix of the sshd policy include the cluster owns", core),
		".bootwright-tree-identity":               fixedBy(kindProof, "the identity file a published package tree carries", tree, anaconda, host),
		"/.bootwright-tree-identity":              fixedBy(kindProof, "the identity file a published package tree carries", tree),
		"bootwright-media.json":                   fixedBy(kindProof, "the receipt the published boot media carries", media),
		"/bootwright-media.json":                  fixedBy(kindProof, "the receipt the published boot media carries", media),
		".bootwright-booted":                      fixedBy(kindProof, "the marker the install leaves once its nodes booted the media", media, install),
		"/.bootwright-booted":                     fixedBy(kindProof, "the marker the install leaves once its nodes booted the media", install),
		"bootwright/containercluster/identity/v2": fixedBy(kindProof, "the domain the installed cluster's identity is derived under", install),
		".bootwright-kubeconfig":                  fixedBy(kindPlace, "the kept copy of the installer's kubeconfig in the work root", install),
		"/.bootwright-kubeconfig":                 fixedBy(kindPlace, "the kept copy of the installer's kubeconfig in the work root", media, install),
		"/etc/bootwright":                         fixedBy(kindPlace, "the guest directory of the registration and update records", anaconda),
		"/etc/bootwright/registration.json":       fixedBy(kindProof, "the registration record the unregistration reads", anaconda),
		"/etc/bootwright/update.json":             fixedBy(kindProof, "the update record the update worker resumes from and an observation reads", anaconda),
		"/etc/rhsm/ca/bootwright-satellite.pem":   fixedBy(kindPlace, "the Satellite CA bundle the registration worker writes and rhsm.conf names", anaconda),
		".bootwright-satellite.pem.tmp":           fixedBy(kindPlace, "the stage the registration worker renames onto the Satellite CA bundle", anaconda),
		"/var/lib/bootwright-install":             fixedBy(kindPlace, "the parent of the installation's work area", anaconda),
		"/var/lib/bootwright-install/":            fixedBy(kindPlace, "the parent of the installation's work area", anaconda),
		"/run/bootwright":                         fixedBy(kindPlace, "the installed machine's network convergence runtime directory", anaconda),
		"bootwright-network-apply.service":        fixedBy(kindPlace, "the installed machine's network convergence unit", anaconda),
		"/var/lib/bootwright-substrate/":          fixedBy(kindPlace, "the parent of the Machine's management controller area", libvirt.MachineImplementation),
		"/var/lib/bootwright-erase":               fixedBy(kindPlace, "the controller root of the erase's per-operation area", erase),
		"bootwright.erase=1":                      fixedBy(kindProof, "the kernel argument that proves the booted environment is the erase environment", erase),

		"/var/lib/bootwright-services/":            validated("the content root every service request must lie beneath"),
		"/var/lib/bootwright-staging/runs":         validated("the private parent every runner-issued run scratch must lie directly beneath"),
		"/var/lib/bootwright-clusters/":            validated("the work root every cluster media and install request must lie beneath"),
		"/var/lib/libvirt/images/bootwright/":      validated("the media pool path every libvirt request must name"),
		"/etc/bootwright/install-marker.json":      validated("the marker path every installation request must name, and the guest agent may read"),
		"/etc/bootwright/host-key.pub":             validated("the host key path every installation request must name, and the guest agent may read"),
		"/etc/bootwright/network.yml":              validated("the network document path every installation request must name"),
		"/etc/bootwright/network-marker.json":      validated("the network marker path every installation request must name"),
		"bootwright-%s-%s-firewalld-ready.service": validated("the readiness unit name a libvirt host request must carry"),

		"/var/lib/bootwright/media":  {class: formatController, reason: "the controller's media store; a block reads media by the name its request carries"},
		"/var/lib/bootwright/media/": {class: formatController, reason: "the controller's media store; a block reads media by the name its request carries"},

		"bootwright":                                        transient("the user of a parser-only SSH option set the host probe never dials"),
		"bootwright_native_cephadm":                         inProcess,
		"bootwright_tunnel_route":                           inProcess,
		"<bootwright-fixed-":                                inProcess,
		"/bootwright/core/plugins/":                         inProcess,
		"bootwright_node_%03d":                              transient("the inventory alias the runner gives each node within one run"),
		".bootwright-":                                      scratch,
		".bootwright-firewall-":                             scratch,
		"bootwright-ceph-":                                  scratch,
		"bootwright-ceph-packages-":                         scratch,
		"bootwright-host-probe-":                            scratch,
		"bootwright-route-probe-":                           scratch,
		"bootwright-run-scratch-":                           transient("the per-run scratch directory the runner creates and removes"),
		"/run/bootwright-ceph-route":                        transient("the route probe's synthetic root, removed by the call that made it"),
		"bootwright-route":                                  transient("the route probe's synthetic account, removed by the call that made it"),
		"bootwright-probe":                                  transient("the host name of a probe container the same call removes"),
		"bootwright.ceph.host.probe=":                       transient("the label of a probe container the same call removes"),
		"/run/bootwright-label":                             transient("the label probe's mount inside its own container"),
		"/run/bootwright-label/proof":                       transient("the label probe's file inside its own container"),
		"bootwright-fips":                                   scriptName,
		"bootwright-network":                                scriptName,
		"bootwright-root-encryption":                        scriptName,
		"$6$bootwright$*":                                   eraseRuntime,
		"/run/bootwright-erase":                             eraseRuntime,
		"/run/bootwright-erase/authorized_keys":             eraseRuntime,
		"/run/bootwright-erase/host-key":                    eraseRuntime,
		"/run/bootwright-erase/runtime.py":                  eraseRuntime,
		"/run/bootwright-erase/sshd.pid":                    eraseRuntime,
		"/run/bootwright-erase/sshd_config":                 eraseRuntime,
		"/run/systemd/system/bootwright-erase-sshd.service": eraseRuntime,
		"/run/systemd/system/bootwright-erase.service":      eraseRuntime,
		"bootwright-erase-sshd.service":                     eraseRuntime,
		"bootwright-erase.service":                          eraseRuntime,

		"//github.com/crmarques/bootwright": {class: formatDocumentation, reason: "the collection's repository URL in galaxy.yml"},
		"bootwright-lab-rhel-01":            {class: formatDocumentation, reason: "a domain name in guest_agent_read's EXAMPLES"},
	}
	for literal, format := range automationCapitalized() {
		identities[literal] = format
	}
	return identities
}

// automationCapitalized classifies the capitalized Bootwright tokens. The
// readiness unit's header lines are a mark: the libvirt host proves the unit
// its own by comparing the whole unit's bytes.
func automationCapitalized() map[string]automationFormat {
	readiness := fixedBy(kindMark, "a header line of the firewall readiness unit, whose exact bytes prove the unit the libvirt host's own", libvirt.HostImplementation)
	prose := automationFormat{class: formatDocumentation, reason: "prose in a docstring, module documentation or a message"}
	description := automationFormat{class: formatDocumentation, reason: "the Description of a unit the role writes from its template, which no run reads back"}
	eraseRuntime := transient("the erase environment's RAM-only runtime, which exists only while that boot runs")
	kickstart := transient("a placeholder the role replaces in the kickstart it renders in the same run")
	return map[string]automationFormat{
		"Bootwright owned readiness barrier":                readiness,
		"Description=Bootwright managed network firewall":   readiness,
		"SSH-2.0-BootwrightRoute":                           transient("the banner the route probe's control channel sends and checks within one call"),
		"CN=Bootwright ephemeral pull broker":               transient("the subject of the pull broker's certificate, made and discarded within one run"),
		"Bootwright-Controller/1":                           transient("the controller download's User-Agent"),
		"_BOOTWRIGHT_SOURCE":                                transient("the attribute that carries a helper's source into the worker process within one call"),
		"._BOOTWRIGHT_SOURCE":                               transient("the attribute that carries a helper's source into the worker process within one call"),
		"@@BOOTWRIGHT_MARKER@@":                             kickstart,
		"@@BOOTWRIGHT_AUTHORIZED_KEY@@":                     kickstart,
		"@@BOOTWRIGHT_PRIVATE_URL@@":                        kickstart,
		"@@BOOTWRIGHT_ARTIFACT_CERTIFICATE@@":               kickstart,
		"BOOTWRIGHT_ERASE_DATA":                             eraseRuntime,
		"BOOTWRIGHT_ERASE_SSH_UNIT":                         eraseRuntime,
		"BOOTWRIGHT_ERASE_UNIT":                             eraseRuntime,
		"Description=Bootwright RAM receipt transport":      eraseRuntime,
		"Description=Bootwright authorized offline logical": eraseRuntime,
		"Description=Bootwright managed artifact server":    description,
		"Description=Bootwright managed name resolution":    description,
		"Description=Bootwright managed proxy":              description,
		"Description=Bootwright managed time service":       description,
		"Description=Bootwright management controller for":  description,
		"Bootwright contributors":                           {class: formatDocumentation, reason: "the author line of galaxy.yml and of each module's documentation"},
		"Bootwright capability adapters":                    {class: formatDocumentation, reason: "the collection description in galaxy.yml"},
		"Bootwright cannot read":                            prose,
		"Bootwright coordinates":                            prose,
		"Bootwright does not realize":                       prose,
		"Bootwright installation publishes":                 prose,
		"Bootwright libvirt provider host":                  prose,
		"Bootwright managed artifact server":                prose,
		"Bootwright managed network service":                prose,
		"Bootwright never changes a":                        prose,
		"Bootwright never pins that":                        prose,
		"Bootwright process":                                prose,
		"Bootwright process reads":                          prose,
		"Bootwright retains it":                             prose,
		"Bootwright sets a default":                         prose,
		"Bootwright virtual machine and":                    prose,
		"Bootwright-managed operating system and":           prose,
	}
}

// formatSelfHealing is a store format Go code writes and reads itself: another
// build's copy is discarded by setup after confirmation and refuses a launch
// naming setup, so it versions no implementation's contract (D152.9).
// formatRuntimeIdentity is a digest domain the setup receipt records: a moved
// one names another runtime, which setup prepares beside the old (D152.7).
const (
	formatSelfHealing     = "self-healing"
	formatRuntimeIdentity = "runtime-identity"
)

// storeFormats classifies the versioned formats the store persists outside
// any implementation's automation, by how another build's copy is met.
func storeFormats() map[string]string {
	return map[string]string{
		prerequisites.SetupStateFormat:     formatSelfHealing,
		runtimelock.Format:                 formatRuntimeIdentity,
		prerequisites.RuntimeFormat:        formatRuntimeIdentity,
		prerequisites.RuntimeClosureFormat: formatRuntimeIdentity,
	}
}

// TestEveryStoreFormatIsClassified holds each store format to a versioned
// name no automation literal also claims, so a moved setup format is a new,
// classified literal rather than a silent reuse.
func TestEveryStoreFormatIsClassified(t *testing.T) {
	versioned := regexp.MustCompile(`-v[0-9]+$`)
	automation := automationFormats()
	for literal, class := range storeFormats() {
		if !versioned.MatchString(literal) {
			t.Errorf("store format %q does not end in a version", literal)
		}
		if class != formatSelfHealing && class != formatRuntimeIdentity {
			t.Errorf("store format %q has class %q, want %q or %q", literal, class, formatSelfHealing, formatRuntimeIdentity)
		}
		if _, claimed := automation[literal]; claimed {
			t.Errorf("store format %q is also classified as an automation literal", literal)
		}
	}
	for literal, want := range map[string]string{
		prerequisites.SetupStateFormat:     "bootwright.setup-state-v1",
		runtimelock.Format:                 "bootwright.controller.runtime-lock-v1",
		prerequisites.RuntimeFormat:        "bootwright.controller.runtime-v1",
		prerequisites.RuntimeClosureFormat: "bootwright.controller.execution-closure-v2",
	} {
		if literal != want {
			t.Errorf("the store format %q moved to %q: classify the new literal", want, literal)
		}
	}
}

const automationCollection = "collections/ansible_collections/bootwright/core/"

var (
	automationTexts  = []string{".py", ".yml", ".yaml", ".j2"}
	versionedLiteral = regexp.MustCompile(`"([^"\\\n]*-v[0-9]+)(?:\\[nrt0])?"|'([^'\\\n]*-v[0-9]+)(?:\\[nrt0])?'`)
	// identityLiteral is a token, quoted or not, naming a bootwright.io URI or
	// key, a host path with bootwright in it, a name, prefix or mark beginning
	// with bootwright and a separator, or a quoted bootwright name.
	identityLiteral = regexp.MustCompile(`(?:^|[^A-Za-z0-9_.%{/$<-])(https://bootwright\.io/[^\s"'\\]*|/[A-Za-z0-9._/%-]*bootwright[A-Za-z0-9._/%-]*|[A-Za-z0-9_%$<-]*\.?bootwright[-.:=$/][A-Za-z0-9._/%:=$*-]*)|["'](bootwright(?:_[A-Za-z0-9_%]*)?)["']`)
	// capitalLiteral is a capitalized Bootwright or BOOTWRIGHT token, with the
	// characters joined to it and up to three following words, such as a unit
	// header, a banner, a certificate subject, a heredoc delimiter or prose.
	capitalLiteral = regexp.MustCompile(`[A-Za-z0-9_.@=-]*(?:Bootwright|BOOTWRIGHT)[A-Za-z0-9_./:=@-]*(?: [A-Za-z][A-Za-z0-9-]*){0,3}`)
	// unescaped turns a string escape into the separator it writes, so the
	// escape's letter never joins the capitalized token after it.
	unescaped = strings.NewReplacer(`\n`, " ", `\r`, " ", `\t`, " ")
)

// identityLiterals is every token of one file identityLiteral finds outside a
// comment line, except the collection's own module and package names.
func identityLiterals(text string) []string {
	var literals []string
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		for _, match := range identityLiteral.FindAllStringSubmatch(line, -1) {
			literal := match[1] + match[2]
			if strings.HasPrefix(literal, "bootwright.core") || strings.Contains(literal, "ansible_collections.bootwright") {
				continue
			}
			literals = append(literals, literal)
		}
		literals = append(literals, capitalLiteral.FindAllString(unescaped.Replace(line), -1)...)
	}
	return literals
}

// automationLiterals is every quoted versioned literal, and every token
// naming a Bootwright host path, name prefix or mark, that the collection's
// embedded plugins, roles and playbooks carry, with the files that carry it,
// and the collection's automation files whose extension it does not read.
func automationLiterals() (map[string][]string, []string) {
	found := map[string][]string{}
	var unread []string
	assets := ansible.Automation()
	for _, name := range slices.Sorted(maps.Keys(assets)) {
		if !strings.HasPrefix(name, automationCollection) {
			continue
		}
		if !slices.Contains(automationTexts, filepath.Ext(name)) {
			unread = append(unread, name)
			continue
		}
		var literals []string
		for _, match := range versionedLiteral.FindAllStringSubmatch(string(assets[name]), -1) {
			literals = append(literals, match[1]+match[2])
		}
		literals = append(literals, identityLiterals(string(assets[name]))...)
		for _, literal := range literals {
			if !slices.Contains(found[literal], name) {
				found[literal] = append(found[literal], name)
			}
		}
	}
	return found, unread
}

// TestEveryAutomationVersionLiteralIsClassified holds the classification of
// the quoted literals ending in a version, of the tokens naming a
// bootwright.io URI or key or a Bootwright host path, name prefix or mark, and
// of the capitalized Bootwright tokens, that the collection's automation files
// carry (.py, .yml, .yaml and .j2; any other extension fails it) exact, so a
// new or renamed format, place or mark cannot
// reach a host without saying whether it is part of an implementation's
// contract. It is also the guard that removal automation reads a target's
// identity only from the frozen request: every value it reads from the build
// in hand instead is a fixed literal naming its implementations, which their
// contract goldens list. A dynamically built value, or one naming no
// Bootwright token, is outside what it sees.
func TestEveryAutomationVersionLiteralIsClassified(t *testing.T) {
	found, unread := automationLiterals()
	formats := automationFormats()
	if len(found) == 0 {
		t.Fatal("the embedded collection carries no versioned literal: the scan is broken")
	}
	for _, name := range unread {
		t.Errorf("%s is embedded automation the scan does not read: read its extension in automationTexts, or make it collection documentation", name)
	}
	for _, sentinel := range []string{"ceph-tunnel-v3", "/var/lib/bootwright/ceph", "bootwright.io/owner", "/var/lib/bootwright-install", "bootwright-network-apply.service"} {
		if _, ok := found[sentinel]; !ok {
			t.Errorf("the scan no longer finds %q: the scan is broken, or the literal moved and another sentinel must replace it", sentinel)
		}
	}
	implementations := map[string]bool{}
	for _, version := range contractVersions() {
		implementations[version.implementation] = true
	}
	for _, literal := range slices.Sorted(maps.Keys(found)) {
		if _, ok := formats[literal]; !ok {
			t.Errorf("%q in %s is not classified: classify it in automationFormats or automationIdentities", literal, strings.Join(found[literal], ", "))
		}
	}
	for _, literal := range slices.Sorted(maps.Keys(formats)) {
		format := formats[literal]
		if _, ok := found[literal]; !ok {
			t.Errorf("%q is classified but no embedded plugin or role carries it: stale classification, remove it", literal)
		}
		named := format.class == formatPersisted || format.class == formatFixed
		if named != (len(format.implementations) != 0) {
			t.Errorf("%q: only a persisted or fixed literal names implementations, and each names one", literal)
		}
		kinds := []string{kindMark, kindProof, kindPlace}
		switch format.class {
		case formatPersisted:
			kinds = append(kinds, "")
		case formatFixed:
		default:
			kinds = []string{""}
		}
		if !slices.Contains(kinds, format.kind) {
			t.Errorf("%q: a fixed literal is a mark, a proof or a place, and only a persisted one may also be one", literal)
		}
		if (format.reason == "") == slices.Contains([]string{formatTransient, formatController, formatFixed, formatValidated, formatDocumentation}, format.class) {
			t.Errorf("%q: a %s literal records its reason, and only it", literal, format.class)
		}
		for _, implementation := range format.implementations {
			if !implementations[implementation] {
				t.Errorf("%q names %s, which no contract version names", literal, implementation)
			}
		}
	}
}

// TestEveryImplementationMovesItsContentDigestWithItsContract holds each
// implementation's contract to its content digest: its Go request and
// evidence schemas, the formats its automation persists on a host, the marks,
// proof identifiers and places its automation fixes outside the frozen
// request, the effect pins its digest hashes and the request members that
// carry its effects' identity. The digest is what a continuation under another
// build compares, so a contract that changes under the same digest would let
// another build continue work half done under the old contract. Such a change
// fails and -update refuses it: move the implementation's version. A moved
// digest is rewritten by -update, except that a changed place also needs the
// request version to move, because a removal compares no digest: only a
// request this build cannot read keeps a removal from looking for the old
// blocks' effects in the new place. A raw-JSON, interface or map[string]any
// member is fingerprinted as an opaque leaf, so the schema nested in one, such
// as the raw cluster services the Ceph core and host requests carry or the
// agent media's install and agent configurations, is the author's to move;
// re-planning still compares its bytes.
func TestEveryImplementationMovesItsContentDigestWithItsContract(t *testing.T) {
	versions := contractVersions()
	contractOfferedImplementations(t, versions)
	recipes, identities := contractRecipes(), requestIdentities()
	sections, formats := map[string]map[string][]string{}, automationFormats()
	literals, _ := automationLiterals()
	for literal := range literals {
		format := formats[literal]
		section := map[string]string{kindMark: "mark-formats", kindProof: "proof-ids", kindPlace: "fixed-places"}[format.kind]
		for _, implementation := range format.implementations {
			if sections[implementation] == nil {
				sections[implementation] = map[string][]string{}
			}
			if format.class == formatPersisted {
				sections[implementation]["automation-formats"] = append(sections[implementation]["automation-formats"], literal)
			}
			if section != "" {
				sections[implementation][section] = append(sections[implementation][section], literal)
			}
		}
	}
	for implementation, proofs := range cephMarkerProofs() {
		if sections[implementation] == nil {
			sections[implementation] = map[string][]string{}
		}
		sections[implementation]["proof-ids"] = append(sections[implementation]["proof-ids"], proofs...)
	}
	directory := filepath.Join("testdata", "contracts")
	wanted := map[string]bool{}
	for _, version := range versions {
		name := version.implementation + ".golden"
		wanted[name] = true
		recipe, ok := recipes[version.implementation]
		if !ok || recipe.digest() != version.digest {
			t.Errorf("%s: its digest recipe does not reproduce its content digest: restate in contractRecipes exactly what its ContentDigest hashes", version.implementation)
			continue
		}
		if !contractIdentitiesInSchema(t, version, identities[version.implementation]) {
			continue
		}
		listed := sections[version.implementation]
		if listed == nil {
			listed = map[string][]string{}
		}
		listed["effect-pins"] = recipe.pins()
		listed["request-identity"] = identities[version.implementation]
		text := contractText(version, recipe.requestVersionOf(), listed)
		contractHoldGolden(t, version, filepath.Join(directory, name), text)
	}
	entries, err := os.ReadDir(directory)
	if err != nil && !*update {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !wanted[entry.Name()] {
			t.Errorf("%s names no implementation a contract version names: remove it", filepath.Join(directory, entry.Name()))
		}
	}
}

// contractIdentitiesInSchema proves an implementation lists the request
// members that carry its effects' identity, and that each is one.
func contractIdentitiesInSchema(t *testing.T, version contractVersion, identities []string) bool {
	t.Helper()
	if len(identities) == 0 {
		t.Errorf("%s lists no request member carrying its effects' identity: add it to requestIdentities", version.implementation)
		return false
	}
	members := map[string]bool{}
	for _, line := range schemaFingerprint(version.request) {
		member, _, _ := strings.Cut(line, " ")
		members[member] = true
	}
	ok := true
	for _, identity := range identities {
		if !members[identity] {
			t.Errorf("%s names %s as a request identity member, which its request schema lacks", version.implementation, identity)
			ok = false
		}
	}
	return ok
}

// contractOfferedImplementations proves the versions name exactly the
// implementations the composition root offers, and each its digest as the
// real capability plans it on the examples the capability contract plans.
func contractOfferedImplementations(t *testing.T, versions []contractVersion) {
	t.Helper()
	resolver := buildCapabilitiesWith(systemClock{}, exampleControllerPorts(t), &contractRunner{}, exampleMediaRecords{})
	offered, listed := map[string]bool{}, map[string]string{}
	for _, binding := range resolver.Bindings() {
		offered[binding.Implementation] = true
	}
	for _, version := range versions {
		if _, twice := listed[version.implementation]; twice {
			t.Errorf("%s is listed twice", version.implementation)
		}
		listed[version.implementation] = version.digest
		if !offered[version.implementation] {
			t.Errorf("%s is listed but the composition root offers no such implementation", version.implementation)
		}
	}
	for implementation := range offered {
		if _, ok := listed[implementation]; !ok {
			t.Errorf("%s is offered but has no contract version: add it to contractVersions", implementation)
		}
	}
	planned := map[string]bool{}
	examples := map[string]bool{}
	for _, row := range contractRows() {
		if examples[row.example] {
			continue
		}
		examples[row.example] = true
		for _, block := range contractExampleOf(t, resolver, row.example).plan.Blocks {
			planned[block.Implementation] = true
			if digest, ok := listed[block.Implementation]; ok && block.ContentDigest != digest {
				t.Errorf("%s plans %s under content digest %s, not the %s its contract version names", block.Implementation, block.ID, block.ContentDigest, digest)
			}
		}
	}
	for implementation := range listed {
		if !planned[implementation] {
			t.Errorf("no example plans %s, so its contract digest is not held to what it plans", implementation)
		}
	}
}

// contractSections is the order of a contract golden's sections. A section a
// recorded golden lacks, or one it carries beyond them, is a changed section.
var contractSections = []string{"implementation", "content-digest", "request-version", "automation-formats", "mark-formats", "proof-ids",
	"fixed-places", "effect-pins", "request-identity", "request-schema", "evidence-schema"}

// contractListSections are the sections that list values one per line.
var contractListSections = []string{"effect-pins", "request-identity", "request-schema", "evidence-schema"}

func contractText(version contractVersion, requestVersion string, listed map[string][]string) string {
	var text strings.Builder
	fmt.Fprintf(&text, "implementation: %s\ncontent-digest: %s\nrequest-version: %s\n", version.implementation, version.digest, requestVersion)
	for _, section := range []string{"automation-formats", "mark-formats", "proof-ids", "fixed-places"} {
		values := "none"
		if len(listed[section]) != 0 {
			values = strings.Join(slices.Sorted(slices.Values(listed[section])), ", ")
		}
		fmt.Fprintf(&text, "%s: %s\n", section, values)
	}
	listed["request-schema"], listed["evidence-schema"] = schemaFingerprint(version.request), schemaFingerprint(version.evidence)
	for _, section := range contractListSections {
		text.WriteString(section + ":\n")
		lines := listed[section]
		if len(lines) == 0 {
			lines = []string{"none"}
		}
		for _, line := range lines {
			text.WriteString("  " + line + "\n")
		}
	}
	return text.String()
}

// contractSectionsOf splits a contract golden into its sections by name.
func contractSectionsOf(text string) map[string]string {
	sections := map[string]string{}
	current := ""
	for line := range strings.SplitSeq(strings.TrimSuffix(text, "\n"), "\n") {
		if !strings.HasPrefix(line, "  ") {
			name, value, _ := strings.Cut(line, ":")
			current = name
			sections[current] = strings.TrimSpace(value)
			continue
		}
		sections[current] += "\n" + line
	}
	return sections
}

func contractHoldGolden(t *testing.T, version contractVersion, path, text string) {
	t.Helper()
	golden, err := os.ReadFile(path)
	if err != nil {
		if !*update {
			t.Errorf("%s: %v; run with -update to write it", version.implementation, err)
			return
		}
		contractWriteGolden(t, path, text)
		return
	}
	if string(golden) == text {
		return
	}
	recorded, current := contractSectionsOf(string(golden)), contractSectionsOf(text)
	var changed []string
	for _, section := range contractSections {
		if value, ok := recorded[section]; !ok || value != current[section] {
			changed = append(changed, section)
		}
	}
	for section := range recorded {
		if !slices.Contains(contractSections, section) {
			changed = append(changed, section)
		}
	}
	_, digested := recorded["content-digest"]
	if (!digested || recorded["content-digest"] == current["content-digest"]) && len(changed) != 0 {
		t.Errorf("%s: its %s changed while its content digest did not, so another build would continue its blocks under the old contract: move %s's version, which moves its content digest (-update refuses to record this):\n%s",
			version.implementation, strings.Join(changed, " and "), version.implementation, lineDiff(string(golden), text))
		return
	}
	if requestVersion, ok := recorded["request-version"]; recorded["fixed-places"] != current["fixed-places"] && (!ok || requestVersion == current["request-version"]) {
		t.Errorf("%s: its fixed places changed while its request version did not, so a removal under this build would look for the old blocks' effects in the new place: move its request version (-update refuses to record this):\n%s",
			version.implementation, lineDiff(string(golden), text))
		return
	}
	if !*update {
		t.Errorf("%s: its content digest moved; rerun with -update to record its contract:\n%s", version.implementation, lineDiff(string(golden), text))
		return
	}
	contractWriteGolden(t, path, text)
}

func contractWriteGolden(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

var (
	schemaRawMessage    = reflect.TypeFor[json.RawMessage]()
	schemaJSONMarshaler = reflect.TypeFor[json.Marshaler]()
	schemaTextMarshaler = reflect.TypeFor[encoding.TextMarshaler]()
)

// schemaFingerprint is one line per field encoding/json writes for a value
// of t, in declared order: its path, kind and tag options. A nested struct
// names its Go type, so a type swap shows; raw JSON, interfaces and
// map[string]any are opaque leaves whose nested schema it never sees.
func schemaFingerprint(t reflect.Type) []string {
	if t == nil {
		return nil
	}
	walker := schemaWalker{visiting: map[reflect.Type]bool{}}
	walker.fields("", t)
	return walker.lines
}

type schemaWalker struct {
	lines    []string
	visiting map[reflect.Type]bool
}

func (w *schemaWalker) fields(path string, t reflect.Type) {
	if w.visiting[t] {
		w.lines = append(w.lines, path+" cycle "+t.String())
		return
	}
	w.visiting[t] = true
	defer delete(w.visiting, t)
	for index := range t.NumField() {
		field := t.Field(index)
		tag := field.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, options, _ := strings.Cut(tag, ",")
		inner := field.Type
		if inner.Kind() == reflect.Pointer {
			inner = inner.Elem()
		}
		if field.Anonymous && name == "" && inner.Kind() == reflect.Struct {
			w.fields(path, inner)
			continue
		}
		if !field.IsExported() {
			continue
		}
		if name == "" {
			name = field.Name
		}
		line := path + name + " " + w.kind(field.Type)
		if options != "" {
			line += " " + strings.ReplaceAll(options, ",", " ")
		}
		w.lines = append(w.lines, line)
		w.nested(path+name, field.Type)
	}
}

// kind names how encoding/json writes a value of t.
func (w *schemaWalker) kind(t reflect.Type) string {
	switch {
	case t == schemaRawMessage || t.Kind() == reflect.Interface:
		return "opaque"
	case t.Kind() == reflect.Map && t.Key().Kind() == reflect.String && t.Elem().Kind() == reflect.Interface:
		return "opaque"
	case t.Implements(schemaJSONMarshaler) || reflect.PointerTo(t).Implements(schemaJSONMarshaler):
		return "marshaler " + t.String()
	case t.Implements(schemaTextMarshaler) || reflect.PointerTo(t).Implements(schemaTextMarshaler):
		return "text " + t.String()
	}
	switch t.Kind() {
	case reflect.Pointer:
		return "*" + w.kind(t.Elem())
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 && t.Kind() == reflect.Slice {
			return "bytes"
		}
		return "[]" + w.kind(t.Elem())
	case reflect.Map:
		return "map[" + t.Key().Kind().String() + "]" + w.kind(t.Elem())
	case reflect.Struct:
		return "struct " + t.String()
	}
	return t.Kind().String()
}

// nested walks the fields of every struct a field of t holds, under the path
// its encoding nests them at.
func (w *schemaWalker) nested(path string, t reflect.Type) {
	if kind := w.kind(t); kind == "opaque" || strings.HasPrefix(kind, "marshaler ") || strings.HasPrefix(kind, "text ") {
		return
	}
	switch t.Kind() {
	case reflect.Pointer:
		w.nested(path, t.Elem())
	case reflect.Slice, reflect.Array:
		w.nested(path+"[]", t.Elem())
	case reflect.Map:
		w.nested(path+"{}", t.Elem())
	case reflect.Struct:
		w.fields(path+".", t)
	}
}
