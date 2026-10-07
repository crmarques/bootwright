package medialocal

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"log"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
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
	if origin, err := New(nil, invokerFiles{}).Origin(media.Source{Path: path}); err != nil || origin != "file://"+path {
		t.Fatalf("origin = %q (%v)", origin, err)
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
	origin, originErr := New(nil, virtualFiles{root: root}).Origin(media.Source{Path: virtual})
	if err != nil || string(data) != "installer" || originErr != nil || origin != "file://"+virtual {
		t.Fatalf("payload %q from %q (%v, %v)", data, origin, err, originErr)
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
		"https:image.iso",
	} {
		_, err := New(nil, nil).Open(context.Background(), media.Source{URL: raw})
		expectRefusal(t, err)
		_, err = New(nil, nil).Origin(media.Source{URL: raw})
		expectRefusal(t, err)
	}
}

// A media URL names exactly the bytes to import, so a redirect would silently
// import an endpoint the operator never authorized.
func TestDownloadsFollowNoRedirectAndRefuseAnythingButOneServedImage(t *testing.T) {
	served := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
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
	acquirer := trusting(t, served, nil)
	acquisition, err := acquirer.Open(context.Background(), media.Source{URL: served.URL + "/image.iso"})
	if err != nil {
		t.Fatalf("open: %#v", diagnostics.Of(err))
	}
	defer acquisition.Payload.Close()
	data, err := io.ReadAll(acquisition.Payload)
	if err != nil || string(data) != "installer" {
		t.Fatalf("payload = %q (%v)", data, err)
	}
	if origin, err := acquirer.Origin(media.Source{URL: served.URL + "/image.iso"}); err != nil || origin != served.URL+"/image.iso" {
		t.Fatalf("origin = %q (%v)", origin, err)
	}
	_, err = acquirer.Open(context.Background(), media.Source{URL: served.URL + "/moved.iso"})
	expectRefusal(t, err)
	_, err = acquirer.Open(context.Background(), media.Source{URL: served.URL + "/absent.iso"})
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
	paths := make(chan string, 1)
	served := servedAs(t, "images.example", time.Now().Add(time.Hour), http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		paths <- request.URL.Path
		io.WriteString(writer, "installer")
	}))
	connects := make(chan string, 4)
	proxy := tunnel(t, served.Listener.Addr().String(), connects)
	endpoint, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	acquirer := trusting(t, served, func(*http.Request) (*url.URL, error) { return endpoint, nil })
	acquisition, err := acquirer.Open(context.Background(), media.Source{URL: "https://images.example/image.iso"})
	if err != nil {
		t.Fatalf("open: %#v", diagnostics.Of(err))
	}
	defer acquisition.Payload.Close()
	data, err := io.ReadAll(acquisition.Payload)
	if requested := <-paths; err != nil || string(data) != "installer" || requested != "/image.iso" {
		t.Fatalf("payload = %q for %q (%v)", data, requested, err)
	}
	if connected := <-connects; connected != "CONNECT images.example:443" {
		t.Fatalf("the proxy was asked %q, want a tunnel to the image's endpoint", connected)
	}
	if origin, err := acquirer.Origin(media.Source{URL: "https://images.example/image.iso"}); err != nil || origin != "https://images.example/image.iso" {
		t.Fatalf("origin = %q (%v)", origin, err)
	}
	var reached atomic.Int32
	counted := servedAs(t, "images.example", time.Now().Add(time.Hour), http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		reached.Add(1)
		io.WriteString(writer, "installer")
	}))
	unusable := trusting(t, counted, func(*http.Request) (*url.URL, error) { return nil, errors.New("route is not qualified") })
	dialing(unusable, counted.Listener.Addr().String())
	// The transport's own hook would refuse the route as well; clearing it
	// leaves only the adapter's refusal between the request and the endpoint.
	transport(unusable).Proxy = nil
	_, err = unusable.Open(context.Background(), media.Source{URL: "https://images.example/image.iso"})
	reported := message(t, err)
	if reported.Message != "the download route this host selected cannot be used" ||
		reported.Remediation != "correct the HTTPS_PROXY and NO_PROXY values this command was invoked with, then repeat it" {
		t.Fatalf("an unusable route reported %#v", reported)
	}
	if seen := reached.Load(); seen != 0 {
		t.Fatalf("an import without a usable route reached its endpoint %d times", seen)
	}
}

