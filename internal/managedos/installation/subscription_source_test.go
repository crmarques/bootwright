package installation

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func onSubscriptionSource(profile api.Object) api.Object {
	return profile.WithSpec(profile.Spec().WithPath(api.MapValue(field("subscription", api.MapValue())), "installer", "anaconda", "packageSource"))
}

func subscriptionSourceProfile() api.Object { return onSubscriptionSource(updatingProfile()) }

func subscriptionSourceCatalog(objects ...api.Object) api.Catalog {
	return labCatalog(append([]api.Object{subscriptionSourceProfile(), satelliteEntitlement()}, objects...)...)
}

func preSection(t *testing.T, kickstart, marker string) string {
	t.Helper()
	for _, section := range strings.Split(kickstart, "%pre --erroronfail --interpreter=/bin/bash\n")[1:] {
		if body := strings.Split(section, "\n%end")[0]; strings.Contains(body, marker) {
			return body
		}
	}
	t.Fatalf("the kickstart has no %%pre holding %q:\n%s", marker, kickstart)
	return ""
}

func TestASubscriptionSourceFreezesAnInstallerRegistrationAndNoTree(t *testing.T) {
	if refused := Refusals(subscriptionSourceCatalog()); len(refused) != 0 {
		t.Fatalf("refusals = %+v, want none", refused)
	}
	request := pinnedRequest(t, subscriptionSourceCatalog())
	if request.Registration == nil || !request.Registration.InstallSource || request.Registration.Satellite == nil {
		t.Fatalf("registration = %+v", request.Registration)
	}
	if request.Tree != nil || request.TreeMedia != nil {
		t.Fatalf("a subscription source froze a package tree: %+v %+v", request.Tree, request.TreeMedia)
	}
	if request.Private == nil || request.Image != nil || request.TLSCertificateRef != "lab-artifacts-tls" {
		t.Fatalf("private = %+v, image = %+v, certificate = %q", request.Private, request.Image, request.TLSCertificateRef)
	}
	lines := kickstartLines(request)
	lineIndex(t, lines, "%include /tmp/bootwright-rhsm.ks")
	for _, line := range lines {
		if line == "cdrom" || strings.HasPrefix(line, "url ") || strings.HasPrefix(line, "repo ") {
			t.Fatalf("a subscription source renders another install source: %q", line)
		}
	}
	lineIndex(t, lines, "subscription-manager")
	canonical, err := request.Canonical()
	if err != nil || !strings.Contains(string(canonical), `"installSource":true`) {
		t.Fatalf("the request does not freeze its install source: %v", err)
	}
	hosted := pinnedRequest(t, labCatalog(updatingProfile(), satelliteEntitlement()))
	canonical, err = hosted.Canonical()
	if err != nil || hosted.Registration.InstallSource || strings.Contains(string(canonical), "installSource") {
		t.Fatalf("a hosted-tree registration froze an install source: %v", err)
	}
	if err := RefusedReleases(subscriptionSourceCatalog(), request, labRecords()); err != nil {
		t.Fatalf("the boot image's release was refused: %v", diagnostics.Of(err))
	}
	unreleased := labRecords()
	unreleased[bootImageName] = MediaRecord{SHA256: bootDigest, Size: 1105199104, Observed: 1105199104}
	if err := RefusedReleases(subscriptionSourceCatalog(), request, unreleased); err == nil {
		t.Fatal("a boot image naming no release was accepted with no other image to name it")
	}
}

