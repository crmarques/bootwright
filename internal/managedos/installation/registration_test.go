package installation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
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

const (
	organizationSentinel = "Organization-SENTINEL-4711"
	activationSentinel   = "Activation-SENTINEL-0815"
	ownedConsumer        = "0f8fad5b-d9cb-469f-a165-70867728950e"
)

func rhelEntitlement(overrides ...api.FieldValue) api.Object {
	rhsm := api.MapValue(
		text("management", "managed"), text("organizationRef", "rhel-org"), text("activationKeyRef", "rhel-key"),
		field("connectToInsights", api.BoolValue(false)),
	)
	for _, override := range overrides {
		rhsm = rhsm.With(override.Name, override.Value)
	}
	return api.NewObject(api.Entitlement, "rhel", api.Value{}, api.MapValue(text("type", "redhat-rhel"), field("rhsm", rhsm)))
}

func externalEntitlement() api.Object {
	return api.NewObject(api.Entitlement, "rhel", api.Value{}, api.MapValue(
		text("type", "redhat-rhel"), field("rhsm", api.MapValue(text("management", "external"))),
	))
}

func registeringProfileEnabling(enable ...string) api.Object {
	customizations := installProfile().Spec().Get("customizations").With("repositories", api.MapValue(
		field("subscription", api.MapValue(field("enable", api.StringList(enable...)), field("disable", api.StringList("*")))),
	))
	return installProfile(field("subscription", api.MapValue(text("entitlementRef", "rhel"))), field("customizations", customizations))
}

func registeringProfile() api.Object {
	return registeringProfileEnabling("rhel-9-for-x86_64-baseos-rpms", "rhel-9-for-x86_64-appstream-rpms")
}

func subscribedProfile() api.Object {
	return installProfile(field("subscription", api.MapValue(text("entitlementRef", "rhel"))))
}

func rhsmMaterial(material map[string]secrets.Material, organization, key []byte) map[string]secrets.Material {
	if organization != nil {
		material["rhel-org"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: organization})
	}
	if key != nil {
		material["rhel-key"] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: key})
	}
	return material
}

func registrationExecution(t *testing.T, request Request, organization, key []byte) lifecycle.Execution {
	t.Helper()
	execution := encryptionExecution(t, request, nil)
	execution.Material = rhsmMaterial(execution.Material, organization, key)
	return execution
}

func TestSubscriptionFreezesRegistrationWithoutSecretBytes(t *testing.T) {
	request, _ := onlyRequest(t, labCatalog(registeringProfile(), rhelEntitlement()))
	want := Registration{ActivationKeyRef: "rhel-key", EntitlementRef: "rhel", OrganizationRef: "rhel-org", Repositories: SubscriptionRepositories{
		Disable: []string{"*"}, Enable: []string{"rhel-9-for-x86_64-appstream-rpms", "rhel-9-for-x86_64-baseos-rpms"},
	}}
	if request.Registration == nil || request.Registration.ActivationKeyRef != want.ActivationKeyRef || request.Registration.EntitlementRef != want.EntitlementRef ||
		request.Registration.OrganizationRef != want.OrganizationRef || !slices.Equal(request.Registration.Repositories.Disable, want.Repositories.Disable) ||
		!slices.Equal(request.Registration.Repositories.Enable, want.Repositories.Enable) {
		t.Fatalf("registration = %+v, want %+v", request.Registration, want)
	}
	for _, reference := range []string{"rhel-org", "rhel-key"} {
		if !slices.Contains(request.SecretReferences(), reference) {
			t.Fatalf("secret references = %v, missing %s", request.SecretReferences(), reference)
		}
	}
	lines := kickstartLines(request)
	if packages := lineIndex(t, lines, "%packages --excludedocs --exclude-weakdeps"); slices.Index(lines, "subscription-manager") < packages {
		t.Fatalf("%%packages does not list subscription-manager:\n%s", request.Kickstart)
	}
	bare, _ := onlyRequest(t, labCatalog(subscribedProfile(), rhelEntitlement()))
	if bare.Registration == nil || bare.Registration.Repositories.Disable == nil || bare.Registration.Repositories.Enable == nil {
		t.Fatalf("a registration without repositories = %+v", bare.Registration)
	}
	canonical, err := bare.Canonical()
	if err != nil || !strings.Contains(string(canonical), `"repositories":{"disable":[],"enable":[]}`) {
		t.Fatalf("canonical = %s (%v)", canonical, err)
	}
	external, _ := onlyRequest(t, labCatalog(subscribedProfile(), externalEntitlement()))
	if external.Registration != nil || slices.Contains(kickstartLines(external), "subscription-manager") {
		t.Fatalf("an external entitlement registers: %+v", external.Registration)
	}
	plain, _ := onlyRequest(t, labCatalog())
	golden, err := os.ReadFile(filepath.Join("testdata", "request-lab-rhel.golden"))
	if err != nil {
		t.Fatal(err)
	}
	var frozen Request
	if err := json.Unmarshal(golden, &frozen); err != nil {
		t.Fatal(err)
	}
	if plain.Kickstart != frozen.Kickstart || plain.Registration != nil {
		t.Fatalf("a profile without spec.subscription moved its kickstart:\n%s", lineDiff(frozen.Kickstart, plain.Kickstart))
	}
	if strings.Contains(string(canonical)+request.Kickstart, organizationSentinel) || strings.Contains(string(canonical)+request.Kickstart, activationSentinel) {
		t.Fatal("a registration value reached the request")
	}
}

