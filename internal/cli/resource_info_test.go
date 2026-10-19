package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

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

func TestResourceInfoNameSelectionIsOptionalExactAndNormalized(t *testing.T) {
	for _, test := range []struct {
		selector string
		supplied bool
		want     []string
	}{
		{want: []string{"alpha", "alpha-two"}},
		{selector: " alpha , alpha , ", supplied: true, want: []string{"alpha"}},
		{selector: "alpha-two", supplied: true, want: []string{"alpha-two"}},
	} {
		for _, kind := range []string{"machine", "secret"} {
			t.Run(kind+"/"+test.selector, func(t *testing.T) {
				machines := &inventory.ListResult{Context: "example", Machines: []inventory.MachineRow{{Name: "alpha", Lifecycle: "applied"}, {Name: "alpha-two", Lifecycle: "not-applied"}}}
				originalMachines, originalSecrets := selectMachineInfo(machines, nil), selectSecretInfo(infoSecrets(), nil)
				record := &dispatchRecord{result: commandResult{machines: machines, secretList: infoSecrets()}}
				args := []string{kind, "info", "--output", "json"}
				if test.supplied {
					args = append(args, "--name", test.selector)
				}
				code, stdout, stderr := runSecretResult(args, record)
				var envelope struct {
					Result struct {
						Machines []struct{ Name string }
						Secrets  []struct{ Name string }
					}
				}
				if err := json.Unmarshal([]byte(stdout), &envelope); code != 0 || stderr != "" || err != nil {
					t.Fatalf("code=%d stdout=%q stderr=%q error=%v", code, stdout, stderr, err)
				}
				got := []string{}
				for _, row := range envelope.Result.Machines {
					got = append(got, row.Name)
				}
				for _, row := range envelope.Result.Secrets {
					got = append(got, row.Name)
				}
				if !slices.Equal(got, test.want) {
					t.Fatalf("names=%q want=%q", got, test.want)
				}
				if !reflect.DeepEqual(machines, originalMachines) || !reflect.DeepEqual(record.result.secretList, originalSecrets) {
					t.Fatal("selection changed service-owned results")
				}
			})
		}
	}
}

func TestResourceInfoRefusesAnExplicitlyNamedUnknownResource(t *testing.T) {
	for _, test := range []struct {
		kind, selector, subject string
	}{
		{"machine", "alp", "no Machine named alp"},
		{"secret", "alpha, typo , alp", "no Secrets named typo, alp"},
		{"machine", "alpha,alp", "no Machine named alp"},
	} {
		t.Run(test.kind+"/"+test.selector, func(t *testing.T) {
			machines := &inventory.ListResult{Context: "example", Machines: []inventory.MachineRow{{Name: "alpha", Lifecycle: "applied"}, {Name: "alpha-two", Lifecycle: "not-applied"}}}
			record := &dispatchRecord{result: commandResult{machines: machines, secretList: infoSecrets()}}
			code, stdout, stderr := runSecretResult([]string{test.kind, "info", "--name", test.selector}, record)
			text := strings.Join(strings.Fields(stdout+stderr), " ")
			if code != 1 || record.calls != 1 {
				t.Fatalf("code=%d calls=%d stdout=%q stderr=%q", code, record.calls, stdout, stderr)
			}
			for _, want := range []string{"access.target", "the context example selects " + test.subject, "bootwright " + test.kind + " list --context example"} {
				if !strings.Contains(text, want) {
					t.Fatalf("missing %q in stdout=%q stderr=%q", want, stdout, stderr)
				}
			}
			if strings.Contains(text, "alpha-two") {
				t.Fatalf("a refusal listed a resource: %q", text)
			}
		})
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

func TestResourceInfoSelectionOwnsReachableMutableValues(t *testing.T) {
	machines := machineList()
	selected := selectMachineInfo(machines, []string{"guest"})
	selected.Machines[0].IPs[0] = "changed"
	selected.Machines[0].Clusters = append(selected.Machines[0].Clusters, "new")
	selected.Machines[0].Name = "changed"
	if machines.Machines[0].Name != "guest" || machines.Machines[0].IPs[0] != "198.51.100.20" || len(machines.Machines[0].Clusters) != 0 {
		t.Fatal("machine copy aliases the inventory")
	}
	stored := infoSecrets()
	selectedSecrets := selectSecretInfo(stored, []string{"alpha"})
	selectedSecrets.Secrets[0].Parts[0] = secrets.PasswordPart
	*selectedSecrets.Secrets[0].CurrentVersion = "changed"
	if stored.Secrets[0].Parts[0] != secrets.ValuePart || *stored.Secrets[0].CurrentVersion != "version-opaque-id" {
		t.Fatal("secret copy aliases custody metadata")
	}
	if selectMachineInfo(nil, nil) != nil || selectSecretInfo(nil, nil) != nil {
		t.Fatal("a missing result became presentable")
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
