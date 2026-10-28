package installation

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

func satelliteCertificate(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Katello Server CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func satelliteExecution(t *testing.T, request Request, bundle []byte) lifecycle.Execution {
	t.Helper()
	execution := registrationExecution(t, request, []byte("org"), []byte("key"))
	if bundle != nil {
		execution.Material["satellite-ca"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.CertificatePart: bundle})
	}
	return execution
}

func TestASatelliteRegistrationIsAdmittedAndFrozen(t *testing.T) {
	if refused := Refusals(labCatalog(registeringProfile(), satelliteEntitlement())); len(refused) != 0 {
		t.Fatalf("refusals = %+v, want none", refused)
	}
	request := pinnedRequest(t, labCatalog(updatingProfile(), satelliteEntitlement()))
	want := Satellite{ContentBaseURL: "https://capsule.example.test/pulp/content", Hostname: "satellite.example.test", TrustBundleRef: "satellite-ca"}
	if request.Registration == nil || request.Registration.Satellite == nil || *request.Registration.Satellite != want {
		t.Fatalf("registration = %+v, want satellite %+v", request.Registration, want)
	}
	if !slices.Contains(request.SecretReferences(), "satellite-ca") {
		t.Fatalf("secret references = %v, missing the CA bundle", request.SecretReferences())
	}
	hosted := pinnedRequest(t, labCatalog(updatingProfile(), rhelEntitlement()))
	canonical, err := hosted.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	if hosted.Registration.Satellite != nil || strings.Contains(string(canonical), "satellite") {
		t.Fatal("a hosted registration froze a satellite")
	}
}

func TestASatelliteKickstartWritesNoReleaseVariable(t *testing.T) {
	satellite := pinnedRequest(t, labCatalog(updatingProfile(), satelliteEntitlement()))
	if strings.Contains(satellite.Kickstart, ReleaseVariablePath) {
		t.Fatalf("a Satellite profile writes %s:\n%s", ReleaseVariablePath, satellite.Kickstart)
	}
	hosted := pinnedRequest(t, labCatalog(updatingProfile(), rhelEntitlement()))
	if !slices.Contains(kickstartLines(hosted), "printf '%s\\n' '9.8' > "+ReleaseVariablePath) {
		t.Fatalf("a hosted profile does not write %s:\n%s", ReleaseVariablePath, hosted.Kickstart)
	}
}

func TestTheSatelliteBundleIsLentOnlyToApply(t *testing.T) {
	request := pinnedRequest(t, labCatalog(registeringProfile(), satelliteEntitlement()))
	bundle := satelliteCertificate(t)
	want := lifecycle.MaterialFile{Name: "rhsm-satellite-ca", Part: secrets.CertificatePart, Secret: "satellite-ca", Variable: "rhsmSatelliteCA"}
	for _, operation := range []string{"apply", "observe", "destroy"} {
		runner := &fakeRunner{}
		execution := satelliteExecution(t, request, bundle)
		capability := New(runner)
		switch operation {
		case "apply":
			_, _ = capability.Apply(context.Background(), execution)
		case "observe":
			_, _ = capability.Observe(context.Background(), execution)
		default:
			_, _ = capability.Destroy(context.Background(), execution)
		}
		if len(runner.requests) != 1 {
			t.Fatalf("%s did not reach the adapter", operation)
		}
		run := runner.requests[0]
		lent := slices.Contains(run.Materials, want)
		if lent != (operation == "apply") {
			t.Fatalf("%s lends the CA bundle: %t (%+v)", operation, lent, run.Materials)
		}
		if _, named := run.Refusals["registration-trust"]; named != (operation == "apply") {
			t.Fatalf("%s names registration-trust: %t", operation, named)
		}
	}
	if slices.ContainsFunc(registrationMaterial("apply", pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement()))),
		func(file lifecycle.MaterialFile) bool { return file.Name == want.Name }) {
		t.Fatal("a hosted registration lends a CA bundle")
	}
}