func TestRegistrationRefusalsNameTheEntitlementField(t *testing.T) {
	agent := substrate.Target{Identity: substrate.Identity{Channel: substrate.ChannelGuestAgent}}
	delivered := substrate.Target{Identity: substrate.Identity{Channel: substrate.ChannelDeliveredKey}}
	metal := server(api.MapValue(text("deviceName", "/dev/disk/by-id/wwn-0x5000c500a1b2c3d4")))
	corporate := externalProxy("corporate", text("httpsProxy", "http://192.0.2.80:3128"))
	for name, test := range map[string]struct {
		catalog api.Catalog
		machine api.Object
		target  substrate.Target
		want    [2]string
	}{
		"physical": {labCatalog(registeringProfile(), rhelEntitlement()), metal, delivered, [2]string{}},
		"satellite": {labCatalog(registeringProfile(), rhelEntitlement(field("satellite", api.MapValue(text("hostname", "satellite.example.test"))))), guest(), agent, [2]string{
			"this executable registers only with the hosted subscription service, and Entitlement/rhel selects a Satellite in spec.rhsm.satellite",
			"remove spec.rhsm.satellite from Entitlement/rhel, or remove spec.subscription from MachineInstallProfile/rhel-9-8",
		}},
		"insights": {labCatalog(registeringProfile(), rhelEntitlement(field("connectToInsights", api.BoolValue(true)))), guest(), agent, [2]string{
			"this executable does not enroll a Machine in Insights, a second remote identity it neither proves nor removes, and Entitlement/rhel sets spec.rhsm.connectToInsights to true",
			"set spec.rhsm.connectToInsights to false on Entitlement/rhel, or remove spec.subscription from MachineInstallProfile/rhel-9-8",
		}},
		"external with repositories": {labCatalog(registeringProfile(), externalEntitlement()), guest(), agent, [2]string{
			"subscription repositories are enabled on a registration this executable performs, and Entitlement/rhel sets spec.rhsm.management to external",
			"remove spec.customizations.repositories.subscription from MachineInstallProfile/rhel-9-8, or set spec.rhsm.management to managed on Entitlement/rhel",
		}},
		"proxy": {labCatalog(registeringProfile(), rhelEntitlement(), corporate, proxiedGuest()), proxiedGuest(), agent, [2]string{
			"this executable registers only over a direct route, and Machine/rhel-01 reaches the network through Proxy/corporate",
			"select direct: {} in spec.proxy of Machine/rhel-01 or of MachineInstallProfile/rhel-9-8, or remove spec.subscription from MachineInstallProfile/rhel-9-8",
		}},
		"hosted direct":                 {labCatalog(registeringProfile(), rhelEntitlement()), guest(field("proxy", api.MapValue(field("direct", api.MapValue())))), agent, [2]string{}},
		"hosted without proxy":          {labCatalog(registeringProfile(), rhelEntitlement()), guest(), agent, [2]string{}},
		"external without repositories": {labCatalog(subscribedProfile(), externalEntitlement()), guest(), agent, [2]string{}},
		"no subscription":               {labCatalog(), metal, delivered, [2]string{}},
		"missing entitlement":           {labCatalog(registeringProfile()), guest(), agent, [2]string{}},
	} {
		t.Run(name, func(t *testing.T) {
			profile, _ := test.catalog.Find(api.MachineInstallProfile, "rhel-9-8")
			reason, remediation := refusedRegistration(test.catalog, test.machine, profile, test.target, true)
			if [2]string{reason, remediation} != test.want {
				t.Fatalf("refusal = %q / %q, want %q", reason, remediation, test.want)
			}
		})
	}
}

