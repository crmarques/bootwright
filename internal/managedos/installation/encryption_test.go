package installation

import (
	"context"
	"encoding/json"
	"maps"
	"slices"
	"strconv"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

func encryptionSecurity(fips bool, pcrIDs ...int) api.Value {
	var tpm []api.FieldValue
	if len(pcrIDs) != 0 {
		ids := make([]api.Value, 0, len(pcrIDs))
		for _, id := range pcrIDs {
			ids = append(ids, api.IntegerValue(strconv.Itoa(id)))
		}
		tpm = append(tpm, field("pcrIds", api.ListValue(ids...)), text("pcrBank", "sha256"))
	}
	security := []api.FieldValue{
		field("selinux", api.MapValue(text("mode", "enforcing"))),
		field("diskEncryption", api.MapValue(
			field("unlock", api.MapValue(field("tpm2", api.MapValue(tpm...)))),
			text("recoveryPassphraseRef", "rhel-luks-recovery"),
		)),
	}
	if fips {
		security = append(security, field("fips", api.MapValue(field("enabled", api.BoolValue(true)))))
	}
	return api.MapValue(security...)
}

func encryptingProfile(pcrIDs ...int) api.Object {
	return installProfile(field("customizations", installProfile().Spec().Get("customizations").With("security", encryptionSecurity(false, pcrIDs...))))
}

func encryptingFIPSProfile(pcrIDs ...int) api.Object {
	return installProfile(field("customizations", installProfile().Spec().Get("customizations").With("security", encryptionSecurity(true, pcrIDs...))))
}

func encryptionExecution(t *testing.T, request Request, recovery []byte) lifecycle.Execution {
	t.Helper()
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	material := fleetMaterial()
	material["lab-artifacts-tls"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: []byte("CERTIFICATE")})
	if recovery != nil {
		material["rhel-luks-recovery"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: recovery})
	}
	return lifecycle.Execution{
		Block: reconciliation.Block{
			BlockDefinition: reconciliation.BlockDefinition{ID: "os-install-rhel-01", Request: canonical},
			RequestDigest:   "digest",
		},
		Context:  testContext,
		Material: material,
	}
}

func kickstartLines(request Request) []string { return strings.Split(request.Kickstart, "\n") }

func lineIndex(t *testing.T, lines []string, line string) int {
	t.Helper()
	index := slices.Index(lines, line)
	if index < 0 {
		t.Fatalf("the kickstart has no line %q:\n%s", line, strings.Join(lines, "\n"))
	}
	return index
}

func TestDiskEncryptionPublishesPrivatelyOnTheGuestAgentChannel(t *testing.T) {
	request, _ := onlyRequest(t, labCatalog(encryptingProfile()))
	if request.Private == nil || request.Image != nil || request.TLSCertificateRef != "lab-artifacts-tls" {
		t.Fatalf("private = %+v, image = %+v, certificate = %q", request.Private, request.Image, request.TLSCertificateRef)
	}
	if request.Target.Channel != substrate.ChannelGuestAgent || request.Target.HostKeyRef != "" || request.Target.HostKeyType != "ed25519" {
		t.Fatalf("target = %+v", request.Target)
	}
	if request.DiskEncryption == nil || request.DiskEncryption.RecoveryPassphraseRef != "rhel-luks-recovery" {
		t.Fatalf("disk encryption = %+v", request.DiskEncryption)
	}
	if !slices.Contains(request.SecretReferences(), "rhel-luks-recovery") {
		t.Fatalf("secret references = %v", request.SecretReferences())
	}
	plain, _ := onlyRequest(t, labCatalog())
	if plain.Private != nil || plain.Image == nil || plain.DiskEncryption != nil || plain.TLSCertificateRef != "" {
		t.Fatalf("a plain lab request is not public: %+v", plain)
	}
}

