package ceph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/substrate"
)

const PhysicalRetirementImplementation = "storage-ceph-physical-retirement-v1"

type PhysicalRetirementHost struct {
	Devices   []Device `json:"devices"`
	EraseWWNs []string `json:"eraseWWNs"`
	Machine   string   `json:"machine"`
	Name      string   `json:"name"`
}

type PhysicalRetirementRequest struct {
	Cluster string                   `json:"cluster"`
	Hosts   []PhysicalRetirementHost `json:"hosts"`
	Version string                   `json:"version"`
}

type PhysicalErasure struct {
	MACs    []string
	Targets []Device
}

type PhysicalEraseReader func(reconciliation.Block, lifecycle.BlockEvidence, string) (PhysicalErasure, bool, error)
type PhysicalUninstalledReader func([]lifecycle.BlockEvidence, []lifecycle.RemovalEvidence, string) (bool, error)

type PhysicalRetirementCapability struct {
	erasure     PhysicalEraseReader
	proofs      DependencyProofs
	uninstalled PhysicalUninstalledReader
}

func NewPhysicalRetirement() PhysicalRetirementCapability { return PhysicalRetirementCapability{} }

func (c PhysicalRetirementCapability) WithProofs(proofs DependencyProofs) PhysicalRetirementCapability {
	c.proofs = proofs
	return c
}

func (c PhysicalRetirementCapability) WithErasureProofs(reader PhysicalEraseReader) PhysicalRetirementCapability {
	c.erasure = reader
	return c
}

func (c PhysicalRetirementCapability) WithUninstalledProofs(reader PhysicalUninstalledReader) PhysicalRetirementCapability {
	c.uninstalled = reader
	return c
}

func PhysicalRetirementBlockID(cluster string) string {
	return reconciliation.BlockIdentity("ceph-erase-", cluster)
}

func PhysicalRetirementContentDigest() string {
	digest := sha256.Sum256([]byte(PhysicalRetirementImplementation + ":original-host-and-completed-erase-proof-v1"))
	return hex.EncodeToString(digest[:])
}

func (r PhysicalRetirementRequest) Canonical() ([]byte, error) {
	return reconciliation.Freeze(r, "physical Ceph retirement")
}

func DecodePhysicalRetirementRequest(data []byte) (PhysicalRetirementRequest, error) {
	r, err := reconciliation.ThawVersion[PhysicalRetirementRequest](data, "physical Ceph retirement", PhysicalRetirementImplementation)
	if err != nil {
		return r, err
	}
	previous := ""
	if r.Cluster == "" || len(r.Hosts) == 0 || len(r.Hosts) > maxClusterHosts {
		return r, physicalPolicyFailure()
	}
	for _, host := range r.Hosts {
		if host.Machine <= previous || host.Name == "" || len(host.Devices) > maxPhysicalDevices || len(host.EraseWWNs) > maxEraseTargets || len(host.EraseWWNs) > 0 && !strictlySorted(host.EraseWWNs) {
			return r, physicalPolicyFailure()
		}
		previous = host.Machine
		paths := []string{}
		for _, device := range host.Devices {
			if !physicalWWN.MatchString(device.Path) || device.Path != "/dev/disk/by-id/wwn-"+device.WWN || device.Serial != "" || device.SizeBytes != 0 || device.Rotational {
				return r, physicalPolicyFailure()
			}
			paths = append(paths, device.Path)
		}
		if len(paths) > 0 && !strictlySorted(paths) {
			return r, physicalPolicyFailure()
		}
	}
	return r, nil
}

const PhysicalRetirementFamily = "storage-physical-retirement"

func (PhysicalRetirementCapability) PhysicalRetirement(catalog api.Catalog, machineName string) ([]reconciliation.ObjectRef, error) {
	references := []reconciliation.ObjectRef{}
	machine, found := catalog.Find(api.Machine, machineName)
	provider, _ := catalog.Find(api.InfraProvider, machine.Spec().Get("substrate", "providerRef").Text())
	if !found || !substrate.RealizesPhysicalNICs(provider) {
		return references, nil
	}
	for _, cluster := range catalog.OfKind(api.CephCluster) {
		for _, node := range cluster.Spec().Get("topology", "nodes").Items() {
			if node.Get("machineRef").Text() == machineName {
				references = append(references, reconciliation.ObjectRef{Kind: string(api.CephCluster), Object: cluster.Name(), Family: PhysicalRetirementFamily})
				break
			}
		}
	}
	return references, nil
}

