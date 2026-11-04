package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	stateencoding "github.com/crmarques/bootwright/internal/desiredstate/encoding"
	"github.com/crmarques/bootwright/internal/diagnostics"
	artifactrendering "github.com/crmarques/bootwright/internal/nativeartifacts/rendering"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

type comparisonSpy struct {
	directory string
	tree      []artifactrendering.TreeFile
	program   string
	pairs     []artifactrendering.ComparisonPair
	writeErr  error
	toolErr   error
	ran       int
}

func (s *comparisonSpy) WriteTrees(_ context.Context, directory string, files []artifactrendering.TreeFile) (string, error) {
	s.directory, s.tree = directory, files
	if s.writeErr != nil {
		return "", s.writeErr
	}
	return "/resolved/" + directory, nil
}

func (s *comparisonSpy) RunTool(_ context.Context, program string, files []artifactrendering.TreeFile, pairs []artifactrendering.ComparisonPair) error {
	s.ran++
	s.program, s.tree, s.pairs = program, files, pairs
	return s.toolErr
}

func (s *comparisonSpy) file(path string) (string, bool) {
	for _, file := range s.tree {
		if file.Path == path {
			return string(file.Data), true
		}
	}
	return "", false
}

type diffRun struct {
	code        int
	out, errOut string
}

func runDiff(result *lifecycle.DiffResult, presentation PresentationOptions, spy *comparisonSpy, args ...string) diffRun {
	record := &dispatchRecord{}
	record.result.lifecycleDiff = result
	services := dispatchSpies(record)
	if spy != nil {
		services.Comparison = spy
	}
	var out, errOut bytes.Buffer
	code := New(Config{
		Out: &out, ErrOut: &errOut, Services: services, Presentation: presentation,
		EncodeEffectiveYAML: stateencoding.YAML, EncodeEffectiveJSON: stateencoding.JSON,
	}).Run(context.Background(), append([]string{"diff"}, args...))
	return diffRun{code, out.String(), errOut.String()}
}

func terminal() PresentationOptions { return PresentationOptions{Columns: func() int { return 80 }} }

func TestDiffFilesHoldEveryObjectOfAFileSeparatedByDocumentLines(t *testing.T) {
	spy := &comparisonSpy{}
	run := runDiff(diffFiled(), PresentationOptions{}, spy, "--output-dir", "cmp")
	if run.code != 0 || run.errOut != "" {
		t.Fatalf("exit %d, stderr %q", run.code, run.errOut)
	}
	desired, _ := spy.file("desired/storage/pools.yaml")
	if strings.Count(desired, "\n---\n") != 1 || !strings.HasPrefix(desired, "apiVersion: ") || strings.Index(desired, "rbd-archive") > strings.Index(desired, "rbd-data") {
		t.Fatalf("the desired pools file does not hold its two objects in name order, split by one --- line:\n%s", desired)
	}
	applied, _ := spy.file("applied/storage/clients.yaml")
	if strings.Count(applied, "\n---\n") != 2 {
		t.Fatalf("the applied clients file does not hold its three objects:\n%s", applied)
	}
	if _, found := spy.file("applied/storage/pools.yaml"); found {
		t.Fatal("a side that holds no object of a file was written for it")
	}
}

func TestDiffMovedObjectLeavesOneFileAndJoinsTheOther(t *testing.T) {
	spy := &comparisonSpy{}
	runDiff(diffFiled(), PresentationOptions{}, spy, "--output-dir", "cmp")
	left, _ := spy.file("desired/storage/pools.yaml")
	joined, found := spy.file("applied/storage/archive.yaml")
	if !strings.Contains(left, "name: rbd-data") || !found || !strings.Contains(joined, "name: rbd-data") {
		t.Fatalf("the moved object is not in the desired file %q and the applied file %q", left, joined)
	}
	if _, found := spy.file("desired/storage/archive.yaml"); found {
		t.Fatal("the desired side has a file the selected revision does not")
	}
	text := runDiff(diffFiled(), PresentationOptions{}, nil).out
	for _, want := range []string{
		"diff desired/storage/archive.yaml applied/storage/archive.yaml\n# CephPool/rbd-data: plan: changed (ceph-pool-rbd-data); realized: done\n--- /dev/null\n+++ applied/storage/archive.yaml\n@@ -0,0 +1,12 @@\n",
		"--- desired/storage/pools.yaml\n+++ /dev/null\n@@ -1,25 +0,0 @@\n",
		"# CephClient/rbd-stray: plan: none\n",
		"4 objects differ: 1 changed, 1 added, 1 removed, 1 unmanaged, 0 unknown; 1 unchanged\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("output lacks %q:\n%s", want, text)
		}
	}
}

