package venvlocal

import (
	"errors"

	"github.com/crmarques/bootwright/internal/acquisition"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

type wheelAcquisitionFailure struct {
	classified *acquisition.Failure
	scoped     *prerequisites.ScopedFailure
}

func (e *wheelAcquisitionFailure) Error() string   { return e.scoped.Error() }
func (e *wheelAcquisitionFailure) Unwrap() []error { return []error{e.classified, e.scoped} }

func wheelTransportFailure(subject, endpoint string, err error) error {
	var scoped *prerequisites.ScopedFailure
	if errors.As(err, &scoped) {
		return err
	}
	failure := acquisition.TransportFailure(subject, endpoint, err)
	return &wheelAcquisitionFailure{classified: failure, scoped: &prerequisites.ScopedFailure{
		Message: failure.Message(), Correction: failure.Correction(), Routable: failure.Routable(),
	}}
}
