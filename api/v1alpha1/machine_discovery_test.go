package v1alpha1

import "testing"

func TestDiscoverySSHOwnsItsPortWithoutAccessCredentials(t *testing.T) {
	substrate, _ := machineSchema().Field("substrate")
	baremetal, _ := substrate.Shape.Field("baremetal")
	root, _ := baremetal.Shape.Field("rootDevice")
	discover, _ := root.Shape.Field("discover")
	ssh, found := discover.Shape.Field("ssh")
	if !found || ssh.Required || ssh.Shape.Open || !ssh.Default.Present() {
		t.Fatal("discovery SSH must be a closed defaulted record")
	}
	port, found := ssh.Shape.Field("port")
	value, integer := port.Default.Int64()
	if !found || port.Required || port.Shape.Minimum != "1" || port.Shape.Maximum != "65535" || !integer || value != 22 {
		t.Fatalf("discovery SSH port: %+v; want bounded integer default 22", port)
	}
	if len(ssh.Shape.Fields) != 1 {
		t.Fatal("discovery SSH gained an operator credential or installed-OS access setting")
	}
}