// A RHEL boot image's .discinfo names a version and arch but no family, which
// is enough for the boot image a subscription installs from, held to the
// declared release and arch.
func TestASubscriptionSourceBootImageMayNameNoFamily(t *testing.T) {
	request := pinnedRequest(t, subscriptionSourceCatalog())
	const profile = "MachineInstallProfile/rhel-9-8"
	for release, want := range map[managedos.MediaRelease]string{
		{Version: "9.8", Arch: "x86_64"}:                          "",
		{Version: "9.7", Arch: "x86_64"}:                          bootImageName + " holds release 9.7 and " + profile + " declares spec.os.version 9.8",
		{Version: "9.8", Arch: "aarch64"}:                         bootImageName + " holds a aarch64 release, and an installation installs only Red Hat Enterprise Linux on x86_64",
		{Family: "CentOS Stream", Version: "9.8", Arch: "x86_64"}: bootImageName + " holds a CentOS Stream x86_64 release, and an installation installs only Red Hat Enterprise Linux on x86_64",
	} {
		records := labRecords()
		record := records[bootImageName]
		record.Release = release
		records[bootImageName] = record
		err := RefusedReleases(subscriptionSourceCatalog(), request, records)
		if want == "" {
			if err != nil {
				t.Fatalf("%+v was refused: %v", release, diagnostics.Of(err))
			}
			continue
		}
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "install.media-unusable" || reported[0].Message != want {
			t.Fatalf("%+v: refusal = %#v, want %q", release, reported, want)
		}
	}
}

func TestASubscriptionSourceWithoutItsRegistrationRefusesAtTheRequest(t *testing.T) {
	want := "a subscription package source installs from the Satellite a managed registration declares, in the content view its activation key selects, and MachineInstallProfile/rhel-9-8 declares no such registration"
	selecting := rhelEntitlement(field("satellite", api.MapValue(
		text("hostname", "satellite.example.test"), text("trustBundleRef", "satellite-ca"),
		text("contentBaseURL", "https://capsule.example.test/pulp/content"),
		text("contentView", "rhel-9"), text("lifecycleEnvironment", "Library"),
	)))
	for name, catalog := range map[string]api.Catalog{
		"hosted service":   labCatalog(subscriptionSourceProfile(), rhelEntitlement()),
		"external":         labCatalog(onSubscriptionSource(withPolicy(subscribedProfile(), "none")), externalEntitlement()),
		"no registration":  labCatalog(onSubscriptionSource(withPolicy(installProfile(), "none"))),
		"explicit content": labCatalog(subscriptionSourceProfile(), selecting),
	} {
		_, _, err := Requests(catalog, "controller", testContext)
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "install.unsupported" || reported[0].Message != want ||
			!strings.Contains(reported[0].Remediation, "spec.installer.anaconda.packageSource.subscription") {
			t.Fatalf("%s: refusal = %#v", name, reported)
		}
	}
}

func TestTheSubscriptionPreUsesOnlyTheVerifiedPathAndNoSatelliteProvisioning(t *testing.T) {
	request := pinnedRequest(t, subscriptionSourceCatalog())
	pre := preSection(t, request.Kickstart, "bootwright_activation_key=")
	for _, forbidden := range []string{"http://", "--insecure", " -k ", "katello", "--noverifyssl", "update-ca-trust"} {
		if strings.Contains(pre, forbidden) {
			t.Fatalf("the registration %%pre holds %q:\n%s", forbidden, pre)
		}
	}
	steps := []string{
		"nmcli --wait 60 connection up uuid ",
		"bootwright_organization=$(curl --fail --silent --show-error --cacert /tmp/bootwright-artifact-ca.pem '" + PrivateURLToken + "/rhsm-organization')",
		"bootwright_activation_key=$(curl --fail --silent --show-error --cacert /tmp/bootwright-artifact-ca.pem '" + PrivateURLToken + "/rhsm-activation-key')",
		"curl --fail --silent --show-error --cacert /tmp/bootwright-artifact-ca.pem --output /etc/rhsm/ca/bootwright-satellite.pem '" + PrivateURLToken + "/rhsm-satellite-ca'",
		"systemctl start rhsm.service",
		`settings = {"server.prefix": "/rhsm", "server.port": "443", "server.insecure": "0", "rhsm.repo_ca_cert": "/etc/rhsm/ca/bootwright-satellite.pem"}`,
		`config = SystemMessageBus().get_proxy("com.redhat.RHSM1", "/com/redhat/RHSM1/Config", interface_name="com.redhat.RHSM1.Config")`,
		`config.SetAll({key: get_variant(Str, value) for key, value in settings.items()}, "")`,
		"mkdir -p /etc/dnf/vars",
		"printf '%s\\n' '9' > /etc/dnf/vars/releasever",
		"chmod 0644 /etc/dnf/vars/releasever",
		"'not-satellite:satellite.example.test' 'https://capsule.example.test/pulp/content' > /tmp/bootwright-rhsm.ks",
		"unset bootwright_organization bootwright_activation_key",
	}
	at := -1
	for _, step := range steps {
		next := strings.Index(pre, step)
		if next <= at {
			t.Fatalf("the registration %%pre lacks %q after its earlier steps:\n%s", step, pre)
		}
		at = next
	}
	if strings.Count(request.Kickstart, "not-satellite:") != 1 || strings.Contains(request.Kickstart, "--server-hostname=satellite.example.test") {
		t.Fatal("the registration command can reach Anaconda's Satellite provisioning")
	}
}

