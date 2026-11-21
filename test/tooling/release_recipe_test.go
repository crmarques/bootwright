package tooling_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	releaseToolOnce sync.Once
	releaseToolPath string
	releaseToolErr  error
	releaseToolDir  string
)

func TestMain(m *testing.M) {
	code := m.Run()
	if releaseToolDir != "" {
		os.RemoveAll(releaseToolDir)
	}
	os.Exit(code)
}

// releaseTool is scripts/tools/release, built once for every checkout a test
// makes, so each recipe runs the command the Makefile names.
func releaseTool(t *testing.T) string {
	t.Helper()
	releaseToolOnce.Do(func() {
		releaseToolDir, releaseToolErr = os.MkdirTemp("", "release-tool-")
		if releaseToolErr != nil {
			return
		}
		releaseToolPath = filepath.Join(releaseToolDir, "release")
		build := exec.Command(filepath.Join("..", "..", "scripts", "go"), "-C", filepath.Join("..", "..", "scripts", "tools"), "build", "-o", releaseToolPath, "./release")
		if output, err := build.CombinedOutput(); err != nil {
			releaseToolErr = fmt.Errorf("%v\n%s", err, output)
		}
	})
	if releaseToolErr != nil {
		t.Fatalf("the release command did not build: %v", releaseToolErr)
	}
	return releaseToolPath
}

// releaseEngine stands in for the container engine of a release build. It
// exports an executable that answers version and sbom, a bill of materials and
// notices that name its digest, and each placeholder is replaced by a test to
// make one part of the build wrong.
const releaseEngine = `#!/bin/sh
set -eu
for argument in "$@"; do
	case "$argument" in
	type=local,dest=*) dest="${argument#type=local,dest=}" ;;
	VERSION=*) version="${argument#VERSION=}" ;;
	COMMIT=*) commit="${argument#COMMIT=}" ;;
	VULNCHECK=*) vulncheck="${argument#VULNCHECK=}" ;;
	esac
done
printf '%s\n' "$vulncheck" >>"$(dirname "$0")/vulncheck"
printf 'RELEASE_STRAY=%s HTTPS_PROXY=%s ARGUMENTS=%s\n' "${RELEASE_STRAY-unset}" "${HTTPS_PROXY-unset}" "$*" >>"$(dirname "$0")/environment"
@IDENTITY@
cat >"$dest/bootwright" <<EOF
#!/bin/sh
case "\$1" in
version) printf '%s\n' '{"result":{"version":"$version","commit":"$commit","source":"@SOURCE@","vulnerabilityCheck":"@CHECK@"}}' ;;
sbom) printf '%s\n' '@SBOM@' ;;
esac
EOF
chmod +x "$dest/bootwright"
printf '%s\n' '{"bom":"one"}' >"$dest/bootwright.cdx.json"
digest="$(sha256sum "$dest/bootwright.cdx.json")"
printf 'Third-party notices of bootwright\n\nBill of materials: sha256:%s\n' "@DIGEST@" >"$dest/bootwright.notices.txt"
@POST@
`

type releaseBuild struct {
	check, source, sbom, digest, version, commit, post string
	omitNotices                                        bool
}

func (b releaseBuild) script() string {
	identity := ""
	if b.version != "" {
		identity += "version=" + b.version + "\n"
	}
	if b.commit != "" {
		identity += "commit=" + b.commit + "\n"
	}
	script := strings.Replace(releaseEngine, "@IDENTITY@\n", identity, 1)
	script = strings.Replace(script, "@POST@\n", b.post+"\n", 1)
	for placeholder, value := range map[string]string{"@CHECK@": b.check, "@SOURCE@": b.source, "@SBOM@": b.sbom, "@DIGEST@": b.digest} {
		script = strings.ReplaceAll(script, placeholder, value)
	}
	if b.omitNotices {
		script = script[:strings.Index(script, "printf 'Third-party notices")]
	}
	return script
}

func goodRelease() releaseBuild {
	return releaseBuild{check: "passed " + time.Now().UTC().Format(time.DateOnly), source: "clean", sbom: `{"bom":"one"}`, digest: `${digest%% *}`}
}