func TestDiskEncryptionKickstartIncludesTheStorageFileThePreScriptWrites(t *testing.T) {
	request, _ := onlyRequest(t, labCatalog(encryptingProfile()))
	lines := kickstartLines(request)
	ignore := lineIndex(t, lines, "ignoredisk --only-use=vda")
	if lines[ignore+1] != "clearpart --all --initlabel --drives=vda" || lines[ignore+2] != "%include /tmp/bootwright-storage.ks" {
		t.Fatalf("storage lines = %q", lines[ignore:ignore+3])
	}
	if slices.Contains(lines, "autopart --type=lvm") {
		t.Fatal("an encrypting kickstart keeps the unencrypted autopart")
	}
	if strings.Count(request.Kickstart, "--passphrase=") != 1 {
		t.Fatalf("--passphrase= appears %d times", strings.Count(request.Kickstart, "--passphrase="))
	}
	printf := `printf '%s%s\n' 'autopart --type=lvm --encrypted --luks-version=luks2 --passphrase=' "${bootwright_recovery}" > /tmp/bootwright-storage.ks`
	lineIndex(t, lines, printf)
	fetch := "bootwright_recovery=$(curl --fail --silent --show-error --cacert /tmp/bootwright-artifact-ca.pem '@@BOOTWRIGHT_PRIVATE_URL@@/recovery')"
	if strings.Count(request.Kickstart, fetch) != 2 {
		t.Fatalf("the recovery passphrase is not fetched once by %%pre and once by %%post:\n%s", request.Kickstart)
	}
	packages := lineIndex(t, lines, "%packages --excludedocs --exclude-weakdeps")
	for _, name := range []string{"clevis-dracut", "clevis-luks", "clevis-systemd"} {
		if index := slices.Index(lines, name); index < packages {
			t.Fatalf("%%packages does not list %s", name)
		}
	}
	plain, _ := onlyRequest(t, labCatalog())
	requireLine(t, plain.Kickstart, "autopart --type=lvm")
	for _, absent := range []string{"%include", "clevis", "tpmrm0", "--passphrase"} {
		if strings.Contains(plain.Kickstart, absent) {
			t.Fatalf("a plain kickstart names %q", absent)
		}
	}
}

func TestDiskEncryptionPreScriptProvesATPMBeforeAnyDiskIsTouched(t *testing.T) {
	request, _ := onlyRequest(t, labCatalog(encryptingProfile()))
	lines := kickstartLines(request)
	start := lineIndex(t, lines, "%pre --erroronfail --interpreter=/bin/bash")
	if !slices.Equal(lines[start+1:start+4], []string{"set -euo pipefail", "umask 077", "export LC_ALL=C"}) {
		t.Fatalf("the %%pre opens with %q", lines[start+1:start+4])
	}
	end := start + slices.Index(lines[start:], "%end")
	pre := lines[start : end+1]
	order := []string{
		"if [ ! -c /dev/tpmrm0 ]; then",
		"cat > /tmp/bootwright-artifact-ca.pem <<'BOOTWRIGHT_ARTIFACT_CA_EOF'",
		"bootwright_recovery=$(curl --fail --silent --show-error --cacert /tmp/bootwright-artifact-ca.pem '@@BOOTWRIGHT_PRIVATE_URL@@/recovery')",
		"bootwright_class='^[]!$%&(-[^-~]+$'",
		"if [ ${#bootwright_recovery} -lt 8 ] || [ ${#bootwright_recovery} -gt 256 ] || [[ ! ${bootwright_recovery} =~ ${bootwright_class} ]]; then",
		`printf '%s%s\n' 'autopart --type=lvm --encrypted --luks-version=luks2 --passphrase=' "${bootwright_recovery}" > /tmp/bootwright-storage.ks`,
		"unset bootwright_recovery",
	}
	last := -1
	for _, line := range order {
		index := slices.Index(pre, line)
		if index <= last {
			t.Fatalf("%q is missing or out of order in:\n%s", line, strings.Join(pre, "\n"))
		}
		last = index
	}
	if strings.Contains(request.Kickstart, "set -x") {
		t.Fatal("the kickstart traces its commands")
	}
	for _, line := range pre {
		if strings.Contains(line, "${bootwright_recovery}") && !strings.HasPrefix(line, "if [ ${#bootwright_recovery}") && !strings.HasPrefix(line, "printf '%s%s") {
			t.Fatalf("the %%pre reads the passphrase in %q", line)
		}
	}
	if include := lineIndex(t, lines, "%include /tmp/bootwright-storage.ks"); include < end {
		t.Fatal("the storage file is included before the pre-script that writes it")
	}
}

