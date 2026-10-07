package environment_test

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/environment"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
)

func controllerRouteCatalog(choice api.Value, proxy api.Value) (api.Object, api.Catalog) {
	declared := api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue().WithPath(api.StringValue("controller"), "controller", "machineRef"))
	controller := api.NewObject(api.Machine, "controller", api.Value{}, api.MapValue().
		WithPath(api.BoolValue(true), "os", "provided").WithPath(api.BoolValue(true), "access", "local").
		With("capabilities", api.StringList("container-runtime")).With("proxy", choice))
	objects := []api.Object{declared, controller}
	if proxy.Present() {
		objects = append(objects, api.NewObject(api.Proxy, "egress", api.Value{}, proxy))
	}
	return declared, api.NewCatalog(objects)
}

// The controller Machine's route follows the grammar every acquisition route
// shares, held where each value is declared: a Proxy's endpoints by its own
// admission and each bypass entry by the proxy choice that declares it. The
// Environment adds no refusal of its own, and the executable's limits on the
// route stay the controller's to refuse.
func TestTheControllerRouteGrammarIsHeldAtItsOwnObjects(t *testing.T) {
	external := func(connection api.Value) api.Value {
		return api.MapValue().With("management", api.StringValue("external")).With("connection", connection)
	}
	endpoint := api.MapValue().With("httpsProxy", api.StringValue("http://proxy.example.test:3128"))
	selects := func(bypass ...string) api.Value {
		choice := api.MapValue().With("proxyRef", api.StringValue("egress"))
		if len(bypass) != 0 {
			choice = choice.With("noProxy", api.StringList(bypass...))
		}
		return choice
	}
	for _, test := range []struct {
		name   string
		choice api.Value
		proxy  api.Value
		field  string
	}{
		{name: "a bypass entry outside the grammar", choice: selects(".example.test", "10.0.0.0/33"), proxy: external(endpoint), field: "$.spec.proxy.noProxy[1]"},
		{name: "a bypass entry longer than the stage's acquisition reads", choice: selects(".example.test", strings.Repeat("a", 1012)+".example.test"), proxy: external(endpoint), field: "$.spec.proxy.noProxy[1]"},
		{name: "an endpoint with a path", choice: selects(), proxy: external(api.MapValue().With("httpsProxy", api.StringValue("http://proxy.example.test:3128/path"))), field: "$.spec.connection.httpsProxy"},
		{name: "an HTTP endpoint with a query", choice: selects(), proxy: external(endpoint.With("httpProxy", api.StringValue("http://proxy.example.test:3128/?q=1"))), field: "$.spec.connection.httpProxy"},
		{name: "a valid route", choice: selects(".example.test", "192.0.2.0/24", "registry.example.test:443", strings.Repeat("a", 1011)+".example.test"), proxy: external(endpoint)},
		{name: "direct access", choice: api.MapValue().With("direct", api.MapValue())},
		{name: "a managed Proxy", choice: selects(), proxy: api.MapValue().With("management", api.StringValue("managed"))},
		{name: "a private trust bundle", choice: selects(), proxy: external(endpoint.With("trustBundleRef", api.StringValue("corporate-ca")))},
		{name: "an HTTP proxy alone", choice: selects(), proxy: external(api.MapValue().With("httpProxy", api.StringValue("http://proxy.example.test:3128")))},
	} {
		t.Run(test.name, func(t *testing.T) {
			declared, catalog := controllerRouteCatalog(test.choice, test.proxy)
			if issues := environment.Validate(declared, catalog); len(issues) != 0 {
				t.Fatalf("the Environment refused the route itself: %+v", issues)
			}
			var issues []api.Issue
			if proxy, found := catalog.Find(api.Proxy, "egress"); found && proxy.Spec().Get("management").Text() == "external" {
				for _, issue := range infrastructureservices.Validate(proxy, catalog) {
					if strings.HasPrefix(issue.Field, "$.spec.connection") {
						issues = append(issues, issue)
					}
				}
			}
			issues = append(issues, infrastructureservices.ValidateProxyChoice(test.choice, "$.spec.proxy")...)
			if test.field == "" {
				if len(issues) != 0 {
					t.Fatalf("issues = %+v", issues)
				}
				return
			}
			if len(issues) != 1 || issues[0].Code != "api.value" || issues[0].Field != test.field ||
				issues[0].Remediation == "" || strings.Contains(issues[0].Message, "example.test") {
				t.Fatalf("issues = %+v", issues)
			}
		})
	}
}