// nativeGo stands in for scripts/go on the host build: it records the Go
// settings of its environment and does what each step of make build needs.
const nativeGo = `#!/bin/sh
set -eu
if [ "${1-}" = -C ] && [ "${2-}" = scripts/tools ] && [ "${3-}" = run ] && [ "${4-}" = ./release ]; then
	shift 4
	exec @RELEASE@ "$@"
fi
printf 'GOFLAGS=%s GOEXPERIMENT=%s GOOS=%s GOARCH=%s GOAMD64=%s GOFIPS140=%s GOENV=%s RELEASE_STRAY=%s HTTPS_PROXY=%s\n' "${GOFLAGS-unset}" "${GOEXPERIMENT-unset}" "${GOOS-unset}" "${GOARCH-unset}" "${GOAMD64-unset}" "${GOFIPS140-unset}" "${GOENV-unset}" "${RELEASE_STRAY-unset}" "${HTTPS_PROXY-unset}" >>"$(dirname "$0")/../go.log"
previous=
for argument in "$@"; do
	case "$previous" in
	-verdict) verdict="$argument" ;;
	-sbom) sbom="$argument" ;;
	-out) out="$argument" ;;
	-ldflags) ldflags="$argument" ;;
	-o) target="$argument" ;;
	esac
	previous="$argument"
done
case "$*" in
*runtimewheels*) ;;
*vulngate*) printf 'passed:%s\n' "$(date -u +%F)" >"$verdict" ;;
*"./licenses notices"*)
	digest="$(sha256sum "$sbom")"
	printf 'Third-party notices of bootwright\n\nBill of materials: sha256:%s\n' "${digest%% *}" >"$out" ;;
build*)
	version="${ldflags#*main.version=}"; version="${version%% *}"
	commit="${ldflags#*main.commit=}"; commit="${commit%% *}"
	check="${ldflags#*main.vulnerabilityCheck=}"; check="${check%% *}"
	cat >"$target" <<EOF
#!/bin/sh
case "\$1" in
version) printf '%s\n' '{"result":{"version":"$version","commit":"$commit","source":"clean","vulnerabilityCheck":"passed ${check#passed:}"}}' ;;
sbom) printf '%s\n' '{"bom":"native"}' ;;
esac
EOF
	chmod +x "$target" ;;
esac
`

type releaseCheckout struct {
	dir, engine, tmp string
}

func gitIn(t *testing.T, dir string, arguments ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=release test", "-c", "user.email=release@example.test", "-c", "commit.gpgsign=false"}, arguments...)...)
	command.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", arguments, err, output)
	}
	return strings.TrimSpace(string(output))
}

// newReleaseCheckout is a committed Git checkout holding the real Makefile,
// the scripts it runs, the stand-in Go of the host build, and ignored release
// and build directories.
func newReleaseCheckout(t *testing.T, build releaseBuild) releaseCheckout {
	t.Helper()
	makefile, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"Makefile":            string(makefile),
		"scripts/cache-dir":   "#!/bin/sh\necho \"$PWD/.cache\"\n",
		"scripts/go":          strings.Replace(nativeGo, "@RELEASE@", releaseTool(t), 1),
		"scripts/govulncheck": "#!/bin/sh\nexit 0\n",
		".gitignore":          "/dist/\n/bin/\n/go.log\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	tmp := t.TempDir()
	engine := filepath.Join(tmp, "engine")
	if err := os.WriteFile(engine, []byte(build.script()), 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "init", "-q")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "release checkout")
	return releaseCheckout{dir: dir, engine: engine, tmp: tmp}
}

func (c releaseCheckout) make(t *testing.T, flags, environment []string, arguments ...string) (string, error) {
	t.Helper()
	command := exec.Command("make", append(append([]string{"-s", "-C", c.dir}, flags...), append([]string{"release", "CONTAINER_ENGINE=" + c.engine}, arguments...)...)...)
	command.Env = append(os.Environ(), environment...)
	output, err := command.CombinedOutput()
	return string(output), err
}

func (c releaseCheckout) release(t *testing.T, arguments ...string) (string, error) {
	t.Helper()
	return c.make(t, nil, nil, append([]string{"RELEASE_BUILD=container"}, arguments...)...)
}

