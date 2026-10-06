//go:build !linux || !amd64

package nativelocal

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

const StagingParent = "/var/lib/bootwright-staging"

type Staging struct{}

func NewStaging(string) *Staging { return &Staging{} }

func (*Staging) Stage(context.Context, string) (prerequisites.Stage, error) {
	return prerequisites.Stage{}, errors.New("dependency resolution staging requires Linux amd64")
}