func TestAnEncryptingSubscriptionSourceActivatesItsNetworkOnce(t *testing.T) {
	profile := onSubscriptionSource(withPolicy(encryptingProfile(), "withinRelease"))
	profile = profile.WithSpec(profile.Spec().With("subscription", api.MapValue(text("entitlementRef", "rhel"))))
	request := pinnedRequest(t, labCatalog(profile, satelliteEntitlement()))
	if strings.Count(request.Kickstart, "nmcli --wait 60 connection load ") != 1 {
		t.Fatalf("the installer network is not activated exactly once:\n%s", request.Kickstart)
	}
	recovery := strings.Index(request.Kickstart, "bootwright_recovery=$(curl ")
	registration := strings.Index(request.Kickstart, "bootwright_organization=$(curl ")
	if recovery < 0 || registration < recovery {
		t.Fatal("the registration fetch does not follow the encryption pre-script that activates the network")
	}
}

func TestTheSubscriptionPostRecordsThePendingRegistrationAndCarriesTheCA(t *testing.T) {
	request := pinnedRequest(t, subscriptionSourceCatalog())
	lines := kickstartLines(request)
	marker := lineIndex(t, lines, "chmod 0444 "+MarkerPath)
	proof := lineIndex(t, lines, "test -s /etc/pki/consumer/cert.pem")
	record := lineIndex(t, lines, `printf '{"consumer":"","request":"%s"}\n' '`+RequestDigestToken+`' > /etc/bootwright/registration.json`)
	mode := lineIndex(t, lines, "chmod 0600 /etc/bootwright/registration.json")
	retained := lineIndex(t, lines, "rm -f /root/anaconda-ks.cfg /root/original-ks.cfg /run/install/ks.cfg")
	if !(marker < proof && proof < record && record < mode && mode < retained) {
		t.Fatalf("the registration record is not written inside the marker's %%post, after the consumer certificate is proved")
	}
	digest := strings.Repeat("ab", 32)
	output, err := exec.Command("/bin/bash", "-c", strings.ReplaceAll(strings.Split(lines[record], " > ")[0], RequestDigestToken, digest)).Output()
	if err != nil {
		t.Fatal(err)
	}
	var written map[string]string
	if err := json.Unmarshal(output, &written); err != nil || len(written) != 2 || written["consumer"] != "" || written["request"] != digest {
		t.Fatalf("the record the worker reads as pending is %q (%v)", output, err)
	}
	copied := lineIndex(t, lines, "cat /etc/rhsm/ca/bootwright-satellite.pem > /mnt/sysimage/etc/rhsm/ca/bootwright-satellite.pem")
	if lines[copied-2] != "set -eu" || lines[copied-3] != "%post --nochroot --erroronfail" || lines[copied+1] != "chmod 0644 /mnt/sysimage/etc/rhsm/ca/bootwright-satellite.pem" {
		t.Fatalf("the CA copy is not its own no-chroot post-script: %q", lines[copied-3:copied+2])
	}
	plain := pinnedRequest(t, labCatalog(updatingProfile(), satelliteEntitlement()))
	for _, absent := range []string{"registration.json", "bootwright-rhsm.ks", RequestDigestToken, "/mnt/sysimage/etc/rhsm"} {
		if strings.Contains(plain.Kickstart, absent) {
			t.Fatalf("a hosted-tree registration renders %q", absent)
		}
	}
}