func TestFromSubscriptionStillRefusesWithItsReason(t *testing.T) {
	profile := installProfile(field("installer", api.MapValue(field("anaconda", api.MapValue(
		text("imageRef", "rhel-9-8-boot"),
		field("redfishVirtualMedia", installProfile().Spec().Get("installer", "anaconda", "redfishVirtualMedia")),
		field("packageSource", api.MapValue(field("fromSubscription", api.MapValue(text("entitlementRef", "rhel"))))),
	)))))
	want := []lifecycle.Refusal{{Kind: "Machine", Name: "rhel-01",
		Reason:      "the install profile selects spec.installer.anaconda.packageSource.fromSubscription, which registers inside the installer, where a failed registration cannot be retried without installing again",
		Remediation: "remove spec.installer.anaconda.packageSource.fromSubscription from MachineInstallProfile/rhel-9-8, to install from its boot media or from spec.installer.anaconda.packageSource.hostedTree",
	}}
	if refused := Refusals(labCatalog(profile, rhelEntitlement())); !slices.Equal(refused, want) {
		t.Fatalf("refusals = %+v, want %+v", refused, want)
	}
}

func TestSubscriptionIsAdmittedOnAVirtualMachine(t *testing.T) {
	if refused := Refusals(labCatalog(registeringProfile(), rhelEntitlement())); len(refused) != 0 {
		t.Fatalf("refusals = %+v, want none", refused)
	}
}

func TestRegistrationMaterialsAreLentOnlyToApply(t *testing.T) {
	request := pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement()))
	lent := []lifecycle.MaterialFile{
		{Name: "rhsm-organization", Part: secrets.ValuePart, Secret: "rhel-org", Variable: "rhsmOrganization"},
		{Name: "rhsm-activation-key", Part: secrets.ValuePart, Secret: "rhel-key", Variable: "rhsmActivationKey"},
	}
	for _, operation := range []string{"apply", "observe", "observe-removal", "destroy"} {
		runner := &fakeRunner{}
		execution := registrationExecution(t, request, []byte(organizationSentinel+"\n"), []byte(activationSentinel))
		capability := New(runner)
		switch operation {
		case "apply":
			_, _ = capability.Apply(context.Background(), execution)
		case "observe":
			_, _ = capability.Observe(context.Background(), execution)
		case "observe-removal":
			_, _ = capability.ObserveRemoval(context.Background(), execution)
		default:
			_, _ = capability.Destroy(context.Background(), execution)
		}
		if len(runner.requests) != 1 {
			t.Fatalf("%s did not reach the adapter", operation)
		}
		run := runner.requests[0]
		var rhsm []lifecycle.MaterialFile
		for _, material := range run.Materials {
			if material.Secret == "rhel-org" || material.Secret == "rhel-key" {
				rhsm = append(rhsm, material)
			}
		}
		exposed := string(run.Canonical)
		for key, value := range run.MaterialValues {
			exposed += key + value
		}
		for _, refused := range run.Refusals {
			for _, reported := range diagnostics.Of(refused) {
				exposed += reported.Message + reported.Remediation
			}
		}
		if strings.Contains(exposed, organizationSentinel) || strings.Contains(exposed, activationSentinel) {
			t.Fatalf("%s exposes a registration value", operation)
		}
		if operation != "apply" {
			if len(rhsm) != 0 {
				t.Fatalf("%s lends %+v", operation, rhsm)
			}
			continue
		}
		assertMaterialsResolve(t, operation, run)
		if !slices.Equal(rhsm, lent) {
			t.Fatalf("apply lends %+v, want %+v", rhsm, lent)
		}
		for _, reason := range []string{"registration-input", "registration-foreign", "registration-refused", "registration-identity-collision", "registration-unavailable", "registration-repositories"} {
			if _, ok := run.Refusals[reason]; !ok {
				t.Fatalf("apply names no %s refusal: %v", reason, slices.Sorted(maps.Keys(run.Refusals)))
			}
		}
		for _, reason := range []string{"registration-unreachable", "transport"} {
			if _, ok := run.Refusals[reason]; ok {
				t.Fatalf("apply names the %s refusal", reason)
			}
		}
		reported := diagnostics.Of(run.Refusals["registration-repositories"])
		if len(reported) != 1 || reported[0].Code != "lifecycle.state" || reported[0].Object == nil || reported[0].Object.Name != "rhel-01" ||
			reported[0].Message != "the subscription service did not accept spec.customizations.repositories.subscription of MachineInstallProfile/rhel-9-8 on Machine/rhel-01" ||
			reported[0].Remediation != "correct the repository IDs in spec.customizations.repositories.subscription of MachineInstallProfile/rhel-9-8, then take this context back with bootwright destroy --context lab and run bootwright apply --context lab; a repository edit is another installation" {
			t.Fatalf("registration-repositories = %#v", reported)
		}
	}
	plain := pinnedRequest(t, labCatalog())
	if registrationMaterial("apply", plain) != nil || registrationRefusals(testContext, plain) != nil {
		t.Fatal("a request without a registration lends registration material")
	}
	if impacts := impacts(reconciliation.Apply, request); !slices.Contains(impacts, "register-subscription rhel-01") {
		t.Fatalf("impacts = %v", impacts)
	}
	if impacts := impacts(reconciliation.Destroy, request); slices.Contains(impacts, "register-subscription rhel-01") {
		t.Fatalf("destroy impacts = %v", impacts)
	}
}

