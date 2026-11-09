//go:build !linux || !amd64

package hostowned

func New(config Config) *Probe { return &Probe{config: config} }
