//go:build linux && amd64

package ansiblerunner

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/adapterprotocol"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

type groupReport struct {
	group, status string
	bytes         lifecycle.ByteProgress
}

func progressJudge(clock *time.Time) (*lifecycleJudge, *[]groupReport, *[]operationstore.LogRecord) {
	var reports []groupReport
	var logged []operationstore.LogRecord
	judge := &lifecycleJudge{loaded: true, now: func() time.Time { return *clock }, request: lifecycle.RunRequest{
		Progress: func(_ context.Context, group, status string, bytes lifecycle.ByteProgress) {
			reports = append(reports, groupReport{group, status, bytes})
		},
		Log: func(_ context.Context, record operationstore.LogRecord) error {
			logged = append(logged, record)
			return nil
		},
	}}
	return judge, &reports, &logged
}

func groupWith(group, status string, completed, total int64) adapterprotocol.Record {
	return adapterprotocol.Record{Phase: "group", Group: group, Status: status, Progress: &adapterprotocol.Progress{Completed: completed, Total: total}}
}

func TestAGroupsByteProgressReachesTheEngineAndTheLogOnlyWhenDue(t *testing.T) {
	clock := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	judge, reports, logged := progressJudge(&clock)
	moment := adapterprotocol.Moment{Open: true}
	const gib = int64(1) << 30
	steps := []struct {
		after     time.Duration
		completed int64
	}{{0, 0}, {2 * time.Second, gib / 100}, {20 * time.Second, gib / 50}, {30 * time.Second, gib / 4}, {40 * time.Second, gib/4 + 1}, {90 * time.Second, gib/4 + 2}}
	for _, step := range steps {
		clock = time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC).Add(step.after)
		if verdict := judge.Judge(context.Background(), groupWith("zero and read back sda", "running", step.completed, gib), moment); !verdict.Valid {
			t.Fatalf("a group with progress was judged %+v", verdict)
		}
	}
	if len(*reports) != len(steps) || (*reports)[3].bytes != (lifecycle.ByteProgress{Completed: gib / 4, Total: gib}) {
		t.Fatalf("progress reached the engine as %+v", *reports)
	}
	var details []string
	for _, record := range *logged {
		if record.Event != "group" || record.Group != "zero and read back sda" {
			t.Fatalf("log record = %+v", record)
		}
		details = append(details, record.Detail)
	}
	want := []string{
		"running 0% (0 B of 1.0 GiB)",
		"running 25% (256.0 MiB of 1.0 GiB), 8.5 MiB/s, about 2m left at the observed rate",
		"running 25% (256.0 MiB of 1.0 GiB), 2.8 MiB/s, about 5m left at the observed rate",
	}
	if !reflect.DeepEqual(details, want) {
		t.Fatalf("log details = %q, want the first count, the five percent step and the minute", details)
	}
}

func TestAGroupWithoutProgressLogsItsStatusAndAnEmptyCountLogsNothing(t *testing.T) {
	clock := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	judge, reports, logged := progressJudge(&clock)
	moment := adapterprotocol.Moment{Open: true}
	judge.Judge(context.Background(), adapterprotocol.Record{Phase: "group", Group: "boot", Status: "ok"}, moment)
	judge.Judge(context.Background(), groupWith("boot", "running", 0, 0), moment)
	if len(*reports) != 2 || (*reports)[0].bytes != (lifecycle.ByteProgress{}) {
		t.Fatalf("reports = %+v", *reports)
	}
	if len(*logged) != 1 || (*logged)[0].Detail != "ok" {
		t.Fatalf("log records = %+v, want the one status", *logged)
	}
	judge.Judge(context.Background(), groupWith("boot", "running", 1, 2), adapterprotocol.Moment{Open: true, Exited: true})
	if len(*reports) != 2 || len(*logged) != 1 {
		t.Fatalf("a record read after the exit reported %+v and logged %+v", *reports, *logged)
	}
}

func TestAGroupsDetailReachesTheEngineAsItsCapabilitysNoteAndOnlyForADeclaredGroup(t *testing.T) {
	clock := time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)
	judge, reports, logged := progressJudge(&clock)
	judge.request.Groups = []string{"erase-1", "erase-2"}
	judge.request.GroupNote = func(group, detail string) (string, bool) {
		return "note " + group + " " + detail, detail == "unanswered:refused:3min"
	}
	moment := adapterprotocol.Moment{Open: true}
	noted := adapterprotocol.Record{Phase: "group", Group: "erase-2", Status: "running", Detail: "unanswered:refused:3min"}
	if verdict := judge.Judge(context.Background(), noted, moment); !verdict.Valid {
		t.Fatalf("a declared group's note was judged %+v", verdict)
	}
	if len(*reports) != 1 || (*reports)[0].bytes.Note != "note erase-2 unanswered:refused:3min" || (*reports)[0].group != "erase-2" {
		t.Fatalf("reports = %+v", *reports)
	}
	if len(*logged) != 1 || (*logged)[0].Detail != "running note erase-2 unanswered:refused:3min" {
		t.Fatalf("log records = %+v", *logged)
	}
	for _, record := range []adapterprotocol.Record{
		{Phase: "group", Group: "waiting for the erase worker", Status: "running"},
		{Phase: "group", Group: "erase-1", Status: "running", Detail: "unanswered:refused:4min"},
	} {
		if verdict := judge.Judge(context.Background(), record, moment); verdict.Valid {
			t.Fatalf("record %+v was judged valid", record)
		}
	}
	unnoted, _, _ := progressJudge(&clock)
	if verdict := unnoted.Judge(context.Background(), noted, moment); verdict.Valid {
		t.Fatal("a detail reached a run whose capability words none")
	}
	if len(*reports) != 1 {
		t.Fatalf("an invalid record reported %+v", *reports)
	}
}
