package bundlelocal

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// FetchMetadata supplies the maintained native solver with bounded publisher
// metadata. The consumer must validate its narrower repository paths and the
// signed metadata's exact size and digest before passing bytes to its solver.
func FetchMetadata(ctx context.Context, method, endpoint string, limit int64, egress prerequisites.SetupEgress) ([]byte, int64, error) {
	origin, err := url.Parse(endpoint)
	approved := func(value *url.URL) bool {
		return approvedOrigin(value) || bootstrapOrigin(value) || toolMetadataOrigin(value)
	}
	if err != nil || !approved(origin) || method != http.MethodGet && method != http.MethodHead || limit <= 0 || limit > 64<<20 {
		return nil, 0, bundleFailure("publisher metadata request is outside its bounded HTTPS contract")
	}
	client, closeIdle, err := acquisitionClient(ctx, egress, approved, 2*time.Minute)
	if err != nil {
		return nil, 0, err
	}
	defer closeIdle()
	request, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("Cache-Control", "no-cache")
	response, err := client.Do(request)
	if err != nil {
		return nil, 0, bundleFailure("publisher metadata acquisition failed")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Header.Get("Content-Encoding") != "" || response.ContentLength > limit {
		return nil, 0, bundleFailure("publisher metadata response violates its bounded representation")
	}
	if method == http.MethodHead {
		if response.ContentLength <= 0 {
			return nil, 0, bundleFailure("publisher did not establish a positive metadata size")
		}
		return nil, response.ContentLength, nil
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		return nil, 0, bundleFailure("publisher metadata response is incomplete or too large")
	}
	return data, int64(len(data)), nil
}
