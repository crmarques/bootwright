//go:build linux && amd64

package material

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/secrets"
)

type fixedOperator struct{ identity FileIdentity }

func (f fixedOperator) FileIdentity(context.Context) (FileIdentity, error) { return f.identity, nil }

func TestFileMaterialUsesInvokingAccountIdentity(t *testing.T) {
	home := t.TempDir()
	writeSecretFile(t, home, "token.secret", []byte("operator-value\n"), 0600)
	t.Setenv("HOME", "/untrusted-home")
	service := New(nil, Options{Operator: fixedOperator{FileIdentity{UID: os.Getuid(), Home: home}}})
	declaration := secrets.Declaration{Name: "token", Type: "token", Source: "file", Origin: "/input/secret.yaml", Files: secrets.FileSource{Path: "~/token.secret"}}
	material, err := service.File(context.Background(), declaration)
	if err != nil {
		t.Fatal("invoking-user home was not used", err)
	}
	material.Clear()
	service = New(nil, Options{Operator: fixedOperator{FileIdentity{UID: os.Getuid() + 1, Home: home}}})
	if _, err := service.File(context.Background(), declaration); err == nil {
		t.Fatal("file belonging to a different account was accepted")
	}
}

func TestAcquireResolvesRelativePathsFromInvocationWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	path := writeSecretFile(t, root, "token.secret", []byte("from-cwd\n"), 0600)
	prior, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(prior); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	service := New(nil)
	value, err := service.Acquire(context.Background(), secrets.Declaration{Type: "token", Source: "contextStore"}, secrets.Input{ValueFile: filepath.Base(path)})
	if err != nil {
		t.Fatalf("Acquire: %v (%s)", err, diagnosticMessage(err))
	}
	defer value.Clear()
	got := requiredPart(t, value, secrets.ValuePart)
	defer clear(got)
	if string(got) != "from-cwd" {
		t.Fatal("acquired token differs from the file bytes")
	}
}

func TestFileResolvesRelativePathsFromDeclarationOriginAndAcceptsPublicDirectories(t *testing.T) {
	root := t.TempDir()
	directory := filepath.Join(root, "public-directory")
	if err := os.Mkdir(directory, 0755); err != nil {
		t.Fatal(err)
	}
	path := writeSecretFile(t, directory, "credentials.json", []byte(`{"username":"operator","password":"value\n"}`), 0400)
	declaration := secrets.Declaration{
		Type: "usernamePassword", Source: "file", Origin: filepath.Join(root, "declaration.yaml"),
		Files: secrets.FileSource{Path: filepath.Join(filepath.Base(directory), filepath.Base(path))},
	}
	value, err := New(nil).File(context.Background(), declaration)
	if err != nil {
		t.Fatalf("File: %v (%s)", err, diagnosticMessage(err))
	}
	defer value.Clear()
	username := requiredPart(t, value, secrets.UsernamePart)
	password := requiredPart(t, value, secrets.PasswordPart)
	defer clear(username)
	defer clear(password)
	if string(username) != "operator" || string(password) != "value" {
		t.Fatal("structured usernamePassword parts differ from the file fields")
	}
}

