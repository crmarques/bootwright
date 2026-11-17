package hostfirewall

import (
	"context"

	"github.com/crmarques/bootwright/internal/managedos/installationtest"
	"github.com/crmarques/bootwright/internal/managedos/installfixtures"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const testContext = installfixtures.TestContext

var (
	field          = installfixtures.Field
	text           = installfixtures.Text
	installProfile = installfixtures.InstallProfile
	guest          = installfixtures.Guest
	labCatalog     = installfixtures.LabCatalog
	realizing      = installfixtures.Realizing
	labMedia       = installationtest.LabMedia
	pinnedRequest  = installationtest.PinnedRequest
	onlyRequest    = installationtest.OnlyRequest
)

var dvdDigest = installationtest.DVDDigest

type fakeRunner struct {
	requests []lifecycle.RunRequest
	result   lifecycle.RunResult
	err      error
}

func (r *fakeRunner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.requests = append(r.requests, request)
	return r.result, r.err
}

const updateNone = "none"
