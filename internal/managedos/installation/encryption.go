package installation

import (
	"encoding/base64"
	"encoding/json"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/substrate"
)

type KickstartEncryption struct {
	PBKDF   string
	PCRBank string
	PCRIDs  []int
}

const storageInclude = "/tmp/bootwright-storage.ks"

var encryptionPackages = []string{"clevis-dracut", "clevis-luks", "clevis-systemd"}

func kickstartEncryptionFor(profile api.Object) *KickstartEncryption {
	selected := diskEncryption(profile)
	if !selected.Present() {
		return nil
	}
	tpm := selected.Get("unlock", "tpm2")
	encryption := KickstartEncryption{PCRBank: tpm.Get("pcrBank").Text()}
	for _, item := range tpm.Get("pcrIds").Items() {
		if id, ok := item.Int64(); ok {
			encryption.PCRIDs = append(encryption.PCRIDs, int(id))
		}
	}
	slices.Sort(encryption.PCRIDs)
	encryption.PCRIDs = slices.Compact(encryption.PCRIDs)
	if fipsEnabled(profile) {
		encryption.PBKDF = "pbkdf2"
	}
	return &encryption
}

const (
	encryptionField      = "spec.customizations.security.diskEncryption"
	maxGeneratedRecovery = 192
)

func refusedEncryption(catalog api.Catalog, machine, profile api.Object, derived substrate.Target, derivedOK bool) (reason, remediation string) {
	selected := diskEncryption(profile)
	if !selected.Present() {
		return "", ""
	}
	if derivedOK && derived.Identity.Channel == substrate.ChannelDeliveredKey {
		return "the disk-encryption profile arm is qualified only on virtual Machines, and " + machine.Identity() + " is physical; physical qualification is B78",
			"install " + machine.Identity() + " with a profile that does not select " + encryptionField + ", or remove " + encryptionField + " from " + profile.Identity()
	}
	if !imageEndpointIsIPLiteral(catalog, profile) {
		return "the installer fetches the recovery passphrase before it configures name resolution, so the artifact server endpoint of " + profile.Identity() + " must be addressed by IP",
			"select an artifact server endpoint whose address is an IP literal in spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint on " + profile.Identity()
	}
	return refusedRecoveryLength(catalog, selected.Get("recoveryPassphraseRef").Text())
}

func imageEndpointIsIPLiteral(catalog api.Catalog, profile api.Object) bool {
	selection := profile.Spec().Get("installer", "anaconda", "redfishVirtualMedia", "artifactServerEndpoint")
	server, err := artifactserver.Selected(catalog, selection, profile.Identity())
	if err != nil {
		return true
	}
	base, err := artifactserver.EndpointURL(catalog, server, selection.Get("endpointRef").Text(), profile.Identity())
	if err != nil {
		return true
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return false
	}
	_, err = netip.ParseAddr(parsed.Hostname())
	return err == nil
}

func refusedRecoveryLength(catalog api.Catalog, reference string) (reason, remediation string) {
	recovery, found := catalog.Find(api.Secret, reference)
	if !found || recovery.Spec().Get("type").Text() != "token" || !recovery.Spec().Has("source", "generated") {
		return "", ""
	}
	count := int64(32)
	if declared, ok := recovery.Spec().Get("source", "generated", "bytes").Int64(); ok {
		count = declared
	}
	if count <= maxGeneratedRecovery {
		return "", ""
	}
	return "the recovery passphrase is limited to 256 characters, and " + recovery.Identity() + " generates " + strconv.FormatInt(count, 10) +
			" bytes, which encode to " + strconv.Itoa(base64.RawURLEncoding.EncodedLen(int(count))) + " characters",
		"set spec.source.generated.bytes to at most " + strconv.Itoa(maxGeneratedRecovery) + " on " + recovery.Identity()
}

func encryptionValue(read func(KickstartEncryption) string) func(Installation) []string {
	return func(i Installation) []string {
		if i.DiskEncryption == nil {
			return nil
		}
		return []string{read(*i.DiskEncryption)}
	}
}

func recoveryFetchLines() []string {
	return []string{
		"cat > /tmp/bootwright-artifact-ca.pem <<'BOOTWRIGHT_ARTIFACT_CA_EOF'",
		CertificateToken,
		"BOOTWRIGHT_ARTIFACT_CA_EOF",
		"bootwright_recovery=$(curl --fail --silent --show-error --cacert /tmp/bootwright-artifact-ca.pem '" + PrivateURLToken + "/" + RecoveryFile + "')",
		"shred -u /tmp/bootwright-artifact-ca.pem 2>/dev/null || rm -f /tmp/bootwright-artifact-ca.pem",
	}
}

func encryptionPreLines(input Installation) []string {
	if input.DiskEncryption == nil {
		return nil
	}
	pbkdf := ""
	if input.DiskEncryption.PBKDF != "" {
		pbkdf = " --pbkdf=" + input.DiskEncryption.PBKDF
	}
	lines := []string{
		"%pre --erroronfail --interpreter=/bin/bash",
		"set -euo pipefail",
		"umask 077",
		"export LC_ALL=C",
		"if [ ! -c /dev/tpmrm0 ]; then",
		"echo 'Bootwright: this machine presents no TPM 2.0 device at /dev/tpmrm0, which its root-disk encryption binds to; refusing before any disk is touched.' >&2",
		"exit 1",
		"fi",
	}
	lines = append(lines, recoveryFetchLines()...)
	return append(lines,
		"bootwright_class='^[]!$%&(-[^-~]+$'",
		"if [ ${#bootwright_recovery} -lt 8 ] || [ ${#bootwright_recovery} -gt 256 ] || [[ ! ${bootwright_recovery} =~ ${bootwright_class} ]]; then",
		"echo 'Bootwright: the recovery passphrase this installation fetched is not 8 to 256 printable characters free of spaces, quotes, backslashes and #; refusing before any disk is touched.' >&2",
		"exit 1",
		"fi",
		`printf '%s%s\n' 'autopart --type=lvm --encrypted --luks-version=luks2`+pbkdf+` --passphrase=' "${bootwright_recovery}" > `+storageInclude,
		"unset bootwright_recovery",
		"%end",
		"",
	)
}

