package bundlelocal

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/acquisition"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

type MirrorFile struct {
	URL   string
	Bytes int64
}

type MirrorReading struct {
	File    MirrorFile
	Status  int
	Served  int64
	Failure string
}

const (
	maxMirrorFiles      = 128
	mirrorProbeTimeout  = 30 * time.Second
	mirrorProbeDeadline = 2 * time.Minute
)

func ProbeMirror(ctx context.Context, files []MirrorFile, egress prerequisites.SetupEgress, anchor []byte) ([]MirrorReading, error) {
	if len(files) == 0 {
		return nil, nil
	}
	origin, err := url.Parse(files[0].URL)
	if err != nil || len(files) > maxMirrorFiles {
		return nil, bundleFailure("the artifact mirror probe is outside its bounds")
	}
	for _, file := range files {
		parsed, err := url.Parse(file.URL)
		if !safeToolURL(file.URL) || err != nil || parsed.Hostname() != origin.Hostname() || file.Bytes <= 0 || file.Bytes > maxToolSourceBytes {
			return nil, bundleFailure("the artifact mirror probe is outside its bounds")
		}
	}
	proxy, err := explicitProxy(egress)
	if err != nil {
		return nil, err
	}
	roots, err := qualifiedSystemRoots(ctx)
	if err != nil {
		return nil, err
	}
	var anchored *trustAnchor
	if len(anchor) != 0 {
		anchored = &trustAnchor{authority: originAuthority(origin), pool: roots.Clone()}
		if !anchored.pool.AppendCertsFromPEM(anchor) {
			return nil, bundleFailure("the artifact mirror's trust anchor holds no certificate")
		}
	}
	client, closeIdle, err := routedClient(proxy, roots, sameOrigin(origin), mirrorProbeTimeout, anchored)
	if err != nil {
		return nil, err
	}
	defer closeIdle()
	return readMirror(ctx, client, origin, files), nil
}

func originAuthority(value *url.URL) string {
	port := value.Port()
	if port == "" {
		port = "443"
	}
	host := strings.ToLower(value.Hostname())
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return host + ":" + port
}

func sameOrigin(origin *url.URL) func(*url.URL) bool {
	return func(candidate *url.URL) bool {
		return candidate != nil && candidate.Scheme == "https" && candidate.User == nil && candidate.Opaque == "" && originAuthority(candidate) == originAuthority(origin)
	}
}

func readMirror(ctx context.Context, client *http.Client, origin *url.URL, files []MirrorFile) []MirrorReading {
	bounded, cancel := context.WithTimeout(ctx, mirrorProbeDeadline)
	defer cancel()
	bounded = acquisition.PrivateAcquisitionEndpoint(bounded, origin)
	readings := make([]MirrorReading, 0, len(files))
	for _, file := range files {
		readings = append(readings, readSize(bounded, client, file))
	}
	return readings
}

func readSize(ctx context.Context, client *http.Client, file MirrorFile) MirrorReading {
	reading := MirrorReading{File: file, Served: -1}
	response, err := sizeRequest(ctx, client, http.MethodHead, file.URL)
	if err == nil && (response.StatusCode == http.StatusMethodNotAllowed || response.StatusCode == http.StatusNotImplemented) {
		response.Body.Close()
		response, err = sizeRequest(ctx, client, http.MethodGet, file.URL)
	}
	if err != nil {
		reading.Failure = mirrorFailure(file.URL, err)
		return reading
	}
	defer response.Body.Close()
	reading.Status, reading.Served = response.StatusCode, servedSize(response)
	return reading
}

func sizeRequest(ctx context.Context, client *http.Client, method, endpoint string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("User-Agent", "Bootwright-Controller/1")
	if method == http.MethodGet {
		request.Header.Set("Range", "bytes=0-0")
	}
	return client.Do(request)
}

func servedSize(response *http.Response) int64 {
	if response.Header.Get("Content-Encoding") != "" {
		return -1
	}
	if response.StatusCode != http.StatusPartialContent {
		return response.ContentLength
	}
	_, total, found := strings.Cut(response.Header.Get("Content-Range"), "/")
	size, err := strconv.ParseInt(total, 10, 64)
	if !found || err != nil || size <= 0 {
		return -1
	}
	return size
}

func mirrorFailure(endpoint string, err error) string {
	var scoped *prerequisites.ScopedFailure
	var verification *tls.CertificateVerificationError
	host := endpoint
	if parsed, parseErr := url.Parse(endpoint); parseErr == nil {
		host = parsed.Hostname()
	}
	switch {
	case errors.As(err, &scoped):
		return "artifact mirror " + host + " refused: " + scoped.Message
	case errors.As(err, &verification), errors.As(err, new(x509.UnknownAuthorityError)), errors.As(err, new(x509.HostnameError)), errors.As(err, new(x509.CertificateInvalidError)):
		return "artifact mirror " + host + " presented a certificate neither the system trust store nor the mirror's trust anchor accepts"
	}
	return acquisition.TransportFailure("artifact mirror", endpoint, err).Message()
}
