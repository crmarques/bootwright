package environment_test

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/environment"
)

func value(fields ...any) api.Value {
	values := []api.FieldValue{}
	for index := 0; index+1 < len(fields); index += 2 {
		next, ok := fields[index+1].(api.Value)
		if !ok {
			next = api.StringValue(fields[index+1].(string))
		}
		values = append(values, api.FieldValue{Name: fields[index].(string), Value: next})
	}
	return api.MapValue(values...)
}

func mirroredEnvironment(artifacts api.Value, protocol, port string, issuer bool) (api.Object, api.Catalog) {
	declared := api.NewObject(api.Environment, "lab", api.Value{}, value("mirror", value("artifacts", artifacts)))
	generated := value()
	if issuer {
		generated = value("issuerRef", "mirror-ca")
	}
	return declared, api.NewCatalog([]api.Object{declared,
		api.NewObject(api.Machine, "mirror", api.Value{}, value("network", value("addresses", api.ListValue(value("name", "ip", "address", "203.0.113.20"))))),
		api.NewObject(api.ArtifactServer, "mirror", api.Value{}, value("management", "managed", "machineRef", "mirror", "tls", value("secretRef", "mirror-tls"),
			"listeners", api.ListValue(value("name", "served", "protocol", protocol, "port", api.IntegerValue(port))),
			"endpoints", api.ListValue(value("name", "ip", "listenerRef", "served", "addressRef", "ip")))),
		api.NewObject(api.Secret, "mirror-ca", api.Value{}, value("type", "caBundle", "source", value("generated", value()))),
		api.NewObject(api.Secret, "mirror-tls", api.Value{}, value("type", "tlsCertificate", "source", value("generated", generated))),
	})
}

// A managed artifact mirror is admitted only on an HTTPS endpoint at the
// default port, whose certificate an authority every consumer can trust
// issues, and an endpointRef never stands beside an operator's url.
func TestAdmissionHoldsAManagedArtifactMirrorToHTTPSOnPort443AndAnIssuer(t *testing.T) {
	managed := value("serverRef", "mirror", "endpointRef", "ip")
	for name, test := range map[string]struct {
		artifacts      api.Value
		protocol, port string
		issuer         bool
		field, says    string
	}{
		"admitted":         {managed, "https", "443", true, "", ""},
		"authored anchor":  {value("serverRef", "mirror", "endpointRef", "ip", "trustBundleRef", "mirror-ca"), "https", "443", false, "", ""},
		"plain HTTP":       {managed, "http", "80", true, "$.spec.mirror.artifacts.endpointRef", "serves HTTP"},
		"another port":     {managed, "https", "8443", true, "$.spec.mirror.artifacts.endpointRef", "default port 443"},
		"no issuer":        {managed, "https", "443", false, "$.spec.mirror.artifacts.serverRef", "names no generated caBundle issuer"},
		"no endpoint":      {value("serverRef", "mirror"), "https", "443", true, "$.spec.mirror.artifacts.endpointRef", ""},
		"endpoint and url": {value("url", "https://mirror.example.test/tools", "endpointRef", "ip"), "https", "443", true, "$.spec.mirror.artifacts.endpointRef", "names the operator's url"},
	} {
		declared, catalog := mirroredEnvironment(test.artifacts, test.protocol, test.port, test.issuer)
		issues := environment.Validate(declared, catalog, testClusterMembers)
		if test.field == "" {
			if len(issues) != 0 {
				t.Errorf("%s: refused %+v", name, issues)
			}
			continue
		}
		if !slices.ContainsFunc(issues, func(issue api.Issue) bool {
			return issue.Field == test.field && strings.Contains(issue.Message, test.says)
		}) {
			t.Errorf("%s: issues %+v name no %s refusal saying %q", name, issues, test.field, test.says)
		}
	}
}

func imageMirrored(selection api.Value, management string, issuer bool) (api.Object, api.Catalog) {
	declared := api.NewObject(api.Environment, "lab", api.Value{}, value("mirror", value("registry", selection)))
	generated := value()
	if issuer {
		generated = value("issuerRef", "mirror-ca")
	}
	registry := value("management", management, "machineRef", "mirror", "tls", value("secretRef", "mirror-tls"), "credentialsRef", "mirror-push",
		"endpoints", api.ListValue(value("name", "ip", "addressRef", "ip"), value("name", "fqdn", "addressRef", "ip")))
	if management == "external" {
		registry = value("management", "external", "url", "mirror.example.test")
	}
	return declared, api.NewCatalog([]api.Object{declared,
		api.NewObject(api.Machine, "mirror", api.Value{}, value("network", value("addresses", api.ListValue(value("name", "ip", "address", "203.0.113.20"))))),
		api.NewObject(api.Registry, "mirror", api.Value{}, registry),
		api.NewObject(api.Secret, "mirror-push", api.Value{}, value("type", "usernamePassword")),
		api.NewObject(api.Secret, "mirror-ca", api.Value{}, value("type", "caBundle", "source", value("generated", value()))),
		api.NewObject(api.Secret, "mirror-tls", api.Value{}, value("type", "tlsCertificate", "source", value("generated", generated))),
	})
}

// A managed image mirror names which of its Registry's endpoints consumers
// read, as every registry selection does, and its certificate an authority
// every consumer can trust issues; an external one takes no endpointRef.
func TestAdmissionHoldsAManagedImageMirrorToAnEndpointAndAnIssuer(t *testing.T) {
	for name, test := range map[string]struct {
		selection   api.Value
		management  string
		issuer      bool
		field, says string
	}{
		"admitted":          {value("registryRef", "mirror", "endpointRef", "ip"), "managed", true, "", ""},
		"external":          {value("registryRef", "mirror"), "external", false, "", ""},
		"no endpoint":       {value("registryRef", "mirror"), "managed", true, "$.spec.mirror.registry.endpointRef", "requires an endpointRef"},
		"external endpoint": {value("registryRef", "mirror", "endpointRef", "ip"), "external", false, "$.spec.mirror.registry.endpointRef", "forbid endpointRef"},
		"no issuer":         {value("registryRef", "mirror", "endpointRef", "ip"), "managed", false, "$.spec.mirror.registry.registryRef", "names no generated caBundle issuer"},
	} {
		declared, catalog := imageMirrored(test.selection, test.management, test.issuer)
		issues := environment.Validate(declared, catalog, testClusterMembers)
		if test.field == "" {
			if len(issues) != 0 {
				t.Errorf("%s: refused %+v", name, issues)
			}
			continue
		}
		if !slices.ContainsFunc(issues, func(issue api.Issue) bool {
			return issue.Field == test.field && strings.Contains(issue.Message, test.says)
		}) {
			t.Errorf("%s: issues %+v name no %s refusal saying %q", name, issues, test.field, test.says)
		}
	}
}
