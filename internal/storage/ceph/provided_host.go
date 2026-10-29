package ceph

import (
	"encoding/base64"
	"encoding/binary"
	"math/big"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/storage/cephcluster"
	"github.com/crmarques/bootwright/internal/trust"
)

const FIPSKernelEnabled, FIPSPolicy = "1", "FIPS"

type HostFIPS struct {
	Kernel string `json:"kernel"`
	Policy string `json:"policy"`
}

func (f *HostFIPS) proved() bool {
	return f != nil && f.Kernel == FIPSKernelEnabled && f.Policy == FIPSPolicy
}

func bindProvidedHost(r *Request, c api.Catalog, m api.Object, node api.Value, tiebreaker, bootstrap, addressRef string) error {
	name := cephcluster.NativeHostname(node)
	if tiebreaker == "" || node.Get("name").Text() != tiebreaker || node.Get("name").Text() == bootstrap {
		return failure("lifecycle.unsupported", "Machine/"+m.Name()+" is provided, and a provided Machine joins a Ceph cluster only as its stretch tiebreaker, which runs a MON alone",
			"select a Bootwright-installed Machine for Ceph host "+name+", or make it spec.topology.stretch.tiebreaker.node")
	}
	fleet, declared := machine.FleetKey(c)
	if !declared {
		return failure("lifecycle.unsupported", "the provided tiebreaker Machine/"+m.Name()+" is dialed with the fleet SSH key, and the Environment declares none",
			"set Environment spec.remoteMachinesAccessKey.keyRef to an sshKeyPair Secret and authorize its public half for user "+r.ClusterSSHUser+" on Machine/"+m.Name())
	}
	ssh, ok := machine.SSH(m)
	if !ok || ssh.User != r.ClusterSSHUser || ssh.KnownHostsRef == "" || ssh.PrivateKeyRef == fleet || ssh.PrivateKeyRef == r.ClusterKeyRef {
		return failure("lifecycle.unsupported", "the provided tiebreaker Machine/"+m.Name()+" needs SSH access as the cluster SSH user with a pinned host key, and Ceph dials it with the fleet key, which the Machine never names",
			"declare access.ssh on Machine/"+m.Name()+" with user "+r.ClusterSSHUser+" and knownHostsRef pinning its host key, keep its auth for operator sessions only (operatorIdentity or a key of its own), and authorize the public half of Secret "+fleet+" (bootwright secret show --name "+fleet+" --part public-key) for user "+r.ClusterSSHUser+" on that host")
	}
	if len(r.Hosts) > 0 && fleet != r.Hosts[0].SSHKeyRef {
		return failure("lifecycle.unsupported", "core Ceph currently requires one fleet SSH key for its guests", "declare a shared fleet identity distinct from the native cluster identity")
	}
	if address, declared := machine.Address(m, "fqdn"); declared && address != name {
		return failure("api.invariant", "native Ceph hostname differs from the provided Machine hostname", "set topology.nodes[].fqdn to the Machine's declared fqdn")
	}
	if addressRef != "" {
		address, err := machine.ResolveAddress(m, addressRef)
		if err != nil {
			return err
		}
		if address != ssh.Address {
			return failure("lifecycle.unsupported", "separate orchestration and fleet addresses are not yet modeled", "use the same declared management address until separate host enrollment addresses are implemented")
		}
	}
	r.Hosts = append(r.Hosts, Host{Name: name, Machine: m.Name(), Address: ssh.Address, User: ssh.User, Port: ssh.Port, Devices: []Device{}, Labels: node.Get("labels").Strings(),
		Location: map[string]string{}, SSHKeyRef: fleet, KnownHostsRef: ssh.KnownHostsRef, Provided: true})
	return nil
}

func providedTransport(h Host) error {
	return failure("lifecycle.unknown", "the Ceph host preparation could not prove its transport boundary with the provided tiebreaker Machine/"+h.Machine+" at "+machine.SSHHost(h.Address, h.Port),
		"inspect the retained operation evidence; when the SSH session itself was refused, authorize the public half of the fleet key Secret "+h.SSHKeyRef+" (bootwright secret show --name "+h.SSHKeyRef+" --part public-key) for user "+h.User+" on Machine/"+h.Machine+
			", check that it offers the host key Secret "+h.KnownHostsRef+" pins and that the controller reaches it, then repeat the operation")
}

func pinnedHostKey(h Host, e lifecycle.Execution) (trust.HostKey, error) {
	value, err := materialPart(e.Material, h.KnownHostsRef, secrets.ValuePart)
	if err != nil {
		return trust.HostKey{}, err
	}
	defer clear(value)
	key, err := trust.ParseKnownHostsLine(string(value), h.Address, h.Port)
	if err != nil {
		return trust.HostKey{}, failure("lifecycle.state", "the SSH host key Secret "+h.KnownHostsRef+" of Machine/"+h.Machine+" is not one known-hosts entry for "+machine.SSHHost(h.Address, h.Port),
			"author access.ssh.knownHostsRef on Machine/"+h.Machine+" as one known-hosts entry for "+machine.SSHHost(h.Address, h.Port))
	}
	return key, nil
}

func fipsHostKey(h Host, key trust.HostKey) error {
	remedy := "pin an ECDSA or an RSA (2048 bits or more) host key of Machine/" + h.Machine + " in Secret " + h.KnownHostsRef + "; a FIPS-mode host offers no Ed25519 key"
	switch key.Type {
	case "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521":
		return nil
	case "ssh-rsa":
		if bits, ok := rsaBits(key.PublicKey); ok && bits >= 2048 {
			return nil
		}
		return failure("api.value", "FIPS Ceph refuses the RSA host key that Secret "+h.KnownHostsRef+" pins for Machine/"+h.Machine+": it is not at least 2048 bits", remedy)
	}
	return failure("api.value", "FIPS Ceph refuses the "+key.Type+" host key that Secret "+h.KnownHostsRef+" pins for Machine/"+h.Machine, remedy)
}

func rsaBits(encoded string) (int, bool) {
	blob, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return 0, false
	}
	fields := [][]byte{}
	for len(blob) > 0 && len(fields) < 3 {
		if len(blob) < 4 {
			return 0, false
		}
		size := binary.BigEndian.Uint32(blob)
		if uint64(size) > uint64(len(blob)-4) {
			return 0, false
		}
		fields, blob = append(fields, blob[4:4+size]), blob[4+size:]
	}
	if len(fields) != 3 || string(fields[0]) != "ssh-rsa" {
		return 0, false
	}
	return new(big.Int).SetBytes(fields[2]).BitLen(), true
}

func fipsNotEnabled(machineName string) error {
	host := "a provided Ceph host"
	if machineName != "" {
		host = "provided Ceph host Machine/" + machineName
	}
	return failure("api.value", host+" does not report FIPS mode: /proc/sys/crypto/fips_enabled must read exactly "+FIPSKernelEnabled+" and /etc/crypto-policies/state/current exactly "+FIPSPolicy,
		"enable FIPS mode on "+host+" (fips-mode-setup --enable at installation, then reboot), then apply again; Bootwright never switches a host's FIPS mode")
}
