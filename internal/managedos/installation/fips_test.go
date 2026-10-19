package installation

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math/big"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

func fipsProfile() api.Object {
	return installProfile(field("customizations", installProfile().Spec().Get("customizations").With("security", api.MapValue(
		field("fips", api.MapValue(field("enabled", api.BoolValue(true))))))))
}

func fleetSecret(source api.Value) api.Object {
	return api.NewObject(api.Secret, "bootwright-machine-key", api.Value{}, api.MapValue(text("type", "sshKeyPair"), field("source", source)))
}

func normalizedFleetSecret(t *testing.T, source api.Value) api.Object {
	t.Helper()
	normalized, err := secrets.Normalize(fleetSecret(source), api.Catalog{})
	if err != nil {
		t.Fatal(err)
	}
	return normalized
}

// rsaPublicLine is an ssh-rsa public line whose modulus has exactly bits bits,
// encoded as RFC 4253 section 6.6 writes it.
func rsaPublicLine(bits int) string {
	sshString := func(data []byte) []byte {
		return append(binary.BigEndian.AppendUint32(nil, uint32(len(data))), data...)
	}
	modulus := new(big.Int).Lsh(big.NewInt(1), uint(bits-1))
	modulus.Add(modulus, big.NewInt(1))
	blob := append(sshString([]byte("ssh-rsa")), sshString([]byte{1, 0, 1})...)
	blob = append(blob, sshString(append([]byte{0}, modulus.Bytes()...))...)
	return "ssh-rsa " + base64.StdEncoding.EncodeToString(blob) + " bootwright"
}

// fipsExecution is an apply of the FIPS lab request with the fleet key's
// public half bound as publicLine.
func fipsExecution(t *testing.T, publicLine string) lifecycle.Execution {
	t.Helper()
	request := pinnedRequest(t, labCatalog(fipsProfile()))
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	material := fleetMaterial()
	material["bootwright-machine-key"] = secrets.NewMaterial(map[secrets.Part][]byte{
		secrets.PublicKeyPart: []byte(publicLine + "\n"), secrets.PrivateKeyPart: []byte("PRIVATE"),
	})
	return lifecycle.Execution{
		Block: reconciliation.Block{
			BlockDefinition: reconciliation.BlockDefinition{ID: "os-install-rhel-01", Request: canonical},
			RequestDigest:   "digest",
		},
		Material: material,
	}
}

func TestFIPSProfileAppendsTheKernelArgumentOnce(t *testing.T) {
	request, _ := onlyRequest(t, labCatalog(fipsProfile()))
	arguments := request.InstallerArguments
	if len(arguments) == 0 || arguments[len(arguments)-1] != "fips=1" || slices.Index(arguments, "fips=1") != len(arguments)-1 {
		t.Fatalf("installer arguments = %v", arguments)
	}
	if !request.fips() {
		t.Fatal("the FIPS request does not read as FIPS")
	}
	plain, _ := onlyRequest(t, labCatalog())
	if slices.Contains(plain.InstallerArguments, "fips=1") || plain.fips() {
		t.Fatalf("a profile without FIPS carries %v", plain.InstallerArguments)
	}
}

func TestFIPSGuestAgentRepublishesTheECDSAHostKey(t *testing.T) {
	request, _ := onlyRequest(t, labCatalog(fipsProfile()))
	if request.Target.HostKeyType != "ecdsa-p256" {
		t.Fatalf("host key type = %q", request.Target.HostKeyType)
	}
	want := []string{
		"test -e /etc/ssh/ssh_host_ecdsa_key || /usr/bin/ssh-keygen -q -t ecdsa -b 256 -N '' -f /etc/ssh/ssh_host_ecdsa_key",
		"/usr/bin/ssh-keygen -A",
		"install -m 0444 /etc/ssh/ssh_host_ecdsa_key.pub /etc/bootwright/host-key.pub",
		"test -s /etc/bootwright/host-key.pub",
		`test "$(cut -d' ' -f1 /etc/bootwright/host-key.pub)" = 'ecdsa-sha2-nistp256'`,
	}
	lines := strings.Split(request.Kickstart, "\n")
	start := slices.Index(lines, want[0])
	if start < 0 || start+len(want) > len(lines) || !slices.Equal(lines[start:start+len(want)], want) {
		t.Fatalf("the kickstart does not generate and republish the ECDSA key in order:\n%s", request.Kickstart)
	}
	if strings.Contains(request.Kickstart, "ssh_host_ed25519_key.pub") {
		t.Fatal("a FIPS installation republishes the Ed25519 key")
	}
}

