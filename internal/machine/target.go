package machine

import (
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// SSHTarget is the effective SSH endpoint one Machine declares. It names the
// declarations an access path needs, never their material.
type SSHTarget struct {
	Address          string
	Port             int
	User             string
	PrivateKeyRef    string
	PasswordRef      string
	KnownHostsRef    string
	SudoPasswordRef  string
	OperatorIdentity bool
}

// Address resolves a Machine-local address reference to the value a consumer
// receives: the host IP without its prefix, or the DNS name. The second result
// reports that the reference named a declared address, which is a different
// fact from the value being usable, so a caller can tell an unresolved
// reference from an empty one.
func Address(o api.Object, reference string) (string, bool) {
	if reference == "" {
		return "", false
	}
	for _, address := range o.Spec().Get("network", "addresses").Items() {
		if address.Get("name").Text() != reference {
			continue
		}
		value := address.Get("address").Text()
		if host, _, found := strings.Cut(value, "/"); found {
			value = host
		}
		return value, true
	}
	return "", false
}

// SSH reads the effective SSH access a Machine declares. Normalization has
// already materialized the port, user and address reference, so this reports
// exactly what desired state resolved to and never guesses a default.
func SSH(o api.Object) (SSHTarget, bool) {
	ssh := o.Spec().Get("access", "ssh")
	if !ssh.Present() {
		return SSHTarget{}, false
	}
	address, ok := Address(o, ssh.Get("addressRef").Text())
	if !ok || address == "" {
		return SSHTarget{}, false
	}
	port := 22
	if value, valid := ssh.Get("port").Int64(); valid && value > 0 {
		port = int(value)
	}
	return SSHTarget{
		Address: address, Port: port, User: ssh.Get("user").Text(),
		PrivateKeyRef: ssh.Get("auth", "privateKeyRef").Text(),
		PasswordRef:   ssh.Get("auth", "passwordRef").Text(),
		KnownHostsRef: ssh.Get("knownHostsRef").Text(), SudoPasswordRef: ssh.Get("sudoPasswordRef").Text(),
		OperatorIdentity: ssh.Has("auth", "operatorIdentity"),
	}, true
}

// Installed reports a Machine whose operating system Bootwright installs.
func Installed(o api.Object) bool { return installed(o) }

// Memberships reports which selected clusters name each Machine, in ascending
// cluster order. A Machine no cluster names has no entry.
func Memberships(c api.Catalog) map[string][]string {
	memberships := map[string][]string{}
	add := func(cluster string, nodes []api.Value) {
		for _, node := range nodes {
			name := node.Get("machineRef").Text()
			if name == "" || slices.Contains(memberships[name], cluster) {
				continue
			}
			memberships[name] = append(memberships[name], cluster)
		}
	}
	for _, cluster := range c.OfKind(api.ContainerCluster) {
		add(cluster.Name(), cluster.Spec().Get("nodes").Items())
	}
	for _, cluster := range c.OfKind(api.StorageCluster) {
		add(cluster.Name(), cluster.Spec().Get("ceph", "topology", "nodes").Items())
	}
	for name := range memberships {
		slices.Sort(memberships[name])
	}
	return memberships
}
