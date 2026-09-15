package medialocal

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos/media"
)

func expectRefusal(t *testing.T, err error) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "media.store" {
		t.Fatalf("unexpected error: %#v", reported)
	}
}

func TestFileSourcesOpenOnlyRegularFilesAndRecordTheirAbsoluteOrigin(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "image.iso")
	if err := os.WriteFile(path, []byte("installer"), 0600); err != nil {
		t.Fatal(err)
	}
	acquisition, err := New().Open(context.Background(), media.Source{Path: path})
	if err != nil {
		t.Fatalf("open: %#v", diagnostics.Of(err))
	}
	defer acquisition.Payload.Close()
	data, err := io.ReadAll(acquisition.Payload)
	if err != nil || string(data) != "installer" {
		t.Fatalf("payload = %q (%v)", data, err)
	}
	if acquisition.Origin != "file://"+path {
		t.Fatalf("origin = %q", acquisition.Origin)
	}
	_, err = New().Open(context.Background(), media.Source{Path: directory})
	expectRefusal(t, err)
	_, err = New().Open(context.Background(), media.Source{Path: filepath.Join(directory, "absent.iso")})
	expectRefusal(t, err)
}

func TestDownloadsRefuseCredentials(t *testing.T) {
	for _, raw := range []string{
		"https://operator:secret@example.test/image.iso",
		"ftp://example.test/image.iso",
		"https:///image.iso",
		"https://example.test/image.iso#fragment",
	} {
		_, err := New().Open(context.Background(), media.Source{URL: raw})
		expectRefusal(t, err)
	}
}

// A media URL names exactly the bytes to import, so a redirect would silently
// import an endpoint the operator never authorized.
func TestDownloadsFollowNoRedirectAndRefuseAnythingButOneServedImage(t *testing.T) {
	served := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/image.iso":
			io.WriteString(writer, "installer")
		case "/moved.iso":
			http.Redirect(writer, request, "/image.iso", http.StatusFound)
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer served.Close()
	acquisition, err := New().Open(context.Background(), media.Source{URL: served.URL + "/image.iso"})
	if err != nil {
		t.Fatalf("open: %#v", diagnostics.Of(err))
	}
	defer acquisition.Payload.Close()
	data, err := io.ReadAll(acquisition.Payload)
	if err != nil || string(data) != "installer" {
		t.Fatalf("payload = %q (%v)", data, err)
	}
	if acquisition.Origin != served.URL+"/image.iso" {
		t.Fatalf("origin = %q", acquisition.Origin)
	}
	_, err = New().Open(context.Background(), media.Source{URL: served.URL + "/moved.iso"})
	expectRefusal(t, err)
	_, err = New().Open(context.Background(), media.Source{URL: served.URL + "/absent.iso"})
	expectRefusal(t, err)
}

func TestCancellationStopsAcquisitionBeforeItOpensAnything(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New().Open(ctx, media.Source{Path: "/images/demo.iso"}); err == nil {
		t.Fatal("a canceled acquisition opened a source")
	}
}
