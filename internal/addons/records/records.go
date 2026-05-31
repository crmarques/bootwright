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
	Cluster           string       `json:"cluster"`
	Extension         string       `json:"addon"`
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
		return Record{}, false, fmt.Errorf("read addon record: %w", err)
	}
	var record Record
	if err := json.Unmarshal(data, &record); err != nil {
		return Record{}, true, fmt.Errorf("decode addon record %s: %w", path, err)
	}
	return record, true, nil
}

func SaveRecord(clustersDir string, record Record) error {
	path := RecordPath(clustersDir, record.Cluster, record.Extension)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create addon record directory: %w", err)
	}
	if err := os.Chmod(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("chmod addon record directory: %w", err)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode addon record: %w", err)
	}
	data = append(data, '\n')
	if err := safefs.AtomicWriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write addon record: %w", err)
	}
	return nil
}
