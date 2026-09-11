package bundlelocal

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

var errToolMetadataNotFound = errors.New("target tool publisher metadata was not found")

func safeToolURL(raw string) bool {
	if len(raw) > 4096 || strings.ContainsAny(raw, "\x00\r\n\t ") {
		return false
	}
	value, err := url.Parse(raw)
	return err == nil && value.Scheme == "https" && value.Hostname() != "" && value.User == nil && value.RawQuery == "" && value.Fragment == "" && value.Opaque == "" && value.RawPath == "" && (value.Port() == "" || value.Port() == "443")
}

func toolMetadataOrigin(value *url.URL) bool {
	if value == nil || value.Scheme != "https" || value.User != nil || value.Fragment != "" || value.Opaque != "" || value.Port() != "" && value.Port() != "443" {
		return false
	}
	switch value.Hostname() {
	case "api.github.com", "github.com", "release-assets.githubusercontent.com", "objects.githubusercontent.com", "get.helm.sh", "mirror.openshift.com", "dl.k8s.io", "cdn.dl.k8s.io":
		return true
	}
	return false
}

func fetchToolMetadata(ctx context.Context, method, endpoint string, maximum int64, egress prerequisites.SetupEgress) (toolMetadata, error) {
	if method != http.MethodGet && method != http.MethodHead || maximum < 0 || maximum > maxToolMetadataBytes || !safeToolURL(endpoint) {
		return toolMetadata{}, bundleFailure("target tool metadata request is outside its read-only boundary")
	}
	parsed, _ := url.Parse(endpoint)
	if !toolMetadataOrigin(parsed) {
		return toolMetadata{}, bundleFailure("target tool metadata source is not an approved publisher")
	}
	client, closeIdle, err := acquisitionClient(ctx, egress, toolMetadataOrigin, 45*time.Second)
	if err != nil {
		return toolMetadata{}, err
	}
	defer closeIdle()
	request, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return toolMetadata{}, bundleFailure("target tool metadata request is invalid")
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("Cache-Control", "no-cache")
	request.Header.Set("User-Agent", "Bootwright-Controller/1")
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return toolMetadata{}, ctx.Err()
		}
		return toolMetadata{}, bundleFailure("target tool publisher metadata could not be acquired")
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return toolMetadata{}, errors.Join(errToolMetadataNotFound, bundleFailure("target tool publisher did not publish the requested release metadata"))
	}
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Encoding") != "" {
		return toolMetadata{}, bundleFailure("target tool metadata returned an unapproved status or representation")
	}
	if method == http.MethodHead {
		if response.ContentLength <= 0 || response.ContentLength > maxToolSourceBytes {
			return toolMetadata{}, bundleFailure("target tool publisher did not establish a bounded asset size")
		}
		return toolMetadata{size: response.ContentLength}, nil
	}
	if response.ContentLength > maximum {
		return toolMetadata{}, bundleFailure("target tool metadata exceeds its read bound")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, maximum+1))
	if err != nil || int64(len(data)) > maximum {
		if ctx.Err() != nil {
			return toolMetadata{}, ctx.Err()
		}
		return toolMetadata{}, bundleFailure("target tool metadata exceeds its read bound or is incomplete")
	}
	return toolMetadata{data: data, size: response.ContentLength}, nil
}