func TestDiffUnmanagedObjectsCountAsDifferingForExitCode(t *testing.T) {
	result := diffFiled()
	result.Objects = []lifecycle.ObjectDiff{result.Objects[3]}
	run := runDiff(result, PresentationOptions{}, nil, "--exit-code")
	if run.code != 3 || run.errOut != "" || !strings.HasSuffix(run.out, "1 object differs: 0 changed, 0 added, 0 removed, 1 unmanaged, 0 unknown; 0 unchanged\n") {
		t.Fatalf("exit %d, stderr %q, stdout %q", run.code, run.errOut, run.out)
	}
	var envelope struct {
		Result struct {
			Objects []struct{ Difference string }
			Summary diffSummary
		}
		ExitCode int
	}
	run = runDiff(result, PresentationOptions{}, nil, "--exit-code", "--output", "json")
	if err := json.Unmarshal([]byte(run.out), &envelope); err != nil {
		t.Fatal(err)
	}
	if run.code != 3 || envelope.Result.Objects[0].Difference != differenceUnmanaged || envelope.Result.Summary.Unmanaged != 1 {
		t.Fatalf("exit %d, envelope %+v", run.code, envelope)
	}
}

func TestDiffNotComparedNamesAtMostTwelvePaths(t *testing.T) {
	result := diffObserved()
	var paths []string
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o"} {
		paths = append(paths, "spec."+name)
	}
	result.Objects[0].Real.NotCompared = paths
	text := runDiff(result, PresentationOptions{}, nil, "--observe").out
	want := "# Machine/rhel-01: not compared: spec.a, spec.b, spec.c, spec.d, spec.e, spec.f, spec.g, spec.h, spec.i, spec.j, spec.k, spec.l and 3 more\n"
	if !strings.Contains(text, want) {
		t.Fatalf("output lacks %q:\n%s", want, text)
	}
}

func TestDiffColorFollowsTheTerminalTheEnvironmentAndTheFlag(t *testing.T) {
	for _, test := range []struct {
		name         string
		presentation PresentationOptions
		args         []string
		colored      bool
	}{
		{"auto without a terminal", PresentationOptions{}, nil, false},
		{"auto on a terminal", terminal(), nil, true},
		{"auto on a terminal with NO_COLOR", PresentationOptions{Columns: terminal().Columns, NoColor: true}, nil, false},
		{"auto on a dumb terminal", PresentationOptions{Columns: terminal().Columns, Dumb: true}, nil, false},
		{"always without a terminal", PresentationOptions{}, []string{"--color", "always"}, true},
		{"never on a terminal", terminal(), []string{"--color", "never"}, false},
		{"always in JSON", terminal(), []string{"--color", "always", "--output", "json"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := runDiff(diffMixed(), test.presentation, nil, test.args...)
			if got := strings.Contains(run.out, "\x1b["); got != test.colored || run.code != 0 {
				t.Fatalf("colored = %t (exit %d), want %t:\n%q", got, run.code, test.colored, run.out)
			}
		})
	}
}

func TestDiffColorStylesEachLineKindOnce(t *testing.T) {
	out := runDiff(diffMixed(), PresentationOptions{}, nil, "--color", "always").out
	plain := runDiff(diffMixed(), PresentationOptions{}, nil, "--color", "never").out
	var stripped strings.Builder
	for _, line := range strings.Split(out, "\n") {
		for _, code := range []string{"1", "2", "31", "32", "36"} {
			if body, found := strings.CutPrefix(line, "\x1b["+code+"m"); found {
				line, _ = strings.CutSuffix(body, "\x1b[0m")
				want := map[string]func(string) bool{
					"1": func(l string) bool {
						return strings.HasPrefix(l, "diff ") || strings.HasPrefix(l, "--- ") || strings.HasPrefix(l, "+++ ")
					},
					"2":  func(l string) bool { return strings.HasPrefix(l, "# ") },
					"31": func(l string) bool { return strings.HasPrefix(l, "-") },
					"32": func(l string) bool { return strings.HasPrefix(l, "+") },
					"36": func(l string) bool { return strings.HasPrefix(l, "@@ ") },
				}
				if !want[code](line) {
					t.Errorf("line %q carries style %s", line, code)
				}
				break
			}
		}
		stripped.WriteString(line + "\n")
	}
	if strings.TrimSuffix(stripped.String(), "\n") != plain {
		t.Fatalf("styling changed the text:\n%s\nversus\n%s", stripped.String(), plain)
	}
}

