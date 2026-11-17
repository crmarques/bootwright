package v1alpha1

import (
	"strings"
	"testing"
)

func TestAContextNameIsALowercaseDNSLabel(t *testing.T) {
	for _, name := range []string{"lab", "lab-1", "a", strings.Repeat("a", 63)} {
		if parsed, ok := ParseContextName(name); !ok || string(parsed) != name {
			t.Errorf("%q was refused or changed to %q", name, parsed)
		}
	}
	for _, name := range []string{"", "Lab", "-lab", "lab-", "lab.one", " lab", "lab\n", strings.Repeat("a", 64)} {
		if _, ok := ParseContextName(name); ok {
			t.Errorf("%q was admitted", name)
		}
	}
}
