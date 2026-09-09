//go:build linux && amd64

package selectionfs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

func TestMain(m *testing.M) {
	if handled, code := ServeHelper(context.Background(), os.Args[1:], os.Stdin, os.Stdout); handled {
		os.Exit(code)
	}
	os.Exit(m.Run())
}

func fixture(t *testing.T) (*Store, string) {
	t.Helper()
	home := t.TempDir()
	return New(Options{UID: os.Getuid(), GID: os.Getegid(), Home: home}), home
}

func TestSelectionRoundTripPrivateAndConditionalClear(t *testing.T) {
	store, home := fixture(t)
	ctx := context.Background()
	if got, err := store.Read(ctx); err != nil || got != (contexts.Selection{}) {
		t.Fatalf("missing: %v %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".bootwright")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("read created selection directory")
	}
	first := contexts.Selection{Version: 1, Name: "test", ID: "ctx-00000000000000000000000000000001"}
	second := contexts.Selection{Version: 1, Name: "other", ID: "ctx-00000000000000000000000000000002"}
	if err := store.Write(ctx, first); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		path string
		mode os.FileMode
	}{{".bootwright", 0700}, {".bootwright/context", 0600}} {
		info, err := os.Lstat(filepath.Join(home, test.path))
		if err != nil {
			t.Fatal(err)
		}
		stat := info.Sys().(*syscall.Stat_t)
		if info.Mode().Perm() != test.mode || stat.Uid != uint32(os.Getuid()) || stat.Gid != uint32(os.Getegid()) {
			t.Fatalf("unsafe %s", test.path)
		}
	}
	if err := store.Write(ctx, second); err != nil {
		t.Fatal(err)
	}
	if err := store.Clear(ctx, first); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Read(ctx); err != nil || got != second {
		t.Fatalf("compare clear: %v %v", got, err)
	}
	if err := store.Clear(ctx, second); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Read(ctx); err != nil || got != (contexts.Selection{}) {
		t.Fatalf("cleared: %v %v", got, err)
	}
	entries, err := os.ReadDir(filepath.Join(home, ".bootwright"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("staging residue %v %v", entries, err)
	}
}

// Err is checked after the record has been observed and before its final
// verification. Substitution here deterministically exercises that boundary.
type afterObservationContext struct {
	context.Context
	checks int
	change func()
}

func (c *afterObservationContext) Err() error {
	c.checks++
	if c.checks == 2 {
		c.change()
	}
	return c.Context.Err()
}

func TestSelectionRejectsRecordSubstitutionAfterObservation(t *testing.T) {
	for _, action := range []string{"read", "write", "clear"} {
		for _, replacement := range []string{"new-pointer", "same-pointer", "in-place", "symlink", "fifo", "appeared"} {
			t.Run(action+"/"+replacement, func(t *testing.T) {
				store, home := fixture(t)
				first := contexts.Selection{Version: 1, Name: "test", ID: "ctx-00000000000000000000000000000001"}
				newer := contexts.Selection{Version: 1, Name: "other", ID: "ctx-00000000000000000000000000000002"}
				if err := store.Write(context.Background(), first); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(home, ".bootwright", "context")
				outside := filepath.Join(home, "outside")
				if err := os.WriteFile(outside, []byte("untouched"), 0600); err != nil {
					t.Fatal(err)
				}
				if replacement == "appeared" {
					if err := os.Remove(path); err != nil {
						t.Fatal(err)
					}
				}
				var substituted os.FileInfo
				ctx := &afterObservationContext{Context: context.Background(), change: func() {
					value := newer
					if replacement == "same-pointer" {
						value = first
					}
					data, err := json.Marshal(value)
					if err != nil {
						t.Fatal(err)
					}
					data = append(data, '\n')
					switch replacement {
					case "in-place", "appeared":
						err = os.WriteFile(path, data, 0600)
					case "symlink", "fifo":
						if err = os.Remove(path); err == nil {
							if replacement == "symlink" {
								err = os.Symlink(outside, path)
							} else {
								err = syscall.Mkfifo(path, 0600)
							}
						}
					default:
						staged := filepath.Join(home, ".bootwright", "replacement")
						if err = os.WriteFile(staged, data, 0600); err == nil {
							err = os.Rename(staged, path)
						}
					}
					if err != nil {
						t.Fatal(err)
					}
					substituted, err = os.Lstat(path)
					if err != nil {
						t.Fatal(err)
					}
				}}
				// Clear must preserve the newer pointer even though its initial
				// observation matched the caller's expected selection exactly.
				if _, err := store.local(ctx, action, first); err == nil {
					t.Fatal("record substitution accepted")
				}
				if substituted == nil {
					t.Fatal("substitution checkpoint was not reached")
				}
				after, err := os.Lstat(path)
				if err != nil || !sameRecord(*substituted.Sys().(*syscall.Stat_t), *after.Sys().(*syscall.Stat_t)) {
					t.Fatalf("substituted pointer was changed: %v", err)
				}
				if got, err := os.ReadFile(outside); err != nil || string(got) != "untouched" {
					t.Fatalf("outside file changed: %q %v", got, err)
				}
			})
		}
	}
}

func TestSelectionRejectsNoncanonicalRecords(t *testing.T) {
	canonical := `{"version":1,"name":"test","id":"ctx-00000000000000000000000000000001"}` + "\n"
	for _, data := range []string{
		strings.TrimSuffix(canonical, "\n"),
		"\n" + canonical,
		canonical + " ",
		strings.Replace(canonical, ":1", ": 1", 1),
		strings.Replace(canonical, "test", `\u0074est`, 1),
		`{"name":"test","version":1,"id":"ctx-00000000000000000000000000000001"}` + "\n",
	} {
		if _, err := decodeSelection([]byte(data)); err == nil {
			t.Fatalf("noncanonical selection accepted: %q", data)
		}
	}
	if _, err := decodeSelection([]byte(canonical)); err != nil {
		t.Fatalf("canonical selection refused: %v", err)
	}
}

func TestSelectionRejectsUnsafeStorage(t *testing.T) {
	for _, kind := range []string{"directory-symlink", "directory-mode", "file-symlink", "file-hardlink", "file-mode", "fifo", "oversized", "malformed", "unknown-field", "duplicate-field"} {
		t.Run(kind, func(t *testing.T) {
			store, home := fixture(t)
			dir := filepath.Join(home, ".bootwright")
			path := filepath.Join(dir, "context")
			value := contexts.Selection{Version: 1, Name: "test", ID: "ctx-00000000000000000000000000000001"}
			if err := store.Write(context.Background(), value); err != nil {
				t.Fatal(err)
			}
			outside := filepath.Join(home, "outside")
			if err := os.WriteFile(outside, []byte("untouched"), 0600); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "directory-symlink":
				os.Remove(path)
				os.Remove(dir)
				os.Symlink(home, dir)
			case "directory-mode":
				os.Chmod(dir, 0755)
			case "file-symlink":
				os.Remove(path)
				os.Symlink(outside, path)
			case "file-hardlink":
				os.Remove(path)
				os.Link(outside, path)
			case "file-mode":
				os.Chmod(path, 0644)
			case "fifo":
				os.Remove(path)
				syscall.Mkfifo(path, 0600)
			case "oversized":
				os.WriteFile(path, bytes.Repeat([]byte("x"), maximumRecord+1), 0600)
			case "malformed":
				os.WriteFile(path, []byte("{"), 0600)
			case "unknown-field":
				os.WriteFile(path, []byte(`{"version":1,"name":"test","id":"ctx-00000000000000000000000000000001","extra":true}`), 0600)
			case "duplicate-field":
				os.WriteFile(path, []byte(`{"version":1,"name":"other","name":"test","id":"ctx-00000000000000000000000000000001"}`), 0600)
			}
			if _, err := store.Read(context.Background()); err == nil {
				t.Fatal("unsafe selection accepted")
			}
			if err := store.Write(context.Background(), value); err == nil {
				t.Fatal("unsafe selection replaced")
			}
			if got, err := os.ReadFile(outside); err != nil || string(got) != "untouched" {
				t.Fatalf("outside changed %q %v", got, err)
			}
		})
	}
}

