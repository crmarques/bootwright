package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	containeraccess "github.com/crmarques/bootwright/internal/containercluster/access"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/custody"
	"github.com/crmarques/bootwright/internal/secrets/encryption"
	"github.com/crmarques/bootwright/internal/secrets/secretstore"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
	"go.yaml.in/yaml/v3"
)

func structuredDocument(t *testing.T, output string) map[string]any {
	t.Helper()
	decoder := yaml.NewDecoder(strings.NewReader(output))
	var document map[string]any
	if err := decoder.Decode(&document); err != nil {
		t.Fatalf("invalid document: %v\n%s", err, output)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		t.Fatalf("extra document: %v %v", extra, err)
	}
	if document["schemaVersion"] != "v1alpha1" || strings.Contains(output, "\x1b") {
		t.Fatalf("invalid envelope: %s", output)
	}
	return document
}

func TestEveryCommandHasUniversalStructuredHelp(t *testing.T) {
	for _, spec := range commandCatalog() {
		for _, format := range []string{"json", "yaml"} {
			t.Run(spec.path+"/"+format, func(t *testing.T) {
				args := append([]string{"-o", format}, strings.Fields(spec.path)...)
				args = append(args, "--help")
				code, out, errOut, record := runRecorded(args)
				if code != 0 || errOut != "" || record.calls != 0 {
					t.Fatalf("code %d stderr %q calls %d", code, errOut, record.calls)
				}
				document := structuredDocument(t, out)
				result := document["result"].(map[string]any)
				target := spec.path
				if target == "help" {
					target = ""
				}
				if document["ok"] != true || result["command"] != target {
					t.Fatalf("help result: %s", out)
				}
			})
		}
	}
}

func TestStructuredPresentationAndUsageAreSingleDocuments(t *testing.T) {
	for _, invocation := range []struct {
		args []string
		code int
	}{
		{[]string{"version", "-oyaml"}, 0},
		{[]string{"help", "context", "init", "-o=json"}, 0},
		{[]string{"completion", "bash", "--output", "yaml"}, 0},
		{[]string{"render", "-o", "yaml"}, 0},
		{[]string{"--output", "yaml", "unknown"}, 2},
		{[]string{"version", "--bad", "-o", "yaml"}, 2},
		{[]string{"context", "init", "-o", "yaml"}, 2},
		{[]string{"version", "--width", "23", "-o", "json"}, 2},
	} {
		code, out, errOut, record := runRecorded(invocation.args)
		if code != invocation.code || errOut != "" || record.calls != 0 {
			t.Fatalf("%v: code %d stderr %q calls %d stdout %s", invocation.args, code, errOut, record.calls, out)
		}
		document := structuredDocument(t, out)
		if document["exitCode"] != code || document["ok"] != (code == 0) {
			t.Fatalf("%v: %s", invocation.args, out)
		}
	}
}

func TestStructuredSessionsRefuseBeforeOpening(t *testing.T) {
	for _, command := range []string{"machine rsh", "machine exec", "cluster rsh", "cluster exec"} {
		for _, format := range []string{"json", "yaml"} {
			args := append([]string{"--output", format}, strings.Fields(command)...)
			args = append(args, "--name", "demo")
			if strings.HasSuffix(command, "exec") {
				args = append(args, "--", "uptime")
			}
			code, out, errOut, record := runRecorded(args)
			if code != 2 || errOut != "" || record.calls != 0 {
				t.Fatalf("%v: %d %q %q calls %d", args, code, out, errOut, record.calls)
			}
			structuredDocument(t, out)
			if ClassifyInvocation(args).RequiresRoot {
				t.Fatalf("refused session acquired privilege: %v", args)
			}
		}
	}
	// Payload text must not select Bootwright's format, even on a usage failure.
	_, out, errOut, _ := runRecorded([]string{"machine", "exec", "uptime", "--output", "yaml"})
	if out != "" || !strings.Contains(errOut, "--name is required") {
		t.Fatalf("payload selected output: %q %q", out, errOut)
	}
}

