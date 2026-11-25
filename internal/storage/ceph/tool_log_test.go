package ceph

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
)

func TestCephadmLogNoticeNamesTheBootstrapMachineAndDefaultPath(t *testing.T) {
	ctx := context.Background()
	var notice, progress string
	execution := lifecycle.Execution{
		Log: func(_ context.Context, record operationstore.LogRecord) error {
			if record.Event != "tool-log" {
				t.Errorf("event = %q", record.Event)
			}
			notice = record.Detail
			return nil
		},
		Progress: func(_ context.Context, _, _ string, progressNote lifecycle.ByteProgress) {
			progress = progressNote.ToolLog
		},
	}
	reportCephadmLog(ctx, execution, Request{Bootstrap: "seed", Hosts: []Host{
		{Name: "other", Machine: "another"},
		{Name: "seed", Machine: "ceph-bootstrap", Address: "192.0.2.4"},
	}})
	const want = "cephadm default log on Machine/ceph-bootstrap: /var/log/ceph/cephadm.log (host-wide; inspect on that host)"
	if notice != want || progress != want {
		t.Fatalf("log = %q, progress = %q", notice, progress)
	}
}
