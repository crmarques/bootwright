package artifactcatalog

import (
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestRegistrationPinsAndCredentialFreeOrigins(t *testing.T) {
	valid := Entry{Name: "helm-linux-amd64", SHA256: strings.Repeat("a", 64), Size: 42}
	for _, origin := range []string{"", "https://example.test/helm", "https://mirror.example.test:443/helm"} {
		entry := valid
		entry.OriginURL = origin
		if !entry.Valid() {
			t.Errorf("refused source %q", origin)
		}
		if origin != "" && entry.Identity() == valid.Identity() {
			t.Errorf("source change did not change the identity for %q", origin)
		}
	}
	for _, origin := range []string{
		"http://example.test/helm", "https://user@example.test/helm", "https://example.test/helm?",
		"https://example.test/helm?signature=credential", "https://example.test/helm#", "https://example.test:444/helm",
		"https://example.test:/helm", "https:///helm", "https://example.test/" + strings.Repeat("a", 4096),
	} {
		entry := valid
		entry.OriginURL = origin
		if entry.Valid() {
			t.Errorf("accepted disallowed origin %.100q", origin)
		}
	}
	if ValidName("../archive") || ValidDigest(valid.SHA256[:63]) {
		t.Fatal("accepted an unsafe name or incomplete digest")
	}
}

func TestReferencesIncludeTheSavedHelmDefaultEvenWhenOverridden(t *testing.T) {
	toolSource := []string{"mirror", "artifacts", "toolSources", "helm", "catalogArtifact"}
	spec := api.MapValue().WithPath(api.StringValue("selected-archive"), toolSource...).
		WithPath(api.StringValue("default-archive"), append([]string{"defaults", "Environment"}, toolSource...)...)
	env := api.NewObject(api.Environment, "lab", api.Value{}, spec)
	without := api.NewObject(api.Environment, "another", api.Value{}, api.MapValue())
	got := References(api.NewCatalog([]api.Object{env, without}))
	slices.Sort(got)
	if !slices.Equal(got, []string{"default-archive", "selected-archive"}) {
		t.Fatalf("references = %v", got)
	}
}
