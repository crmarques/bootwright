package acquisition

import "testing"

func TestEveryOriginIsClassedByItsHost(t *testing.T) {
	for host, want := range map[string]string{
		"registry.redhat.io": ClassVendor, "cp.icr.io": ClassVendor, "icr.io": ClassVendor, "MIRROR.OPENSHIFT.COM": ClassVendor,
		"public.dhe.ibm.com:443": ClassVendor, "quay.io": ClassPublic, "docker.io": ClassPublic, "github.com": ClassPublic,
		"get.helm.sh": ClassPublic, "registry.example.test:5000": ClassDeclared, "us.icr.io": ClassDeclared, "quay.io.example.test": ClassDeclared,
	} {
		if got := OriginClass(host); got != want {
			t.Errorf("%s is classed %s, want %s", host, got, want)
		}
	}
}
