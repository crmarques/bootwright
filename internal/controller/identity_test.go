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
