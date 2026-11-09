package cli

import (
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/reconciliation/logview"
)

const logsOperation = "op-6f1c2a9e0b7d4c3f8a5e2d1b0c9f8e7a"

func logsEvents() *logview.Result {
	return &logview.Result{Context: "lab", Operation: logsOperation, Verb: "apply", State: "running", Events: []logview.Event{
		{Time: "2026-11-08T14:02:11Z", Event: "opened", Detail: "apply " + logsOperation},
		{Time: "2026-11-08T14:02:12Z", Block: "artifact-server-lab-artifacts", Attempt: 1, Event: "started", Detail: "attempt 1"},
		{Time: "2026-11-08T14:02:40Z", Block: "artifact-server-lab-artifacts", Attempt: 1, Event: "group", Group: "pull-image", Detail: "done"},
		{Time: "2026-11-08T14:03:05Z", Block: "artifact-server-lab-artifacts", Attempt: 1, Event: "outcome", Detail: "changed"},
		{Time: "2026-11-08T14:03:06Z", Block: "machine-rhel-01", Attempt: 2, Resolution: 1, Event: "started", Detail: "attempt 2 resolution 1"},
		{Time: "", Block: "machine-rhel-01", Attempt: 2, Resolution: 1, Event: "truncated", Detail: "3 records dropped"},
	}}
}

func logsRaw() *logview.Result {
	return &logview.Result{Context: "lab", Operation: logsOperation, Verb: "apply", State: "failed",
		Raw: &logview.Raw{
			Source: logview.Source{Kind: logview.SourceAttempt, Operation: logsOperation, Block: "machine-rhel-01", Attempt: 2},
			Path:   "contexts/lab/operations/" + logsOperation + "/blocks/machine-rhel-01/attempt-000002.output",
			Bytes:  56, Truncated: true, Content: []byte("TASK [define the domain]\r\n\x1b[31mfatal: no such pool\x1b[0m\nok\n"),
		},
		Warnings: []diagnostics.Diagnostic{{Severity: "warning", Code: "logs.truncated",
			Message: "the retained output contexts/lab/operations/" + logsOperation + "/blocks/machine-rhel-01/attempt-000002.output was cut short at its bound; what the adapter printed after it is not retained"}},
	}
}

func logsStream(kind, name, reference string) *logview.Result {
	source := logview.Source{Kind: kind, Run: name}
	if kind == logview.SourceInstall {
		source = logview.Source{Kind: kind, Install: name}
	}
	return &logview.Result{Raw: &logview.Raw{Source: source, Path: reference, Bytes: 27, Content: []byte("PLAY [power]\nok: [rhel-01]\n")}}
}

// liveStatus is a running apply with one block on its second attempt's first
// resolution, whose output holds 184 KiB, and one whose output is not yet
// written; asked with --paths it also names where the context lives.
func liveStatus(paths bool) *lifecycle.StatusResult {
	blocks := []lifecycle.BlockResult{
		{ID: "artifact-server-lab-artifacts", Description: "serve artifacts for lab-artifacts on controller", Stage: "infrastructure-services", State: "done", Attempts: 1},
		{ID: "machine-rhel-01", Description: "realize the virtual machine rhel-01 and its emulated BMC", Stage: "machines", State: "running", Attempts: 2},
		{ID: "machine-rhel-02", Description: "realize the virtual machine rhel-02 and its emulated BMC", Stage: "machines", State: "running", Attempts: 1},
	}
	operation := "contexts/lab/operations/" + logsOperation + "/blocks/"
	result := &lifecycle.StatusResult{
		Context:     lifecycle.ContextIdentity{Name: "lab", Revision: additiveRevision, Mode: "ready"},
		SetupChecks: []lifecycle.SetupCheck{{ID: "controller-binding", Status: "ready"}},
		Desired:     lifecycle.DesiredSummary{Revision: additiveRevision, Environment: "lab", Files: 6, Objects: 6},
		Secrets:     lifecycle.SecretSummary{Declared: 1, Bindings: 1},
		Handoff:     lifecycle.HandoffSummary{Reasons: []string{"the apply is running, not done"}},
		NextSteps:   []string{"bootwright apply --context lab"},
		Lifecycle: &lifecycle.LifecycleSummary{
			Operation: logsOperation, Verb: "apply", State: "running", Next: "continue-apply", Blocks: blocks,
			Logs: []string{"contexts/lab/operations/" + logsOperation + "/operation.jsonl"}, Executable: "1.4.0 (9f2c1ab)",
			Live: []lifecycle.LiveBlock{
				{Block: "machine-rhel-01", Attempt: 2, Resolution: 1, Output: operation + "machine-rhel-01/attempt-000002-resolution-000001.output", Present: true, Bytes: 188_500},
				{Block: "machine-rhel-02", Attempt: 1, Output: operation + "machine-rhel-02/attempt-000001.output"},
			},
		},
		Contradictions: []string{},
		LogLocation:    "/var/log/bootwright/contexts/lab/operations/" + logsOperation,
	}
	if paths {
		result.Paths = []lifecycle.ContextPath{
			{Kind: "store", Path: "/var/lib/bootwright/contexts/lab", Present: true},
			{Kind: "logs", Path: "/var/log/bootwright/contexts/lab", Present: true},
			{Kind: "hosted", Path: "/var/lib/bootwright-hosted/lab"},
			{Kind: "libvirt-images", Path: "/var/lib/libvirt/images/bootwright/lab", Present: true},
		}
	}
	return result
}