type subscriptionPreRun struct {
	include, ca, trust string
	release            string
	includeMode        os.FileMode
	caMode             os.FileMode
	releaseMode        os.FileMode
	artifactCA         bool
	err                error
}

// runSubscriptionPre executes the actual %pre with an isolated /tmp and CA
// directory, and its installer release variable in that directory too. Only nmcli, curl, systemctl and python3's module path are
// replaced, and only tools the installer environment holds are on the path;
// the embedded Python runs as written
// against a dasbus double that keeps what the subscription service was given
// and, as dasbus does, binds an object proxy's Get to the standard Properties
// interface.
func runSubscriptionPre(t *testing.T, organization, key, failure string) subscriptionPreRun {
	t.Helper()
	root := t.TempDir()
	for _, tool := range []string{"mkdir", "chmod", "cat", "rm"} {
		if err := os.Symlink("/bin/"+tool, filepath.Join(root, tool)); err != nil {
			t.Fatal(err)
		}
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write("nmcli", "#!/bin/bash\nexit 0\n")
	write("curl", `#!/bin/bash
set -eu
output=''
while [ "$#" -gt 1 ]; do
  case "$1" in
    --cacert) test -s "$2"; shift ;;
    --output) output=$2; shift ;;
  esac
  shift
done
leaf=${1##*/}
[ "$PRE_FAIL" != "fetch-$leaf" ]
case "$leaf" in
  rhsm-organization) printf '%s\n' "$PRE_ORGANIZATION" ;;
  rhsm-activation-key) printf '%s\n' "$PRE_KEY" ;;
  rhsm-satellite-ca) printf '%s\n' 'SyntheticCA' > "$output" ;;
  *) exit 22 ;;
esac
`)
	write("python3", `#!/bin/bash
test "$*" = '-I -'
exec /usr/bin/python3 -I -c 'import sys; sys.path.insert(0, sys.argv[1]); exec(compile(sys.stdin.read(), "pre", "exec"))' "$PRE_ROOT/double"
`)
	if err := os.MkdirAll(filepath.Join(root, "double", "dasbus"), 0o700); err != nil {
		t.Fatal(err)
	}
	write("double/dasbus/__init__.py", "")
	write("double/dasbus/typing.py", `Str = "s"


class Variant:
    def __init__(self, value):
        self.value = value

    def unpack(self):
        return self.value


def get_variant(kind, value):
    assert kind == Str and isinstance(value, str)
    return Variant(value)
`)
	write("double/dasbus/connection.py", `import json
import os

from dasbus.typing import Variant

DEFAULTS = {"server.prefix": "/subscription", "server.port": "443", "server.insecure": "0", "rhsm.repo_ca_cert": "/etc/rhsm/ca/redhat-uep.pem"}


class Config:
    path = os.path.join(os.environ["PRE_ROOT"], "trust")

    def SetAll(self, values, locale):
        assert locale == ""
        if os.environ["PRE_FAIL"] == "trust":
            raise RuntimeError("the subscription service is not on the bus")
        taken = {key: value.unpack() for key, value in values.items() if os.environ["PRE_FAIL"] != "ignored-" + key}
        with open(self.path, "w") as stream:
            json.dump(taken, stream, sort_keys=True)

    def Get(self, key, locale):
        with open(self.path) as stream:
            return Variant(json.load(stream).get(key, DEFAULTS[key]))


class Properties:
    def Get(self, interface, name):
        raise RuntimeError("org.freedesktop.DBus.Error.UnknownInterface: " + interface)


class ObjectProxy(Config):
    Get = Properties.Get


class SystemMessageBus:
    def get_proxy(self, service, path, interface_name=None):
        assert (service, path) == ("com.redhat.RHSM1", "/com/redhat/RHSM1/Config")
        assert os.path.exists(os.path.join(os.environ["PRE_ROOT"], "rhsm-started"))
        if interface_name is None:
            return ObjectProxy()
        assert interface_name == "com.redhat.RHSM1.Config"
        return Config()
`)
	write("systemctl", `#!/bin/bash
set -e
test "$*" = 'start rhsm.service'
[ "$PRE_FAIL" != rhsm-start ]
: > "$PRE_ROOT/rhsm-started"
`)
	input := physicalInstallation()
	input.Physical, input.ExpectedMACs, input.RootSerial, input.RootWWN, input.RootSizeBytes = false, nil, "", "", 0
	input.Subscription = &KickstartSubscription{ContentBaseURL: "https://capsule.example.test/pulp/content", Hostname: "satellite.example.test", Release: "9"}
	lines := subscriptionPreLines(input)
	script := strings.Join(lines[1:len(lines)-2], "\n")
	script = strings.ReplaceAll(script, "/usr/bin/python3", "python3")
	script = strings.ReplaceAll(script, "/tmp/", root+"/")
	script = strings.ReplaceAll(script, "mkdir -p /etc/rhsm/ca", "mkdir -p "+root+"/rhsm-ca")
	script = strings.ReplaceAll(script, "--output /etc/rhsm/ca/", "--output "+root+"/rhsm-ca/")
	script = strings.ReplaceAll(script, "chmod 0644 /etc/rhsm/ca/", "chmod 0644 "+root+"/rhsm-ca/")
	script = strings.ReplaceAll(script, "test -s /etc/rhsm/ca/", "test -s "+root+"/rhsm-ca/")
	script = strings.ReplaceAll(script, "/etc/dnf/vars", root+"/dnf-vars")
	script = strings.ReplaceAll(script, bootstrapConnections, filepath.Join(root, "connections"))
	script = strings.ReplaceAll(script, CertificateToken, "SyntheticArtifactCertificate")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "/bin/bash", "-c", script)
	command.Dir = root
	command.Env = []string{"PATH=" + root, "PRE_ROOT=" + root, "PRE_FAIL=" + failure, "PRE_ORGANIZATION=" + organization, "PRE_KEY=" + key}
	output, err := command.CombinedOutput()
	if len(output) != 0 {
		t.Logf("pre-script output: %s", output)
	}
	if strings.Contains(string(output), organization) && organization != "" || strings.Contains(string(output), key) && key != "" {
		t.Fatalf("the pre-script printed a registration value: %s", output)
	}
	run := subscriptionPreRun{err: err}
	read := func(name string) (string, os.FileMode) {
		data, readErr := os.ReadFile(filepath.Join(root, name))
		if readErr != nil {
			return "", 0
		}
		info, _ := os.Stat(filepath.Join(root, name))
		return string(data), info.Mode().Perm()
	}
	run.include, run.includeMode = read("bootwright-rhsm.ks")
	run.ca, run.caMode = read("rhsm-ca/bootwright-satellite.pem")
	run.release, run.releaseMode = read("dnf-vars/releasever")
	run.trust, _ = read("trust")
	_, statErr := os.Stat(filepath.Join(root, "bootwright-artifact-ca.pem"))
	run.artifactCA = statErr == nil
	return run
}

