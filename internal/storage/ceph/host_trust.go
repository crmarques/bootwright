package ceph

import (
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/trust"
)

// provedHostKey uses the installed identity the original apply already proved.
// It never fetches a key from a guest or substitutes ambient known_hosts.
func provedHostKey(h Host, e lifecycle.Execution, readers DependencyProofs) (trust.HostKey, error) {
	if h.Provided {
		return pinnedHostKey(h, e)
	}
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

func proveFIPS(e lifecycle.Execution, r Request, readers DependencyProofs) error {
	if r.Security == nil || !r.Security.FIPS {
		return nil
	}
	if readers == nil {
		return failure("lifecycle.state", "Ceph installation proof readers are not bound", "")
	}
	proofs := e.ApplyProved
	if len(proofs) == 0 {
		proofs = e.Proved
	}
	for _, host := range r.Hosts {
		if host.Provided {
			if r.Cluster == "" {
				continue
			}
			if err := observedFIPS(proofs, r.Cluster, host.Machine); err != nil {
				return err
			}
			continue
		}
		if err := readers.FIPS(proofs, host.Machine, host.Address); err != nil {
			return err
		}
	}
	return nil
}

func observedFIPS(proofs []lifecycle.BlockEvidence, cluster, machineName string) error {
	_, evidence, err := preparedHostProof(proofs, cluster, machineName)
	if err != nil {
		return err
	}
	if !evidence.FIPS.proved() {
		return fipsNotEnabled(machineName)
	}
	return nil
}

func proveClusterKey(e lifecycle.Execution, policy *Security, ref string) error {
	if policy == nil || !policy.FIPS {
		return nil
	}
	public, err := materialPart(e.Material, ref, secrets.PublicKeyPart)
	if err != nil {
		return err
	}
	defer clear(public)
	key, err := trust.ParseAuthorizedKey(string(public))
	if err != nil || key.Type == "ssh-ed25519" {
		return failure("api.value", "FIPS Ceph refuses the bound cluster SSH key", "bind an ECDSA or RSA cluster SSH key")
	}
	return nil
}
