package workflow

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/render"
)

const StorageDestroyResultFileName = "storage-destroy-result.json"

const storageDestroyProof = "ceph-lvm-quiet-v2"

const storageDestroyScanScope = "all-node-pvs"

const storageDestroyOutcomeCompleted = "completed"

const storageDestroyOutcomeSkipped = "skipped"

const storageDestroyAbsenceSSHUnreachable = "ssh-unreachable"

const storageDestroyAbsenceConnectionLost = "connection-lost"

const storageDestroyStatusAttr = "destroyStatus"

const storageDestroyStatusPartial = "partial"

const storageDestroySkippedNodesAttr = "destroySkippedNodes"

const storageDestroySkipUnreachableExtraVar = "bootwright_destroy_skip_unreachable"

type StorageDestroyClusterResult struct {
	Name  string                     `json:"name"`
	Nodes []StorageDestroyNodeResult `json:"nodes"`
}

type StorageDestroyNodeResult struct {
	Name           string `json:"name"`
	Host           string `json:"host"`
	Outcome        string `json:"outcome"`
	ProofVersion   string `json:"proofVersion"`
	ScanScope      string `json:"scanScope"`
	ScannedRows    *int   `json:"scannedRows"`
	OwnedSurvivors *int   `json:"ownedSurvivors"`
	ScanDigest     string `json:"scanDigest"`
	LVMScanRC      *int   `json:"lvmScanRC"`
	CompletionRC   *int   `json:"completionRC"`
	AbsenceClass   string `json:"absenceClass"`
	Reason         string `json:"reason"`
}

type StorageDestroyResult struct {
	SchemaVersion int                           `json:"schemaVersion"`
	Clusters      []StorageDestroyClusterResult `json:"clusters"`
}

func StorageDestroyExpectedNodes(state v1alpha1.State, storageNames []string) map[string][]string {
	selected := map[string]bool{}
	for _, name := range storageNames {
		selected[name] = true
	}
	out := map[string][]string{}
	for _, cluster := range state.StorageClusters {
		name := cluster.Metadata.Name
		if storageNames != nil && !selected[name] {
			continue
		}
		if !v1alpha1.StorageClusterManaged(cluster) || cluster.Spec.Ceph == nil {
			continue
		}
		out[name] = nil
		for _, node := range cluster.Spec.Ceph.Topology.Nodes {
			out[name] = append(out[name], strings.TrimSpace(node.MachineRef.Name))
		}
		sort.Strings(out[name])
	}
	return out
}

func StorageDestroyExpectedSeedHosts(state v1alpha1.State, storageNames []string) map[string]string {
	selected := map[string]bool{}
	for _, name := range storageNames {
		selected[name] = true
	}
	out := map[string]string{}
	for _, cluster := range state.StorageClusters {
		name := cluster.Metadata.Name
		if storageNames != nil && !selected[name] {
			continue
		}
		if !v1alpha1.StorageClusterManaged(cluster) || cluster.Spec.Ceph == nil {
			continue
		}
		out[name] = render.StorageSeedHostName(cluster)
	}
	return out
}

func StorageDestroyExpectedNodesForLedger(state v1alpha1.State, ledger RunLedger) map[string][]string {
	return StorageDestroyExpectedNodes(state, storageDestroyLedgerClusterNames(ledger))
}

func StorageDestroyExpectedSeedHostsForLedger(state v1alpha1.State, ledger RunLedger) map[string]string {
	return StorageDestroyExpectedSeedHosts(state, storageDestroyLedgerClusterNames(ledger))
}

func storageDestroyLedgerClusterNames(ledger RunLedger) []string {
	names := []string{}
	for _, task := range ledger.Tasks {
		if task.Kind != DestroyTaskKindStorageCluster {
			continue
		}
		names = append(names, DestroyTaskClusterKeys(task)...)
	}
	return names
}

func (result StorageDestroyClusterResult) SkippedNodes() []string {
	var out []string
	for _, node := range result.Nodes {
		if node.Outcome == storageDestroyOutcomeSkipped {
			out = append(out, node.Name)
		}
	}
	return sortedStorageDestroyValues(out)
}

func (result StorageDestroyClusterResult) SkippedReasons() []string {
	var out []string
	for _, node := range result.Nodes {
		if node.Outcome == storageDestroyOutcomeSkipped {
			out = append(out, node.Reason)
		}
	}
	return sortedStorageDestroyValues(out)
}