// transport is the acquirer's own transport, which in-package tests point at
// a test endpoint's trust anchor or dialer before its first request.
func transport(acquirer Acquirer) *http.Transport {
	return acquirer.client.Transport.(*http.Transport)
}

// trusting is an acquirer whose transport trusts the server's certificate.
func trusting(t *testing.T, server *httptest.Server, proxy func(*http.Request) (*url.URL, error)) Acquirer {
	t.Helper()
	acquirer := New(proxy, nil)
	pool := x509.NewCertPool()
	pool.AddCert(server.Certificate())
	transport(acquirer).TLSClientConfig.RootCAs = pool
	t.Cleanup(transport(acquirer).CloseIdleConnections)
	return acquirer
}

// dialing points every connection of the acquirer at address, whatever host
// the URL names.
func dialing(acquirer Acquirer, address string) {
	transport(acquirer).DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
}

// servedAs serves HTTPS under a self-signed certificate for host that expires
// at notAfter.
func servedAs(t *testing.T, host string, notAfter time.Time, handler http.Handler) *httptest.Server {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: host}, DNSNames: []string{host},
		NotBefore: notAfter.Add(-48 * time.Hour), NotAfter: notAfter,
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true, IsCA: true,
	}
	certificate, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{certificate}, PrivateKey: key}}}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server
}

// tunnel is an HTTP proxy that reports each request line it receives and
// tunnels a CONNECT to upstream, whatever authority it names.
func tunnel(t *testing.T, upstream string, requests chan<- string) *httptest.Server {
	t.Helper()
	proxy := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		requests <- request.Method + " " + request.Host
		if request.Method != http.MethodConnect {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		server, err := net.Dial("tcp", upstream)
		if err != nil {
			writer.WriteHeader(http.StatusBadGateway)
			return
		}
		defer server.Close()
		client, buffered, err := writer.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer client.Close()
		if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
			return
		}
		go func() {
			io.Copy(server, buffered)
			server.Close()
		}()
		io.Copy(client, server)
	}))
	t.Cleanup(proxy.Close)
	return proxy
}

// message is the one media.store refusal an error reports.
func message(t *testing.T, err error) diagnostics.Diagnostic {
	t.Helper()
	expectRefusal(t, err)
	return diagnostics.Of(err)[0]
}

func TestPlainHTTPMediaURLsRefuseBeforeAnyRequest(t *testing.T) {
	var requests atomic.Int32
	served := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		io.WriteString(writer, "installer")
	}))
	defer served.Close()
	acquirer := New(nil, nil)
	defer transport(acquirer).CloseIdleConnections()
	for _, refused := range []error{
		func() error {
			_, err := acquirer.Open(context.Background(), media.Source{URL: served.URL + "/image.iso"})
			return err
		}(),
		func() error { _, err := acquirer.Origin(media.Source{URL: served.URL + "/image.iso"}); return err }(),
	} {
		if reported := message(t, refused); reported.Message != "the media URL is not HTTPS" || reported.Remediation != "name the image's https:// URL" {
			t.Fatalf("plain HTTP refused as %#v", reported)
		}
	}
	if seen := requests.Load(); seen != 0 {
		t.Fatalf("a plain HTTP media URL reached its endpoint %d times", seen)
	}
}

func TestARedirectNamesItsStatusAndTarget(t *testing.T) {
	long := "/" + strings.Repeat("b", 2000) + ".iso"
	served := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/long.iso" {
			http.Redirect(writer, request, long+"?sig=x", http.StatusMovedPermanently)
			return
		}
		http.Redirect(writer, request, "/image.iso?sig=x", http.StatusFound)
	}))
	defer served.Close()
	acquirer := trusting(t, served, nil)
	host := strings.TrimPrefix(served.URL, "https://")
	for path, want := range map[string]string{
		"/moved.iso?sig=y": "answered HTTP 302 redirecting to " + served.URL + "/image.iso,",
		"/long.iso":        "answered HTTP 301 redirecting to " + (served.URL + long)[:media.MaxMediaOrigin] + "...,",
	} {
		_, err := acquirer.Open(context.Background(), media.Source{URL: served.URL + path})
		reported := message(t, err)
		if reported.Message != "the media endpoint "+host+" "+want+" and a media download follows no redirect" ||
			reported.Remediation != "name the exact image URL" || strings.Contains(err.Error(), "sig=") {
			t.Fatalf("the redirect from %s reported %#v (%v)", path, reported, err)
		}
	}
}

