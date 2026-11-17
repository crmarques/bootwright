package physicalerase

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestInstallationAndEraseKeepTheExplicitVerificationPolicy(t *testing.T) {
	m := metalOn("eno1", hardwareNIC("eno1", "52:54:00:9a:1b:01"))
	m = m.WithSpec(m.Spec().WithPath(api.BoolValue(true), "hardware", "bmc", "virtualMedia", "tls", "enableVerification"))
	request := requestOf(t, metalCatalog(m, metalNetwork(declaredInterface("eno1", "ethernet"))), m.Name())
	if !request.Target.Controller.VirtualMedia.EnableVerification {
		t.Fatal("installation lost verification setup intent")
	}
	erase := EraseRequest{Install: request}
	if !slices.ContainsFunc(eraseImpacts(erase), func(impact string) bool { return strings.HasPrefix(impact, "enable-bmc-media-verification ") }) {
		t.Fatal("erase preview lost verification setup intent")
	}
}
