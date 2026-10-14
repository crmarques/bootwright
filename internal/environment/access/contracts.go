package access

import (
	"context"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
)

type CurrentSelection func(context.Context) (string, error)

type Graph interface {
	Effective(ctx context.Context, contextName string) (api.Catalog, error)
}

type Sessions interface {
	Rsh(ctx context.Context, contextName, machineName string, ssh machine.SSHOptions) (*machine.SessionResult, error)
	Exec(ctx context.Context, contextName, machineName string, ssh machine.SSHOptions, command []string) (*machine.SessionResult, error)
}
