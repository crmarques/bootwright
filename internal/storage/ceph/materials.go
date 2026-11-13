package ceph

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const knownHostsMaterial = "ceph.internal/known-hosts"
const registryMaterial = "ceph.internal/registry"

func materialPart(material map[string]secrets.Material, name string, part secrets.Part) ([]byte, error) {
	value, ok := material[name].Part(part)
	if !ok || len(value) == 0 {
		clear(value)
		return nil, failure("lifecycle.state", "a required Ceph credential component is not bound", "")
	}
	return value, nil
}

func registryTrustFile(e lifecycle.Execution, ref string) ([]lifecycle.MaterialFile, error) {
	return trustFile(e, ref, "registry-ca", "registryCA")
}

func packageRepositoryTrustFile(e lifecycle.Execution, ref string) ([]lifecycle.MaterialFile, error) {
	return trustFile(e, ref, "package-repository-ca", "packageRepositoryCA")
}

func trustFile(e lifecycle.Execution, ref, name, variable string) ([]lifecycle.MaterialFile, error) {
	if ref == "" {
		return nil, nil
	}
	value, err := materialPart(e.Material, ref, secrets.CertificatePart)
	if err != nil {
		return nil, err
	}
	clear(value)
	return []lifecycle.MaterialFile{{Name: name, Secret: ref, Part: secrets.CertificatePart, Variable: variable}}, nil
}

// boundMaterial builds only protected, per-run files. Registry passwords and
// private keys never become canonical requests, command arguments or evidence.
func boundMaterial(e lifecycle.Execution, r Request, operation string, readers DependencyProofs) (lifecycle.Execution, []lifecycle.MaterialFile, func(), error) {
	cleanup := func() {}
	if len(r.Hosts) == 0 {
		return e, nil, cleanup, failure("lifecycle.state", "Ceph has no frozen hosts", "")
	}
	if operation == "apply" {
		if err := proveFIPS(e, r, readers); err != nil {
			return e, nil, cleanup, err
		}
		if err := proveClusterKey(e, r.Security, r.ClusterKeyRef); err != nil {
			return e, nil, cleanup, err
		}
	}
	bound := maps.Clone(e.Material)
	if bound == nil {
		bound = map[string]secrets.Material{}
	}
	known := strings.Builder{}
	for _, h := range r.Hosts {
		key, err := fleetHostKey(h, r.Security, e, readers)
		if err != nil {
			return e, nil, cleanup, err
		}
		known.WriteString(key.Line(h.Address, h.Port))
		known.WriteByte('\n')
	}
	bound[knownHostsMaterial] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte(known.String())})
	cleanup = func() { bound[knownHostsMaterial].Clear(); bound[registryMaterial].Clear() }
	files := []lifecycle.MaterialFile{{Name: "fleet-key", Secret: r.Hosts[0].SSHKeyRef, Part: secrets.PrivateKeyPart, Variable: "sshKey"}, {Name: "known-hosts", Secret: knownHostsMaterial, Part: secrets.ValuePart, Variable: "knownHosts"}}
	if operation == "apply" {
		user, err := materialPart(e.Material, r.RegistryRef, secrets.UsernamePart)
		if err != nil {
			cleanup()
			return e, nil, func() {}, err
		}
		defer clear(user)
		password, err := materialPart(e.Material, r.RegistryRef, secrets.PasswordPart)
		if err != nil {
			cleanup()
			return e, nil, func() {}, err
		}
		defer clear(password)
		registry, err := json.Marshal(map[string]string{"url": r.Registry, "username": string(user), "password": string(password)})
		if err != nil {
			cleanup()
			return e, nil, func() {}, err
		}
		bound[registryMaterial] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: registry})
		clear(registry)
		files = append(files, lifecycle.MaterialFile{Name: "cluster-private", Secret: r.ClusterKeyRef, Part: secrets.PrivateKeyPart, Variable: "clusterPrivateKey"}, lifecycle.MaterialFile{Name: "cluster-public", Secret: r.ClusterKeyRef, Part: secrets.PublicKeyPart, Variable: "clusterPublicKey"}, lifecycle.MaterialFile{Name: "registry", Secret: registryMaterial, Part: secrets.ValuePart, Variable: "registryJSON"})
	}
	gateway, err := gatewayMaterial(e, r, operation)
	if err != nil {
		cleanup()
		return e, nil, func() {}, err
	}
	unit, err := unitMaterial(e, r, operation)
	if err != nil {
		cleanup()
		return e, nil, func() {}, err
	}
	e.Material = bound
	return e, append(append(files, gateway...), unit...), cleanup, nil
}