func TestSelectionCancellationAndLockRefusal(t *testing.T) {
	store, home := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	value := contexts.Selection{Version: 1, Name: "test", ID: "ctx-00000000000000000000000000000001"}
	if err := store.Write(ctx, value); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".bootwright")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled write created directory")
	}
	if err := store.Write(context.Background(), value); err != nil {
		t.Fatal(err)
	}
	dir, err := store.openDirectory(false)
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	if err := syscall.Flock(int(dir.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(context.Background()); err == nil {
		t.Fatal("locked selection accepted")
	}
}

func TestSelectionHelperBoundedProtocol(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("helper deliberately refuses root; credential-drop test covers this under root")
	}
	store, home := fixture(t)
	value := contexts.Selection{Version: 1, Name: "test", ID: "ctx-00000000000000000000000000000001"}
	data, err := json.Marshal(helperRequest{Account: store.options, Action: "write", Selection: value})
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	handled, code := ServeHelper(context.Background(), []string{HelperMode}, bytes.NewReader(data), &output)
	if !handled || code != 0 {
		t.Fatalf("helper %v %d", handled, code)
	}
	if _, err := os.Stat(filepath.Join(home, ".bootwright/context")); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{strings.Repeat("x", maximumRecord+1), `{"account":{"UID":0},"action":"write"}`, `{"action":"execute"}`} {
		if _, code := ServeHelper(context.Background(), []string{HelperMode}, strings.NewReader(input), &output); code == 0 {
			t.Fatal("unsafe helper request accepted")
		}
	}
}

