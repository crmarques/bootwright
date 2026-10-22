package compilation_test

import (
	"context"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestMachineMediaSchemaParsesDeclaredMediaAndRefusesOtherValuesOrSubstrates(t *testing.T) {
	for _, test := range []struct{ variant, media, code, suffix string }{
		{"libvirt", "ssd", "", "media"}, {"libvirt", "SSD", "api.value", "media"}, {"libvirt", "hdd", "", "media"}, {"libvirt", "tape", "api.value", "media"},
		{"libvirt", "''", "api.value", "media"}, {"libvirt", "null", "api.type", "media"}, {"libvirt", "true", "api.type", "media"},
		{"vsphere", "ssd", "api.field", "media"}, {"kubevirt", "ssd", "api.field", "dataDisks"},
	} {
		t.Run(test.variant+"/"+test.media, func(t *testing.T) {
			body := environmentYAML + "  defaults:\n    InfraProvider:\n      " + test.variant + ":\n        machineProfiles:\n          - name: small\n            dataDisks: [{name: data, sizeGiB: 20, media: " + test.media + "}]\n"
			_, _, err := compiler().Compile(context.Background(), sources(body))
			if test.code == "" {
				if err != nil {
					t.Fatal("valid declared media refused", err)
				}
				return
			}
			for _, d := range diagnostics.Of(err) {
				if d.Code == test.code && strings.HasSuffix(d.Field, "."+test.suffix) {
					return
				}
			}
			t.Fatalf("wanted %s at %s; got %+v", test.code, test.suffix, diagnostics.Of(err))
		})
	}
}
