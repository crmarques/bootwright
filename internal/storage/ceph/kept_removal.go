package ceph

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/canonicaljson"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const (
	KeptTeardownVersion    = "ceph-kept-teardown-evidence-v1"
	KeptImplementation     = "storage-ceph-kept-teardown-v1"
	KeptHostImplementation = "storage-ceph-host-kept-release-v1"
)

func KeptHostContentDigest() string {
	return reconciliation.ContentDigestOf(KeptHostImplementation, HostVersion, HostEvidenceVersion, "host-records-removed-packages-and-image-kept")
}

func KeptContentDigest() string {
	return reconciliation.ContentDigestOf(KeptImplementation, Version, KeptTeardownVersion, "managers-stopped-before-fsid-removal",
		"fsid-tagged-named-volume-zap", "positive-absence-per-host", "fsid-records-removed-last",
		"auth-and-pools-unused-before-manager-stop", "one-device-zap-per-call")
}

const (
	keptRetirement = "kept"
	keptReason     = "no step of the cluster that hands out access ever completed, which the removal plan froze and every invocation proves again, so no data client holds access to it"
)

type KeptTeardown struct {
	FSID    string             `json:"fsid"`
	Hosts   []KeptTeardownHost `json:"hosts"`
	Owner   string             `json:"owner"`
	Request string             `json:"request"`
	Version string             `json:"version"`
}

type KeptTeardownHost struct {
	Attempted bool   `json:"attempted"`
	Host      string `json:"host"`
	Removed   bool   `json:"removed"`
}

func keptBlock(block reconciliation.Block) bool {
	return block.Inverse != nil && len(block.Inverse.KeptHosts) != 0
}

func keptPhysicalRefusal() string {
	return "a physical Ceph host keeps what the cluster wrote to its disks, and no staged destroy takes a cluster back from one yet (B1885)"
}

func handedOut(r Request) string {
	switch {
	case r.Action == "cluster":
		return "its dashboard login"
	case r.Client != nil:
		return "the CephX key of client " + r.Client.Name
	case r.ObjectUser != nil:
		return "the S3 keys of object user " + r.ObjectUser.Name
	case r.ObjectBucket != nil:
		return "object bucket " + r.ObjectBucket.Name
	case r.NFSExport != nil:
		return "NFS export " + r.NFSExport.Name
	case r.NVMeOF != nil:
		return "an NVMe-oF subsystem"
	case r.Gateway != nil:
		return "an object gateway endpoint"
	case r.Service != nil:
		return "service " + r.Service.name()
	}
	return ""
}

func (Capability) KeptHostsRefusal(block reconciliation.Block, records lifecycle.RemovalRecords, _ bool) string {
	r, err := DecodeRequest(block.Request)
	if err != nil {
		return "its frozen request cannot be read"
	}
	if physicalHosts(r.Hosts) {
		return keptPhysicalRefusal()
	}
	access := handedOut(r)
	switch {
	case access == "" || records.State == reconciliation.BlockFailed || records.State == reconciliation.BlockPending:
		return ""
	case records.State == reconciliation.BlockDone:
		return "the step completed and handed out " + access + ", so a data client may hold access to the cluster; taking back a cluster that was used while its hosts stay waits for B491"
	}
	return "the step's outcome is " + string(records.State) + ", and resolving it may prove it completed and handed out " + access +
		", so it is taken back while its hosts stay only once bootwright apply resolved it failed"
}

func (Capability) KeptHostsContract() (string, string) {
	return KeptImplementation, KeptContentDigest()
}

func (HostCapability) KeptHostsContract() (string, string) {
	return KeptHostImplementation, KeptHostContentDigest()
}

func (Capability) OnlineKeptHosts(block reconciliation.Block) ([]string, error) {
	r, err := DecodeRequest(block.Request)
	if err != nil {
		return nil, err
	}
	return requestMachines(r), nil
}

func (c Capability) KeptHostsRemoval(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	removal, err := c.Removal(ctx, block)
	if err != nil {
		return removal, err
	}
	r, err := DecodeRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	if r.Action != "cluster" {
		removal.Description = "take back Ceph " + subjectOf(r, block.Object) + " with its cluster's teardown from its kept hosts"
		return removal, nil
	}
	removal.Description = "take the Ceph cluster " + r.Cluster + " back from its kept hosts, which stay running"
	removal.Impacts = []string{"stop and disable every manager of the cluster on each host before any host is cleaned"}
	for _, host := range r.Hosts {
		volumes := "no OSD volume"
		if paths := devicePaths(host); len(paths) != 0 {
			volumes = "the OSD volumes tagged with the cluster's FSID on " + strings.Join(paths, ", ")
		}
		removal.Impacts = append(removal.Impacts, "remove the cluster's FSID from Machine/"+host.Machine+": its daemons, data, logs and units, "+volumes+
			", the cluster-key line, the sshd drop-in, the registry credential and Bootwright's records of the cluster")
	}
	removal.Retains = []string{"the Ceph packages and the pulled image on each host, by design",
		"each host's firewall openings, which its own host firewall step keeps",
		"every disk, volume and file of another cluster or of none, which the teardown never touches"}
	return removal, nil
}

