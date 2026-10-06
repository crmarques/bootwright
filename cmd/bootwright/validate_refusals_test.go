//go:build linux && amd64

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatePrintsOneRefusalAlikeInTextAndJSON(t *testing.T) {
	root := t.TempDir()
	copySources(t, labExampleSources(t), root)
	machine := filepath.Join(root, "infra", "machines", "rhel-01.yaml")
	content, err := os.ReadFile(machine)
	if err != nil {
		t.Fatal(err)
	}
	misspelled := strings.Replace(string(content), "    installAddressRef: ip\n", "    installAdressRef: ip\n", 1)
	if misspelled == string(content) {
		t.Fatal("the example no longer declares the key this test misspells")
	}
	if err := os.WriteFile(machine, []byte(misspelled), 0o600); err != nil {
		t.Fatal(err)
	}
	const field = "$.spec.network.installAdressRef"
	const remediation = "rename it to installAddressRef"
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"validate", "-f", root}, &out, &errOut); code != 1 {
		t.Fatalf("validate exit = %d\nstdout=%s\nstderr=%s", code, out.String(), errOut.String())
	}
	text := out.String() + errOut.String()
	if !strings.Contains(text, "[FAIL] api.field") || !strings.Contains(text, "[Machine/rhel-01] ("+field+"); next: "+remediation+"\n") {
		t.Fatalf("text refusal lacks its object, field or next step:\n%s", text)
	}
	out.Reset()
	errOut.Reset()
	if code := run(context.Background(), []string{"validate", "-f", root, "--output", "json"}, &out, &errOut); code != 1 || errOut.Len() != 0 {
		t.Fatalf("validate --output json exit = %d\nstdout=%s\nstderr=%s", code, out.String(), errOut.String())
	}
	var envelope struct {
		Diagnostics []struct {
			Code, Field, Remediation string
			Object                   *struct{ Kind, Name string }
		}
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatal(err, out.String())
	}
	if len(envelope.Diagnostics) != 1 {
		t.Fatalf("JSON diagnostics = %+v, want the one refusal", envelope.Diagnostics)
	}
	d := envelope.Diagnostics[0]
	if d.Code != "api.field" || d.Field != field || d.Remediation != remediation || d.Object == nil || d.Object.Kind != "Machine" || d.Object.Name != "rhel-01" {
		t.Fatalf("JSON refusal = %+v", d)
	}
}
