package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestStoragePreflightUnknownWritesOneCompleteJSONResult(t *testing.T) {
	record := &dispatchRecord{result: commandResult{storagePreflight: pendingStoragePreflight()}, err: diagnostics.NewFailure("preflight.unknown", "storage readiness is not proved", "")}
	var out, errOut bytes.Buffer
	exit := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"preflight", "storage-cluster", "--output", "json"})
	if exit != 1 || errOut.Len() != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, errOut.String())
	}
	decoder := json.NewDecoder(&out)
	var envelope struct {
		OK     bool
		Result struct {
			Checks  []storageCheckResult
			Summary storageSummaryResult
		}
	}
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.OK || len(envelope.Result.Checks) != 2 || envelope.Result.Summary.Pending != 1 {
		t.Fatalf("%+v", envelope)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		t.Fatalf("extra output: %v", err)
	}
}
func TestStoragePreflightHumanFailureKeepsChecksAndEscapesText(t *testing.T) {
	report := pendingStoragePreflight()
	report.Checks[0].Reason = "not ready\x1b[31m\nforged"
	record := &dispatchRecord{result: commandResult{storagePreflight: report}, err: diagnostics.NewFailure("preflight.unknown", "storage readiness is not proved", "")}
	var out, errOut bytes.Buffer
	exit := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{"preflight", "storage-cluster"})
	if exit != 1 || !strings.Contains(out.String(), "pending") || !strings.Contains(errOut.String(), "preflight.unknown") || strings.Contains(out.String(), "\x1b") || strings.Contains(out.String(), "\nforged") {
		t.Fatalf("%d %q %q", exit, out.String(), errOut.String())
	}
}
