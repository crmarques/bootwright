package custody

import (
	"context"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
)

func TestListSelectsTheNamedSecretsAndRefusesAnUnknownName(t *testing.T) {
	s, _, _, _ := serviceFixture(t, declarationYAML("stored", "")+declarationYAML("generated", "  source: {generated: {}}\n"))
	ctx := context.Background()
	if _, err := s.Set(ctx, SetRequest{Name: "stored", Input: secrets.Input{ValueStdin: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Generate(ctx, GenerateRequest{}); err != nil {
		t.Fatal(err)
	}
	all, err := s.List(ctx, ListRequest{})
	if err != nil || len(all.Secrets) != 2 {
		t.Fatalf("an omitted selector must keep every Secret: %+v err=%v", all, err)
	}
	selected, err := s.List(ctx, ListRequest{Names: []string{"stored"}})
	if err != nil || len(selected.Secrets) != 1 || selected.Secrets[0].Name != "stored" {
		t.Fatalf("selected=%+v err=%v", selected, err)
	}
	_, err = s.List(ctx, ListRequest{Names: []string{"stored", "absent"}})
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "access.target" || reported[0].Message != "the context "+all.Context.Name+" selects no Secret named absent" {
		t.Fatalf("err = %v, diagnostics = %+v", err, reported)
	}
}
