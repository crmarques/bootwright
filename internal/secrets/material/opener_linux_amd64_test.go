//go:build linux && amd64

package material

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
)

type secretRequest struct {
	path  string
	flags int
}

// virtualSecretFiles serves absolute paths beneath a temporary root, so a
// path it serves exists nowhere acquisition could open on its own. It records
// every request, and replace may answer one itself.
type virtualSecretFiles struct {
	root    string
	prefix  string
	mu      sync.Mutex
	issued  map[*os.File]string
	asked   []secretRequest
	replace func(path string, flags int) (*os.File, bool, error)
}

func newVirtualSecretFiles(t *testing.T) *virtualSecretFiles {
	t.Helper()
	prefix := filepath.Join("/", "bootwright-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	if _, err := os.Lstat(prefix); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the virtual prefix exists on this host, so the test proves nothing: %v", err)
	}
	return &virtualSecretFiles{root: t.TempDir(), prefix: prefix, issued: map[*os.File]string{}}
}

func (v *virtualSecretFiles) write(t *testing.T, name string, data []byte, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(v.prefix, name)
	if err := os.MkdirAll(v.host(filepath.Dir(path)), 0700); err != nil {
		t.Fatal(err)
	}
	writeSecretFile(t, v.host(filepath.Dir(path)), filepath.Base(path), data, mode)
	return path
}

func (v *virtualSecretFiles) host(path string) string { return filepath.Join(v.root, path) }

func (v *virtualSecretFiles) requested() []secretRequest {
	v.mu.Lock()
	defer v.mu.Unlock()
	return slices.Clone(v.asked)
}

func (v *virtualSecretFiles) Begin(context.Context) (FileSession, error) { return v, nil }