func TestYAMLUsesJSONNamesAndPreservesIntegerPrecision(t *testing.T) {
	var out bytes.Buffer
	writer := WithPresentation(&out, PresentationOptions{Format: "yaml"})
	const size = int64(9007199254740993)
	if err := writeStructuredResult(writer, "media add", mediaMutationPresentation{Name: "demo.iso", Outcome: "stored", Size: size}, nil, nil, 0); err != nil {
		t.Fatal(err)
	}
	document := structuredDocument(t, out.String())
	result := document["result"].(map[string]any)
	if result["size"] != int(size) || result["name"] != "demo.iso" || result["sha256"] != nil {
		t.Fatal(result)
	}
	if document["logs"] == nil || document["diagnostics"] == nil {
		t.Fatal(document)
	}
	out.Reset()
	if err := writeDiagnostics(writer, io.Discard, "validate", []diagnostic{{Severity: "error", Code: "api.required", Message: "missing input"}}, 1, true, nil); err != nil {
		t.Fatal(err)
	}
	document = structuredDocument(t, out.String())
	if document["result"] != nil {
		t.Fatal(document)
	}
}

func TestStructuredContextAndPlanUseReturnedFacts(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		for _, test := range []struct {
			args   []string
			result commandResult
			field  string
		}{
			{[]string{"context", "list"}, commandResult{list: &contexts.ListResult{Contexts: []contexts.Summary{{Name: "demo", Mode: contexts.Ready, Current: true, Configured: true}}}}, "contexts"},
			{[]string{"context", "current"}, commandResult{current: &contexts.CurrentResult{Context: contexts.Summary{Name: "demo", Mode: contexts.Ready}}}, "context"},
			{[]string{"context", "use", "--name", "demo"}, commandResult{use: &contexts.UseResult{Context: contexts.Summary{Name: "demo", Mode: contexts.Ready}}}, "context"},
			{[]string{"context", "init", "--name", "demo"}, commandResult{admission: &contexts.AdmissionResult{Context: contexts.Summary{Name: "demo", Mode: contexts.Ready}}}, "filesCopied"},
			{[]string{"context", "update", "--name", "demo", "--input-dir", "input", "--yes"}, commandResult{admission: &contexts.AdmissionResult{Context: contexts.Summary{Name: "demo", Mode: contexts.Ready}, FilesCopied: 3, InputChanged: true}}, "filesCopied"},
			{[]string{"context", "delete", "--name", "demo", "--purge", "--yes"}, commandResult{deletion: &contexts.DeleteResult{Name: "demo", Outcome: "deleted", OrphansAbandoned: true}}, "orphansAbandoned"},
			{[]string{"plan"}, commandResult{lifecyclePlan: &lifecycle.PlanResult{Context: lifecycle.ContextIdentity{Name: "demo"}, Verb: "apply", Steps: []lifecycle.PlanStep{{ID: "prepare", Description: "Prepare demo", Stage: "machines", After: []int{1}, Wave: 2}}, Startable: 1, Deferred: 2, Waves: 2, Bound: 8, Receipt: lifecycle.Receipt{Operation: "none", Verb: "apply", State: "preview", Next: "apply"}}}, "steps"},
		} {
			args := append([]string{"--output", format}, test.args...)
			record := &dispatchRecord{result: test.result}
			code, out, errOut := runSecretResult(args, record)
			if code != 0 || errOut != "" || record.calls != 1 {
				t.Fatalf("%v: %d %s %q", args, code, out, errOut)
			}
			document := structuredDocument(t, out)
			result := document["result"].(map[string]any)
			if _, present := result[test.field]; !present {
				t.Fatalf("missing result fact %s: %s", test.field, out)
			}
		}
	}
}

