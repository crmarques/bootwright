package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	safefs "github.com/crmarques/bootwright/internal/host/safefs"
	"github.com/crmarques/bootwright/internal/ownership"
)

const storageDestroyCompletionAPIVersion = "bootwright.io/storage-destroy-completion/v1"

const storageDestroyCompletionStateCompleted = "completed"

const storageDestroyCompletionStateResetPending = "reset-pending"

const storageDestroyCompletionStateReleasePending = "release-pending"

const storageDestroyCompletionStateApplyStarted = "apply-started"

type StorageDestroyCompletionReceipt struct {
	APIVersion string                      `json:"apiVersion"`
	State      string                      `json:"state"`
	Context    string                      `json:"context"`
	Cluster    string                      `json:"cluster"`
	SeedHost   string                      `json:"seedHost"`
	Result     StorageDestroyClusterResult `json:"result"`
}

func storageDestroyCompletionReceiptPath(ownershipDir, cluster string) string {
	return filepath.Join(ownershipDir, "storage-destroy-completion", convergeSafetyRecordFileName(cluster))
}

func writeStorageDestroyCompletionReceipt(ownershipDir string, receipt StorageDestroyCompletionReceipt) error {
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return fmt.Errorf("encode storage destroy completion receipt for %s: %w", receipt.Cluster, err)
	}
	if err := safefs.WriteFileEnsuringDir(storageDestroyCompletionReceiptPath(ownershipDir, receipt.Cluster), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write storage destroy completion receipt for %s: %w", receipt.Cluster, err)
	}
	return nil
}