func devicePaths(host Host) []string {
	paths := make([]string, 0, len(host.Devices))
	for _, device := range host.Devices {
		paths = append(paths, device.Path)
	}
	return paths
}

func (c Capability) keptTeardown(ctx context.Context, e lifecycle.Execution, operation string) (KeptTeardown, json.RawMessage, error) {
	r, err := c.identity(ctx, e)
	if err != nil {
		return KeptTeardown{}, nil, err
	}
	if physicalHosts(r.Hosts) {
		return KeptTeardown{}, nil, physicalRemovalRefusal()
	}
	if r.Action != "cluster" {
		teardown := KeptTeardown{FSID: r.FSID, Hosts: []KeptTeardownHost{}, Owner: r.Owner, Request: e.Block.RequestDigest, Version: KeptTeardownVersion}
		data, err := json.Marshal(teardown)
		if err != nil {
			return KeptTeardown{}, nil, cephRequestInvalid.Fail("the Ceph kept-host teardown evidence cannot be encoded", "")
		}
		return teardown, data, nil
	}
	if r, err = c.prepare(ctx, e); err != nil {
		return KeptTeardown{}, nil, err
	}
	result, err := c.invoke(ctx, e, operation, r, map[string]string{"retirement": keptRetirement}, keptRefusals(r))
	if err != nil {
		return KeptTeardown{}, nil, err
	}
	teardown, err := decodeKeptTeardown(result.Evidence, r, e.Block.RequestDigest)
	if err != nil {
		return KeptTeardown{}, nil, err
	}
	if operation == "destroy" && (!result.Outcome.Settled() || slices.ContainsFunc(teardown.Hosts, func(host KeptTeardownHost) bool { return !host.Removed })) {
		return KeptTeardown{}, nil, cephUnproved.Fail("the Ceph cluster's teardown from its kept hosts did not prove every host clean", "")
	}
	return teardown, result.Evidence, nil
}

func (c Capability) destroyKept(ctx context.Context, e lifecycle.Execution) (lifecycle.Result, error) {
	_, evidence, err := c.keptTeardown(ctx, e, "destroy")
	if err != nil {
		return lifecycle.Result{Outcome: lifecycle.AttemptOutcome(err)}, err
	}
	return lifecycle.Result{Outcome: reconciliation.OutcomeChanged, Evidence: evidence}, nil
}

func (c Capability) observeKept(ctx context.Context, e lifecycle.Execution) (lifecycle.Observation, error) {
	teardown, evidence, err := c.keptTeardown(ctx, e, "observe")
	if err != nil {
		return lifecycle.Observation{Effect: reconciliation.EffectUnknown}, err
	}
	switch {
	case !slices.ContainsFunc(teardown.Hosts, func(host KeptTeardownHost) bool { return !host.Removed }):
		return lifecycle.Observation{Effect: reconciliation.EffectCompleted, Evidence: evidence}, nil
	case slices.ContainsFunc(teardown.Hosts, func(host KeptTeardownHost) bool { return host.Attempted }):
		return lifecycle.Observation{Effect: reconciliation.EffectPartial}, nil
	}
	return lifecycle.Observation{Effect: reconciliation.EffectNoEffect}, nil
}

func decodeKeptTeardown(data []byte, r Request, digest string) (KeptTeardown, error) {
	e, err := reconciliation.DecodeEvidence[KeptTeardown](data, 16*1024, "Ceph kept-host teardown")
	if err != nil {
		return KeptTeardown{}, evidenceFailure()
	}
	members, err := reconciliation.DecodeEvidence[map[string]json.RawMessage](data, 16*1024, "Ceph kept-host teardown members")
	if err != nil || !closedMembers(members, "fsid", "hosts", "owner", "request", "version") || canonicaljson.ProveObject(data) != nil {
		return KeptTeardown{}, evidenceFailure()
	}
	if e.Version != KeptTeardownVersion || e.Request != digest || e.Owner != r.Owner || e.FSID != r.FSID || len(e.Hosts) != len(r.Hosts) {
		return KeptTeardown{}, evidenceFailure()
	}
	for index, host := range e.Hosts {
		if host.Host != r.Hosts[index].Name {
			return KeptTeardown{}, evidenceFailure()
		}
	}
	return e, nil
}