func TestTheSubscriptionPreWritesTheRegistrationCommandTheKickstartIncludes(t *testing.T) {
	run := runSubscriptionPre(t, "Example_Org", "rhel-9-key!$%&()*+,-./:;<=>?@[]^_`{|}~", "")
	if run.err != nil {
		t.Fatal(run.err)
	}
	want := "rhsm --organization=Example_Org --activation-key=rhel-9-key!$%&()*+,-./:;<=>?@[]^_`{|}~" +
		" --server-hostname=not-satellite:satellite.example.test --rhsm-baseurl=https://capsule.example.test/pulp/content\n"
	if run.include != want || run.includeMode != 0o600 {
		t.Fatalf("include = %q mode %o, want %q mode 600", run.include, run.includeMode, want)
	}
	if run.ca != "SyntheticCA\n" || run.caMode != 0o644 {
		t.Fatalf("installer CA = %q mode %o", run.ca, run.caMode)
	}
	if run.release != "9\n" || run.releaseMode != 0o644 {
		t.Fatalf("installer release variable = %q mode %o, want the major the installed system's dnf derives", run.release, run.releaseMode)
	}
	want = `{"rhsm.repo_ca_cert": "/etc/rhsm/ca/bootwright-satellite.pem", "server.insecure": "0", "server.port": "443", "server.prefix": "/rhsm"}`
	if run.trust != want {
		t.Fatalf("the subscription service was given %s, want the Satellite's API prefix and port, verification and content CA %s", run.trust, want)
	}
	if run.artifactCA {
		t.Fatal("the artifact certificate outlived the fetch")
	}
}

