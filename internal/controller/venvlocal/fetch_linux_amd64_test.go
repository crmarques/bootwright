//go:build linux && amd64

package venvlocal

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }

// publisher answers each locked URL with the bytes named by its filename,
// counting requests and the most that were ever in flight together.
type publisher struct {
	requests, inFlight, peak atomic.Int32
	body                     func(filename string) []byte
	mutex                    sync.Mutex
	urls                     []string
}

func (p *publisher) client(context.Context, prerequisites.SetupEgress) (*http.Client, error) {
	return &http.Client{Transport: roundTrip(func(request *http.Request) (*http.Response, error) {
		p.requests.Add(1)
		current := p.inFlight.Add(1)
		defer p.inFlight.Add(-1)
		for {
			peak := p.peak.Load()
			if current <= peak || p.peak.CompareAndSwap(peak, current) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		p.mutex.Lock()
		p.urls = append(p.urls, request.URL.String())
		p.mutex.Unlock()
		body := p.body(path.Base(request.URL.Path))
		return &http.Response{StatusCode: http.StatusOK, ContentLength: int64(len(body)), Body: io.NopCloser(bytes.NewReader(body)), Header: http.Header{}}, nil
	})}, nil
}

func TestACacheHitMakesNoRequest(t *testing.T) {
	host := newFakeHost(t)
	adapter := host.adapter(testLock(t, "3.14"))
	source := &publisher{body: wheelBytes}
	adapter.client = source.client
	if err := adapter.Fetch(context.Background(), "3.14", prerequisites.SetupEgress{}); err != nil {
		t.Fatal(err)
	}
	files, _ := adapter.lock.Files("3.14")
	if got, want := source.requests.Load(), int32(len(files)+1); got != want {
		t.Fatalf("a cold cache made %d requests, want one per locked file and the installer, %d", got, want)
	}
	if peak := source.peak.Load(); peak > fetchers {
		t.Fatalf("%d fetches ran at once, above the bound of %d", peak, fetchers)
	}
	wheels, err := adapter.Wheels(context.Background(), "3.14")
	if err != nil {
		t.Fatal(err)
	}
	for _, wheel := range wheels {
		if !wheel.Cached {
			t.Fatalf("%s is not cached after a fetch", wheel.Filename)
		}
	}
	source.requests.Store(0)
	if err := adapter.Fetch(context.Background(), "3.14", prerequisites.SetupEgress{}); err != nil {
		t.Fatal(err)
	}
	if got := source.requests.Load(); got != 0 {
		t.Fatalf("a warm cache made %d requests", got)
	}
	if err := os.WriteFile(adapter.cachePath(files[0]), []byte("tampered"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Fetch(context.Background(), "3.14", prerequisites.SetupEgress{}); err != nil {
		t.Fatal(err)
	}
	if got := source.requests.Load(); got != 1 {
		t.Fatalf("one tampered cache entry made %d requests, want 1", got)
	}
}

func TestAWheelWithAnotherHashIsRefused(t *testing.T) {
	host := newFakeHost(t)
	adapter := host.adapter(testLock(t, "3.14"))
	files, _ := adapter.lock.Files("3.14")
	target := files[0].Filename
	adapter.client = (&publisher{body: func(filename string) []byte {
		data := wheelBytes(filename)
		if filename == target {
			data[0] ^= 0xff
		}
		return data
	}}).client
	err := adapter.Fetch(context.Background(), "3.14", prerequisites.SetupEgress{})
	if err == nil {
		t.Fatal("a wheel whose bytes differ from the lock's SHA-256 was accepted")
	}
	directory := filepath.Join(adapter.wheels, files[0].SHA256)
	entries, _ := os.ReadDir(directory)
	if len(entries) != 0 {
		t.Fatalf("a refused wheel left %v in its cache directory", entries)
	}
	if adapter.cached(files[0]) == nil {
		t.Fatal("the refused wheel reads as cached")
	}
}

func TestAWheelURLOutsideHTTPSIsRefused(t *testing.T) {
	for _, address := range []string{"http://files.pythonhosted.org/x.whl", "https://user@files.pythonhosted.org/x.whl", "file:///x.whl", "https://files.pythonhosted.org:8443/x.whl"} {
		parsed, err := http.NewRequest(http.MethodGet, address, nil)
		if err != nil {
			t.Fatal(err)
		}
		if approvedURL(parsed.URL) {
			t.Errorf("%s is approved", address)
		}
	}
}
