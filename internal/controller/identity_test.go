package controller_test

import (
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
)

func TestInstalledIdentityCanonicalTuple(t *testing.T) {
	makeIdentity := func(machine string) (controller.InstalledHostIdentity, error) {
		return controller.NewInstalledHostIdentity(controller.LinuxInstalledIdentityV1, machine, "12345678-90ab-cdef-1234-567890abcdef", "98765432-10ab-cdef-1234-567890abcdef")
	}
	first, err := makeIdentity("1234567890abcdef1234567890abcdef")
	firstDigest, digestErr := first.PrivateDigest()
	if err != nil || digestErr != nil || !first.Valid() || len(firstDigest) != 64 {
		t.Fatal(first, err)
	}
	second, _ := makeIdentity("1234567890abcdef1234567890abcdef")
	secondDigest, _ := second.PrivateDigest()
	if !first.Equal(second) || firstDigest != secondDigest {
		t.Fatal("identity is not stable")
	}
	for _, invalid := range []string{"", strings.Repeat("0", 32), strings.Repeat("f", 32), "1234567890ABCDEF1234567890ABCDEF"} {
		if _, err := makeIdentity(invalid); err == nil {
			t.Fatal("invalid identity accepted")
		}
	}
}

// A host mismatch names its exits in order: the original host or its identity
// values first, the abandoning deletion last.
func TestTheHostMismatchRemedyNamesItsExitsInOrder(t *testing.T) {
	remedy := controller.HostMismatchRemedy("lab")
	order := []string{"run on the host this controller state belongs to", "machine-id, DMI product UUID and root filesystem UUID", "bootwright controller rebind",
		"otherwise bootwright context delete --name lab --purge --allow-orphans", "abandons"}
	at := -1
	for _, part := range order {
		index := strings.Index(remedy, part)
		if index <= at {
			t.Fatalf("%q does not follow the earlier exits in %q", part, remedy)
		}
		at = index
	}
	if !strings.Contains(controller.HostMismatchRemedy(""), "--name <name>") {
		t.Fatalf("a remedy for every bound context names no placeholder: %q", controller.HostMismatchRemedy(""))
	}
}
