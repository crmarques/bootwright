//go:build !linux || !amd64

package selectionfs

import (
	"context"
	"io"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

const HelperMode = "__bootwright_selection"

func (*Store) perform(context.Context, string, contexts.Selection) (contexts.Selection, error) {
	return contexts.Selection{}, state("selection storage requires Linux amd64")
}

func ServeHelper(_ context.Context, args []string, _ io.Reader, _ io.Writer) (bool, int) {
	return len(args) > 0 && args[0] == HelperMode, 1
}
