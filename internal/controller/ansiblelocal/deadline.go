package ansiblelocal

import (
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// An acquisition deadline scales with the bytes a source declares, so a large
// client is never held to the bound a small one needs, and a stalled transfer
// still ends.
const (
	acquisitionBase       = 2 * time.Minute
	acquisitionRate int64 = 512 << 10
)

// nativeStagingCeiling holds native staging within the controller stage's
// 2-hour ceiling less the controller run's 10-minute base, so a run that
// stages only native packages is never refused for its size.
const nativeStagingCeiling = 110 * time.Minute

type toolAcquisition struct {
	Source  string `json:"source"`
	Seconds int64  `json:"seconds"`
}

func acquisitionDeadline(bytes int64) time.Duration {
	return acquisitionBase + time.Duration((bytes+acquisitionRate-1)/acquisitionRate)*time.Second
}

// nativeStaging bounds staging every native package of one run together: the
// acquisition deadline of their declared bytes, held to nativeStagingCeiling,
// and nothing when there is no package.
func nativeStaging(packages []prerequisites.NativePackage) time.Duration {
	if len(packages) == 0 {
		return 0
	}
	var bytes int64
	for _, item := range packages {
		bytes += item.Source.Bytes
	}
	return min(acquisitionDeadline(bytes), nativeStagingCeiling)
}
