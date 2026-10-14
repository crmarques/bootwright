//go:build linux && amd64

package ansiblelocal

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// scriptedRun runs one shell script as the adapter, with the result channel
// on descriptor 3 and the acknowledgement channel on descriptor 4, and
// returns its result and diagnostics.
func scriptedRun(t *testing.T, script string, configure func(*capabilityRequest)) (prerequisites.ActionResult, []diagnostics.Diagnostic) {
	t.Helper()
	launch, request, boundary := runnerFixture(t, "unused")
	if configure != nil {
		configure(&request)
	}
	boundary.completedDrain, boundary.authorizedDrain = 5*time.Second, 5*time.Second
	boundary.command = func(string, ...string) *exec.Cmd { return exec.Command("/bin/sh", "-c", script) }
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := runProcess(ctx, launch, treeOf(request), request, func(context.Context, []string) error { return nil }, nil, nil, boundary)
	if ctx.Err() != nil {
		t.Fatalf("the adapter ran until the deadline (%v)", err)
	}
	return result, diagnostics.Of(err)
}

// refusalFixture is one tool from a publisher host, with the records an
// adapter writes for it.
type refusalFixture struct {
	tool                 prerequisites.ToolDefinition
	handoff, toolPrepare string
}

func newRefusalFixture(t *testing.T) refusalFixture {
	t.Helper()
	data, err := canonicalRecord(map[string]any{"phase": "prepared", "preparation": map[string]any{"addedSources": []string{"tool-helm"}}})
	if err != nil {
		t.Fatal(err)
	}
	return refusalFixture{
		tool:        prerequisites.ToolDefinition{Kind: "helm", Version: "3.17.3", Source: prerequisites.DependencySource{ID: "tool-helm", URL: "https://get.example.test/helm.tar.gz", SHA256: strings.Repeat("d", 64), Bytes: 1}},
		handoff:     `printf '{"phase":"loaded"}\n' >&3; read -r reply <&4; `,
		toolPrepare: string(data) + `\n`,
	}
}

func (f refusalFixture) request(request *capabilityRequest) {
	request.Tools = []prerequisites.ToolDefinition{f.tool}
}

func acknowledged(record string) string {
	return `printf '` + record + `' >&3; read -r reply <&4; `
}

// afterTheExit has a descendant write records once the adapter is reaped, so
// the runner reads the failed exit first.
func afterTheExit(records string) string {
	return `adapter=$$; (exec >/dev/null 2>&1 4<&-; while kill -0 "$adapter" 2>/dev/null; do sleep 0.01; done; sleep 0.05; printf '` + records + `' >&3) & exec 3>&- 4<&-; exit 2`
}

// A refused record names the class of what refused the adapter and the tool
// it was acquiring: the runner gives that class its diagnostic, naming the
// source's host, whichever of the record and the failed exit it reads first.
// Any source but the tool being installed, or an acquisition class without
// one, breaks the protocol. An internal refusal keeps the generic failure.
func TestARefusedRecordNamesItsClassifiedCause(t *testing.T) {
	f := newRefusalFixture(t)
	refused := func(reason, source string) string {
		if source == "" {
			return `{"phase":"refused","reason":"` + reason + `"}\n`
		}
		return `{"phase":"refused","reason":"` + reason + `","source":"` + source + `"}\n`
	}
	installing := f.handoff + acknowledged(f.toolPrepare) + acknowledged(`{"phase":"continue"}\n`)
	for _, check := range []struct {
		name, script, code, message, outcome string
		routable                             bool
	}{
		{"a tool transfer past its deadline", installing + `printf '` + refused("timeout", "tool-helm") + `' >&3; exit 2`, "controller.setup", "get.example.test did not answer within its bounded acquisition deadline", "unknown", true},
		{"a tool source unresolved, exit first", installing + afterTheExit(refused("dns", "tool-helm")), "controller.setup", "get.example.test could not be resolved by this host's configured resolver", "unknown", true},
		{"a source other than the tool being installed", installing + `printf '` + refused("timeout", "tool-other") + `' >&3; exit 2`, "controller.unknown", "the Ansible capability protocol was invalid", "unknown", false},
		{"a tool source before its continue", f.handoff + acknowledged(f.toolPrepare) + `printf '` + refused("dns", "tool-helm") + `' >&3; exit 2`, "controller.unknown", "the Ansible capability protocol was invalid", "failed", false},
		{"an acquisition class without its source", installing + `printf '` + refused("dns", "") + `' >&3; exit 2`, "controller.unknown", "the Ansible capability protocol was invalid", "unknown", false},
		{"a native class", f.handoff + `printf '` + refused("database", "") + `' >&3; exit 2`, "controller.unknown", "the Ansible capability protocol was invalid", "failed", false},
		{"an internal refusal", f.handoff + acknowledged(f.toolPrepare) + `printf '` + refused("internal", "") + `' >&3; exit 2`, "controller.setup", "Ansible did not complete the authorized dependency operation", "failed", false},
		{"a record after an internal refusal", f.handoff + `printf '` + refused("internal", "") + refused("dns", "tool-helm") + `' >&3; exit 2`, "controller.unknown", "the Ansible capability protocol was invalid", "failed", false},
		{"an unknown class", installing + `printf '` + refused("quota", "tool-helm") + `' >&3; exit 2`, "controller.unknown", "the Ansible capability protocol was invalid", "unknown", false},
	} {
		t.Run(check.name, func(t *testing.T) {
			result, found := scriptedRun(t, check.script, f.request)
			if len(found) != 1 || found[0].Code != check.code || found[0].Message != check.message {
				t.Fatalf("the run reported %+v, want %s %q", found, check.code, check.message)
			}
			if result.Outcome != check.outcome {
				t.Fatalf("the run left %s, want %s", result.Outcome, check.outcome)
			}
			if routed := strings.Contains(found[0].Remediation, "HTTPS_PROXY"); routed != check.routable {
				t.Fatalf("the remedy %q offers a route: %v, want %v", found[0].Remediation, routed, check.routable)
			}
		})
	}
}

