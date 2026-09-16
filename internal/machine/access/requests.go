package access

import "github.com/crmarques/bootwright/internal/machine"

type RshRequest struct {
	ContextName string
	Name        string
	SSH         machine.SSHOptions
}

type ExecRequest struct {
	ContextName string
	Name        string
	SSH         machine.SSHOptions
	Command     []string
}
