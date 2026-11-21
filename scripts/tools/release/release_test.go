package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const head = "0123456789abcdef0123456789abcdef01234567"

type fakeCommands struct {
	status, ignored string
	version, sbom   string
	build           func(environment []string, arguments []string) error
	environments    [][]string
	arguments       [][]string
}

func (f *fakeCommands) output(name string, arguments ...string) ([]byte, error) {
	if name == "git" {
		switch strings.Join(arguments, " ") {
		case "rev-parse HEAD":
			return []byte(head + "\n"), nil
		case "status --porcelain":
			return []byte(f.status), nil
		case "status --porcelain --ignored -- ansible/collections":
			return []byte(f.ignored), nil
		}
		return nil, errors.New("unexpected git call")
	}
	switch arguments[0] {
	case "version":
		return []byte(f.version), nil
	case "sbom":
		return []byte(f.sbom), nil
	}
	return nil, errors.New("unexpected executable call")
}

func (f *fakeCommands) run(_ context.Context, environment []string, _, _ io.Writer, _ string, arguments ...string) error {
	f.environments = append(f.environments, environment)
	f.arguments = append(f.arguments, arguments)
	if f.build == nil {
		return nil
	}
	return f.build(environment, arguments)
}

func goodConfig() config {
	return config{
		MakefileList: "Makefile", Inputs: "VERSION COMMIT", Vulncheck: "on", Build: "container", Version: "v1.2.3", Commit: head, Source: "clean",
		Dir: "dist", Make: "make", Files: "bootwright bootwright.cdx.json bootwright.notices.txt", Environment: "PATH HOME",
	}
}

func freshVerdict() string {
	return "passed " + time.Now().UTC().Format(time.DateOnly)
}

func envelope(version, check, source string) string {
	return `{"schemaVersion":"v1","ok":true,"result":{"version":"` + version + `","commit":"` + head + `","source":"` + source + `","vulnerabilityCheck":"` + check + `"},"diagnostics":[]}`
}

func goodCommands() *fakeCommands {
	bill := "{\"bom\":\"one\"}\n"
	digest := sha256.Sum256([]byte(bill))
	f := &fakeCommands{version: envelope("v1.2.3", freshVerdict(), "clean"), sbom: bill}
	f.build = func(_ []string, arguments []string) error {
		var stage string
		for _, argument := range arguments {
			if value, found := strings.CutPrefix(argument, "CONTAINER_OUT="); found {
				stage = value
			}
		}
		for name, content := range map[string]string{
			"bootwright":             "executable",
			"bootwright.cdx.json":    bill,
			"bootwright.notices.txt": "Third-party notices\n\nBill of materials: sha256:" + hex.EncodeToString(digest[:]) + "\n",
		} {
			if err := os.WriteFile(filepath.Join(stage, name), []byte(content), 0o755); err != nil {
				return err
			}
		}
		return nil
	}
	return f
}

func runRelease(t *testing.T, c config, f *fakeCommands, environ ...string) error {
	t.Helper()
	return release(context.Background(), c, f, environ, io.Discard, io.Discard)
}

