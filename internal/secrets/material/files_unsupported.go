//go:build !linux || !amd64

package material

import (
	"context"

	"github.com/crmarques/bootwright/internal/secrets"
)

func (s *Service) readFileParts(ctx context.Context, requests []fileRequest) (map[secrets.Part][]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(requests) == 0 {
		return map[secrets.Part][]byte{}, nil
	}
	return nil, failure("input", "secret file acquisition requires Linux on amd64", "")
}
