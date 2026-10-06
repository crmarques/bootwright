package lifecycle

import (
	"context"
	"slices"
	"sync"

	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// currentReads records, for each test binder, the names every ReadCurrent
// asked for, in order. The binder's own fields belong to the journey suite.
var currentReads = struct {
	sync.Mutex
	by map[*testBinder][][]string
}{by: map[*testBinder][][]string{}}

// ReadCurrent serves a copy of each named Secret's material, as the custody
// store reads a fresh copy of each current version, and binds nothing: no
// binding is issued, listed or released.
func (b *testBinder) ReadCurrent(ctx context.Context, request custody.ReadCurrentRequest) ([]secretstore.BoundMaterial, error) {
	currentReads.Lock()
	currentReads.by[b] = append(currentReads.by[b], slices.Clone(request.Names))
	currentReads.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := []secretstore.BoundMaterial{}
	for _, name := range request.Names {
		value, ok := b.material[name]
		if !ok {
			continue
		}
		parts := map[secrets.Part][]byte{}
		for _, part := range value.Parts() {
			parts[part], _ = value.Part(part)
		}
		out = append(out, secretstore.BoundMaterial{
			Version:  secretstore.Version{Declaration: secrets.VersionDeclaration{Name: name}},
			Material: secrets.NewMaterial(parts),
		})
	}
	return out, nil
}

// reads names what every ReadCurrent of this binder asked for.
func (b *testBinder) reads() [][]string {
	currentReads.Lock()
	defer currentReads.Unlock()
	return slices.Clone(currentReads.by[b])
}
