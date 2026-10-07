package managedservice

// RequestVersion is the one frozen request shape every managed network service writes and reads.
const RequestVersion = "managed-service-v3"

// Variable names the one role's request, digest and material variables.
const Variable = "bootwright_managed_service"

// Identity names the block, context and service one request belongs to.
type Identity struct {
	Block   string `json:"block"`
	Context string `json:"context"`
	Service string `json:"service"`
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
