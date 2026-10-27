package installation

import (
	"encoding/base64"
	"encoding/binary"
	"math/big"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/substrate"
)

// The placement-host adapter names only these bounded reasons. Public
// diagnostics come from the actual bound key types, never native output.
func keyPolicyRefusals(request Request, values map[string]string) map[string]error {
	remedy := "choose keys compatible with the qualified placement host SSH policy before planning this installation again"
	result := map[string]error{
		"ssh-policy-unavailable":    refusal("access.unavailable", "the installation placement host SSH crypto policy could not be evaluated before publication or boot", "restore a supported crypto-only SSH policy and the qualified SSH client on the installation placement host"),
		"ssh-policy-fleet-excluded": refusal("access.unavailable", "Secret/"+request.FleetKeyRef+" key type "+values["fleetKeyType"]+" is excluded by the installation placement host SSH fleet key policy; nothing was published or booted", remedy),
	}
	if request.Target.HostKeyRef != "" {
		result["ssh-policy-host-excluded"] = refusal("access.unavailable", "Secret/"+request.Target.HostKeyRef+" key type "+values["hostKeyType"]+" is excluded by the installation placement host SSH host key policy; nothing was published or booted", remedy)
	}
	if request.Target.Channel == substrate.ChannelGuestAgent {
		generated := values["generatedHostKeyType"]
		generatedRemedy := "run from a controller whose SSH policy admits " + generated
		if generated == "ssh-ed25519" {
			generatedRemedy = "enable spec.customizations.security.fips on MachineInstallProfile/" + request.Identity.Profile +
				" so the installation generates an ecdsa-sha2-nistp256 host key, or run from a controller whose SSH policy admits ssh-ed25519"
		}
		result["ssh-policy-generated-excluded"] = refusal("access.unavailable", "the "+generated+" host key the installation of Machine/"+request.Identity.Object+
			" generates is excluded by the installation placement host SSH host key policy; nothing was published or booted", generatedRemedy)
	}
	return result
}

func generatedHostKeyType(contextName string, request Request) (string, error) {
	if _, known := guestHostKeys[request.Target.HostKeyType]; known {
		return deliveredHostKeys[request.Target.HostKeyType].algorithm, nil
	}
	return "", refusal("lifecycle.state", "the frozen guest host key type of Machine/"+request.Identity.Object+" is not one this executable generates",
		"take this context back with bootwright destroy --context "+contextName+", then plan and apply it again")
}

// fipsFleetKey refuses a bound fleet key a FIPS-mode machine would reject, read
// from its public line alone, before anything is published or booted.
func fipsFleetKey(contextName string, request Request, publicLine string) error {
	return fipsKey(contextName, request, publicLine, request.FleetKeyRef, "fleet key")
}

func fipsKey(contextName string, request Request, publicLine, reference, purpose string) error {
	fields := strings.Fields(publicLine)
	machine, secret := "Machine/"+request.Identity.Object, "Secret/"+reference
	remedy := "take this context back with bootwright destroy --context " + contextName + ", then replace " + secret +
		" with an ECDSA key or an RSA key of at least 2048 bits, for example a generated keyType ecdsaP256 or rsa, or disable spec.customizations.security.fips on MachineInstallProfile/" +
		request.Identity.Profile + ", and run bootwright apply --context " + contextName + "; an apply that continues reads the key version this context bound"
	switch fields[0] {
	case "ecdsa-sha2-nistp256", "ecdsa-sha2-nistp384", "ecdsa-sha2-nistp521":
		return nil
	case "ssh-rsa":
	default:
		return refusal("api.value", secret+" holds a "+purpose+" of type "+fields[0]+", which a FIPS-mode installation of "+machine+" refuses; nothing was published or booted", remedy)
	}
	bits, ok := 0, len(fields) > 1
	if ok {
		bits, ok = rsaModulusBits(fields[1])
	}
	if !ok {
		return refusal("api.value", secret+" holds an ssh-rsa "+purpose+" whose size cannot be read, so a FIPS-mode installation of "+machine+" cannot admit it; nothing was published or booted", remedy)
	}
	if bits < 2048 {
		return refusal("api.value", secret+" holds an ssh-rsa "+purpose+" of "+strconv.Itoa(bits)+" bits, and a FIPS-mode installation of "+machine+" needs at least 2048; nothing was published or booted", remedy)
	}
	return nil
}

// rsaModulusBits reads the modulus size of an ssh-rsa public blob: the
// algorithm name, the exponent and the modulus, each length-prefixed.
func rsaModulusBits(encoded string) (int, bool) {
	blob, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return 0, false
	}
	var parts [][]byte
	for len(parts) < 3 {
		if len(blob) < 4 || uint64(binary.BigEndian.Uint32(blob)) > uint64(len(blob)-4) {
			return 0, false
		}
		size := int(binary.BigEndian.Uint32(blob))
		parts, blob = append(parts, blob[4:4+size]), blob[4+size:]
	}
	if string(parts[0]) != "ssh-rsa" || len(blob) != 0 {
		return 0, false
	}
	return new(big.Int).SetBytes(parts[2]).BitLen(), true
}

func fipsNotEnabled(contextName string, request Request) error {
	return &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{
		Severity: "error", Code: "lifecycle.state",
		Message:     "Machine/" + request.Identity.Object + " was installed with FIPS mode enabled, but its kernel or its crypto policy does not report FIPS mode",
		Remediation: "take this context back with bootwright destroy --context " + contextName + ", then run bootwright apply --context " + contextName + "; FIPS mode is set only at installation",
		Object:      &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(api.Machine), Name: request.Identity.Object},
	}}}
}