func physicalRetirementRequest(catalog api.Catalog, cluster Request) PhysicalRetirementRequest {
	r := PhysicalRetirementRequest{Version: PhysicalRetirementImplementation, Cluster: cluster.Cluster, Hosts: []PhysicalRetirementHost{}}
	for _, host := range cluster.Hosts {
		if !host.Physical {
			continue
		}
		machine, _ := catalog.Find(api.Machine, host.Machine)
		entry := PhysicalRetirementHost{Name: host.Name, Machine: host.Machine, Devices: slices.Clone(host.Devices), EraseWWNs: []string{}}
		policy := machine.Spec().Get("hardware", "eraseOnRemoval")
		if policy.Get("method").Text() == "logicalOverwrite" {
			for _, target := range policy.Get("targets").Items() {
				entry.EraseWWNs = append(entry.EraseWWNs, target.Get("wwn").Text())
			}
		}
		slices.Sort(entry.EraseWWNs)
		entry.EraseWWNs = slices.Compact(entry.EraseWWNs)
		r.Hosts = append(r.Hosts, entry)
	}
	slices.SortFunc(r.Hosts, func(a, b PhysicalRetirementHost) int { return strings.Compare(a.Machine, b.Machine) })
	return r
}

func physicalPolicyFailure() error {
	return failure("lifecycle.state", "the physical Ceph retirement policy is incomplete or inconsistent", "restore the original apply and removal records")
}

func (r PhysicalRetirementRequest) authorized() error {
	for _, host := range r.Hosts {
		if len(host.EraseWWNs) == 0 {
			return physicalRemovalRefusal()
		}
		for _, device := range host.Devices {
			if !slices.Contains(host.EraseWWNs, device.WWN) {
				return failure("lifecycle.unsupported", "physical Ceph retirement requires explicit erasure of every selected data, DB and WAL disk", "include the root disk and every selected Ceph WWN in each Machine's original eraseOnRemoval policy")
			}
		}
	}
	return nil
}

func (c PhysicalRetirementCapability) Plan(ctx context.Context, input lifecycle.PlanInput) (lifecycle.CapabilityPlan, error) {
	plan := lifecycle.CapabilityPlan{Definitions: []reconciliation.BlockDefinition{}}
	if err := ctx.Err(); err != nil {
		return plan, err
	}
	if input.State == nil {
		return plan, physicalPolicyFailure()
	}
	catalog := input.State.Effective()
	for _, object := range catalog.OfKind(api.CephCluster) {
		cluster, err := baseRequest(catalog, object, input.Context.Name)
		if err != nil {
			return plan, err
		}
		if !physicalHosts(cluster.Hosts) {
			continue
		}
		request := physicalRetirementRequest(catalog, cluster)
		claims := []reconciliation.ObjectRef{}
		for _, host := range request.Hosts {
			claims = append(claims, reconciliation.ObjectRef{Kind: string(api.Machine), Object: host.Machine, Family: substrate.MachineFamily})
		}
		canonical, err := request.Canonical()
		if err != nil {
			return plan, err
		}
		definition := reconciliation.BlockDefinition{ID: PhysicalRetirementBlockID(cluster.Cluster), Description: "retain the physical Ceph retirement policy of " + cluster.Cluster,
			Stage: reconciliation.StageMachines, Requires: claims, Kind: string(api.CephCluster), Object: cluster.Cluster, Family: PhysicalRetirementFamily, Implementation: PhysicalRetirementImplementation,
			ContentDigest: PhysicalRetirementContentDigest(), Request: canonical}
		if input.Verb == reconciliation.Destroy {
			definition.Description = "verify physical Ceph retirement before releasing " + cluster.Cluster
		}
		plan.Definitions = append(plan.Definitions, definition)
	}
	return plan, nil
}

func (PhysicalRetirementCapability) Removal(ctx context.Context, block reconciliation.Block) (lifecycle.Removal, error) {
	request, err := DecodePhysicalRetirementRequest(block.Request)
	return lifecycle.Removal{Description: "verify physical Ceph retirement before releasing " + request.Cluster}, err
}

func (PhysicalRetirementCapability) Quiescent(ctx context.Context, _ lifecycle.Probe) (lifecycle.Quiescence, error) {
	if err := ctx.Err(); err != nil {
		return lifecycle.Quiescence{State: lifecycle.Unproved}, err
	}
	return lifecycle.Quiescence{State: lifecycle.Quiescent, Reason: "physical retirement verification owns only a durable policy"}, nil
}