func unitMaterial(e lifecycle.Execution, r Request, operation string) ([]lifecycle.MaterialFile, error) {
	if r.Action != "service" || r.Service == nil || len(r.Service.Bindings) == 0 {
		return nil, nil
	}
	types := map[string]string{}
	for _, document := range unitDocuments(r) {
		types[documentName(document)] = document.ServiceType
	}
	files := []lifecycle.MaterialFile{}
	for _, b := range r.Service.Bindings {
		leaf, known := unitLeaf(types[b.ServiceRef], b.Path)
		if !known {
			return nil, failure("lifecycle.state", "a Ceph service unit binds a credential leaf this build does not realize", "")
		}
		if operation != "apply" && !leaf.Observed {
			continue
		}
		value, err := materialPart(e.Material, b.SecretRef, leaf.Part)
		if err != nil {
			return nil, err
		}
		clear(value)
		files = append(files, lifecycle.MaterialFile{Name: leaf.File, Secret: b.SecretRef, Part: leaf.Part, Variable: leaf.Variable})
	}
	return files, nil
}

func gatewayMaterial(e lifecycle.Execution, r Request, operation string) ([]lifecycle.MaterialFile, error) {
	secret, key := "", operation == "apply"
	if r.Action == "gateway" {
		secret = gatewayTLSSecret(r.Gateway)
	}
	if r.Action == "object-bucket" && r.ObjectBucket != nil {
		secret, key = r.ObjectBucket.CertificateRef, false
	}
	if secret == "" {
		return nil, nil
	}
	files := []lifecycle.MaterialFile{{Name: "gateway-certificate", Secret: secret, Part: secrets.CertificatePart, Variable: "gatewayCertificate"}}
	if key {
		files = append(files, lifecycle.MaterialFile{Name: "gateway-key", Secret: secret, Part: secrets.PrivateKeyPart, Variable: "gatewayKey"})
	}
	for _, file := range files {
		value, err := materialPart(e.Material, secret, file.Part)
		if err != nil {
			return nil, err
		}
		clear(value)
	}
	return files, nil
}

const UnrecordedReading = "unrecorded"

func (c Capability) run(ctx context.Context, e lifecycle.Execution, operation string, r Request) (lifecycle.RunResult, error) {
	if operation == "observe" && r.Action == "cluster" && !lifecycle.Recorded(e) {
		return c.invoke(ctx, e, operation, r, map[string]string{"reading": UnrecordedReading}, nil)
	}
	return c.invoke(ctx, e, operation, r, nil, nil)
}

func (c Capability) invoke(ctx context.Context, e lifecycle.Execution, operation string, r Request, values map[string]string, named map[string]error) (lifecycle.RunResult, error) {
	if c.runner == nil {
		return lifecycle.RunResult{}, failure("lifecycle.state", "the Ceph adapter is not configured", "")
	}
	if r.Controller == "" {
		return lifecycle.RunResult{}, failure("lifecycle.state", "Ceph requires its frozen controller identity", "")
	}
	canonical, err := r.Canonical()
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	deadline, err := cephDeadline(r, operation)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	execution, files, cleanup, err := boundMaterial(e, r, operation, c.proofs)
	if err != nil {
		return lifecycle.RunResult{}, err
	}
	defer cleanup()
	outputs := []lifecycle.OutputFile{}
	produces := (operation == "apply" || operation == "observe" && lifecycle.Recorded(e)) && values == nil
	if produces && r.Action == "client" {
		outputs = append(outputs, lifecycle.OutputFile{Name: "client", Variable: "client"})
	}
	if produces && r.Action == "object-user" {
		outputs = append(outputs, lifecycle.OutputFile{Name: ObjectUserOutput, Variable: ObjectUserOutput})
	}
	if produces && r.Action == "cluster" {
		outputs = append(outputs, lifecycle.OutputFile{Name: DashboardOutput, Variable: DashboardOutput})
	}
	refusals := adapterRefusals()
	if r.Client != nil {
		refusals["foreign-client"] = foreignClientRefusal(r.Client.Name)
	}
	maps.Copy(refusals, named)
	return c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{Implementation: Implementation, Operation: operation, Variable: "bootwright_ceph", Canonical: canonical, Placement: machine.Placement{Connection: machine.ConnectionLocal, Machine: r.Controller}, Materials: files, Values: values, Outputs: outputs, Refusals: refusals, NativeErrors: true, Deadline: deadline}))
}

const (
	nativeCall    = 660
	bootstrapCall = 1860
	readinessWait = 900
)