func TestRegistrationValuesOutsideTheWorkerClassRefuseBeforeEffects(t *testing.T) {
	request := pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement()))
	for _, value := range []string{"", "a b", "a\tb", "a\nb", strings.Repeat("a", 256), "\x7f", "key\n\n"} {
		for _, secret := range []string{"rhel-org", "rhel-key"} {
			organization, key := []byte("org"), []byte("key")
			subject := "organization"
			if secret == "rhel-org" {
				organization = []byte(value)
			} else {
				key, subject = []byte(value), "activation key"
			}
			runner := &fakeRunner{}
			_, err := New(runner).Apply(context.Background(), registrationExecution(t, request, organization, key))
			reported := diagnostics.Of(err)
			want := "Secret/" + secret + " holds an " + subject + " for Entitlement/rhel that is not one line of 1 to 255 printable ASCII characters without whitespace, so this attempt changed nothing"
			if len(reported) != 1 || reported[0].Code != "api.value" || reported[0].Message != want ||
				reported[0].Remediation != "replace Secret/"+secret+" with one line of 1 to 255 printable ASCII characters without whitespace, then run bootwright apply --context lab again" {
				t.Fatalf("%q in %s: refusal = %#v", value, secret, reported)
			}
			if len(runner.requests) != 0 {
				t.Fatalf("%q in %s reached the adapter", value, secret)
			}
		}
	}
	for _, value := range []string{"key\n", strings.Repeat("a", 255), "!~"} {
		runner := &fakeRunner{}
		_, _ = New(runner).Apply(context.Background(), registrationExecution(t, request, []byte(value), []byte(value)))
		if len(runner.requests) != 1 {
			t.Fatalf("%q did not reach the adapter", value)
		}
	}
	runner := &fakeRunner{}
	_, err := New(runner).Apply(context.Background(), registrationExecution(t, request, []byte("org"), nil))
	if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "secret.store" ||
		reported[0].Message != "the bound activation key of Entitlement/rhel is not available to this attempt" || len(runner.requests) != 0 {
		t.Fatalf("an unbound activation key: refusal = %#v", reported)
	}
}

func registrationEvidence(t *testing.T, request Request, marker string, registration RegistrationEvidence, change func(*Evidence)) []byte {
	t.Helper()
	var evidence Evidence
	if err := json.Unmarshal(completeEvidence(request, "digest", marker), &evidence); err != nil {
		t.Fatal(err)
	}
	evidence.Registration = registration
	if change != nil {
		change(&evidence)
	}
	return encode(t, evidence)
}

func TestCompletionRequiresAnOwnedRegistration(t *testing.T) {
	request := pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement()))
	marker, _ := MarkerFor(request, "digest")
	for _, registration := range []RegistrationEvidence{{}, {State: "absent"}, {Consumer: ownedConsumer, State: "pending"}, {State: "foreign"}} {
		err := ValidatePresence(registrationEvidence(t, request, string(marker), registration, nil), request, "digest", string(marker))
		if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Code != "lifecycle.state" ||
			reported[0].Message != "the installed machine is not registered with a subscription consumer this installation created" {
			t.Fatalf("%+v: refusal = %#v", registration, reported)
		}
	}
	owned := RegistrationEvidence{Consumer: ownedConsumer, State: "owned"}
	if err := ValidatePresence(registrationEvidence(t, request, string(marker), owned, nil), request, "digest", string(marker)); err != nil {
		t.Fatalf("an owned registration refused: %v", diagnostics.Of(err))
	}
	plain := pinnedRequest(t, labCatalog())
	plainMarker, _ := MarkerFor(plain, "digest")
	if err := ValidatePresence(registrationEvidence(t, plain, string(plainMarker), RegistrationEvidence{}, nil), plain, "digest", string(plainMarker)); err != nil {
		t.Fatalf("an installation without a registration needs one: %v", diagnostics.Of(err))
	}
}

