package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	safefs "github.com/crmarques/bootwright/internal/runtime/fs"
)

const (
	ConvergeSafetyAPIVersion = "bootwright.io/converge-safety/v1"
	ConvergeSafetyOwner      = "bootwright"
)

type ConvergeSafetyClassification string

const (
	ConvergeSafetyMatch       ConvergeSafetyClassification = "match"
	ConvergeSafetyDrift       ConvergeSafetyClassification = "drift"
	ConvergeSafetyForeign     ConvergeSafetyClassification = "foreign"
	ConvergeSafetyUnknown     ConvergeSafetyClassification = "unknown"
	ConvergeSafetyMissing     ConvergeSafetyClassification = "missing"
	ConvergeSafetyDestructive ConvergeSafetyClassification = "destructive"
)

type ConvergeSafetyStatus string

const (
	ConvergeSafetyStatusCreated    ConvergeSafetyStatus = "created"
	ConvergeSafetyStatusReconciled ConvergeSafetyStatus = "reconciled"
	ConvergeSafetyStatusSkipped    ConvergeSafetyStatus = "skipped"
)

type ConvergeSafetyOwnerIdentity struct {
	Manager string `json:"manager"`
	Context string `json:"context,omitempty"`
}

type ConvergeSafetyObservation struct {
	Classification ConvergeSafetyClassification `json:"classification"`
	ObservedHash   string                       `json:"observedHash,omitempty"`
	Reason         string                       `json:"reason,omitempty"`
}

type ConvergeSafetyRecord struct {
	APIVersion   string                      `json:"apiVersion"`
	ResourceID   string                      `json:"resourceID"`
	ResourceKind string                      `json:"resourceKind"`
	TaskID       string                      `json:"taskID"`
	TaskKind     string                      `json:"taskKind"`
	DesiredHash  string                      `json:"desiredHash"`
	Owner        ConvergeSafetyOwnerIdentity `json:"owner"`
	Observation  ConvergeSafetyObservation   `json:"observation"`
	Status       ConvergeSafetyStatus        `json:"status"`
	RunID        string                      `json:"runID,omitempty"`
	ResourceKeys []string                    `json:"resourceKeys,omitempty"`
	UpdatedAt    time.Time                   `json:"updatedAt"`
}

func LoadConvergeSafetyRecord(runsDir, resourceID string) (ConvergeSafetyRecord, bool, error) {
	path := ConvergeSafetyRecordPath(runsDir, resourceID)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ConvergeSafetyRecord{}, false, nil
	}
	if err != nil {
		return ConvergeSafetyRecord{}, false, fmt.Errorf("read converge safety record: %w", err)
	}
	var record ConvergeSafetyRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return ConvergeSafetyRecord{}, true, fmt.Errorf("decode converge safety record %s: %w", path, err)
	}
	return record, true, nil
}

func SaveConvergeSafetyRecord(runsDir string, record ConvergeSafetyRecord) error {
	path := ConvergeSafetyRecordPath(runsDir, record.ResourceID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create converge safety record directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("chmod converge safety record directory: %w", err)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode converge safety record: %w", err)
	}
	data = append(data, '\n')
	if err := safefs.AtomicWriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write converge safety record: %w", err)
	}
	return nil
}

func ConvergeSafetyRecordPath(runsDir, resourceID string) string {
	return filepath.Join(runsDir, "safety", convergeSafetyRecordFileName(resourceID))
}

func MarkApplyTaskConvergeSafety(runsDir, contextName, runID string, task ApplyTask, status ConvergeSafetyStatus, now time.Time) error {
	if strings.TrimSpace(runsDir) == "" || strings.TrimSpace(task.Entry.ID) == "" {
		return nil
	}
	desiredHash, err := ApplyTaskDesiredHash(task)
	if err != nil {
		return err
	}
	resourceID := applyTaskSafetyResourceID(task)
	record := ConvergeSafetyRecord{
		APIVersion:   ConvergeSafetyAPIVersion,
		ResourceID:   resourceID,
		ResourceKind: task.Entry.Kind,
		TaskID:       task.Entry.ID,
		TaskKind:     task.Entry.Kind,
		DesiredHash:  desiredHash,
		Owner: ConvergeSafetyOwnerIdentity{
			Manager: ConvergeSafetyOwner,
			Context: effectiveContextName(contextName),
		},
		Observation: ConvergeSafetyObservation{
			Classification: ConvergeSafetyUnknown,
			Reason:         "task completed through provider-specific workflow; external probe classification is not available",
		},
		Status:       status,
		RunID:        runID,
		ResourceKeys: append([]string(nil), task.Entry.ResourceKeys...),
		UpdatedAt:    now.UTC(),
	}
	return SaveConvergeSafetyRecord(runsDir, record)
}

func ApplyTaskDesiredHash(task ApplyTask) (string, error) {
	payload := struct {
		APIVersion   string         `json:"apiVersion"`
		TaskID       string         `json:"taskID"`
		TaskKind     string         `json:"taskKind"`
		Cluster      string         `json:"cluster,omitempty"`
		ClusterKind  string         `json:"clusterKind,omitempty"`
		Node         string         `json:"node,omitempty"`
		Host         string         `json:"host,omitempty"`
		ResourceKeys []string       `json:"resourceKeys,omitempty"`
		Playbook     string         `json:"playbook,omitempty"`
		Limit        string         `json:"limit,omitempty"`
		State        v1alpha1.State `json:"state"`
	}{
		APIVersion:   v1alpha1.APIVersion,
		TaskID:       task.Entry.ID,
		TaskKind:     task.Entry.Kind,
		Cluster:      task.Entry.Cluster,
		ClusterKind:  task.Entry.ClusterKind,
		Node:         task.Entry.Node,
		Host:         task.Entry.Host,
		ResourceKeys: append([]string(nil), task.Entry.ResourceKeys...),
		Playbook:     task.Playbook,
		Limit:        task.Limit,
		State:        task.State,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode apply task safety hash input: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func ClassifyConvergeSafety(record ConvergeSafetyRecord, desiredHash, ownerManager string) ConvergeSafetyClassification {
	if strings.TrimSpace(record.ResourceID) == "" {
		return ConvergeSafetyMissing
	}
	if record.Owner.Manager != ownerManager {
		return ConvergeSafetyForeign
	}
	if record.DesiredHash == desiredHash {
		return ConvergeSafetyMatch
	}
	return ConvergeSafetyDrift
}

func applyTaskSafetyResourceID(task ApplyTask) string {
	if len(task.Entry.ResourceKeys) > 0 {
		keys := append([]string(nil), task.Entry.ResourceKeys...)
		return strings.Join(keys, ",")
	}
	if task.Entry.Kind != "" && task.Entry.ID != "" {
		return task.Entry.Kind + "/" + task.Entry.ID
	}
	return task.Entry.ID
}

var convergeSafetySafeName = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

func convergeSafetyRecordFileName(resourceID string) string {
	clean := strings.Trim(convergeSafetySafeName.ReplaceAllString(resourceID, "-"), "-")
	if clean == "" {
		clean = "resource"
	}
	if len(clean) > 80 {
		clean = clean[:80]
	}
	sum := sha256.Sum256([]byte(resourceID))
	return clean + "-" + hex.EncodeToString(sum[:])[:12] + ".json"
}
