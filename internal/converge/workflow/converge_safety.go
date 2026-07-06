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
	safefs "github.com/crmarques/bootwright/internal/host/safefs"
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
	APIVersion   string `json:"apiVersion"`
	ResourceID   string `json:"resourceID"`
	ResourceKind string `json:"resourceKind"`
	TaskID       string `json:"taskID"`
	TaskKind     string `json:"taskKind"`
	DesiredHash  string `json:"desiredHash"`
	// StructuralHash, when set, hashes only the destructive-identity portion of the
	// desired state (for a StorageCluster: everything except the OSD device
	// selection). When the full DesiredHash drifts but StructuralHash still matches,
	// the drift is reconcilable in place (a device add) rather than a destructive
	// rebuild, so apply proceeds and --override does not wipe. Empty on records
	// written before this field existed and on tasks that set no structural
	// projection — both fall back to treating any drift as structural (fail-safe).
	StructuralHash string                      `json:"structuralHash,omitempty"`
	Owner          ConvergeSafetyOwnerIdentity `json:"owner"`
	Observation    ConvergeSafetyObservation   `json:"observation"`
	Status         ConvergeSafetyStatus        `json:"status"`
	RunID          string                      `json:"runID,omitempty"`
	ResourceKeys   []string                    `json:"resourceKeys,omitempty"`
	UpdatedAt      time.Time                   `json:"updatedAt"`
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

// loadConvergeSafetyRecordLenient is the read-only state-check variant of
// LoadConvergeSafetyRecord: a per-file read or decode failure is returned as a
// warning naming the file (and the record reported not-found) so the caller
// skips just that record, instead of propagating an error that would brick the
// whole read-only state-check report on a single corrupt file. LoadConvergeSafetyRecord
// itself stays strict — apply's preflight gate must still fail loud on a corrupt
// record rather than silently treat it as absent.
func loadConvergeSafetyRecordLenient(runsDir, resourceID string) (ConvergeSafetyRecord, bool, string, error) {
	path := ConvergeSafetyRecordPath(runsDir, resourceID)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return ConvergeSafetyRecord{}, false, "", nil
	}
	if err != nil {
		return ConvergeSafetyRecord{}, false, fmt.Sprintf("read converge safety record %s: %v", path, err), nil
	}
	var record ConvergeSafetyRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return ConvergeSafetyRecord{}, false, fmt.Sprintf("decode converge safety record %s: %v", path, err), nil
	}
	return record, true, "", nil
}

func SaveConvergeSafetyRecord(runsDir string, record ConvergeSafetyRecord) error {
	path := ConvergeSafetyRecordPath(runsDir, record.ResourceID)
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode converge safety record: %w", err)
	}
	data = append(data, '\n')
	if err := safefs.WriteFileEnsuringDir(path, data, 0o600); err != nil {
		return fmt.Errorf("write converge safety record: %w", err)
	}
	return nil
}

func ConvergeSafetyRecordPath(runsDir, resourceID string) string {
	return filepath.Join(runsDir, "safety", convergeSafetyRecordFileName(resourceID))
}

// HasConvergeSafetyRecords reports whether the context has any convergence-safety
// record on disk — i.e. Bootwright has applied at least one object. It is the cheap
// gate the status next-step spine uses to start suggesting `diff` (the
// read-only drift verb) only once there is a recorded apply to compare against; it
// never reads or classifies the records.
func HasConvergeSafetyRecords(runsDir string) bool {
	if strings.TrimSpace(runsDir) == "" {
		return false
	}
	entries, err := os.ReadDir(filepath.Join(runsDir, "safety"))
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			return true
		}
	}
	return false
}

// RemoveConvergeSafetyRecord deletes the convergence-safety record for a resource
// if present (a no-op when absent). Destroy calls it so a torn-down object
// reclassifies as missing: a later apply creates it instead of skipping a gone
// object as already-applied, and apply --expect-new no longer refuses it.
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
	structuralHash, err := ApplyTaskStructuralHash(task)
	if err != nil {
		return err
	}
	resourceID := applyTaskSafetyResourceID(task)
	record := ConvergeSafetyRecord{
		APIVersion:     ConvergeSafetyAPIVersion,
		ResourceID:     resourceID,
		ResourceKind:   task.Entry.Kind,
		TaskID:         task.Entry.ID,
		TaskKind:       task.Entry.Kind,
		DesiredHash:    desiredHash,
		StructuralHash: structuralHash,
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

// ApplyTaskStructuralHash hashes the task's structural desired-state projection —
// the destructive-identity subset that, when unchanged, makes a DesiredHash drift
// reconcilable in place rather than a rebuild. Returns "" when the task sets no
// structural projection (every non-storage task today), so such tasks never
// classify as reconcilable drift.
func ApplyTaskStructuralHash(task ApplyTask) (string, error) {
	if task.StructuralHashVars == nil {
		return "", nil
	}
	payload := struct {
		APIVersion     string `json:"apiVersion"`
		TaskID         string `json:"taskID"`
		TaskKind       string `json:"taskKind"`
		StructuralVars any    `json:"structuralVars"`
	}{
		APIVersion:     v1alpha1.APIVersion,
		TaskID:         task.Entry.ID,
		TaskKind:       task.Entry.Kind,
		StructuralVars: task.StructuralHashVars,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode apply task structural hash input: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// IsReconcilableDrift reports whether a drifted record's drift is reconcilable in
// place: the full desired hash changed but the structural hash is unchanged (a
// StorageCluster OSD-device add, not a cluster rebuild). An empty structural hash
// on either side — a task with no structural projection, or a record written
// before the field existed — is never reconcilable, so the drift falls back to
// structural (fail-safe: apply still refuses, --override still rebuilds).
func IsReconcilableDrift(record ConvergeSafetyRecord, desiredHash, structuralHash string) bool {
	if record.DesiredHash == desiredHash {
		return false
	}
	if strings.TrimSpace(structuralHash) == "" || strings.TrimSpace(record.StructuralHash) == "" {
		return false
	}
	return record.StructuralHash == structuralHash
}

// ClassifyConvergeSafety classifies a recorded object against the desired hash by
// comparing desired-vs-RECORDED-desired only — it does not read live cluster state.
// The foreign arm fires when a record's Manager is not this run's owner; today every
// bootwright-written record carries Manager=ConvergeSafetyOwner, so foreign is a
// fail-safe for a future non-bootwright writer, not a live out-of-band-drift detector.
// Drift injected outside bootwright (a `ceph` pool resize, an `oc edit`) does not
// change the recorded desired hash and so is invisible here by design; live
// divergence is surfaced by the per-role Ansible reconcile gates and by
// `bootwright diff --live`, not by this preflight classification.
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
