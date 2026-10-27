package clusterpreflight

import (
	"context"

	containerpreflight "github.com/crmarques/bootwright/internal/containercluster/preflight"
	"github.com/crmarques/bootwright/internal/machine"
	storagepreflight "github.com/crmarques/bootwright/internal/storage/preflight"
)

type PreflightRequest struct {
	ContextName     string
	Clusters        []string
	DryRun          bool
	TrustOnFirstUse bool
	SSH             machine.SSHOptions
}

type Report = storagepreflight.Report

type ContainerChecker interface {
	Check(context.Context, containerpreflight.PreflightRequest) (*containerpreflight.Report, error)
}

type StorageChecker interface {
	Check(context.Context, storagepreflight.PreflightRequest) (*storagepreflight.Report, error)
}
