package installation

import (
	"bytes"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

// KickstartSubscription is what the registration %pre writes. Release is the
// major the installed system's dnf derives, which the installer's dnf takes
// in place of its boot image's minor.
type KickstartSubscription struct {
	ContentBaseURL string
	Hostname       string
	Release        string
}

const (
	subscriptionSourceField = "spec.installer.anaconda.packageSource.subscription"
	subscriptionInclude     = "/tmp/bootwright-rhsm.ks"
	subscriptionSource      = "%include " + subscriptionInclude
	satelliteCAPath         = "/etc/rhsm/ca/bootwright-satellite.pem"
	satellitePrefix         = "/rhsm"
	satellitePort           = "443"
	registrationRecordPath  = "/etc/bootwright/registration.json"
	consumerCertificatePath = "/etc/pki/consumer/cert.pem"
)

const notSatellitePrefix = "not-satellite:"

const RequestDigestToken = "@@BOOTWRIGHT_REQUEST_DIGEST@@"

const (
	OrganizationFile  = "rhsm-organization"
	ActivationKeyFile = "rhsm-activation-key"
	SatelliteCAFile   = "rhsm-satellite-ca"
)

func subscriptionSourced(profile api.Object) bool {
	return profile.Spec().Has("installer", "anaconda", "packageSource", "subscription")
}

func (r Request) subscriptionSourced() bool {
	return r.Registration != nil && r.Registration.InstallSource
}

func kickstartSubscriptionFor(request Request, profile api.Object) *KickstartSubscription {
	if !request.subscriptionSourced() || request.Registration.Satellite == nil {
		return nil
	}
	major, _, _ := strings.Cut(profile.Spec().Get("os", "version").Text(), ".")
	return &KickstartSubscription{
		ContentBaseURL: request.Registration.Satellite.ContentBaseURL,
		Hostname:       request.Registration.Satellite.Hostname,
		Release:        major,
	}
}

func subscriptionValue(read func(KickstartSubscription) string) func(Installation) []string {
	return func(i Installation) []string {
		if i.Subscription == nil {
			return nil
		}
		return []string{read(*i.Subscription)}
	}
}

func subscriptionSourceRefusal(input Installation) error {
	if (input.PackageSource == subscriptionSource) != (input.Subscription != nil) {
		return installRequestInvalid.Fail("the subscription package source and its installer registration disagree", "plan the installation again")
	}
	if input.Subscription != nil && (input.Subscription.Hostname == "" || input.Subscription.ContentBaseURL == "" || input.Subscription.Release == "") {
		return installRequestInvalid.Fail("the subscription package source names no Satellite host name, content root and release", "plan the installation again")
	}
	return nil
}

func subscriptionPreLines(input Installation) []string {
	if input.Subscription == nil {
		return nil
	}
	lines := []string{
		"%pre --erroronfail --interpreter=/bin/bash",
		"set -euo pipefail",
		"umask 077",
		"export LC_ALL=C",
	}
	if input.DiskEncryption == nil {
		lines = append(lines, bootstrapNetworkLines(input)...)
	}
	fetch := "curl --fail --silent --show-error --cacert /tmp/bootwright-artifact-ca.pem"
	return append(lines,
		"cat > /tmp/bootwright-artifact-ca.pem <<'BOOTWRIGHT_ARTIFACT_CA_EOF'",
		CertificateToken,
		"BOOTWRIGHT_ARTIFACT_CA_EOF",
		"bootwright_organization=$("+fetch+" '"+PrivateURLToken+"/"+OrganizationFile+"')",
		"bootwright_activation_key=$("+fetch+" '"+PrivateURLToken+"/"+ActivationKeyFile+"')",
		"mkdir -p "+parentOf(satelliteCAPath),
		fetch+" --output "+satelliteCAPath+" '"+PrivateURLToken+"/"+SatelliteCAFile+"'",
		"shred -u /tmp/bootwright-artifact-ca.pem 2>/dev/null || rm -f /tmp/bootwright-artifact-ca.pem",
		"chmod 0644 "+satelliteCAPath,
		"test -s "+satelliteCAPath,
		"bootwright_class='^[]!$%&(-[^-~]{1,255}$'",
		"if [[ ! ${bootwright_organization} =~ ${bootwright_class} || ! ${bootwright_activation_key} =~ ${bootwright_class} ]]; then",
		"echo 'Bootwright: the organization or activation key this installation fetched is not 1 to 255 printable characters free of spaces, quotes, backslashes and #; refusing before it registers.' >&2",
		"exit 1",
		"fi",
		"systemctl start rhsm.service",
		"/usr/bin/python3 -I - <<'BOOTWRIGHT_RHSM_TRUST_EOF'",
		"from dasbus.connection import SystemMessageBus",
		"from dasbus.typing import Str, get_variant",
		`settings = {"server.prefix": "`+satellitePrefix+`", "server.port": "`+satellitePort+`", "server.insecure": "0", "rhsm.repo_ca_cert": "`+satelliteCAPath+`"}`,
		`config = SystemMessageBus().get_proxy("com.redhat.RHSM1", "/com/redhat/RHSM1/Config", interface_name="com.redhat.RHSM1.Config")`,
		`config.SetAll({key: get_variant(Str, value) for key, value in settings.items()}, "")`,
		"for key, value in settings.items():",
		`    if config.Get(key, "").unpack() != value:`,
		`        raise SystemExit("Bootwright: the installer's subscription service did not take the Satellite setting " + key + "; refusing before it registers.")`,
		"BOOTWRIGHT_RHSM_TRUST_EOF",
		"mkdir -p "+parentOf(ReleaseVariablePath),
		"printf '%s\\n' '"+input.Subscription.Release+"' > "+ReleaseVariablePath,
		"chmod 0644 "+ReleaseVariablePath,
		`printf 'rhsm --organization=%s --activation-key=%s --server-hostname=%s --rhsm-baseurl=%s\n' "${bootwright_organization}" "${bootwright_activation_key}" '`+
			notSatellitePrefix+input.Subscription.Hostname+`' '`+input.Subscription.ContentBaseURL+`' > `+subscriptionInclude,
		"unset bootwright_organization bootwright_activation_key",
		"%end",
		"",
	)
}

func subscriptionRecordLines(input Installation) []string {
	if input.Subscription == nil {
		return nil
	}
	return []string{
		"test -s " + consumerCertificatePath,
		`printf '{"consumer":"","request":"%s"}\n' '` + RequestDigestToken + `' > ` + registrationRecordPath,
		"chmod 0600 " + registrationRecordPath,
	}
}

func subscriptionPostLines(input Installation) []string {
	if input.Subscription == nil {
		return nil
	}
	return []string{
		"%post --nochroot --erroronfail",
		"set -eu",
		"mkdir -p /mnt/sysimage" + parentOf(satelliteCAPath),
		"cat " + satelliteCAPath + " > /mnt/sysimage" + satelliteCAPath,
		"chmod 0644 /mnt/sysimage" + satelliteCAPath,
		"%end",
	}
}

func installerCarried(value []byte) bool {
	return registrationCarried(value) && !bytes.ContainsAny(value, "\"'\\#")
}

func refusedSubscriptionSource(catalog api.Catalog, profile api.Object) (reason, remediation string) {
	if !subscriptionSourced(profile) || imageEndpointIsIPLiteral(catalog, profile) {
		return "", ""
	}
	return "the installer fetches its registration material before it configures name resolution, so the artifact server endpoint of " + profile.Identity() + " must be addressed by IP",
		"select an artifact server endpoint whose address is an IP literal in spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint on " + profile.Identity()
}
