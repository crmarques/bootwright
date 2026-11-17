//go:build linux && amd64

package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller/tooltable"
)

var pinnedToolsLock sync.Mutex

func e2eCacheDirectory(t *testing.T) string {
	t.Helper()
	output, err := exec.Command(filepath.Join("..", "..", "scripts", "cache-dir")).Output()
	if err != nil {
		t.Fatalf("scripts/cache-dir: %v", err)
	}
	return strings.TrimSpace(string(output))
}

// pinnedPython is the CPython the Ansible check pins in the shared cache,
// prepared by scripts/ansible-check the first time it is missing.
func pinnedPython(t *testing.T) string {
	t.Helper()
	pinnedToolsLock.Lock()
	defer pinnedToolsLock.Unlock()
	interpreter := filepath.Join(e2eCacheDirectory(t), "ansible-check", "venv", "bin", "python")
	if _, err := os.Stat(interpreter); err != nil {
		if output, err := exec.Command(filepath.Join("..", "..", "scripts", "ansible-check"), "--help").CombinedOutput(); err != nil {
			t.Fatalf("preparing the pinned python with scripts/ansible-check: %v\n%s", err, output)
		}
		if _, err := os.Stat(interpreter); err != nil {
			t.Fatalf("scripts/ansible-check left no pinned python: %v", err)
		}
	}
	return interpreter
}

// pinnedHelmDirectory holds the helm release the controller's tool table
// pins, fetched once into the shared cache and checked against the table's
// size and SHA-256 before it is written.
func pinnedHelmDirectory(t *testing.T) string {
	t.Helper()
	pinnedToolsLock.Lock()
	defer pinnedToolsLock.Unlock()
	pin, ok, err := tooltable.PinOf("helm")
	if err != nil || !ok {
		t.Fatalf("the tool table pins no helm: %v", err)
	}
	directory := filepath.Join(e2eCacheDirectory(t), "e2e-tools", "helm-"+pin.Default.Version+"-"+pin.Default.SHA256[:12])
	if _, err := os.Stat(filepath.Join(directory, "helm")); err == nil {
		return directory
	}
	client := http.Client{Timeout: 10 * time.Minute}
	response, err := client.Get("https://get.helm.sh/helm-v" + pin.Default.Version + "-linux-amd64.tar.gz")
	if err != nil {
		t.Fatalf("fetching the pinned helm %s: %v", pin.Default.Version, err)
	}
	defer response.Body.Close()
	archive, err := io.ReadAll(io.LimitReader(response.Body, pin.Default.Bytes+1))
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("fetching the pinned helm %s: status %d: %v", pin.Default.Version, response.StatusCode, err)
	}
	sum := sha256.Sum256(archive)
	if int64(len(archive)) != pin.Default.Bytes || hex.EncodeToString(sum[:]) != pin.Default.SHA256 {
		t.Fatalf("the helm %s archive does not match the tool table: %d bytes", pin.Default.Version, len(archive))
	}
	binary := extractHelm(t, archive)
	if err := os.MkdirAll(filepath.Dir(directory), 0o755); err != nil {
		t.Fatal(err)
	}
	staging, err := os.MkdirTemp(filepath.Dir(directory), ".helm-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "helm"), binary, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(staging, directory); err != nil {
		if _, statErr := os.Stat(filepath.Join(directory, "helm")); statErr != nil {
			t.Fatal(err)
		}
		os.RemoveAll(staging)
	}
	return directory
}

func extractHelm(t *testing.T, archive []byte) []byte {
	t.Helper()
	unzipped, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	reader := tar.NewReader(unzipped)
	for {
		header, err := reader.Next()
		if err != nil {
			t.Fatalf("the helm archive holds no linux-amd64/helm: %v", err)
		}
		if header.Name == "linux-amd64/helm" && header.Typeflag == tar.TypeReg {
			binary, err := io.ReadAll(io.LimitReader(reader, header.Size+1))
			if err != nil || int64(len(binary)) != header.Size {
				t.Fatalf("reading linux-amd64/helm: %v", err)
			}
			return binary
		}
	}
}

func pinnedHelm(t *testing.T) string {
	t.Helper()
	return filepath.Join(pinnedHelmDirectory(t), "helm")
}

func e2ePath(t *testing.T, directories ...string) string {
	t.Helper()
	return strings.Join(append(directories, pinnedHelmDirectory(t), os.Getenv("PATH")), string(os.PathListSeparator))
}
