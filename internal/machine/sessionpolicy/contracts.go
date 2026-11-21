package sessionpolicy

import "context"

type Store interface {
	ReadSessionPolicy(context.Context, string) ([]byte, error)
}