func unfinished(evidence *Evidence) { evidence.Postcondition = false }

func TestUnregisteredInstalledMachineIsAPositivePartial(t *testing.T) {
	request := pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement()))
	marker, _ := MarkerFor(request, "digest")
	for _, registration := range []RegistrationEvidence{{State: "absent"}, {Consumer: ownedConsumer, State: "pending"}} {
		evidence := registrationEvidence(t, request, string(marker), registration, unfinished)
		if effect, err := installationEffect(reconciliation.Apply, evidence, request, "digest", string(marker)); effect != reconciliation.EffectPartial {
			t.Fatalf("%+v: effect = %s (%v)", registration, effect, diagnostics.Of(err))
		}
	}
}

func TestForeignRegistrationStaysUnknown(t *testing.T) {
	request := pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement()))
	marker, _ := MarkerFor(request, "digest")
	evidence := registrationEvidence(t, request, string(marker), RegistrationEvidence{State: "foreign"}, unfinished)
	if effect, _ := installationEffect(reconciliation.Apply, evidence, request, "digest", string(marker)); effect != reconciliation.EffectUnknown {
		t.Fatalf("effect = %s", effect)
	}
	canonical, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{ID: request.Identity.Block, Object: "rhel-01", Request: canonical}, RequestDigest: "digest"}
	unresolved, explains := New(nil).Unresolved(reconciliation.Apply, block, evidence)
	if !explains || !strings.Contains(unresolved.Reason, "the installed machine is registered with a subscription consumer this installation did not create") {
		t.Fatalf("unresolved = %+v (%t)", unresolved, explains)
	}
}

func TestRepositoryEditMovesTheInstallationRequestAndMarker(t *testing.T) {
	frozen := func(profile api.Object) ([]byte, string, []byte) {
		request := pinnedRequest(t, labCatalog(profile, rhelEntitlement()))
		canonical, err := request.Canonical()
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(canonical)
		digest := string(sum[:])
		marker, err := MarkerFor(request, digest)
		if err != nil {
			t.Fatal(err)
		}
		return canonical, digest, marker
	}
	before, beforeDigest, beforeMarker := frozen(registeringProfile())
	after, afterDigest, afterMarker := frozen(registeringProfileEnabling("rhel-9-for-x86_64-baseos-rpms"))
	if string(before) == string(after) || beforeDigest == afterDigest || string(beforeMarker) == string(afterMarker) {
		t.Fatal("a repository edit kept the frozen request, its digest or its marker")
	}
}

func TestMalformedRegistrationEvidenceIsRefused(t *testing.T) {
	request := pinnedRequest(t, labCatalog(registeringProfile(), rhelEntitlement()))
	marker, _ := MarkerFor(request, "digest")
	for _, registration := range []RegistrationEvidence{
		{State: "registered"}, {State: "owned"}, {Consumer: "not-a-uuid", State: "owned"},
		{Consumer: strings.ToUpper(ownedConsumer), State: "pending"}, {Consumer: ownedConsumer, State: "absent"},
		{Consumer: ownedConsumer, State: "foreign"}, {Consumer: ownedConsumer},
	} {
		evidence := registrationEvidence(t, request, string(marker), registration, nil)
		_, err := decodeEvidence(evidence, "digest")
		if reported := diagnostics.Of(err); len(reported) != 1 || reported[0].Message != "the installation evidence reports a malformed registration" {
			t.Fatalf("%+v: refusal = %#v", registration, reported)
		}
	}
	for _, registration := range []RegistrationEvidence{{}, {State: "absent"}, {State: "foreign"}, {Consumer: ownedConsumer, State: "owned"}, {Consumer: ownedConsumer, State: "pending"}} {
		if _, err := decodeEvidence(registrationEvidence(t, request, string(marker), registration, nil), "digest"); err != nil {
			t.Fatalf("%+v refused: %v", registration, diagnostics.Of(err))
		}
	}
}
