//go:build linux && amd64

package main

import (
	"strings"
	"testing"
)

// Through the composed services, each flag set a Secret's declared type does
// not take is a usage failure that names the Secret, its type and the flags
// it takes, with the command bound to the context; a flag set it takes is
// stored.
func TestSecretSetFlagMistakesExitTwoNamingTheType(t *testing.T) {
	services, _, input, _ := contextFixture(t)
	addSecretInput(t, input, "secret.yaml", strings.Join([]string{
		secretDocument("opaque", "opaque", ""), secretDocument("token", "token", ""), secretDocument("docker", "dockerConfigJson", ""),
		secretDocument("password", "usernamePassword", ""), secretDocument("ca", "caBundle", ""), secretDocument("tls", "tlsCertificate", ""),
		secretDocument("ssh", "sshKeyPair", ""),
	}, "\n---\n"))
	contextRun(t, services, 0, "context", "init", "--name", "alpha", "--input-dir", input)
	file := addSecretInput(t, t.TempDir(), "value", "synthetic-flag-value")
	const values = "--value-file <path> or --value-stdin"
	for _, test := range []struct {
		name, kind, takes, remedy string
		wrong                     [][]string
	}{
		{"opaque", "opaque", values, "--value-file <path>", [][]string{{}, {"--certificate-file", file}, {"--username", "operator", "--value-file", file}, {"--value-file", file, "--password-stdin=false"}}},
		{"token", "token", values, "--value-file <path>", [][]string{{"--password-stdin"}, {"--private-key-file", file}}},
		{"docker", "dockerConfigJson", values, "--value-file <path>", [][]string{{"--public-key-file", file}}},
		{"password", "usernamePassword", "--username <username> with --password-file <path> or --password-stdin", "--username <username> --password-stdin", [][]string{{"--username", "operator"}, {"--password-file", file}, {"--value-file", file}}},
		{"ca", "caBundle", "--certificate-file <path>", "--certificate-file <path>", [][]string{{"--certificate-file", file, "--private-key-file", file}, {"--value-stdin"}}},
		{"tls", "tlsCertificate", "--certificate-file <path> --private-key-file <path>", "--certificate-file <path> --private-key-file <path>", [][]string{{"--certificate-file", file}, {"--private-key-file", file}}},
		{"ssh", "sshKeyPair", "--private-key-file <path> [--public-key-file <path>]", "--private-key-file <path> [--public-key-file <path>]", [][]string{{"--public-key-file", file}, {"--certificate-file", file, "--private-key-file", file}}},
	} {
		for _, flags := range test.wrong {
			args := append([]string{"secret", "set", "--name", test.name}, flags...)
			stdout, stderr := contextRun(t, services, 2, args...)
			want := "[FAIL] secret.input: Secret " + test.name + " is of type " + test.kind + "; secret set takes " + test.takes +
				" [Secret/" + test.name + "]; next: bootwright secret set --context alpha --name " + test.name + " " + test.remedy +
				"\nUsage: bootwright secret set [flags]\nRun 'bootwright help' for available commands.\n"
			if stdout != "" || stderr != want {
				t.Errorf("%v wrote %q and %q, want only %q", args, stdout, stderr, want)
			}
		}
	}
	_, stderr := contextRun(t, services, 2, "secret", "set", "--name", "password", "--username", "two words", "--password-file", file)
	if !strings.HasPrefix(stderr, "[FAIL] secret.input: Secret password takes a --username that is one nonempty UTF-8 line of at most 1 MiB with no whitespace or colon [Secret/password]; next: bootwright secret set --context alpha --name password ") {
		t.Errorf("an unusable --username gave %q", stderr)
	}
	contextRun(t, services, 0, "secret", "set", "--name", "opaque", "--value-file", file)
	contextRun(t, services, 0, "secret", "set", "--name", "password", "--username", "operator", "--password-file", file)
}
