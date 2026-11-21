package verifiedfetch

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pinOf(data string) Pin {
	sum := sha256.Sum256([]byte(data))
	return Pin{Name: "artifact", Bytes: int64(len(data)), SHA256: hex.EncodeToString(sum[:])}
}

func serving(t *testing.T, body string, status int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestAFileTakesItsNameOnlyOnceItMatchesItsPin(t *testing.T) {
	directory := t.TempDir()
	pin := pinOf("pinned bytes")
	for name, served := range map[string]string{"other bytes": "other bytes!", "longer": "pinned bytes and more", "shorter": "pinned"} {
		target := filepath.Join(directory, strings.ReplaceAll(name, " ", "-"))
		server := serving(t, served, http.StatusOK)
		if err := Write(target, pin, 0o644, Download(server.Client(), server.URL+"/f", pin)); err == nil {
			t.Errorf("%s: a download that is not the pinned file was kept", name)
		}
		if _, err := os.Lstat(target); err == nil {
			t.Errorf("%s: the refused download took its name", name)
		}
	}
	entries, _ := os.ReadDir(directory)
	if len(entries) != 0 {
		t.Errorf("refused downloads left %d files behind", len(entries))
	}
	server := serving(t, "pinned bytes", http.StatusOK)
	target := filepath.Join(directory, "kept")
	if err := Write(target, pin, 0o640, Download(server.Client(), server.URL+"/f", pin)); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(target); err != nil || info.Mode().Perm() != 0o640 {
		t.Errorf("kept file = %v, %v; want mode 0640", info, err)
	}
	if err := Verified(target, pin); err != nil {
		t.Errorf("a kept file is not verified: %v", err)
	}
}

func TestAnOnDiskFileIsOpenedOnlyWhenItIsARegularFileOfItsPin(t *testing.T) {
	directory := t.TempDir()
	pin := pinOf("pinned bytes")
	regular := filepath.Join(directory, "regular")
	os.WriteFile(regular, []byte("pinned bytes"), 0o600)
	link := filepath.Join(directory, "link")
	os.Symlink(regular, link)
	huge := filepath.Join(directory, "huge")
	os.WriteFile(huge, []byte("pinned bytes with a long tail"), 0o600)
	wrong := filepath.Join(directory, "wrong")
	os.WriteFile(wrong, []byte("pinned BYTES"), 0o600)
	handle, err := Open(regular, pin)
	if err != nil {
		t.Fatal(err)
	}
	defer handle.Close()
	if data, _ := io.ReadAll(handle); string(data) != "pinned bytes" {
		t.Errorf("the opened file is not rewound: %q", data)
	}
	for name, file := range map[string]string{"link": link, "oversized": huge, "other digest": wrong, "absent": filepath.Join(directory, "none")} {
		if other, err := Open(file, pin); err == nil {
			other.Close()
			t.Errorf("%s was opened as the pinned file", name)
		}
	}
}

func TestACachedFileIsFetchedOnceAndRefetchedWhenItIsReplaced(t *testing.T) {
	var requests int
	pin := pinOf("pinned bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		io.WriteString(w, "pinned bytes")
	}))
	defer server.Close()
	directory := filepath.Join(t.TempDir(), pin.SHA256)
	for range 2 {
		handle, err := Cached(server.Client(), server.URL+"/tool.tar.gz", directory, pin)
		if err != nil {
			t.Fatal(err)
		}
		handle.Close()
	}
	if requests != 1 {
		t.Fatalf("%d requests for one cached file", requests)
	}
	os.WriteFile(filepath.Join(directory, "tool.tar.gz"), []byte("tampered!!!!"), 0o600)
	handle, err := Cached(server.Client(), server.URL+"/tool.tar.gz", directory, pin)
	if err != nil || requests != 2 {
		t.Fatalf("a replaced cache entry was not fetched again: %v, %d requests", err, requests)
	}
	handle.Close()
	refused := serving(t, "gone", http.StatusNotFound)
	if _, err := Cached(refused.Client(), refused.URL+"/other.tar.gz", directory, pin); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("a refused fetch = %v, want the server's status", err)
	}
}

type redirectTransport struct{}

func (redirectTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://cdn.example.test/file?signature=synthetic-private-token"}}, Request: request}, nil
}

func TestAFailedRedirectKeepsItsSignedQueryPrivate(t *testing.T) {
	const token = "synthetic-private-token"
	client := &http.Client{Transport: redirectTransport{}}
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return errors.New("redirect failure with " + token)
	}
	_, err := Cached(client, "https://publisher.example.test/asset", t.TempDir(), pinOf("artifact"))
	if err == nil || !strings.Contains(err.Error(), "cdn.example.test") || strings.Contains(err.Error(), token) {
		t.Fatalf("the fetch exposed its private redirect or omitted its destination: %v", err)
	}
}
