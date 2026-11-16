package reconciliation

import "testing"

func TestContentDigestOfHashesTheNULJoinedParts(t *testing.T) {
	for name, test := range map[string]struct {
		parts []string
		want  string
	}{
		"two parts":  {[]string{"a", "b"}, "59b271ae1bbcb1d31d41929817f4b16fb439eb4f31520b5ad1d5ce98920a7138"},
		"one part":   {[]string{"only"}, "f905b19542ed08c9a9c26543cca32e5711d207dcffb81b4cdb44ce0b989431c9"},
		"no part":    {nil, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		"an empty a": {[]string{"a", ""}, ContentDigestOf("a\x00")},
	} {
		if got := ContentDigestOf(test.parts...); got != test.want {
			t.Errorf("%s: %s, want %s", name, got, test.want)
		}
	}
	if ContentDigestOf("a", "b") == ContentDigestOf("ab") {
		t.Fatal("a part boundary does not change the digest")
	}
}