func TestASatelliteBundleOutsideItsGrammarRefusesBeforeEffects(t *testing.T) {
	request := pinnedRequest(t, labCatalog(registeringProfile(), satelliteEntitlement()))
	certificate := satelliteCertificate(t)
	key := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("sentinel-private-key")})
	request64 := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: []byte("sentinel")})
	oversized := slices.Concat(certificate, []byte(strings.Repeat(" ", maxSatelliteBundle)))
	for name, bundle := range map[string][]byte{
		"empty": []byte(""), "whitespace": []byte(" \n"), "garbage": []byte("sentinel-garbage"),
		"private key": slices.Concat(certificate, key), "other block": slices.Concat(certificate, request64),
		"trailing": slices.Concat(certificate, []byte("sentinel-trailing")), "not x509": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("sentinel")}),
		"too many": []byte(strings.Repeat(string(certificate), maxSatelliteCertificates+1)), "oversized": oversized,
		"headers": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Headers: map[string]string{"Proc-Type": "4,ENCRYPTED"}, Bytes: []byte("x")}),
	} {
		runner := &fakeRunner{}
		_, err := New(runner).Apply(context.Background(), satelliteExecution(t, request, bundle))
		reported := diagnostics.Of(err)
		if len(reported) != 1 || reported[0].Code != "api.value" ||
			reported[0].Message != "Secret/satellite-ca holds a Satellite CA bundle for Entitlement/rhel that is not 1 to 32 PEM CERTIFICATE blocks of X.509 certificates and nothing else within 64 KiB, so this attempt changed nothing" ||
			reported[0].Remediation != "replace Secret/satellite-ca with the PEM certificates of the CA that signs the certificate of satellite.example.test, such as the Satellite's katello-server-ca.crt, then run bootwright apply --context lab again" {
			t.Fatalf("%s: refusal = %#v", name, reported)
		}
		if strings.Contains(reported[0].Message+reported[0].Remediation, "sentinel") || len(runner.requests) != 0 {
			t.Fatalf("%s reached the adapter or echoed the bundle", name)
		}
	}
	for name, bundle := range map[string][]byte{
		"one": certificate, "padded": slices.Concat([]byte("\v\f\n"), certificate, []byte("\r\n\f")),
		"spaced lines": spacedLines(certificate),
		"at the bound": []byte(strings.Repeat(string(certificate), maxSatelliteCertificates)),
	} {
		runner := &fakeRunner{}
		_, _ = New(runner).Apply(context.Background(), satelliteExecution(t, request, bundle))
		if len(runner.requests) != 1 {
			t.Fatalf("%s did not reach the adapter", name)
		}
	}
	runner := &fakeRunner{}
	_, err := New(runner).Apply(context.Background(), satelliteExecution(t, request, nil))
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "secret.store" ||
		reported[0].Message != "the bound Satellite CA bundle of Entitlement/rhel is not available to this attempt" || len(runner.requests) != 0 {
		t.Fatalf("an unbound CA bundle: refusal = %#v", reported)
	}
}

func TestRegistrationTrustNamesTheSatelliteAndItsBundle(t *testing.T) {
	refusals := registrationRefusals(testContext, pinnedRequest(t, labCatalog(registeringProfile(), satelliteEntitlement())))
	reported := diagnostics.Of(refusals["registration-trust"])
	if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Object == nil || reported[0].Object.Name != "rhel-01" ||
		reported[0].Message != "the Satellite satellite.example.test presented a certificate that the CA bundle of Secret/satellite-ca does not verify" ||
		reported[0].Remediation != "set Secret/satellite-ca to the CA that signs the certificate of satellite.example.test, such as the Satellite's katello-server-ca.crt, then run bootwright apply --context lab again" {
		t.Fatalf("registration-trust = %#v", reported)
	}
	if _, named := registrationRefusals(testContext, pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement())))["registration-trust"]; named {
		t.Fatal("a hosted registration names registration-trust")
	}
}

func TestCompletionRequiresThePinItsRegistrationHolds(t *testing.T) {
	for name, test := range map[string]struct {
		request  Request
		accepted []string
	}{
		"satellite":       {pinnedRequest(t, labCatalog(updatingProfile(), satelliteEntitlement())), []string{pinRelease, pinContentView}},
		"hosted":          {pinnedRequest(t, labCatalog(updatingProfile(), rhelEntitlement())), []string{pinRelease}},
		"no registration": {pinnedRequest(t, labCatalog(withPolicy(installProfile(), "withinRelease"))), []string{pinVariable}},
	} {
		marker, _ := MarkerFor(test.request, "digest")
		for _, pin := range []string{pinRelease, pinContentView, pinVariable} {
			update := completedUpdate(test.request)
			update.Pin = pin
			evidence := updateEvidence(t, test.request, string(marker), update, nil)
			if test.request.Registration == nil {
				evidence = updateEvidence(t, test.request, string(marker), update, func(e *Evidence) { e.Registration = RegistrationEvidence{} })
			}
			err := ValidatePresence(evidence, test.request, "digest", string(marker))
			if (err == nil) != slices.Contains(test.accepted, pin) {
				t.Fatalf("%s with pin %s: %v", name, pin, diagnostics.Of(err))
			}
			if err != nil && diagnostics.Of(err)[0].Message != "the installed machine's update within release 9.8 names a pin its registration does not hold" {
				t.Fatalf("%s with pin %s: %#v", name, pin, diagnostics.Of(err))
			}
		}
	}
}

func TestAWorkerInputRefusalNamesTheSatelliteInputs(t *testing.T) {
	reported := diagnostics.Of(registrationRefusals(testContext, pinnedRequest(t, labCatalog(registeringProfile(), satelliteEntitlement())))["registration-input"])
	if len(reported) != 1 ||
		reported[0].Message != "the registration worker on Machine/rhel-01 refused the organization or activation key of Entitlement/rhel as not one line of printable ASCII without whitespace, or the Satellite CA bundle or content root of Entitlement/rhel" ||
		reported[0].Remediation != "replace Secret/rhel-org or Secret/rhel-key with one line of 1 to 255 printable ASCII characters without whitespace, or Secret/satellite-ca with the PEM certificates of the CA that signs the certificate of satellite.example.test, then run bootwright apply --context lab again" {
		t.Fatalf("registration-input = %#v", reported)
	}
	hosted := diagnostics.Of(registrationRefusals(testContext, pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement())))["registration-input"])
	if strings.Contains(hosted[0].Message+hosted[0].Remediation, "Satellite") {
		t.Fatalf("a hosted registration-input names a Satellite: %#v", hosted)
	}
}

func spacedLines(certificate []byte) []byte {
	lines := strings.Split(string(certificate), "\n")
	lines[1] += "  \t"
	return []byte(strings.Join(lines, "\n"))
}