func TestFIPSDiskEncryptionPinsPBKDF2(t *testing.T) {
	fips, _ := onlyRequest(t, labCatalog(encryptingFIPSProfile()))
	requireLine(t, fips.Kickstart, `printf '%s%s\n' 'autopart --type=lvm --encrypted --luks-version=luks2 --pbkdf=pbkdf2 --passphrase=' "${bootwright_recovery}" > /tmp/bootwright-storage.ks`)
	plain, _ := onlyRequest(t, labCatalog(encryptingProfile()))
	if strings.Contains(plain.Kickstart, "--pbkdf") {
		t.Fatal("an encryption without FIPS pins a key derivation")
	}
}

func TestDiskEncryptionPostBindsTPM2WithTheNativePCRConfig(t *testing.T) {
	const pass = `test "$(clevis luks pass -d "${bootwright_device}" -s "${bootwright_slot}")" = "${bootwright_recovery}"`
	for _, row := range []struct {
		ids    []int
		config string
		pass   bool
	}{
		{[]int{8, 7, 8}, `{"pcr_bank":"sha256","pcr_ids":"7,8"}`, false},
		{nil, `{}`, true},
	} {
		request, _ := onlyRequest(t, labCatalog(encryptingProfile(row.ids...)))
		lines := kickstartLines(request)
		bind := `printf '%s' "${bootwright_recovery}" | clevis luks bind -y -d "${bootwright_device}" -k - tpm2 '` + row.config + `'`
		bound := lineIndex(t, lines, bind)
		if slices.Contains(lines, pass) != row.pass {
			t.Fatalf("PCRs %v: the clevis luks pass proof is present = %t", row.ids, !row.pass)
		}
		post := slices.Index(lines, "%post --erroronfail --interpreter=/bin/bash")
		marker := lineIndex(t, lines, "%post --erroronfail")
		if post < 0 || post > bound || marker < bound {
			t.Fatalf("PCRs %v: the encryption %%post does not precede the marker %%post", row.ids)
		}
		end := bound + slices.Index(lines[bound:], "%end")
		if lines[end-1] != "/usr/bin/dracut -f --regenerate-all" {
			t.Fatalf("PCRs %v: the encryption %%post ends with %q", row.ids, lines[end-1])
		}
		for _, line := range lines {
			if strings.Contains(line, "bootwright_recovery}\"") && strings.Contains(line, ">") && !strings.HasSuffix(line, "> /tmp/bootwright-storage.ks") {
				t.Fatalf("PCRs %v: %q writes the passphrase to a file", row.ids, line)
			}
		}
	}
}

