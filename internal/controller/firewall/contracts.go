package firewall

import "context"

type Inspector interface {
	Inspect(context.Context, Socket) (Observation, error)
}

type Sessioner interface {
	Session() Inspector
}