func cephDeadline(r Request, operation string) (time.Duration, error) {
	if r.Action == "service" {
		return unitDeadline(r, operation)
	}
	if r.Action == "gateway" {
		return gatewayDeadline(operation), nil
	}
	if r.Action == "nvmeof" {
		return nvmeofDeadline(r, operation)
	}
	if r.Action == "nfs-export" {
		return nfsExportDeadline(operation), nil
	}
	if r.Action == "object-user" {
		return objectUserDeadline(operation), nil
	}
	if r.Action == "object-bucket" {
		return objectBucketDeadline(operation), nil
	}
	if len(r.Hosts) > hostBound(r.Stretch != nil) {
		return 0, clusterBudgetFailure(r.Cluster, len(r.Hosts), r.Stretch != nil)
	}
	nativeCalls, waits := len(r.Hosts)+2, 0
	if operation == "observe" && r.Action == "cluster" {
		nativeCalls++
	}
	if operation == "apply" {
		nativeCalls = 2*len(r.Hosts) + 3
		if r.Action == "cluster" {
			return applyDeadline(len(r.Hosts), r.Stretch != nil), nil
		}
		if r.Action == "filesystem" && r.Filesystem != nil {
			nativeCalls, waits = 2*len(r.Hosts)+3+len(r.Filesystem.DataPools)+len(r.Filesystem.Groups), readinessWait
			if len(r.Filesystem.Groups) > 0 {
				waits += readinessWait
			}
		}
	}
	deadline := time.Duration(nativeCalls*nativeCall+waits+60) * time.Second
	if deadline > lifecycle.MaxDeadline && r.Action == "filesystem" && r.Filesystem != nil {
		return 0, failure("lifecycle.unsupported", fmt.Sprintf("CephFilesystem %s needs a run deadline of %s, past the %s every adapter run is held to: two native calls for each of the %d Ceph hosts of CephCluster %s and one for each of its %d data pools and %d subvolume groups", r.Filesystem.Name, deadline, lifecycle.MaxDeadline, len(r.Hosts), r.Cluster, len(r.Filesystem.DataPools), len(r.Filesystem.Groups)),
			fmt.Sprintf("declare fewer spec.dataPoolRefs or spec.subvolumeGroups on CephFilesystem %s, or fewer entries in spec.topology.nodes of CephCluster %s", r.Filesystem.Name, r.Cluster))
	}
	if deadline > lifecycle.MaxDeadline {
		return 0, clusterBudgetFailure(r.Cluster, len(r.Hosts), r.Stretch != nil)
	}
	return deadline, nil
}

func clusterApplyDeadline(hosts int) time.Duration {
	return time.Duration((16+hosts)*nativeCall+bootstrapCall+2*readinessWait+60) * time.Second
}

func applyDeadline(hosts int, stretch bool) time.Duration {
	if stretch {
		return clusterApplyDeadline(hosts) + time.Duration(2*nativeCall+readinessWait)*time.Second
	}
	return clusterApplyDeadline(hosts)
}

func largestCluster(stretch bool) int {
	hosts := 0
	for applyDeadline(hosts+1, stretch) <= lifecycle.MaxDeadline {
		hosts++
	}
	return hosts
}

var maxClusterHosts, maxStretchHosts = largestCluster(false), largestCluster(true)

func hostBound(stretch bool) int {
	if stretch {
		return maxStretchHosts
	}
	return maxClusterHosts
}

func clusterBudgetFailure(cluster string, hosts int, stretch bool) error {
	reason, remedy := clusterHostBudget(cluster, hosts, stretch)
	return failure("lifecycle.unsupported", reason, remedy)
}

const (
	hostPackagesCall     = 1980
	hostImagesBudget     = 4285
	hostCallSlack        = 60
	hostLaterCalls       = 4
	hostRoleStepOverhead = 10
)

func maxHostRoleSteps() int {
	return 2 + (len(AuxiliaryNames)+PullConcurrency-1)/PullConcurrency + hostLaterCalls
}

func hostRoleMargin() int {
	return maxHostRoleSteps() * hostRoleStepOverhead
}

func hostDeadline(operation string) time.Duration {
	switch operation {
	case "apply":
		return time.Duration(hostPackagesCall+hostImagesBudget+hostCallSlack+hostLaterCalls*nativeCall+hostRoleMargin()) * time.Second
	case "observe", "destroy":
		return time.Duration(660+60) * time.Second
	}
	return 2 * time.Minute
}

func sshPolicyRefusals() map[string]error {
	return map[string]error{
		"ssh-policy-host-excluded": failure("lifecycle.unsupported", "a Ceph host's pinned SSH host key is of a type this controller's SSH crypto policy excludes, so Bootwright opened no session to it", "the adapter detail names the host by its Ceph hostname and its key type; run Bootwright from a controller whose SSH crypto policy admits that type, then apply again; or take back what this context owns with bootwright destroy --context <context>, change spec.customizations.security.fips on that host's MachineInstallProfile so its fresh installation generates a host key of an admitted type (ssh-ed25519 with fips off, ecdsa-sha2-nistp256 with fips on) with bootwright context update, then apply; Bootwright never pins an algorithm the policy excludes"),
		"ssh-policy-unavailable":   failure("lifecycle.unsupported", "this controller's SSH crypto policy could not be read or evaluated, so Bootwright opened no Ceph session", "check that /etc/crypto-policies/back-ends/openssh.config, when present, is a regular file of at most 64 KiB holding only crypto-policies algorithm directives, and that /usr/bin/ssh -G -F <that file> localhost succeeds, then apply again"),
	}
}

const osdDiskRemedy = "preserve any disk that may hold this cluster's data, DB or WAL; an unproved allocation must not be cleared to force continuation; the adapter detail names the code site that refused, not the disk; run wipefs --no-act <disk> on each declared OSD disk of the Ceph hosts to see what it holds; " +
	"if it is a disk of a Ceph cluster you destroyed or abandoned, declare it in its Machine's spec.hardware.clearOnInstall and install that Machine again, " +
	"or clear it yourself on the host after confirming it is that disk, then apply again; Bootwright never clears a disk it cannot prove its own"

