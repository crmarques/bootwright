//go:build !linux || !amd64

package stagelocal

import (
	"context"
	"errors"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

type AutomationStager struct{}

func NewAutomationStager(string) *AutomationStager { return &AutomationStager{} }

func (*AutomationStager) Stage(context.Context) (prerequisites.StagedAutomation, error) {
	return prerequisites.StagedAutomation{}, errors.New("automation staging requires Linux amd64")
}