// A storage refusal names the publication bundle its tool was being written
// into, so the remedy frees the filesystem that filled.
func TestAStorageRefusalNamesTheFilesystemItsSourceFills(t *testing.T) {
	f := newRefusalFixture(t)
	bundle := t.TempDir()
	script := f.handoff + acknowledged(f.toolPrepare) + acknowledged(`{"phase":"continue"}\n`) + `printf '{"phase":"refused","reason":"storage","source":"tool-helm"}\n' >&3; exit 2`
	_, found := scriptedRun(t, script, func(request *capabilityRequest) {
		f.request(request)
		request.PublicationBundle = bundleLocation{Path: bundle, Writable: true}
	})
	if len(found) != 1 || found[0].Code != "controller.setup" ||
		found[0].Message != "the filesystem holding "+bundle+" could not hold the approved download from get.example.test" ||
		!strings.HasPrefix(found[0].Remediation, "Free space on the filesystem holding "+bundle+", ") {
		t.Fatalf("the run reported %+v", found)
	}
}

// A run that fails after publishing its preparation but before Go
// acknowledged any continue record installed nothing, since the adapter waits
// for that acknowledgement before a tool installation, so the action is failed
// and the next run replaces it. Once one is acknowledged it stays unknown.
func TestAClientInstallationThatFailsBeforeAnyAcknowledgementIsFailed(t *testing.T) {
	f := newRefusalFixture(t)
	result, found := scriptedRun(t, f.handoff+acknowledged(f.toolPrepare)+`exit 2`, f.request)
	if len(found) != 1 || result.Outcome != "failed" || string(result.Evidence) != `{"intentRecorded":true,"postcondition":false}` {
		t.Fatalf("the client installation left %s %s with %+v, want failed with its intent recorded", result.Outcome, result.Evidence, found)
	}
}

func TestAClientInstallationAfterAnAcknowledgedToolStaysUnknown(t *testing.T) {
	f := newRefusalFixture(t)
	result, found := scriptedRun(t, f.handoff+acknowledged(f.toolPrepare)+acknowledged(`{"phase":"continue"}\n`)+`exit 2`, f.request)
	if len(found) != 1 || result.Outcome != "unknown" {
		t.Fatalf("the client installation left %s with %+v, want unknown", result.Outcome, found)
	}
}

// canceledRun runs script as the adapter and cancels the invocation when the
// runner reports detail, or when it publishes the preparation if detail is
// empty.
func canceledRun(t *testing.T, script, detail string, configure func(*capabilityRequest)) (prerequisites.ActionResult, error) {
	t.Helper()
	launch, request, boundary := runnerFixture(t, "unused")
	configure(&request)
	boundary.completedDrain, boundary.authorizedDrain = 5*time.Second, 5*time.Second
	boundary.command = func(string, ...string) *exec.Cmd { return exec.Command("/bin/sh", "-c", script) }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	publish := func(context.Context, []string) error {
		if detail == "" {
			cancel()
		}
		return nil
	}
	progress := func(event prerequisites.ProgressEvent) {
		if detail != "" && strings.HasPrefix(event.Detail, detail) {
			cancel()
		}
	}
	return runProcess(ctx, launch, treeOf(request), request, publish, progress, nil, boundary)
}

// Canceled once its preparation is published but before its continue record
// is acknowledged, a run installed nothing, so its action is failed; canceled
// after that acknowledgement, a tool may be half installed, so it stays
// unknown.
func TestACanceledInstallationBeforeItsContinueIsFailed(t *testing.T) {
	f := newRefusalFixture(t)
	result, err := canceledRun(t, f.handoff+acknowledged(f.toolPrepare)+`sleep 5; exit 2`, "", f.request)
	if !errors.Is(err, context.Canceled) || result.Outcome != "failed" || string(result.Evidence) != `{"intentRecorded":true,"postcondition":false}` {
		t.Fatalf("the canceled run left %s %s with %v, want failed with its intent recorded", result.Outcome, result.Evidence, err)
	}
}

func TestACanceledInstallationAfterItsContinueStaysUnknown(t *testing.T) {
	f := newRefusalFixture(t)
	result, err := canceledRun(t, f.handoff+acknowledged(f.toolPrepare)+acknowledged(`{"phase":"continue"}\n`)+`sleep 0.2; exit 2`, "installing helm", f.request)
	if !errors.Is(err, context.Canceled) || result.Outcome != "unknown" {
		t.Fatalf("the canceled run left %s with %v, want unknown", result.Outcome, err)
	}
}
