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

	"github.com/crmarques/bootwright/internal/diagnostics"
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
	files := &secureFiles{failureCode: "input", ownerUID: uint32(os.Getuid())}
	for _, part := range []secrets.Part{secrets.ValuePart, secrets.PasswordPart, secrets.PrivateKeyPart, secrets.CertificatePart, secrets.PublicKeyPart} {
		request := fileRequest{Part: part, Path: "secret"}
		base := syscall.Stat_t{Mode: syscall.S_IFREG | 0600, Uid: uint32(os.Getuid()), Nlink: 1, Size: 1}
		if err := files.unsafeFile(base, request, "/secret"); err != nil {
			t.Fatal("private regular file was rejected", part, err)
		}
		for _, change := range []func(*syscall.Stat_t){
			func(s *syscall.Stat_t) { s.Uid++ },
			func(s *syscall.Stat_t) { s.Nlink = 2 },
			func(s *syscall.Stat_t) { s.Size = -1 },
			func(s *syscall.Stat_t) { s.Mode = syscall.S_IFCHR | 0600 },
			func(s *syscall.Stat_t) { s.Mode = syscall.S_IFBLK | 0600 },
			func(s *syscall.Stat_t) { s.Mode = syscall.S_IFSOCK | 0600 },
			func(s *syscall.Stat_t) { s.Mode |= syscall.S_ISUID },
			func(s *syscall.Stat_t) { s.Mode |= 0020 },
		} {
			changed := base
			change(&changed)
			if files.unsafeFile(changed, request, "/secret") == nil {
				t.Fatal("unsafe file identity was accepted", part)
			}
		}
	}
}

// Only a certificate or public key file may be readable by others or root's;
// a value, password or private key file stays the invoking account's own and
// private to it (D72).
func TestOnlyCertificateAndPublicKeyFilesTakeTheRelaxedRule(t *testing.T) {
	const invoker = 4242
	files := &secureFiles{failureCode: "input", ownerUID: invoker}
	for _, test := range []struct {
		part    secrets.Part
		relaxed bool
	}{
		{secrets.ValuePart, false}, {secrets.PasswordPart, false}, {secrets.PrivateKeyPart, false},
		{secrets.CertificatePart, true}, {secrets.PublicKeyPart, true},
	} {
		request := fileRequest{Part: test.part, Path: "secret"}
		for _, private := range []syscall.Stat_t{
			{Mode: syscall.S_IFREG | 0600, Uid: invoker, Nlink: 1, Size: 1},
			{Mode: syscall.S_IFREG | 0400, Uid: invoker, Nlink: 1, Size: 1},
		} {
			if err := files.unsafeFile(private, request, "/secret"); err != nil {
				t.Fatalf("%s: a private file of mode %04o was refused: %+v", test.part, private.Mode&07777, diagnostics.Of(err))
			}
		}
		for _, shared := range []syscall.Stat_t{
			{Mode: syscall.S_IFREG | 0644, Uid: invoker, Nlink: 1, Size: 1},
			{Mode: syscall.S_IFREG | 0640, Uid: invoker, Nlink: 1, Size: 1},
			{Mode: syscall.S_IFREG | 0444, Uid: invoker, Nlink: 1, Size: 1},
			{Mode: syscall.S_IFREG | 0600, Uid: 0, Nlink: 1, Size: 1},
			{Mode: syscall.S_IFREG | 0644, Uid: 0, Nlink: 1, Size: 1},
		} {
			err := files.unsafeFile(shared, request, "/secret")
			if (err == nil) != test.relaxed {
				t.Fatalf("%s: a file of mode %04o owned by uid %d: admitted %v, want %v", test.part, shared.Mode&07777, shared.Uid, err == nil, test.relaxed)
			}
		}
	}
}
