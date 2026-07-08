package records

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/crmarques/bootwright/internal/host/safefs"
)

const RecordRelativeDir = "addons"

type RecordStatus string

const (
	RecordStatusApplying RecordStatus = "applying"
	RecordStatusWaiting  RecordStatus = "waiting"
	RecordStatusReady    RecordStatus = "ready"
	RecordStatusFailed   RecordStatus = "failed"
)

type RecordPhase string

const (
	RecordPhaseApplying RecordPhase = "applying"
	RecordPhaseApplied  RecordPhase = "applied"
	RecordPhaseWaiting  RecordPhase = "waiting"
	RecordPhaseComplete RecordPhase = "complete"
)

type Record struct {
	Cluster           string                `json:"cluster"`
	Extension         string                `json:"addon"`
	DesiredHash       string                `json:"desiredHash"`
	Status            RecordStatus          `json:"status"`
	Phase             RecordPhase           `json:"phase"`
	RunID             string                `json:"runId,omitempty"`
	StartedAt         time.Time             `json:"startedAt,omitempty"`
	UpdatedAt         time.Time             `json:"updatedAt"`
	AppliedAt         *time.Time            `json:"appliedAt,omitempty"`
	ObservedResources []string              `json:"observedResources,omitempty"`
	LastObserved      string                `json:"lastObserved,omitempty"`
	Hooks             map[string]HookRecord `json:"hooks,omitempty"`
}

// HookRecord is the per-hook lifecycle state written by the hook executor. It is
// keyed by hook name in Record.Hooks. Digest is the hook's content+inputs digest
// used to skip an unchanged run: onChange hook. LastError holds a non-secret
// failure summary only.
type HookRecord struct {
	Lifecycle string       `json:"lifecycle"`
	Status    RecordStatus `json:"status"`
	Digest    string       `json:"digest,omitempty"`
	RanAt     time.Time    `json:"ranAt,omitempty"`
	LastError string       `json:"lastError,omitempty"`
}

func RecordPath(clustersDir, cluster, extension string) string {
	return filepath.Join(clustersDir, cluster, "runtime", RecordRelativeDir, extension+".json")
}

func LoadRecord(clustersDir, cluster, extension string) (Record, bool, error) {
	path := RecordPath(clustersDir, cluster, extension)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("read add-on record: %w", err)
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, true, fmt.Errorf("decode add-on record %s: %w", path, err)
	}
	return record, true, nil
}

// SaveRecord writes the add-on record, preserving any per-hook state already on
// disk when the incoming record carries none. The add-on apply engine (Apply/
// Wait) rebuilds the Record from scratch each save and never sets Hooks, while
// the hook executor writes Hooks out of band via SetHook; preserving on-disk
// Hooks here keeps an engine save from clobbering the executor's updates.
func SaveRecord(clustersDir string, record Record) error {
	if record.Hooks == nil {
		if existing, found, err := LoadRecord(clustersDir, record.Cluster, record.Extension); err == nil && found {
			record.Hooks = existing.Hooks
		}
	}
	return writeRecord(clustersDir, record)
}

func writeRecord(clustersDir string, record Record) error {
	path := RecordPath(clustersDir, record.Cluster, record.Extension)
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode add-on record: %w", err)
	}
	data = append(data, '\n')
	if err := safefs.WriteFileEnsuringDir(path, data, 0o600); err != nil {
		return fmt.Errorf("write add-on record: %w", err)
	}
	return nil
}

// SetHook records one hook's lifecycle state by load-modify-save, preserving the
// rest of the add-on record. It is the hook executor's only writer of the
// record, so the add-on engine's full-record saves (which leave Hooks nil) never
// race it.
func SetHook(clustersDir, cluster, extension, name string, hook HookRecord) error {
	record, found, err := LoadRecord(clustersDir, cluster, extension)
	if err != nil {
		return err
	}
	if !found {
		record = Record{Cluster: cluster, Extension: extension}
	}
	if record.Hooks == nil {
		record.Hooks = map[string]HookRecord{}
	}
	record.Hooks[name] = hook
	return writeRecord(clustersDir, record)
}
