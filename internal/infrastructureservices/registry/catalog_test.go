package registry

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/desiredstate/compilation"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	secretmaterial "github.com/crmarques/bootwright/internal/secrets/material"
)

const testContext = "lab"

var testMoment = time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)

type fixedClock struct{}

func (fixedClock) Now() time.Time { return testMoment }

type scriptedRunner struct {
	requests []lifecycle.RunRequest
	result   lifecycle.RunResult
}

func (r *scriptedRunner) Run(_ context.Context, request lifecycle.RunRequest) (lifecycle.RunResult, error) {
	r.requests = append(r.requests, request)
	return r.result, nil
}

func field(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}

func text(name, value string) api.FieldValue { return field(name, api.StringValue(value)) }

func catalog(credentialType string) api.Catalog {
	host := api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue(
		field("os", api.MapValue(field("provided", api.BoolValue(true)))),
		field("proxy", api.MapValue(field("direct", api.MapValue()))),
		field("network", api.MapValue(field("addresses", api.ListValue(
			api.MapValue(text("name", "fqdn"), text("address", "controller.lab.example.test")),
			api.MapValue(text("name", "ip"), text("address", "192.0.2.1")),
		)))),
		field("access", api.MapValue(field("local", api.BoolValue(true)))),
	))
	mirror := api.NewObject(api.Registry, "mirror", api.Value{}, api.MapValue(
		text("management", "managed"), text("machineRef", "controller"), text("implementation", "distribution"),
		text("bindAddress", "192.0.2.1"), field("port", api.IntegerValue("5000")),
		field("endpoints", api.ListValue(api.MapValue(text("name", "ip"), text("addressRef", "ip")))),
		field("tls", api.MapValue(text("secretRef", "mirror-serving"), text("minVersion", "TLSv1.3"))), text("credentialsRef", "mirror-push"),
	))
	serving := api.NewObject(api.Secret, "mirror-serving", api.Value{}, api.MapValue(text("type", "tlsCertificate"), text("source", "contextStore")))
	push := api.NewObject(api.Secret, "mirror-push", api.Value{}, api.MapValue(text("type", credentialType), text("source", "contextStore")))
	return api.NewCatalog([]api.Object{host, mirror, serving, push})
}

func frozen(t *testing.T, capability managedservice.Capability, objects api.Catalog) (reconciliation.Block, lifecycle.CapabilityPlan) {
	t.Helper()
	plan, err := capability.Plan(context.Background(), lifecycle.PlanInput{
		Verb: reconciliation.Apply, Context: lifecycle.ContextIdentity{Name: testContext},
		State: compilation.NewState(objects, objects, nil), Controller: "controller",
	})
	if err != nil {
		t.Fatal(err)
	}
	built, err := reconciliation.NewPlan(reconciliation.Apply, plan.Definitions)
	if err != nil {
		t.Fatal(err)
	}
	return built.Blocks[0], plan
}

func serving(t *testing.T, addresses ...string) (secrets.Material, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(2026), Subject: pkix.Name{CommonName: "controller.lab.example.test"},
		NotBefore: testMoment.Add(-time.Hour), NotAfter: testMoment.Add(24 * time.Hour),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, address := range addresses {
		template.IPAddresses = append(template.IPAddresses, net.ParseIP(address))
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(der)
	return secrets.NewMaterial(map[secrets.Part][]byte{
		secrets.CertificatePart: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		secrets.PrivateKeyPart:  pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encoded}),
	}), hex.EncodeToString(digest[:])
}

func credential(username, password string) secrets.Material {
	return secrets.NewMaterial(map[secrets.Part][]byte{secrets.UsernamePart: []byte(username), secrets.PasswordPart: []byte(password)})
}

func message(err error) string {
	reported := diagnostics.Of(err)
	if len(reported) == 0 {
		return ""
	}
	return reported[0].Message
}