func TestAnUnresolvableNameIsNamed(t *testing.T) {
	acquirer := New(nil, nil)
	transport(acquirer).DialContext = func(context.Context, string, string) (net.Conn, error) {
		return nil, &net.DNSError{Err: "no such host", Name: "images.example", IsNotFound: true}
	}
	_, err := acquirer.Open(context.Background(), media.Source{URL: "https://images.example/image.iso?sig=x"})
	reported := message(t, err)
	if reported.Message != "the media endpoint images.example could not be reached: the name images.example could not be resolved by this host's resolver" ||
		!strings.HasPrefix(reported.Remediation, "make images.example resolvable from this host") || strings.Contains(err.Error(), "sig=") {
		t.Fatalf("the unresolvable name reported %#v", reported)
	}
}

func TestAnUntrustedCertificateIsNamed(t *testing.T) {
	handler := http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) { io.WriteString(writer, "installer") })
	unknown := httptest.NewTLSServer(handler)
	defer unknown.Close()
	expired := servedAs(t, "images.example", time.Now().Add(-time.Hour), handler)
	other := servedAs(t, "other.example", time.Now().Add(time.Hour), handler)
	for _, test := range []struct {
		acquirer Acquirer
		served   *httptest.Server
		url      string
		reason   string
	}{
		{New(nil, nil), nil, unknown.URL + "/image.iso", " (signed by an unknown authority)"},
		{trusting(t, expired, nil), expired, "https://images.example/image.iso", " (expired or not yet valid)"},
		{trusting(t, other, nil), other, "https://images.example/image.iso", " (not valid for the host)"},
	} {
		if test.served != nil {
			dialing(test.acquirer, test.served.Listener.Addr().String())
		}
		_, err := test.acquirer.Open(context.Background(), media.Source{URL: test.url})
		reported := message(t, err)
		host := strings.TrimPrefix(strings.TrimSuffix(test.url, "/image.iso"), "https://")
		if reported.Message != "the media endpoint "+host+" presented a certificate this host's system trust store does not accept"+test.reason ||
			!strings.HasPrefix(reported.Remediation, "install the issuing certificate authority in this host's system trust store") {
			t.Fatalf("%s reported %#v", test.url, reported)
		}
		transport(test.acquirer).CloseIdleConnections()
	}
}

type dialTimeoutError struct{}

func (dialTimeoutError) Error() string   { return "i/o timeout" }
func (dialTimeoutError) Timeout() bool   { return true }
func (dialTimeoutError) Temporary() bool { return true }

func TestAConnectionTimeoutNamesItsBounds(t *testing.T) {
	acquirer := New(nil, nil)
	transport(acquirer).DialContext = func(context.Context, string, string) (net.Conn, error) { return nil, dialTimeoutError{} }
	_, err := acquirer.Open(context.Background(), media.Source{URL: "https://images.example/image.iso"})
	if reported := message(t, err); reported.Message != "the media endpoint images.example, reached directly, did not answer within its 30-second connection timeout and 60-second response timeout" {
		t.Fatalf("the timeout reported %#v", reported)
	}
}