func keptUsageRefusals(cluster string) map[string]error {
	const whole = "take back the whole context with bootwright destroy --context <context>, which removes the hosts too; the staged destroy stopped no manager and removed nothing"
	return map[string]error{
		"kept-foreign-client": cephBoundaryUnproved.Fail("Ceph cluster "+cluster+" holds a CephX entity that neither its frozen request nor Bootwright's bootstrap creates, such as a client created outside Bootwright, possibly through bootwright cluster rsh, so a data client may hold access and the cluster is not taken back while its hosts stay", whole),
		"kept-user-data":      cephBoundaryUnproved.Fail("a pool of Ceph cluster "+cluster+" other than .mgr holds objects or stored bytes, data written outside Bootwright, possibly through bootwright cluster rsh, so the cluster is not taken back while its hosts stay", whole),
		"kept-unread": cephBoundaryUnproved.Fail("Ceph cluster "+cluster+" did not answer ceph auth ls or ceph df on its bootstrap host, so whether it was used is unknown and the cluster is not taken back while its hosts stay",
			"start the cluster's monitor and manager on the bootstrap host and repeat the destroy, or "+whole),
	}
}

func keptRefusals(r Request) map[string]error {
	refusals := keptUsageRefusals(r.Cluster)
	maps.Copy(refusals, map[string]error{
		"transport": cephBoundaryUnproved.Fail("a kept host of Ceph cluster "+r.Cluster+" did not answer, so whether the cluster remains on it is unknown and nothing is taken as removed",
			"check that every host Machine of the cluster runs and answers SSH with its fleet key and pinned host key, then repeat the destroy"),
		"tiebreaker-foreign": cephBoundaryUnproved.Fail("a kept host of Ceph cluster "+r.Cluster+" holds Ceph state this cluster cannot prove its own: another cluster's directory or daemon, a volume of this cluster on a device the request does not name or beside another's volume, or a key line, drop-in or credential other than the recorded ones",
			"inspect the host as the native error names and remove what another tool placed there, then repeat the destroy; Bootwright never removes what it cannot prove its own"),
		"managers-running": cephBoundaryUnproved.Fail("a manager of Ceph cluster "+r.Cluster+" still runs or stays enabled on a kept host after the stop round, so no host's FSID removal started",
			"inspect the attempt's adapter output, then repeat the destroy; the stop round repeats first"),
	})
	return refusals
}

func (HostCapability) KeptHostsRefusal(block reconciliation.Block, _ lifecycle.RemovalRecords, _ bool) string {
	r, err := DecodeHostRequest(block.Request)
	if err != nil {
		return "its frozen request cannot be read"
	}
	if host, _ := r.target(); host.Physical {
		return keptPhysicalRefusal()
	}
	return ""
}

func (HostCapability) OnlineKeptHosts(block reconciliation.Block) ([]string, error) {
	r, err := DecodeHostRequest(block.Request)
	if err != nil {
		return nil, err
	}
	host, _ := r.target()
	return []string{host.Machine}, nil
}

func (c HostCapability) KeptHostsRemoval(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	removal, err := c.Removal(ctx, block)
	if err != nil {
		return removal, err
	}
	r, err := DecodeHostRequest(block.Request)
	if err != nil {
		return lifecycle.Removal{}, err
	}
	host, _ := r.target()
	removal.Description = "release Ceph host preparation " + r.Host + " of " + r.Cluster.Cluster + " on its kept Machine"
	removal.Impacts = []string{"remove Bootwright's host preparation records of the cluster from Machine/" + host.Machine}
	removal.Retains = []string{"the Ceph packages, the package records and the pulled image on Machine/" + host.Machine + ", by design"}
	return removal, nil
}

func (c HostCapability) releaseKept(ctx context.Context, e lifecycle.Execution, operation string) (HostEvidence, json.RawMessage, error) {
	r, err := c.prepare(ctx, e)
	if err != nil {
		return HostEvidence{}, nil, err
	}
	if host, _ := r.target(); host.Physical {
		return HostEvidence{}, nil, physicalRemovalRefusal()
	}
	result, err := c.run(ctx, e, operation, r, operation == "observe")
	if err != nil {
		return HostEvidence{}, nil, err
	}
	evidence, err := decodeHostEvidence(result.Evidence, e.Block.RequestDigest)
	if err != nil || !evidence.identifies(r) {
		return HostEvidence{}, nil, cephUnproved.Fail("the Ceph host release on its kept Machine did not prove the original host", "")
	}
	if operation == "destroy" && (!result.Outcome.Settled() || !retained(evidence, r)) {
		return HostEvidence{}, nil, cephUnproved.Fail("the Ceph host release on its kept Machine did not prove its records removed", "")
	}
	return evidence, result.Evidence, nil
}