// A managed Registry freezes the Secret it serves from and the one its clients
// authenticate with, by name alone, binds both before its operation registers,
// and lends each operation only the parts it reads: an apply the certificate,
// its key and the credential its password file is derived from, an observation
// the credential its readiness authenticates with, a removal nothing.
func TestARegistryFreezesItsServingAndClientSecretsAndLendsEachOperationItsParts(t *testing.T) {
	runner := &scriptedRunner{result: lifecycle.RunResult{Outcome: "changed"}}
	capability := managedservice.NewCapability(Definition(secretmaterial.BcryptPasswordFile), runner).WithClock(fixedClock{})
	block, plan := frozen(t, capability, catalog("usernamePassword"))
	request, err := managedservice.DecodeRequest(block.Request, managedservice.RequestVersion)
	if err != nil {
		t.Fatal(err)
	}
	if request.TLS == nil || *request.TLS != (managedservice.TLS{MinVersion: "TLSv1.3", Secret: "mirror-serving"}) || request.CredentialsRef != "mirror-push" {
		t.Fatalf("the request froze %+v and %q", request.TLS, request.CredentialsRef)
	}
	if request.Unit != "bootwright-lab-registry-mirror" || !strings.HasSuffix(request.ContentRoot, "/lab/services/registry/mirror") || request.Port != 5000 {
		t.Fatalf("the request places the registry at %s, %s:%d", request.Unit, request.ContentRoot, request.Port)
	}
	if !slices.Equal(plan.Secrets, []string{"mirror-push", "mirror-serving"}) {
		t.Fatalf("the plan binds %v", plan.Secrets)
	}
	if strings.Contains(string(block.Request), "password") {
		t.Fatalf("the frozen request carries a credential: %s", block.Request)
	}
	certificate, _ := serving(t, "192.0.2.1")
	material := map[string]secrets.Material{"mirror-serving": certificate, "mirror-push": credential("mirror", "secret")}
	execution := lifecycle.Execution{Operation: "op-1", Context: testContext, Attempt: 1, Block: block, Material: material}
	_, _ = capability.Apply(context.Background(), execution)
	_, _ = capability.Observe(context.Background(), execution)
	_, _ = capability.Destroy(context.Background(), execution)
	lent := map[string][]string{}
	for index, operation := range []string{"apply", "observe", "destroy"} {
		for _, file := range runner.requests[index].Materials {
			lent[operation] = append(lent[operation], file.Variable)
		}
	}
	want := map[string][]string{"apply": {"certificate", "privateKey", "clientUsername", "clientPassword", "passwordFile"}, "observe": {"clientUsername", "clientPassword"}}
	for operation, variables := range want {
		if !slices.Equal(lent[operation], variables) {
			t.Errorf("%s was lent %v, want %v", operation, lent[operation], variables)
		}
	}
	if len(lent["destroy"]) != 0 {
		t.Errorf("a removal was lent %v", lent["destroy"])
	}
	for index, operation := range []string{"apply", "observe"} {
		if got := runner.requests[index].MaterialValues["fingerprint"]; len(got) != 64 {
			t.Errorf("%s was not given the bound certificate's fingerprint: %q", operation, got)
		}
	}
	file, ok := runner.requests[0].Material["bootwright:password-file"].Part(secrets.ValuePart)
	if !ok || !bytes.HasPrefix(file, []byte("mirror:$2a$12$")) || bytes.Contains(file, []byte("secret")) {
		t.Errorf("the apply was lent the password file %q", file)
	}
	if strings.Contains(string(runner.requests[0].Canonical), "$2a$") {
		t.Error("the frozen request carries the password file")
	}
}

// A Registry whose credential is a registry document refuses at plan, because
// its password file needs a username and a password.
func TestARegistryAuthenticatingFromARegistryDocumentRefusesAtPlan(t *testing.T) {
	capability := managedservice.NewCapability(Definition(secretmaterial.BcryptPasswordFile), &scriptedRunner{})
	_, err := capability.Requests(catalog("dockerConfigJson"), "controller", testContext)
	if !strings.Contains(message(err), "usernamePassword") {
		t.Fatalf("a dockerConfigJson credential = %v, want a refusal naming usernamePassword", err)
	}
}

