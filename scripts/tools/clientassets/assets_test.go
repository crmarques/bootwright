package clientassets

import (
	"strings"
	"testing"
)

func TestEveryDefaultClientHasAnHTTPSAssetAndOnlyArchivesNameAMember(t *testing.T) {
	archives := map[string]bool{"govc": true, "helm": true}
	for _, kind := range []string{"govc", "helm", "kubectl", "virtctl"} {
		asset, found := Of(kind, "1.2.3")
		if !found || !strings.HasPrefix(asset.URL, "https://") || !strings.Contains(asset.URL, "1.2.3") {
			t.Errorf("%s has asset %+v", kind, asset)
		}
		if archives[kind] != (asset.Member != "") || archives[kind] != strings.HasSuffix(asset.URL, ".tar.gz") {
			t.Errorf("%s names member %q for %s", kind, asset.Member, asset.URL)
		}
	}
	if _, found := Of("oc", "1.2.3"); found {
		t.Error("an unpinned kind has an asset")
	}
}
