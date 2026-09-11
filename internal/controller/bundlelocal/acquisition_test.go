package bundlelocal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func TestAcquisitionRequiresExactBoundedPublisherBytesAndRedactsFailures(t *testing.T) {
	approved := []byte("approved artifact")
	source := fixtureSource("test-artifact", approved)
	for _, test := range []struct {
		name     string
		status   int
		body     []byte
		length   int64
		encoding string
		pass     bool
	}{
		{"exact", http.StatusOK, approved, int64(len(approved)), "", true},
		{"unknown length exact", http.StatusOK, approved, -1, "", true},
		{"wrong hash", http.StatusOK, []byte("modified artifact"), int64(len(approved)), "", false},
		{"truncated", http.StatusOK, approved[:4], 4, "", false},
		{"overflow", http.StatusOK, append(bytes.Clone(approved), '!'), -1, "", false},
		{"oversized header", http.StatusOK, approved, int64(len(approved)) + 1, "", false},
		{"server error", http.StatusServiceUnavailable, approved, int64(len(approved)), "", false},
		{"encoded", http.StatusOK, approved, int64(len(approved)), "gzip", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := &observedBody{Reader: bytes.NewReader(test.body)}
			data, err := receiveSource(t.Context(), source, func(request *http.Request) (*http.Response, error) {
				if request.URL.String() != source.URL || request.Method != http.MethodGet || request.Header.Get("Accept-Encoding") != "identity" || request.Header.Get("Authorization") != "" || request.Header.Get("Cookie") != "" {
					t.Fatal("acquisition request changed exact source or added ambient credentials")
				}
				headers := http.Header{}
				if test.encoding != "" {
					headers.Set("Content-Encoding", test.encoding)
				}
				return &http.Response{StatusCode: test.status, ContentLength: test.length, Header: headers, Body: body}, nil
			})
			if (err == nil) != test.pass || test.pass && !bytes.Equal(data, approved) {
				t.Fatalf("unexpected integrity result: %v", err)
			}
			if !body.closed || body.read > source.Bytes+1 {
				t.Fatal("acquisition failed its close or byte bound")
			}
		})
	}
	_, err := receiveSource(t.Context(), source, func(*http.Request) (*http.Response, error) {
		return nil, errors.New("proxy password or confidential address")
	})
	if err == nil || strings.Contains(err.Error(), "password") || strings.Contains(err.Error(), "confidential") {
		t.Fatal("raw transport failure leaked")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = receiveSource(ctx, source, func(request *http.Request) (*http.Response, error) { return nil, request.Context().Err() })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation identity lost: %v", err)
	}
}

type observedBody struct {
	*bytes.Reader
	closed bool
	read   int64
}

func (b *observedBody) Read(data []byte) (int, error) {
	n, err := b.Reader.Read(data)
	b.read += int64(n)
	return n, err
}
func (b *observedBody) Close() error { b.closed = true; return nil }

var _ io.ReadCloser = (*observedBody)(nil)

func TestAcquisitionOriginsRejectAuthoritySubstitution(t *testing.T) {
	for _, origin := range []string{"http://github.com/release", "https://github.com.evil.example/release", "https://user:secret@github.com/release", "https://github.com:444/release", "https://github.com/release#fragment", "https://127.0.0.1/release", "https://unapproved.example/release"} {
		parsed, err := url.Parse(origin)
		if err != nil {
			t.Fatal(err)
		}
		if approvedOrigin(parsed) {
			t.Fatalf("accepted origin %q", origin)
		}
	}
	for _, origin := range []string{"https://github.com/release", "https://release-assets.githubusercontent.com/release?signature=opaque", "https://files.pythonhosted.org/package.whl", "https://cdn-ubi.redhat.com/package.rpm"} {
		parsed, err := url.Parse(origin)
		if err != nil || !approvedOrigin(parsed) {
			t.Fatalf("rejected approved origin %q", origin)
		}
	}
}

func TestPublicAcquirerRejectsSourceSubstitutionBeforeTrustOrNetwork(t *testing.T) {
	source := fixtureSource("unapproved", []byte("synthetic"))
	if _, err := (Acquirer{}).Acquire(t.Context(), source, prerequisites.SetupEgress{}); err == nil {
		t.Fatal("public acquisition accepted a source outside the compiled catalog")
	}
	definition, err := (Catalog{}).Select(prerequisites.Platform{OS: "fedora", Release: "43", Architecture: "amd64"}, prerequisites.NativeRequirements{})
	if err != nil {
		t.Fatal(err)
	}
	source = definition.Sources[0]
	source.URL = "https://files.pythonhosted.org/unapproved-origin"
	if _, err := (Acquirer{}).Acquire(t.Context(), source, prerequisites.SetupEgress{}); err == nil {
		t.Fatal("public acquisition accepted a substituted URL with a retained digest")
	}
}

func TestExplicitProxyIgnoresAmbientAndMatchesWithoutDNS(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://ambient.invalid:3128")
	t.Setenv("HTTP_PROXY", "http://ambient.invalid:3128")
	t.Setenv("ALL_PROXY", "http://ambient.invalid:3128")
	t.Setenv("NO_PROXY", "*")
	request, _ := http.NewRequest(http.MethodGet, "https://files.pythonhosted.org/package", nil)
	direct, err := explicitProxy(prerequisites.SetupEgress{})
	if err != nil {
		t.Fatal(err)
	}
	if selected, err := direct(request); err != nil || selected != nil {
		t.Fatal("direct acquisition used ambient proxy")
	}
	proxied, err := explicitProxy(prerequisites.SetupEgress{HTTPSProxy: "http://explicit.example:3128", NoProxy: []string{".bypass.example", "192.0.2.0/24", "exact.example:443"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		target string
		bypass bool
	}{
		{"https://files.pythonhosted.org/package", false},
		{"https://bypass.example/package", false},
		{"https://child.bypass.example/package", true},
		{"https://192.0.2.20/package", true},
		{"https://192.0.3.20/package", false},
		{"https://exact.example/package", true},
		{"https://exact.example:444/package", false},
	} {
		request, _ := http.NewRequest(http.MethodGet, test.target, nil)
		selected, err := proxied(request)
		if err != nil || (selected == nil) != test.bypass {
			t.Fatalf("proxy route %q: %v %v", test.target, selected, err)
		}
	}
	for _, policy := range []prerequisites.SetupEgress{{HTTPSProxy: "http://user:secret@proxy.example"}, {HTTPProxy: "http://proxy.example"}, {HTTPSProxy: "http://proxy.example/config"}, {NoProxy: []string{"https://invalid.example"}}} {
		if _, err := explicitProxy(policy); err == nil {
			t.Fatal("accepted unqualified explicit proxy policy")
		}
	}
}