func readStorageDestroyCompletionReceipt(ownershipDir, cluster string) (StorageDestroyCompletionReceipt, bool, error) {
	path := storageDestroyCompletionReceiptPath(ownershipDir, cluster)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return StorageDestroyCompletionReceipt{}, false, nil
	}
	if err != nil {
		return StorageDestroyCompletionReceipt{}, false, fmt.Errorf("read storage destroy completion receipt for %s: %w", cluster, err)
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	var receipt StorageDestroyCompletionReceipt
	if err := decoder.Decode(&receipt); err != nil {
		return StorageDestroyCompletionReceipt{}, false, fmt.Errorf("decode storage destroy completion receipt for %s: %w", cluster, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return StorageDestroyCompletionReceipt{}, false, fmt.Errorf("decode storage destroy completion receipt for %s: %w", cluster, err)
	}
	return receipt, true, nil
}

func BeginStorageApplyLifecycle(ownershipDir, contextName, cluster string) error {
	if strings.TrimSpace(cluster) == "" {
		return errors.New("storage apply task has no cluster identity")
	}
	receipt, found, err := readStorageDestroyCompletionReceipt(ownershipDir, cluster)
	if err != nil {
		return err
	}
	if found {
		if _, err := validateStorageDestroyCompletionReceiptIntrinsic(receipt, contextName, cluster); err != nil {
			return err
		}
		if receipt.State == storageDestroyCompletionStateResetPending || receipt.State == storageDestroyCompletionStateReleasePending {
			return fmt.Errorf("storage cluster %s has validated destroy evidence pending release or controller reset; rerun destroy with the receipt's original storage topology before apply", cluster)
		}
	}
	if err := prepareStorageDestroyOwnerBeforeApply(ownershipDir, contextName, cluster, receipt, found); err != nil {
		return err
	}
	if !found || receipt.State == storageDestroyCompletionStateApplyStarted {
		return nil
	}
	receipt.State = storageDestroyCompletionStateApplyStarted
	return writeStorageDestroyCompletionReceipt(ownershipDir, receipt)
}

func prepareStorageDestroyOwnerBeforeApply(ownershipDir, contextName, cluster string, receipt StorageDestroyCompletionReceipt, receiptFound bool) error {
	records, err := ownership.LoadContext(ownershipDir, contextName)
	if err != nil {
		return fmt.Errorf("load storage ownership before apply: %w", err)
	}
	receiptProof := ""
	if receiptFound {
		receiptProof, err = encodeStorageDestroyClusterProof(receipt.Result)
		if err != nil {
			return fmt.Errorf("encode completed storage destroy proof for %s before apply: %w", cluster, err)
		}
	}
	found := false
	for _, record := range records {
		if record.Kind != string(ownership.KindStorageCluster) || record.Name != cluster || record.IsReference() {
			continue
		}
		if found {
			return fmt.Errorf("storage cluster ownership record %s has more than one controller owner before apply", cluster)
		}
		found = true
		if record.Owner != ownership.Owner || record.EffectiveRole() != ownership.RoleOwner || record.APIVersion != "bootwright.io/ownership/v1alpha1" || record.Context != contextName || record.Cluster != cluster || record.Host == "" || record.Host != record.Attributes["seedHost"] || !storageDestroyFSIDPattern.MatchString(record.Attributes["fsid"]) {
			return fmt.Errorf("storage cluster ownership record %s contradicts the controller owner contract before apply", cluster)
		}
		status := record.Attributes[storageDestroyStatusAttr]
		switch status {
		case "", storageDestroyStatusPartial:
			continue
		case storageDestroyStatusEvidenceReleased:
			return fmt.Errorf("storage cluster %s has completed remote destroy evidence pending controller reset; rerun destroy with the same storage topology before apply", cluster)
		case storageDestroyStatusProofValidated:
			completionPending := receiptFound && storageDestroyReceiptHasRemoteCompletion(receipt.State) && record.Host == receipt.SeedHost && record.Attributes["fsid"] == receipt.Result.FSID && record.Attributes[storageDestroyProofAttr] == receiptProof
			if completionPending {
				return fmt.Errorf("storage cluster %s has completed remote destroy evidence pending controller reset; rerun destroy with the receipt's original storage topology before apply", cluster)
			}
			delete(record.Attributes, storageDestroyStatusAttr)
			delete(record.Attributes, storageDestroyProofAttr)
			delete(record.Attributes, storageDestroySkippedNodesAttr)
			record.UpdatedAt = time.Time{}
			if err := ownership.SaveResource(ownershipDir, record); err != nil {
				return fmt.Errorf("invalidate stale storage destroy proof for %s before apply: %w", cluster, err)
			}
		default:
			return fmt.Errorf("storage cluster ownership record %s has unknown destroy status %q before apply", cluster, status)
		}
	}
	return nil
}

func validateStorageDestroyCompletionReceiptIntrinsic(receipt StorageDestroyCompletionReceipt, contextName, cluster string) (StorageDestroyClusterResult, error) {
	if receipt.APIVersion != storageDestroyCompletionAPIVersion || receipt.Context != contextName || receipt.Cluster != cluster {
		return StorageDestroyClusterResult{}, fmt.Errorf("storage destroy completion receipt for %s contradicts the selected context, cluster, or seed", cluster)
	}
	if !storageDestroyReceiptCanReplayRelease(receipt.State) && receipt.State != storageDestroyCompletionStateApplyStarted {
		return StorageDestroyClusterResult{}, fmt.Errorf("storage destroy completion receipt for %s has invalid state %q", cluster, receipt.State)
	}
	if receipt.SeedHost != strings.TrimSpace(receipt.SeedHost) || receipt.SeedHost == "" {
		return StorageDestroyClusterResult{}, fmt.Errorf("storage destroy completion receipt for %s has invalid seed host %q", cluster, receipt.SeedHost)
	}
	expected := make([]string, 0, len(receipt.Result.Nodes))
	seedFound := false
	for _, node := range receipt.Result.Nodes {
		expected = append(expected, node.Name)
		seedFound = seedFound || node.Host == receipt.SeedHost
	}
	if len(expected) == 0 || !seedFound {
		return StorageDestroyClusterResult{}, fmt.Errorf("storage destroy completion receipt for %s does not bind its seed to a completed node", cluster)
	}
	validated, err := ValidateStorageDestroyResults(
		[]StorageDestroyResult{{SchemaVersion: 1, Clusters: []StorageDestroyClusterResult{receipt.Result}}},
		map[string][]string{cluster: expected},
		false,
	)
	if err != nil {
		return StorageDestroyClusterResult{}, fmt.Errorf("validate storage destroy completion receipt for %s: %w", cluster, err)
	}
	result := validated[cluster]
	if len(result.SkippedNodes()) > 0 {
		return StorageDestroyClusterResult{}, fmt.Errorf("storage destroy completion receipt for %s is not a complete proof", cluster)
	}
	return result, nil
}

func validateStorageDestroyCompletionReceipt(receipt StorageDestroyCompletionReceipt, contextName, cluster, seedHost string, expected []string) (StorageDestroyClusterResult, error) {
	if receipt.SeedHost != seedHost {
		return StorageDestroyClusterResult{}, fmt.Errorf("storage destroy completion receipt for %s contradicts the selected context, cluster, or seed", cluster)
	}
	if _, err := validateStorageDestroyCompletionReceiptIntrinsic(receipt, contextName, cluster); err != nil {
		return StorageDestroyClusterResult{}, err
	}
	validated, err := ValidateStorageDestroyResults(
		[]StorageDestroyResult{{SchemaVersion: 1, Clusters: []StorageDestroyClusterResult{receipt.Result}}},
		map[string][]string{cluster: expected},
		false,
	)
	if err != nil {
		return StorageDestroyClusterResult{}, fmt.Errorf("validate storage destroy completion receipt for %s: %w", cluster, err)
	}
	return validated[cluster], nil
}

func matchingStorageDestroyCompletionReceipt(ownershipDir, contextName, cluster, seedHost string, result StorageDestroyClusterResult) (StorageDestroyCompletionReceipt, bool, error) {
	receipt, found, err := readStorageDestroyCompletionReceipt(ownershipDir, cluster)
	if err != nil || !found {
		return receipt, found, err
	}
	if _, err := validateStorageDestroyCompletionReceiptIntrinsic(receipt, contextName, cluster); err != nil {
		return StorageDestroyCompletionReceipt{}, false, err
	}
	if !storageDestroyReceiptCanReplayRelease(receipt.State) {
		return StorageDestroyCompletionReceipt{}, false, fmt.Errorf("storage destroy completion receipt for %s is %s and cannot authorize destroy completion", cluster, receipt.State)
	}
	if receipt.SeedHost != seedHost {
		return StorageDestroyCompletionReceipt{}, false, fmt.Errorf("storage destroy completion receipt for %s contradicts the selected context, cluster, or seed", cluster)
	}
	want, err := encodeStorageDestroyClusterProof(result)
	if err != nil {
		return StorageDestroyCompletionReceipt{}, false, fmt.Errorf("encode storage destroy proof for receipt comparison on %s: %w", cluster, err)
	}
	got, err := encodeStorageDestroyClusterProof(receipt.Result)
	if err != nil {
		return StorageDestroyCompletionReceipt{}, false, fmt.Errorf("encode storage destroy completion receipt proof for %s: %w", cluster, err)
	}
	if got != want || len(result.SkippedNodes()) > 0 {
		return StorageDestroyCompletionReceipt{}, false, fmt.Errorf("storage destroy completion receipt for %s contradicts the validated destroy proof", cluster)
	}
	return receipt, true, nil
}

func storageDestroyReceiptHasRemoteCompletion(state string) bool {
	return state == storageDestroyCompletionStateResetPending || state == storageDestroyCompletionStateCompleted
}

func storageDestroyReceiptCanReplayRelease(state string) bool {
	return state == storageDestroyCompletionStateReleasePending || storageDestroyReceiptHasRemoteCompletion(state)
}
