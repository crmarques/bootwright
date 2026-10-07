package main

import (
	"context"

	"github.com/crmarques/bootwright/internal/managedos/media"
)

// shelfHeld is the shelf's one image, held for a checksum listing.
type shelfHeld struct{ digest string }

func (h shelfHeld) Digest(context.Context) (string, error) { return h.digest, nil }

func (shelfHeld) Close() error { return nil }

func (s *mediaShelf) Hold(context.Context, string) (media.Held, error) {
	return shelfHeld{digest: s.entry.SHA256}, nil
}
