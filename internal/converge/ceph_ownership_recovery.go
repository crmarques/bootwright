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

type CephOwnershipRecoveryConflictError struct {
	Cluster  string
	SeedHost string
	Detail   string
}

func (e *CephOwnershipRecoveryConflictError) Error() string {
	return fmt.Sprintf("--recover-ceph-ownership cannot recover StorageCluster %q: existing controller ownership evidence conflicts with declared seed host %q (%s)", e.Cluster, e.SeedHost, e.Detail)
}

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

func ValidateDestroyCephOwnershipRecovery(state v1alpha1.State, storageWorkNames []string, ownershipDir, contextName string, records []ownership.ResourceRecord, confirmed map[string]string) error {
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
		record, exists, loadErr := ownership.LoadOwnerResource(ownershipDir, string(ownership.KindStorageCluster), name)
		if loadErr != nil {
			return &CephOwnershipRecoveryConflictError{Cluster: name, SeedHost: seedHost, Detail: loadErr.Error()}
		}
		if exists {
			if conflict := conflictingCephOwnershipRecoveryRecord(record, contextName, name, seedHost, confirmed[name]); conflict != "" {
				return &CephOwnershipRecoveryConflictError{Cluster: name, SeedHost: seedHost, Detail: conflict}
			}
		}
		if conflict := conflictingCephOwnershipRecoveryRecords(records, contextName, name, seedHost, confirmed[name]); conflict != "" {
			return &CephOwnershipRecoveryConflictError{Cluster: name, SeedHost: seedHost, Detail: conflict}
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

func conflictingCephOwnershipRecoveryRecords(records []ownership.ResourceRecord, contextName, clusterName, seedHost, confirmedFSID string) string {
	for _, record := range records {
		if record.Kind != string(ownership.KindStorageCluster) || record.Name != clusterName {
			continue
		}
		if conflict := conflictingCephOwnershipRecoveryRecord(record, contextName, clusterName, seedHost, confirmedFSID); conflict != "" {
			return conflict
		}
	}
	return ""
}

func conflictingCephOwnershipRecoveryRecord(record ownership.ResourceRecord, contextName, clusterName, seedHost, confirmedFSID string) string {
	var conflicts []string
	checks := []struct {
		field string
		got   string
		want  string
	}{
		{field: "apiVersion", got: record.APIVersion, want: "bootwright.io/ownership/v1alpha1"},
		{field: "kind", got: record.Kind, want: string(ownership.KindStorageCluster)},
		{field: "name", got: record.Name, want: clusterName},
		{field: "owner", got: record.Owner, want: ownership.Owner},
		{field: "role", got: record.EffectiveRole(), want: ownership.RoleOwner},
		{field: "context", got: record.Context, want: contextName},
		{field: "cluster", got: record.Cluster, want: clusterName},
		{field: "host", got: record.Host, want: seedHost},
		{field: "attributes.seedHost", got: record.Attributes["seedHost"], want: seedHost},
	}
	for _, check := range checks {
		if check.got != check.want {
			conflicts = append(conflicts, fmt.Sprintf("%s=%q, want %q", check.field, check.got, check.want))
		}
	}
	fsid := strings.TrimSpace(record.Attributes["fsid"])
	if fsid != "" {
		switch {
		case !cephFSIDPattern.MatchString(fsid):
			conflicts = append(conflicts, fmt.Sprintf("attributes.fsid=%q is not a UUID", fsid))
		case !strings.EqualFold(fsid, confirmedFSID):
			conflicts = append(conflicts, fmt.Sprintf("attributes.fsid=%q, want confirmed fsid %q", fsid, confirmedFSID))
		}
	}
	return strings.Join(conflicts, "; ")
}