func TestStructuredExportsPreserveBytesAndWarnings(t *testing.T) {
	for _, format := range []string{"text", "json", "yaml"} {
		value := []byte{0, 255, '\n', '\\', 128}
		material := secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: value})
		record := &dispatchRecord{result: commandResult{secretReveal: &custody.RevealResult{Material: material, Part: secrets.ValuePart}}}
		code, out, errOut := runSecretResult([]string{"secret", "show", "--name", "demo", "--part", "value", "--output", format}, record)
		if code != 0 || errOut != "" {
			t.Fatalf("%s: %d %q %q", format, code, out, errOut)
		}
		if format == "text" {
			if !bytes.Equal([]byte(out), value) {
				t.Fatalf("raw bytes changed: %q", out)
			}
		} else {
			document := structuredDocument(t, out)
			export := document["result"].(map[string]any)["export"].(map[string]any)
			decoded, err := base64.StdEncoding.DecodeString(export["content"].(string))
			if err != nil || export["encoding"] != "base64" || !bytes.Equal(decoded, value) {
				t.Fatalf("export changed: %s", out)
			}
		}
	}
	for _, format := range []string{"json", "yaml"} {
		value := []byte("apiVersion: v1\n")
		record := &dispatchRecord{result: commandResult{kubeconfig: &containeraccess.KubeconfigResult{Cluster: "demo", Unproved: true, Material: secrets.NewMaterial(map[secrets.Part][]byte{secrets.ValuePart: value})}}}
		code, out, errOut := runSecretResult([]string{"cluster", "kubeconfig", "--name", "demo", "--output", format}, record)
		if code != 0 || errOut != "" {
			t.Fatalf("%s: %d %q %q", format, code, out, errOut)
		}
		document := structuredDocument(t, out)
		if len(document["diagnostics"].([]any)) != 1 {
			t.Fatalf("warning missing: %s", out)
		}
		export := document["result"].(map[string]any)["export"].(map[string]any)
		if export["content"] != string(value) || export["encoding"] != "utf-8" {
			t.Fatalf("text bytes changed: %s", out)
		}
	}
}

func TestStructuredLifecycleFailuresAndInterruptionsKeepEvidence(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		for _, interrupted := range []bool{false, true} {
			for _, verb := range []string{"apply", "destroy"} {
				operation := &lifecycle.OperationResult{Context: lifecycle.ContextIdentity{Name: "demo"}, Verb: verb, Blocks: []lifecycle.BlockResult{{ID: "prepare", State: "unknown", Attempts: 1}}, Logs: []string{"operations/op-demo/logs/runtime.log"}, LogLocation: "/private/operator/logs", NextCommand: "bootwright " + verb + " --context demo", Receipt: lifecycle.Receipt{Operation: "op-demo", Verb: verb, State: "running", Next: "continue-" + verb}}
				record := &dispatchRecord{result: commandResult{lifecycleOperation: operation}, err: diagnostics.NewFailure("lifecycle.state", "operation did not settle", "")}
				var cancel context.CancelCauseFunc
				if interrupted {
					record.afterCall = func() { cancel(ErrInterrupted) }
				}
				var out, errOut bytes.Buffer
				runner := New(Config{Out: &out, ErrOut: &errOut, Services: dispatchSpies(record), BeginOperation: func(ctx context.Context) (context.Context, func()) {
					ctx, cancel = context.WithCancelCause(ctx)
					return ctx, func() { cancel(nil) }
				}})
				code := runner.Run(context.Background(), []string{verb, "--yes", "--output", format})
				want := 1
				if interrupted {
					want = 130
				}
				if code != want || errOut.Len() != 0 || strings.Contains(out.String(), operation.LogLocation) {
					t.Fatalf("%s: %d %s %q", format, code, out.String(), errOut.String())
				}
				document := structuredDocument(t, out.String())
				if document["exitCode"] != want || document["result"] == nil || len(document["logs"].([]any)) != 1 {
					t.Fatal(document)
				}
			}
		}
	}
}

