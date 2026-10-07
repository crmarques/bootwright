//go:build !(linux && amd64)

package adapterprotocol

import "context"

// Run starts nothing off Linux on amd64.
func Run(context.Context, Invocation, Judge) Ending {
	return Ending{Kind: NotStarted}
}
