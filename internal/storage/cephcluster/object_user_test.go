package cephcluster

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func objectUser(name string, pairs ...any) api.Object {
	spec := m("clusterRef", "storage", "objectGatewayRef", "objects")
	for i := 0; i < len(pairs); i += 2 {
		spec = spec.With(pairs[i].(string), v(pairs[i+1]))
	}
	return object(api.CephObjectUser, name, spec)
}

func userIssues(t *testing.T, user api.Object, catalog api.Catalog) []api.Issue {
	t.Helper()
	catalog = replace(catalog, user)
	return Validate(user, catalog)
}

func TestObjectUserAdmitsAGatewayOfItsCluster(t *testing.T) {
	catalog := gatewayCatalog(t)
	for _, user := range []api.Object{objectUser("app-writer"), objectUser("app-writer", "displayName", "App writer"), objectUser(strings.Repeat("u", 46)),
		objectUser("app-writer", "displayName", strings.Repeat("é", 128))} {
		if issues := userIssues(t, user, catalog); len(issues) != 0 {
			t.Fatalf("%s refused: %#v", user.Name(), issues)
		}
	}
}

func TestObjectUserReservesDashboard(t *testing.T) {
	exactlyOne(t, userIssues(t, objectUser("dashboard"), gatewayCatalog(t)), "$.metadata.name",
		"the RGW user ID dashboard is reserved: the mgr dashboard module creates a user of that ID in every realm",
		"rename the CephObjectUser; its metadata.name is its native RGW user ID")
}

func TestObjectUserNameHasNoBlockDerivedLimit(t *testing.T) {
	if issues := userIssues(t, objectUser(strings.Repeat("u", 63)), gatewayCatalog(t)); len(issues) != 0 {
		t.Fatalf("a 63-character user name was refused: %v", issues)
	}
}

func TestObjectUserDisplayNameNeverLooksLikeAnOption(t *testing.T) {
	for _, display := range []string{"-v", "--rgw_zone=x", "-"} {
		exactlyOne(t, userIssues(t, objectUser("app-writer", "displayName", display), gatewayCatalog(t)), "$.spec.displayName",
			"CephObjectUser app-writer has a display name that begins with -, which radosgw-admin reads as an option instead of the display name",
			"start spec.displayName with another character, or omit it to use metadata.name")
	}
	if found := issuesAt(userIssues(t, objectUser("app-writer", "displayName", "a-v"), gatewayCatalog(t)), "$.spec.displayName"); len(found) != 0 {
		t.Fatalf("a display name with an inner hyphen refused: %#v", found)
	}
}

func TestObjectUserRefusesAGatewayOfAnotherCluster(t *testing.T) {
	gateway, catalog := editGateway(t, gatewayCatalog(t), "clusterRef", "other")
	catalog = replace(catalog, gateway)
	found := issuesAt(userIssues(t, objectUser("app-writer"), catalog), "$.spec.objectGatewayRef")
	if len(found) != 1 || found[0].Message != "referenced storage child belongs to a different cluster" {
		t.Fatalf("a gateway of another cluster: %#v", found)
	}
}

func TestObjectUserDisplayNameIsBoundedText(t *testing.T) {
	const message, remedy = "an RGW display name is at most 256 bytes of text without control characters", "shorten spec.displayName and remove its control characters, or omit it to use metadata.name"
	for _, display := range []string{strings.Repeat("x", 257), "line\nbreak", "tab\there", strings.Repeat("é", 129)} {
		exactlyOne(t, userIssues(t, objectUser("app-writer", "displayName", display), gatewayCatalog(t)), "$.spec.displayName", message, remedy)
	}
	if found := issuesAt(userIssues(t, objectUser("app-writer", "displayName", strings.Repeat("x", 256)), gatewayCatalog(t)), "$.spec.displayName"); len(found) != 0 {
		t.Fatalf("a 256-byte display name refused: %#v", found)
	}
}
