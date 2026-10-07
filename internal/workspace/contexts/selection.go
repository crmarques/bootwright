package contexts

import (
	"context"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// SelectionWorkspace resolves an omitted context name through the invoking
// user's selection, so a secret command cannot silently act on another context.
// The embedded Workspace supplies the remaining store capabilities unchanged.
type SelectionWorkspace struct {
	secretstore.Workspace
	Selection SelectionStore
}

func (w SelectionWorkspace) SecretContext(ctx context.Context, name string) (secretstore.ContextSnapshot, error) {
	if name == "" {
		if w.Selection == nil {
			return secretstore.ContextSnapshot{}, StateError("current context selection is not configured")
		}
		selected, err := w.Selection.Read(ctx)
		if err != nil {
			return secretstore.ContextSnapshot{}, err
		}
		if selected.Name == "" {
			return secretstore.ContextSnapshot{}, NoSelection()
		}
		name = selected.Name
	}
	if w.Workspace == nil {
		return secretstore.ContextSnapshot{}, StateError("secret workspace is not configured")
	}
	return w.Workspace.SecretContext(ctx, name)
}
