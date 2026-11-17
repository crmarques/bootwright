package cli

import (
	"context"

	"github.com/spf13/cobra"
)

type renderCall struct {
	ctx     context.Context
	command *cobra.Command
	path    string
	result  commandResult
}

type resultWriter func(*Runner, renderCall) (bool, error)

type shownResult func(commandResult) (shown jsonResult, reported []diagnostic, ok bool)

type commandRenderer struct {
	structured resultWriter
	human      resultWriter
	failed     func(*Runner, renderCall, []diagnostic) (bool, error)
	outcome    func(*Runner, renderCall, error) (bool, int)
}

func structuredShown(build shownResult) resultWriter {
	return func(r *Runner, c renderCall) (bool, error) {
		shown, reported, ok := build(c.result)
		if !ok {
			return false, nil
		}
		return true, writeStructuredResult(r.config.Out, c.path, shown, reported, nil, 0)
	}
}

func (r *Runner) writeResult(ctx context.Context, command *cobra.Command, path string, result commandResult) (bool, error) {
	render := specFor(path).render
	call := renderCall{ctx: ctx, command: command, path: path, result: result}
	if selectedStructured(command) && render.structured != nil {
		if handled, err := render.structured(r, call); handled {
			return true, err
		}
	}
	if render.human != nil {
		return render.human(r, call)
	}
	return false, nil
}