func TestAcquireFileLineTransportBounds(t *testing.T) {
	tests := []struct {
		name        string
		declaration secrets.Declaration
		input       func(string) secrets.Input
		payload     func() []byte
		part        secrets.Part
		valid       bool
	}{
		{
			name: "token exact normalized limit", declaration: secrets.Declaration{Type: "token", Source: "contextStore"},
			input: func(path string) secrets.Input { return secrets.Input{ValueFile: path} },
			payload: func() []byte {
				return append(bytes.Repeat([]byte{'t'}, secrets.MaxPartBytes), '\n')
			}, part: secrets.ValuePart, valid: true,
		},
		{
			name: "password exact normalized limit", declaration: secrets.Declaration{Type: "usernamePassword", Source: "contextStore"},
			input: func(path string) secrets.Input {
				return secrets.Input{Username: "operator", PasswordFile: path}
			},
			payload: func() []byte {
				return append(bytes.Repeat([]byte{'p'}, secrets.MaxPartBytes), '\n')
			}, part: secrets.PasswordPart, valid: true,
		},
		{
			name: "token non-LF overflow", declaration: secrets.Declaration{Type: "token", Source: "contextStore"},
			input: func(path string) secrets.Input { return secrets.Input{ValueFile: path} },
			payload: func() []byte {
				return bytes.Repeat([]byte{'t'}, secrets.MaxPartBytes+1)
			},
		},
		{
			name: "password non-LF overflow", declaration: secrets.Declaration{Type: "usernamePassword", Source: "contextStore"},
			input: func(path string) secrets.Input {
				return secrets.Input{Username: "operator", PasswordFile: path}
			},
			payload: func() []byte {
				return bytes.Repeat([]byte{'p'}, secrets.MaxPartBytes+1)
			},
		},
		{
			name: "token two-byte overflow", declaration: secrets.Declaration{Type: "token", Source: "contextStore"},
			input: func(path string) secrets.Input { return secrets.Input{ValueFile: path} },
			payload: func() []byte {
				return append(bytes.Repeat([]byte{'t'}, secrets.MaxPartBytes), '\n', 'x')
			},
		},
		{
			name: "password two-byte overflow", declaration: secrets.Declaration{Type: "usernamePassword", Source: "contextStore"},
			input: func(path string) secrets.Input {
				return secrets.Input{Username: "operator", PasswordFile: path}
			},
			payload: func() []byte {
				return append(bytes.Repeat([]byte{'p'}, secrets.MaxPartBytes), '\n', 'x')
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := test.payload()
			defer clear(payload)
			path := writeSecretFile(t, t.TempDir(), "line.secret", payload, 0600)
			value, err := New(nil).Acquire(context.Background(), test.declaration, test.input(path))
			defer value.Clear()
			if !test.valid {
				assertFailureCode(t, err, "secret.store.limit")
				return
			}
			if err != nil {
				t.Fatalf("Acquire: %v (%s)", err, diagnosticMessage(err))
			}
			got := requiredPart(t, value, test.part)
			defer clear(got)
			if len(got) != secrets.MaxPartBytes || got[len(got)-1] == '\n' {
				t.Fatal("line transport was not normalized to the exact decoded part limit")
			}
		})
	}
}

func TestFileUsernamePasswordAllowsExactWorstCaseTransportBound(t *testing.T) {
	document := make([]byte, 0, maxUsernamePasswordJSONBytes)
	document = append(document, `{"`...)
	document = append(document, `\u0075\u0073\u0065\u0072\u006e\u0061\u006d\u0065`...)
	document = append(document, `":"`...)
	for range secrets.MaxPartBytes {
		document = append(document, `\u0075`...)
	}
	document = append(document, `","`...)
	document = append(document, `\u0070\u0061\u0073\u0073\u0077\u006f\u0072\u0064`...)
	document = append(document, `":"`...)
	for range secrets.MaxPartBytes {
		document = append(document, `\u0070`...)
	}
	document = append(document, `\u000a"}`...)
	document = append(document, '\n')
	if len(document) != maxUsernamePasswordJSONBytes {
		t.Fatalf("maximal structured document size = %d, want %d", len(document), maxUsernamePasswordJSONBytes)
	}
	defer clear(document)
	root := t.TempDir()
	path := writeSecretFile(t, root, "credentials.json", document, 0600)
	declaration := secrets.Declaration{
		Type: "usernamePassword", Source: "file", Origin: filepath.Join(root, "declaration.yaml"),
		Files: secrets.FileSource{Path: path},
	}
	value, err := New(nil).File(context.Background(), declaration)
	if err != nil {
		t.Fatalf("File: %v (%s)", err, diagnosticMessage(err))
	}
	defer value.Clear()
	username := requiredPart(t, value, secrets.UsernamePart)
	password := requiredPart(t, value, secrets.PasswordPart)
	defer clear(username)
	defer clear(password)
	if len(username) != secrets.MaxPartBytes || len(password) != secrets.MaxPartBytes {
		t.Fatal("structured fields were not decoded and normalized at the exact version limit")
	}
}

