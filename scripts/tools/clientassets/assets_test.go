package clientassets

import (
	"errors"
	"fmt"
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

func TestTheEmbeddedBuildModulesArePinnedByDigestAndOnlyForKnownPublishers(t *testing.T) {
	pins, err := Modules()
	if err != nil || len(pins) == 0 {
		t.Fatalf("the embedded pins: %v, %v", pins, err)
	}
	for _, pin := range pins {
		if !strings.HasPrefix(pin.URL, "https://") || !strings.Contains(pin.URL, "v"+pin.Version+"/go.mod") {
			t.Errorf("%s %s names %s", pin.Kind, pin.Version, pin.URL)
		}
		if found, err := ModulesOf(pin.Kind, pin.Version); err != nil || found != pin {
			t.Errorf("ModulesOf(%s, %s) = %+v, %v", pin.Kind, pin.Version, found, err)
		}
	}
	if _, err := ModulesOf("virtctl", "0.0.1"); !errors.Is(err, ErrUnpinned) {
		t.Errorf("a release with no pinned go.mod resolved: %v", err)
	}
}

func TestABuildModulesFileWithoutADigestOrWithAnUnknownPublisherIsRefused(t *testing.T) {
	digest := strings.Repeat("a", 64)
	entry := func(kind, version, sum string, size int) string {
		return fmt.Sprintf(`{"format":%q,"releases":[{"kind":%q,"version":%q,"bytes":%d,"sha256":%q}]}`, ModulesFormat, kind, version, size, sum)
	}
	if _, err := ParseModules([]byte(entry("virtctl", "1.9.1", digest, 10))); err != nil {
		t.Fatalf("a complete pin failed: %v", err)
	}
	for name, document := range map[string]string{
		"no digest":         entry("virtctl", "1.9.1", "", 10),
		"a short digest":    entry("virtctl", "1.9.1", digest[:63], 10),
		"an upper digest":   entry("virtctl", "1.9.1", strings.ToUpper(digest), 10),
		"no size":           entry("virtctl", "1.9.1", digest, 0),
		"an unbounded size": entry("virtctl", "1.9.1", digest, 1<<21),
		"an unknown kind":   entry("helm", "4.3.1", digest, 10),
		"a prerelease":      entry("virtctl", "1.9.1-rc.1", digest, 10),
		"another format":    strings.Replace(entry("virtctl", "1.9.1", digest, 10), ModulesFormat, "other", 1),
		"an unknown field":  strings.Replace(entry("virtctl", "1.9.1", digest, 10), `"bytes"`, `"extra":1,"bytes"`, 1),
		"two documents":     entry("virtctl", "1.9.1", digest, 10) + entry("virtctl", "1.9.1", digest, 10),
		"a duplicate":       strings.Replace(entry("virtctl", "1.9.1", digest, 10), `}]}`, `},{"kind":"virtctl","version":"1.9.1","bytes":10,"sha256":"`+digest+`"}]}`, 1),
	} {
		if _, err := ParseModules([]byte(document)); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
}