// shipped lists what RELEASE_DIR holds, a directory as "<directory>".
func (c releaseCheckout) shipped(t *testing.T) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(c.dir, "dist"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() {
			files[entry.Name()] = "<directory>"
			continue
		}
		data, err := os.ReadFile(filepath.Join(c.dir, "dist", entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = string(data)
	}
	return files
}

func (c releaseCheckout) engineCalls(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(c.tmp, "vulncheck"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

func (c releaseCheckout) previousRelease(t *testing.T) map[string]string {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(c.dir, "dist"), 0o755); err != nil {
		t.Fatal(err)
	}
	previous := map[string]string{"bootwright": "old executable\n", "bootwright.cdx.json": "old bill\n", "bootwright.notices.txt": "old notices\n", "SHA256SUMS": "old sums\n"}
	for file, text := range previous {
		if err := os.WriteFile(filepath.Join(c.dir, "dist", file), []byte(text), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return previous
}

func equalFiles(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		if other, ok := b[key]; !ok || other != value {
			return false
		}
	}
	return true
}

func TestReleaseShipsTheExecutableItsBillOfMaterialsItsNoticesAndTheirDigests(t *testing.T) {
	checkout := newReleaseCheckout(t, goodRelease())
	if output, err := checkout.release(t, "VERSION=v1.2.3"); err != nil {
		t.Fatalf("release of a clean checkout failed: %v\n%s", err, output)
	}
	files := checkout.shipped(t)
	var names []string
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	if want := []string{"SHA256SUMS", "bootwright", "bootwright.cdx.json", "bootwright.notices.txt"}; !slices.Equal(names, want) {
		t.Fatalf("the release holds %v, want %v and no staging left behind", names, want)
	}
	commit := gitIn(t, checkout.dir, "rev-parse", "HEAD")
	if !strings.Contains(files["bootwright"], `"version":"v1.2.3","commit":"`+commit+`"`) {
		t.Errorf("the executable was not asked for the release's version and commit:\n%s", files["bootwright"])
	}
	check := exec.Command("sha256sum", "--check", "--strict", "SHA256SUMS")
	check.Dir = filepath.Join(checkout.dir, "dist")
	if output, err := check.CombinedOutput(); err != nil {
		t.Errorf("SHA256SUMS does not verify the shipped files: %v\n%s", err, output)
	}
	if lines := strings.Split(strings.TrimSpace(files["SHA256SUMS"]), "\n"); len(lines) != 3 {
		t.Errorf("SHA256SUMS names %d files, want the executable, its bill of materials and its notices:\n%s", len(lines), files["SHA256SUMS"])
	}
	if files["bootwright.cdx.json"] != "{\"bom\":\"one\"}\n" {
		t.Errorf("the bill of materials was changed on its way to the release: %q", files["bootwright.cdx.json"])
	}
}

func TestReleaseTurnsTheVulnerabilityGateOnForTheBuild(t *testing.T) {
	checkout := newReleaseCheckout(t, goodRelease())
	if output, err := checkout.release(t, "VERSION=v1.2.3"); err != nil {
		t.Fatalf("release failed: %v\n%s", err, output)
	}
	if got := checkout.engineCalls(t); got != "on\n" {
		t.Errorf("the build was handed VULNCHECK %q, want on", got)
	}
	output, err := checkout.release(t, "VERSION=v1.2.3", "VULNCHECK=skip")
	if err == nil || !strings.Contains(output, "VULNCHECK=skip is refused") {
		t.Errorf("a release under VULNCHECK=skip was not refused (%v):\n%s", err, output)
	}
	if got := checkout.engineCalls(t); got != "on\n" {
		t.Errorf("a refused release still started the build: %q", got)
	}
}

func TestReleaseRefusesWhatItMustNotShip(t *testing.T) {
	good := goodRelease()
	variant := func(change func(*releaseBuild)) releaseBuild {
		build := good
		change(&build)
		return build
	}
	toDirectory := func(name string) func(*testing.T, releaseCheckout) {
		return func(t *testing.T, checkout releaseCheckout) {
			path := filepath.Join(checkout.dir, "dist", name)
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(path, 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	for name, test := range map[string]struct {
		build       releaseBuild
		arguments   []string
		environment []string
		prepare     func(t *testing.T, checkout releaseCheckout)
		want        string
		built       bool
		kept        map[string]string
	}{
		"an executable stamped unchecked":            {build: variant(func(b *releaseBuild) { b.check = "unchecked" }), built: true, want: "not a pass"},
		"an executable stamped with no pass":         {build: variant(func(b *releaseBuild) { b.check = "failed" }), built: true, want: "not a pass"},
		"a bill of materials another build wrote":    {build: variant(func(b *releaseBuild) { b.sbom = `{"bom":"two"}` }), built: true, want: "is not the bill of materials of this executable"},
		"notices of another bill of materials":       {build: variant(func(b *releaseBuild) { b.digest = strings.Repeat("0", 64) }), built: true, want: "was not composed from this bill of materials"},
		"an executable built from a modified tree":   {build: variant(func(b *releaseBuild) { b.source = "modified" }), built: true, want: "reports source 'modified'"},
		"an executable reporting another version":    {build: variant(func(b *releaseBuild) { b.version = "v9.9.9" }), built: true, want: "reports version 'v9.9.9'"},
		"an executable reporting another commit":     {build: variant(func(b *releaseBuild) { b.commit = strings.Repeat("b", 40) }), built: true, want: "reports commit"},
		"an export that omits the notices":           {build: variant(func(b *releaseBuild) { b.omitNotices = true }), built: true, want: "exported no bootwright.notices.txt"},
		"an export that omits the executable":        {build: variant(func(b *releaseBuild) { b.post = `rm -f "$dest/bootwright"` }), built: true, want: "exported no bootwright"},
		"an empty executable":                        {build: variant(func(b *releaseBuild) { b.post = `: >"$dest/bootwright"` }), built: true, want: "bootwright is missing or empty"},
		"an executable that cannot run":              {build: variant(func(b *releaseBuild) { b.post = `chmod -x "$dest/bootwright"` }), built: true, want: "bootwright is not executable"},
		"an empty bill of materials":                 {build: variant(func(b *releaseBuild) { b.post = `: >"$dest/bootwright.cdx.json"` }), built: true, want: "bootwright.cdx.json is missing or empty"},
		"VULNCHECK=skip":                             {build: good, arguments: []string{"VULNCHECK=skip"}, want: "VULNCHECK=skip is refused"},
		"a VULNDB that is not the Makefile's":        {build: good, arguments: []string{"VULNDB=file:///nowhere"}, want: "VULNDB comes from the command line"},
		"a GO that is not the Makefile's":            {build: good, arguments: []string{"GO=true"}, want: "GO comes from the command line"},
		"a shell that is not make's":                 {build: good, arguments: []string{"SHELL=/bin/true"}, want: "SHELL or .SHELLFLAGS is set"},
		"shell flags that are not make's":            {build: good, arguments: []string{".SHELLFLAGS=-c"}, want: "SHELL or .SHELLFLAGS is set"},
		"a STAMP that is not the Makefile's":         {build: good, arguments: []string{"STAMP=-X main.vulnerabilityCheck=passed:2026-11-10"}, want: "STAMP comes from the command line"},
		"a verdict that is not the Makefile's":       {build: good, arguments: []string{"VULNERABILITY_CHECK=passed:2026-11-10"}, want: "VULNERABILITY_CHECK comes from the command line"},
		"a license table that is not the Makefile's": {build: good, arguments: []string{"GO_LICENSES=/nowhere.json"}, want: "GO_LICENSES comes from the command line"},
		"a tool table that is not the Makefile's":    {build: good, arguments: []string{"TOOL_TABLE=/nowhere.json"}, want: "TOOL_TABLE comes from the command line"},
		"proxy settings that are not the Makefile's": {build: good, arguments: []string{"CONTAINER_SETTINGS=HOME"}, want: "CONTAINER_SETTINGS comes from the command line"},
		"a variable the Makefile does not name":      {build: good, arguments: []string{"GOFLAGS=-overlay=/nowhere.json"}, want: "GOFLAGS comes from the command line"},
		"a wheel cache from the environment":         {build: good, environment: []string{"WHEELS_CACHE=/nowhere"}, want: "WHEELS_CACHE comes from the environment"},
		"an environment that overrides the Makefile": {build: good, arguments: []string{"-e"}, environment: []string{"STAMP=-X main.vulnerabilityCheck=passed:2026-11-10"}, want: "STAMP comes from the environment override"},
		"another makefile read first":                {build: good, environment: []string{"MAKEFILES=/dev/null"}, want: "reads more than the Makefile"},
		"a makefile line evaluated first":            {build: good, arguments: []string{"--eval=GO_LICENSES=/nowhere.json"}, want: "reads more than the Makefile"},
		"a Go image named by a tag alone":            {build: good, arguments: []string{"GO_IMAGE=docker.io/library/golang:1.26.11-trixie"}, want: "is not pinned by @sha256:"},
		"a Go image from the environment by a tag":   {build: good, environment: []string{"GO_IMAGE=mirror.example/golang:1.26.11-trixie@sha256:short"}, want: "is not pinned by @sha256:"},
		"no version":                               {build: good, arguments: []string{"VERSION="}, want: "name the release with VERSION"},
		"a dirty version":                          {build: good, arguments: []string{"VERSION=v1.2.3-dirty"}, want: "names a dirty tree"},
		"a commit that is not the checkout's":      {build: good, arguments: []string{"COMMIT=" + strings.Repeat("a", 40)}, want: "is not the checkout's commit"},
		"a source declared modified":               {build: good, arguments: []string{"SOURCE=modified"}, want: "SOURCE=modified is not clean"},
		"an unknown build":                         {build: good, arguments: []string{"RELEASE_BUILD=docker"}, want: "neither native nor container"},
		"a directory where the executable belongs": {build: good, prepare: toDirectory("bootwright"), want: "is a directory or belongs to another user", kept: map[string]string{"bootwright": "<directory>", "bootwright.cdx.json": "old bill\n", "bootwright.notices.txt": "old notices\n", "SHA256SUMS": "old sums\n"}},
		"a directory where the checksums belong":   {build: good, prepare: toDirectory("SHA256SUMS"), want: "is a directory or belongs to another user", kept: map[string]string{"bootwright": "old executable\n", "bootwright.cdx.json": "old bill\n", "bootwright.notices.txt": "old notices\n", "SHA256SUMS": "<directory>"}},
		"a modified tracked file": {build: good, want: "modified or untracked files", prepare: func(t *testing.T, checkout releaseCheckout) {
			if err := os.WriteFile(filepath.Join(checkout.dir, "scripts", "cache-dir"), []byte("#!/bin/sh\necho \"$PWD/.cache\" # changed\n"), 0o755); err != nil {
				t.Fatal(err)
			}
		}},
		"an untracked file": {build: good, want: "modified or untracked files", prepare: func(t *testing.T, checkout releaseCheckout) {
			if err := os.WriteFile(filepath.Join(checkout.dir, "extra.go"), []byte("package extra\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(name, func(t *testing.T) {
			checkout := newReleaseCheckout(t, test.build)
			want := checkout.previousRelease(t)
			if test.prepare != nil {
				test.prepare(t, checkout)
			}
			if test.kept != nil {
				want = test.kept
			}
			output, err := checkout.make(t, nil, test.environment, append([]string{"RELEASE_BUILD=container", "VERSION=v1.2.3"}, test.arguments...)...)
			if err == nil {
				t.Fatalf("release finished:\n%s", output)
			}
			if !strings.Contains(output, test.want) {
				t.Errorf("the refusal does not say %q:\n%s", test.want, output)
			}
			if !test.built && checkout.engineCalls(t) != "" {
				t.Errorf("a refusal before the build still started it: %q", checkout.engineCalls(t))
			}
			if got := checkout.shipped(t); !equalFiles(got, want) {
				t.Errorf("the refused release changed the shipped files to %v, so they no longer match each other", got)
			}
		})
	}
}

func TestReleaseRefusesAMakeThatWouldRunOnlyPartOfIt(t *testing.T) {
	for _, flag := range []string{"-n", "-i", "-t", "-q", "--dry-run", "-ni"} {
		for _, build := range []string{"container", "native"} {
			t.Run(flag+" "+build, func(t *testing.T) {
				unchecked := goodRelease()
				unchecked.check = "unchecked"
				checkout := newReleaseCheckout(t, unchecked)
				previous := checkout.previousRelease(t)
				output, err := checkout.make(t, []string{flag}, nil, "RELEASE_BUILD="+build, "VERSION=v1.2.3")
				if err == nil || !strings.Contains(output, "make was run with") {
					t.Fatalf("make %s was not refused (%v):\n%s", flag, err, output)
				}
				if got := checkout.shipped(t); !equalFiles(got, previous) {
					t.Errorf("make %s changed the shipped files to %v", flag, got)
				}
				if checkout.engineCalls(t) != "" {
					t.Errorf("make %s started the build", flag)
				}
			})
		}
	}
	t.Run("-k is no reason to refuse", func(t *testing.T) {
		unchecked := goodRelease()
		unchecked.check = "unchecked"
		checkout := newReleaseCheckout(t, unchecked)
		output, err := checkout.make(t, []string{"-k"}, nil, "RELEASE_BUILD=container", "VERSION=v1.2.3")
		if err == nil || !strings.Contains(output, "not a pass") {
			t.Errorf("make -k did not run the recipe to its refusal (%v):\n%s", err, output)
		}
	})
}

func TestReleaseRefusesATreeThatChangesDuringTheBuild(t *testing.T) {
	growing := goodRelease()
	growing.post = `touch extra.go`
	checkout := newReleaseCheckout(t, growing)
	previous := checkout.previousRelease(t)
	output, err := checkout.make(t, nil, nil, "RELEASE_BUILD=container", "VERSION=v1.2.3")
	if err == nil || !strings.Contains(output, "extra.go") {
		t.Fatalf("a file added during the build was shipped (%v):\n%s", err, output)
	}
	if got := checkout.shipped(t); !equalFiles(got, previous) {
		t.Errorf("the refused release changed the shipped files to %v", got)
	}
}

func TestReleaseOnTheHostRunsTheBuildWithAnEnvironmentThatCannotChangeTheCompile(t *testing.T) {
	checkout := newReleaseCheckout(t, goodRelease())
	goenv := filepath.Join(t.TempDir(), "env")
	if err := os.WriteFile(goenv, []byte("GOEXPERIMENT=boringcrypto\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	output, err := checkout.make(t, nil, []string{"GOFLAGS=-overlay=/nowhere.json", "GOEXPERIMENT=boringcrypto", "GOOS=windows", "GOARCH=arm64", "GOAMD64=v4", "GOFIPS140=latest", "GOENV=" + goenv, "RELEASE_STRAY=inherited", "HTTPS_PROXY=http://proxy.example:3128"}, "RELEASE_BUILD=native", "VERSION=v1.2.3")
	if err != nil {
		t.Fatalf("release on the host failed: %v\n%s", err, output)
	}
	files := checkout.shipped(t)
	if !strings.Contains(files["bootwright"], `"version":"v1.2.3"`) || !strings.Contains(files["bootwright"], `"vulnerabilityCheck":"`+goodRelease().check+`"`) {
		t.Errorf("the host build was not stamped with the release and a passed gate:\n%s", files["bootwright"])
	}
	log, err := os.ReadFile(filepath.Join(checkout.dir, "go.log"))
	if err != nil || len(log) == 0 {
		t.Fatalf("the host build ran no Go step (%v)", err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(log)), "\n") {
		if line != "GOFLAGS=-mod=readonly GOEXPERIMENT=unset GOOS=unset GOARCH=unset GOAMD64=unset GOFIPS140=unset GOENV=off RELEASE_STRAY=unset HTTPS_PROXY=http://proxy.example:3128" {
			t.Errorf("a Go step ran with %q, want the module graph read-only, no go env file, no inherited Go setting and the proxy alone passed on", line)
		}
	}
	environment, err := checkout.make(t, nil, []string{"VULNDB=file:///nowhere"}, "RELEASE_BUILD=native", "VERSION=v1.2.3")
	if err == nil || !strings.Contains(environment, "VULNDB comes from the environment") {
		t.Errorf("a VULNDB in the environment was not refused (%v):\n%s", err, environment)
	}
}

func TestReleaseInAContainerPassesOnlyTheAllowedEnvironmentAndADigestPinnedImage(t *testing.T) {
	checkout := newReleaseCheckout(t, goodRelease())
	image := "mirror.example/golang:1.26.11-trixie@sha256:" + strings.Repeat("a", 64)
	output, err := checkout.make(t, nil, []string{"RELEASE_STRAY=inherited", "HTTPS_PROXY=http://proxy.example:3128"}, "RELEASE_BUILD=container", "VERSION=v1.2.3", "GO_IMAGE="+image)
	if err != nil {
		t.Fatalf("a container release with a digest-pinned image failed: %v\n%s", err, output)
	}
	data, err := os.ReadFile(filepath.Join(checkout.tmp, "environment"))
	if err != nil {
		t.Fatal(err)
	}
	if line := strings.TrimSpace(string(data)); !strings.HasPrefix(line, "RELEASE_STRAY=unset HTTPS_PROXY=http://proxy.example:3128 ") || !strings.Contains(line, "--build-arg GO_IMAGE="+image+" ") {
		t.Errorf("the engine ran with %q, want no inherited variable, the proxy passed on and the pinned image", line)
	}
}

func TestEveryVariableTheEnvironmentCanSetIsAReleaseInputOrRefused(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	makefile := string(data)
	list := func(name string) []string {
		match := regexp.MustCompile(`(?m)^` + name + ` = (.*)$`).FindStringSubmatch(makefile)
		if match == nil {
			t.Fatalf("the Makefile does not list %s", name)
		}
		return strings.Fields(match[1])
	}
	var defaulted []string
	for _, match := range regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_]*) \?=`).FindAllStringSubmatch(makefile, -1) {
		defaulted = append(defaulted, match[1])
	}
	listed := append(list("RELEASE_INPUTS"), list("RELEASE_DEFAULTED")...)
	slices.Sort(defaulted)
	slices.Sort(listed)
	if !slices.Equal(defaulted, listed) {
		t.Errorf("the Makefile's ?= variables are %v, but RELEASE_INPUTS and RELEASE_DEFAULTED list %v; a release must take each from the caller on purpose or refuse it from the environment", defaulted, listed)
	}
}

func TestReleaseOnTheHostRefusesAnIgnoredFileTheCollectionWouldEmbed(t *testing.T) {
	checkout := newReleaseCheckout(t, goodRelease())
	if err := os.WriteFile(filepath.Join(checkout.dir, ".gitignore"), []byte("/dist/\n/bin/\n/go.log\n*.py\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, checkout.dir, "add", ".gitignore")
	gitIn(t, checkout.dir, "commit", "-q", "-m", "ignore python")
	if err := os.MkdirAll(filepath.Join(checkout.dir, "ansible", "collections"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout.dir, "ansible", "collections", "stray.py"), []byte("print()\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	output, err := checkout.make(t, nil, nil, "RELEASE_BUILD=native", "VERSION=v1.2.3")
	if err == nil || !strings.Contains(output, "would be embedded") {
		t.Errorf("an ignored file under ansible/collections was not refused (%v):\n%s", err, output)
	}
}

func TestTheReleaseRecipeIsOneRecipeThatRunsTheBuildWithTheVulnerabilityGateOn(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "Makefile"))
	if err != nil {
		t.Fatal(err)
	}
	_, recipe, found := strings.Cut(string(data), "\nrelease:\n")
	if !found {
		t.Fatal("the Makefile has no release target without prerequisites, so a check could be skipped as one")
	}
	recipe, _, _ = strings.Cut(recipe, "\n\n")
	logical := strings.ReplaceAll(recipe, "\\\n", " ")
	if strings.Count(logical, "\n") != 0 {
		t.Errorf("the release recipe is more than one line, so a flag could skip one of them:\n%s", recipe)
	}
	for _, want := range []string{"+@GOENV=off GOFLAGS=-mod=readonly ./scripts/go -C scripts/tools run ./release ", "-vulncheck ", "-make-flags ", "-foreign "} {
		if !strings.Contains(logical, want) {
			t.Errorf("the release recipe does not contain %q:\n%s", want, recipe)
		}
	}
	if strings.Count(logical, "$(MAKE)") != 1 || strings.Contains(logical, "release-verify") || strings.Contains(logical, "release-preflight") {
		t.Error("a release check runs as a sub-make or prerequisite of the recipe, which -n, -i or -o would turn into a no-op")
	}
	ignored, err := os.ReadFile(filepath.Join("..", "..", ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(strings.Split(string(ignored), "\n"), "/dist/") {
		t.Error("the default RELEASE_DIR, dist, is not ignored, so a release would dirty the tree it was built from")
	}
}

func TestReleaseCarriesAValueWithAQuoteToTheCommandWhole(t *testing.T) {
	checkout := newReleaseCheckout(t, goodRelease())
	value := "a'; touch hacked; echo 'b"
	output, err := checkout.release(t, "VERSION=v1.2.3", "COMMIT="+value)
	if err == nil || !strings.Contains(output, "COMMIT="+value+" is not the checkout's commit") {
		t.Errorf("a commit with quotes was not carried to the command whole (%v):\n%s", err, output)
	}
	if _, statErr := os.Stat(filepath.Join(checkout.dir, "hacked")); statErr == nil {
		t.Error("a quote in COMMIT ran a command")
	}
}