func TestFileUsernamePasswordRejectsOversizedDecodedParts(t *testing.T) {
	tests := []struct {
		name     string
		username []byte
		password []byte
	}{
		{name: "username", username: bytes.Repeat([]byte{'u'}, secrets.MaxPartBytes+1), password: []byte("password")},
		{name: "password", username: []byte("operator"), password: bytes.Repeat([]byte{'p'}, secrets.MaxPartBytes+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer clear(test.username)
			defer clear(test.password)
			document := make([]byte, 0, len(test.username)+len(test.password)+32)
			document = append(document, `{"username":"`...)
			document = append(document, test.username...)
			document = append(document, `","password":"`...)
			document = append(document, test.password...)
			document = append(document, `"}`...)
			defer clear(document)
			root := t.TempDir()
			path := writeSecretFile(t, root, "credentials.json", document, 0600)
			value, err := New(nil).File(context.Background(), secrets.Declaration{
				Type: "usernamePassword", Source: "file", Origin: filepath.Join(root, "declaration.yaml"),
				Files: secrets.FileSource{Path: path},
			})
			value.Clear()
			assertFailureCode(t, err, "secret.store.limit")
		})
	}
}

func TestSSHPrivateOnlyDerivesPublicForAcquireAndFile(t *testing.T) {
	service := New(nil, Options{Random: cryptorand.Reader, Clock: func() time.Time { return generationTime }})
	generated, err := service.Generate(context.Background(), secrets.Declaration{Type: "sshKeyPair", Source: "generated"})
	if err != nil {
		t.Fatal(err)
	}
	defer generated.Clear()
	privateKey := requiredPart(t, generated, secrets.PrivateKeyPart)
	defer clear(privateKey)
	root := t.TempDir()
	path := writeSecretFile(t, root, "id", privateKey, 0600)

	acquired, err := service.Acquire(context.Background(), secrets.Declaration{Type: "sshKeyPair", Source: "contextStore"}, secrets.Input{PrivateKeyFile: path})
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer acquired.Clear()
	if err := service.Validate(context.Background(), secrets.Declaration{Type: "sshKeyPair", Source: "contextStore"}, acquired); err != nil {
		t.Fatalf("acquired pair: %v", err)
	}

	fromFile, err := service.File(context.Background(), secrets.Declaration{
		Type: "sshKeyPair", Source: "file", Origin: filepath.Join(root, "desired.yaml"), Files: secrets.FileSource{PrivateKey: filepath.Base(path)},
	})
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	defer fromFile.Clear()
	want := requiredPart(t, generated, secrets.PublicKeyPart)
	acquiredPublic := requiredPart(t, acquired, secrets.PublicKeyPart)
	filePublic := requiredPart(t, fromFile, secrets.PublicKeyPart)
	defer clear(want)
	defer clear(acquiredPublic)
	defer clear(filePublic)
	if !bytes.Equal(want, acquiredPublic) || !bytes.Equal(want, filePublic) {
		t.Fatal("derived SSH public keys differ")
	}
}