func ReadStorageDestroyResult(path string) (StorageDestroyResult, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return StorageDestroyResult{}, false, nil
	}
	if err != nil {
		return StorageDestroyResult{}, false, fmt.Errorf("read storage destroy result: %w", err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var result StorageDestroyResult
	if err := decoder.Decode(&result); err != nil {
		return StorageDestroyResult{}, false, fmt.Errorf("decode storage destroy result: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return StorageDestroyResult{}, false, fmt.Errorf("decode storage destroy result: %w", err)
	}
	return result, true, nil
}

func ValidateStorageDestroyResults(reports []StorageDestroyResult, expected map[string][]string, allowSkipped bool) (map[string]StorageDestroyClusterResult, error) {
	if len(expected) == 0 {
		return map[string]StorageDestroyClusterResult{}, nil
	}
	if len(reports) == 0 {
		return nil, errors.New("storage teardown produced no completion attestation")
	}
	results := map[string]StorageDestroyClusterResult{}
	for _, report := range reports {
		if report.SchemaVersion != 1 {
			return nil, fmt.Errorf("storage destroy attestation schemaVersion = %d, want 1", report.SchemaVersion)
		}
		if len(report.Clusters) == 0 {
			return nil, errors.New("storage destroy attestation names no clusters")
		}
		for _, cluster := range report.Clusters {
			name := strings.TrimSpace(cluster.Name)
			if name == "" {
				return nil, errors.New("storage destroy attestation carries an empty cluster name")
			}
			if name != cluster.Name {
				return nil, fmt.Errorf("storage destroy attestation cluster name %q is not canonical", cluster.Name)
			}
			if _, found := expected[name]; !found {
				return nil, fmt.Errorf("storage destroy attestation names unexpected cluster %s", name)
			}
			if _, found := results[name]; found {
				return nil, fmt.Errorf("storage destroy attestation names cluster %s more than once", name)
			}
			if err := validateStorageDestroyClusterResult(cluster, expected[name], allowSkipped); err != nil {
				return nil, fmt.Errorf("storage destroy attestation for %s: %w", name, err)
			}
			results[name] = cluster
		}
	}
	for name := range expected {
		if _, found := results[name]; !found {
			return nil, fmt.Errorf("storage destroy attestation is missing cluster %s", name)
		}
	}
	return results, nil
}

func validateStorageDestroyClusterResult(result StorageDestroyClusterResult, expected []string, allowSkipped bool) error {
	expectedSet, err := storageDestroyStringSet("controller expectedNodes", expected)
	if err != nil {
		return err
	}
	accounted := map[string]bool{}
	hosts := map[string]bool{}
	for _, node := range result.Nodes {
		name := strings.TrimSpace(node.Name)
		if name == "" {
			return errors.New("nodes contains an empty name")
		}
		if name != node.Name {
			return fmt.Errorf("node name %q is not canonical", node.Name)
		}
		if accounted[name] {
			return fmt.Errorf("nodes contains duplicate %s", name)
		}
		accounted[name] = true
		host := strings.TrimSpace(node.Host)
		if host == "" {
			return fmt.Errorf("node %s carries an empty host", name)
		}
		if host != node.Host {
			return fmt.Errorf("node %s host %q is not canonical", name, node.Host)
		}
		if hosts[host] {
			return fmt.Errorf("nodes contains duplicate host %s", host)
		}
		hosts[host] = true
		if want := render.StorageNodeHostName(result.Name, name); host != want {
			return fmt.Errorf("node %s host = %q, want %q", name, host, want)
		}
		if err := validateStorageDestroyNodeResult(node, allowSkipped); err != nil {
			return fmt.Errorf("node %s: %w", name, err)
		}
	}
	return requireSameStorageDestroySet("nodes", accounted, expectedSet)
}

func validateStorageDestroyNodeResult(node StorageDestroyNodeResult, allowSkipped bool) error {
	switch node.Outcome {
	case storageDestroyOutcomeCompleted:
		if node.ProofVersion != storageDestroyProof {
			return fmt.Errorf("proofVersion = %q, want %q", node.ProofVersion, storageDestroyProof)
		}
		if node.ScanScope != storageDestroyScanScope {
			return fmt.Errorf("scanScope = %q, want %q", node.ScanScope, storageDestroyScanScope)
		}
		if node.ScannedRows == nil || *node.ScannedRows < 0 {
			return fmt.Errorf("scannedRows = %s, want a nonnegative count", storageDestroyRC(node.ScannedRows))
		}
		if node.OwnedSurvivors == nil || *node.OwnedSurvivors != 0 {
			return fmt.Errorf("ownedSurvivors = %s, want 0", storageDestroyRC(node.OwnedSurvivors))
		}
		if decoded, err := hex.DecodeString(node.ScanDigest); err != nil || len(decoded) != 32 {
			return fmt.Errorf("scanDigest = %q, want a SHA-256 digest", node.ScanDigest)
		}
		if node.LVMScanRC == nil || *node.LVMScanRC != 0 {
			return fmt.Errorf("lvmScanRC = %s, want 0", storageDestroyRC(node.LVMScanRC))
		}
		if node.CompletionRC == nil || *node.CompletionRC != 0 {
			return fmt.Errorf("completionRC = %s, want 0", storageDestroyRC(node.CompletionRC))
		}
		if node.AbsenceClass != "" || node.Reason != "" {
			return errors.New("completed outcome carries absence evidence")
		}
		return nil
	case storageDestroyOutcomeSkipped:
		if !allowSkipped {
			return errors.New("skipped outcome requires the unreachable-nodes authorization")
		}
		if node.AbsenceClass != storageDestroyAbsenceSSHUnreachable && node.AbsenceClass != storageDestroyAbsenceConnectionLost {
			return fmt.Errorf("absenceClass = %q, want a positive unreachable classification", node.AbsenceClass)
		}
		if strings.TrimSpace(node.Reason) == "" {
			return errors.New("skipped outcome carries no reason")
		}
		if node.ProofVersion != "" || node.ScanScope != "" || node.ScannedRows != nil || node.OwnedSurvivors != nil || node.ScanDigest != "" || node.LVMScanRC != nil || node.CompletionRC != nil {
			return errors.New("skipped outcome carries a completion claim")
		}
		return nil
	case "":
		return errors.New("outcome is empty")
	default:
		return fmt.Errorf("outcome = %q is not terminal", node.Outcome)
	}
}

func storageDestroyRC(value *int) string {
	if value == nil {
		return "<missing>"
	}
	return fmt.Sprint(*value)
}

func storageDestroyStringSet(label string, values []string) (map[string]bool, error) {
	out := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%s contains an empty value", label)
		}
		if out[value] {
			return nil, fmt.Errorf("%s contains duplicate %s", label, value)
		}
		out[value] = true
	}
	return out, nil
}

