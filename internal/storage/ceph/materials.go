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
	e.Material = bound
	return e, files, cleanup, nil
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
	nativeCalls, waits := len(r.Hosts)+2, 0
	if operation == "observe" && r.Action == "cluster" {
		nativeCalls++
	}
	if operation == "apply" {
		nativeCalls = 2*len(r.Hosts) + 3
		if r.Action == "cluster" {
			nativeCalls, waits = 7*len(r.Hosts)+6, bootstrapCall+2*readinessWait
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

func adapterRefusals() map[string]error {
	out := map[string]error{"unsupported-release": unsupportedFailure(), "runtime-profile": failure("lifecycle.unknown", "the Ceph host runtime profile is missing or contradicts the frozen image", "apply again so each ceph-host block re-proves its runtime profile before the cluster block continues"), "device-identity": failure("lifecycle.unknown", "the OSD device identities are missing or contradict the host preparation", "apply again so each ceph-host block re-derives its device identities"), "cache-unproved": failure("lifecycle.unknown", "the published cephadm cache is not the exact authenticated payload", "inspect the host's /var/lib/ceph cache before continuing; a foreign cephadm binary is never executed"), "dashboard-identity": failure("lifecycle.unknown", "the Ceph dashboard user admin or its retained password is not the one this operation created", "inspect the dashboard's users; an admin user without this operation's attempt record is never adopted, and a lost retained password is never regenerated"), "dashboard-manager": failure("lifecycle.unknown", "a Ceph MGR's container or dashboard port no longer matches the deployment this operation recorded", "inspect `ceph orch ps --daemon_type mgr`; Bootwright never adopts or repairs an MGR container it did not record, so take back what it owns with `bootwright destroy --context <context>` before applying again"), "dashboard-drift": failure("lifecycle.unknown", "the Ceph dashboard user admin no longer matches the password this context's custody holds", "log in with the custodied password that bootwright cluster info --secrets --name <cluster> reveals, or set the admin password back to it; Bootwright never resets it"), "dashboard-policy": failure("lifecycle.unknown", "the Ceph dashboard refused the generated administrator password under its password policy", "inspect the dashboard's password policy settings; Bootwright never retries with a new password"), "dashboard-verify": failure("lifecycle.unsupported", "the bootstrap host cannot verify the dashboard password hash", "use the pinned managed OS, whose Python reads bcrypt hashes; the check is never skipped"), "pool-identity": failure("lifecycle.unknown", "the Ceph pool's identity is not the one this operation created", "inspect the pool; a same-name pool without this operation's creation record is never adopted"), "client-quiescence-unqualified": failure("lifecycle.unsupported", "Ceph client quiescence has not been qualified", "prove all consumers stopped before qualifying online removal"), "native-option-undeclared": failure("lifecycle.unsupported", "the selected host cephadm package does not declare a bootstrap option Bootwright passes", "the apply froze this input, so take back what it owns with bootwright destroy --context <context>, select a cephadm package version whose release qualification covers this bootstrap with bootwright context update, then apply; the native error names the missing option and the selected package version"), "call-home-unqualified": failure("lifecycle.unsupported", "IBM Call Home intent has no qualified path on the selected Ceph build", "the apply froze this input, so take back what it owns with bootwright destroy --context <context>, correct ceph.ibm.callHome for the selected build with bootwright context update, then apply; see .agents/knowledge/version-specializations.md")}
	out["foreign-pool"] = failure("lifecycle.unknown", "a Ceph pool of the StoragePool's name exists that this operation did not create", "rename the StoragePool, or remove the foreign pool outside Bootwright; a pool without this operation's creation record is never adopted")
	out["foreign-profile"] = failure("lifecycle.unknown", "an erasure-code profile named after the StoragePool exists that this operation did not create", "rename the StoragePool, or remove the foreign profile outside Bootwright; Bootwright never overrides a profile with --force")
	out["foreign-rule"] = failure("lifecycle.unknown", "a CRUSH rule of the frozen name exists that this operation did not create, or it is not the rule this operation created", "rename the StoragePlacementPolicy's ruleName or the erasure-coded StoragePool, or remove the foreign rule outside Bootwright")
	out["crush-capacity"] = failure("lifecycle.unknown", "fewer OSD hosts under root default carry the frozen device class than the protection width needs", "add OSD hosts whose OSDs carry that device class, or change crushDeviceClass, then apply again")
	out["foreign-rbd-image"] = failure("lifecycle.unknown", "an RBD image of the StorageRBDImage's name exists in its pool that this operation did not create", "rename the StorageRBDImage, or remove or rename the foreign image outside Bootwright; an image without this operation's creation record is never adopted")
	out["rbd-image-identity"] = failure("lifecycle.unknown", "the RBD image is not the one this operation created, is gone, or no longer matches its declared size, data pool or features", "inspect it with rbd info and rbd trash ls; restore the StorageRBDImage's sizeBytes, dataPoolRef and features to the values it was created with, since Bootwright never recreates, resizes, moves or re-features an image it created")
	out["pg-budget"] = failure("lifecycle.unknown", "the monitor refused the pool: placement groups per OSD on its CRUSH rule would exceed mon_max_pg_per_osd", "add OSDs, lower pgNumMin or targetSizeRatio, or declare fewer pools on that CRUSH rule, then apply again")
	for _, reason := range []string{"request", "artifact", "ownership", "evidence", "material", "packages", "package-artifact", "package-drift", "package-journal", "package-license", "package-limit", "package-timeout", "package-platform", "package-host", "call-home-drift", "config-drift", "partial-cluster", "not-ready", "not-removed", "cluster-unavailable", "device", "foreign-cluster", "native-command", "native-timeout", "native-output", "unknown", "transport", "remote-refusal", "adapter-exception"} {
		out[reason] = failure("lifecycle.unknown", "the Ceph adapter could not prove its "+reason+" boundary", "inspect the retained operation evidence before continuing")
	}
	return out
}
