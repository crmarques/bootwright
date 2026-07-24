package converge

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/render"
)

const DestroyCephOwnershipRecoveryExtraVar = "bootwright_ceph_destroy_confirmed_fsids"

var cephFSIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func ParseDestroyCephOwnershipRecovery(value string) (map[string]string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	confirmed := map[string]string{}
	for _, entry := range strings.Split(value, ",") {
		name, fsid, ok := strings.Cut(strings.TrimSpace(entry), "=")
		name = strings.TrimSpace(name)
		fsid = strings.ToLower(strings.TrimSpace(fsid))
		if !ok || name == "" || fsid == "" {
			return nil, fmt.Errorf("--recover-ceph-ownership must be a comma-separated <StorageCluster>=<fsid> mapping")
		}
		if !cephFSIDPattern.MatchString(fsid) {
			return nil, fmt.Errorf("--recover-ceph-ownership fsid for StorageCluster %q must be a UUID, got %q", name, fsid)
		}
		if _, exists := confirmed[name]; exists {
			return nil, fmt.Errorf("--recover-ceph-ownership repeats StorageCluster %q", name)
		}
		confirmed[name] = fsid
	}
	return confirmed, nil
}

func ValidateDestroyCephOwnershipRecovery(state v1alpha1.State, storageWorkNames []string, records []ownership.ResourceRecord, confirmed map[string]string) error {
	if len(confirmed) == 0 {
		return nil
	}
	selected := map[string]bool{}
	for _, name := range storageWorkNames {
		selected[name] = true
	}
	clusters := map[string]v1alpha1.StorageCluster{}
	for _, cluster := range state.StorageClusters {
		if !v1alpha1.StorageClusterManaged(cluster) || cluster.Spec.Ceph == nil {
			continue
		}
		if storageWorkNames != nil && !selected[cluster.Metadata.Name] {
			continue
		}
		clusters[cluster.Metadata.Name] = cluster
	}
	for name := range confirmed {
		cluster, ok := clusters[name]
		if !ok {
			return fmt.Errorf("--recover-ceph-ownership names StorageCluster %q, which is not a selected managed Ceph cluster", name)
		}
		seedHost := render.StorageSeedHostName(cluster)
		if !hasCephOwnershipRecoveryRecord(records, name, seedHost) {
			return fmt.Errorf("--recover-ceph-ownership cannot authorize StorageCluster %q: no Bootwright owner record for declared seed host %q exists in this context; restore the ownership record or tear the cluster down manually", name, seedHost)
		}
	}
	return nil
}

func ApplyDestroyCephOwnershipRecoveryExtraVar(plan *WorkflowPlan, confirmed map[string]string) error {
	if len(confirmed) == 0 {
		return nil
	}
	data, err := json.Marshal(map[string]map[string]string{DestroyCephOwnershipRecoveryExtraVar: confirmed})
	if err != nil {
		return fmt.Errorf("encode Ceph ownership recovery: %w", err)
	}
	plan.ExtraVarPairs = append(plan.ExtraVarPairs, string(data))
	return nil
}

func hasCephOwnershipRecoveryRecord(records []ownership.ResourceRecord, clusterName, seedHost string) bool {
	for _, record := range records {
		if record.Kind != string(ownership.KindStorageCluster) ||
			record.Name != clusterName ||
			record.Owner != ownership.Owner ||
			record.IsReference() ||
			record.Cluster != clusterName ||
			record.Host != seedHost ||
			record.Attributes["seedHost"] != seedHost {
			continue
		}
		return true
	}
	return false
}
