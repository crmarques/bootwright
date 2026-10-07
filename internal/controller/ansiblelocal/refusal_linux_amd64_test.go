//go:build linux && amd64

package ansiblelocal

import (
	"context"
	"encoding/json"
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
	return scriptedRunUnder(t, t.TempDir(), script, configure)
}

// scriptedRunUnder is scriptedRun with its package scratch under scratch.
func scriptedRunUnder(t *testing.T, scratch, script string, configure func(*capabilityRequest)) (prerequisites.ActionResult, []diagnostics.Diagnostic) {
	t.Helper()
	launch, request, boundary := runnerFixture(t, "unused")
	boundary.scratchParent = scratch
	if configure != nil {
		configure(&request)
	}
	boundary.completedDrain, boundary.authorizedDrain = 5*time.Second, 5*time.Second
	boundary.command = func(string, ...string) *exec.Cmd { return exec.Command("/bin/sh", "-c", script) }
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := runProcess(ctx, launch, request, func() error { return nil }, func(context.Context, prerequisites.NativePreparation) error { return nil }, nil, nil, boundary)
	if ctx.Err() != nil {
		t.Fatalf("the adapter ran until the deadline (%v)", err)
	}
	return result, diagnostics.Of(err)
}

// refusalFixture is a native plan with one package from a publisher host and
// one tool from another, with the records an adapter writes for them.
type refusalFixture struct {
	plan                                 *prerequisites.NativeResolvedPlan
	pkg                                  prerequisites.NativePackage
	tool                                 prerequisites.ToolDefinition
	handoff, nativePrepared, toolPrepare string
}

func newRefusalFixture(t *testing.T) refusalFixture {
	t.Helper()
	sha := strings.Repeat("a", 64)
	plan := &prerequisites.NativeResolvedPlan{Digest: strings.Repeat("c", 64), BeforeSHA256: sha, AfterSHA256: strings.Repeat("b", 64), Actions: []prerequisites.NativeAction{{SourceID: "native-one"}}}
	transitions, err := prerequisites.NativeTransitionsDigest(plan.Actions)
	if err != nil {
		t.Fatal(err)
	}
	record := func(value any) string {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(data) + `\n`
	}
	tool := prerequisites.ToolDefinition{Kind: "helm", Version: "3.17.3", Source: prerequisites.DependencySource{ID: "tool-helm", URL: "https://get.example.test/helm.tar.gz", SHA256: strings.Repeat("d", 64), Bytes: 1}}
	return refusalFixture{
		plan: plan, tool: tool,
		pkg:            prerequisites.NativePackage{Source: prerequisites.DependencySource{ID: "native-one", URL: "https://cdn.example.test/el9/one.rpm", SHA256: sha, Bytes: 1}},
		handoff:        `printf '{"phase":"loaded"}\n' >&3; read -r reply <&4; `,
		nativePrepared: record(map[string]any{"phase": "prepared", "preparation": prerequisites.NativePreparation{InventorySHA256: sha, AfterInventorySHA256: plan.AfterSHA256, PlanDigest: plan.Digest, TransitionsSHA256: transitions, AddedSources: []string{"native-one"}}}),
		toolPrepare:    record(map[string]any{"phase": "prepared", "preparation": prerequisites.NativePreparation{InventorySHA256: sha, AddedSources: []string{tool.Source.ID}}}),
	}
}

func acknowledged(record string) string {
	return `printf '` + record + `' >&3; read -r reply <&4; `
}

// afterTheExit has a descendant write records once the adapter is reaped, so
// the runner reads the failed exit first.
func afterTheExit(records string) string {
	return `adapter=$$; (exec >/dev/null 2>&1 4<&-; while kill -0 "$adapter" 2>/dev/null; do sleep 0.01; done; sleep 0.05; printf '` + records + `' >&3) & exec 3>&- 4<&-; exit 2`
}

