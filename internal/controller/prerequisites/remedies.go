package prerequisites

import (
	"errors"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// ScopedFailure is a definite dependency failure raised by an adapter that
// setup and a context's controller stage share. The adapter knows the
// correction; the command that settles it, and whether an unreachable
// publisher may be routed around, belong to the scope that met it, because
// setup reads HTTPS_PROXY and NO_PROXY on every run while a context's stage
// froze its controller Machine's proxy choice with its block. Read without a
// scope, it is setup's.
type ScopedFailure struct {
	Message    string
	Correction string
	// Routable marks a publisher this host could not reach or resolve, which
	// a scope that selects its route on every run can settle over another.
	Routable bool
}

func (f *ScopedFailure) Error() string { return "failed with diagnostics" }

func (f *ScopedFailure) Unwrap() error {
	return f.remedied("set HTTPS_PROXY to a proxy that reaches it and keep it out of NO_PROXY", "rerun bootwright setup")
}

// InStage renders a scoped failure as the named context's controller stage
// meets it: that stage settles it, by the correction alone. The stage acquires
// over the proxy choice its block froze, and the failed apply holding that
// block continues only over its exact input, which no context update may
// change meanwhile, so another route is no remedy its command can settle. Any
// other error is returned unchanged.
func InStage(err error, name string) error {
	var scoped *ScopedFailure
	if !errors.As(err, &scoped) {
		return err
	}
	return scoped.remedied("", stageCommand(name))
}

// remedied is the correction, then the route a routable failure is offered
// when the scope has one, then the command that settles it.
func (f *ScopedFailure) remedied(route, command string) error {
	remedy := f.Correction
	if f.Routable && route != "" {
		remedy += ", or " + route
	}
	return diagnostics.NewFailureWithRemediation("controller.setup", f.Message, "", remedy+", then "+command+".")
}