func TestRecoveryPassphraseOutsideTheTokenClassRefusesBeforeEffects(t *testing.T) {
	request := pinnedRequest(t, labCatalog(encryptingProfile()))
	const sentinel = "SENTINELxyz"
	for name, value := range map[string]string{
		"space": sentinel + " a", "double quote": sentinel + `"`, "single quote": sentinel + "'", "backslash": sentinel + `\`,
		"hash": sentinel + "#", "seven bytes": "SENTIN7", "257 bytes": sentinel + strings.Repeat("a", 257-len(sentinel)),
		"trailing newline": sentinel + "\n", "delete": sentinel + "\x7f", "non-ASCII": sentinel + "é",
	} {
		runner := &fakeRunner{}
		_, err := New(runner).Apply(context.Background(), encryptionExecution(t, request, []byte(value)))
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "api.value" || !strings.HasPrefix(reported[0].Message, "Secret/rhel-luks-recovery holds a recovery passphrase that is not 8 to 256 printable ASCII characters") ||
			reported[0].Remediation != "replace Secret/rhel-luks-recovery with a value of 8 to 256 printable ASCII characters other than space, double quote, single quote, backslash and #" {
			t.Fatalf("%s: refusal = %#v", name, reported)
		}
		if strings.Contains(reported[0].Message+reported[0].Remediation, "SENTIN") {
			t.Fatalf("%s: the refusal quotes the passphrase", name)
		}
		if len(runner.requests) != 0 {
			t.Fatalf("%s reached the adapter", name)
		}
	}
	for _, value := range []string{"abcdefgh", strings.Repeat("!", 256), "Zm9vYmFyLWJhei1xdXV4LWNvcmdlLWdyYXVsdC1nYXJw"} {
		runner := &fakeRunner{}
		_, _ = New(runner).Apply(context.Background(), encryptionExecution(t, request, []byte(value)))
		if len(runner.requests) != 1 {
			t.Fatalf("a %d-byte passphrase did not reach the adapter", len(value))
		}
	}
	runner := &fakeRunner{}
	_, err := New(runner).Apply(context.Background(), encryptionExecution(t, request, nil))
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "secret.store" || len(runner.requests) != 0 {
		t.Fatalf("an unbound passphrase: refusal = %#v", reported)
	}
}

func TestRecoveryPassphraseNeverReachesRequestValuesOrEvidence(t *testing.T) {
	request := pinnedRequest(t, labCatalog(encryptingProfile()))
	const sentinel = "Recovery-SENTINEL-0123456789"
	runner := &fakeRunner{}
	_, _ = New(runner).Apply(context.Background(), encryptionExecution(t, request, []byte(sentinel)))
	if len(runner.requests) != 1 {
		t.Fatal("the apply did not reach the adapter")
	}
	run := runner.requests[0]
	want := lifecycle.MaterialFile{Name: "recovery", Part: secrets.ValuePart, Secret: "rhel-luks-recovery", Variable: "recoveryPassphrase"}
	if !slices.Contains(run.Materials, want) {
		t.Fatalf("materials = %+v", run.Materials)
	}
	exposed := string(run.Canonical)
	for key, value := range run.MaterialValues {
		exposed += key + value
	}
	for _, refusal := range run.Refusals {
		for _, reported := range diagnostics.Of(refusal) {
			exposed += reported.Message + reported.Remediation
		}
	}
	if strings.Contains(exposed, sentinel) {
		t.Fatal("the passphrase reached the request, a value or a refusal")
	}
	if _, ok := run.Refusals["root-encryption-unproved"]; !ok {
		t.Fatalf("refusals = %v", slices.Sorted(maps.Keys(run.Refusals)))
	}
	for _, operation := range []string{"observe", "destroy"} {
		runner := &fakeRunner{}
		execution := encryptionExecution(t, request, []byte(sentinel))
		if operation == "observe" {
			_, _ = New(runner).Observe(context.Background(), execution)
		} else {
			_, _ = New(runner).Destroy(context.Background(), execution)
		}
		if len(runner.requests) != 1 {
			t.Fatalf("%s did not reach the adapter", operation)
		}
		for _, material := range runner.requests[0].Materials {
			if material.Secret == "rhel-luks-recovery" {
				t.Fatalf("%s lends the recovery passphrase as %+v", operation, material)
			}
		}
	}
	data, err := reconciliation.Freeze(Evidence{RootEncryption: true, NetworkDifferences: []string{}}, "installation evidence")
	if err != nil || strings.Contains(string(data), sentinel) {
		t.Fatalf("evidence = %s (%v)", data, err)
	}
}

func TestEncryptedCompletionRequiresRootOnLUKSWithATPM2Slot(t *testing.T) {
	encrypted := pinnedRequest(t, labCatalog(encryptingProfile()))
	marker, _ := MarkerFor(encrypted, "digest")
	evidence := func(request Request, rootEncryption bool) []byte {
		var decoded map[string]any
		if err := json.Unmarshal(completeEvidence(request, "digest", string(marker)), &decoded); err != nil {
			t.Fatal(err)
		}
		decoded["image"], decoded["private"], decoded["rootEncryption"] = request.Private == nil, false, rootEncryption
		data, _ := json.Marshal(decoded)
		return data
	}
	err := ValidatePresence(evidence(encrypted, false), encrypted, "digest", string(marker))
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
		reported[0].Message != "the installed machine's root filesystem is not on the LUKS device with a TPM2 binding its installation encrypted" {
		t.Fatalf("refusal = %#v", reported)
	}
	if err := ValidatePresence(evidence(encrypted, true), encrypted, "digest", string(marker)); err != nil {
		t.Fatalf("a proved encrypted root refused: %v", diagnostics.Of(err))
	}
	plain := pinnedRequest(t, labCatalog())
	plainMarker, _ := MarkerFor(plain, "digest")
	if err := ValidatePresence(completeEvidence(plain, "digest", string(plainMarker)), plain, "digest", string(plainMarker)); err != nil {
		t.Fatalf("a plain installation needs a root encryption proof: %v", diagnostics.Of(err))
	}
}

func TestPrivateControllerRulesApplyToEveryPrivateInstallation(t *testing.T) {
	agent := func(change func(*substrate.Target)) substrate.Target {
		target := substrate.Target{Identity: substrate.Identity{Channel: substrate.ChannelGuestAgent}}
		target.Controller.Endpoint, target.Controller.TLSVerify = "https://192.0.2.1:8000/redfish/v1/Systems/rhel-01", true
		change(&target)
		return target
	}
	unverifiedFetch := agent(func(target *substrate.Target) {
		target.Controller.VirtualMedia.Trust = substrate.TrustDisableVerification
	})
	if reason, _ := refusedTarget(guest(), unverifiedFetch, true); reason != "a Machine that delivers private material through its installation cannot let its controller fetch without verifying the artifact server" {
		t.Fatalf("reason = %q", reason)
	}
	if reason, _ := refusedTarget(guest(), unverifiedFetch, false); reason != "" {
		t.Fatalf("a public installation refused: %q", reason)
	}
	for name, change := range map[string]func(*substrate.Target){
		"http": func(target *substrate.Target) {
			target.Controller.Endpoint = "http://192.0.2.1:8000/redfish/v1/Systems/rhel-01"
		},
		"unverified": func(target *substrate.Target) { target.Controller.TLSVerify = false },
	} {
		reason, _ := refusedTarget(guest(), agent(change), true)
		if !strings.HasPrefix(reason, "a Machine whose installation delivers private material hands its controller the private installer image URL") {
			t.Fatalf("%s: reason = %q", name, reason)
		}
		if reason, _ := refusedTarget(guest(), agent(change), false); reason != "" {
			t.Fatalf("%s: a public installation refused: %q", name, reason)
		}
	}
	catalog := labCatalog(encryptingProfile())
	derived, err := substrate.TargetFor(catalog, guest(), testContext, "controller")
	if err != nil {
		t.Fatal(err)
	}
	if !privateInstallation(catalog, guest(), derived.Identity.Channel) {
		t.Fatal("an encrypting guest is not a private installation")
	}
	if reason, _ := refusedTarget(guest(), derived, true); reason != "" {
		t.Fatalf("the lab target refused: %q", reason)
	}
}
