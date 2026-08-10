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
	Steps             map[string]StepRecord `json:"steps,omitempty"`
}

type StepRecord struct {
	Lifecycle       string            `json:"lifecycle"`
	Status          RecordStatus      `json:"status"`
	Digest          string            `json:"digest,omitempty"`
	ObservedDigests map[string]string `json:"observedDigests,omitempty"`
	RanAt           time.Time         `json:"ranAt,omitempty"`
	LastError       string            `json:"lastError,omitempty"`
}

func (r Record) HasFailedStep() bool {
	for _, step := range r.Steps {
		if step.Status == RecordStatusFailed {
			return true
		}
	}
	return false
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

func SaveRecord(clustersDir string, record Record) error {
	if record.Steps == nil {
		if existing, found, err := LoadRecord(clustersDir, record.Cluster, record.Extension); err == nil && found {
			record.Steps = existing.Steps
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

func SetStep(clustersDir, cluster, extension, name string, step StepRecord) error {
	record, found, err := LoadRecord(clustersDir, cluster, extension)
	if err != nil {
		return err
	}
	if !found {
		record = Record{Cluster: cluster, Extension: extension}
	}
	if record.Steps == nil {
		record.Steps = map[string]StepRecord{}
	}
	record.Steps[name] = step
	return writeRecord(clustersDir, record)
}