func requireSameStorageDestroySet(label string, actual, expected map[string]bool) error {
	var missing, unexpected []string
	for value := range expected {
		if !actual[value] {
			missing = append(missing, value)
		}
	}
	for value := range actual {
		if !expected[value] {
			unexpected = append(unexpected, value)
		}
	}
	sort.Strings(missing)
	sort.Strings(unexpected)
	if len(missing) == 0 && len(unexpected) == 0 {
		return nil
	}
	return fmt.Errorf("%s does not match the selected topology (missing: %s; unexpected: %s)", label, strings.Join(missing, ", "), strings.Join(unexpected, ", "))
}

func ReconcileStorageDestroyOwnership(ownershipDir, contextName string, results map[string]StorageDestroyClusterResult, expectedSeedHosts map[string]string) error {
	return reconcileStorageDestroyOwnership(ownershipDir, contextName, results, expectedSeedHosts, true)
}

func StampPartialStorageDestroyOwnership(ownershipDir, contextName string, results map[string]StorageDestroyClusterResult, expectedSeedHosts map[string]string) error {
	return reconcileStorageDestroyOwnership(ownershipDir, contextName, results, expectedSeedHosts, false)
}

func reconcileStorageDestroyOwnership(ownershipDir, contextName string, results map[string]StorageDestroyClusterResult, expectedSeedHosts map[string]string, releaseCompleted bool) error {
	records, err := ownership.LoadContext(ownershipDir, contextName)
	if err != nil {
		return fmt.Errorf("load ownership records for storage destroy completion: %w", err)
	}
	byName := map[string]ownership.ResourceRecord{}
	for _, record := range records {
		if record.Kind != string(ownership.KindStorageCluster) || record.IsReference() {
			continue
		}
		if _, targeted := results[record.Name]; !targeted {
			continue
		}
		expectedSeedHost := expectedSeedHosts[record.Name]
		if record.Owner != ownership.Owner || record.EffectiveRole() != ownership.RoleOwner || record.APIVersion != "bootwright.io/ownership/v1alpha1" || record.Context != contextName || record.Cluster != record.Name || expectedSeedHost == "" || record.Host != expectedSeedHost || record.Attributes["seedHost"] != expectedSeedHost {
			return fmt.Errorf("storage cluster ownership record %s contradicts the controller owner contract", record.Name)
		}
		if _, found := byName[record.Name]; found {
			return fmt.Errorf("storage cluster ownership record %s has more than one controller owner", record.Name)
		}
		byName[record.Name] = record
	}
	names := make([]string, 0, len(results))
	for name := range results {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		record, found := byName[name]
		if !found {
			continue
		}
		result := results[name]
		skippedNodes := result.SkippedNodes()
		if len(skippedNodes) > 0 {
			joinedSkippedNodes := strings.Join(skippedNodes, ",")
			if record.Attributes[storageDestroyStatusAttr] == storageDestroyStatusPartial && record.Attributes[storageDestroySkippedNodesAttr] == joinedSkippedNodes {
				continue
			}
			if record.Attributes == nil {
				record.Attributes = map[string]string{}
			}
			record.Attributes[storageDestroyStatusAttr] = storageDestroyStatusPartial
			record.Attributes[storageDestroySkippedNodesAttr] = joinedSkippedNodes
			record.UpdatedAt = time.Time{}
			if err := ownership.SaveResource(ownershipDir, record); err != nil {
				return fmt.Errorf("persist partial-destroy marker for storage cluster %s: %w", name, err)
			}
			continue
		}
		if !releaseCompleted {
			continue
		}
		path, err := ownership.ResourcePath(ownershipDir, record)
		if err != nil {
			return fmt.Errorf("resolve completed storage ownership record %s: %w", name, err)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove completed storage ownership record %s: %w", name, err)
		}
	}
	return nil
}

func sortedStorageDestroyValues(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
