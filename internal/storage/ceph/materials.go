package ceph

import (
	"context"
	"encoding/json"
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
	bound := maps.Clone(e.Material)
	if bound == nil {
		bound = map[string]secrets.Material{}
	}
	known := strings.Builder{}
	for _, h := range r.Hosts {
		key, err := provedHostKey(h, e, readers)
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
	secret := ""
	if r.Action == "gateway" {
		secret = gatewayTLSSecret(r.Gateway)
	}
	if secret == "" {
		return nil, nil
	}
	files := []lifecycle.MaterialFile{{Name: "gateway-certificate", Secret: secret, Part: secrets.CertificatePart, Variable: "gatewayCertificate"}}
	if operation == "apply" {
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

func (c Capability) run(ctx context.Context, e lifecycle.Execution, operation string, r Request) (lifecycle.RunResult, error) {
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
	if (operation == "apply" || operation == "observe") && r.Action == "client" {
		outputs = append(outputs, lifecycle.OutputFile{Name: "client", Variable: "client"})
	}
	if (operation == "apply" || operation == "observe") && r.Action == "cluster" {
		outputs = append(outputs, lifecycle.OutputFile{Name: DashboardOutput, Variable: DashboardOutput})
	}
	return c.runner.Run(ctx, lifecycle.RunFor(execution, lifecycle.Invocation{Implementation: Implementation, Operation: operation, Variable: "bootwright_ceph", Canonical: canonical, Placement: machine.Placement{Connection: machine.ConnectionLocal, Machine: r.Controller}, Materials: files, Outputs: outputs, Refusals: adapterRefusals(), NativeErrors: true, Deadline: deadline}))
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
	if r.Action == "nfs-export" {
		return nfsExportDeadline(operation), nil
	}
	nativeCalls, waits := len(r.Hosts)+2, 0
	if operation == "observe" && r.Action == "cluster" {
		nativeCalls++
	}
	if operation == "apply" {
		nativeCalls = 2*len(r.Hosts) + 3
		if r.Action == "cluster" {
			nativeCalls, waits = 7*len(r.Hosts)+6, bootstrapCall+2*readinessWait
		}
		if r.Action == "filesystem" && r.Filesystem != nil {
			nativeCalls, waits = 2*len(r.Hosts)+3+len(r.Filesystem.DataPools)+len(r.Filesystem.Groups), readinessWait
			if len(r.Filesystem.Groups) > 0 {
				waits += readinessWait
			}
		}
	}
	deadline := time.Duration(nativeCalls*nativeCall+waits+60) * time.Second
	if deadline > lifecycle.MaxDeadline {
		return 0, failure("lifecycle.unsupported", "the serial Ceph adapter's bounded work exceeds its six-hour run ceiling", "use the three-node core lab; larger clusters need a qualified execution budget")
	}
	return deadline, nil
}

func hostDeadline(operation string) time.Duration {
	switch operation {
	case "apply":
		return time.Duration(1980+4345+3*660+60) * time.Second
	case "observe":
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

func adapterRefusals() map[string]error {
	out := map[string]error{"unsupported-release": unsupportedFailure(), "runtime-profile": failure("lifecycle.unknown", "the Ceph host runtime profile is missing or contradicts the frozen image", "apply again so each ceph-host block re-proves its runtime profile before the cluster block continues"), "device-identity": failure("lifecycle.unknown", "the OSD device identities are missing or contradict the host preparation", "apply again so each ceph-host block re-derives its device identities"), "cache-unproved": failure("lifecycle.unknown", "the published cephadm cache is not the exact authenticated payload", "inspect the host's /var/lib/ceph cache before continuing; a foreign cephadm binary is never executed"), "dashboard-identity": failure("lifecycle.unknown", "the Ceph dashboard user admin or its retained password is not the one this operation created", "inspect the dashboard's users; an admin user without this operation's attempt record is never adopted, and a lost retained password is never regenerated"), "dashboard-manager": failure("lifecycle.unknown", "a Ceph MGR's container or dashboard port no longer matches the deployment this operation recorded", "inspect `ceph orch ps --daemon_type mgr`; Bootwright never adopts or repairs an MGR container it did not record, so take back what it owns with `bootwright destroy --context <context>` before applying again"), "dashboard-drift": failure("lifecycle.unknown", "the Ceph dashboard user admin no longer matches the password this context's custody holds", "log in with the custodied password that bootwright cluster info --secrets --name <cluster> reveals, or set the admin password back to it; Bootwright never resets it"), "dashboard-policy": failure("lifecycle.unknown", "the Ceph dashboard refused the generated administrator password under its password policy", "inspect the dashboard's password policy settings; Bootwright never retries with a new password"), "dashboard-verify": failure("lifecycle.unsupported", "the bootstrap host cannot verify the dashboard password hash", "use the pinned managed OS, whose Python reads bcrypt hashes; the check is never skipped"), "pool-identity": failure("lifecycle.unknown", "the Ceph pool's identity is not the one this operation created", "inspect the pool; a same-name pool without this operation's creation record is never adopted"), "client-quiescence-unqualified": failure("lifecycle.unsupported", "Ceph client quiescence has not been qualified", "prove all consumers stopped before qualifying online removal"), "native-option-undeclared": failure("lifecycle.unsupported", "the selected host cephadm package does not declare a bootstrap option Bootwright passes", "the apply froze this input, so take back what it owns with bootwright destroy --context <context>, select a cephadm package version whose release qualification covers this bootstrap with bootwright context update, then apply; the native error names the missing option and the selected package version"), "call-home-unqualified": failure("lifecycle.unsupported", "IBM Call Home intent has no qualified path on the selected Ceph build", "the apply froze this input, so take back what it owns with bootwright destroy --context <context>, correct ceph.ibm.callHome for the selected build with bootwright context update, then apply; see .agents/knowledge/version-specializations.md")}
	out["service-credential-foreign"] = failure("lifecycle.unknown", "a Ceph service a service-unit block applied stores a credential other than the one its bound Secret holds", "find the service and leaf the native error names with ceph orch ls --export; Bootwright never replaces a credential of a deployed service: store the deployed value again in the Secret that the service's spec.ceph.serviceBindings entry binds to that leaf with bootwright secret set, or take back what this context owns with bootwright destroy --context <context>, then apply")
	out["foreign-pool"] = failure("lifecycle.unknown", "a Ceph pool of the StoragePool's name, or of a zone pool a StorageObjectGateway derives, exists that this operation did not create", "rename the StoragePool or the StorageObjectGateway's spec.zone, or remove the foreign pool outside Bootwright; a pool without this operation's creation record is never adopted")
	out["foreign-profile"] = failure("lifecycle.unknown", "an erasure-code profile named after the StoragePool exists that this operation did not create", "rename the StoragePool, or remove the foreign profile outside Bootwright; Bootwright never overrides a profile with --force")
	out["foreign-rule"] = failure("lifecycle.unknown", "a CRUSH rule of the frozen name exists that this operation did not create, or it is not the rule this operation created", "rename the StoragePlacementPolicy's ruleName or the erasure-coded StoragePool, or remove the foreign rule outside Bootwright")
	out["crush-capacity"] = failure("lifecycle.unknown", "fewer OSD hosts under root default carry the frozen device class than the protection width needs", "add OSD hosts whose OSDs carry that device class, or change crushDeviceClass, then apply again")
	out["foreign-rbd-image"] = failure("lifecycle.unknown", "an RBD image of the StorageRBDImage's name exists in its pool that this operation did not create", "rename the StorageRBDImage, or remove or rename the foreign image outside Bootwright; an image without this operation's creation record is never adopted")
	out["rbd-image-identity"] = failure("lifecycle.unknown", "the RBD image is not the one this operation created, is gone, or no longer matches its declared size, data pool or features", "inspect it with rbd info and rbd trash ls; restore the StorageRBDImage's sizeBytes, dataPoolRef and features to the values it was created with, since Bootwright never recreates, resizes, moves or re-features an image it created")
	out["foreign-filesystem"] = failure("lifecycle.unknown", "a CephFS filesystem of the StorageFilesystem's name exists, or uses one of its pools, that this operation did not create", "rename the StorageFilesystem or give it other pools, or remove the foreign filesystem outside Bootwright; a filesystem without this operation's creation record is never adopted")
	out["filesystem-identity"] = failure("lifecycle.unknown", "the CephFS filesystem is not the one this operation created, is gone, or no longer matches its declared pools", "inspect it with ceph fs dump; restore the StorageFilesystem to the pools and settings it was created with, since Bootwright never recreates a filesystem or moves its pools")
	out["filesystem-multiple-disabled"] = failure("lifecycle.unknown", "the cluster's FSMap disables multiple filesystems", "the cluster's enable_multiple flag is off; declare one filesystem, or enable the flag outside Bootwright")
	out["foreign-subvolume-group"] = failure("lifecycle.unknown", "a subvolume group of the declared name exists in the filesystem that this operation did not create", "rename the group in spec.subvolumeGroups, or remove the foreign group outside Bootwright; Bootwright never re-creates a group it did not create, because a native create rewrites its attributes")
	out["mds-coverage"] = failure("lifecycle.unknown", "fewer MDS daemons of the bound service are available than the filesystem's active ranks, standby-replay followers and wanted standbys need", "check the MDS daemons with ceph orch ps --daemon_type mds, add hosts to the mds service placement or lower spec.mds.activeCount or spec.mds.standbyCountWanted, then apply again")
	out["foreign-subvolume"] = failure("lifecycle.unknown", "a CephFS subvolume of the StorageSubvolume's name exists in its filesystem and group that this operation did not create", "rename the StorageSubvolume, or remove or rename the foreign subvolume outside Bootwright; a subvolume without this operation's creation record is never adopted, because a native create rewrites its attributes")
	out["subvolume-identity"] = failure("lifecycle.unknown", "the CephFS subvolume is not the one this operation created, is gone, no longer matches its declared group, layout, owner, mode or quota, or its owner metadata names another operation", "inspect it with ceph fs subvolume info and ceph fs subvolume metadata ls; restore the StorageSubvolume to the values it was created with, since Bootwright never recreates, resizes or moves a subvolume it created")
	out["foreign-image"] = failure("lifecycle.unknown", "a cephadm container_image setting of the Ceph cluster holds an image this operation did not freeze, is set outside the mgr section, or no longer holds the frozen image a declared service needs", "find the setting the native error names in ceph config dump, then remove it with ceph config rm or set it to the frozen image outside Bootwright and apply again; Bootwright never overwrites a container_image setting it did not write")
	out["nfs-module-disabled"] = failure("lifecycle.unknown", "the Ceph cluster's nfs manager module is disabled, although the cluster's settings block enabled it for the declared NFS service", "enable it again outside Bootwright with ceph mgr module enable nfs, then apply again; the settings block does not run again once it completed, and an NFS service block never enables a manager module")
	out["nfs-pool-foreign"] = failure("lifecycle.unknown", "a Ceph pool named .nfs exists that this operation's NFS services did not create, or it is not the pool they recorded", "find the .nfs pool with ceph osd pool ls detail; if nothing uses it, remove it outside Bootwright and apply again, or take back what this context owns with bootwright destroy --context <context>, then apply; Bootwright never adopts a .nfs pool it did not create")
	out["object-realm-foreign"] = failure("lifecycle.unknown", "the cluster holds RGW configuration this operation did not create: a .rgw.root pool without this operation's claim, or a realm, zone group or zone of the StorageObjectGateway's names without its creation record, or one it created that is gone or changed", "find what the native error names with ceph osd pool ls and radosgw-admin realm list, zonegroup list and zone list; for a foreign name, rename spec.realm, spec.zoneGroup or spec.zone of the StorageObjectGateway, or remove the foreign configuration outside Bootwright, then apply again; when the native error says one this operation created is gone or changed, take back what this context owns with bootwright destroy --context <context>, then apply; Bootwright never adopts RGW configuration it did not create")
	out["object-zone-pools"] = failure("lifecycle.unknown", "the zone a StorageObjectGateway created names a pool Bootwright did not plan, or its default placement does not hold the pools the gateway froze", "inspect the zone with radosgw-admin zone get --rgw-realm <realm> --rgw-zone <zone>; the native error names the pool; take back what this context owns with bootwright destroy --context <context>, then apply; Bootwright creates every zone pool before RGW starts and never lets RGW create a pool it did not plan")
	out["object-endpoint-certificate"] = failure("lifecycle.unknown", "a TLS service of a StorageObjectGateway holds a certificate or private key other than the one its bound Secret holds", "find the service the native error names with ceph orch ls; Bootwright never replaces the certificate of a deployed gateway service: store the deployed certificate and key in the Secret again with bootwright secret set --name <secret> --certificate-file <path> --private-key-file <path>, or take back what this context owns with bootwright destroy --context <context>, then apply")
	out["nfs-export-foreign"] = failure("lifecycle.unknown", "an NFS export exists at the StorageNFSExport's pseudo path on its NFS service that this operation did not create, or the export this operation created is gone or now has another export ID or CephFS principal", "find the export the native error names with ceph nfs export info <service> <pseudo>; for an export this operation did not create, set another spec.native.pseudo on the StorageNFSExport or remove the foreign export outside Bootwright, then apply again; when the native error says the export this operation created is gone or changed, take back what this context owns with bootwright destroy --context <context>, then apply; Bootwright never adopts or overwrites an NFS export it did not create")
	out["nfs-export-apply"] = failure("lifecycle.unknown", "the NFS manager did not apply the StorageNFSExport's export cleanly: its NFS service is not listed, the apply failed or reported a warning, or the stored export differs from the one applied", "read the native error, which holds the NFS manager's answer; check the service with ceph nfs cluster ls and the export with ceph nfs export info <service> <pseudo>, correct what the error names outside Bootwright (a missing directory, a path another protocol claims, or NFS daemons that need ceph orch restart nfs.<service>), then apply again")
	out["nfs-export-path-escape"] = failure("lifecycle.unknown", "the StorageNFSExport's spec.native.path does not lie within the StorageSubvolume it binds, or its spec.native.fsal.cmount_path is neither / nor the export path or a directory above it", "omit spec.native.path so the export serves the subvolume's own path, or set it to the path ceph fs subvolume getpath reports for the subvolume or a directory below it, and set spec.native.fsal.cmount_path to / or a directory that contains the export path, then apply again; Bootwright wrote nothing for this export")
	out["ingress-firewall"] = failure("lifecycle.unknown", "a host that runs the ingress of a Ceph NFS service or StorageObjectGateway has an active firewalld that does not permit VRRP (IP protocol 112) or the ingress's client port on the interface that carries its virtual IP", "permit VRRP (IP protocol 112) and the port the native error names in the firewalld zone it names on the host it names, for example with firewall-cmd --permanent --zone=<zone> --add-protocol=vrrp --add-port=<port>/tcp and firewall-cmd --reload, or install that host without an active firewall, then apply again; Bootwright never changes a host's firewall, and this apply changed nothing before the check")
	out["pg-budget"] = failure("lifecycle.unknown", "the monitor refused the pool: placement groups per OSD on its CRUSH rule would exceed mon_max_pg_per_osd", "add OSDs, lower pgNumMin or targetSizeRatio, or declare fewer pools on that CRUSH rule, then apply again")
	maps.Copy(out, sshPolicyRefusals())
	out["tunnel-host-key-excluded"] = failure("lifecycle.unsupported", "the SSH crypto policy inside the selected Ceph image admits none of the algorithms of a Ceph host's pinned host key, so cephadm's tunnel could not reach that host; the cluster block refused before this apply claimed or authorized any Ceph host", "the native error names the host and its key type; the apply froze this input, so take back what it owns with bootwright destroy --context <context>, then with bootwright context update select a Ceph image whose SSH crypto policy admits that type, or change spec.customizations.security.fips on that host's MachineInstallProfile so its fresh installation generates a host key of an admitted type (ssh-ed25519 with fips off, ecdsa-sha2-nistp256 with fips on), then apply")
	out["pool-drift"] = failure("lifecycle.unknown", "a setting this operation completed on the Ceph pool (size, min_size, a quota, a compression option, erasure-coded overwrites or its application) no longer has the value its StoragePool or StorageObjectGateway froze", "compare ceph osd pool ls detail with the StoragePool and set that setting back to its frozen value outside Bootwright, then apply again; or take back what this context owns with bootwright destroy --context <context>, then apply; Bootwright never rewrites a completed pool setting")
	out["sso-unverified"] = failure("lifecycle.unknown", "an oauth2-proxy daemon of a Ceph management gateway runs with a configuration that skips the identity provider's certificate verification, so Bootwright did not enable the dashboard's single sign-on", "the native error names the host, the daemon and where the skip was found; take back what this context owns with bootwright destroy --context <context>, then select a Ceph image whose cephadm renders oauth2-proxy without that skip with bootwright context update, or remove the oauth2-proxy service, its spec.ceph.serviceBindings entry and spec.enable_auth on the mgmt-gateway, then apply")
	for _, reason := range []string{"request", "artifact", "ownership", "evidence", "material", "packages", "package-artifact", "package-drift", "package-journal", "package-license", "package-limit", "package-timeout", "package-platform", "package-host", "call-home-drift", "config-drift", "partial-cluster", "not-ready", "not-removed", "cluster-unavailable", "device", "foreign-cluster", "native-command", "native-timeout", "native-output", "unknown", "transport", "remote-refusal", "adapter-exception"} {
		out[reason] = failure("lifecycle.unknown", "the Ceph adapter could not prove its "+reason+" boundary", "inspect the retained operation evidence before continuing")
	}
	return out
}