func TestRootSelectionHelperDropsIdentityBeforeOpeningHome(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("credential-drop acceptance requires an isolated root test process")
	}
	base, err := os.MkdirTemp("/tmp", "bootwright-selection-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(base)
	if err := os.Chmod(base, 0755); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(base, "home")
	if err := os.Mkdir(home, 0700); err != nil {
		t.Fatal(err)
	}
	const uid, gid = 60001, 60002
	if err := os.Chown(home, uid, gid); err != nil {
		t.Fatal(err)
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// The package test binary may live inside a root-private go-build directory;
	// copying it into this fixture grants execute access only to the test helper.
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	executable := filepath.Join(base, "selection-helper")
	if err := os.WriteFile(executable, data, 0755); err != nil {
		t.Fatal(err)
	}
	value := contexts.Selection{Version: 1, Name: "test", ID: "ctx-00000000000000000000000000000001"}
	request, err := json.Marshal(helperRequest{Account: Options{UID: uid, GID: gid, Home: home}, Action: "write", Selection: value})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, executable, HelperMode)
	command.Env = []string{"LANG=C"}
	command.Stdin = bytes.NewReader(request)
	command.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: uid, Gid: gid, Groups: []uint32{gid}}}
	output, err := command.Output()
	if err != nil {
		t.Fatal(err)
	}
	var response helperResponse
	if json.Unmarshal(output, &response) != nil || response.Failed {
		t.Fatalf("helper response %s", output)
	}
	info, err := os.Stat(filepath.Join(home, ".bootwright", "context"))
	if err != nil {
		t.Fatal(err)
	}
	stat := info.Sys().(*syscall.Stat_t)
	if stat.Uid != uid || stat.Gid != gid || info.Mode().Perm() != 0600 {
		t.Fatal("helper published with incorrect identity")
	}
	// Run the complete root-side adapter from the accessible fixture executable,
	// so its exact-self check and its actual credential-drop path are both used.
	adapter := exec.CommandContext(ctx, executable, "-test.run", "^TestRootSelectionAdapterFixture$", "-test.v")
	adapter.Env = []string{"LANG=C", "BOOTWRIGHT_SELECTION_FIXTURE=" + home}
	if output, err := adapter.CombinedOutput(); err != nil {
		t.Fatalf("root adapter fixture failed: %v\n%s", err, output)
	}
}

func TestRootSelectionAdapterFixture(t *testing.T) {
	home := os.Getenv("BOOTWRIGHT_SELECTION_FIXTURE")
	if home == "" {
		t.Skip("invoked by isolated credential-drop acceptance fixture")
	}
	if os.Geteuid() != 0 || !strings.HasPrefix(home, "/tmp/bootwright-selection-test-") {
		t.Fatal("invalid isolated fixture")
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	store := New(Options{UID: 60001, GID: 60002, Home: home, Groups: []uint32{60002}, Executable: executable})
	ctx := context.Background()
	first := contexts.Selection{Version: 1, Name: "test", ID: "ctx-00000000000000000000000000000001"}
	second := contexts.Selection{Version: 1, Name: "other", ID: "ctx-00000000000000000000000000000002"}
	if got, err := store.Read(ctx); err != nil || got != first {
		t.Fatalf("read %v %v", got, err)
	}
	if err := store.Write(ctx, second); err != nil {
		t.Fatal(err)
	}
	if err := store.Clear(ctx, first); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Read(ctx); err != nil || got != second {
		t.Fatalf("compare clear %v %v", got, err)
	}
	if err := store.Clear(ctx, second); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Read(ctx); err != nil || got != (contexts.Selection{}) {
		t.Fatalf("cleared %v %v", got, err)
	}
}
