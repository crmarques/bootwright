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
	"sync"
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
	APIVersion     string                      `json:"apiVersion"`
	ResourceID     string                      `json:"resourceID"`
	ResourceKind   string                      `json:"resourceKind"`
	TaskID         string                      `json:"taskID"`
	TaskKind       string                      `json:"taskKind"`
	DesiredHash    string                      `json:"desiredHash"`
	StructuralHash string                      `json:"structuralHash,omitempty"`
	HashSchema     int                         `json:"hashSchema,omitempty"`
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

func RemoveAllConvergeSafetyRecords(runsDir string) error {
	if strings.TrimSpace(runsDir) == "" {
		return nil
	}
	dir := filepath.Join(runsDir, "safety")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list converge safety records: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove converge safety record %s: %w", entry.Name(), err)
		}
	}
	return nil
}

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
		HashSchema:     ConvergeHashSchema,
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

type applyTaskHashCache struct {
	mu            sync.Mutex
	identity      string
	desired       string
	desiredErr    error
	desiredSet    bool
	structural    string
	structuralErr error
	structuralSet bool
}

func applyTaskHashIdentity(task ApplyTask) string {
	return strings.Join(append([]string{
		task.Entry.ID,
		task.Entry.Kind,
		task.Entry.Cluster,
		task.Entry.ClusterKind,
		task.Entry.Node,
		task.Entry.Host,
		task.Playbook,
		task.Limit,
	}, task.Entry.ResourceKeys...), "\x00")
}

func (t *ApplyTask) hashCache() *applyTaskHashCache {
	if t.hashes == nil {
		t.hashes = &applyTaskHashCache{identity: applyTaskHashIdentity(*t)}
	}
	return t.hashes
}

func (c *applyTaskHashCache) reuseFor(task ApplyTask) bool {
	return c != nil && c.identity == applyTaskHashIdentity(task)
}

func (t *ApplyTask) DesiredHash() (string, error) {
	cache := t.hashCache()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if !cache.desiredSet {
		cache.desired, cache.desiredErr = computeApplyTaskDesiredHash(*t)
		cache.desiredSet = true
	}
	return cache.desired, cache.desiredErr
}

func (t *ApplyTask) StructuralHash() (string, error) {
	cache := t.hashCache()
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if !cache.structuralSet {
		cache.structural, cache.structuralErr = computeApplyTaskStructuralHash(*t)
		cache.structuralSet = true
	}
	return cache.structural, cache.structuralErr
}

func ApplyTaskDesiredHash(task ApplyTask) (string, error) {
	if cache := task.hashes; cache.reuseFor(task) {
		cache.mu.Lock()
		defer cache.mu.Unlock()
		if !cache.desiredSet {
			cache.desired, cache.desiredErr = computeApplyTaskDesiredHash(task)
			cache.desiredSet = true
		}
		return cache.desired, cache.desiredErr
	}
	return computeApplyTaskDesiredHash(task)
}

func ApplyTaskStructuralHash(task ApplyTask) (string, error) {
	if cache := task.hashes; cache.reuseFor(task) {
		cache.mu.Lock()
		defer cache.mu.Unlock()
		if !cache.structuralSet {
			cache.structural, cache.structuralErr = computeApplyTaskStructuralHash(task)
			cache.structuralSet = true
		}
		return cache.structural, cache.structuralErr
	}
	return computeApplyTaskStructuralHash(task)
}

func computeApplyTaskDesiredHash(task ApplyTask) (string, error) {
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
	if task.DesiredHashVars != nil {
		payload.FabricVars = task.DesiredHashVars
	} else {
		src := task.State
		if task.DesiredHashState != nil {
			src = *task.DesiredHashState
		}
		state := hashScopedState(src)
		payload.State = &state
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode apply task safety hash input: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func computeApplyTaskStructuralHash(task ApplyTask) (string, error) {
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

func IsReconcilableDrift(record ConvergeSafetyRecord, desiredHash, structuralHash string) bool {
	if record.DesiredHash == desiredHash {
		return false
	}
	if record.HashSchema < ConvergeHashSchema {
		return false
	}
	if strings.TrimSpace(structuralHash) == "" || strings.TrimSpace(record.StructuralHash) == "" {
		return false
	}
	return record.StructuralHash == structuralHash
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
