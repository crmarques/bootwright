package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	stateencoding "github.com/crmarques/bootwright/internal/desiredstate/encoding"
	"github.com/spf13/cobra"
)

func TestCommandResultFieldRegistryMatchesSpec(t *testing.T) {
	registry := map[string][]reflect.Type{}
	used := map[reflect.Type]bool{}
	for _, row := range []struct {
		typ   reflect.Type
		paths string
	}{
		{reflect.TypeFor[addOnInfoDocument](), "add-ons info"},
		{reflect.TypeFor[addOnsPresentation](), "add-ons list"},
		{reflect.TypeFor[controllerPresentation](), "controller install|controller check"},
		{reflect.TypeFor[setupPlanResult](), "controller install"},
		{reflect.TypeFor[rebindPresentation](), "controller rebind"},
		{reflect.TypeFor[uninstallPresentation](), "controller uninstall"},
		{reflect.TypeFor[contextPresentation](), "context use|context current"},
		{reflect.TypeFor[contextListPresentation](), "context list"},
		{reflect.TypeFor[contextAdmissionPresentation](), "context init|context update"},
		{reflect.TypeFor[contextDeletionPresentation](), "context delete"},
		{reflect.TypeFor[secretMutationPresentation](), "secret set|secret generate|secret delete"},
		{reflect.TypeFor[mediaMutationPresentation](), "media add|media delete"},
		{reflect.TypeFor[encryptionMutationPresentation](), "secret encryption init|secret encryption rotate"},
		{reflect.TypeFor[exportResult](), "completion bash|completion fish|completion zsh|completion powershell|secret show|cluster kubeconfig|cluster keyring"},
		{reflect.TypeFor[helpPresentation](), "help"},
		{reflect.TypeFor[versionPresentation](), "version"},
		{reflect.TypeFor[schemaPresentation](), "schema"},
		{reflect.TypeFor[billOfMaterialsPresentation](), "sbom"},
		{reflect.TypeFor[acquisitionsPresentation](), "render acquisitions"},
		{reflect.TypeFor[planPresentation](), "plan"},
		{reflect.TypeFor[operationPresentation](), "apply|destroy"},
		{reflect.TypeFor[validationResult](), "validate"},
		{reflect.TypeFor[storagePreflightResult](), "preflight"},
		{reflect.TypeFor[effectiveResult](), "render effective"},
		{reflect.TypeFor[renderPresentation](), "render effective"},
		{reflect.TypeFor[statusResult](), "status"},
		{reflect.TypeFor[diffResult](), "diff"},
		{reflect.TypeFor[secretCheckResult](), "secret check"},
		{reflect.TypeFor[secretListResult](), "secret list"},
		{reflect.TypeFor[secretInfoResult](), "secret info"},
		{reflect.TypeFor[encryptionStatusResult](), "secret encryption status"},
		{reflect.TypeFor[trustResult](), "machine trust"},
		{reflect.TypeFor[machineListPresentation](), "machine list"},
		{reflect.TypeFor[machineInfoPresentation](), "machine info"},
		{reflect.TypeFor[machinePowerPresentation](), "machine start|machine stop|machine restart"},
		{reflect.TypeFor[machineCopyPresentation](), "machine cp"},
		{reflect.TypeFor[mediaListPresentation](), "media list"},
		{reflect.TypeFor[clusterListPresentation](), "cluster list"},
		{reflect.TypeFor[clusterInfoPresentation](), "cluster info"},
		{reflect.TypeFor[logsResult](), "logs"},
		{reflect.TypeFor[logsOutputResult](), "logs"},
	} {
		if !slices.Contains(cliResults, row.typ) {
			t.Fatalf("registry names an unregistered result type %s", row.typ)
		}
		used[row.typ] = true
		for _, path := range strings.Split(row.paths, "|") {
			registry[path] = append(registry[path], row.typ)
		}
	}
	for _, typ := range cliResults {
		if !used[typ] {
			t.Errorf("result %s has no command in the field registry", typ)
		}
	}
	root, err := newCommandTree(New(Config{}))
	if err != nil {
		t.Fatal(err)
	}
	available := map[string]bool{}
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		path := strings.TrimSpace(strings.TrimPrefix(command.CommandPath(), "bootwright"))
		spec := specFor(path)
		if len(command.Commands()) == 0 && !command.Hidden && !spec.session && (spec.implemented || presentationCommand(path)) {
			available[path] = true
			if len(registry[path]) == 0 {
				t.Errorf("available command %q has no result-field row", path)
			}
		}
		for _, child := range command.Commands() {
			walk(child)
		}
	}
	walk(root)
	for path := range registry {
		if !available[path] {
			t.Errorf("registry command %q is absent, unavailable or a session", path)
		}
	}
	for _, fixture := range cliGoldens() {
		path := fixture.path()
		variants := registry[path]
		words := strings.Fields(fixture.args)
		// These modes intentionally expose only a text stream.
		if fixture.code != 0 || len(variants) == 0 || slices.Contains(words, "--short") || slices.Contains(words, "--follow") {
			continue
		}
		t.Run("emitted/"+fixture.golden, func(t *testing.T) {
			record := &dispatchRecord{}
			if fixture.record != nil {
				fixture.record(record)
			}
			var out, errOut bytes.Buffer
			record.out = &out
			args := append([]string{"--output=json"}, words...)
			code := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record),
				EncodeEffectiveYAML: stateencoding.YAML, EncodeEffectiveJSON: stateencoding.JSON,
				Acquisitions: goldenAcquisitions}).Run(context.Background(), args)
			if code != 0 {
				t.Fatalf("structured fixture failed: %d, %s, %s", code, out.String(), errOut.String())
			}
			var document struct {
				Result map[string]json.RawMessage `json:"result"`
			}
			if err := json.Unmarshal(out.Bytes(), &document); err != nil {
				t.Fatal(err)
			}
			for _, typ := range variants {
				if resultRegistryMatches(document.Result, typ) {
					return
				}
			}
			t.Fatalf("%s emits fields outside its registered DTO variants: %s", path, out.String())
		})
	}
	grouped := map[string][]string{}
	for path, variants := range registry {
		fields := resultRegistryFields(variants)
		grouped[strings.Join(fields, ", ")] = append(grouped[strings.Join(fields, ", ")], path)
	}
	type row struct{ commands, fields string }
	rows := []row{}
	for fields, commands := range grouped {
		slices.Sort(commands)
		for i := range commands {
			commands[i] = string(rune(96)) + commands[i] + string(rune(96))
		}
		rows = append(rows, row{strings.Join(commands, ", "), fields})
	}
	slices.SortFunc(rows, func(a, b row) int { return strings.Compare(a.commands, b.commands) })
	var table strings.Builder
	table.WriteString("| Command | Result fields |\n| --- | --- |\n")
	for _, row := range rows {
		table.WriteString("| " + row.commands + " | " + row.fields + " |\n")
	}
	file := "../../specs/cli/output.md"
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	start := strings.Index(text, "| Command | Result fields |")
	if start < 0 {
		t.Fatal("result registry table is absent")
	}
	end := start + strings.Index(text[start:], "\n\n")
	want := strings.TrimSuffix(table.String(), "\n")
	if *update {
		if err := os.WriteFile(file, []byte(text[:start]+want+text[end:]), 0o644); err != nil {
			t.Fatal(err)
		}
		data, err = os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		text = string(data)
		end = start + strings.Index(text[start:], "\n\n")
	}
	if text[start:end] != want {
		t.Fatal("result fields differ from CLI DTO tags; run TestCommandResultFieldRegistryMatchesSpec with -update")
	}
}

