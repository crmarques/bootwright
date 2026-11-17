package acquisition

import "testing"

func TestAnImageReferenceSplitsAndQualifiesOnce(t *testing.T) {
	digest := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for name, test := range map[string]struct {
		reference                       string
		repository, tag, digest, qualed string
	}{
		"registry digest":   {"quay.io/org/image@sha256:" + digest, "quay.io/org/image", "", digest, "quay.io/org/image"},
		"tag and digest":    {"quay.io/org/image:1.2@sha256:" + digest, "quay.io/org/image", "1.2", digest, "quay.io/org/image"},
		"short name":        {"library/nginx@sha256:" + digest, "library/nginx", "", digest, "docker.io/library/nginx"},
		"bare name":         {"nginx:1.25", "nginx", "1.25", "", "docker.io/nginx"},
		"localhost":         {"localhost/image:dev", "localhost/image", "dev", "", "localhost/image"},
		"registry port":     {"registry.test:5000/image", "registry.test:5000/image", "", "", "registry.test:5000/image"},
		"port and tag":      {"registry.test:5000/image:v1", "registry.test:5000/image", "v1", "", "registry.test:5000/image"},
		"already qualified": {"docker.io/library/nginx", "docker.io/library/nginx", "", "", "docker.io/library/nginx"},
	} {
		parsed := ParseImageReference(test.reference)
		if parsed.Repository != test.repository || parsed.Tag != test.tag || parsed.Digest != test.digest {
			t.Errorf("%s: parsed %+v", name, parsed)
		}
		if got := parsed.Qualified().Repository; got != test.qualed {
			t.Errorf("%s: qualified %q, want %q", name, got, test.qualed)
		}
		if got := parsed.String(); got != test.reference {
			t.Errorf("%s: String() = %q, want %q", name, got, test.reference)
		}
	}
	if got := ParseImageReference("quay.io/org/image:1@sha256:" + digest).Pinned(); got != "quay.io/org/image@sha256:"+digest {
		t.Errorf("pinned = %q", got)
	}
}