func TestDiffOutputDirWritesEveryFileOfBothSidesWithMarkersAndNotes(t *testing.T) {
	spy := &comparisonSpy{}
	run := runDiff(diffFiled(), PresentationOptions{}, spy, "--output-dir", "cmp")
	if run.code != 0 || spy.directory != "cmp" || !strings.HasSuffix(run.out, "\nWrote /resolved/cmp\n") {
		t.Fatalf("exit %d, directory %q, stdout %q", run.code, spy.directory, run.out)
	}
	var paths []string
	for _, file := range spy.tree {
		paths = append(paths, file.Path)
	}
	want := []string{
		"applied/storage/archive.yaml", "applied/storage/clients.yaml",
		"desired/storage/clients.yaml", "desired/storage/pools.yaml",
		"desired/storage/.keep", "applied/storage/.keep", "notes.txt",
	}
	slices.Sort(paths)
	slices.Sort(want)
	if !slices.Equal(paths, want) {
		t.Fatalf("tree files %q, want %q", paths, want)
	}
	if kept, _ := spy.file("desired/storage/.keep"); kept != "desired\n" {
		t.Fatalf("the desired marker is %q", kept)
	}
	if kept, _ := spy.file("applied/storage/.keep"); kept != "applied\n" {
		t.Fatalf("the applied marker is %q", kept)
	}
	notes, _ := spy.file("notes.txt")
	if !strings.HasPrefix(notes, "Context:  lab at input revision ") || !strings.Contains(notes, "# CephClient/rbd-stray: plan: none\n") || strings.Contains(notes, "\n\n") || strings.Contains(notes, "--- ") {
		t.Fatalf("notes.txt is not the header and the # lines:\n%s", notes)
	}
}

func TestDiffOutputDirUsesTheRealTreeWithObserve(t *testing.T) {
	spy := &comparisonSpy{}
	runDiff(diffObserved(), PresentationOptions{}, spy, "--observe", "--output-dir", "cmp")
	if _, found := spy.file("real/infra/provider.yaml"); !found {
		t.Fatalf("no real tree in %v", spy.tree)
	}
}

func TestDiffOutputDirRefusalWritesNothingToStandardOutput(t *testing.T) {
	spy := &comparisonSpy{writeErr: diagnostics.NewFailureWithRemediation("render.publish", "cmp is not empty", "", "pass a directory that does not exist yet or is empty")}
	run := runDiff(diffRecorded(), PresentationOptions{}, spy, "--output-dir", "cmp")
	if run.code != 1 || run.out != "" || !strings.Contains(run.errOut, "render.publish") || !strings.Contains(run.errOut, "cmp is not empty") {
		t.Fatalf("exit %d, stdout %q, stderr %q", run.code, run.out, run.errOut)
	}
}

func TestDiffToolRunsOverEachListedPairWithAnEmptyFileForAMissingSide(t *testing.T) {
	spy := &comparisonSpy{}
	run := runDiff(diffFiled(), terminal(), spy, "--tool", "vimdiff")
	if run.code != 0 || run.errOut != "" || spy.ran != 1 || spy.program != "vimdiff" {
		t.Fatalf("exit %d, stderr %q, runs %d, program %q", run.code, run.errOut, spy.ran, spy.program)
	}
	want := []artifactrendering.ComparisonPair{
		{Desired: "desired/storage/archive.yaml", Other: "applied/storage/archive.yaml"},
		{Desired: "desired/storage/clients.yaml", Other: "applied/storage/clients.yaml"},
		{Desired: "desired/storage/pools.yaml", Other: "applied/storage/pools.yaml"},
	}
	if !slices.Equal(spy.pairs, want) {
		t.Fatalf("pairs %v, want %v", spy.pairs, want)
	}
	if empty, found := spy.file("desired/storage/archive.yaml"); !found || empty != "" {
		t.Fatalf("the missing desired side is %q (found %t), want an empty file", empty, found)
	}
	if empty, found := spy.file("applied/storage/pools.yaml"); !found || empty != "" {
		t.Fatalf("the missing applied side is %q (found %t), want an empty file", empty, found)
	}
	if !strings.HasSuffix(run.out, "1 unmanaged, 0 unknown; 1 unchanged\n") {
		t.Fatalf("the result was not written before the tool ran: %q", run.out)
	}
}

func TestDiffToolSkipsFilesThatAreNotListed(t *testing.T) {
	spy := &comparisonSpy{}
	runDiff(diffObserved(), terminal(), spy, "--observe", "--tool", "meld")
	for _, pair := range spy.pairs {
		if strings.Contains(pair.Desired, "storage/clients.yaml") {
			t.Fatalf("an unlisted file was compared: %v", spy.pairs)
		}
	}
	if len(spy.pairs) != 2 {
		t.Fatalf("pairs %v, want the two listed files", spy.pairs)
	}
}