func logsGoldens() []cliGolden {
	return []cliGolden{
		{golden: "cli-logs", args: "logs --context lab", record: func(r *dispatchRecord) { r.result.logs = logsEvents() }},
		{golden: "cli-logs-full", args: "logs --context lab --detail full", record: func(r *dispatchRecord) { r.result.logs = logsEvents() }},
		{golden: "cli-logs-json", args: "logs --context lab --output json", record: func(r *dispatchRecord) { r.result.logs = logsEvents() }},
		{golden: "cli-logs-raw", args: "logs --context lab --block machine-rhel-01 --raw",
			stderr: "  warning  logs.truncated\n    the retained output\n    contexts/lab/operations/" + logsOperation + "/blocks/machine-rhel-01/attempt-000002.output\n    was cut short at its bound; what the adapter printed after it is not retained\n",
			record: func(r *dispatchRecord) { r.result.logs = logsRaw() }},
		{golden: "cli-logs-raw-json", args: "logs --context lab --block machine-rhel-01 --raw --output json", record: func(r *dispatchRecord) { r.result.logs = logsRaw() }},
		{golden: "cli-logs-run", args: "logs --context lab --run run-000003", record: func(r *dispatchRecord) {
			r.result.logs = logsStream(logview.SourceRun, "run-000003", "contexts/lab/runs/run-000003.output")
		}},
		{golden: "cli-logs-install", args: "logs --install", record: func(r *dispatchRecord) {
			r.result.logs = logsStream(logview.SourceInstall, "install-000003", "controller/install-000003.output")
		}},
		{golden: "cli-logs-follow", args: "logs --context lab --follow",
			stderr: "  warning  logs.detached\n    operation " + logsOperation + " is running but no apply or destroy holds\n    context lab\n    Next: continue it with bootwright apply --context lab\n",
			record: func(r *dispatchRecord) {
				r.followed = []followedLine{
					{Line: logview.Line{Text: "14:03:06  machine-rhel-01  started  attempt 2 resolution 1"}, event: true},
					{Line: logview.Line{Source: "machine-rhel-01", Text: "TASK [define the domain]"}},
					{Line: logview.Line{Source: "machine-rhel-01", Text: "\x1b[31mfatal: no such pool\x1b[0m"}},
					{Line: logview.Line{Text: "14:03:30  machine-rhel-01  outcome  failed"}, event: true},
				}
				r.result.logsFollow = &logview.Summary{Context: "lab", Operation: logsOperation, State: "running", Warnings: []diagnostics.Diagnostic{{
					Severity: "warning", Code: "logs.detached", Message: "operation " + logsOperation + " is running but no apply or destroy holds context lab",
					Remediation: "continue it with bootwright apply --context lab"}}}
			}},
		{golden: "cli-status-live", args: "status --context lab", record: func(r *dispatchRecord) { r.result.lifecycleStatus = liveStatus(false) }},
		{golden: "cli-status-live-json", args: "status --context lab --output json", record: func(r *dispatchRecord) { r.result.lifecycleStatus = liveStatus(false) }},
		{golden: "cli-status-paths", args: "status --context lab --paths --width 140", record: func(r *dispatchRecord) { r.result.lifecycleStatus = liveStatus(true) }},
		{golden: "cli-status-paths-json", args: "status --context lab --paths --output json", record: func(r *dispatchRecord) { r.result.lifecycleStatus = liveStatus(true) }},
	}
}
