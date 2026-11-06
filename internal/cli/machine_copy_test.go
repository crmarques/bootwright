package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
)

func TestCopyOperandErrorsHaveNoDispatch(t *testing.T) {
	for _, tail := range [][]string{nil, {"local:a"}, {"local:a", "local:b"}, {"remote:a", "remote:b"}, {"local:", "remote:b"}, {"a", "remote:b"}, {"local:a", "remote:b", "extra"}} {
		args := append([]string{"machine", "cp", "--name", "guest", "--output", "json"}, tail...)
		status, out, errOut, record := runRecorded(args)
		var envelope commandEnvelope
		if err := json.Unmarshal([]byte(out), &envelope); err != nil {
			t.Fatal(err)
		}
		if status != 2 || errOut != "" || record.calls != 0 || envelope.Diagnostics[0].Code != "cli.usage" {
			t.Fatalf("%v status=%d out=%s err=%s", args, status, out, errOut)
		}
	}
}

func copyRecordedResult() *machineaccess.CopyResult {
	return &machineaccess.CopyResult{Context: "lab", Source: machine.CopyPath{Remote: true, Path: "/logs"}, Machines: []machineaccess.CopyRow{{Machine: "guest", Destination: "/collection/guest/logs", Status: "copied", Files: 2, Bytes: 123}}}
}

func TestCopyDispatchAndStructuredResults(t *testing.T) {
	record := &dispatchRecord{}
	record.result.copy = copyRecordedResult()
	runner, out, errOut := recordedRunner(record)
	status := runner.Run(context.Background(), []string{"machine", "cp", "--name", " guest,host,guest ", "--ssh-user", "admin", "--overwrite", "--output", "json", "remote:/logs", "local:./collected"})
	if status != 0 || errOut.String() != "" {
		t.Fatalf("status=%d out=%s err=%s", status, out, errOut)
	}
	request, ok := record.request.(machineaccess.CopyRequest)
	if !ok || strings.Join(request.Names, ",") != "guest,host" || request.SSH.User != "admin" || !request.Overwrite || !request.NonInteractive {
		t.Fatalf("request %#v", record.request)
	}
	var envelope struct {
		OK     bool                    `json:"ok"`
		Result machineCopyPresentation `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if !envelope.OK || len(envelope.Result.Machines) != 1 || envelope.Result.Machines[0].Bytes != 123 {
		t.Fatalf("out %s", out)
	}
}

func TestCopyPartialFailureAndInterruptKeepResult(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		record := &dispatchRecord{}
		record.result.copy = copyRecordedResult()
		record.result.copy.Machines[0].Status = "unknown"
		record.err = diagnostics.NewFailureWithRemediation("access.copy", "copy publication unknown", "", "inspect the destination")
		runner, out, errOut := recordedRunner(record)
		ctx := context.Background()
		want := 1
		if interrupted {
			var cancel context.CancelCauseFunc
			ctx, cancel = context.WithCancelCause(ctx)
			record.afterCall = func() { cancel(ErrInterrupted) }
			want = 130
		}
		status := runner.Run(ctx, []string{"machine", "cp", "--name", "guest", "--output", "json", "remote:/logs", "local:./collected"})
		if status != want || errOut.String() != "" || !strings.Contains(out.String(), `"status":"unknown"`) {
			t.Fatalf("interrupt=%v status=%d out=%s err=%s", interrupted, status, out, errOut)
		}
	}
}

func recordedRunner(record *dispatchRecord) (*Runner, *bytes.Buffer, *bytes.Buffer) {
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	return New(Config{Out: out, ErrOut: errOut, Services: dispatchSpies(record)}), out, errOut
}