func TestAcquireReadsCompleteTLSVersion(t *testing.T) {
	service := New(nil, Options{Random: cryptorand.Reader, Clock: func() time.Time { return generationTime }})
	declaration := secrets.Declaration{Type: "tlsCertificate", Source: "generated", Generation: secrets.Generation{CommonName: "tls.example", ValidityDays: 10}}
	generated, err := service.Generate(context.Background(), declaration)
	if err != nil {
		t.Fatal(err)
	}
	defer generated.Clear()
	certificate := requiredPart(t, generated, secrets.CertificatePart)
	privateKey := requiredPart(t, generated, secrets.PrivateKeyPart)
	defer clear(certificate)
	defer clear(privateKey)
	root := t.TempDir()
	certificatePath := writeSecretFile(t, root, "tls.crt", certificate, 0600)
	privatePath := writeSecretFile(t, root, "tls.key", privateKey, 0400)
	acquired, err := service.Acquire(context.Background(), secrets.Declaration{Type: "tlsCertificate", Source: "contextStore"}, secrets.Input{
		CertificateFile: certificatePath, PrivateKeyFile: privatePath,
	})
	if err != nil {
		t.Fatalf("Acquire TLS: %v (%s)", err, diagnosticMessage(err))
	}
	defer acquired.Clear()
	acquiredCertificate := requiredPart(t, acquired, secrets.CertificatePart)
	acquiredPrivateKey := requiredPart(t, acquired, secrets.PrivateKeyPart)
	defer clear(acquiredCertificate)
	defer clear(acquiredPrivateKey)
	if !bytes.Equal(acquiredCertificate, certificate) || !bytes.Equal(acquiredPrivateKey, privateKey) {
		t.Fatal("acquired TLS version differs")
	}
}

func TestFileReaderRejectsUnsafeTypesLinksModesAndAncestors(t *testing.T) {
	root := t.TempDir()
	safe := writeSecretFile(t, root, "safe", []byte("value"), 0600)
	public := writeSecretFile(t, root, "public", []byte("value"), 0644)
	hardlink := filepath.Join(root, "hardlink")
	if err := os.Link(safe, hardlink); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(root, "symlink")
	if err := os.Symlink(public, symlink); err != nil {
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
	realAncestor := filepath.Join(root, "real")
	if err := os.Mkdir(realAncestor, 0700); err != nil {
		t.Fatal(err)
	}
	writeSecretFile(t, realAncestor, "nested", []byte("value"), 0600)
	symlinkAncestor := filepath.Join(root, "linked-directory")
	if err := os.Symlink(realAncestor, symlinkAncestor); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"world-readable":   public,
		"hardlink":         hardlink,
		"symlink":          symlink,
		"directory":        directory,
		"fifo":             fifo,
		"symlink ancestor": filepath.Join(symlinkAncestor, "nested"),
	} {
		t.Run(name, func(t *testing.T) {
			value, err := New(nil).Acquire(context.Background(), secrets.Declaration{Type: "opaque", Source: "contextStore"}, secrets.Input{ValueFile: path})
			value.Clear()
			assertFailureCode(t, err, "secret.input")
		})
	}
}

func TestFileReaderRejectsOversizeBeforeReading(t *testing.T) {
	tests := []struct {
		name string
		size int64
		read func(string) (secrets.Material, error)
	}{
		{
			name: "exact opaque", size: secrets.MaxPartBytes + 1,
			read: func(path string) (secrets.Material, error) {
				return New(nil).Acquire(context.Background(), secrets.Declaration{Type: "opaque", Source: "contextStore"}, secrets.Input{ValueFile: path})
			},
		},
		{
			name: "optional-LF token", size: secrets.MaxPartBytes + 2,
			read: func(path string) (secrets.Material, error) {
				return New(nil).Acquire(context.Background(), secrets.Declaration{Type: "token", Source: "contextStore"}, secrets.Input{ValueFile: path})
			},
		},
		{
			name: "structured usernamePassword", size: int64(maxUsernamePasswordJSONBytes) + 1,
			read: func(path string) (secrets.Material, error) {
				root := filepath.Dir(path)
				return New(nil).File(context.Background(), secrets.Declaration{
					Type: "usernamePassword", Source: "file", Origin: filepath.Join(root, "declaration.yaml"), Files: secrets.FileSource{Path: path},
				})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "oversize")
			file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
			if err != nil {
				t.Fatal(err)
			}
			if err := file.Truncate(test.size); err != nil {
				file.Close()
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			value, err := test.read(path)
			value.Clear()
			assertFailureCode(t, err, "secret.store.limit")
		})
	}
}

