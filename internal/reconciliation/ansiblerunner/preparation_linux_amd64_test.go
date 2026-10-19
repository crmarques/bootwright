//go:build linux && amd64

package ansiblerunner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/adapterprotocol"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func preparingRun(t *testing.T, prepare func(context.Context, json.RawMessage) error) (string, error) {
	t.Helper()
	recorded := filepath.Join(t.TempDir(), "recorded")
	runner := sweepingRunner(t, func() *exec.Cmd {
		return exec.Command("/bin/sh", "-c", `printf '{"phase":"loaded"}\n' >&3; read -r reply <&4; `+
			`printf '{"phase":"prepared","preparation":{"marker":"m-1"}}\n' >&3; read -r reply <&4 || exit 3; `+
			`if [ -f "$0" ]; then echo recorded; else echo unrecorded; fi; `+
			`printf '{"evidence":{"absent":false},"outcome":"changed","phase":"completed"}\n' >&3; exit 0`, recorded)
	})
	var output bytes.Buffer
	request := adapterRequest(t, &output)
	request.Prepare = func(ctx context.Context, preparation json.RawMessage) error {
		time.Sleep(200 * time.Millisecond)
		if err := prepare(ctx, preparation); err != nil {
			return err
		}
		return os.WriteFile(recorded, preparation, 0600)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := runner.Run(ctx, request)
	if ctx.Err() != nil {
		t.Fatalf("the preparing adapter ran until the deadline (%v)", err)
	}
	return output.String(), err
}

func TestLifecyclePreparationIsRecordedBeforeItIsAcknowledged(t *testing.T) {
	var published []json.RawMessage
	output, err := preparingRun(t, func(_ context.Context, preparation json.RawMessage) error {
		published = append(published, preparation)
		return nil
	})
	if err != nil || output != "recorded\n" || len(published) != 1 || string(published[0]) != `{"marker":"m-1"}` {
		t.Fatalf("the run printed %q, published %q and reported %v", output, published, err)
	}
	output, err = preparingRun(t, func(context.Context, json.RawMessage) error {
		return errors.New("the attempt record could not be written")
	})
	reported := diagnostics.Of(err)
	if output != "" || len(reported) != 1 || reported[0].Code != "lifecycle.unknown" || reported[0].Message != "the attempt's preparation could not be recorded" {
		t.Fatalf("an unrecorded preparation let the adapter print %q and reported %+v (%v)", output, reported, err)
	}
	if outcome := lifecycle.AttemptOutcome(err); outcome == "changed" {
		t.Fatalf("an unrecorded preparation completed the attempt")
	}
	const refused = "the installation's automation published a before-state that does not name this installation's marker"
	output, err = preparingRun(t, func(context.Context, json.RawMessage) error {
		return diagnostics.NewFailureWithRemediation("lifecycle.state", refused, "", "")
	})
	reported = diagnostics.Of(err)
	if output != "" || len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Message != refused {
		t.Fatalf("a refused preparation let the adapter print %q and reported %+v (%v)", output, reported, err)
	}
}

func TestLifecyclePreparationRefusedWithoutAHookOrOnAReadOnlyRun(t *testing.T) {
	prepared := adapterprotocol.Record{Phase: "prepared", Preparation: json.RawMessage(`{"marker":"m-1"}`)}
	calls := 0
	hook := func(context.Context, json.RawMessage) error { calls++; return nil }
	for name, judge := range map[string]*lifecycleJudge{
		"no hook":      {request: lifecycle.RunRequest{}, loaded: true},
		"read-only":    {request: lifecycle.RunRequest{ReadOnly: true, Prepare: hook}, loaded: true},
		"before ready": {request: lifecycle.RunRequest{Prepare: hook}},
	} {
		if verdict := judge.Judge(context.Background(), prepared, adapterprotocol.Moment{Open: true}); verdict.Valid || verdict.Acknowledge {
			t.Fatalf("%s: a preparation was judged %+v", name, verdict)
		}
	}
	if calls != 0 {
		t.Fatalf("a refused preparation was recorded %d times", calls)
	}
	judge := &lifecycleJudge{request: lifecycle.RunRequest{Prepare: hook}, loaded: true}
	if verdict := judge.Judge(context.Background(), prepared, adapterprotocol.Moment{Open: true, Exited: true}); !verdict.Valid || calls != 0 {
		t.Fatalf("a preparation read after the adapter exited was judged %+v and recorded %d times", verdict, calls)
	}
}

func TestLifecyclePreparationOnlyOnce(t *testing.T) {
	prepared := adapterprotocol.Record{Phase: "prepared", Preparation: json.RawMessage(`{"marker":"m-1"}`)}
	calls := 0
	judge := &lifecycleJudge{request: lifecycle.RunRequest{Prepare: func(context.Context, json.RawMessage) error { calls++; return nil }}, loaded: true}
	if verdict := judge.Judge(context.Background(), prepared, adapterprotocol.Moment{Open: true}); !verdict.Valid || !verdict.Acknowledge {
		t.Fatalf("the first preparation was judged %+v", verdict)
	}
	if verdict := judge.Judge(context.Background(), prepared, adapterprotocol.Moment{Open: true}); verdict.Valid || verdict.Acknowledge {
		t.Fatalf("a second preparation was judged %+v", verdict)
	}
	if calls != 1 {
		t.Fatalf("the preparation was recorded %d times", calls)
	}
}