func TestTheSubscriptionPreRefusesBeforeItWritesTheRegistrationCommand(t *testing.T) {
	for name, attempt := range map[string][3]string{
		"a quoted organization":      {`Example"Org`, "kQ7zX1", ""},
		"a commented activation key": {"Example_Org", "kQ7zX#1", ""},
		"an escaped activation key":  {"Example_Org", `kQ7zX\1`, ""},
		"a spaced organization":      {"Example Org", "kQ7zX1", ""},
		"an empty activation key":    {"Example_Org", "", ""},
		"an unfetched organization":  {"Example_Org", "kQ7zX1", "fetch-rhsm-organization"},
		"an unfetched key":           {"Example_Org", "kQ7zX1", "fetch-rhsm-activation-key"},
		"an unfetched CA":            {"Example_Org", "kQ7zX1", "fetch-rhsm-satellite-ca"},
		"an unstarted service":       {"Example_Org", "kQ7zX1", "rhsm-start"},
		"an unreachable service":     {"Example_Org", "kQ7zX1", "trust"},
		"an untaken API prefix":      {"Example_Org", "kQ7zX1", "ignored-server.prefix"},
		"an untaken content CA":      {"Example_Org", "kQ7zX1", "ignored-rhsm.repo_ca_cert"},
	} {
		t.Run(name, func(t *testing.T) {
			run := runSubscriptionPre(t, attempt[0], attempt[1], attempt[2])
			if run.err == nil || run.include != "" {
				t.Fatalf("error = %v, include = %q", run.err, run.include)
			}
		})
	}
}

func TestASubscriptionSourceRefusesAHostnameArtifactEndpoint(t *testing.T) {
	want := []lifecycle.Refusal{{Kind: "Machine", Name: "rhel-01",
		Reason:      "the installer fetches its registration material before it configures name resolution, so the artifact server endpoint of MachineInstallProfile/rhel-9-8 must be addressed by IP",
		Remediation: "select an artifact server endpoint whose address is an IP literal in spec.installer.anaconda.redfishVirtualMedia.artifactServerEndpoint on MachineInstallProfile/rhel-9-8",
	}}
	catalog := labCatalog(hostnameEndpointServer(), onEndpoint(subscriptionSourceProfile(), "fqdn-https"), satelliteEntitlement())
	if refused := Refusals(catalog); !slices.Equal(refused, want) {
		t.Fatalf("refusals = %+v", refused)
	}
	if refused := Refusals(labCatalog(hostnameEndpointServer(), onEndpoint(subscriptionSourceProfile(), "ip-https"), satelliteEntitlement())); len(refused) != 0 {
		t.Fatalf("an IP endpoint was refused: %+v", refused)
	}
}

