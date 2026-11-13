package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestResolutionFailuresNameTheUnknownInput(t *testing.T) {
	for _, test := range []struct {
		name, command, message string
		args                   []string
	}{
		{"flag", "context delete", `unknown flag: "--prune"`, []string{"context", "delete", "--name", "demo", "--prune", "--yes"}},
		{"attached value", "context delete", `unknown flag: "--prune"`, []string{"context", "delete", "--prune=private-value"}},
		{"separate value", "context delete", `unknown flag: "--prune"`, []string{"context", "delete", "--prune", "private-value"}},
		{"short flag", "version", `unknown flag: "-x"`, []string{"version", "-xprivate-value"}},
		{"explicit help", "context delete", `unknown flag: "--prune"`, []string{"context", "delete", "--help", "--prune"}},
		{"root command", "", `unknown command: "contxt"`, []string{"contxt", "private-value"}},
		{"subcommand", "context", `unknown command: "deleet"`, []string{"context", "deleet", "private-value"}},
		{"help command", "help", `unknown help command: "contxt"`, []string{"help", "contxt", "private-value"}},
		{"help subcommand", "help", `unknown help command: "deleet"`, []string{"help", "context", "deleet", "private-value"}},
		{"flag controls", "version", `unknown flag: "--bad\\n\\x1b"`, []string{"version", "--bad\n\x1b=private-value"}},
		{"command controls", "", `unknown command: "bad\\n\\x1b"`, []string{"bad\n\x1b", "private-value"}},
	} {
		for _, format := range []string{"text", "json", "yaml"} {
			t.Run(test.name+"/"+format, func(t *testing.T) {
				args := append([]string{"--output", format}, test.args...)
				code, out, errOut, record := runRecorded(args)
				if code != 2 || record.calls != 0 || strings.Contains(out+errOut, "private-value") {
					t.Fatalf("code=%d out=%q stderr=%q calls=%d", code, out, errOut, record.calls)
				}
				if format == "text" {
					if out != "" || !strings.HasPrefix(errOut, "  error  cli.usage\n    "+test.message+"\n") || !strings.Contains(errOut, "Usage: bootwright") {
						t.Fatalf("out=%q stderr=%q", out, errOut)
					}
					return
				}
				var envelope commandEnvelope
				data := []byte(out)
				if format == "yaml" {
					var err error
					data, err = json.Marshal(structuredDocument(t, out))
					if err != nil {
						t.Fatal(err)
					}
				}
				if err := json.Unmarshal(data, &envelope); err != nil {
					t.Fatal(err)
				}
				if errOut != "" || envelope.Command != test.command || envelope.OK || envelope.ExitCode != 2 || envelope.Result != nil || len(envelope.Logs) != 0 || len(envelope.Diagnostics) != 1 || envelope.Diagnostics[0].Code != "cli.usage" || envelope.Diagnostics[0].Message != test.message {
					t.Fatalf("out=%q stderr=%q", out, errOut)
				}
			})
		}
	}
}
