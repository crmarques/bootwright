package access

import "github.com/crmarques/bootwright/internal/machine"

type ClusterRshRequest struct {
	ContextName string
	Name        string
	Node        string
	SSH         machine.SSHOptions
}

type ClusterExecRequest struct {
	ContextName string
	Name        string
	Node        string
	SSH         machine.SSHOptions
	Command     []string
}