func osdDeviceRefusals() map[string]error {
	return map[string]error{
		"device-not-empty": failure("lifecycle.unsupported", "an OSD disk on a Ceph host holds a partition table, filesystem, LVM or other signature whose owned allocation this operation cannot prove", osdDiskRemedy),
		"foreign-osd": failure("lifecycle.unsupported", "an OSD disk on a Ceph host, or the Ceph cluster itself, holds a Ceph OSD of another cluster or one this operation did not create", osdDiskRemedy+"; for an OSD the cluster runs, find it with ceph osd metadata and remove it outside Bootwright; "+
			"a never-booted OSD that an interrupted OSD creation left is purged by the next apply when this operation cleared its disk, "+
			"otherwise purge it with ceph osd purge-new osd.<id> --yes-i-really-mean-it after ceph osd dump shows it never booted (down, up_from 0, state exists and new)"),
		"osd-partial": failure("lifecycle.unknown", "an OSD disk on a Ceph host that this operation prepared holds LVM metadata without a complete OSD, and an interrupted OSD creation cannot be told apart from a foreign write: it is not exactly this operation's own unfinished ceph-volume volume, its OSD service declares DB or WAL devices, ceph-volume is still running on that host, or this operation already cleared that disk once",
			"apply again once no ceph-volume runs on the host, which clears the disk only when it holds exactly one ceph-volume volume group with one untagged block volume of this cluster and nothing else, for an OSD service without DB or WAL devices; otherwise run wipefs --no-act <disk>, pvs and lvs -o +lv_tags on the host to see what it holds, and clear it yourself after confirming it is that disk, then apply again; Bootwright never clears a disk it cannot prove its own"),
		"osd-clear-failed": failure("lifecycle.unknown", "ceph-volume lvm zap --destroy failed on an OSD disk that held only this operation's own unfinished OSD volume, so the disk may be partly cleared",
			"read the native error, correct what it names on the host, then apply again: the clear's intent record admits the partly cleared disk, and the clear runs again; Bootwright never clears any other disk"),
		"osd-orphan-booted": failure("lifecycle.unknown", "the Ceph OSD map holds an OSD of the OSD fsid that a cleared, unfinished ceph-volume volume named, and that OSD has booted, so Bootwright did not purge it",
			"find it with ceph osd dump on the bootstrap host; an OSD that booted is not the orphan of an interrupted creation, so inspect it and remove it outside Bootwright, then apply again; Bootwright purges only an OSD that never booted"),
		"osd-orphan-unproved": failure("lifecycle.unknown", "the never-booted OSD that a cleared, unfinished ceph-volume volume named is still in the Ceph OSD map after ceph osd purge-new, or the map holds more than one OSD of its fsid",
			"read the native error and check the OSD map with ceph osd dump on the bootstrap host, then apply again; the purge settles from the map, so an OSD already gone is not purged again"),
		"osd-clear-unproved": failure("lifecycle.unknown", "after ceph-volume cleared an OSD disk that held only this operation's own unfinished OSD volume, the disk did not read as empty, or the host's LVM devices file kept that volume's entry",
			"run wipefs --no-act <disk> and lvmdevices on the host to see what remains, then apply again; the clear's intent record admits a partly cleared disk and its replay finishes the clear"),
	}
}

func foreignClientRefusal(name string) error {
	return failure("lifecycle.unknown", "Ceph client client."+name+" exists that this operation did not create",
		"inspect the foreign client's consumers and authorization outside Bootwright; if it can be removed safely, remove it outside Bootwright, then apply again with the same input; "+
			"to use another CephClient name, first take back what this context owns with bootwright destroy --context <context>, then rename the CephClient with bootwright context update and apply; Bootwright never adopts a client it did not create")
}