func (v *virtualSecretFiles) Root() (*os.File, error) {
	fd, err := syscall.Open(v.root, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	return v.issue(os.NewFile(uintptr(fd), "/"), "/"), nil
}

func (v *virtualSecretFiles) OpenAt(parent *os.File, name string, flags int) (*os.File, error) {
	v.mu.Lock()
	base, issued := v.issued[parent]
	path := filepath.Join(base, name)
	v.asked = append(v.asked, secretRequest{path: path, flags: flags})
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

func (*virtualSecretFiles) Close() error { return nil }

func (v *virtualSecretFiles) issue(file *os.File, path string) *os.File {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.issued[file] = path
	return file
}

// secretDenial is an opener's permission failure, which names whether it ran
// with root's credentials.
type secretDenial struct {
	errno syscall.Errno
	root  bool
}

func (d secretDenial) Error() string { return "open: " + d.errno.Error() }

func (d secretDenial) Unwrap() error { return d.errno }

func (d secretDenial) DeniedToRoot() bool { return d.root }

func acquireValueFile(service *Service, path string) (secrets.Material, error) {
	return service.Acquire(context.Background(), secrets.Declaration{Type: "opaque", Source: "contextStore"}, secrets.Input{ValueFile: path})
}

func TestSecretFilesOpenOnlyThroughTheOpener(t *testing.T) {
	files := newVirtualSecretFiles(t)
	absolute := files.write(t, "secrets/value", []byte("absolute-value"), 0600)
	home := filepath.Join(files.prefix, "home")
	inHome := files.write(t, "home/token", []byte("home-value"), 0400)
	service := New(nil, Options{Files: files, Operator: fixedOperator{FileIdentity{UID: os.Getuid(), Home: home}}})
	for _, test := range []struct{ authored, path, want string }{
		{absolute, absolute, "absolute-value"},
		{"~/token", inHome, "home-value"},
	} {
		before := len(files.requested())
		value, err := acquireValueFile(service, test.authored)
		if err != nil {
			t.Fatalf("%s: %#v", test.authored, diagnostics.Of(err))
		}
		got := requiredPart(t, value, secrets.ValuePart)
		if string(got) != test.want {
			t.Fatalf("%s = %q, want %q", test.authored, got, test.want)
		}
		value.Clear()
		asked := files.requested()[before:]
		parent := filepath.Dir(test.path)
		for _, want := range []secretRequest{
			{files.prefix, pathHandle | syscall.O_DIRECTORY},
			{parent, pathHandle | syscall.O_DIRECTORY},
			{test.path, syscall.O_RDONLY | syscall.O_NONBLOCK},
		} {
			if !slices.Contains(asked, want) {
				t.Errorf("%s: acquisition opened %s (%#x) without the opener; it asked for %v", test.authored, want.path, want.flags, asked)
			}
		}
		walk := []secretRequest{{files.prefix, pathHandle | syscall.O_DIRECTORY}, {parent, pathHandle | syscall.O_DIRECTORY}, {test.path, pathHandle}}
		if len(asked) < len(walk) || !slices.Equal(asked[len(asked)-len(walk):], walk) {
			t.Errorf("%s: the fresh walk asked for %v, want it to end with %v", test.authored, asked, walk)
		}
	}
}

func TestSecretFilesReproveReceivedDescriptors(t *testing.T) {
	const unsafe = "secret file type, owner, links, or permissions are unsafe"
	for _, test := range []struct {
		name  string
		mode  os.FileMode
		links bool
		owner int
	}{
		{name: "group and world readable", mode: 0644, owner: os.Getuid()},
		{name: "two links", mode: 0600, links: true, owner: os.Getuid()},
		{name: "another account's file", mode: 0600, owner: os.Getuid() + 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			files := newVirtualSecretFiles(t)
			path := files.write(t, "secrets/value", []byte("value"), test.mode)
			if test.links {
				if err := os.Link(files.host(path), files.host(path)+"-link"); err != nil {
					t.Fatal(err)
				}
			}
			service := New(nil, Options{Files: files, Operator: fixedOperator{FileIdentity{UID: test.owner, Home: files.prefix}}})
			value, err := acquireValueFile(service, path)
			value.Clear()
			assertFailureCode(t, err, "secret.input")
			if reported := diagnostics.Of(err)[0]; reported.Message != unsafe || reported.Source == nil || reported.Source.Path != path {
				t.Fatalf("diagnostic = %#v, want %q at %s", reported, unsafe, path)
			}
		})
	}
	t.Run("changed between open and verify", func(t *testing.T) {
		files := newVirtualSecretFiles(t)
		first := files.write(t, "secrets/certificate", []byte("certificate"), 0600)
		second := files.write(t, "secrets/key", []byte("key"), 0600)
		files.replace = func(path string, flags int) (*os.File, bool, error) {
			if path == second && flags&pathHandle == 0 {
				changed := time.Now().Add(-time.Hour)
				if err := os.Chtimes(files.host(first), changed, changed); err != nil {
					return nil, true, err
				}
			}
			return nil, false, nil
		}
		service := New(nil, Options{Files: files, Operator: fixedOperator{FileIdentity{UID: os.Getuid(), Home: files.prefix}}})
		parts, err := service.readFileParts(context.Background(), []fileRequest{
			{Part: secrets.CertificatePart, Path: first}, {Part: secrets.PrivateKeyPart, Path: second},
		})
		clearParts(parts)
		assertFailureCode(t, err, "secret.input")
		if reported := diagnostics.Of(err)[0]; reported.Message != "secret file changed during acquisition" || reported.Source == nil || reported.Source.Path != first {
			t.Fatalf("diagnostic = %#v", reported)
		}
	})
}

func TestSecretFilePermissionDenialsNameTheCause(t *testing.T) {
	const (
		accountMessage     = "the invoking account cannot read this secret file or a directory above it (permission denied)"
		accountRemediation = "give the invoking account read access to it, or copy it to a directory that account can read"
		rootMessage        = "root cannot read this secret file or a directory above it (a network home with root squash?)"
		rootRemediation    = "copy it to a local directory and name the copy"
	)
	for _, errno := range []syscall.Errno{syscall.EACCES, syscall.EPERM} {
		for _, root := range []bool{false, true} {
			for _, place := range []string{"home", "final open"} {
				t.Run(errno.Error()+"/root="+strconv.FormatBool(root)+"/"+place, func(t *testing.T) {
					files := newVirtualSecretFiles(t)
					home := filepath.Join(files.prefix, "home")
					token := files.write(t, "home/token", []byte("value"), 0600)
					denied := home
					if place == "final open" {
						denied = token
					}
					files.replace = func(path string, flags int) (*os.File, bool, error) {
						if path == denied && (place == "home" || flags&pathHandle == 0) {
							return nil, true, secretDenial{errno: errno, root: root}
						}
						return nil, false, nil
					}
					service := New(nil, Options{Files: files, Operator: fixedOperator{FileIdentity{UID: os.Getuid(), Home: home}}})
					value, err := acquireValueFile(service, "~/token")
					value.Clear()
					message, remediation := accountMessage, accountRemediation
					if root {
						message, remediation = rootMessage, rootRemediation
					}
					assertSecretDenial(t, err, "~/token", message, remediation)
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
		files := newVirtualSecretFiles(t)
		home := filepath.Join(files.prefix, "home")
		token := files.write(t, "home/token", []byte("value"), 0600)
		files.replace = func(path string, flags int) (*os.File, bool, error) {
			if path != token {
				return nil, false, nil
			}
			file, err := os.OpenFile(reopen, flags|syscall.O_CLOEXEC, 0)
			return file, true, err
		}
		service := New(nil, Options{Files: files, Operator: fixedOperator{FileIdentity{UID: os.Getuid(), Home: home}}})
		value, err := acquireValueFile(service, "~/token")
		value.Clear()
		assertSecretDenial(t, err, "~/token", accountMessage, accountRemediation)
		file, err := os.OpenFile(reopen, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		parts, err := readHeldParts(context.Background(), []heldPart{{request: fileRequest{Part: secrets.ValuePart, Path: "~/token"}, file: file}}, "input", true)
		clearParts(parts)
		assertSecretDenial(t, err, "~/token", rootMessage, rootRemediation)
	})
}

func assertSecretDenial(t *testing.T, err error, source, message, remediation string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "secret.input" || reported[0].Source == nil || reported[0].Source.Path != source ||
		reported[0].Message != message || reported[0].Remediation != remediation {
		t.Fatalf("diagnostics = %#v, want %q (%q) at %s", reported, message, remediation, source)
	}
}

type unavailableSecretFiles struct{}

func (unavailableSecretFiles) Begin(context.Context) (FileSession, error) {
	return nil, errors.New("the opener's helper did not start")
}

func TestSecretFilesRefuseWhenTheOpenerCannotBegin(t *testing.T) {
	path := writeSecretFile(t, t.TempDir(), "value", []byte("value"), 0600)
	service := New(nil, Options{Files: unavailableSecretFiles{}, Operator: fixedOperator{FileIdentity{UID: os.Getuid(), Home: filepath.Dir(path)}}})
	value, err := acquireValueFile(service, path)
	value.Clear()
	assertFailureCode(t, err, "secret.input")
	if message := diagnosticMessage(err); message != "secret files cannot be opened under the invoking account" {
		t.Fatalf("message = %q", message)
	}
}