// Bound material that cannot serve or authenticate refuses before the
// operation registers, naming its Secret and never its material.
func TestARegistryProvesItsBoundCertificateAndCredentialBeforeRegistration(t *testing.T) {
	capability := managedservice.NewCapability(Definition(secretmaterial.BcryptPasswordFile), &scriptedRunner{}).WithClock(fixedClock{})
	block, _ := frozen(t, capability, catalog("usernamePassword"))
	good, _ := serving(t, "192.0.2.1")
	other, _ := serving(t, "192.0.2.99")
	for name, tc := range map[string]struct {
		material map[string]secrets.Material
		refusal  string
	}{
		"usable":          {map[string]secrets.Material{"mirror-serving": good, "mirror-push": credential("mirror", "hunter2")}, ""},
		"unnamed address": {map[string]secrets.Material{"mirror-serving": other, "mirror-push": credential("mirror", "hunter2")}, "does not name 192.0.2.1"},
		"no credential":   {map[string]secrets.Material{"mirror-serving": good}, "is not available"},
		"colon":           {map[string]secrets.Material{"mirror-serving": good, "mirror-push": credential("mir:ror", "hunter2")}, "colon"},
		"long password":   {map[string]secrets.Material{"mirror-serving": good, "mirror-push": credential("mirror", strings.Repeat("p", 73))}, "72 bytes"},
		"no certificate":  {map[string]secrets.Material{"mirror-push": credential("mirror", "hunter2")}, "mirror-serving"},
	} {
		err := capability.ProveBinding(context.Background(), testContext, block, tc.material)
		if tc.refusal == "" && err != nil || tc.refusal != "" && !strings.Contains(message(err), tc.refusal) {
			t.Errorf("%s: proved %v, want a refusal naming %q", name, err, tc.refusal)
		}
		if reported := fmt.Sprint(diagnostics.Of(err)); strings.Contains(reported, "hunter2") || strings.Contains(reported, strings.Repeat("p", 73)) {
			t.Errorf("%s: the refusal carries material: %s", name, reported)
		}
	}
}

// An apply is complete only when every probe target answered presenting the
// certificate the attempt bound; another certificate fails the attempt rather
// than leaving it unproved.
func TestARegistryIsPresentOnlyWhenItPresentsTheBoundCertificate(t *testing.T) {
	certificate, fingerprint := serving(t, "192.0.2.1")
	material := map[string]secrets.Material{"mirror-serving": certificate, "mirror-push": credential("mirror", "secret")}
	for name, tc := range map[string]struct {
		presented string
		outcome   reconciliation.Outcome
	}{
		"the bound certificate": {fingerprint, reconciliation.OutcomeChanged},
		"another certificate":   {strings.Repeat("e", 64), reconciliation.OutcomeFailed},
		"no certificate":        {"", reconciliation.OutcomeFailed},
	} {
		runner := &scriptedRunner{}
		capability := managedservice.NewCapability(Definition(secretmaterial.BcryptPasswordFile), runner).WithClock(fixedClock{})
		block, _ := frozen(t, capability, catalog("usernamePassword"))
		request, _ := managedservice.DecodeRequest(block.Request, managedservice.RequestVersion)
		evidence, _ := json.Marshal(managedservice.Evidence{
			Answers:   []managedservice.Answer{{Address: "192.0.2.1", Answer: "HTTP/1.1 200 OK", Fingerprint: tc.presented, Port: 5000}},
			Container: request.Image, ContentRoot: true, Postcondition: true, Request: block.RequestDigest, StartedAfterFiles: true, Unit: "active",
		})
		runner.result = lifecycle.RunResult{Outcome: "changed", Evidence: evidence}
		result, err := capability.Apply(context.Background(), lifecycle.Execution{Operation: "op-1", Context: testContext, Attempt: 1, Block: block, Material: material})
		if result.Outcome != tc.outcome {
			t.Errorf("%s: outcome %s (%v), want %s", name, result.Outcome, err, tc.outcome)
		}
		if tc.outcome == reconciliation.OutcomeFailed && !strings.Contains(message(err), "different certificate") {
			t.Errorf("%s: refused as %v", name, err)
		}
	}
}
