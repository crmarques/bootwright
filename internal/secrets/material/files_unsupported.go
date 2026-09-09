//go:build !linux || !amd64

package material

import (
	"context"

	"github.com/crmarques/bootwright/internal/secrets"
)

func (s *Service) readFileParts(ctx context.Context, requests []fileRequest, _ string, relativeToOrigin bool) (map[secrets.Part][]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(requests) == 0 {
		return map[secrets.Part][]byte{}, nil
	}
	code := "input"
	if relativeToOrigin {
		code = "source"
	}
	return nil, failure(code, "secret file acquisition requires Linux on amd64", "")
}
