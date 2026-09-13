//go:build !linux || !amd64

package nativelocal

import (
	"context"
	"errors"
	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

type MetadataReader func(context.Context, string, string, int64, prerequisites.SetupEgress) ([]byte, int64, error)

type Resolver struct{}

func New(MetadataReader) *Resolver { return &Resolver{} }
func (*Resolver) Resolve(context.Context, prerequisites.Platform, prerequisites.NativeRequirements, controller.DependencyVersions, prerequisites.SetupEgress) (prerequisites.NativeResolvedPlan, error) {
	return prerequisites.NativeResolvedPlan{}, errors.New("native dependency setup requires Linux amd64")
}
func (*Resolver) Check(context.Context, prerequisites.NativeResolvedPlan) (prerequisites.NativePresence, error) {
	return prerequisites.NativePresence{}, errors.New("native dependency inspection requires Linux amd64")
}