func TestEachHTTPStatusIsNamed(t *testing.T) {
	served := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		code, err := strconv.Atoi(strings.TrimPrefix(request.URL.Path, "/"))
		if err != nil {
			code = http.StatusTeapot
		}
		writer.WriteHeader(code)
	}))
	defer served.Close()
	acquirer := trusting(t, served, nil)
	host := strings.TrimPrefix(served.URL, "https://")
	for code, want := range map[int]struct{ status, remedy string }{
		http.StatusNotFound:           {"404 Not Found", "verify the image URL"},
		http.StatusForbidden:          {"403 Forbidden", "name a URL that serves the image without credentials, such as a pre-signed one"},
		http.StatusServiceUnavailable: {"503 Service Unavailable", "repeat the command later, or name the image on another mirror"},
	} {
		_, err := acquirer.Open(context.Background(), media.Source{URL: served.URL + "/" + strconv.Itoa(code)})
		reported := message(t, err)
		if reported.Message != "the media endpoint "+host+" answered HTTP "+want.status+", so it served no image" || reported.Remediation != want.remedy {
			t.Fatalf("HTTP %d reported %#v", code, reported)
		}
	}
}

// stalledBody answers every read with the error its transfer context reports,
// as a response body does once that context ends.
type stalledBody struct{ transfer context.Context }

func (b stalledBody) Read([]byte) (int, error) { return 0, b.transfer.Err() }
func (stalledBody) Close() error               { return nil }

func TestTheTransferDeadlineIsNamed(t *testing.T) {
	transfer, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	expired := &download{body: stalledBody{transfer}, parent: context.Background(), transfer: transfer, cancel: cancel, host: "images.example"}
	_, err := expired.Read(make([]byte, 1))
	if reported := message(t, err); reported.Message != "the download from images.example did not finish within its 6-hour transfer deadline" ||
		reported.Remediation != "copy the image locally and add it with --from-file, or repeat the command over a faster route" {
		t.Fatalf("the transfer deadline reported %#v", reported)
	}
	expired.Close()
	parent, stop := context.WithCancel(context.Background())
	stop()
	transfer, cancel = context.WithTimeout(parent, transferTimeout)
	canceled := &download{body: stalledBody{transfer}, parent: parent, transfer: transfer, cancel: cancel, host: "images.example"}
	defer canceled.Close()
	if _, err := canceled.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) || len(diagnostics.Of(err)) != 0 {
		t.Fatalf("a canceled download reported %v (%#v)", err, diagnostics.Of(err))
	}
}

func TestACanceledDownloadReportsTheCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := httptest.NewTLSServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		cancel()
		<-request.Context().Done()
	}))
	defer served.Close()
	_, err := trusting(t, served, nil).Open(ctx, media.Source{URL: served.URL + "/image.iso"})
	if !errors.Is(err, context.Canceled) || len(diagnostics.Of(err)) != 0 {
		t.Fatalf("a canceled download reported %v (%#v)", err, diagnostics.Of(err))
	}
}

// A download canceled while its body streams passes the cancellation through
// unchanged, so the store reports it as the cancellation.
func TestADownloadCanceledMidBodyReportsTheCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	served := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Length", "1000000")
		io.WriteString(writer, strings.Repeat("x", 1000))
		writer.(http.Flusher).Flush()
		<-request.Context().Done()
	}))
	defer served.Close()
	acquisition, err := trusting(t, served, nil).Open(ctx, media.Source{URL: served.URL + "/image.iso"})
	if err != nil {
		t.Fatalf("open: %#v", diagnostics.Of(err))
	}
	defer acquisition.Payload.Close()
	buffer := make([]byte, 64)
	if n, err := acquisition.Payload.Read(buffer); n == 0 || err != nil {
		t.Fatalf("the first read = %d (%v)", n, err)
	}
	cancel()
	for range 1000 {
		if _, err = acquisition.Payload.Read(buffer); err != nil {
			break
		}
	}
	if !errors.Is(err, context.Canceled) || len(diagnostics.Of(err)) != 0 {
		t.Fatalf("a download canceled mid-body reported %v (%#v)", err, diagnostics.Of(err))
	}
}

