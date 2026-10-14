package ceph

import (
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/trust"
)

// provedHostKey uses the installed identity the original apply already proved.
// It never fetches a key from a guest or substitutes ambient known_hosts.
func provedHostKey(h Host, e lifecycle.Execution, readers DependencyProofs) (trust.HostKey, error) {
	if h.HostKeyRef != "" {
		value, err := materialPart(e.Material, h.HostKeyRef, secrets.PublicKeyPart)
		if err != nil {
			return trust.HostKey{}, err
		}
		defer clear(value)
		key, err := trust.ParseAuthorizedKey(string(value))
		if err != nil {
			return trust.HostKey{}, failure("lifecycle.state", "the delivered Ceph host public key is invalid", "")
		}
		return key, nil
	}
	proofs := e.ApplyProved
	if len(proofs) == 0 {
		proofs = e.Proved
	}
	if readers == nil {
		return trust.HostKey{}, failure("lifecycle.state", "Ceph dependency proof readers are not bound", "")
	}
	return readers.HostKey(proofs, h.Machine, h.Address)
}
