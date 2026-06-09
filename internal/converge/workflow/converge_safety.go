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
	ConvergeSafetyMatch   ConvergeSafetyClassification = "match"
	ConvergeSafetyDrift   ConvergeSafetyClassification = "drift"
	ConvergeSafetyForeign ConvergeSafetyClassification = "foreign"
	// ConvergeSafetyUnknown marks a record written by a probe-less mutating task
	// (most provider-service and infra-component config tasks): the task completed
	// but no external probe classified observed state. It is never returned by
	// ClassifyConvergeSafety; state-check reports it as recorded-but-not-classified.
	ConvergeSafetyUnknown ConvergeSafetyClassification = "unknown"
	ConvergeSafetyMissing ConvergeSafetyClassification = "missing"
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

// RemoveConvergeSafetyRecord deletes the convergence-safety record for a resource
// if present (a no-op when absent). Destroy calls it so a torn-down object
// reclassifies as missing: a later greenfield apply recreates it instead of
// refusing, and apply --continue creates it instead of skipping a gone object as
// already-applied.
func RemoveConvergeSafetyRecord(runsDir, resourceID string) error {
	if strings.TrimSpace(runsDir) == "" || strings.TrimSpace(resourceID) == "" {
		return nil
	}
	path := ConvergeSafetyRecordPath(runsDir, resourceID)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove converge safety record: %w", err)
	}
	return nil
}

// RemoveApplyTaskConvergeSafety removes the convergence-safety record for one apply
// task, identified the same way MarkApplyTaskConvergeSafety wrote it.
func RemoveApplyTaskConvergeSafety(runsDir string, task ApplyTask) error {
	if strings.TrimSpace(task.Entry.ID) == "" {
		return nil
	}
	return RemoveConvergeSafetyRecord(runsDir, applyTaskSafetyResourceID(task))
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
		APIVersion   string          `json:"apiVersion"`
		TaskID       string          `json:"taskID"`
		TaskKind     string          `json:"taskKind"`
		Cluster      string          `json:"cluster,omitempty"`
		ClusterKind  string          `json:"clusterKind,omitempty"`
		Node         string          `json:"node,omitempty"`
		Host         string          `json:"host,omitempty"`
		ResourceKeys []string        `json:"resourceKeys,omitempty"`
		Playbook     string          `json:"playbook,omitempty"`
		Limit        string          `json:"limit,omitempty"`
		State        *v1alpha1.State `json:"state,omitempty"`
		FabricVars   any             `json:"fabricVars,omitempty"`
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
	}
	// Fabric tasks hash a host-scoped projection of the rendered vars so an
	// unrelated fleet edit does not flip the infrastructure root to drift. Every
	// other task keeps hashing the full State; its payload stays byte-identical to
	// the prior definition (a non-nil State pointer marshals the same as the value),
	// so recorded hashes remain valid.
	if task.DesiredHashVars != nil {
		payload.FabricVars = task.DesiredHashVars
	} else {
		state := task.State
		payload.State = &state
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

// applyTaskSafetyResourceID identifies the converge-safety record for a task.
// It keys on the TASK identity, not on task.Entry.ResourceKeys: ResourceKeys are
// the scheduler's mutual-exclusion lock keys and are deliberately SHARED across
// distinct tasks that mutate the same host/storage resource (e.g. a provider
// task, a machine-infra prepare task, and a finalize task all carry
// "host:<host>:mutating"; storageinfra and storage both carry "storage:<name>").
// Keying the record by the shared lock made several tasks write one file with
// different per-task desired hashes, so the last writer won and state-check
// misreported the others as drift on a clean apply.
func applyTaskSafetyResourceID(task ApplyTask) string {
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
