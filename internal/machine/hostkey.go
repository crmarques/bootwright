package machine

import "github.com/crmarques/bootwright/internal/trust"

// HostKeyEvidence is the server identity one context's own installation
// proved: the address it answered on and the key it presents there. It is a
// proof this context already holds, so a session pins it without consulting
// any trust record and without trusting anything on first sight.
type HostKeyEvidence struct {
	Address string
	HostKey trust.HostKey
}

func (e HostKeyEvidence) Present() bool { return e.Address != "" && e.HostKey.Present() }