func listing(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestAGoodReleaseShipsTheFourFilesAndNothingElse(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := runRelease(t, goodConfig(), goodCommands()); err != nil {
		t.Fatal(err)
	}
	if got, want := listing(t, "dist"), []string{"SHA256SUMS", "bootwright", "bootwright.cdx.json", "bootwright.notices.txt"}; !slices.Equal(got, want) {
		t.Fatalf("dist holds %v, want %v", got, want)
	}
	sums, _ := os.ReadFile("dist/SHA256SUMS")
	for _, name := range []string{"bootwright", "bootwright.cdx.json", "bootwright.notices.txt"} {
		data, _ := os.ReadFile(filepath.Join("dist", name))
		digest := sha256.Sum256(data)
		if !strings.Contains(string(sums), hex.EncodeToString(digest[:])+"  "+name+"\n") {
			t.Errorf("SHA256SUMS lacks the digest of %s:\n%s", name, sums)
		}
	}
}

func TestEachInvocationRefusalNamesItsCause(t *testing.T) {
	for name, test := range map[string]struct {
		change func(*config)
		want   string
	}{
		"make flags":         {func(c *config) { c.MakeFlags = "n" }, "make was run with -n"},
		"a second makefile":  {func(c *config) { c.MakefileList = "Makefile other.mk" }, "reads more than the Makefile (Makefile other.mk)"},
		"no makefile":        {func(c *config) { c.MakefileList = "" }, "reads more than the Makefile"},
		"an eval":            {func(c *config) { c.Eval = true }, "Makefile and --eval)"},
		"a foreign variable": {func(c *config) { c.Foreign = listFlag{"STAMP=command line", "GO=environment override"} }, "STAMP comes from the command line; GO comes from the environment override; each could change"},
		"an unpinned image":  {func(c *config) { c.GoImage = "golang:1.26" }, "is not pinned by @sha256:"},
		"a short digest":     {func(c *config) { c.GoImage = "golang:1.26@sha256:abc" }, "is not pinned by @sha256:"},
		"a whitespace image": {func(c *config) { c.GoImage = "golang:1.26@sha256:" + strings.Repeat("a", 64) + " x" }, "is not pinned by @sha256:"},
		"skipped scanning":   {func(c *config) { c.Vulncheck = "skip" }, "VULNCHECK=skip is refused"},
		"an empty gate":      {func(c *config) { c.Vulncheck = "" }, "VULNCHECK= is refused"},
		"another build":      {func(c *config) { c.Build = "docker" }, "neither native nor container"},
		"no version":         {func(c *config) { c.Version = "" }, "name the release with VERSION"},
		"a dirty version":    {func(c *config) { c.Version = "v1-dirty" }, "names a dirty tree"},
		"another commit":     {func(c *config) { c.Commit = strings.Repeat("a", 40) }, "is not the checkout's commit " + head},
		"a modified source":  {func(c *config) { c.Source = "modified" }, "SOURCE=modified is not clean"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			c := goodConfig()
			test.change(&c)
			f := goodCommands()
			err := runRelease(t, c, f)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err = %v, want %q", err, test.want)
			}
			if len(f.arguments) != 0 {
				t.Errorf("a refused release started the build: %v", f.arguments)
			}
			if _, statErr := os.Stat("dist"); statErr == nil {
				t.Errorf("a refusal before the build created the release directory")
			}
		})
	}
}

func TestADirtyTreeIsRefusedBeforeAndAfterTheBuild(t *testing.T) {
	t.Chdir(t.TempDir())
	f := goodCommands()
	f.status = " M Makefile\n"
	if err := runRelease(t, goodConfig(), f); err == nil || !strings.Contains(err.Error(), "modified or untracked files") || !strings.Contains(err.Error(), "M Makefile") {
		t.Fatalf("err = %v", err)
	}
	if len(f.arguments) != 0 {
		t.Fatal("the build started on a dirty tree")
	}
	grow := &fakeCommands{version: goodCommands().version, sbom: goodCommands().sbom}
	inner := goodCommands().build
	grow.build = func(environment, arguments []string) error {
		grow.status = "?? extra.go\n"
		return inner(environment, arguments)
	}
	t.Chdir(t.TempDir())
	if err := runRelease(t, goodConfig(), grow); err == nil || !strings.Contains(err.Error(), "extra.go") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat("dist/bootwright"); err == nil {
		t.Fatal("a tree that changed during the build was shipped")
	}
	if names := listing(t, "dist"); len(names) != 0 {
		t.Fatalf("a refused release left %v", names)
	}
}

func TestOnTheHostAnIgnoredFileUnderTheCollectionIsRefused(t *testing.T) {
	t.Chdir(t.TempDir())
	c := goodConfig()
	c.Build = "native"
	f := goodCommands()
	f.ignored = "!! ansible/collections/x/stray.py\n"
	if err := runRelease(t, c, f); err == nil || !strings.Contains(err.Error(), "would be embedded") {
		t.Fatalf("err = %v", err)
	}
	c.Build = "container"
	if err := runRelease(t, c, f); err != nil {
		t.Fatalf("a container release does not embed the host's files: %v", err)
	}
}

func TestTheBuildRunsInACleanEnvironmentThatKeepsOnlyTheListedVariables(t *testing.T) {
	t.Chdir(t.TempDir())
	f := goodCommands()
	if err := runRelease(t, goodConfig(), f, "PATH=/usr/bin", "HOME=/home/x", "RELEASE_STRAY=1", "GOFLAGS=-overlay=/x", "HTTPS_PROXY=http://p"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"PATH=/usr/bin", "HOME=/home/x", "GOENV=off"}; !slices.Equal(f.environments[0], want) {
		t.Errorf("the build ran with %v, want %v", f.environments[0], want)
	}
	if want := []string{"--no-print-directory", "container-build", "VULNCHECK=on", "VERSION=v1.2.3", "COMMIT=" + head, "SOURCE=clean"}; !slices.Equal(f.arguments[0][:6], want) {
		t.Errorf("the build ran %v, want it to start with %v", f.arguments[0], want)
	}
}