func TestSubscriptionSourceValuesOutsideTheKickstartClassRefuseBeforeEffects(t *testing.T) {
	request := pinnedRequest(t, subscriptionSourceCatalog())
	bundle := satelliteCertificate(t)
	for _, value := range []string{`a"b`, "a'b", `a\b`, "a#b"} {
		for _, secret := range []string{"rhel-org", "rhel-key"} {
			organization, key, subject := []byte("org"), []byte("key"), "organization"
			if secret == "rhel-org" {
				organization = []byte(value)
			} else {
				key, subject = []byte(value), "activation key"
			}
			execution := satelliteExecution(t, request, bundle)
			execution.Material = rhsmMaterial(execution.Material, organization, key)
			runner := &fakeRunner{}
			_, err := New(runner).Apply(context.Background(), execution)
			reported := diagnostics.Of(err)
			want := "Secret/" + secret + " holds an " + subject + " for Entitlement/rhel with a quote, a backslash or '#', which the registration command of an installer that installs from its subscription cannot carry, so this attempt changed nothing"
			if len(reported) != 1 || reported[0].Code != "api.value" || reported[0].Message != want || strings.Contains(reported[0].Message+reported[0].Remediation, value) {
				t.Fatalf("%q in %s: refusal = %#v", value, secret, reported)
			}
			if len(runner.requests) != 0 {
				t.Fatalf("%q in %s reached the adapter", value, secret)
			}
		}
	}
	hosted := pinnedRequest(t, labCatalog(updatingProfile(), satelliteEntitlement()))
	execution := satelliteExecution(t, hosted, bundle)
	execution.Material = rhsmMaterial(execution.Material, []byte(`a"b`), []byte("a#b"))
	runner := &fakeRunner{}
	if _, _ = New(runner).Apply(context.Background(), execution); len(runner.requests) != 1 {
		t.Fatal("the worker's own class was narrowed for a registration its worker performs")
	}
}

func TestASubscriptionSourceLendsItsRegistrationMaterialToApplyAlone(t *testing.T) {
	request := pinnedRequest(t, subscriptionSourceCatalog())
	names := func(operation string) []string {
		var found []string
		for _, file := range registrationMaterial(operation, request) {
			found = append(found, file.Name)
		}
		return found
	}
	if got := names("apply"); !slices.Equal(got, []string{OrganizationFile, ActivationKeyFile, SatelliteCAFile}) {
		t.Fatalf("apply lends %v, and the Kickstart fetches %s, %s and %s", got, OrganizationFile, ActivationKeyFile, SatelliteCAFile)
	}
	for _, operation := range []string{"observe", "destroy"} {
		if got := names(operation); len(got) != 0 {
			t.Fatalf("%s lends %v", operation, got)
		}
	}
}

// The installer registers before any apply proves the marker; a removal reads
// the guest exactly when the apply recorded that proof, as for any consumer.
func TestASubscriptionSourceRemovalReadsTheGuestOnlyAfterItsMarkerProof(t *testing.T) {
	request := pinnedRequest(t, subscriptionSourceCatalog())
	if request.Registration == nil || !request.Registration.InstallSource {
		t.Fatalf("registration = %+v", request.Registration)
	}
	skipped := removalRunner(t, RegistrationEvidence{})
	if _, err := New(skipped).Destroy(context.Background(), encryptionExecution(t, request, nil)); err != nil ||
		len(skipped.requests) != 1 || skipped.requests[0].MaterialValues["unregister"] != "skip" {
		t.Fatalf("an unproved subscription-sourced destroy = %v, adapter %+v", diagnostics.Of(err), skipped.requests)
	}
	read := removalRunner(t, RegistrationEvidence{Consumer: ownedConsumer, State: "unregistered"})
	if _, err := New(read).Destroy(context.Background(), proving(t, encryptionExecution(t, request, nil), request)); err != nil ||
		len(read.requests) != 1 || read.requests[0].MaterialValues["unregister"] != "guest" {
		t.Fatalf("a proved subscription-sourced destroy = %v, adapter %+v", diagnostics.Of(err), read.requests)
	}
}
