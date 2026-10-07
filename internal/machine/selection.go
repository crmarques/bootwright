package machine

import (
	"context"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// CurrentSelection resolves the invoking user's context when none is explicit.
// Every Machine command binds the same reader, so a stale or absent marker is
// caught in one place for all of them.
type CurrentSelection func(context.Context) (string, error)

// SelectedContext fixes the context one invocation acts on, so its result
// names the exact context its answer came from rather than the selection it
// happened to read.
func SelectedContext(ctx context.Context, selection CurrentSelection, name string) (string, error) {
	if name != "" {
		return name, nil
	}
	if selection == nil {
		return "", diagnostics.NewFailure("context.state", "current context selection is not configured", "")
	}
	selected, err := selection(ctx)
	if err != nil {
		return "", err
	}
	if selected == "" {
		return "", diagnostics.NewFailureWithRemediation("context.state", "no current context is selected", "",
			"select one with bootwright context use --name <context>, or create one with bootwright context init --name <context>")
	}
	return selected, nil
}
