package privilege

import (
	"context"
	"time"
)

type Executor interface {
	Run(context.Context, Command) (int, error)
}

type Delay interface {
	Wait(context.Context, time.Duration) error
}

type AccountResolver interface {
	Resolve(context.Context) (Account, error)
}
