//go:build linux && amd64

package medialocal

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos/media"
)

// servedFiles answers every request with one fixed outcome, whatever an honest
// opener would return for the path.
type servedFiles struct {
	file  *os.File
	err   error
	begin error
}

func (s servedFiles) Begin(context.Context) (FileSession, error) {
	if s.begin != nil {
		return nil, s.begin
	}
	return s, nil
}

func (s servedFiles) OpenFile(string) (*os.File, error) { return s.file, s.err }

func (servedFiles) Close() error { return nil }

type denial struct {
	errno syscall.Errno
	root  bool
}

func (d denial) Error() string { return d.errno.Error() }

func (d denial) Unwrap() error { return d.errno }

func (d denial) DeniedToRoot() bool { return d.root }

func expectFileRefusal(t *testing.T, err error, path, message, remediation string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "media.store" || reported[0].Source == nil || reported[0].Source.Path != path {
		t.Fatalf("%s: unexpected refusal %#v (%v)", path, reported, err)
	}
	if reported[0].Message != message || reported[0].Remediation != remediation {
		t.Errorf("%s: refused with %q / %q, want %q / %q", path, reported[0].Message, reported[0].Remediation, message, remediation)
	}
}

func TestFromFileRefusesAFIFOAFinalLinkADeviceAndAnUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	image := filepath.Join(dir, "image.iso")
	if err := os.WriteFile(image, []byte("installer"), 0600); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo.iso")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link.iso")
	if err := os.Symlink(image, link); err != nil {
		t.Fatal(err)
	}
	unreadable := filepath.Join(dir, "unreadable.iso")
	if err := os.WriteFile(unreadable, []byte("installer"), 0); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		path, message, remediation string
		root                       bool
	}{
		{fifo, "the media source is not a regular file (a FIFO)", "name a regular image file", true},
		{link, "the media source is a symbolic link", "name the image file itself rather than a link to it", true},
		{os.DevNull, "the media source is not a regular file (a character device)", "name a regular image file", true},
		{dir, "the media source is not a regular file (a directory)", "name a regular image file", true},
		{filepath.Join(dir, "absent.iso"), "the media source does not exist", "name an existing image file", true},
		{unreadable, "the invoking account cannot read the media source (permission denied)", "give the invoking account read access to it, or copy it to a directory that account can read", false},
	} {
		if !row.root && os.Geteuid() == 0 {
			continue
		}
		refused := make(chan error, 1)
		go func() {
			acquisition, err := New(nil, invokerFiles{}).Open(context.Background(), media.Source{Path: row.path})
			if err == nil {
				acquisition.Payload.Close()
			}
			refused <- err
		}()
		select {
		case err := <-refused:
			expectFileRefusal(t, err, row.path, row.message, row.remediation)
		case <-time.After(2 * time.Second):
			if writer, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
				writer.Close()
			}
			t.Fatalf("opening %s blocked", row.path)
		}
	}
}

func TestFromFileReprovesTheDescriptorItReceives(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	socket, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		open    func() (*os.File, error)
		message string
	}{
		{func() (*os.File, error) { return os.NewFile(uintptr(socket), "socket"), nil }, "the media source is not a regular file (a socket)"},
		{func() (*os.File, error) { return os.Open(dir) }, "the media source is not a regular file (a directory)"},
		{func() (*os.File, error) { return os.OpenFile(fifo, os.O_RDONLY|syscall.O_NONBLOCK, 0) }, "the media source is not a regular file (a FIFO)"},
	} {
		file, err := row.open()
		if err != nil {
			t.Fatal(err)
		}
		_, err = New(nil, servedFiles{file: file}).Open(context.Background(), media.Source{Path: "/images/image.iso"})
		expectFileRefusal(t, err, "/images/image.iso", row.message, "name a regular image file")
		if file.Fd() != ^uintptr(0) {
			t.Errorf("the refused %s descriptor was kept open", file.Name())
			file.Close()
		}
	}
}

func TestFromFilePermissionDenialNamesWhoCannotReadIt(t *testing.T) {
	const path = "/home/operator/image.iso"
	for _, row := range []struct {
		denial               denial
		message, remediation string
	}{
		{denial{syscall.EACCES, true}, "root cannot read the media source (a network home with root squash?)", "copy it to a local directory and name the copy"},
		{denial{syscall.EPERM, true}, "root cannot read the media source (a network home with root squash?)", "copy it to a local directory and name the copy"},
		{denial{syscall.EACCES, false}, "the invoking account cannot read the media source (permission denied)", "give the invoking account read access to it, or copy it to a directory that account can read"},
		{denial{syscall.EPERM, false}, "the invoking account cannot read the media source (permission denied)", "give the invoking account read access to it, or copy it to a directory that account can read"},
	} {
		_, err := New(nil, servedFiles{err: row.denial}).Open(context.Background(), media.Source{Path: path})
		expectFileRefusal(t, err, path, row.message, row.remediation)
	}
}

func TestFromFileNamesAnOpenerThatCannotStart(t *testing.T) {
	const path = "/images/image.iso"
	_, err := New(nil, servedFiles{begin: errors.New("invoking account cannot be verified")}).Open(context.Background(), media.Source{Path: path})
	expectFileRefusal(t, err, path, "the media source cannot be opened under the invoking account", "")
	_, err = New(nil, nil).Open(context.Background(), media.Source{Path: path})
	expectFileRefusal(t, err, path, "the media source opener is not configured", "")
	_, err = New(nil, servedFiles{err: syscall.EIO}).Open(context.Background(), media.Source{Path: path})
	expectFileRefusal(t, err, path, "the media source file cannot be opened", "name a readable image file")
}
