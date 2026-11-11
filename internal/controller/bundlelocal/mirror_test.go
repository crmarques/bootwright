package bundlelocal

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/acquisition"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

func mirrorServer(t *testing.T, requests *[]string) *httptest.Server {
	t.Helper()
	var server *httptest.Server
	server = httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		*requests = append(*requests, request.Method+" "+request.URL.Path+" "+request.Header.Get("Range"))
		switch request.URL.Path {
		case "/tools/present", "/tools/wrong-size":
			size := "1024"
			if request.URL.Path == "/tools/wrong-size" {
				size = "7"
			}
			writer.Header().Set("Content-Length", size)
			if request.Method == http.MethodHead {
				return
			}
			t.Errorf("a HEAD-capable file was read with %s", request.Method)
		case "/tools/no-head":
			if request.Method == http.MethodHead {
				writer.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			writer.Header().Set("Content-Range", "bytes 0-0/1024")
			writer.WriteHeader(http.StatusPartialContent)
			_, _ = writer.Write([]byte{0})
		case "/tools/moved":
			http.Redirect(writer, request, "/tools/present", http.StatusFound)
		case "/tools/downgraded":
			http.Redirect(writer, request, "http://"+strings.TrimPrefix(server.URL, "https://")+"/tools/present", http.StatusFound)
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func probeClient(t *testing.T, server *httptest.Server, roots *x509.CertPool) (*http.Client, *url.URL) {
	t.Helper()
	origin, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	proxy, err := explicitProxy(prerequisites.SetupEgress{NoProxy: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	client, closeIdle, err := routedClient(proxy, roots, sameOrigin(origin), 5*time.Second, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeIdle)
	return client, origin
}

// Each file's size is read with no body: a HEAD, a one-byte ranged GET where
// HEAD is refused, a redirect followed on the mirror's own HTTPS host and
// never to plain HTTP.
func TestTheMirrorProbeReadsEachSizeWithoutItsBody(t *testing.T) {
	var requests []string
	server := mirrorServer(t, &requests)
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	client, origin := probeClient(t, server, roots)
	files := []MirrorFile{}
	for _, name := range []string{"present", "absent", "wrong-size", "no-head", "moved", "downgraded"} {
		files = append(files, MirrorFile{URL: server.URL + "/tools/" + name, Bytes: 1024})
	}
	readings := readMirror(context.Background(), client, origin, files)
	want := map[string]struct {
		status int
		served int64
	}{"present": {200, 1024}, "absent": {404, int64(len("404 page not found\n"))}, "wrong-size": {200, 7}, "no-head": {206, 1024}, "moved": {200, 1024}, "downgraded": {0, -1}}
	for _, reading := range readings {
		name := reading.File.URL[strings.LastIndex(reading.File.URL, "/")+1:]
		expected := want[name]
		if reading.Status != expected.status || reading.Served != expected.served {
			t.Errorf("%s: status %d serving %d (%s), want %d serving %d", name, reading.Status, reading.Served, reading.Failure, expected.status, expected.served)
		}
		if (reading.Status == 0) != (reading.Failure != "") {
			t.Errorf("%s: failure %q beside status %d", name, reading.Failure, reading.Status)
		}
	}
	if downgraded := readings[5]; !strings.Contains(downgraded.Failure, "refused") {
		t.Errorf("a redirect to plain HTTP was not refused: %q", downgraded.Failure)
	}
	for _, request := range requests {
		if strings.HasPrefix(request, "GET") && request != "GET /tools/no-head bytes=0-0" {
			t.Errorf("the probe asked for a body: %s", request)
		}
	}
}

func TestTheMirrorProbeVerifiesTheMirrorAgainstItsAnchors(t *testing.T) {
	var requests []string
	server := mirrorServer(t, &requests)
	client, origin := probeClient(t, server, x509.NewCertPool())
	readings := readMirror(context.Background(), client, origin, []MirrorFile{{URL: server.URL + "/tools/present", Bytes: 1024}})
	if readings[0].Status != 0 || !strings.Contains(readings[0].Failure, "neither the system trust store nor the mirror's trust anchor accepts") {
		t.Fatalf("an unanchored mirror was read: %+v", readings[0])
	}
	if strings.Contains(readings[0].Failure, "/tools/") {
		t.Fatalf("the failure names the request path: %q", readings[0].Failure)
	}
}

func TestTheMirrorProbeRefusesFilesOutsideOneMirrorBeforeAnyRequest(t *testing.T) {
	for name, files := range map[string][]MirrorFile{
		"two hosts":    {{URL: "https://mirror.example.test/tools/helm", Bytes: 1}, {URL: "https://other.example.test/tools/govc", Bytes: 1}},
		"plain HTTP":   {{URL: "http://mirror.example.test/tools/helm", Bytes: 1}},
		"no size":      {{URL: "https://mirror.example.test/tools/helm"}},
		"a userinfo":   {{URL: "https://user@mirror.example.test/tools/helm", Bytes: 1}},
		"another port": {{URL: "https://mirror.example.test:8443/tools/helm", Bytes: 1}},
	} {
		if _, err := ProbeMirror(context.Background(), files, prerequisites.SetupEgress{NoProxy: []string{}}, nil); err == nil {
			t.Errorf("%s: the probe was admitted", name)
		}
	}
}

// The anchor verifies the mirror alone: a TLS connection to any other
// authority, an https:// proxy included, verifies against the system pool.
func TestTheMirrorAnchorVerifiesOnlyTheMirrorsOwnAuthority(t *testing.T) {
	var requests []string
	mirror, proxy := mirrorServer(t, &requests), mirrorServer(t, &requests)
	anchored := x509.NewCertPool()
	anchored.AddCert(mirror.Certificate())
	origin, err := url.Parse(mirror.URL)
	if err != nil {
		t.Fatal(err)
	}
	dial := acquisition.AnchoredDial((&net.Dialer{Timeout: 5 * time.Second}).DialContext, x509.NewCertPool(), originAuthority(origin), anchored)
	connection, err := dial(context.Background(), "tcp", origin.Host)
	if err != nil {
		t.Fatalf("the mirror's own authority did not verify against its anchor: %v", err)
	}
	connection.Close()
	if connection, err := dial(context.Background(), "tcp", strings.TrimPrefix(proxy.URL, "https://")); err == nil {
		connection.Close()
		t.Fatal("another authority, such as an https:// proxy, verified against the mirror's anchor")
	}
	explicit, err := explicitProxy(prerequisites.SetupEgress{NoProxy: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	client, closeIdle, err := routedClient(explicit, x509.NewCertPool(), sameOrigin(origin), 5*time.Second, &trustAnchor{authority: originAuthority(origin), pool: anchored})
	if err != nil {
		t.Fatal(err)
	}
	defer closeIdle()
	if reading := readMirror(context.Background(), client, origin, []MirrorFile{{URL: mirror.URL + "/tools/present", Bytes: 1024}})[0]; reading.Status != 200 || reading.Served != 1024 {
		t.Fatalf("the anchored mirror was not read: %+v", reading)
	}
}

func TestMirrorTLSAcceptsPublicServerAnchorsAndRetainsFullVerification(t *testing.T) {
	fixtures := "../../../ansible/collections/ansible_collections/bootwright/core/tests/unit/plugins/module_utils/fixtures/tls-trust"
	for _, name := range []string{"issued", "self-signed", "wrong-name", "expired", "client-only"} {
		t.Run(name, func(t *testing.T) {
			certificate, err := os.ReadFile(filepath.Join(fixtures, name+".pem"))
			if err != nil {
				t.Fatal(err)
			}
			key, err := os.ReadFile(filepath.Join(fixtures, "server.key"))
			if err != nil {
				t.Fatal(err)
			}
			pair, err := tls.X509KeyPair(certificate, key)
			if err != nil {
				t.Fatal(err)
			}
			var reached atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { reached.Add(1); w.Header().Set("Content-Length", "1024") }))
			server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}}
			server.StartTLS()
			defer server.Close()
			roots := x509.NewCertPool()
			if !roots.AppendCertsFromPEM(certificate) {
				t.Fatal("fixture is not PEM")
			}
			client, origin := probeClient(t, server, roots)
			reading := readMirror(context.Background(), client, origin, []MirrorFile{{URL: server.URL + "/tool", Bytes: 1024}})[0]
			valid := name == "issued" || name == "self-signed"
			if (reading.Status == 200) != valid || (reached.Load() > 0) != valid {
				t.Fatalf("%s: full TLS proof failed: %+v, HTTP count %d", name, reading, reached.Load())
			}
		})
	}
}
