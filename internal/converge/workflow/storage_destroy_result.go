package workflow

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	safefs "github.com/crmarques/bootwright/internal/host/safefs"
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

const storageDestroyStatusProofValidated = "proof-validated"

const storageDestroyStatusEvidenceReleased = "evidence-released"

const storageDestroySkippedNodesAttr = "destroySkippedNodes"

const storageDestroyProofAttr = "destroyProof"

const storageDestroySkipUnreachableExtraVar = "bootwright_destroy_skip_unreachable"

const StorageDestroyReleaseManifestExtraVar = "bootwright_storage_destroy_release_manifest_path"

const StorageDestroyReleaseValidationExtraVar = "bootwright_storage_destroy_release_validation_path"

const StorageDestroyReleaseValidationFileName = "storage-destroy-release-validated"

var storageDestroyFSIDPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type StorageDestroyClusterResult struct {
	Name  string                     `json:"name"`
	FSID  string                     `json:"fsid"`
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

type StorageDestroyReleaseCluster struct {
	FSID  string            `json:"fsid"`
	Nodes map[string]string `json:"nodes"`
}

type StorageDestroyReleaseManifest struct {
	SchemaVersion int                                     `json:"schemaVersion"`
	Clusters      map[string]StorageDestroyReleaseCluster `json:"clusters"`
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

func writeStorageDestroyResult(path string, results map[string]StorageDestroyClusterResult) error {
	names := make([]string, 0, len(results))
	for name := range results {
		names = append(names, name)
	}
	sort.Strings(names)
	report := StorageDestroyResult{SchemaVersion: 1}
	for _, name := range names {
		report.Clusters = append(report.Clusters, results[name])
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode storage destroy result: %w", err)
	}
	if err := safefs.WriteFileEnsuringDir(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write storage destroy result: %w", err)
	}
	return nil
}

func WriteStorageDestroyReleaseManifest(path string, manifest StorageDestroyReleaseManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("encode storage destroy release manifest: %w", err)
	}
	if err := safefs.WriteFileEnsuringDir(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write storage destroy release manifest: %w", err)
	}
	return nil
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
			if cluster.FSID != "" && !storageDestroyFSIDPattern.MatchString(cluster.FSID) {
				return nil, fmt.Errorf("storage destroy attestation for %s carries invalid fsid %q", name, cluster.FSID)
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

func RecoverStorageDestroyResults(ownershipDir, contextName string, expected map[string][]string, expectedSeedHosts map[string]string) (map[string]StorageDestroyClusterResult, error) {
	targets := make(map[string]bool, len(expected))
	for name := range expected {
		targets[name] = true
	}
	records, err := storageDestroyOwnerRecords(ownershipDir, contextName, targets, expectedSeedHosts)
	if err != nil {
		return nil, err
	}
	recovered := map[string]StorageDestroyClusterResult{}
	for _, name := range sortedStorageDestroyExpectedKeys(expected) {
		record, ownerFound := records[name]
		status := record.Attributes[storageDestroyStatusAttr]
		if ownerFound && status != storageDestroyStatusProofValidated && status != storageDestroyStatusEvidenceReleased {
			continue
		}
		if !ownerFound {
			receipt, found, err := readStorageDestroyCompletionReceipt(ownershipDir, name)
			if err != nil {
				return nil, err
			}
			if !found {
				continue
			}
			if _, err := validateStorageDestroyCompletionReceiptIntrinsic(receipt, contextName, name); err != nil {
				return nil, err
			}
			if receipt.State == storageDestroyCompletionStateApplyStarted {
				continue
			}
			result, err := validateStorageDestroyCompletionReceipt(receipt, contextName, name, expectedSeedHosts[name], expected[name])
			if err != nil {
				return nil, err
			}
			recovered[name] = result
			continue
		}
		result, err := decodeStorageDestroyClusterProof(record.Attributes[storageDestroyProofAttr])
		if err != nil {
			return nil, fmt.Errorf("decode validated storage destroy proof for %s: %w", name, err)
		}
		validated, err := ValidateStorageDestroyResults([]StorageDestroyResult{{SchemaVersion: 1, Clusters: []StorageDestroyClusterResult{result}}}, map[string][]string{name: expected[name]}, false)
		if err != nil {
			return nil, fmt.Errorf("validate retained storage destroy proof for %s: %w", name, err)
		}
		if result.FSID != record.Attributes["fsid"] {
			return nil, fmt.Errorf("retained storage destroy proof for %s names fsid %s, controller owner names %s", name, result.FSID, record.Attributes["fsid"])
		}
		if status == storageDestroyStatusEvidenceReleased {
			_, found, err := matchingStorageDestroyCompletionReceipt(ownershipDir, contextName, name, expectedSeedHosts[name], result)
			if err != nil {
				return nil, err
			}
			if !found {
				return nil, fmt.Errorf("storage cluster %s says its host evidence was released but has no durable completion receipt", name)
			}
		}
		recovered[name] = validated[name]
	}
	return recovered, nil
}

func PrepareStorageDestroyOwnershipRelease(ownershipDir, contextName string, results map[string]StorageDestroyClusterResult, expectedSeedHosts map[string]string) (StorageDestroyReleaseManifest, error) {
	manifest := StorageDestroyReleaseManifest{SchemaVersion: 1, Clusters: map[string]StorageDestroyReleaseCluster{}}
	targets := storageDestroyResultTargets(results)
	records, err := storageDestroyOwnerRecords(ownershipDir, contextName, targets, expectedSeedHosts)
	if err != nil {
		return manifest, err
	}
	names := sortedStorageDestroyMapKeys(results)
	proofs := map[string]string{}
	completedReceipts := map[string]bool{}
	for _, name := range names {
		result := results[name]
		record, found := records[name]
		if expectedSeedHosts[name] == "" {
			return manifest, fmt.Errorf("storage destroy proof for %s has no selected seed host", name)
		}
		if len(result.SkippedNodes()) > 0 {
			if found && result.FSID != "" && result.FSID != record.Attributes["fsid"] {
				return manifest, fmt.Errorf("partial storage destroy proof for %s names fsid %s, controller owner names %s", name, result.FSID, record.Attributes["fsid"])
			}
			continue
		}
		if result.FSID != "" {
			if !found {
				receipt, receiptFound, err := matchingStorageDestroyCompletionReceipt(ownershipDir, contextName, name, expectedSeedHosts[name], result)
				if err != nil {
					return manifest, err
				}
				if !receiptFound {
					return manifest, fmt.Errorf("storage destroy proof for %s names fsid %s but no exact controller owner or completion receipt exists", name, result.FSID)
				}
				if storageDestroyReceiptHasRemoteCompletion(receipt.State) {
					completedReceipts[name] = true
				}
				continue
			}
			if result.FSID != record.Attributes["fsid"] {
				return manifest, fmt.Errorf("storage destroy proof for %s names fsid %s, controller owner names %s", name, result.FSID, record.Attributes["fsid"])
			}
		} else {
			if found {
				return manifest, fmt.Errorf("storage destroy proof for %s claims a clean no-op while an exact controller owner still exists", name)
			}
			receipt, receiptFound, err := readStorageDestroyCompletionReceipt(ownershipDir, name)
			if err != nil {
				return manifest, err
			}
			if receiptFound {
				if _, err := validateStorageDestroyCompletionReceiptIntrinsic(receipt, contextName, name); err != nil {
					return manifest, err
				}
				if storageDestroyReceiptCanReplayRelease(receipt.State) {
					if _, _, err := matchingStorageDestroyCompletionReceipt(ownershipDir, contextName, name, expectedSeedHosts[name], result); err != nil {
						return manifest, err
					}
					if storageDestroyReceiptHasRemoteCompletion(receipt.State) {
						completedReceipts[name] = true
					}
				}
			}
		}
		if result.FSID == "" {
			continue
		}
		proof, err := encodeStorageDestroyClusterProof(result)
		if err != nil {
			return manifest, fmt.Errorf("encode validated storage destroy proof for %s: %w", name, err)
		}
		proofs[name] = proof
		status := record.Attributes[storageDestroyStatusAttr]
		if status == storageDestroyStatusProofValidated || status == storageDestroyStatusEvidenceReleased {
			receipt, receiptFound, err := readStorageDestroyCompletionReceipt(ownershipDir, name)
			if err != nil {
				return manifest, err
			}
			if receiptFound {
				if _, err := validateStorageDestroyCompletionReceiptIntrinsic(receipt, contextName, name); err != nil {
					return manifest, err
				}
				if storageDestroyReceiptHasRemoteCompletion(receipt.State) {
					if _, _, err := matchingStorageDestroyCompletionReceipt(ownershipDir, contextName, name, expectedSeedHosts[name], result); err != nil {
						return manifest, err
					}
					completedReceipts[name] = true
				}
			}
			if status == storageDestroyStatusEvidenceReleased && (!receiptFound || !storageDestroyReceiptHasRemoteCompletion(receipt.State)) {
				return manifest, fmt.Errorf("storage cluster %s says its host evidence was released but has no durable completion receipt", name)
			}
		}
	}
	for _, name := range names {
		record, found := records[name]
		result := results[name]
		if len(result.SkippedNodes()) > 0 {
			if found {
				if err := savePartialStorageDestroyRecord(ownershipDir, record, result); err != nil {
					return manifest, err
				}
			}
			continue
		}
		if completedReceipts[name] {
			if found && record.Attributes[storageDestroyStatusAttr] == storageDestroyStatusProofValidated {
				if record.Attributes[storageDestroyProofAttr] != proofs[name] {
					return manifest, fmt.Errorf("storage cluster ownership record %s no longer carries the completed destroy proof", name)
				}
				record.Attributes[storageDestroyStatusAttr] = storageDestroyStatusEvidenceReleased
				record.UpdatedAt = time.Time{}
				if err := ownership.SaveResource(ownershipDir, record); err != nil {
					return manifest, fmt.Errorf("persist recovered storage evidence release for %s: %w", name, err)
				}
			}
			continue
		}
		if result.FSID != "" && found {
			proof := proofs[name]
			if record.Attributes == nil {
				record.Attributes = map[string]string{}
			}
			record.Attributes[storageDestroyStatusAttr] = storageDestroyStatusProofValidated
			record.Attributes[storageDestroyProofAttr] = proof
			delete(record.Attributes, storageDestroySkippedNodesAttr)
			record.UpdatedAt = time.Time{}
			if err := ownership.SaveResource(ownershipDir, record); err != nil {
				return manifest, fmt.Errorf("persist validated storage destroy proof for %s: %w", name, err)
			}
		}
		if result.FSID == "" {
			if err := writeStorageDestroyCompletionReceipt(ownershipDir, StorageDestroyCompletionReceipt{
				APIVersion: storageDestroyCompletionAPIVersion,
				State:      storageDestroyCompletionStateReleasePending,
				Context:    contextName,
				Cluster:    name,
				SeedHost:   expectedSeedHosts[name],
				Result:     result,
			}); err != nil {
				return manifest, err
			}
		}
		nodes := make(map[string]string, len(result.Nodes))
		for _, node := range result.Nodes {
			nodes[node.Name] = node.Host
		}
		manifest.Clusters[name] = StorageDestroyReleaseCluster{FSID: result.FSID, Nodes: nodes}
	}
	return manifest, nil
}

func MarkStorageDestroyOwnershipReleased(ownershipDir, contextName string, results map[string]StorageDestroyClusterResult, expectedSeedHosts map[string]string, manifest StorageDestroyReleaseManifest) error {
	if len(manifest.Clusters) == 0 {
		return nil
	}
	targets := make(map[string]bool, len(manifest.Clusters))
	for name := range manifest.Clusters {
		targets[name] = true
	}
	records, err := storageDestroyOwnerRecords(ownershipDir, contextName, targets, expectedSeedHosts)
	if err != nil {
		return err
	}
	names := sortedStorageDestroyReleaseKeys(manifest.Clusters)
	proofs := map[string]string{}
	for _, name := range names {
		result, found := results[name]
		if !found || len(result.SkippedNodes()) > 0 {
			return fmt.Errorf("storage release manifest for %s has no complete destroy proof", name)
		}
		if manifest.Clusters[name].FSID != result.FSID || !sameStorageDestroyNodes(manifest.Clusters[name].Nodes, result.Nodes) {
			return fmt.Errorf("storage release manifest for %s contradicts the validated destroy proof", name)
		}
		proof, err := encodeStorageDestroyClusterProof(result)
		if err != nil {
			return fmt.Errorf("encode released storage destroy proof for %s: %w", name, err)
		}
		if record, ownerFound := records[name]; ownerFound {
			if record.Attributes[storageDestroyProofAttr] != proof {
				return fmt.Errorf("storage cluster ownership record %s no longer carries the validated destroy proof", name)
			}
			status := record.Attributes[storageDestroyStatusAttr]
			if status != storageDestroyStatusProofValidated && status != storageDestroyStatusEvidenceReleased {
				return fmt.Errorf("storage cluster ownership record %s is not pending evidence release", name)
			}
		} else {
			receipt, receiptFound, err := matchingStorageDestroyCompletionReceipt(ownershipDir, contextName, name, expectedSeedHosts[name], result)
			if err != nil {
				return err
			}
			if !receiptFound || receipt.State != storageDestroyCompletionStateReleasePending {
				return fmt.Errorf("storage cluster %s has neither an exact controller owner nor a release-pending receipt", name)
			}
		}
		proofs[name] = proof
	}
	for _, name := range names {
		if err := writeStorageDestroyCompletionReceipt(ownershipDir, StorageDestroyCompletionReceipt{
			APIVersion: storageDestroyCompletionAPIVersion,
			State:      storageDestroyCompletionStateResetPending,
			Context:    contextName,
			Cluster:    name,
			SeedHost:   expectedSeedHosts[name],
			Result:     results[name],
		}); err != nil {
			return err
		}
	}
	for _, name := range names {
		record, found := records[name]
		if !found {
			continue
		}
		if record.Attributes[storageDestroyStatusAttr] == storageDestroyStatusEvidenceReleased {
			continue
		}
		record.Attributes[storageDestroyStatusAttr] = storageDestroyStatusEvidenceReleased
		record.Attributes[storageDestroyProofAttr] = proofs[name]
		record.UpdatedAt = time.Time{}
		if err := ownership.SaveResource(ownershipDir, record); err != nil {
			return fmt.Errorf("persist storage evidence release for %s: %w", name, err)
		}
	}
	return nil
}

func ResetStorageDestroyOwnershipProof(ownershipDir, contextName string, results map[string]StorageDestroyClusterResult, expectedSeedHosts map[string]string, manifest StorageDestroyReleaseManifest) error {
	targets := make(map[string]bool, len(manifest.Clusters))
	for name := range manifest.Clusters {
		targets[name] = true
	}
	records, err := storageDestroyOwnerRecords(ownershipDir, contextName, targets, expectedSeedHosts)
	if err != nil {
		return err
	}
	for _, name := range sortedStorageDestroyReleaseKeys(manifest.Clusters) {
		record, found := records[name]
		if !found || record.Attributes[storageDestroyStatusAttr] != storageDestroyStatusProofValidated {
			continue
		}
		result, found := results[name]
		if !found {
			return fmt.Errorf("storage release manifest for %s has no validated destroy proof", name)
		}
		proof, err := encodeStorageDestroyClusterProof(result)
		if err != nil {
			return fmt.Errorf("encode storage destroy proof reset for %s: %w", name, err)
		}
		if record.Attributes[storageDestroyProofAttr] != proof {
			return fmt.Errorf("storage cluster ownership record %s no longer carries the validated destroy proof", name)
		}
		delete(record.Attributes, storageDestroyStatusAttr)
		delete(record.Attributes, storageDestroyProofAttr)
		delete(record.Attributes, storageDestroySkippedNodesAttr)
		record.UpdatedAt = time.Time{}
		if err := ownership.SaveResource(ownershipDir, record); err != nil {
			return fmt.Errorf("reset invalidated storage destroy proof for %s: %w", name, err)
		}
	}
	return nil
}

func reconcileStorageDestroyOwnership(ownershipDir, contextName string, results map[string]StorageDestroyClusterResult, expectedSeedHosts map[string]string, releaseCompleted bool) error {
	records, err := storageDestroyOwnerRecords(ownershipDir, contextName, storageDestroyResultTargets(results), expectedSeedHosts)
	if err != nil {
		return err
	}
	names := sortedStorageDestroyMapKeys(results)
	proofs := map[string]string{}
	for _, name := range names {
		record, found := records[name]
		result := results[name]
		if len(result.SkippedNodes()) > 0 {
			if result.FSID != "" {
				if found && result.FSID != record.Attributes["fsid"] {
					return fmt.Errorf("partial storage destroy proof for %s does not match an exact controller owner", name)
				}
			}
			continue
		}
		if !releaseCompleted {
			continue
		}
		if result.FSID == "" {
			if found {
				return fmt.Errorf("storage destroy proof for %s claims a clean no-op while an exact controller owner still exists", name)
			}
			receipt, receiptFound, err := matchingStorageDestroyCompletionReceipt(ownershipDir, contextName, name, expectedSeedHosts[name], result)
			if err != nil {
				return err
			}
			if !receiptFound || !storageDestroyReceiptHasRemoteCompletion(receipt.State) {
				return fmt.Errorf("storage cluster %s has a clean no-op proof but no durable completion receipt", name)
			}
			continue
		}
		proof, err := encodeStorageDestroyClusterProof(result)
		if err != nil {
			return fmt.Errorf("encode completed storage destroy proof for %s: %w", name, err)
		}
		proofs[name] = proof
		if found && (record.Attributes[storageDestroyStatusAttr] != storageDestroyStatusEvidenceReleased || record.Attributes[storageDestroyProofAttr] != proof) {
			return fmt.Errorf("storage cluster %s has terminal proof but its host ownership evidence was not fully released", name)
		}
		receipt, receiptFound, err := matchingStorageDestroyCompletionReceipt(ownershipDir, contextName, name, expectedSeedHosts[name], result)
		if err != nil {
			return err
		}
		if !receiptFound || !storageDestroyReceiptHasRemoteCompletion(receipt.State) {
			return fmt.Errorf("storage cluster %s has terminal proof but no durable completion receipt", name)
		}
	}
	if releaseCompleted {
		for _, name := range names {
			result := results[name]
			if len(result.SkippedNodes()) > 0 {
				continue
			}
			receipt, receiptFound, err := matchingStorageDestroyCompletionReceipt(ownershipDir, contextName, name, expectedSeedHosts[name], result)
			if err != nil {
				return err
			}
			if !receiptFound {
				return fmt.Errorf("storage cluster %s has no durable completion receipt", name)
			}
			if receipt.State == storageDestroyCompletionStateCompleted {
				continue
			}
			receipt.State = storageDestroyCompletionStateCompleted
			if err := writeStorageDestroyCompletionReceipt(ownershipDir, receipt); err != nil {
				return err
			}
		}
	}
	for _, name := range names {
		record, found := records[name]
		result := results[name]
		if len(result.SkippedNodes()) > 0 {
			if found {
				if err := savePartialStorageDestroyRecord(ownershipDir, record, result); err != nil {
					return err
				}
			}
			continue
		}
		if !releaseCompleted || result.FSID == "" || !found {
			continue
		}
		if record.Attributes[storageDestroyProofAttr] != proofs[name] {
			return fmt.Errorf("storage cluster %s controller proof changed before owner release", name)
		}
		path, err := ownership.ResourcePath(ownershipDir, record)
		if err != nil {
			return fmt.Errorf("resolve completed storage ownership record %s: %w", name, err)
		}
		if err := safefs.RemoveFileDurable(path); err != nil {
			return fmt.Errorf("remove completed storage ownership record %s: %w", name, err)
		}
	}
	return nil
}

func storageDestroyOwnerRecords(ownershipDir, contextName string, targets map[string]bool, expectedSeedHosts map[string]string) (map[string]ownership.ResourceRecord, error) {
	records, err := ownership.LoadContext(ownershipDir, contextName)
	if err != nil {
		return nil, fmt.Errorf("load ownership records for storage destroy completion: %w", err)
	}
	byName := map[string]ownership.ResourceRecord{}
	for _, record := range records {
		if record.Kind != string(ownership.KindStorageCluster) || record.IsReference() {
			continue
		}
		if !targets[record.Name] {
			continue
		}
		expectedSeedHost := expectedSeedHosts[record.Name]
		if record.Owner != ownership.Owner || record.EffectiveRole() != ownership.RoleOwner || record.APIVersion != "bootwright.io/ownership/v1alpha1" || record.Context != contextName || record.Cluster != record.Name || expectedSeedHost == "" || record.Host != expectedSeedHost || record.Attributes["seedHost"] != expectedSeedHost || !storageDestroyFSIDPattern.MatchString(record.Attributes["fsid"]) {
			return nil, fmt.Errorf("storage cluster ownership record %s contradicts the controller owner contract", record.Name)
		}
		switch status := record.Attributes[storageDestroyStatusAttr]; status {
		case "", storageDestroyStatusPartial, storageDestroyStatusProofValidated, storageDestroyStatusEvidenceReleased:
		default:
			return nil, fmt.Errorf("storage cluster ownership record %s has unknown destroy status %q", record.Name, status)
		}
		if _, found := byName[record.Name]; found {
			return nil, fmt.Errorf("storage cluster ownership record %s has more than one controller owner", record.Name)
		}
		byName[record.Name] = record
	}
	return byName, nil
}

func savePartialStorageDestroyRecord(ownershipDir string, record ownership.ResourceRecord, result StorageDestroyClusterResult) error {
	joinedSkippedNodes := strings.Join(result.SkippedNodes(), ",")
	if record.Attributes[storageDestroyStatusAttr] == storageDestroyStatusPartial && record.Attributes[storageDestroySkippedNodesAttr] == joinedSkippedNodes && record.Attributes[storageDestroyProofAttr] == "" {
		return nil
	}
	if record.Attributes == nil {
		record.Attributes = map[string]string{}
	}
	record.Attributes[storageDestroyStatusAttr] = storageDestroyStatusPartial
	record.Attributes[storageDestroySkippedNodesAttr] = joinedSkippedNodes
	delete(record.Attributes, storageDestroyProofAttr)
	record.UpdatedAt = time.Time{}
	if err := ownership.SaveResource(ownershipDir, record); err != nil {
		return fmt.Errorf("persist partial-destroy marker for storage cluster %s: %w", record.Name, err)
	}
	return nil
}

func encodeStorageDestroyClusterProof(result StorageDestroyClusterResult) (string, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func decodeStorageDestroyClusterProof(raw string) (StorageDestroyClusterResult, error) {
	if strings.TrimSpace(raw) == "" {
		return StorageDestroyClusterResult{}, errors.New("proof is empty")
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var result StorageDestroyClusterResult
	if err := decoder.Decode(&result); err != nil {
		return StorageDestroyClusterResult{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return StorageDestroyClusterResult{}, err
	}
	return result, nil
}

func sameStorageDestroyNodes(actual map[string]string, nodes []StorageDestroyNodeResult) bool {
	if len(actual) != len(nodes) {
		return false
	}
	for _, node := range nodes {
		if actual[node.Name] != node.Host {
			return false
		}
	}
	return true
}

func sortedStorageDestroyExpectedKeys(expected map[string][]string) []string {
	names := make([]string, 0, len(expected))
	for name := range expected {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func storageDestroyResultTargets(results map[string]StorageDestroyClusterResult) map[string]bool {
	out := make(map[string]bool, len(results))
	for name := range results {
		out[name] = true
	}
	return out
}

func sortedStorageDestroyMapKeys(results map[string]StorageDestroyClusterResult) []string {
	names := make([]string, 0, len(results))
	for name := range results {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedStorageDestroyReleaseKeys(results map[string]StorageDestroyReleaseCluster) []string {
	names := make([]string, 0, len(results))
	for name := range results {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedStorageDestroyValues(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
