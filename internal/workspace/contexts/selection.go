package contexts

import (
	"context"

	"github.com/crmarques/bootwright/internal/secrets/secretstore"
)

// SelectionWorkspace resolves an omitted context name through the invoking
// user's selection and refuses a stale marker, so a secret command cannot
// silently act on another context. The embedded Workspace supplies the
// remaining store capabilities unchanged.
type SelectionWorkspace struct {
	secretstore.Workspace
	Selection SelectionStore
}

func (w SelectionWorkspace) SecretContext(ctx context.Context, name string) (secretstore.ContextSnapshot, error) {
	var selected Selection
	if name == "" {
		if w.Selection == nil {
			return secretstore.ContextSnapshot{}, StateError("current context selection is not configured")
		}
		var err error
		selected, err = w.Selection.Read(ctx)
		if err != nil {
			return secretstore.ContextSnapshot{}, err
		}
		if selected.Name == "" {
			return secretstore.ContextSnapshot{}, StateError("no current context; run context use --name <name>")
		}
		name = selected.Name
	}
	if w.Workspace == nil {
		return secretstore.ContextSnapshot{}, StateError("secret workspace is not configured")
	}
	snapshot, err := w.Workspace.SecretContext(ctx, name)
	if err != nil {
		return secretstore.ContextSnapshot{}, err
	}
	if selected.ID != "" && snapshot.Context.ID != selected.ID {
		return secretstore.ContextSnapshot{}, StateError("current context selection is stale; run context use --name " + name)
	}
	return snapshot, nil
}
