package main

import (
	"context"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// A libvirt provider names the one address its emulated BMCs listen on, because
// no default names an address every hosted Machine's controller endpoint can
// reach. Its absence is reported once, by the schema, and no rule adds a second
// diagnostic for the same field. An IPv6 address compiles in the one spelling
// netip prints.
func TestALibvirtProviderMustNameItsEmulatedBMCAddress(t *testing.T) {
	const field = "$.spec.libvirt.bmcEmulationDefaults.bindAddress"
	host := strings.Replace(serviceHost, "[container-runtime]", "[container-runtime, libvirt]", 1)
	credential := "apiVersion: bootwright.io/v1alpha1\nkind: Secret\nmetadata: {name: emulated-bmc}\nspec: {type: usernamePassword}\n"
	provider := func(defaults string) string {
		return "apiVersion: bootwright.io/v1alpha1\nkind: InfraProvider\nmetadata: {name: lab}\nspec:\n  libvirt:\n    machineRef: service-host\n    uri: qemu:///system\n    bmcEmulationDefaults:\n" + defaults + "      auth: {credentialsRef: emulated-bmc}\n"
	}
	omitted := controllerInputs(serviceEnvironment, host, credential, provider(""))
	trustFailure(t, omitted, "api.required", field)
	_, _, err := wireCompiler().Compile(context.Background(), omitted)
	for _, diagnostic := range diagnostics.Of(err) {
		if diagnostic.Field == field && diagnostic.Code == "api.invariant" {
			t.Fatal("an absent address was refused twice", diagnostic)
		}
	}
	compileAcceptance(t, controllerInputs(serviceEnvironment, host, credential, provider("      bindAddress: 192.0.2.10\n")))
	state, _ := compileAcceptance(t, controllerInputs(serviceEnvironment, host, credential, provider("      bindAddress: '2001:DB8:0::10'\n")))
	if address := requireObject(t, state.Effective(), api.InfraProvider, "lab").Spec().Get("libvirt", "bmcEmulationDefaults", "bindAddress").Text(); address != "2001:db8::10" {
		t.Fatalf("an IPv6 listener compiled to %q", address)
	}
}
