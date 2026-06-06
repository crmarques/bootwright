package cli

import (
	"io"

	"github.com/crmarques/bootwright/internal/cli/output"
)

// statusHubCommand is the command operators run to see the suggested next step
// for the current context state.
const statusHubCommand = "bootwright status"

// printNextStatusHint routes the operator back to the status hub after a
// journey command succeeds. `bootwright status` is the single place that
// computes the suggested next command from normalized state, secret material,
// and the apply ledger, so each preparing or mutating command points there
// instead of duplicating that sequencing logic.
func printNextStatusHint(stdout io.Writer) {
	p := output.NewContinuation(stdout)
	p.Section("Next steps")
	p.List([]output.Item{{Label: statusHubCommand, Detail: "show the suggested next command"}})
}
