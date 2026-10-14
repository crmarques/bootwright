package prerequisites

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestAWheelMirrorIsAFileDirectoryOrAnHTTPSBase(t *testing.T) {
	accepted := map[string][2]string{
		"":                                  {"", ""},
		"file:///srv/wheels":                {"file:///srv/wheels", "file:///srv/wheels/pip.whl"},
		"file:///srv/wheels/":               {"file:///srv/wheels", "file:///srv/wheels/pip.whl"},
		"file:///srv/a%20b/../wheels":       {"file:///srv/wheels", "file:///srv/wheels/pip.whl"},
		"https://mirror.example":            {"https://mirror.example", "https://mirror.example/pip.whl"},
		"https://mirror.example/a/b/":       {"https://mirror.example/a/b", "https://mirror.example/a/b/pip.whl"},
		"https://mirror.example:443/a":      {"https://mirror.example:443/a", "https://mirror.example:443/a/pip.whl"},
		"https://mirror.example/a//b/../c/": {"https://mirror.example/a/c", "https://mirror.example/a/c/pip.whl"},
		"https://mirror.example/a%3Fb":      {"https://mirror.example/a%3Fb", "https://mirror.example/a%3Fb/pip.whl"},
		"https://mirror.example/a%2Fb":      {"https://mirror.example/a%2Fb", "https://mirror.example/a%2Fb/pip.whl"},
		"https://mirror.example/a b":        {"https://mirror.example/a%20b", "https://mirror.example/a%20b/pip.whl"},
	}
	for raw, want := range accepted {
		mirror, err := ParseWheelMirror(raw)
		if err != nil || mirror.Set() != (raw != "") {
			t.Errorf("%q: %+v, %v", raw, mirror, err)
			continue
		}
		if raw != "" && (mirror.String() != want[0] || mirror.Location("pip.whl") != want[1]) {
			t.Errorf("%q: String %q Location %q, want %q and %q", raw, mirror.String(), mirror.Location("pip.whl"), want[0], want[1])
		}
	}
	for _, raw := range []string{
		"http://mirror.example/wheels", "ftp://mirror.example", "wheels", "/srv/wheels", "file://host/srv/wheels", "file://localhost/srv/wheels",
		"file:wheels", "file://", "https://", "https:///wheels", "https://user@mirror.example/wheels", "https://user:secret@mirror.example",
		"https://mirror.example:8443/wheels", "https://mirror.example/wheels?token=x", "https://mirror.example/wheels?", "https://mirror.example/wheels#x",
		"https:mirror.example", "  https://mirror.example", "https://mirror.example/\x00",
	} {
		if mirror, err := ParseWheelMirror(raw); err == nil || mirror.Set() || !strings.Contains(err.Error(), "file:///") {
			t.Errorf("%q was admitted (%+v, %v)", raw, mirror, err)
		}
	}
}

func TestAMirrorEscapesTheFilenameItReads(t *testing.T) {
	mirror, err := ParseWheelMirror("https://mirror.example/base")
	if err != nil {
		t.Fatal(err)
	}
	if got := mirror.Location("a b+c.whl"); got != "https://mirror.example/base/a%20b+c.whl" {
		t.Fatalf("Location = %q", got)
	}
	if _, local := mirror.Directory(); local {
		t.Fatal("an HTTPS mirror reported a directory")
	}
}

func TestSetupReadsTheMirrorAndTheRuntimeActionNamesIt(t *testing.T) {
	f := newSetupFixture(t)
	f.python.ready = false
	report, err := f.service.Setup(context.Background(), SetupRequest{WheelMirror: "file:///srv/wheels"})
	if err != nil || report.Outcome != "changed" {
		t.Fatalf("Setup = %+v, %v", report, err)
	}
	if !slices.Equal(f.python.mirrors, []string{"file:///srv/wheels"}) {
		t.Fatalf("the runtime fetched through %v", f.python.mirrors)
	}
	if !slices.ContainsFunc(f.plan.Actions, func(action string) bool {
		return strings.HasSuffix(action, ", read from the mirror file:///srv/wheels")
	}) {
		t.Fatalf("the plan %v does not name the mirror", f.plan.Actions)
	}
}

