//go:build linux && amd64

package material

import (
	"context"
	cryptorand "crypto/rand"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
)

// Every input-file refusal names its condition and the remedy for it. A file
// carrying a value, password, token or private key is private to the invoking
// account; a certificate or public key file may be readable by others and
// root's, but writable by no one else (D72).
func TestEachInputFileRefusalNamesItsConditionAndRemedy(t *testing.T) {
	root := t.TempDir()
	service := New(nil, Options{Random: cryptorand.Reader, Clock: func() time.Time { return generationTime }})
	authority, err := service.Generate(context.Background(), secrets.Declaration{Type: "caBundle", Source: "generated", Generation: secrets.Generation{CommonName: "fixture-ca", ValidityDays: 30}})
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Clear()
	pair, err := New(nil).Generate(context.Background(), secrets.Declaration{Type: "sshKeyPair", Source: "generated", Generation: secrets.Generation{KeyType: "ed25519"}})
	if err != nil {
		t.Fatal(err)
	}
	defer pair.Clear()
	certificate := requiredPart(t, authority, secrets.CertificatePart)
	privateKey := requiredPart(t, pair, secrets.PrivateKeyPart)
	publicKey := requiredPart(t, pair, secrets.PublicKeyPart)
	defer clear(privateKey)

	file := func(name string, data []byte, mode os.FileMode) string {
		return writeSecretFile(t, root, name, data, mode)
	}
	value := file("value", []byte("synthetic-value"), 0600)
	link := filepath.Join(root, "link")
	if err := os.Symlink(value, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "real"), 0700); err != nil {
		t.Fatal(err)
	}
	writeSecretFile(t, filepath.Join(root, "real"), "nested", []byte("synthetic-value"), 0600)
	if err := os.Symlink(filepath.Join(root, "real"), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(root, "directory")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(root, "fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	linked := file("linked-value", []byte("synthetic-value"), 0600)
	if err := os.Link(linked, filepath.Join(root, "second-link")); err != nil {
		t.Fatal(err)
	}
	setuid := file("setuid", []byte("synthetic-value"), 0600|os.ModeSetuid)
	readable := file("readable", []byte("synthetic-value"), 0644)
	readablePassword := file("readable-password", []byte("synthetic-password"), 0644)
	password := file("password", []byte("synthetic-password"), 0600)
	publicCertificate := file("certificate", certificate, 0644)
	groupWritable := file("group-writable", certificate, 0664)
	otherWritable := file("other-writable", certificate, 0646)
	privateFile := file("private-key", privateKey, 0600)
	publicPrivate := file("public-private-key", privateKey, 0644)
	publicKeyFile := file("public-key", publicKey, 0644)

	const (
		strict = "secret file mode is 0644; a file carrying a value, password, token or private key must be 0600 or 0400"
		copied = "copy it to a file the invoking account owns, e.g. install -m "
	)
	opaque := secrets.Declaration{Type: "opaque", Source: "contextStore"}
	authorityDeclaration := secrets.Declaration{Type: "caBundle", Source: "contextStore"}
	pairDeclaration := secrets.Declaration{Type: "sshKeyPair", Source: "contextStore"}
	credentials := secrets.Declaration{Type: "usernamePassword", Source: "contextStore"}
	token := secrets.Declaration{Type: "token", Source: "contextStore"}
	for _, test := range []struct {
		name                  string
		declaration           secrets.Declaration
		input                 secrets.Input
		other                 bool
		path, message, remedy string
	}{
		{"missing", opaque, secrets.Input{ValueFile: filepath.Join(root, "missing")}, false, filepath.Join(root, "missing"), "secret file does not exist", "check the path"},
		{"final symbolic link", opaque, secrets.Input{ValueFile: link}, false, link, "secret file is a symbolic link", "name its target (readlink -f " + link + ")"},
		{"symbolic link above", opaque, secrets.Input{ValueFile: filepath.Join(root, "linked", "nested")}, false, filepath.Join(root, "linked", "nested"),
			"a directory above the secret file is a symbolic link or not a directory", "name the path without symbolic links (realpath " + filepath.Join(root, "linked", "nested") + ")"},
		{"directory", opaque, secrets.Input{ValueFile: directory}, false, directory, "secret file is not a regular file", "name a regular file"},
		{"fifo", opaque, secrets.Input{ValueFile: fifo}, false, fifo, "secret file is not a regular file", "name a regular file"},
		{"hard link", opaque, secrets.Input{ValueFile: linked}, false, linked, "secret file has more than one hard link", "copy it to a new file you own and name the copy"},
		{"setuid", opaque, secrets.Input{ValueFile: setuid}, false, setuid, "secret file has its setuid, setgid or sticky bit set", "chmod u-s,g-s,o-t " + setuid},
		{"readable value", opaque, secrets.Input{ValueFile: readable}, false, readable, strict, "chmod 600 " + readable},
		{"another account's value", opaque, secrets.Input{ValueFile: value}, true, value, "secret file is owned by another account", copied + "600 " + value + " <copy>"},
		{"readable token", token, secrets.Input{ValueFile: readable}, false, readable, strict, "chmod 600 " + readable},
		{"private password", credentials, secrets.Input{Username: "operator", PasswordFile: password}, false, "", "", ""},
		{"readable password", credentials, secrets.Input{Username: "operator", PasswordFile: readablePassword}, false, readablePassword, strict, "chmod 600 " + readablePassword},
		{"another account's password", credentials, secrets.Input{Username: "operator", PasswordFile: password}, true, password,
			"secret file is owned by another account", copied + "600 " + password + " <copy>"},
		{"readable certificate", authorityDeclaration, secrets.Input{CertificateFile: publicCertificate}, false, "", "", ""},
		{"readable public key", pairDeclaration, secrets.Input{PrivateKeyFile: privateFile, PublicKeyFile: publicKeyFile}, false, "", "", ""},
		{"group-writable certificate", authorityDeclaration, secrets.Input{CertificateFile: groupWritable}, false, groupWritable, "secret file is writable by its group or others", "chmod go-w " + groupWritable},
		{"other-writable certificate", authorityDeclaration, secrets.Input{CertificateFile: otherWritable}, false, otherWritable, "secret file is writable by its group or others", "chmod go-w " + otherWritable},
		{"another account's certificate", authorityDeclaration, secrets.Input{CertificateFile: publicCertificate}, true, publicCertificate,
			"secret file is owned by neither the invoking account nor root", copied + "644 " + publicCertificate + " <copy>"},
		{"readable private key", pairDeclaration, secrets.Input{PrivateKeyFile: publicPrivate, PublicKeyFile: publicKeyFile}, false, publicPrivate, strict, "chmod 600 " + publicPrivate},
	} {
		t.Run(test.name, func(t *testing.T) {
			uid := os.Getuid()
			if test.other {
				if uid == 0 {
					t.Skip("a file root writes is root's, which the certificate rule admits")
				}
				uid++
			}
			acquirer := New(nil, Options{Clock: func() time.Time { return generationTime }, Operator: fixedOperator{FileIdentity{UID: uid, Home: root}}})
			acquired, err := acquirer.Acquire(context.Background(), test.declaration, test.input)
			acquired.Clear()
			if test.message == "" {
				if err != nil {
					t.Fatalf("a file the rule admits was refused: %+v", diagnostics.Of(err))
				}
				return
			}
			found := diagnostics.Of(err)
			if len(found) != 1 || found[0].Code != "secret.input" || found[0].Source == nil || found[0].Source.Path != test.path ||
				found[0].Message != test.message || found[0].Remediation != test.remedy {
				t.Fatalf("diagnostics = %+v, want %q (%q) at %s", found, test.message, test.remedy, test.path)
			}
		})
	}
}

// A certificate root owns is admitted when it is readable and writable by no
// one else; a value root owns is not the invoking account's own.
func TestARootOwnedCertificateFileIsAdmitted(t *testing.T) {
	files := &secureFiles{failureCode: "input", ownerUID: uint32(os.Getuid() + 1)}
	stat := syscall.Stat_t{Mode: syscall.S_IFREG | 0644, Uid: 0, Nlink: 1, Size: 1}
	if err := files.unsafeFile(stat, fileRequest{Part: secrets.CertificatePart, Path: "ca.pem"}, "/ca.pem"); err != nil {
		t.Fatal("a root-owned certificate was refused", diagnostics.Of(err))
	}
	if err := files.unsafeFile(stat, fileRequest{Part: secrets.ValuePart, Path: "value"}, "/value"); err == nil {
		t.Fatal("a root-owned value file was admitted")
	}
	for _, candidate := range []string{"/etc/passwd", "/etc/group", "/etc/hosts"} {
		info, err := os.Lstat(candidate)
		if err != nil {
			continue
		}
		observed, ok := info.Sys().(*syscall.Stat_t)
		if !ok || !info.Mode().IsRegular() || observed.Uid != 0 || observed.Nlink != 1 || observed.Mode&0022 != 0 || info.Size() > secrets.MaxPartBytes {
			continue
		}
		virtual := newVirtualSecretFiles(t)
		path := virtual.write(t, "trust/ca.pem", []byte("placeholder"), 0600)
		virtual.replace = func(requested string, flags int) (*os.File, bool, error) {
			if requested != path {
				return nil, false, nil
			}
			fd, err := syscall.Open(candidate, flags|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
			if err != nil {
				return nil, true, err
			}
			return os.NewFile(uintptr(fd), requested), true, nil
		}
		reader := New(nil, Options{Files: virtual, Operator: fixedOperator{FileIdentity{UID: os.Getuid(), Home: virtual.prefix}}})
		parts, err := reader.readFileParts(context.Background(), []fileRequest{{Part: secrets.CertificatePart, Path: path}})
		clearParts(parts)
		if err != nil {
			t.Fatalf("the root-owned file %s was refused as a certificate: %+v", candidate, diagnostics.Of(err))
		}
		return
	}
	t.Log("no root-owned single-link file on this host to open; the stat above stands for it")
}
