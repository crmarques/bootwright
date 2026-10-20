package addons

import (
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestCatalogImageNormalizationChangesOnlyDigest(t *testing.T) {
	for _, image := range []string{"registry.example.test/catalog:VersionA", "registry.example.test/catalog@SHA256:" + strings.Repeat("AB", 32)} {
		o := obj(api.ClusterAddon, "addon", m("olm", m("catalogSource", m("image", s(image)))))
		normalized, issues := Normalize(o, api.NewCatalog(nil))
		if len(issues) != 0 || normalized.Spec().Get("olm", "catalogSource", "image").Text() != api.CanonicalImage(image) || o.Spec().Get("olm", "catalogSource", "image").Text() != image {
			t.Fatal("digest normalization or authored preservation", normalized, issues)
		}
		again, _ := Normalize(normalized, api.NewCatalog(nil))
		if !again.Value().Equal(normalized.Value()) {
			t.Fatal("normalization is not idempotent")
		}
	}
}