func TestDiffToolRefusals(t *testing.T) {
	for _, test := range []struct {
		name         string
		presentation PresentationOptions
		args         []string
		want         string
	}{
		{"without a terminal", PresentationOptions{}, []string{"--tool", "vimdiff"}, "--tool requires a terminal on standard output"},
		{"with an output directory", terminal(), []string{"--tool", "vimdiff", "--output-dir", "cmp"}, "--tool conflicts with --output-dir"},
		{"with JSON", terminal(), []string{"--tool", "vimdiff", "--output", "json"}, "--tool requires --output text"},
	} {
		t.Run(test.name, func(t *testing.T) {
			spy := &comparisonSpy{}
			run := runDiff(diffRecorded(), test.presentation, spy, test.args...)
			if run.code != 2 || !strings.Contains(run.out+run.errOut, test.want) || spy.ran != 0 || (run.out != "" && !strings.Contains(test.name, "JSON")) {
				t.Fatalf("exit %d, stdout %q, stderr %q, runs %d", run.code, run.out, run.errOut, spy.ran)
			}
		})
	}
}

func TestDiffToolThatCannotBeStartedRefusesAfterTheResult(t *testing.T) {
	spy := &comparisonSpy{toolErr: diagnostics.NewFailureWithRemediation("runtime.tool", "the comparison program nodiff was not found on PATH", "", "pass --tool a program on PATH")}
	run := runDiff(diffRecorded(), terminal(), spy, "--tool", "nodiff")
	if run.code != 1 || !strings.Contains(run.errOut, "runtime.tool") || !strings.Contains(run.errOut, "nodiff") || !strings.HasSuffix(run.out, "1 unchanged\n") {
		t.Fatalf("exit %d, stdout %q, stderr %q", run.code, run.out, run.errOut)
	}
}

func TestDiffTreesNeedAConfiguredComparisonService(t *testing.T) {
	run := runDiff(diffRecorded(), PresentationOptions{}, nil, "--output-dir", "cmp")
	if run.code != 1 || run.out != "" || !strings.Contains(run.errOut, "runtime.internal") {
		t.Fatalf("exit %d, stdout %q, stderr %q", run.code, run.out, run.errOut)
	}
	var failure *resultFailure
	err := writeDiff(context.Background(), &bytes.Buffer{}, diffRecorded(), false, stateencoding.YAML, stateencoding.JSON)
	if errors.As(err, &failure) {
		t.Fatalf("a plain comparison needs no comparison service: %v", err)
	}
}

func TestDiffFallbackPathIsOneCleanElement(t *testing.T) {
	for name, want := range map[string]string{
		"rbd-x": "CephClient-rbd-x.yaml", "": "", ".": "", "..": "", "../escape": "", "a/b": "", `a\b`: "", "a\x00b": "",
	} {
		if got := diffFallback(lifecycle.ObjectDiff{Kind: api.CephClient, Name: name}); got != want {
			t.Errorf("fallback of %q = %q, want %q", name, got, want)
		}
	}
	for _, object := range []lifecycle.ObjectDiff{
		{Kind: api.CephClient, Name: "../escape", Desired: diffClient("x"), Real: lifecycle.RealSide{Source: "applied", State: "absent"}},
		{Kind: api.CephClient, Name: "a/b", Real: lifecycle.RealSide{Source: "applied", State: "present", Object: diffClient("x")}},
		{Kind: api.CephClient, Name: "..", Real: lifecycle.RealSide{Source: "applied", State: "absent"}},
	} {
		files, err := diffFiles(context.Background(), []diffRow{{object: object, difference: differenceUnmanaged}}, "applied", stateencoding.YAML)
		var failure *resultFailure
		if !errors.As(err, &failure) || failure.code != "runtime.internal" || files != nil {
			t.Errorf("%q: files %v, error %v", object.Name, files, err)
		}
	}
	files, err := diffFiles(context.Background(), []diffRow{{object: lifecycle.ObjectDiff{Kind: api.CephClient, Name: "rbd-x", Desired: diffClient("rbd-x"), Real: lifecycle.RealSide{Source: "applied", State: "absent"}}, difference: differenceAdded}}, "applied", stateencoding.YAML)
	if err != nil || len(files) != 1 || files[0].path != "CephClient-rbd-x.yaml" {
		t.Fatalf("files %v, error %v", files, err)
	}
}