// The record names the image without the query, which may carry a signature,
// while the download still requests it.
func TestTheRecordedOriginDropsTheQuery(t *testing.T) {
	queries := make(chan string, 1)
	served := httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		queries <- request.URL.RawQuery
		io.WriteString(writer, "installer")
	}))
	defer served.Close()
	acquirer := trusting(t, served, nil)
	signed := media.Source{URL: served.URL + "/p/rhel.iso?X-Amz-Signature=abc"}
	if origin, err := acquirer.Origin(signed); err != nil || origin != served.URL+"/p/rhel.iso" {
		t.Fatalf("origin = %q (%v)", origin, err)
	}
	acquisition, err := acquirer.Open(context.Background(), signed)
	if err != nil {
		t.Fatalf("open: %#v", diagnostics.Of(err))
	}
	acquisition.Payload.Close()
	if query := <-queries; query != "X-Amz-Signature=abc" {
		t.Fatalf("the endpoint received the query %q", query)
	}
}

// proxied is an acquirer whose supplied route selects the proxy at raw for
// every request.
func proxied(t *testing.T, raw string) (Acquirer, *url.URL) {
	t.Helper()
	proxy, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	acquirer := New(func(*http.Request) (*url.URL, error) { return proxy, nil }, nil)
	t.Cleanup(transport(acquirer).CloseIdleConnections)
	return acquirer, proxy
}

// A failure behind a proxy names that proxy, so an operator who already set
// HTTPS_PROXY is never told to set it.
func TestATransportFailureNamesTheProxyItTook(t *testing.T) {
	connects := make(chan string, 4)
	refusing := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connects <- request.Method + " " + request.Host
		writer.WriteHeader(http.StatusForbidden)
	}))
	defer refusing.Close()
	acquirer, proxy := proxied(t, refusing.URL)
	_, err := acquirer.Open(context.Background(), media.Source{URL: "https://images.example/image.iso?sig=x"})
	reported := message(t, err)
	if reported.Message != "the media endpoint images.example could not be reached from this host through the proxy "+proxy.Host ||
		reported.Remediation != "verify that the proxy "+proxy.Host+" admits this host and reaches images.example over HTTPS" ||
		strings.Contains(err.Error(), "sig=") {
		t.Fatalf("the refused tunnel reported %#v", reported)
	}
	if connected := <-connects; connected != "CONNECT images.example:443" {
		t.Fatalf("the proxy was asked %q, want a tunnel to the image's endpoint", connected)
	}
}

// Behind a proxy the name this host resolves is the proxy's, so the remedy
// names a resolvable proxy rather than a proxy to add.
func TestAnUnresolvableProxyIsNamed(t *testing.T) {
	acquirer, _ := proxied(t, "http://proxy.example:3128")
	dialed := make(chan string, 4)
	transport(acquirer).DialContext = func(_ context.Context, _, address string) (net.Conn, error) {
		dialed <- address
		return nil, &net.DNSError{Err: "no such host", Name: "proxy.example", IsNotFound: true}
	}
	_, err := acquirer.Open(context.Background(), media.Source{URL: "https://images.example/image.iso"})
	reported := message(t, err)
	if reported.Message != "the media endpoint images.example could not be reached: the name proxy.example could not be resolved by this host's resolver" ||
		reported.Remediation != "make proxy.example resolvable from this host, or name a proxy in HTTPS_PROXY that this host resolves" {
		t.Fatalf("the unresolvable proxy reported %#v", reported)
	}
	if address := <-dialed; address != "proxy.example:3128" {
		t.Fatalf("the download dialed %q, want the proxy", address)
	}
}

// An HTTPS proxy presents its own certificate before the endpoint does, so a
// refused certificate names both.
func TestAnUntrustedProxyCertificateNamesTheProxy(t *testing.T) {
	untrusted := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusMethodNotAllowed)
	}))
	untrusted.Config.ErrorLog = log.New(io.Discard, "", 0)
	untrusted.StartTLS()
	defer untrusted.Close()
	acquirer, proxy := proxied(t, untrusted.URL)
	_, err := acquirer.Open(context.Background(), media.Source{URL: "https://images.example/image.iso"})
	reported := message(t, err)
	if reported.Message != "the media endpoint images.example or the proxy "+proxy.Host+
		" presented a certificate this host's system trust store does not accept (signed by an unknown authority)" ||
		!strings.HasPrefix(reported.Remediation, "install the issuing certificate authority in this host's system trust store") {
		t.Fatalf("the untrusted proxy reported %#v", reported)
	}
}
