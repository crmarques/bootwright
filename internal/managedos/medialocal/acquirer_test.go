package medialocal

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/workspace/invokerfs"
)

// invokerFiles opens through the invoking account's opener as a process that
// is not root does: in-process, under its own credentials.
type invokerFiles struct{}

func (invokerFiles) Begin(ctx context.Context) (FileSession, error) {
	var opener *invokerfs.Opener
	session, err := opener.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return session, nil
}

// virtualFiles serves absolute paths beneath root, so a path it serves exists
// nowhere this adapter could open on its own.
type virtualFiles struct{ root string }

func (v virtualFiles) Begin(context.Context) (FileSession, error) { return v, nil }

func (v virtualFiles) OpenFile(path string) (*os.File, error) {
	return os.Open(filepath.Join(v.root, path))
}

func (virtualFiles) Close() error { return nil }

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
	acquisition, err := New(nil, invokerFiles{}).Open(context.Background(), media.Source{Path: path})
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
	_, err = New(nil, invokerFiles{}).Open(context.Background(), media.Source{Path: directory})
	expectRefusal(t, err)
	_, err = New(nil, invokerFiles{}).Open(context.Background(), media.Source{Path: filepath.Join(directory, "absent.iso")})
	expectRefusal(t, err)
}

func TestFromFileOpensOnlyThroughTheOpener(t *testing.T) {
	root := t.TempDir()
	virtual := filepath.Join("/", "bootwright-"+strconv.FormatInt(time.Now().UnixNano(), 36), "image.iso")
	if err := os.MkdirAll(filepath.Join(root, filepath.Dir(virtual)), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, virtual), []byte("installer"), 0600); err != nil {
		t.Fatal(err)
	}
	acquisition, err := New(nil, virtualFiles{root: root}).Open(context.Background(), media.Source{Path: virtual})
	if err != nil {
		t.Fatalf("open: %#v", diagnostics.Of(err))
	}
	defer acquisition.Payload.Close()
	data, err := io.ReadAll(acquisition.Payload)
	if err != nil || string(data) != "installer" || acquisition.Origin != "file://"+virtual {
		t.Fatalf("payload %q from %q (%v)", data, acquisition.Origin, err)
	}
	if _, err := os.Lstat(virtual); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the virtual source exists on this host, so the test proves nothing: %v", err)
	}
}

func TestDownloadsRefuseCredentials(t *testing.T) {
	for _, raw := range []string{
		"https://operator:secret@example.test/image.iso",
		"ftp://example.test/image.iso",
		"https:///image.iso",
		"https://example.test/image.iso#fragment",
	} {
		_, err := New(nil, nil).Open(context.Background(), media.Source{URL: raw})
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
	acquisition, err := New(nil, nil).Open(context.Background(), media.Source{URL: served.URL + "/image.iso"})
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
	_, err = New(nil, nil).Open(context.Background(), media.Source{URL: served.URL + "/moved.iso"})
	expectRefusal(t, err)
	_, err = New(nil, nil).Open(context.Background(), media.Source{URL: served.URL + "/absent.iso"})
	expectRefusal(t, err)
}

func TestCancellationStopsAcquisitionBeforeItOpensAnything(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := New(nil, nil).Open(ctx, media.Source{Path: "/images/demo.iso"}); err == nil {
		t.Fatal("a canceled acquisition opened a source")
	}
}

// The route is supplied by the caller, so an import reaches the endpoint the
// operator's environment selected rather than one this adapter discovers.
func TestDownloadsTakeTheSuppliedRouteAndFailClosedWithoutOne(t *testing.T) {
	var requested string
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requested = request.URL.String()
		io.WriteString(writer, "installer")
	}))
	defer proxy.Close()
	endpoint, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	acquisition, err := New(func(*http.Request) (*url.URL, error) { return endpoint, nil }, nil).
		Open(context.Background(), media.Source{URL: "http://images.example/image.iso"})
	if err != nil {
		t.Fatalf("open: %#v", diagnostics.Of(err))
	}
	defer acquisition.Payload.Close()
	data, err := io.ReadAll(acquisition.Payload)
	if err != nil || string(data) != "installer" {
		t.Fatalf("payload = %q (%v)", data, err)
	}
	if requested != "http://images.example/image.iso" {
		t.Fatalf("the proxy was not asked for the exact image: %q", requested)
	}
	if acquisition.Origin != "http://images.example/image.iso" {
		t.Fatalf("origin = %q", acquisition.Origin)
	}
	unusable := New(func(*http.Request) (*url.URL, error) { return nil, errors.New("route is not qualified") }, nil)
	if _, err := unusable.Open(context.Background(), media.Source{URL: "http://images.example/image.iso"}); err == nil {
		t.Fatal("an import proceeded without a usable route")
	}
}