func TestFileTransportValidationPrecedesPathAcquisition(t *testing.T) {
	parts, err := New(nil).readFileParts(context.Background(), []fileRequest{
		{Part: secrets.PrivateKeyPart, Path: "missing-relative-file"},
		{Part: secrets.PublicKeyPart, Path: "missing-public-file", Encoding: transportEncoding(255)},
	}, "", false)
	clearParts(parts)
	assertFailureCode(t, err, "secret.input")
	if !strings.Contains(diagnosticMessage(err), "transport encoding") {
		t.Fatal("file acquisition preceded complete transport validation")
	}
}

func TestHeldMultipartReaderDetectsReplacementBeforePublication(t *testing.T) {
	root := t.TempDir()
	firstPath := writeSecretFile(t, root, "first", []byte("first"), 0600)
	secondPath := writeSecretFile(t, root, "second", []byte("second"), 0600)
	replacement := writeSecretFile(t, root, "replacement", []byte("changed"), 0600)
	reader, err := newSecureFiles(context.Background(), "input")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.close()
	first, err := reader.openPart(fileRequest{Part: secrets.CertificatePart, Path: firstPath}, firstPath)
	if err != nil {
		t.Fatal(err)
	}
	defer first.file.Close()
	second, err := reader.openPart(fileRequest{Part: secrets.PrivateKeyPart, Path: secondPath}, secondPath)
	if err != nil {
		t.Fatal(err)
	}
	defer second.file.Close()
	if _, err := readSecretFile(context.Background(), first.file); err != nil {
		t.Fatal(err)
	}
	if _, err := readSecretFile(context.Background(), second.file); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(replacement, secondPath); err != nil {
		t.Fatal(err)
	}
	assertFailureCode(t, reader.verify([]heldPart{first, second}), "secret.input")
}

func TestTildeResolutionUsesAccountDatabaseNotHomeEnvironment(t *testing.T) {
	fake := t.TempDir()
	t.Setenv("HOME", fake)
	account, err := accountHome(context.Background(), "input")
	if err != nil {
		t.Skipf("account database unavailable: %v", err)
	}
	resolved, err := resolveSecretPath(context.Background(), "/", "~/secret", "input")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != filepath.Join(account, "secret") || strings.HasPrefix(resolved, fake+string(filepath.Separator)) {
		t.Fatal("tilde resolution did not use the current account database")
	}
	if _, err := resolveSecretPath(context.Background(), "/", "~another/secret", "input"); err == nil {
		t.Fatal("named-account tilde unexpectedly accepted")
	}
}

func TestFileReaderClosesHeldHandlesOnSuccessAndFailure(t *testing.T) {
	root := t.TempDir()
	valid := writeSecretFile(t, root, "valid", []byte("value"), 0600)
	invalid := writeSecretFile(t, root, "invalid", []byte("value"), 0644)
	before := openFileDescriptors(t)
	for range 50 {
		value, err := New(nil).Acquire(context.Background(), secrets.Declaration{Type: "opaque", Source: "contextStore"}, secrets.Input{ValueFile: valid})
		if err != nil {
			t.Fatal(err)
		}
		value.Clear()
		value, err = New(nil).Acquire(context.Background(), secrets.Declaration{Type: "opaque", Source: "contextStore"}, secrets.Input{ValueFile: invalid})
		value.Clear()
		if err == nil {
			t.Fatal("unsafe file unexpectedly accepted")
		}
	}
	after := openFileDescriptors(t)
	if after > before+2 {
		t.Fatalf("file descriptors grew from %d to %d", before, after)
	}
}

func openFileDescriptors(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Skipf("descriptor accounting unavailable: %v", err)
	}
	return len(entries)
}

func writeSecretFile(t *testing.T, directory, name string, data []byte, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}