func TestSetupWithoutAMirrorFetchesFromTheLock(t *testing.T) {
	f := newSetupFixture(t)
	f.python.ready = false
	if _, err := f.service.Setup(context.Background(), SetupRequest{}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.python.mirrors, []string{""}) || slices.ContainsFunc(f.plan.Actions, func(action string) bool { return strings.Contains(action, "mirror") }) {
		t.Fatalf("a setup with no mirror fetched through %v with the plan %v", f.python.mirrors, f.plan.Actions)
	}
}

func TestADryRunNamesTheMirrorAndStillListsTheLocksURLs(t *testing.T) {
	f := newSetupFixture(t)
	report, err := f.service.Setup(context.Background(), SetupRequest{DryRun: true, WheelMirror: "https://mirror.example/wheels"})
	if err != nil || report.Outcome != "planned" {
		t.Fatalf("Setup = %+v, %v", report, err)
	}
	if !slices.ContainsFunc(report.Actions, func(action string) bool {
		return strings.HasSuffix(action, ", read from the mirror https://mirror.example/wheels")
	}) {
		t.Fatalf("the dry run %v does not name the mirror", report.Actions)
	}
	if !slices.Equal(report.Files, testLock.Files) {
		t.Fatalf("the dry run lists %+v, want the lock's files", report.Files)
	}
	if len(f.effects()) != 0 || f.count("export") != 0 || f.count("fetch") != 0 {
		t.Fatalf("a dry run ran %v", f.events)
	}
}

func TestAnInvalidMirrorRefusesBeforeAnyInspection(t *testing.T) {
	for _, request := range []SetupRequest{
		{WheelMirror: "http://mirror.example"}, {DryRun: true, WheelMirror: "https://user@mirror.example"},
		{DownloadOnly: true, Destination: "out", WheelMirror: "wheels"},
	} {
		f := newSetupFixture(t)
		report, err := f.service.Setup(context.Background(), request)
		reported := diagnostics.Of(err)
		if report != nil || len(reported) != 1 || reported[0].Code != "controller.setup" || !strings.Contains(reported[0].Remediation, WheelMirrorUsage) {
			t.Fatalf("%+v: %+v, %v", request, report, err)
		}
		if len(f.events) != 0 {
			t.Fatalf("%+v ran %v before refusing", request, f.events)
		}
	}
}

func TestDownloadOnlyTouchesNoHostPortAndElevatesNothing(t *testing.T) {
	f := newSetupFixture(t)
	service := New(nil, nil, nil, Ports{Python: f.python, Lock: testLock}, Options{AmbientRoute: controller.Route{}})
	report, err := service.Setup(context.Background(), SetupRequest{DownloadOnly: true, Destination: "wheels", WheelMirror: "file:///srv/wheels"})
	if err != nil || report.Outcome != "downloaded" || report.Export == nil || report.Export.Directory != "wheels" || report.DryRun {
		t.Fatalf("Setup = %+v, %v", report, err)
	}
	if !slices.Equal(f.events, []string{"export"}) || !slices.Equal(f.python.exported, []string{"wheels file:///srv/wheels"}) {
		t.Fatalf("a download-only setup ran %v (%v)", f.events, f.python.exported)
	}
	if len(f.store.recorded) != 0 || len(f.packages.present) != 2 {
		t.Fatal("a download-only setup changed host or store state")
	}
}

func TestDownloadOnlyRefusesWhatItCannotDo(t *testing.T) {
	for name, request := range map[string]SetupRequest{
		"no directory": {DownloadOnly: true},
		"a dry run":    {DownloadOnly: true, DryRun: true, Destination: "wheels"},
	} {
		f := newSetupFixture(t)
		report, err := f.service.Setup(context.Background(), request)
		reported := diagnostics.Of(err)
		if report != nil || len(reported) != 1 || reported[0].Code != "controller.setup" || reported[0].Remediation == "" || len(f.events) != 0 {
			t.Errorf("%s: %+v, %v, %v", name, report, err, f.events)
		}
	}
}

func TestDownloadOnlyWithoutTheLockIsUnavailable(t *testing.T) {
	service := New(nil, nil, nil, Ports{}, Options{})
	if report, err := service.Setup(context.Background(), SetupRequest{DownloadOnly: true, Destination: "wheels"}); report != nil || err == nil {
		t.Fatalf("Setup = %+v, %v", report, err)
	}
}