func TestResolvePresentationPreservesInjectedCapabilities(t *testing.T) {
	columns := func() int { return 120 }
	options := ResolvePresentation([]string{"--color", "always", "version", "--width", "48", "--detail", "full", "--ascii", "-o", "yaml"}, PresentationOptions{Columns: columns, NoColor: true, Dumb: true})
	if options.Format != "yaml" || options.Color != "always" || options.Detail != "full" || options.Width != 48 || !options.ASCII || !options.NoColor || !options.Dumb || options.Columns() != 120 || options.Command != "version" {
		t.Fatalf("options %+v", options)
	}
	classification := ClassifyInvocation([]string{"context", "list", "-o", "yaml"})
	if !classification.RequiresRoot || !classification.JSON || classification.Format != "yaml" {
		t.Fatal(classification)
	}
	var out, errOut bytes.Buffer
	if code := classification.Diagnostic(&out, &errOut, diagnostic{Severity: "error", Code: "runtime.privilege", Message: "refused"}, 1); code != 1 || errOut.Len() != 0 {
		t.Fatalf("boundary %d %q", code, errOut.String())
	}
	structuredDocument(t, out.String())
}

func TestYAMLAcceptsRawEffectiveJSONWithoutRetypingStrings(t *testing.T) {
	var out bytes.Buffer
	writer := WithPresentation(&out, PresentationOptions{Format: "yaml"})
	if err := writeStructuredResult(writer, "render effective", effectiveResult{EffectiveState: json.RawMessage(`[{"string":"1e3","number":9007199254740993,"empty":[]}]`)}, nil, nil, 0); err != nil {
		t.Fatal(err)
	}
	document := structuredDocument(t, out.String())
	object := document["result"].(map[string]any)["effectiveState"].([]any)[0].(map[string]any)
	if object["string"] != "1e3" || object["number"] != int(9007199254740993) || object["empty"] == nil {
		t.Fatal(object)
	}
}

func TestStructuredControllerRetainsNextAndExecutionEvidence(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		for _, path := range []string{"setup", "preflight controller"} {
			for _, failed := range []bool{false, true} {
				report := controllerReport("ready", false)
				if path == "setup" {
					report.Outcome = "unchanged"
				}
				report.ContextName, report.Machine, report.Route, report.Next = "demo", "controller", "direct", "bootwright plan --context demo"
				report.Progress = []prerequisites.ActionProgress{{ID: "python-runtime", Phase: "observed", Outcome: "unchanged"}}
				report.LogLocation = "/private/operator/setup-log"
				record := &dispatchRecord{result: commandResult{controller: report}}
				want := 0
				if failed {
					report.Outcome = "not-ready"
					record.err = diagnostics.NewFailure("controller.prerequisites", "missing prerequisites", "")
					want = 1
				}
				args := append([]string{"--output", format}, strings.Fields(path)...)
				code, out, errOut := runSecretResult(args, record)
				if code != want || errOut != "" || strings.Contains(out, report.LogLocation) {
					t.Fatalf("%v: %d %s %q", args, code, out, errOut)
				}
				shown := structuredDocument(t, out)["result"].(map[string]any)
				if shown["next"] != report.Next || shown["context"] != "demo" || len(shown["progress"].([]any)) != 1 {
					t.Fatal(shown)
				}
			}
		}
	}
}

