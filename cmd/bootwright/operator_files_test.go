//go:build linux && amd64

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/cli"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/privilege"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/secrets"
	secretmaterial "github.com/crmarques/bootwright/internal/secrets/material"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// virtualOperatorFiles serves absolute paths beneath root, so a path it
// serves exists nowhere a reader could open on its own, and records every
// path it is asked to open.
type virtualOperatorFiles struct {
	root   string
	prefix string
	mu     sync.Mutex
	issued map[*os.File]string
	asked  []string
	denied map[string]bool
}

func newVirtualOperatorFiles(t *testing.T) *virtualOperatorFiles {
	t.Helper()
	prefix := filepath.Join("/", "bootwright-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	if _, err := os.Lstat(prefix); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the virtual prefix exists on this host, so the test proves nothing: %v", err)
	}
	return &virtualOperatorFiles{root: t.TempDir(), prefix: prefix, issued: map[*os.File]string{}, denied: map[string]bool{}}
}

// write creates one file beneath the virtual prefix and returns its virtual
// path.
func (v *virtualOperatorFiles) write(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(v.prefix, name)
	if err := os.MkdirAll(filepath.Join(v.root, filepath.Dir(path)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(v.root, path), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

// deny refuses every open of path that would read it, as the kernel refuses
// an account without read permission; a path-only open still succeeds.
func (v *virtualOperatorFiles) deny(path string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.denied[path] = true
}

// take returns the paths asked for since the last take.
func (v *virtualOperatorFiles) take() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	asked := v.asked
	v.asked = nil
	return asked
}

func (v *virtualOperatorFiles) Begin(context.Context) (operatorFileSession, error) { return v, nil }

func (v *virtualOperatorFiles) Root() (*os.File, error) {
	fd, err := syscall.Open(v.root, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return v.issue(fd, "/"), nil
}

func (v *virtualOperatorFiles) OpenAt(parent *os.File, name string, flags int) (*os.File, error) {
	v.mu.Lock()
	base, issued := v.issued[parent]
	path := filepath.Join(base, name)
	v.asked = append(v.asked, path)
	refused := v.denied[path] && flags&0x200000 == 0
	v.mu.Unlock()
	if !issued {
		return nil, syscall.EBADF
	}
	if refused {
		return nil, syscall.EACCES
	}
	fd, err := syscall.Openat(int(parent.Fd()), name, flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return v.issue(fd, path), nil
}

func (v *virtualOperatorFiles) OpenFile(path string) (*os.File, error) {
	v.mu.Lock()
	v.asked = append(v.asked, path)
	v.mu.Unlock()
	return os.Open(filepath.Join(v.root, path))
}

func (*virtualOperatorFiles) Close() error { return nil }

func (v *virtualOperatorFiles) issue(fd int, path string) *os.File {
	file := os.NewFile(uintptr(fd), path)
	v.mu.Lock()
	defer v.mu.Unlock()
	v.issued[file] = path
	return file
}

// operatorFileServices binds the complete graph as testServices does, with
// files as the invoking account's opener.
func operatorFileServices(t *testing.T, files *virtualOperatorFiles) cli.Services {
	t.Helper()
	root := filepath.Join(t.TempDir(), "state")
	repository := testRepository(root)
	deps := testContextWiring(t, root)
	deps.Files = files
	deps.Repository, deps.Workspace, deps.Trust = repository, repository, repository
	deps.Lifecycle = lifecycleDependencies{
		Workspace: repository,
		Inputs:    contexts.Inputs{Repository: repository, Selection: deps.Selection},
		Host:      testLifecycleHost{}, Guard: testLifecycleGuard{}, Selection: deps.Selection,
		Presenter: testLifecyclePresenter{}, Confirmer: deps.Confirmer,
		Capabilities: testLifecycleCapabilities{},
	}
	return assembleServices(deps)
}

func TestTheRootChildOpensNoOperatorPathItself(t *testing.T) {
	files := newVirtualOperatorFiles(t)
	environment := files.write(t, "env/environment.yaml", syntheticEnvironment)
	host := files.write(t, "env/controller.yaml", serviceHost)
	declaration := files.write(t, "env/secret.yaml", secretDocument("opaque", "opaque", ""))
	configuration := files.write(t, "context.yaml", string(contexts.DefaultConfiguration("alpha").Canonical()))
	value := files.write(t, "value", "synthetic-opaque\n")
	image := files.write(t, "image.iso", "installer")
	input := filepath.Dir(environment)
	services := operatorFileServices(t, files)
	for _, step := range []struct {
		args []string
		want []string
	}{
		{[]string{"context", "init", "--name", "alpha", "--input-dir", input, "-f", configuration}, []string{input, environment, host, declaration, configuration}},
		{[]string{"secret", "set", "--name", "opaque", "--value-file", value}, []string{value}},
		{[]string{"validate", "-f", input}, []string{input, environment, host, declaration}},
	} {
		contextRun(t, services, 0, step.args...)
		asked := files.take()
		for _, path := range step.want {
			if !slices.Contains(asked, path) {
				t.Errorf("%v opened %s without the opener; it asked for %v", step.args, path, asked)
			}
		}
	}
	acquisition, err := localMediaDependencies(nil, nil, nil, nil, controller.Route{}, files).Acquirer.Open(context.Background(), media.Source{Path: image})
	if err != nil {
		t.Fatalf("media open: %#v", diagnostics.Of(err))
	}
	acquisition.Payload.Close()
	if asked := files.take(); !slices.Equal(asked, []string{image}) {
		t.Errorf("the media acquirer asked for %v, want %s", asked, image)
	}
}

func TestValidateAndContextInitAgreeOnOneDirectory(t *testing.T) {
	files := newVirtualOperatorFiles(t)
	environment := files.write(t, "env/environment.yaml", syntheticEnvironment)
	host := files.write(t, "env/controller.yaml", serviceHost)
	input := filepath.Dir(environment)
	services := operatorFileServices(t, files)
	contextRun(t, services, 0, "validate", "-f", input)
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	files.deny(host)
	want := diagnostics.Diagnostic{
		Severity: "error", Code: "input.read", Source: &diagnostics.SourceLocation{Path: host},
		Message:     "the invoking account cannot read this input path (permission denied)",
		Remediation: "give the invoking account read access to it, or copy it to a directory that account can read",
	}
	validate := []string{"validate", "-f", input}
	initialize := []string{"context", "init", "--name", "beta", "--input-dir", input}
	text := "[FAIL] input.read " + host + ": " + want.Message + "; next: " + want.Remediation
	for _, args := range [][]string{validate, initialize} {
		out, errOut := contextRun(t, services, 1, args...)
		if !slices.Contains(strings.Split(out+errOut, "\n"), text) {
			t.Errorf("%v reported\nstdout=%s\nstderr=%s\nwant the line %s", args, out, errOut, text)
		}
	}
	out, _ := contextRun(t, services, 1, append(slices.Clone(validate), "--output", "json")...)
	var envelope struct{ Diagnostics []diagnostics.Diagnostic }
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Diagnostics) != 1 || !sameDiagnostic(envelope.Diagnostics[0], want) {
		t.Errorf("validate reported %s, want %#v", out, want)
	}
}

func sameDiagnostic(got, want diagnostics.Diagnostic) bool {
	return got.Code == want.Code && got.Message == want.Message && got.Remediation == want.Remediation &&
		got.Source != nil && want.Source != nil && got.Source.Path == want.Source.Path
}

func TestMediaSourcesOpenThroughTheBoundOpener(t *testing.T) {
	files := newVirtualOperatorFiles(t)
	virtual := files.write(t, "image.iso", "installer")
	deps := localMediaDependencies(nil, nil, nil, nil, controller.Route{}, files)
	acquisition, err := deps.Acquirer.Open(context.Background(), media.Source{Path: virtual})
	if err != nil {
		t.Fatalf("open: %#v", diagnostics.Of(err))
	}
	defer acquisition.Payload.Close()
	if data, err := io.ReadAll(acquisition.Payload); err != nil || string(data) != "installer" {
		t.Fatalf("payload = %q (%v)", data, err)
	}
	unbound := localMediaDependencies(nil, nil, nil, nil, controller.Route{}, nil)
	_, err = unbound.Acquirer.Open(context.Background(), media.Source{Path: virtual})
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Message != "the media source opener is not configured" {
		t.Fatalf("an unbound opener did not refuse: %#v (%v)", reported, err)
	}
}

func TestAnUnboundOpenerReachesTheAdaptersAsNil(t *testing.T) {
	if reader := inputReader(nil); reader.Files != nil {
		t.Fatalf("the input reader received %#v for no opener", reader.Files)
	}
	if files := secretFiles(nil); files != nil {
		t.Fatalf("secret material received %#v for no opener", files)
	}
	if reader := inputReader(newVirtualOperatorFiles(t)); reader.Files == nil {
		t.Fatal("the input reader received no bound opener")
	}
	if files := secretFiles(newVirtualOperatorFiles(t)); files == nil {
		t.Fatal("secret material received no bound opener")
	}
}

// TestProductionBindsTheInvokingAccountsOpener holds the shipped graph to the
// opener: an adapter handed none opens operator-named paths with this
// process's own credentials, which in the elevated child are root's.
func TestProductionBindsTheInvokingAccountsOpener(t *testing.T) {
	deps, release := localServiceDependencies(processDependencies{})
	defer release()
	bound, ok := deps.Files.(openerFiles)
	if !ok || bound.opener == nil {
		t.Fatalf("production bound %#v as the invoking account's opener", deps.Files)
	}
	if reader := inputReader(deps.Files); reader.Files == nil {
		t.Fatal("production hands the input reader no opener")
	}
	if files := secretFiles(deps.Files); files == nil {
		t.Fatal("production hands secret material no opener")
	}
	missing := filepath.Join(t.TempDir(), "image.iso")
	_, err := deps.Media.Acquirer.Open(context.Background(), media.Source{Path: missing})
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Message == "the media source opener is not configured" {
		t.Fatalf("production hands media acquisition no opener: %#v (%v)", reported, err)
	}
}

// TestTheBoundOpenerServesInputAndSecretFiles drives the production opener,
// which a process that is not root runs in-process, through both adapters, so
// every open they ask of it passes its flag and name rules.
func TestTheBoundOpenerServesInputAndSecretFiles(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a root process would resolve the invoking account and start the helper")
	}
	files := openerFiles{opener: invokingAccount{resolver: privilege.Resolver{}}.files()}
	root := t.TempDir()
	input := filepath.Join(root, "env")
	if err := os.MkdirAll(filepath.Join(input, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"environment.yaml": syntheticEnvironment, "nested/controller.yaml": serviceHost, "value": "synthetic-opaque"} {
		if err := os.WriteFile(filepath.Join(input, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	sources, err := inputReader(files).ReadDirectory(context.Background(), input)
	if err != nil || len(sources.Files) != 2 {
		t.Fatalf("input = %v (%#v)", sources, diagnostics.Of(err))
	}
	data, err := inputReader(files).ReadFile(context.Background(), filepath.Join(input, "environment.yaml"), 4096)
	if err != nil || string(data) != syntheticEnvironment {
		t.Fatalf("input file = %q (%#v)", data, diagnostics.Of(err))
	}
	acquisition := secretmaterial.New(nil, secretmaterial.Options{Files: secretFiles(files), Operator: testMaterialOperator{home: root}})
	value, err := acquisition.Acquire(context.Background(), secrets.Declaration{Type: "opaque", Source: "contextStore"}, secrets.Input{ValueFile: filepath.Join(input, "value")})
	if err != nil {
		t.Fatalf("secret file: %#v", diagnostics.Of(err))
	}
	defer value.Clear()
	if got, _ := value.Part(secrets.ValuePart); string(got) != "synthetic-opaque" {
		t.Fatalf("secret value = %q", got)
	}
}
