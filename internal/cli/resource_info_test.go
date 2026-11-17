package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine/inventory"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
)

func infoSecrets() *custody.ListResult {
	version := "version-opaque-id"
	return &custody.ListResult{Context: secretResultContext(), Secrets: []custody.ListRow{
		{Name: "alpha", Type: "token", Source: "contextStore", Parts: []secrets.Part{secrets.ValuePart}, State: "current", CurrentVersion: &version, CurrentSequence: 2, BoundVersions: 1},
		{Name: "alpha-two", Type: "sshKeyPair", Source: "generated", Parts: []secrets.Part{secrets.PrivateKeyPart, secrets.PublicKeyPart}, State: "orphaned", BoundVersions: 3},
	}}
}

func TestResourceInfoDispatchUsesOnlyMetadataAndLocalInventory(t *testing.T) {
	for _, kind := range []string{"machine", "secret"} {
		for _, format := range []string{"text", "json"} {
			t.Run(kind+"/"+format, func(t *testing.T) {
				record := &dispatchRecord{result: commandResult{machines: machineList(), secretList: infoSecrets(),
					secretReveal: &custody.RevealResult{Part: secrets.ValuePart, Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: []byte("do-not-reveal-this-fixture")})}}}
				defer record.result.secretReveal.Material.Clear()
				var out, errOut bytes.Buffer
				code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record)}).Run(context.Background(), []string{kind, "info", "--context", "example", "--output", format})
				if code != 0 || errOut.Len() != 0 || record.calls != 1 || record.path != kind+" list" {
					t.Fatalf("code=%d calls=%d path=%s stdout=%q stderr=%q", code, record.calls, record.path, out.String(), errOut.String())
				}
				if kind == "machine" {
					request, ok := record.request.(inventory.ListRequest)
					if !ok || request.ContextName != "example" || request.Power || request.Clusters != nil {
						t.Fatalf("request = %#v", record.request)
					}
				} else if !reflect.DeepEqual(record.request, custody.ListRequest{ContextName: "example"}) {
					t.Fatalf("request = %#v", record.request)
				}
				if strings.Contains(out.String(), "do-not-reveal-this-fixture") {
					t.Fatal("metadata view included material")
				}
				if format == "json" {
					var envelope struct {
						Command string
						OK      bool
						Result  map[string]json.RawMessage
					}
					if err := json.Unmarshal(out.Bytes(), &envelope); err != nil || envelope.Command != kind+" info" || !envelope.OK {
						t.Fatalf("envelope=%q: %v", out.String(), err)
					}
				}
			})
		}
	}
}

func TestResourceInfoPassesTheNormalizedNamesToItsService(t *testing.T) {
	for _, test := range []struct {
		selector string
		supplied bool
		want     []string
	}{
		{want: nil},
		{selector: " alpha , alpha , ", supplied: true, want: []string{"alpha"}},
		{selector: "alpha-two,alpha", supplied: true, want: []string{"alpha-two", "alpha"}},
	} {
		for _, kind := range []string{"machine", "secret"} {
			t.Run(kind+"/"+test.selector, func(t *testing.T) {
				record := &dispatchRecord{result: commandResult{machines: machineList(), secretList: infoSecrets()}}
				args := []string{kind, "info", "--output", "json"}
				if test.supplied {
					args = append(args, "--name", test.selector)
				}
				code, stdout, stderr := runSecretResult(args, record)
				if code != 0 || stderr != "" || record.calls != 1 {
					t.Fatalf("code=%d calls=%d stdout=%q stderr=%q", code, record.calls, stdout, stderr)
				}
				var got []string
				if request, ok := record.request.(inventory.ListRequest); ok {
					got = request.Names
				} else if request, ok := record.request.(custody.ListRequest); ok {
					got = request.Names
				} else {
					t.Fatalf("request = %#v", record.request)
				}
				if !slices.Equal(got, test.want) || (got == nil) != (test.want == nil) {
					t.Fatalf("names=%#v want=%#v", got, test.want)
				}
			})
		}
	}
}

func TestResourceInfoRendersAServiceRefusalOfAnUnknownName(t *testing.T) {
	for _, kind := range []string{"machine", "secret"} {
		refusal := diagnostics.UnknownNames(strings.ToUpper(kind[:1])+kind[1:], "example", []string{"alp"}, []string{"alpha"})
		record := &dispatchRecord{err: refusal}
		code, stdout, stderr := runSecretResult([]string{kind, "info", "--name", "alp"}, record)
		text := strings.Join(strings.Fields(stdout+stderr), " ")
		if code != 1 || record.calls != 1 {
			t.Fatalf("%s: code=%d calls=%d stdout=%q stderr=%q", kind, code, record.calls, stdout, stderr)
		}
		for _, want := range []string{"access.target", "the context example selects no " + strings.ToUpper(kind[:1]) + kind[1:] + " named alp", "bootwright " + kind + " list --context example"} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s: missing %q in stdout=%q stderr=%q", kind, want, stdout, stderr)
			}
		}
	}
}

func TestResourceInfoSeparatorOnlyNamesAreAUsageError(t *testing.T) {
	for _, kind := range []string{"machine", "secret"} {
		code, _, stderr, record := runRecorded([]string{kind, "info", "--name", " , "})
		if code != 2 || record.calls != 0 || !strings.Contains(stderr, "--name must name at least one "+kind) {
			t.Fatalf("%s: code=%d calls=%d stderr=%q", kind, code, record.calls, stderr)
		}
	}
}

func TestResourceInfoRejectsEffectAndRevealFlags(t *testing.T) {
	for _, args := range [][]string{
		{"machine", "info", "--name", ""}, {"secret", "info", "--name", ""},
		{"machine", "info", "--power-status"}, {"secret", "info", "--secrets"},
		{"secret", "info", "--part", "private-key"},
	} {
		code, _, _, record := runRecorded(args)
		if code != 2 || record.calls != 0 {
			t.Fatalf("%q: code=%d calls=%d", args, code, record.calls)
		}
	}
}
