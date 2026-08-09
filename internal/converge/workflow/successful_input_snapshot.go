package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	safefs "github.com/crmarques/bootwright/internal/host/safefs"
)

const successfulInputSnapshotAPIVersion = "bootwright.io/successful-input/v1"

type successfulInputSnapshot struct {
	APIVersion string          `json:"apiVersion"`
	RunID      string          `json:"runID"`
	ResourceID string          `json:"resourceID"`
	TaskID     string          `json:"taskID"`
	TaskKind   string          `json:"taskKind"`
	TaskStatus TaskStatus      `json:"taskStatus"`
	HashSchema int             `json:"hashSchema"`
	Input      json.RawMessage `json:"input"`
}

func successfulInputSnapshotPath(runsDir, runID, resourceID string) string {
	return filepath.Join(runsDir, "history", runID, "successful-inputs", convergeSafetyRecordFileName(resourceID))
}

func saveSuccessfulInputSnapshot(runsDir, runID, resourceID, taskID, taskKind string, taskStatus TaskStatus, hashSchema int, input []byte) error {
	if strings.TrimSpace(runsDir) == "" || strings.TrimSpace(runID) == "" {
		return nil
	}
	if strings.TrimSpace(resourceID) == "" || strings.TrimSpace(taskID) == "" || strings.TrimSpace(taskKind) == "" || !json.Valid(input) {
		return fmt.Errorf("write successful input snapshot: run, resource, task identity and valid input are required")
	}
	canonicalInput, err := canonicalSuccessfulInput(input)
	if err != nil {
		return err
	}
	snapshot := successfulInputSnapshot{
		APIVersion: successfulInputSnapshotAPIVersion,
		RunID:      runID,
		ResourceID: resourceID,
		TaskID:     taskID,
		TaskKind:   taskKind,
		TaskStatus: taskStatus,
		HashSchema: hashSchema,
		Input:      canonicalInput,
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("encode successful input snapshot: %w", err)
	}
	data = append(data, '\n')
	path := successfulInputSnapshotPath(runsDir, runID, resourceID)
	existing, err := os.ReadFile(path)
	switch {
	case err == nil:
		if bytes.Equal(existing, data) {
			return nil
		}
		return fmt.Errorf("successful input snapshot %s already exists with different content; refusing to replace immutable run evidence", path)
	case !errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("read successful input snapshot %s: %w", path, err)
	}
	if err := safefs.EnsureDir(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := safefs.WriteNewFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write successful input snapshot: %w", err)
	}
	return nil
}

func successfulInputSnapshotMatches(runsDir, runID, resourceID, taskID, taskKind string, taskStatus TaskStatus, hashSchema int, currentInput []byte) (bool, error) {
	if strings.TrimSpace(runID) == "" {
		return false, fmt.Errorf("successful input snapshot has no run identity")
	}
	if hashSchema != ConvergeHashSchema-1 {
		return false, fmt.Errorf("successful input snapshot schema %d cannot prove a schema-%d record", hashSchema, ConvergeHashSchema)
	}
	path := successfulInputSnapshotPath(runsDir, runID, resourceID)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("successful input snapshot %s is missing", path)
	}
	if err != nil {
		return false, fmt.Errorf("read successful input snapshot %s: %w", path, err)
	}
	var snapshot successfulInputSnapshot
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return false, fmt.Errorf("decode successful input snapshot %s: %w", path, err)
	}
	if snapshot.APIVersion != successfulInputSnapshotAPIVersion || snapshot.RunID != runID || snapshot.ResourceID != resourceID || snapshot.TaskID != taskID || snapshot.TaskKind != taskKind || snapshot.TaskStatus != taskStatus || snapshot.HashSchema != hashSchema || !json.Valid(snapshot.Input) {
		return false, fmt.Errorf("successful input snapshot %s has ambiguous or mismatched run, resource, task, status, schema, or input identity", path)
	}
	ledger, found, err := LoadArchivedRunLedger(runsDir, runID)
	if err != nil {
		return false, err
	}
	if !found {
		return false, fmt.Errorf("successful input snapshot %s has no archived run ledger", path)
	}
	if ledger.RunID != runID || ledger.Status != RunStatusOK {
		return false, fmt.Errorf("successful input snapshot %s is not bound to an archived successful run", path)
	}
	matches := 0
	for _, task := range ledger.Tasks {
		if task.ID != taskID {
			continue
		}
		matches++
		if task.Kind != taskKind || task.Status != taskStatus {
			return false, fmt.Errorf("successful input snapshot %s does not match its archived task identity or terminal status", path)
		}
	}
	if matches != 1 {
		return false, fmt.Errorf("successful input snapshot %s has ambiguous archived task identity: found %d tasks named %s", path, matches, taskID)
	}
	snapshotInput, err := canonicalSuccessfulInput(snapshot.Input)
	if err != nil {
		return false, fmt.Errorf("successful input snapshot %s has invalid input: %w", path, err)
	}
	canonicalCurrent, err := canonicalSuccessfulInput(currentInput)
	if err != nil {
		return false, err
	}
	return bytes.Equal(snapshotInput, canonicalCurrent), nil
}

func canonicalSuccessfulInput(input []byte) (json.RawMessage, error) {
	if !json.Valid(input) {
		return nil, fmt.Errorf("successful input snapshot requires valid JSON input")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, input); err != nil {
		return nil, fmt.Errorf("canonicalize successful input snapshot: %w", err)
	}
	return append(json.RawMessage(nil), compact.Bytes()...), nil
}