func resultRegistryFields(variants []reflect.Type) []string {
	order := []string{}
	count := map[string]int{}
	optional := map[string]bool{}
	for _, typ := range variants {
		for i := range typ.NumField() {
			field := typ.Field(i)
			name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
			if !field.IsExported() || name == "" || name == "-" {
				continue
			}
			if count[name] == 0 {
				order = append(order, name)
			}
			count[name]++
			optional[name] = optional[name] || strings.Contains(options, "omitempty") || strings.Contains(options, "omitzero")
		}
	}
	for i, name := range order {
		order[i] = string(rune(96)) + name + string(rune(96))
		if optional[name] || count[name] != len(variants) {
			order[i] = "optional " + order[i]
		}
	}
	return order
}

func resultRegistryMatches(result map[string]json.RawMessage, typ reflect.Type) bool {
	if result == nil {
		return false
	}
	fields := map[string]bool{}
	for i := range typ.NumField() {
		field := typ.Field(i)
		name, options, _ := strings.Cut(field.Tag.Get("json"), ",")
		if !field.IsExported() || name == "" || name == "-" {
			continue
		}
		fields[name] = true
		if !strings.Contains(options, "omitempty") && !strings.Contains(options, "omitzero") {
			if _, present := result[name]; !present {
				return false
			}
		}
	}
	for name := range result {
		if !fields[name] {
			return false
		}
	}
	return true
}
