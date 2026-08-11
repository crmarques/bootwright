package workflow

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestRecordedRunFinalizerFailurePreventsSuccessfulLedger(t *testing.T) {
	baseDir := t.TempDir()
	runsDir := filepath.Join(baseDir, "runs")
	runner := &fakeRunner{}
	finalized := false
	_, err := Run(context.Background(), RunOptions{
		State:              minimalState(),
		RenderedDir:        filepath.Join(baseDir, "rendered"),
		ClustersDir:        filepath.Join(baseDir, "clusters"),
		RunsDir:            runsDir,
		SecretsDir:         filepath.Join(baseDir, "secrets"),
		ManagedServicesDir: filepath.Join(baseDir, "managed-services"),
		ProviderStateDir:   filepath.Join(baseDir, "provider-state"),
		OwnershipDir:       filepath.Join(baseDir, "ownership"),
		BundleDir:          filepath.Join(baseDir, "bundle"),
		Playbook:           "bootwright.core.workflow_infra_destroy_artifact_server",
		ArtifactsBaseName:  "infra-destroy-artifact-server",
		OutputLogPath:      filepath.Join(baseDir, "destroy.log"),
		AcquireRunLease:    true,
		RecordRunLedger:    true,
		PostRunFinalizer: func(RunResult) error {
			finalized = true
			return errors.New("completion proof missing")
		},
	}, runner, nil)
	if err == nil || !strings.Contains(err.Error(), "completion proof missing") {
		t.Fatalf("Run finalizer error = %v", err)
	}
	if !runner.runCalled || !finalized {
		t.Fatalf("runner/finalizer called = %t/%t", runner.runCalled, finalized)
	}
	ledger, found, loadErr := LoadRunLedger(runsDir)
	if loadErr != nil || !found {
		t.Fatalf("LoadRunLedger: found=%t err=%v", found, loadErr)
	}
	if ledger.Status != RunStatusFailed || len(ledger.Tasks) != 1 || ledger.Tasks[0].Status != TaskStatusFailed {
		t.Fatalf("finalizer failure ledger = status %s tasks %+v", ledger.Status, ledger.Tasks)
	}
	archived, found, loadErr := LoadArchivedRunLedger(runsDir, ledger.RunID)
	if loadErr != nil || !found || archived.Status != RunStatusFailed {
		t.Fatalf("archived finalizer failure = found=%t status=%s err=%v", found, archived.Status, loadErr)
	}
}
