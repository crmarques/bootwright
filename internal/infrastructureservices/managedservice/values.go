package managedservice

// Identity names the block, context and service one request belongs to.
type Identity struct {
	Block   string `json:"block"`
	Context string `json:"context"`
	Service string `json:"service"`
}

// Placement fixes where the effect runs. The local arm needs no address or
// credential; the SSH arm names exactly one target and account.
type Placement struct {
	Address         string `json:"address,omitempty"`
	Connection      string `json:"connection"`
	KnownHostsRef   string `json:"knownHostsRef,omitempty"`
	Machine         string `json:"machine"`
	Port            int    `json:"port,omitempty"`
	PrivateKeyRef   string `json:"privateKeyRef,omitempty"`
	SudoPasswordRef string `json:"sudoPasswordRef,omitempty"`
	User            string `json:"user,omitempty"`
}

// Egress is the placement Machine's own route for image acquisition. It comes
// from that Machine's normalized proxy choice and nothing else.
type Egress struct {
	HTTPProxy  string   `json:"httpProxy,omitempty"`
	HTTPSProxy string   `json:"httpsProxy,omitempty"`
	NoProxy    []string `json:"noProxy"`
}

// Endpoint is one named address a consumer selects this service through.
type Endpoint struct {
	Address string `json:"address"`
	Name    string `json:"name"`
}

const (
	ConnectionLocal = "local"
	ConnectionSSH   = "ssh"
)

func (p Placement) Local() bool { return p.Connection == ConnectionLocal }

// SecretReferences names every declaration a placement needs bound before the
// operation registers.
func (p Placement) SecretReferences() []string {
	var references []string
	for _, reference := range []string{p.PrivateKeyRef, p.KnownHostsRef, p.SudoPasswordRef} {
		if reference != "" {
			references = append(references, reference)
		}
	}
	return references
}