func TestNonFIPSGuestAgentKeepsEd25519(t *testing.T) {
	request, _ := onlyRequest(t, labCatalog())
	if request.Target.HostKeyType != "ed25519" {
		t.Fatalf("host key type = %q", request.Target.HostKeyType)
	}
	want := []string{
		"/usr/bin/ssh-keygen -A",
		"install -m 0444 /etc/ssh/ssh_host_ed25519_key.pub /etc/bootwright/host-key.pub",
		"test -s /etc/bootwright/host-key.pub",
	}
	lines := strings.Split(request.Kickstart, "\n")
	start := slices.Index(lines, want[0])
	if start < 1 || start+len(want) > len(lines) || !slices.Equal(lines[start:start+len(want)], want) || strings.HasPrefix(lines[start-1], "test -e ") {
		t.Fatalf("the kickstart does not republish the Ed25519 key as before:\n%s", request.Kickstart)
	}
	if strings.Contains(request.Kickstart, "ssh_host_ecdsa_key") {
		t.Fatal("an installation without FIPS generates an ECDSA key")
	}
}

func TestAGuestHostKeyTypeWithNoGeneratedPathRefuses(t *testing.T) {
	request, _ := onlyRequest(t, labCatalog())
	request.Target.HostKeyType = "rsa"
	install, err := installationFor(labCatalog(), guest(), installProfile(), request, &Requirements{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = RenderKickstart(install)
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Message != "the guest-agent SSH host key type rsa has no generated path" {
		t.Fatalf("refusal = %#v", reported)
	}
}

func TestFIPSRefusesAnEd25519FleetKeyBeforeRegistration(t *testing.T) {
	want := []lifecycle.Refusal{{Kind: "Machine", Name: "rhel-01",
		Reason:      "a FIPS-mode installation refuses Ed25519 keys, and the fleet key Secret/bootwright-machine-key generates one",
		Remediation: "declare spec.source.generated.keyType as ecdsa-p256, ecdsa-p384, ecdsa-p521 or rsa on Secret/bootwright-machine-key",
	}}
	generated := func(fields ...api.FieldValue) api.Value {
		return api.MapValue(field("generated", api.MapValue(fields...)))
	}
	for _, row := range []struct {
		name    string
		secret  api.Object
		refused bool
	}{
		{"raw default", fleetSecret(generated()), true},
		{"normalized default", normalizedFleetSecret(t, generated()), true},
		{"ed25519", fleetSecret(generated(text("keyType", "ed25519"))), true},
		{"ecdsa-p256", fleetSecret(generated(text("keyType", "ecdsa-p256"))), false},
		{"rsa", fleetSecret(generated(text("keyType", "rsa"))), false},
		{"context store", fleetSecret(api.MapValue(field("contextStore", api.MapValue()))), false},
	} {
		t.Run(row.name, func(t *testing.T) {
			got := Refusals(labCatalog(fipsProfile(), row.secret))
			if row.refused && !slices.Equal(got, want) {
				t.Fatalf("refusals = %+v, want %+v", got, want)
			}
			if !row.refused && len(got) != 0 {
				t.Fatalf("refusals = %+v", got)
			}
		})
	}
	if got := Refusals(labCatalog(fleetSecret(generated()))); len(got) != 0 {
		t.Fatalf("an installation without FIPS refused its Ed25519 fleet key: %+v", got)
	}
}

func TestPhysicalMachineRefusesTheFIPSArmNamingB78(t *testing.T) {
	catalog := labCatalog(metalProvider(), server(api.MapValue(text("deviceName", "/dev/sda"))),
		sshKeySecret(api.MapValue(field("generated", api.MapValue()))), fipsProfile())
	refused := Refusals(catalog)
	if identities := lifecycle.Identities(refused); !slices.Equal(identities, []string{"Machine/metal-01"}) {
		t.Fatalf("refused = %+v", refused)
	}
	if !strings.Contains(refused[0].Reason, "B78") || !strings.Contains(refused[0].Reason, "Machine/metal-01 is physical") {
		t.Fatalf("reason = %q", refused[0].Reason)
	}
}

func TestFIPSApplyRefusesABoundEd25519OrShortRSAFleetKey(t *testing.T) {
	short := rsaPublicLine(1024)
	blob := strings.Fields(short)[1]
	for _, row := range []struct {
		line, message string
	}{
		{"ssh-ed25519 AAAAPUBLIC bootwright", "Secret/bootwright-machine-key holds a fleet key of type ssh-ed25519, which a FIPS-mode installation of Machine/rhel-01 refuses; nothing was published or booted"},
		{short, "Secret/bootwright-machine-key holds an ssh-rsa fleet key of 1024 bits, and a FIPS-mode installation of Machine/rhel-01 needs at least 2048; nothing was published or booted"},
		{"ssh-rsa !!! bootwright", "Secret/bootwright-machine-key holds an ssh-rsa fleet key whose size cannot be read, so a FIPS-mode installation of Machine/rhel-01 cannot admit it; nothing was published or booted"},
	} {
		runner := &fakeRunner{}
		_, err := New(runner).Apply(context.Background(), fipsExecution(t, row.line))
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "api.value" || reported[0].Message != row.message {
			t.Fatalf("%s: refusal = %#v (%v)", row.line, reported, err)
		}
		if reported[0].Remediation != "replace Secret/bootwright-machine-key with an ECDSA key or an RSA key of at least 2048 bits, "+
			"for example a generated keyType ecdsa-p256 or rsa, or disable spec.customizations.security.fips on MachineInstallProfile/rhel-9-8" {
			t.Fatalf("remediation = %q", reported[0].Remediation)
		}
		if text := reported[0].Message + reported[0].Remediation; strings.Contains(text, "AAAAPUBLIC") || strings.Contains(text, blob) {
			t.Fatal("the refusal quotes key bytes")
		}
		if len(runner.requests) != 0 {
			t.Fatalf("%s reached the adapter", row.line)
		}
	}
	for _, line := range []string{"ecdsa-sha2-nistp256 AAAAPUBLIC bootwright", rsaPublicLine(2048)} {
		runner := &fakeRunner{}
		_, _ = New(runner).Apply(context.Background(), fipsExecution(t, line))
		if len(runner.requests) != 1 {
			t.Fatalf("%s did not reach the adapter", line)
		}
	}
}

func TestFIPSCompletionRequiresTheProvedFIPSState(t *testing.T) {
	fips := pinnedRequest(t, labCatalog(fipsProfile()))
	marker, _ := MarkerFor(fips, "digest")
	evidence := func(request Request, proved bool) []byte {
		var decoded map[string]any
		if err := json.Unmarshal(completeEvidence(request, "digest", string(marker)), &decoded); err != nil {
			t.Fatal(err)
		}
		decoded["fips"] = proved
		data, _ := json.Marshal(decoded)
		return data
	}
	err := ValidatePresence(evidence(fips, false), fips, "digest", string(marker))
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Message != "the installed machine does not report the FIPS mode its installation enabled" {
		t.Fatalf("refusal = %#v", reported)
	}
	if err := ValidatePresence(evidence(fips, true), fips, "digest", string(marker)); err != nil {
		t.Fatalf("proved FIPS refused: %v", diagnostics.Of(err))
	}
	plain := pinnedRequest(t, labCatalog())
	plainMarker, _ := MarkerFor(plain, "digest")
	if err := ValidatePresence(completeEvidence(plain, "digest", string(plainMarker)), plain, "digest", string(plainMarker)); err != nil {
		t.Fatalf("an installation without FIPS needs its proof: %v", diagnostics.Of(err))
	}
}

func TestGuestAgentHostKeyTypeEntersThePlacementPolicyCheck(t *testing.T) {
	call, _ := execution(t, "digest")
	runner := &fakeRunner{}
	_, _ = New(runner).Apply(context.Background(), call)
	if len(runner.requests) != 1 || runner.requests[0].MaterialValues["generatedHostKeyType"] != "ssh-ed25519" {
		t.Fatalf("requests = %+v", runner.requests)
	}
	excluded := diagnostics.Of(runner.requests[0].Refusals["ssh-policy-generated-excluded"])
	if len(excluded) != 1 || excluded[0].Message != "the ssh-ed25519 host key the installation of Machine/rhel-01 generates is excluded by the installation placement host SSH host key policy; nothing was published or booted" ||
		!strings.Contains(excluded[0].Remediation, "enable spec.customizations.security.fips on MachineInstallProfile/rhel-9-8") {
		t.Fatalf("refusal = %#v", excluded)
	}
	if runner.requests[0].Refusals["fips-not-enabled"] != nil {
		t.Fatal("an installation without FIPS names the FIPS refusal")
	}
	fips := &fakeRunner{}
	_, _ = New(fips).Apply(context.Background(), fipsExecution(t, "ecdsa-sha2-nistp256 AAAAPUBLIC bootwright"))
	if len(fips.requests) != 1 || fips.requests[0].MaterialValues["generatedHostKeyType"] != "ecdsa-sha2-nistp256" {
		t.Fatalf("requests = %+v", fips.requests)
	}
	_, err := New(refusingRunner{reason: "ssh-policy-generated-excluded"}).Apply(context.Background(), fipsExecution(t, "ecdsa-sha2-nistp256 AAAAPUBLIC bootwright"))
	if reported := diagnostics.Of(err); len(reported) != 1 || !strings.Contains(reported[0].Message, "the ecdsa-sha2-nistp256 host key the installation of Machine/rhel-01") ||
		reported[0].Remediation != "run from a controller whose SSH policy admits ecdsa-sha2-nistp256" {
		t.Fatalf("refusal = %#v", reported)
	}
	notEnabled := fipsExecution(t, "ecdsa-sha2-nistp256 AAAAPUBLIC bootwright")
	notEnabled.Context = "lab-b"
	_, err = New(refusingRunner{reason: "fips-not-enabled"}).Apply(context.Background(), notEnabled)
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Object == nil || reported[0].Object.Kind != "Machine" || reported[0].Object.Name != "rhel-01" ||
		reported[0].Message != "Machine/rhel-01 was installed with FIPS mode enabled, but its kernel or its crypto policy does not report FIPS mode" ||
		reported[0].Remediation != "take this context back with bootwright destroy --context lab-b, then run bootwright apply --context lab-b; FIPS mode is set only at installation" {
		t.Fatalf("refusal = %#v", reported)
	}
	delivered, request := execution(t, "digest")
	request.Target = Target{Channel: substrate.ChannelDeliveredKey, HostKeyRef: "metal-01-host-key", HostKeyType: "ed25519", Physical: true, Substrate: substrate.ArmBaremetal}
	delivered.Material["metal-01-host-key"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.PublicKeyPart: []byte("ssh-ed25519 AAAAHOST\n")})
	marker, _ := MarkerFor(request, "digest")
	physical := &fakeRunner{}
	capability := New(nil).WithIdentities(&pinReader{})
	capability.runner = physical
	if _, err := capability.run(context.Background(), delivered, "apply", request, marker, ""); err != nil {
		t.Fatalf("run: %v", diagnostics.Of(err))
	}
	if _, present := physical.requests[0].MaterialValues["generatedHostKeyType"]; present || physical.requests[0].Refusals["ssh-policy-generated-excluded"] != nil {
		t.Fatalf("a delivered-key apply carries the generated key: %+v", physical.requests[0])
	}
}

func TestAFrozenGuestHostKeyTypeThisExecutableDoesNotGenerateRefuses(t *testing.T) {
	call, request := execution(t, "digest")
	request.Target.HostKeyType = "rsa"
	marker, _ := MarkerFor(request, "digest")
	runner := &fakeRunner{}
	capability := New(nil).WithIdentities(&pinReader{})
	capability.runner = runner
	_, err := capability.run(context.Background(), call, "apply", request, marker, "")
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Message != "the frozen guest host key type of Machine/rhel-01 is not one this executable generates" || len(runner.requests) != 0 {
		t.Fatalf("refusal = %#v", reported)
	}
}
