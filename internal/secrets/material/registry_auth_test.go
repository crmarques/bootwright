package material

import (
	"encoding/base64"
	"encoding/json"
	"maps"
	"testing"
)

// The derived document holds exactly the origin's token from the pull secret,
// every other member and entry dropped, and the mirror's credential for its
// own location; a pull secret without the origin's token derives nothing.
func TestARegistryAuthDocumentHoldsTheOriginsTokenAndTheMirrorsCredentialAlone(t *testing.T) {
	pullSecret := []byte(`{"auths":{"cloud.openshift.com":{"auth":"Y2xvdWQ6dG9rZW4=","email":"owner@example.test"},` +
		`"quay.io":{"auth":"cXVheTp0b2tlbg==","email":"owner@example.test"},"registry.redhat.io":{"auth":"cmVkaGF0OnRva2Vu"}}}`)
	document, err := RegistryAuth(pullSecret, "quay.io", []byte("mirror"), []byte("s3cret"), "[2001:db8::20]:5000")
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Auths map[string]map[string]string `json:"auths"`
	}
	if err := json.Unmarshal(document, &parsed); err != nil {
		t.Fatalf("%s: %v", document, err)
	}
	want := map[string]map[string]string{"quay.io": {"auth": "cXVheTp0b2tlbg=="}, "[2001:db8::20]:5000": {"auth": base64.StdEncoding.EncodeToString([]byte("mirror:s3cret"))}}
	if !maps.EqualFunc(parsed.Auths, want, maps.Equal) {
		t.Fatalf("the document holds %v", parsed.Auths)
	}
	for name, secret := range map[string][]byte{
		"no origin":     []byte(`{"auths":{"registry.redhat.io":{"auth":"cmVkaGF0OnRva2Vu"}}}`),
		"not a string":  []byte(`{"auths":{"quay.io":{"auth":{"nested":true}}}}`),
		"escaped token": []byte(`{"auths":{"quay.io":{"auth":"a\"b"}}}`),
		"no document":   []byte(`not json`),
	} {
		if derived, err := RegistryAuth(secret, "quay.io", []byte("mirror"), []byte("s3cret"), "203.0.113.20:5000"); err == nil {
			t.Errorf("%s: derived %s", name, derived)
		}
	}
	if _, err := RegistryAuth(pullSecret, "quay.io", nil, []byte("s3cret"), "203.0.113.20:5000"); err == nil {
		t.Error("an empty mirror credential derived a document")
	}
}
