package cli

import (
	"io"

	"github.com/crmarques/bootwright/internal/cli/output"
)

const statusHubCommand = "bootwright status"

func printNextStatusHint(stdout io.Writer) {
	p := output.NewContinuation(stdout)
	p.Section("Next steps")
	p.List([]output.Item{{Label: statusHubCommand, Detail: "show the suggested next command"}})
}