func TestOnTheHostTheBuildReadsTheModuleGraphOnlyAndCopiesWhatItBuilt(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("bin", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("bin/bootwright", []byte("stale"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := goodConfig()
	c.Build = "native"
	built := goodCommands()
	bill := built.sbom
	digest := sha256.Sum256([]byte(bill))
	f := &fakeCommands{version: built.version, sbom: bill}
	f.build = func(environment, arguments []string) error {
		if _, err := os.Stat("bin/bootwright"); err == nil {
			t.Error("a stale executable survived into the build")
		}
		for name, content := range map[string]string{"bootwright": "fresh", "bootwright.cdx.json": bill,
			"bootwright.notices.txt": "Bill of materials: sha256:" + hex.EncodeToString(digest[:]) + "\n"} {
			if err := os.WriteFile(filepath.Join("bin", name), []byte(content), 0o755); err != nil {
				return err
			}
		}
		return nil
	}
	if err := runRelease(t, c, f, "PATH=/usr/bin"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(f.environments[0], "GOFLAGS=-mod=readonly") || !slices.Contains(f.environments[0], "GOENV=off") {
		t.Errorf("the host build ran with %v", f.environments[0])
	}
	if data, _ := os.ReadFile("dist/bootwright"); string(data) != "fresh" {
		t.Errorf("the shipped executable is %q", data)
	}
	if info, err := os.Stat("dist/bootwright"); err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Errorf("the shipped executable lost its mode: %v", err)
	}
}

func TestEachStagedFileIsHeldToItsCheckAndARefusedReleaseLeavesThePreviousWhole(t *testing.T) {
	previous := map[string]string{"bootwright": "old executable", "bootwright.cdx.json": "old bill", "bootwright.notices.txt": "old notices", "SHA256SUMS": "old sums"}
	for name, test := range map[string]struct {
		change func(*fakeCommands)
		want   string
	}{
		"no pass":            {func(f *fakeCommands) { f.version = envelope("v1.2.3", "unchecked", "clean") }, "not a pass"},
		"a failed pass":      {func(f *fakeCommands) { f.version = envelope("v1.2.3", "failed 2026-11-10", "clean") }, "not a pass"},
		"a stale pass":       {func(f *fakeCommands) { f.version = envelope("v1.2.3", "passed 2000-01-01", "clean") }, "fresh UTC-day verdict"},
		"a pass with text":   {func(f *fakeCommands) { f.version = envelope("v1.2.3", "passed 2026-11-10 and more", "clean") }, "not a pass"},
		"another version":    {func(f *fakeCommands) { f.version = envelope("v9.9.9", freshVerdict(), "clean") }, "reports version 'v9.9.9'"},
		"a modified source":  {func(f *fakeCommands) { f.version = envelope("v1.2.3", freshVerdict(), "modified") }, "reports source 'modified'"},
		"not JSON":           {func(f *fakeCommands) { f.version = "version v1.2.3" }, "is not JSON"},
		"no result":          {func(f *fakeCommands) { f.version = `{"ok":true}` }, "not a pass"},
		"another bill":       {func(f *fakeCommands) { f.sbom = "{\"bom\":\"two\"}\n" }, "is not the bill of materials of this executable"},
		"no pinned notices":  {func(f *fakeCommands) { f.build = withNotices("Third-party notices\n") }, "was not composed from this bill"},
		"a notice substring": {func(f *fakeCommands) { f.build = withNotices("x Bill of materials: sha256:" + digestOf(f.sbom) + "\n") }, "was not composed from this bill"},
		"an empty file":      {func(f *fakeCommands) { f.build = stage(map[string]string{"bootwright.cdx.json": ""}) }, "is missing or empty"},
		"a missing file":     {func(f *fakeCommands) { f.build = stage(map[string]string{"bootwright.notices.txt": "-"}) }, "is missing or empty"},
		"a failed build":     {func(f *fakeCommands) { f.build = func([]string, []string) error { return errors.New("exit status 2") } }, "the build failed"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			if err := os.MkdirAll("dist", 0o755); err != nil {
				t.Fatal(err)
			}
			for file, text := range previous {
				if err := os.WriteFile(filepath.Join("dist", file), []byte(text), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			f := goodCommands()
			test.change(f)
			err := runRelease(t, goodConfig(), f)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("err = %v, want %q", err, test.want)
			}
			if got, want := listing(t, "dist"), []string{"SHA256SUMS", "bootwright", "bootwright.cdx.json", "bootwright.notices.txt"}; !slices.Equal(got, want) {
				t.Fatalf("dist holds %v after a refusal, want the previous four", got)
			}
			for file, text := range previous {
				if data, _ := os.ReadFile(filepath.Join("dist", file)); string(data) != text {
					t.Errorf("%s changed to %q on a refused release", file, data)
				}
			}
		})
	}
}

func digestOf(text string) string {
	digest := sha256.Sum256([]byte(text))
	return hex.EncodeToString(digest[:])
}

func stage(overrides map[string]string) func([]string, []string) error {
	inner := goodCommands().build
	return func(environment, arguments []string) error {
		if err := inner(environment, arguments); err != nil {
			return err
		}
		for _, argument := range arguments {
			if dir, found := strings.CutPrefix(argument, "CONTAINER_OUT="); found {
				for name, content := range overrides {
					path := filepath.Join(dir, name)
					if content == "-" {
						if err := os.Remove(path); err != nil {
							return err
						}
					} else if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
						return err
					}
				}
			}
		}
		return nil
	}
}

func withNotices(text string) func([]string, []string) error {
	return stage(map[string]string{"bootwright.notices.txt": text})
}

func TestTheVersionIsDecodedFromTheResultNotScannedForAKey(t *testing.T) {
	t.Chdir(t.TempDir())
	pretty := "{\n  \"diagnostics\": [{\"version\": \"v9.9.9\", \"commit\": \"x\"}],\n  \"result\": {\n    \"source\": \"clean\",\n    \"vulnerabilityCheck\": \"" + freshVerdict() + "\",\n    \"commit\": \"" + head + "\",\n    \"version\": \"v1.2.3\",\n    \"go\": \"go1.26\"\n  }\n}\n"
	f := goodCommands()
	f.version = pretty
	if err := runRelease(t, goodConfig(), f); err != nil {
		t.Fatalf("a pretty-printed envelope with another key order was refused: %v", err)
	}
	t.Chdir(t.TempDir())
	f = goodCommands()
	f.version = `{"result":{"version":"v1.2.3","note":"a \"version\":\"v1.2.3\" inside text","commit":"` + head + `","source":"modified","vulnerabilityCheck":"` + freshVerdict() + `"}}`
	if err := runRelease(t, goodConfig(), f); err == nil || !strings.Contains(err.Error(), "reports source 'modified'") {
		t.Fatalf("err = %v", err)
	}
}

func TestADirectoryOrAnotherUsersFileWhereAShippedFileBelongsIsRefused(t *testing.T) {
	t.Chdir(t.TempDir())
	if err := os.MkdirAll("dist/SHA256SUMS", 0o755); err != nil {
		t.Fatal(err)
	}
	f := goodCommands()
	if err := runRelease(t, goodConfig(), f); err == nil || !strings.Contains(err.Error(), "dist/SHA256SUMS is a directory or belongs to another user") {
		t.Fatalf("err = %v", err)
	}
	if len(f.arguments) != 0 {
		t.Fatal("the build started")
	}
	if names := listing(t, "dist"); !slices.Equal(names, []string{"SHA256SUMS"}) {
		t.Fatalf("dist holds %v", names)
	}
}

func TestTheStagedChecksumsMustNameEveryFileWithItsDigest(t *testing.T) {
	stageDir := t.TempDir()
	files := []string{"a", "b"}
	for _, name := range files {
		if err := os.WriteFile(filepath.Join(stageDir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeSums(stageDir, files); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(string) string{
		"a changed digest": func(s string) string { return "0" + s[1:] },
		"a missing line":   func(s string) string { return strings.SplitAfter(s, "\n")[0] },
		"an extra line":    func(s string) string { return s + s },
		"a reordered file": func(s string) string { lines := strings.SplitAfter(s, "\n"); return lines[1] + lines[0] },
	} {
		data, _ := os.ReadFile(filepath.Join(stageDir, sums))
		if err := os.WriteFile(filepath.Join(stageDir, sums), []byte(mutate(string(data))), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := checkSums(stageDir, files); err == nil {
			t.Errorf("%s verified", name)
		}
		if err := writeSums(stageDir, files); err != nil {
			t.Fatal(err)
		}
	}
}

func TestTheFlagsCarryEveryValueWhole(t *testing.T) {
	c, err := parse([]string{"-root", "/r", "-dir", "dist", "-files", "a b", "-foreign", "X=command line", "-foreign", "Y=environment", "-version", "v1'; x", "-eval", "-make-flags", "ni"})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.Foreign, listFlag{"X=command line", "Y=environment"}) || c.Version != "v1'; x" || !c.Eval || c.MakeFlags != "ni" || c.Make != "make" {
		t.Errorf("parse = %+v", c)
	}
	if _, err := parse([]string{"-root", "/r"}); err == nil {
		t.Error("a release with no directory or files parsed")
	}
}

func TestOutputIsSilentUntilTheRelease(t *testing.T) {
	t.Chdir(t.TempDir())
	var out bytes.Buffer
	if err := release(context.Background(), goodConfig(), goodCommands(), nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if want := "release: dist holds bootwright bootwright.cdx.json bootwright.notices.txt SHA256SUMS of v1.2.3, commit " + head + "\n"; out.String() != want {
		t.Errorf("output = %q, want %q", out.String(), want)
	}
}
