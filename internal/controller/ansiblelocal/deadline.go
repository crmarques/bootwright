package ansiblelocal

import "time"

// An acquisition deadline scales with the bytes a source declares, so a large
// client is never held to the bound a small one needs, and a stalled transfer
// still ends.
const (
	acquisitionBase       = 2 * time.Minute
	acquisitionRate int64 = 512 << 10
)

type toolAcquisition struct {
	Source  string `json:"source"`
	Seconds int64  `json:"seconds"`
}

func acquisitionDeadline(bytes int64) time.Duration {
	return acquisitionBase + time.Duration((bytes+acquisitionRate-1)/acquisitionRate)*time.Second
}
