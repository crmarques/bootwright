//go:build linux && amd64

package hostlinux

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/trust"
)

func ed25519Line(seed byte) (trust.HostKey, string) {
	public := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{seed}, ed25519.SeedSize)).Public().(ed25519.PublicKey)
	blob := binary.BigEndian.AppendUint32(nil, uint32(len("ssh-ed25519")))
	blob = append(blob, "ssh-ed25519"...)
	blob = binary.BigEndian.AppendUint32(blob, uint32(len(public)))
	blob = append(blob, public...)
	key := trust.HostKey{Type: "ssh-ed25519", PublicKey: base64.StdEncoding.EncodeToString(blob)}
	return key, key.Type + " " + key.PublicKey + " root@controller\n"
}

// The controller's own sshd host keys are read from the fixed public key
// files, read-only and through held trusted directories (B868). A file that is
// absent, unsafe or not one accepted public key gives no key and no refusal,
// so a controller without sshd keys reads none.
func TestSSHHostKeysReadsOnlyTheControllersSafePublicKeyFiles(t *testing.T) {
	i := fixture(t, "xfs")
	if keys, err := i.SSHHostKeys(context.Background()); err != nil || len(keys) != 0 {
		t.Fatalf("a controller without sshd keys read %+v (%v)", keys, err)
	}
	key, line := ed25519Line(1)
	writeFixture(t, i, "/etc/ssh/ssh_host_ed25519_key.pub", line, 0644)
	writeFixture(t, i, "/etc/ssh/ssh_host_ecdsa_key.pub", "not a public key\n", 0644)
	_, other := ed25519Line(2)
	writeFixture(t, i, "/etc/ssh/ssh_host_rsa_key.pub", other, 0666)
	must(t, os.Chmod(filepath.Join(i.view.root, "etc/ssh/ssh_host_rsa_key.pub"), 0666))
	before := snapshot(t, i.view.root)
	keys, err := i.SSHHostKeys(context.Background())
	if err != nil || len(keys) != 1 || keys[0] != key {
		t.Fatalf("read %+v (%v), want only %+v", keys, err, key)
	}
	if after := snapshot(t, i.view.root); len(after) != len(before) {
		t.Fatal("reading host keys changed the filesystem")
	}
	must(t, os.Remove(filepath.Join(i.view.root, "etc/ssh/ssh_host_ed25519_key.pub")))
	must(t, os.Symlink("../../tmp/elsewhere", filepath.Join(i.view.root, "etc/ssh/ssh_host_ed25519_key.pub")))
	if keys, err := i.SSHHostKeys(context.Background()); err != nil || len(keys) != 0 {
		t.Fatalf("a dangling link read %+v (%v)", keys, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := i.SSHHostKeys(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("a cancelled read returned %v", err)
	}
}