// A refused record names the class of what refused the adapter, and an
// acquisition the source it was acquiring: the runner gives that class its
// diagnostic, naming the source's host, whichever of the record and the
// failed exit it reads first. A source the run is not acquiring breaks the
// protocol: a package source before its preparation or after its native
// record, and any source but the tool being installed, and a native refusal after Go authorized its transaction leaves
// that transaction unknown. An internal refusal keeps the generic failure.
func TestARefusedRecordNamesItsClassifiedCause(t *testing.T) {
	f := newRefusalFixture(t)
	withPackage := func(request *capabilityRequest) {
		request.Native, request.Packages = f.plan, []prerequisites.NativePackage{f.pkg}
	}
	withTool := func(request *capabilityRequest) { request.Tools = []prerequisites.ToolDefinition{f.tool} }
	refused := func(reason, source string) string {
		if source == "" {
			return `{"phase":"refused","reason":"` + reason + `"}\n`
		}
		return `{"phase":"refused","reason":"` + reason + `","source":"` + source + `"}\n`
	}
	staging := f.handoff + acknowledged(f.nativePrepared)
	for _, check := range []struct {
		name, script, code, message, outcome string
		request                              func(*capabilityRequest)
		routable                             bool
	}{
		{"a package source unresolved", staging + `printf '` + refused("dns", "native-one") + `' >&3; exit 2`, "controller.setup", "cdn.example.test could not be resolved by this host's configured resolver", "failed", withPackage, true},
		{"a package source unresolved, exit first", staging + afterTheExit(refused("dns", "native-one")), "controller.setup", "cdn.example.test could not be resolved by this host's configured resolver", "failed", withPackage, true},
		{"a source the run is not acquiring", staging + `printf '` + refused("dns", "native-two") + `' >&3; exit 2`, "controller.unknown", "the Ansible capability protocol was invalid", "failed", withPackage, false},
		{"a package source before its preparation", f.handoff + `printf '` + refused("dns", "native-one") + `' >&3; exit 2`, "controller.unknown", "the Ansible capability protocol was invalid", "failed", withPackage, false},
		{"a package source after its native record", staging + acknowledged(`{"phase":"native"}\n`) + `printf '` + refused("dns", "native-one") + `' >&3; exit 2`, "controller.unknown", "the Ansible capability protocol was invalid", "unknown", withPackage, false},
		{"a source other than the tool being installed", f.handoff + acknowledged(f.toolPrepare) + acknowledged(`{"phase":"continue"}\n`) + `printf '` + refused("timeout", "tool-other") + `' >&3; exit 2`, "controller.unknown", "the Ansible capability protocol was invalid", "unknown", withTool, false},
		{"an acquisition class without its source", staging + `printf '` + refused("dns", "") + `' >&3; exit 2`, "controller.unknown", "the Ansible capability protocol was invalid", "failed", withPackage, false},
		{"a tool transfer past its deadline", f.handoff + acknowledged(f.toolPrepare) + acknowledged(`{"phase":"continue"}\n`) + `printf '` + refused("timeout", "tool-helm") + `' >&3; exit 2`, "controller.setup", "get.example.test did not answer within its bounded acquisition deadline", "unknown", withTool, true},
		{"a native refusal before preparation", f.handoff + `printf '` + refused("database", "") + `' >&3; exit 2`, "controller.setup", "the native package database was busy or changed during the operation", "failed", withPackage, false},
		{"a native refusal after authorization", staging + acknowledged(`{"phase":"native"}\n`) + `printf '` + refused("solver-conflict", "") + `' >&3; exit 2`, "controller.unknown", "the native package solver found the requested dependencies in conflict with this host's installed packages", "unknown", withPackage, false},
		{"an internal native refusal", staging + `printf '` + refused("internal", "") + `' >&3; exit 2`, "controller.setup", "Ansible did not complete the authorized dependency operation", "failed", withPackage, false},
		{"a record after an internal refusal", staging + `printf '` + refused("internal", "") + refused("database", "") + `' >&3; exit 2`, "controller.unknown", "the Ansible capability protocol was invalid", "failed", withPackage, false},
		{"an unknown class", staging + `printf '` + refused("quota", "") + `' >&3; exit 2`, "controller.unknown", "the Ansible capability protocol was invalid", "failed", withPackage, false},
	} {
		t.Run(check.name, func(t *testing.T) {
			result, found := scriptedRun(t, check.script, check.request)
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

// A storage refusal names the filesystem its source was being written into:
// a package stages in the run's scratch, a tool is written into its
// publication bundle, so the remedy frees the one that filled.
func TestAStorageRefusalNamesTheFilesystemItsSourceFills(t *testing.T) {
	f := newRefusalFixture(t)
	scratch, bundle := t.TempDir(), t.TempDir()
	refused := func(source string) string {
		return `printf '{"phase":"refused","reason":"storage","source":"` + source + `"}\n' >&3; exit 2`
	}
	for _, check := range []struct {
		name, script, area, host string
		request                  func(*capabilityRequest)
	}{
		{"a package", f.handoff + acknowledged(f.nativePrepared) + refused("native-one"), scratch, "cdn.example.test", func(request *capabilityRequest) {
			request.Native, request.Packages = f.plan, []prerequisites.NativePackage{f.pkg}
		}},
		{"a tool", f.handoff + acknowledged(f.toolPrepare) + acknowledged(`{"phase":"continue"}\n`) + refused("tool-helm"), bundle, "get.example.test", func(request *capabilityRequest) {
			request.Tools = []prerequisites.ToolDefinition{f.tool}
			request.PublicationBundle = bundleLocation{Path: bundle, Writable: true}
		}},
	} {
		t.Run(check.name, func(t *testing.T) {
			_, found := scriptedRunUnder(t, scratch, check.script, check.request)
			if len(found) != 1 || found[0].Code != "controller.setup" ||
				found[0].Message != "the filesystem holding "+check.area+" could not hold the approved download from "+check.host ||
				!strings.HasPrefix(found[0].Remediation, "Free space on the filesystem holding "+check.area+", ") {
				t.Fatalf("the run reported %+v", found)
			}
		})
	}
}

// Setup's own run that fails after publishing its preparation but before Go
// acknowledged a native record authorized nothing, since the adapter waits for
// that acknowledgement before its transaction, so the action is failed and
// the next setup replaces it. Once the native record is acknowledged, or for
// a client installation, which publishes into another area, it stays unknown.
func TestASetupThatFailsBeforeItsNativeRecordIsFailed(t *testing.T) {
	f := newRefusalFixture(t)
	result, found := scriptedRun(t, f.handoff+acknowledged(f.nativePrepared)+`exit 2`, func(request *capabilityRequest) {
		request.Native, request.Packages = f.plan, []prerequisites.NativePackage{f.pkg}
	})
	if len(found) != 1 || found[0].Code != "controller.setup" || result.Outcome != "failed" || string(result.Evidence) != `{"intentRecorded":true,"postcondition":false}` {
		t.Fatalf("the run left %s %s with %+v, want failed with its intent recorded", result.Outcome, result.Evidence, found)
	}
}

func TestASetupThatFailsAfterItsNativeAcknowledgementStaysUnknown(t *testing.T) {
	f := newRefusalFixture(t)
	result, found := scriptedRun(t, f.handoff+acknowledged(f.nativePrepared)+acknowledged(`{"phase":"native"}\n`)+`exit 2`, func(request *capabilityRequest) {
		request.Native, request.Packages = f.plan, []prerequisites.NativePackage{f.pkg}
	})
	if len(found) != 1 || result.Outcome != "unknown" || string(result.Evidence) != `{"intentRecorded":true,"postcondition":false}` {
		t.Fatalf("the run left %s %s with %+v, want unknown", result.Outcome, result.Evidence, found)
	}
}

func TestAClientInstallationThatFailsBeforeNativeStaysUnknown(t *testing.T) {
	f := newRefusalFixture(t)
	result, found := scriptedRun(t, f.handoff+acknowledged(f.nativePrepared)+`exit 2`, func(request *capabilityRequest) {
		request.Native, request.Packages = f.plan, []prerequisites.NativePackage{f.pkg}
		request.PublicationBundle = bundleLocation{Path: request.Bundle.Path + "-clients", Writable: true}
	})
	if len(found) != 1 || result.Outcome != "unknown" {
		t.Fatalf("the client installation left %s with %+v, want unknown", result.Outcome, found)
	}
}