func TestStructuredMutationsUseMetadataResults(t *testing.T) {
	selection := secretstore.Selection{Type: "local-keyring", Store: secretResultComponent("local-v1"), KeyCustody: secretResultComponent("local-keyfile-v1")}
	for _, format := range []string{"json", "yaml"} {
		for _, test := range []struct {
			args   []string
			result commandResult
			field  string
		}{
			{[]string{"secret", "set", "--name", "demo", "--value-file", "value", "--yes"}, commandResult{secretMutation: &custody.MutationResult{Context: secretResultContext(), Name: "demo", Changed: 1, Parts: []secrets.Part{secrets.ValuePart}}}, "parts"},
			{[]string{"secret", "generate"}, commandResult{secretMutation: &custody.MutationResult{Context: secretResultContext(), Changed: 1, ChangedNames: []string{"demo"}}}, "changedNames"},
			{[]string{"secret", "delete", "--name", "demo", "--yes"}, commandResult{secretMutation: &custody.MutationResult{Context: secretResultContext(), Name: "demo", Changed: 1}}, "changed"},
			{[]string{"media", "add", "--name", "demo.iso", "--from-file", "image", "--yes"}, commandResult{mediaMutation: &media.MutationResult{Name: "demo.iso", Outcome: "stored", Size: 42}}, "size"},
			{[]string{"media", "delete", "--name", "demo.iso", "--yes"}, commandResult{mediaMutation: &media.MutationResult{Name: "demo.iso", Outcome: "deleted"}}, "outcome"},
			{[]string{"secret", "encryption", "init"}, commandResult{encryptionMutation: &encryption.MutationResult{Context: secretResultContext(), Implementation: selection, ActiveKey: "key-1", Changed: true}}, "implementation"},
			{[]string{"secret", "encryption", "rotate", "--yes"}, commandResult{encryptionMutation: &encryption.MutationResult{Context: secretResultContext(), Implementation: selection, ActiveKey: "key-2", Changed: true, RetiredKeys: []string{"key-1"}, ReencryptedParts: 2, ReencryptedVersions: 1}}, "reencryptedParts"},
			{[]string{"apply", "--yes"}, commandResult{lifecycleOperation: &lifecycle.OperationResult{Verb: "apply", Receipt: lifecycle.Receipt{Operation: "op-demo", Verb: "apply", State: "done", Next: "none"}}}, "receipt"},
			{[]string{"destroy", "--yes"}, commandResult{lifecycleOperation: &lifecycle.OperationResult{Verb: "destroy", Receipt: lifecycle.Receipt{Operation: "op-demo", Verb: "destroy", State: "done", Next: "none"}}}, "receipt"},
			{[]string{"setup", "--download-only", "--dest-dir", "wheels"}, commandResult{controller: &prerequisites.Report{Outcome: "downloaded", Export: &prerequisites.WheelExport{Directory: "wheels", Files: []prerequisites.ExportedWheel{{Filename: "demo.whl", SHA256: strings.Repeat("a", 64), Size: 42, Origin: prerequisites.OriginDownload}}}}}, "files"},
		} {
			record := &dispatchRecord{result: test.result}
			args := append([]string{"--output", format}, test.args...)
			code, out, errOut := runSecretResult(args, record)
			if code != 0 || errOut != "" || record.calls != 1 {
				t.Fatalf("%v: %d %s %q calls %d", args, code, out, errOut, record.calls)
			}
			shown := structuredDocument(t, out)["result"].(map[string]any)
			if _, present := shown[test.field]; !present {
				t.Fatalf("%v: missing %s: %s", args, test.field, out)
			}
		}
	}
}

func TestStructuredWriterFailuresDoNotAppendFallbacks(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		writer := &partialSecretWriter{}
		var errOut bytes.Buffer
		code := New(Config{Out: writer, ErrOut: &errOut}).Run(context.Background(), []string{"version", "--output", format})
		if code != 1 || errOut.Len() != 0 || strings.Contains(writer.String(), "runtime.internal") {
			t.Fatalf("%s: %d %q %q", format, code, writer.String(), errOut.String())
		}
	}
	for _, args := range [][]string{{"version", "--output", "invalid", "--help"}, {"help", "--output", "invalid"}} {
		code, out, errOut, record := runRecorded(args)
		if code != 0 || out == "" || errOut != "" || record.calls != 0 {
			t.Fatalf("explicit help precedence %v: %d %q %q", args, code, out, errOut)
		}
	}
}
