//go:build linux && amd64

package material

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/secrets"
)

func TestHeldAcquisitionRejectsInPlaceAndAncestorChanges(t *testing.T) {
	for _, change := range []string{"same-size-write", "truncate", "permissions", "ancestor"} {
		t.Run(change, func(t *testing.T) {
			root := t.TempDir()
			directory := filepath.Join(root, "inputs")
			if err := os.Mkdir(directory, 0700); err != nil {
				t.Fatal(err)
			}
			path := writeSecretFile(t, directory, "value", []byte("initial"), 0600)
			reader, err := newSecureFiles(context.Background(), "source")
			if err != nil {
				t.Fatal(err)
			}
			defer reader.close()
			held, err := reader.openPart(fileRequest{Part: secrets.ValuePart, Path: path}, path)
			if err != nil {
				t.Fatal(err)
			}
			defer held.file.Close()
			data, err := readSecretFile(context.Background(), held.file)
			clear(data)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "same-size-write":
				err = os.WriteFile(path, []byte("changed"), 0600)
			case "truncate":
				err = os.Truncate(path, 0)
			case "permissions":
				err = os.Chmod(path, 0644)
			case "ancestor":
				err = os.Rename(directory, filepath.Join(root, "retained"))
				if err == nil {
					err = os.Mkdir(directory, 0700)
				}
				if err == nil {
					writeSecretFile(t, directory, "value", []byte("initial"), 0600)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			assertFailureCode(t, reader.verify([]heldPart{held}), "secret.source")
		})
	}
}

type cancelAfterChecks struct {
	context.Context
	remaining int
	cancel    context.CancelFunc
}

func (c *cancelAfterChecks) Err() error {
	c.remaining--
	if c.remaining == 0 {
		c.cancel()
	}
	return c.Context.Err()
}

func TestFileAcquisitionCancellationDiscardsPartialBytes(t *testing.T) {
	path := writeSecretFile(t, t.TempDir(), "value", bytes.Repeat([]byte{7}, 100000), 0600)
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controlled := &cancelAfterChecks{Context: ctx, remaining: 2, cancel: cancel}
	value, err := readSecretFile(controlled, file)
	defer clear(value)
	if !errors.Is(err, context.Canceled) || len(value) != 0 {
		t.Fatal("canceled acquisition retained partial material")
	}
	offset, err := file.Seek(0, 1)
	if err != nil || offset != 32768 {
		t.Fatal("cancellation did not interrupt the bounded read loop", offset, err)
	}
}

func TestSecretFileTrustPredicateRejectsOwnerAndSpecialTypes(t *testing.T) {
	base := syscall.Stat_t{Mode: syscall.S_IFREG | 0600, Uid: uint32(os.Getuid()), Nlink: 1, Size: 1}
	if !safeOperatorFile(base) {
		t.Fatal("private regular file was rejected")
	}
	for _, change := range []func(*syscall.Stat_t){
		func(s *syscall.Stat_t) { s.Uid++ },
		func(s *syscall.Stat_t) { s.Nlink = 2 },
		func(s *syscall.Stat_t) { s.Size = -1 },
		func(s *syscall.Stat_t) { s.Mode = syscall.S_IFCHR | 0600 },
		func(s *syscall.Stat_t) { s.Mode = syscall.S_IFBLK | 0600 },
		func(s *syscall.Stat_t) { s.Mode = syscall.S_IFSOCK | 0600 },
		func(s *syscall.Stat_t) { s.Mode |= syscall.S_ISUID },
	} {
		changed := base
		change(&changed)
		if safeOperatorFile(changed) {
			t.Fatal("unsafe file identity was accepted")
		}
	}
}
