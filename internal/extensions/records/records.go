package records

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/crmarques/bootwright/internal/runtime/fs"
)

const RecordRelativeDir = "extension-records"

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
	Cluster           string       `json:"cluster"`
	Extension         string       `json:"extension"`
	DesiredHash       string       `json:"desiredHash"`
	Status            RecordStatus `json:"status"`
	Phase             RecordPhase  `json:"phase"`
	RunID             string       `json:"runId,omitempty"`
	StartedAt         time.Time    `json:"startedAt,omitempty"`
	UpdatedAt         time.Time    `json:"updatedAt"`
	AppliedAt         *time.Time   `json:"appliedAt,omitempty"`
	ObservedResources []string     `json:"observedResources,omitempty"`
	LastObserved      string       `json:"lastObserved,omitempty"`
}

func RecordPath(runtimeDir, cluster, extension string) string {
	return filepath.Join(runtimeDir, RecordRelativeDir, cluster, extension+".json")
}

func LoadRecord(runtimeDir, cluster, extension string) (Record, bool, error) {
	path := RecordPath(runtimeDir, cluster, extension)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, false, nil
	}
	if err != nil {
		return Record{}, false, fmt.Errorf("read extension record: %w", err)
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, true, fmt.Errorf("decode extension record %s: %w", path, err)
	}
	return record, true, nil
}

func SaveRecord(runtimeDir string, record Record) error {
	path := RecordPath(runtimeDir, record.Cluster, record.Extension)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create extension record directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("chmod extension record directory: %w", err)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode extension record: %w", err)
	}
	data = append(data, '\n')
	if err := safefs.AtomicWriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write extension record: %w", err)
	}
	return nil
}
