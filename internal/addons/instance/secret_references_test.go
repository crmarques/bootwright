package instance

import (
	"slices"
	"testing"

	"github.com/crmarques/bootwright/internal/addons"
	co "github.com/crmarques/bootwright/internal/addons/clusterobjects"
)

func placedRequest(t *testing.T, secrets ...string) Request {
	t.Helper()
	request := fixtureRequest(t)
	program, err := request.Objects()
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range secrets {
		entry, err := co.NewPlacementEntry(co.SecretPlacement{Secret: secret, Namespace: "fixture-operator", Name: secret, Type: "Opaque", Keys: []co.KeyPart{{Key: "password", Part: "password"}}})
		if err != nil {
			t.Fatal(err)
		}
		program.Steps = append(program.Steps, co.Step{Kind: "place", Place: &entry})
	}
	if request.Program, err = co.Freeze(program, co.Options{Namespace: "fixture-operator"}); err != nil {
		t.Fatal(err)
	}
	request.Secrets = secrets
	return request
}

// An add-on instance references the Secrets its program places and no others.
func TestAnInstanceReportsTheSecretsItsProgramPlaces(t *testing.T) {
	for name, tc := range map[string]struct {
		secrets []string
		want    []string
	}{
		"none":    {},
		"one":     {secrets: []string{"credentials"}, want: []string{"credentials"}},
		"several": {secrets: []string{"alpha", "credentials"}, want: []string{"alpha", "credentials"}},
	} {
		t.Run(name, func(t *testing.T) {
			block := fixtureBlock(t, placedRequest(t, tc.secrets...))
			references, err := New(nil, addons.Registry{}, nil).SecretReferences(block)
			if err != nil || !slices.Equal(references, tc.want) {
				t.Fatalf("references = %v (%v), want %v", references, err, tc.want)
			}
		})
	}
}
