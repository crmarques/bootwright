package inputfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/desiredstate"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

type request struct {
	path  string
	flags int
}

// virtualFiles serves absolute paths beneath a temporary root, so a path it
// serves exists nowhere the reader could open on its own. It records every
// request, and replace may answer one itself.
type virtualFiles struct {
	root    string
	prefix  string
	mu      sync.Mutex
	issued  map[*os.File]string
	asked   []request
	replace func(path string, flags int) (*os.File, bool, error)
}

func newVirtualFiles(t *testing.T) *virtualFiles {
	t.Helper()
	prefix := filepath.Join("/", "bootwright-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	if _, err := os.Lstat(prefix); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the virtual prefix exists on this host, so the test proves nothing: %v", err)
	}
	return &virtualFiles{root: t.TempDir(), prefix: prefix, issued: map[*os.File]string{}}
}

func (v *virtualFiles) write(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(v.prefix, name)
	writeFixture(t, v.root, path, content)
	return path
}

func (v *virtualFiles) requested() []request {
	v.mu.Lock()
	defer v.mu.Unlock()
	return slices.Clone(v.asked)
}

func (v *virtualFiles) Begin(context.Context) (FileSession, error) { return v, nil }

func (v *virtualFiles) Root() (*os.File, error) {
	fd, err := syscall.Open(v.root, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return v.issue(os.NewFile(uintptr(fd), "/"), "/"), nil
}

func (v *virtualFiles) OpenAt(parent *os.File, name string, flags int) (*os.File, error) {
	v.mu.Lock()
	base, issued := v.issued[parent]
	path := filepath.Join(base, name)
	v.asked = append(v.asked, request{path: path, flags: flags})
	v.mu.Unlock()
	if !issued {
		return nil, syscall.EBADF
	}
	if v.replace != nil {
		if file, answered, err := v.replace(path, flags); answered {
			if err != nil {
				return nil, err
			}
			return v.issue(file, path), nil
		}
	}
	fd, err := syscall.Openat(int(parent.Fd()), name, flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return v.issue(os.NewFile(uintptr(fd), path), path), nil
}

func (*virtualFiles) Close() error { return nil }

func (v *virtualFiles) issue(file *os.File, path string) *os.File {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.issued[file] = path
	return file
}

// openHost opens a host path in place of a virtual one, with the flags the
// reader asked for.
func openHost(host string, flags int) (*os.File, bool, error) {
	fd, err := syscall.Open(host, flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, true, err
	}
	return os.NewFile(uintptr(fd), host), true, nil
}

// denial is an opener's permission failure, which names whether it ran with
// root's credentials.
type denial struct {
	errno syscall.Errno
	root  bool
}

func (d denial) Error() string { return "open: " + d.errno.Error() }

func (d denial) Unwrap() error { return d.errno }

func (d denial) DeniedToRoot() bool { return d.root }

func TestReaderOpensEveryInputThroughItsOpener(t *testing.T) {
	files := newVirtualFiles(t)
	environment := files.write(t, "env/environment.yaml", "environment")
	inside := files.write(t, "env/nested/inside.yml", "inside")
	single := files.write(t, "single.yaml", "single")
	input := filepath.Dir(environment)
	reader := Reader{Files: files}
	ctx := context.Background()
	want := map[string]string{environment: "environment", inside: "inside"}
	for name, read := range map[string]func() (desiredstate.Sources, error){
		"Read":          func() (desiredstate.Sources, error) { return reader.Read(ctx, []string{input}) },
		"ReadDirectory": func() (desiredstate.Sources, error) { return reader.ReadDirectory(ctx, input) },
	} {
		sources, err := read()
		if err != nil {
			t.Fatalf("%s: %#v", name, diagnostics.Of(err))
		}
		got := map[string]string{}
		for _, file := range sources.Files {
			got[file.Path()] = string(file.Bytes())
		}
		if !reflect.DeepEqual(got, want) || !slices.Equal(sources.Roots, []string{input}) {
			t.Fatalf("%s = %v from %v, want %v from %s", name, got, sources.Roots, want, input)
		}
	}
	sources, err := reader.Read(ctx, []string{single})
	if err != nil || len(sources.Files) != 1 || sources.Files[0].Path() != single || string(sources.Files[0].Bytes()) != "single" {
		t.Fatalf("Read of one file = %v (%#v)", sources, diagnostics.Of(err))
	}
	data, err := reader.ReadFile(ctx, single, 64)
	if err != nil || string(data) != "single" {
		t.Fatalf("ReadFile = %q (%#v)", data, diagnostics.Of(err))
	}
	asked := map[string]bool{}
	for _, r := range files.requested() {
		asked[r.path] = true
	}
	for _, path := range []string{files.prefix, input, environment, filepath.Dir(inside), inside, single} {
		if !asked[path] {
			t.Errorf("the reader opened %s without its opener", path)
		}
	}
}

func TestReaderReprovesTheDescriptorsItReceives(t *testing.T) {
	host := t.TempDir()
	other := writeFixture(t, host, "other.yaml", "substitute")
	fifo := filepath.Join(host, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(host, "directory")
	writeFixture(t, directory, "environment.yaml", "environment")
	for _, test := range []struct {
		name    string
		replace func(input, environment, nested string) func(string, int) (*os.File, bool, error)
		code    string
		message string
		source  func(input, environment, nested string) string
	}{
		{
			name: "another regular file at the read open",
			replace: func(_, environment, _ string) func(string, int) (*os.File, bool, error) {
				return func(path string, flags int) (*os.File, bool, error) {
					if path == environment && flags&pathHandle == 0 {
						return openHost(other, flags)
					}
					return nil, false, nil
				}
			},
			code: "input.read", message: "input changed before reading",
			source: func(_, environment, _ string) string { return environment },
		},
		{
			name: "a FIFO where a directory was proved",
			replace: func(_, environment, nested string) func(string, int) (*os.File, bool, error) {
				reading := false
				return func(path string, flags int) (*os.File, bool, error) {
					reading = reading || path == environment && flags&pathHandle == 0
					if reading && path == nested && flags == pathHandle {
						return openHost(fifo, flags)
					}
					return nil, false, nil
				}
			},
			code: "input.not-directory", message: "input ancestor must be a directory without symbolic links",
			source: func(_, _, nested string) string { return nested },
		},
		{
			name: "another directory on the re-walk",
			replace: func(input, environment, _ string) func(string, int) (*os.File, bool, error) {
				reading := false
				return func(path string, flags int) (*os.File, bool, error) {
					reading = reading || path == environment && flags&pathHandle == 0
					if reading && path == input {
						return openHost(directory, flags)
					}
					return nil, false, nil
				}
			},
			code: "input.read", message: "input directory was replaced",
			source: func(input, _, _ string) string { return input },
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := newVirtualFiles(t)
			environment := files.write(t, "env/environment.yaml", "environment")
			inside := files.write(t, "env/nested/inside.yaml", "inside")
			input, nested := filepath.Dir(environment), filepath.Dir(inside)
			files.replace = test.replace(input, environment, nested)
			result, err := (Reader{Files: files}).ReadDirectory(context.Background(), input)
			assertFailure(t, result, err, test.code)
			reported := diagnostics.Of(err)[0]
			if reported.Message != test.message || reported.Source == nil || reported.Source.Path != test.source(input, environment, nested) {
				t.Fatalf("diagnostic = %#v, want %q at %s", reported, test.message, test.source(input, environment, nested))
			}
		})
	}
}

func TestReaderNamesWhoCannotReadAnInput(t *testing.T) {
	const (
		accountMessage     = "the invoking account cannot read this input path (permission denied)"
		accountRemediation = "give the invoking account read access to it, or copy it to a directory that account can read"
		rootMessage        = "root cannot read this input path (a network home with root squash?)"
		rootRemediation    = "copy it to a local directory and name the copy"
	)
	for _, errno := range []syscall.Errno{syscall.EACCES, syscall.EPERM} {
		for _, root := range []bool{false, true} {
			for _, place := range []string{"ancestor", "final read"} {
				t.Run(errno.Error()+"/root="+strconv.FormatBool(root)+"/"+place, func(t *testing.T) {
					files := newVirtualFiles(t)
					environment := files.write(t, "env/environment.yaml", "environment")
					input := filepath.Dir(environment)
					denied := files.prefix
					if place == "final read" {
						denied = environment
					}
					files.replace = func(path string, flags int) (*os.File, bool, error) {
						if path == denied && (place == "ancestor" || flags&pathHandle == 0) {
							return nil, true, denial{errno: errno, root: root}
						}
						return nil, false, nil
					}
					result, err := (Reader{Files: files}).ReadDirectory(context.Background(), input)
					assertFailure(t, result, err, "input.read")
					message, remediation := accountMessage, accountRemediation
					if root {
						message, remediation = rootMessage, rootRemediation
					}
					assertDenial(t, err, denied, message, remediation)
				})
			}
		}
	}
	t.Run("a read through a received descriptor", func(t *testing.T) {
		stack, err := os.OpenFile("/proc/self/stack", pathHandle|syscall.O_CLOEXEC, 0)
		if err != nil {
			t.Skipf("no kernel stack file to read: %v", err)
		}
		defer stack.Close()
		reopen := "/proc/self/fd/" + strconv.Itoa(int(stack.Fd()))
		if data, err := os.ReadFile(reopen); !errors.Is(err, syscall.EACCES) {
			t.Skipf("this process may read its kernel stack (%d bytes, %v), so no read is denied", len(data), err)
		}
		files := newVirtualFiles(t)
		environment := files.write(t, "env/environment.yaml", "environment")
		input := filepath.Dir(environment)
		files.replace = func(path string, flags int) (*os.File, bool, error) {
			if path != environment {
				return nil, false, nil
			}
			file, err := os.OpenFile(reopen, flags|syscall.O_CLOEXEC, 0)
			return file, true, err
		}
		result, err := (Reader{Files: files}).ReadDirectory(context.Background(), input)
		assertFailure(t, result, err, "input.read")
		assertDenial(t, err, environment, accountMessage, accountRemediation)
		scan, err := discoverThrough(context.Background(), files)
		if err != nil {
			t.Fatal(err)
		}
		defer scan.root.Close()
		scan.rootReads = true
		if err := scan.source(input, true); err != nil {
			t.Fatalf("discovery: %#v", diagnostics.Of(err))
		}
		_, err = scan.readFiles(scan.files, desiredstate.MaxFileBytes, desiredstate.MaxAllFileBytes, "YAML file bytes", "aggregate YAML bytes")
		assertDenial(t, err, environment, rootMessage, rootRemediation)
	})
}

func assertDenial(t *testing.T, err error, source, message, remediation string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "input.read" || reported[0].Source == nil || reported[0].Source.Path != source ||
		reported[0].Message != message || reported[0].Remediation != remediation {
		t.Fatalf("diagnostics = %#v, want %q (%q) at %s", reported, message, remediation, source)
	}
}

type unavailableFiles struct{}

func (unavailableFiles) Begin(ctx context.Context) (FileSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, errors.New("the opener's helper did not start")
}

func TestReaderRefusesWhenItsOpenerCannotBegin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "input")
	reader := Reader{Files: unavailableFiles{}}
	_, err := reader.ReadDirectory(context.Background(), path)
	_, fileErr := reader.ReadFile(context.Background(), filepath.Join(path, "context.yaml"), 64)
	for want, err := range map[string]error{path: err, filepath.Join(path, "context.yaml"): fileErr} {
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "input.read" || reported[0].Message != "the input cannot be opened under the invoking account" ||
			reported[0].Source == nil || reported[0].Source.Path != want {
			t.Fatalf("diagnostics = %#v (%v), want the opener's failure at %s", reported, err, want)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := reader.ReadDirectory(ctx, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled read = %v", err)
	}
}
