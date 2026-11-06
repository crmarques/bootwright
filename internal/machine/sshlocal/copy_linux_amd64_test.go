//go:build linux && amd64

package sshlocal

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/machine"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
)

func copyFixture(t *testing.T) (Copier, context.Context) {
	t.Helper()
	if _, err := os.Stat("/usr/bin/python3"); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	client := filepath.Join(dir, "ssh-fixture")
	if err := os.WriteFile(client, []byte("#!/bin/sh\nexec /usr/bin/python3 -I -B -c "+quoteWord(copyWorkerSource)+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	l := New(nil, nil, nil)
	l.Client, l.Scratch, l.PolicyPath = client, dir, ""
	l.qualify = func(value string) (string, error) { return value, nil }
	c := Copier{qualify: func(value string) (string, error) { return value, nil }, SSH: l, Account: func(context.Context) (machineaccess.CopyAccount, error) {
		return machineaccess.CopyAccount{UID: os.Geteuid(), GID: os.Getegid()}, nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return c, ctx
}

func prepareFixture(t *testing.T, c Copier, ctx context.Context, source, destination string, download, collect, overwrite bool) machineaccess.PreparedCopy {
	t.Helper()
	s := keySession()
	s.Machine = "guest"
	p, err := c.Prepare(ctx, s, machineaccess.CopyRequest{Source: machine.CopyPath{Remote: download, Path: source}, Destination: machine.CopyPath{Remote: !download, Path: destination}, Overwrite: overwrite}, collect)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func TestCopyFilesAndDirectoriesInBothDirections(t *testing.T) {
	for _, download := range []bool{false, true} {
		t.Run(map[bool]string{false: "upload", true: "download"}[download], func(t *testing.T) {
			c, ctx := copyFixture(t)
			root := t.TempDir()
			source := filepath.Join(root, "source")
			if err := os.Mkdir(source, 0o700); err != nil {
				t.Fatal(err)
			}
			data := bytes.Repeat([]byte{0, 1, 255, 10}, 256<<10)
			if err := os.WriteFile(filepath.Join(source, "binary"), data, 0o751); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(filepath.Join(source, "binary"), 0o751); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, "empty"), nil, 0o600); err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(root, "new-directory")
			p := prepareFixture(t, c, ctx, source, destination, download, false, false)
			if _, err := os.Stat(destination); !os.IsNotExist(err) {
				t.Fatalf("preflight wrote destination: %v", err)
			}
			progress, err := p.Run(ctx)
			if err != nil || progress.Files != 2 || progress.Bytes != int64(len(data)) {
				t.Fatalf("progress %+v: %v", progress, err)
			}
			got, err := os.ReadFile(filepath.Join(destination, "binary"))
			if err != nil || !bytes.Equal(got, data) {
				t.Fatalf("binary differs: %v", err)
			}
			info, err := os.Stat(filepath.Join(destination, "binary"))
			if err != nil || info.Mode().Perm() != 0o751 {
				t.Fatalf("permissions: %v, %v", info, err)
			}
		})
	}
}

func TestCopyCollectionCreatesMachineAndSourceDirectories(t *testing.T) {
	c, ctx := copyFixture(t)
	root := t.TempDir()
	source := filepath.Join(root, "logs")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "app.log"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "collected")
	p := prepareFixture(t, c, ctx, source, destination, true, true, false)
	if p.Destination() != filepath.Join(destination, "guest", "logs") {
		t.Fatal(p.Destination())
	}
	if _, err := p.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(destination, "guest", "logs", "app.log")); err != nil || string(data) != "hello" {
		t.Fatalf("data %q: %v", data, err)
	}
}

func TestCopyOverwritePreservesDestinationUntilComplete(t *testing.T) {
	c, ctx := copyFixture(t)
	root := t.TempDir()
	source, destination := filepath.Join(root, "source"), filepath.Join(root, "destination")
	if err := os.WriteFile(source, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := keySession()
	request := machineaccess.CopyRequest{Source: machine.CopyPath{Path: source}, Destination: machine.CopyPath{Remote: true, Path: destination}}
	if _, err := c.Prepare(ctx, s, request, false); err == nil || !strings.Contains(err.Error(), "destination-exists") {
		t.Fatalf("overwrite admitted: %v", err)
	}
	p := prepareFixture(t, c, ctx, source, destination, false, false, true)
	if err := os.WriteFile(source, []byte("changed source"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Run(ctx); err == nil {
		t.Fatal("source change admitted")
	}
	if got, _ := os.ReadFile(destination); string(got) != "old" {
		t.Fatalf("destination overwritten: %q", got)
	}
	p = prepareFixture(t, c, ctx, source, destination, false, false, true)
	if _, err := p.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(destination); string(got) != "changed source" {
		t.Fatalf("destination: %q", got)
	}
}

func TestCopyRejectsSymlinksAndSpecialFilesWithoutWriting(t *testing.T) {
	for _, shape := range []string{"source-link", "parent-link", "destination-link", "fifo"} {
		t.Run(shape, func(t *testing.T) {
			c, ctx := copyFixture(t)
			root := t.TempDir()
			source, destination := filepath.Join(root, "source"), filepath.Join(root, "destination")
			if err := os.WriteFile(source, []byte("payload"), 0o600); err != nil {
				t.Fatal(err)
			}
			switch shape {
			case "source-link":
				if err := os.Symlink(source, filepath.Join(root, "link")); err != nil {
					t.Fatal(err)
				}
				source = filepath.Join(root, "link")
			case "parent-link":
				if err := os.Symlink(root, filepath.Join(root, "link")); err != nil {
					t.Fatal(err)
				}
				destination = filepath.Join(root, "link", "destination")
			case "destination-link":
				if err := os.Symlink(source, destination); err != nil {
					t.Fatal(err)
				}
			case "fifo":
				if err := os.Remove(source); err != nil {
					t.Fatal(err)
				}
				if err := syscall.Mkfifo(source, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			s := keySession()
			_, err := c.Prepare(ctx, s, machineaccess.CopyRequest{Source: machine.CopyPath{Path: source}, Destination: machine.CopyPath{Remote: true, Path: destination}, Overwrite: true}, false)
			if err == nil {
				t.Fatal("unsafe path admitted")
			}
		})
	}
}

func TestCopyTrailingDestinationSlashCreatesContainer(t *testing.T) {
	c, ctx := copyFixture(t)
	root := t.TempDir()
	source := filepath.Join(root, "source")
	if err := os.WriteFile(source, []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(root, "container") + "/"
	p := prepareFixture(t, c, ctx, source, destination, true, false, false)
	if _, err := p.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destination, "source")); err != nil {
		t.Fatal(err)
	}
}

func TestCopyWorkerExitAndCancellationUnblockProtocolReaders(t *testing.T) {
	for _, cancelEarly := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		worker, err := startCopyWorker(ctx, func(ctx context.Context, in io.Reader, out io.Writer) error {
			code := "pass"
			if cancelEarly {
				code = "import time; time.sleep(60)"
			}
			command := exec.CommandContext(ctx, "/usr/bin/python3", "-I", "-B", "-c", code)
			command.Stdin, command.Stdout = in, out
			return command.Run()
		})
		if err != nil {
			t.Fatal(err)
		}
		finished := make(chan error, 1)
		go func() { _, err := worker.receive(); finished <- err }()
		if cancelEarly {
			cancel()
		}
		select {
		case err := <-finished:
			if err == nil {
				t.Error("unexpected protocol success")
			}
		case <-time.After(2 * time.Second):
			cancel()
			worker.close()
			t.Fatal("reader remained blocked after worker exit")
		}
		_ = worker.close()
		cancel()
	}
}

func TestCopyPrivateStageSubstitutionAndFailureCleanup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	module, _, ok := strings.Cut(copyWorkerSource, "\nsignal.signal(")
	if !ok {
		t.Fatal("worker entrypoint moved")
	}
	code := module + `
import io
import errno
OUTPUT = io.BytesIO()
root = sys.argv[1]
destination = root + "/result"
entry = {"path":"", "kind":"file", "mode":384, "size":7}
worker = Worker()
worker.prepare({"path":destination,"overwrite":False,"entries":[entry],"parents":[]})
INPUT = io.BytesIO(b"payload")
worker.begin("")
parent_fd, private, target, stage, name, entry = worker.stage
os.rename(stage, stage+"-moved", src_dir_fd=parent_fd, dst_dir_fd=parent_fd)
os.symlink("unrelated", stage, dir_fd=parent_fd)
worker.commit()
worker.close()
assert open(destination,"rb").read() == b"payload"
assert os.path.islink(root + "/" + stage)
worker = Worker()
worker.prepare({"path":root+"/failed","overwrite":False,"entries":[entry],"parents":[]})
original_open = os.open
def failed_open(name, *args, **kwargs):
    if name == "payload":
        raise OSError(errno.ENOSPC, "injected-full-filesystem")
    return original_open(name,*args,**kwargs)
os.open = failed_open
before = set(os.listdir(root))
try:
    worker.begin("")
    raise AssertionError("stage creation should fail")
except OSError:
    pass
finally:
    os.open = original_open
    worker.close()
assert set(os.listdir(root)) == before
worker = Worker()
worker.prepare({"path":root+"/failed-directory-open","overwrite":False,"entries":[entry],"parents":[]})
def failed_directory_open(name, *args, **kwargs):
    if name.startswith(".bootwright-cp-"):
        raise OSError(errno.EMFILE, "injected-file-limit")
    return original_open(name,*args,**kwargs)
os.open = failed_directory_open
before = set(os.listdir(root))
try:
    worker.begin("")
    raise AssertionError("stage directory open should fail")
except OSError:
    pass
finally:
    os.open = original_open
    worker.close()
assert set(os.listdir(root)) == before
`
	command := exec.CommandContext(ctx, "/usr/bin/python3", "-I", "-B", "-c", code, t.TempDir())
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("worker safety proof: %v\n%s", err, output)
	}
}

func TestCopyRejectsUnsafeSharedParentsBeforeAnyWrite(t *testing.T) {
	c, ctx := copyFixture(t)
	root := t.TempDir()
	source := filepath.Join(root, "empty-directory")
	if err := os.Mkdir(source, 0o700); err != nil {
		t.Fatal(err)
	}
	shared := filepath.Join(root, "shared")
	if err := os.Mkdir(shared, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(shared, 0o777); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"result", "new-container/result"} {
		s := keySession()
		_, err := c.Prepare(ctx, s, machineaccess.CopyRequest{Source: machine.CopyPath{Path: source}, Destination: machine.CopyPath{Remote: true, Path: filepath.Join(shared, suffix)}}, false)
		if err == nil {
			t.Fatalf("unsafe shared destination %s admitted", suffix)
		}
		entries, err := os.ReadDir(shared)
		if err != nil || len(entries) != 0 {
			t.Fatalf("preflight wrote shared destination: %v %v", entries, err)
		}
	}
}

func TestCopyManifestRejectsTraversalAndMalformedParents(t *testing.T) {
	for _, reply := range []copyReply{
		{Path: "/x/..", Entries: []copyEntry{{Kind: "file"}}},
		{Path: "/source", Entries: []copyEntry{{Kind: "directory"}, {Path: "../escape", Kind: "file"}}},
		{Path: "/source", Entries: []copyEntry{{Kind: "directory"}, {Path: "missing/child", Kind: "file"}}},
	} {
		if err := validCopyManifest(reply); err == nil {
			t.Fatalf("unsafe manifest admitted: %+v", reply)
		}
	}
}