func adapterRefusals() map[string]error {
	out := map[string]error{"enrollment-budget": failure("lifecycle.unknown", "the Ceph enrollment worker has insufficient remaining budget to admit a host", "apply again to continue the prepared cluster enrollment"), "unsupported-release": unsupportedFailure(), "runtime-profile": failure("lifecycle.unknown", "the Ceph host runtime profile is missing or contradicts the frozen image", "apply again so each ceph-host block re-proves its runtime profile before the cluster block continues"), "device-identity": failure("lifecycle.unknown", "the OSD device identities are missing or contradict the host preparation", "apply again so each ceph-host block re-derives its device identities"), "cache-unproved": failure("lifecycle.unknown", "the published cephadm cache is not the exact authenticated payload", "inspect the host's /var/lib/ceph cache before continuing; a foreign cephadm binary is never executed"), "dashboard-identity": failure("lifecycle.unknown", "the Ceph dashboard user admin or its retained password is not the one this operation created", "inspect the dashboard's users; an admin user without this operation's attempt record is never adopted, and a lost retained password is never regenerated"), "dashboard-manager": failure("lifecycle.unknown", "a Ceph MGR's container or dashboard port no longer matches the deployment this operation recorded", "inspect `ceph orch ps --daemon_type mgr`; Bootwright never adopts or repairs an MGR container it did not record, so take back what it owns with `bootwright destroy --context <context>` before applying again"), "dashboard-drift": failure("lifecycle.unknown", "the Ceph dashboard user admin no longer matches the password this context's custody holds", "log in with the custodied password that bootwright cluster info --secrets --name <cluster> reveals, or set the admin password back to it; Bootwright never resets it"), "dashboard-policy": failure("lifecycle.unknown", "the Ceph dashboard refused the generated administrator password under its password policy", "inspect the dashboard's password policy settings; Bootwright never retries with a new password"), "dashboard-verify": failure("lifecycle.unsupported", "the bootstrap host cannot verify the dashboard password hash", "use the pinned managed OS, whose Python reads bcrypt hashes; the check is never skipped"), "pool-identity": failure("lifecycle.unknown", "the Ceph pool's identity is not the one this operation created", "inspect the pool; a same-name pool without this operation's creation record is never adopted"), "client-quiescence-unqualified": failure("lifecycle.unsupported", "Ceph client quiescence has not been qualified", "prove all consumers stopped before qualifying online removal"), "native-option-undeclared": failure("lifecycle.unsupported", "the selected host cephadm package does not declare a bootstrap option Bootwright passes", "the apply froze this input, so take back what it owns with bootwright destroy --context <context>, select a cephadm package version whose release qualification covers this bootstrap with bootwright context update, then apply; the native error names the missing option and the selected package version"), "call-home-unqualified": failure("lifecycle.unsupported", "IBM Call Home intent has no qualified path on the selected Ceph build", "the apply froze this input, so take back what it owns with bootwright destroy --context <context>, correct spec.distribution.ibm.callHome for the selected build with bootwright context update, then apply; see .agents/knowledge/version-specializations.md")}
	out["dashboard-tls-identity"] = failure("lifecycle.unknown", "the Ceph dashboard TLS pair or its retained custody is not the one this operation created", "inspect the retained dashboard TLS custody and native global and per-manager certificates; Bootwright never adopts or resets foreign material, changed material or missing completed material")
	out["fips-policy"] = failure("lifecycle.state", "a Ceph host no longer proves the installed FIPS kernel and inherited crypto policy", "restore the installed FIPS policy; Bootwright never changes crypto policy")
	out["foreign-client"] = foreignClientRefusal("<name>")
	out["cephx-policy"] = failure("lifecycle.unsupported", "native Ceph keys do not prove the requested AES CephX policy", "select native artifacts that realize security.cephx.keyType aes; inspect the native error and retain the operation for recovery or destroy")
	out["service-credential-foreign"] = failure("lifecycle.unknown", "a Ceph service a service-unit block applied stores a credential other than the one its bound Secret holds", "find the service and leaf the native error names with ceph orch ls --export; Bootwright never replaces a credential of a deployed service: store the deployed value again in the Secret that the service's spec.serviceBindings entry binds to that leaf with bootwright secret set, or take back what this context owns with bootwright destroy --context <context>, then apply")
	out["foreign-pool"] = failure("lifecycle.unknown", "a Ceph pool of the CephPool's name, or of a zone pool a CephObjectGateway derives, exists that this operation did not create", "rename the CephPool or the CephObjectGateway's spec.zone, or remove the foreign pool outside Bootwright; a pool without this operation's creation record is never adopted")
	out["foreign-profile"] = failure("lifecycle.unknown", "an erasure-code profile named after the CephPool exists that this operation did not create", "rename the CephPool, or remove the foreign profile outside Bootwright; Bootwright never overrides a profile with --force")
	out["foreign-rule"] = failure("lifecycle.unknown", "a CRUSH rule of the frozen name exists that this operation did not create, or it is not the rule this operation created", "rename the CephPlacementPolicy's ruleName or the erasure-coded CephPool, or remove the foreign rule outside Bootwright")
	out["osd-class-drift"] = failure("lifecycle.unknown", "a booted OSD of the Ceph cluster carries a CRUSH device class other than the crush_device_class its OSD service declares", "find the OSD with ceph osd tree; set its class back outside Bootwright with ceph osd crush rm-device-class osd.<id> and ceph osd crush set-device-class <class> osd.<id>, then apply again; cephadm gives a class only to an OSD it creates, and Bootwright never changes the class of one")
	out["crush-capacity"] = failure("lifecycle.unknown", "fewer OSD hosts under root default carry the frozen device class than the protection width needs", "add OSD hosts whose OSDs carry that device class, or change crushDeviceClass, then apply again")
	out["foreign-rbd-image"] = failure("lifecycle.unknown", "an RBD image of the CephRBDImage's name exists in its pool that this operation did not create", "rename the CephRBDImage, or remove or rename the foreign image outside Bootwright; an image without this operation's creation record is never adopted")
	out["rbd-image-identity"] = failure("lifecycle.unknown", "the RBD image is not the one this operation created, is gone, or no longer matches its declared size, data pool or features", "inspect it with rbd info and rbd trash ls; restore the CephRBDImage's sizeBytes, dataPoolRef and features to the values it was created with, since Bootwright never recreates, resizes, moves or re-features an image it created")
	out["foreign-filesystem"] = failure("lifecycle.unknown", "a CephFS filesystem of the CephFilesystem's name exists, or uses one of its pools, that this operation did not create", "rename the CephFilesystem or give it other pools, or remove the foreign filesystem outside Bootwright; a filesystem without this operation's creation record is never adopted")
	out["filesystem-identity"] = failure("lifecycle.unknown", "the CephFS filesystem is not the one this operation created, is gone, or no longer matches its declared pools", "inspect it with ceph fs dump; restore the CephFilesystem to the pools and settings it was created with, since Bootwright never recreates a filesystem or moves its pools")
	out["filesystem-multiple-disabled"] = failure("lifecycle.unknown", "the cluster's FSMap disables multiple filesystems", "the cluster's enable_multiple flag is off; declare one filesystem, or enable the flag outside Bootwright")
	out["foreign-subvolume-group"] = failure("lifecycle.unknown", "a subvolume group of the declared name exists in the filesystem that this operation did not create", "rename the group in spec.subvolumeGroups, or remove the foreign group outside Bootwright; Bootwright never re-creates a group it did not create, because a native create rewrites its attributes")
	out["mds-coverage"] = failure("lifecycle.unknown", "fewer MDS daemons of the bound service are available than the filesystem's active ranks, standby-replay followers and wanted standbys need", "check the MDS daemons with ceph orch ps --daemon_type mds, add hosts to the mds service placement or lower spec.mds.activeCount or spec.mds.standbyCountWanted, then apply again")
	out["foreign-subvolume"] = failure("lifecycle.unknown", "a CephFS subvolume of the CephSubvolume's name exists in its filesystem and group that this operation did not create", "rename the CephSubvolume, or remove or rename the foreign subvolume outside Bootwright; a subvolume without this operation's creation record is never adopted, because a native create rewrites its attributes")
	out["subvolume-identity"] = failure("lifecycle.unknown", "the CephFS subvolume is not the one this operation created, is gone, no longer matches its declared group, layout, owner, mode or quota, or its owner metadata names another operation", "inspect it with ceph fs subvolume info and ceph fs subvolume metadata ls; restore the CephSubvolume to the values it was created with, since Bootwright never recreates, resizes or moves a subvolume it created")
	out["foreign-image"] = failure("lifecycle.unknown", "a cephadm container_image setting of the Ceph cluster holds an image this operation did not freeze, is set outside the mgr section, or no longer holds the frozen image a declared service needs", "find the setting the native error names in ceph config dump, then remove it with ceph config rm or set it to the frozen image outside Bootwright and apply again; Bootwright never overwrites a container_image setting it did not write")
	out["nfs-module-disabled"] = failure("lifecycle.unknown", "the Ceph cluster's nfs manager module is disabled, although the cluster's settings block enabled it for the declared NFS service", "enable it again outside Bootwright with ceph mgr module enable nfs, then apply again; the settings block does not run again once it completed, and an NFS service block never enables a manager module")
	out["nfs-pool-foreign"] = failure("lifecycle.unknown", "a Ceph pool named .nfs exists that this operation's NFS services did not create, or it is not the pool they recorded", "find the .nfs pool with ceph osd pool ls detail; if nothing uses it, remove it outside Bootwright and apply again, or take back what this context owns with bootwright destroy --context <context>, then apply; Bootwright never adopts a .nfs pool it did not create")
	out["object-realm-foreign"] = failure("lifecycle.unknown", "the cluster holds RGW configuration this operation did not create: a .rgw.root pool without this operation's claim, or a realm, zone group or zone of the CephObjectGateway's names without its creation record, or one it created that is gone or changed", "find what the native error names with ceph osd pool ls and radosgw-admin realm list, zonegroup list and zone list; for a foreign name, rename spec.realm, spec.zoneGroup or spec.zone of the CephObjectGateway, or remove the foreign configuration outside Bootwright, then apply again; when the native error says one this operation created is gone or changed, take back what this context owns with bootwright destroy --context <context>, then apply; Bootwright never adopts RGW configuration it did not create")
	out["object-zone-pools"] = failure("lifecycle.unknown", "the zone a CephObjectGateway created names a pool Bootwright did not plan, or its default placement does not hold the pools the gateway froze", "inspect the zone with radosgw-admin zone get --rgw-realm <realm> --rgw-zone <zone>; the native error names the pool; take back what this context owns with bootwright destroy --context <context>, then apply; Bootwright creates every zone pool before RGW starts and never lets RGW create a pool it did not plan")
	out["object-endpoint-certificate"] = failure("lifecycle.unknown", "a TLS service of a CephObjectGateway holds a certificate or private key other than the one its bound Secret holds", "find the service the native error names with ceph orch ls; Bootwright never replaces the certificate of a deployed gateway service: store the deployed certificate and key in the Secret again with bootwright secret set --name <secret> --certificate-file <path> --private-key-file <path>, or take back what this context owns with bootwright destroy --context <context>, then apply")
	out["object-user-foreign"] = failure("lifecycle.unknown", "an RGW user of the CephObjectUser's ID exists in its CephObjectGateway's realm that this operation did not create, or the user this operation created is gone", "find the user the native error names with radosgw-admin user list --rgw-realm <realm> on the bootstrap host; for a user this operation did not create, rename the CephObjectUser or remove the foreign user outside Bootwright, then apply again; when the native error says the user this operation created is gone, take back what this context owns with bootwright destroy --context <context>, then apply; Bootwright never adopts an RGW user it did not create")
	out["object-user-identity"] = failure("lifecycle.unknown", "the RGW user this operation created no longer matches its CephObjectUser or the S3 key pair this context's custody retains: its display name, keys, subusers, Swift keys, capabilities or suspension changed, or the retained key pair is missing or unreadable", "inspect the user with radosgw-admin user info --uid <user> --rgw-realm <realm> on the bootstrap host and restore its display name, its one S3 key and its unsuspended state, removing any key, subuser or capability added outside Bootwright, then apply again; or take back what this context owns with bootwright destroy --context <context>, then apply; Bootwright never rotates or regenerates the keys of a user it created")
	out["object-bucket-foreign"] = failure("lifecycle.unknown", "a bucket of the CephObjectBucket's name exists in its CephObjectGateway's realm that this operation did not create, or the bucket this operation created is gone", "find the bucket the native error names with radosgw-admin bucket list --rgw-realm <realm> on the bootstrap host; for a bucket this operation did not create, rename the CephObjectBucket or remove the foreign bucket outside Bootwright, then apply again; when the native error says the bucket this operation created is gone, take back what this context owns with bootwright destroy --context <context>, then apply; Bootwright never adopts a bucket it did not create")
	out["object-bucket-identity"] = failure("lifecycle.unknown", "the bucket this operation created no longer matches its CephObjectBucket: another owner, bucket ID or zone group, or a bucket policy other than the declared one", "inspect the bucket with radosgw-admin bucket stats --bucket <bucket> --rgw-realm <realm> on the bootstrap host and its policy with an S3 GetBucketPolicy as its owner; restore its owner and the declared spec.policy, or remove a policy the CephObjectBucket does not declare, outside Bootwright, then apply again; or take back what this context owns with bootwright destroy --context <context>, then apply")
	out["object-bucket-s3"] = failure("lifecycle.unknown", "an RGW daemon of the CephObjectGateway answered the CephObjectBucket's S3 request, signed with its owner's key, with an error or a status of 500 or more, or took the request and sent no complete answer, after which the request goes to no other daemon", "read the native error, which holds the S3 request, the HTTP status and RGW's error code and message: correct spec.policy for MalformedPolicy, choose another bucket name for BucketAlreadyExists, or restore the owner CephObjectUser's key and unsuspended state for AccessDenied, InvalidAccessKeyId or UserSuspended, outside Bootwright, then apply again; for a status of 500 or more or no complete answer, check the gateway's rgw daemons with ceph orch ps, then apply again, which finds a bucket the lost request created with bucket stats and does not create it again")
	out["nfs-export-foreign"] = failure("lifecycle.unknown", "an NFS export exists at the CephNFSExport's pseudo path on its NFS service that this operation did not create, or the export this operation created is gone or now has another export ID, CephFS principal or S3 key", "find the export the native error names with ceph nfs export info <service> <pseudo>; for an export this operation did not create, set another spec.native.pseudo on the CephNFSExport or remove the foreign export outside Bootwright, then apply again; when the native error says the export this operation created is gone or changed, take back what this context owns with bootwright destroy --context <context>, then apply; Bootwright never adopts or overwrites an NFS export it did not create")
	out["nvmeof-budget"] = failure("lifecycle.unsupported", "the NVMe-oF subsystem exceeds the six-hour bounded execution limit", "declare fewer namespaces or initiator hosts in this subsystem")
	out["nvmeof-native"] = failure("lifecycle.unknown", "the qualified NVMe-oF native command is unavailable or refused the operation", "verify gateway mTLS and the selected image's registered ceph nvmeof commands, then retry the original operation")
	out["nvmeof-identity"] = failure("lifecycle.unknown", "NVMe-oF native identity, backing image, authorization or gateway state differs from the frozen request", "restore the original owned gateway, image and subsystem identities before retrying; Bootwright does not overwrite foreign resources")
	out["nvmeof-foreign"] = failure("lifecycle.unknown", "an NVMe-oF subsystem exists without this operation's creation intent", "choose an unused subsystem NQN or retire the foreign resource outside Bootwright")
	out["nfs-export-apply"] = failure("lifecycle.unknown", "the NFS manager did not apply the CephNFSExport's export cleanly: its NFS service is not listed, the apply failed or reported a warning, or the stored export differs from the one applied", "read the native error, which holds the NFS manager's answer; check the service with ceph nfs cluster ls and the export with ceph nfs export info <service> <pseudo>, correct what the error names outside Bootwright (a missing directory, a path another protocol claims, or NFS daemons that need ceph orch restart nfs.<service>), then apply again")
	out["nfs-export-realm-default"] = failure("lifecycle.unknown", "an export of an RGW user needs its CephObjectGateway's realm as the cluster's default realm, through which the NFS manager and NFS-Ganesha find the user, and another realm is the default realm", "find the default realm with radosgw-admin realm list on the bootstrap host; when nothing else relies on it, clear it with radosgw-admin realm default rm outside Bootwright, then apply again; Bootwright wrote nothing for this export and never moves a default realm another tool set")
	out["nfs-export-path-escape"] = failure("lifecycle.unknown", "the CephNFSExport's spec.native.path does not lie within the CephSubvolume it binds, or its spec.native.fsal.cmount_path is neither / nor the export path or a directory above it", "omit spec.native.path so the export serves the subvolume's own path, or set it to the path ceph fs subvolume getpath reports for the subvolume or a directory below it, and set spec.native.fsal.cmount_path to / or a directory that contains the export path, then apply again; Bootwright wrote nothing for this export")
	out["ingress-firewall"] = failure("lifecycle.unknown", "a host that runs the ingress of a Ceph NFS service, management gateway or CephObjectGateway has an active firewalld that does not permit VRRP (IP protocol 112) or a port the ingress serves on the interface that carries its virtual IP", "either permit what the native error names in the firewalld zone it names on the host it names, for example with firewall-cmd --permanent --zone=<zone> --add-protocol=vrrp --add-port=<port>/tcp and firewall-cmd --reload, or let Bootwright realize them by installing that host's Machine with customizations.security.firewall.enabled: true, then apply again; this apply changed nothing before the check")
	out["pg-budget"] = failure("lifecycle.unknown", "the monitor refused the pool: placement groups per OSD on its CRUSH rule would exceed mon_max_pg_per_osd", "add OSDs, lower pgNumMin or targetSizeRatio, or declare fewer pools on that CRUSH rule, then apply again")
	maps.Copy(out, sshPolicyRefusals())
	maps.Copy(out, osdDeviceRefusals())
	maps.Copy(out, stretchRefusals())
	out["tunnel-host-key-excluded"] = failure("lifecycle.unsupported", "the SSH crypto policy inside the selected Ceph image admits none of the algorithms of a Ceph host's pinned host key, so cephadm's tunnel could not reach that host; the cluster block refused before this apply claimed or authorized any Ceph host", "the native error names the host and its key type; the apply froze this input, so take back what it owns with bootwright destroy --context <context>, then with bootwright context update select a Ceph image whose SSH crypto policy admits that type, or change spec.customizations.security.fips on that host's MachineInstallProfile so its fresh installation generates a host key of an admitted type (ssh-ed25519 with fips off, ecdsa-sha2-nistp256 with fips on), then apply")
	out["pool-drift"] = failure("lifecycle.unknown", "a setting this operation completed on the Ceph pool (size, min_size, a quota, a compression option, erasure-coded overwrites or its application) no longer has the value its CephPool or CephObjectGateway froze", "compare ceph osd pool ls detail with the CephPool and set that setting back to its frozen value outside Bootwright, then apply again; or take back what this context owns with bootwright destroy --context <context>, then apply; Bootwright never rewrites a completed pool setting")
	out["sso-unverified"] = failure("lifecycle.unknown", "an oauth2-proxy daemon of a Ceph management gateway runs with a configuration that skips the identity provider's certificate verification, so Bootwright did not enable the dashboard's single sign-on", "the native error names the host, the daemon and where the skip was found; take back what this context owns with bootwright destroy --context <context>, then with bootwright context update either select a Ceph image whose cephadm renders oauth2-proxy without that skip, or remove the oauth2-proxy service, its spec.serviceBindings entries and spec.enable_auth on the mgmt-gateway, then apply; Bootwright never changes an applied management gateway unit, so removing the service without that destroy refuses")
	for _, reason := range []string{"request", "artifact", "ownership", "evidence", "material", "packages", "package-artifact", "package-drift", "package-journal", "package-license", "package-limit", "package-timeout", "package-platform", "package-host", "call-home-drift", "config-drift", "partial-cluster", "not-ready", "not-removed", "cluster-unavailable", "device", "foreign-cluster", "native-command", "native-timeout", "native-output", "unknown", "transport", "remote-refusal", "adapter-exception"} {
		out[reason] = failure("lifecycle.unknown", "the Ceph adapter could not prove its "+reason+" boundary", "inspect the retained operation evidence before continuing")
	}
	return out
}

func clusterHostBudget(cluster string, hosts int, stretch bool) (string, string) {
	return fmt.Sprintf("CephCluster %s declares %d Ceph hosts, whose cluster run deadline of %s is past the %s every adapter run is held to", cluster, hosts, applyDeadline(hosts, stretch), lifecycle.MaxDeadline),
		fmt.Sprintf("declare at most %d entries in spec.topology.nodes of CephCluster %s; B1183 tracks larger clusters", hostBound(stretch), cluster)
}