func encryptionPostLines(input Installation) []string {
	if input.DiskEncryption == nil {
		return nil
	}
	lines := []string{"%post --erroronfail --interpreter=/bin/bash", "set -euo pipefail", "umask 077", "export LC_ALL=C"}
	lines = append(lines, recoveryFetchLines()...)
	lines = append(lines,
		"bootwright_bound=0",
		`for bootwright_device in $(lsblk -rpno NAME,FSTYPE | awk '$2 == "crypto_LUKS" {print $1}'); do`,
		`printf '%s' "${bootwright_recovery}" | clevis luks bind -y -d "${bootwright_device}" -k - tpm2 '`+clevisTPM2Config(*input.DiskEncryption)+`'`,
		`bootwright_slot=$(clevis luks list -d "${bootwright_device}" | awk -F': ' '$2 ~ /^tpm2 / && !found {print $1; found=1}')`,
		`test -n "${bootwright_slot}"`,
	)
	if len(input.DiskEncryption.PCRIDs) == 0 {
		lines = append(lines, `clevis luks pass -d "${bootwright_device}" -s "${bootwright_slot}" | cryptsetup open --test-passphrase --key-slot "${bootwright_slot}" --key-file - "${bootwright_device}"`)
	}
	return append(lines,
		"bootwright_bound=$((bootwright_bound + 1))",
		"done",
		"unset bootwright_recovery",
		`if [ "${bootwright_bound}" -eq 0 ]; then`,
		"echo 'Bootwright: the installed system holds no LUKS device to bind to the TPM; refusing to finish an installation whose root disk is not encrypted.' >&2",
		"exit 1",
		"fi",
		"/usr/bin/dracut -f --regenerate-all",
		"%end",
		"",
	)
}

func clevisTPM2Config(encryption KickstartEncryption) string {
	if len(encryption.PCRIDs) == 0 {
		return "{}"
	}
	ids := make([]string, 0, len(encryption.PCRIDs))
	for _, id := range encryption.PCRIDs {
		ids = append(ids, strconv.Itoa(id))
	}
	data, _ := json.Marshal(struct {
		PCRBank string `json:"pcr_bank"`
		PCRIDs  string `json:"pcr_ids"`
	}{encryption.PCRBank, strings.Join(ids, ",")})
	return string(data)
}

func recoveryPassphraseCarried(value []byte) bool {
	if len(value) < 8 || len(value) > 256 {
		return false
	}
	for _, b := range value {
		if b < 0x21 || b > 0x7e || b == '"' || b == '#' || b == '\'' || b == '\\' {
			return false
		}
	}
	return true
}

func encryptionRefusals(execution lifecycle.Execution, request Request, refusals map[string]error) error {
	if request.DiskEncryption == nil {
		return nil
	}
	if err := recoveryPassphrase(execution, request); err != nil {
		return err
	}
	refusals["root-encryption-unproved"] = rootEncryptionUnproved(execution.Context, request)
	return nil
}

func recoveryPassphrase(execution lifecycle.Execution, request Request) error {
	reference := request.DiskEncryption.RecoveryPassphraseRef
	material, ok := execution.Material[reference]
	var value []byte
	if ok {
		value, ok = material.Part(secrets.ValuePart)
	}
	if !ok {
		return refusal("secret.store", "the bound recovery passphrase is not available to this attempt", lifecycle.ReopenBindings(execution.Context, execution.Continuation))
	}
	if !recoveryPassphraseCarried(value) {
		return refusal("api.value", "Secret/"+reference+" holds a recovery passphrase that is not 8 to 256 printable ASCII characters free of spaces, quotes, backslashes and #, which the installer's storage directive cannot carry; nothing was published or booted",
			"replace Secret/"+reference+" with a value of 8 to 256 printable ASCII characters other than space, double quote, single quote, backslash and #")
	}
	return nil
}

func recoveryMaterial(operation string, request Request) []lifecycle.MaterialFile {
	if operation != "apply" || request.DiskEncryption == nil {
		return nil
	}
	return []lifecycle.MaterialFile{{Name: RecoveryFile, Part: secrets.ValuePart, Secret: request.DiskEncryption.RecoveryPassphraseRef, Variable: "recoveryPassphrase"}}
}

func rootEncryptionUnproved(contextName string, request Request) error {
	return &diagnostics.Failure{Diagnostics: []diagnostics.Diagnostic{{
		Severity: "error", Code: "lifecycle.state",
		Message:     "Machine/" + request.Identity.Object + " was installed with an encrypted root disk, but its root filesystem is not on a LUKS device with a TPM2 binding",
		Remediation: "take this context back with bootwright destroy --context " + contextName + ", then run bootwright apply --context " + contextName + "; the root disk is encrypted only at installation",
		Object:      &diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: string(api.Machine), Name: request.Identity.Object},
	}}}
}
