//go:build linux && amd64

package sshlocal

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/trust"
)

// fipsStylePolicy is a representative FIPS-style crypto-policy backend, not a
// verbatim vendor file. Its host-key list holds no Ed25519 and no SHA-1 RSA.
const fipsStylePolicy = "# generated\n" +
	"Ciphers aes256-gcm@openssh.com,aes256-ctr,aes128-gcm@openssh.com,aes128-ctr\n" +
	"KexAlgorithms ecdh-sha2-nistp256,ecdh-sha2-nistp384,ecdh-sha2-nistp521,diffie-hellman-group14-sha256\n" +
	"HostKeyAlgorithms ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,rsa-sha2-256,rsa-sha2-512\n" +
	"RequiredRSASize 2048\n"

const fipsStyleList = "ecdsa-sha2-nistp256,ecdsa-sha2-nistp384,ecdsa-sha2-nistp521,rsa-sha2-256,rsa-sha2-512"

type policyRig struct {
	launcher Launcher
	records  string
}

// policyClient stands in for the pinned client. Asked to evaluate (-G), it
// records its standard input and arguments and prints output; otherwise it
// records the session's arguments.
func policyClient(t *testing.T, output string, exit int, policy string) policyRig {
	t.Helper()
	records := t.TempDir()
	if err := os.WriteFile(filepath.Join(records, "output"), []byte(output), 0600); err != nil {
		t.Fatal(err)
	}
	dir := quoteWord(records)
	client := stubClient(t, `if [ "$1" = -G ]; then
 cat > `+dir+`/policy-stdin
 printf '%s\n' "$*" >> `+dir+`/evaluations
 cat `+dir+`/output
 exit `+strconv.Itoa(exit)+`
fi
printf '%s\n' "$*" >> `+dir+`/sessions
exit 0`)
	l := launcher(t, client)
	l.PolicyPath = filepath.Join(t.TempDir(), "openssh.config")
	if policy != "" {
		if err := os.WriteFile(l.PolicyPath, []byte(policy), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return policyRig{launcher: l, records: records}
}

func (r policyRig) recorded(t *testing.T, name string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(r.records, name))
	if os.IsNotExist(err) {
		return "", false
	}
	if err != nil {
		t.Fatal(err)
	}
	return string(data), true
}

func keyedSession(keyType string) machine.Session {
	s := keySession()
	s.HostKey = trust.HostKey{Type: keyType, PublicKey: hostKey().PublicKey}
	return s
}

func requireRefusal(t *testing.T, err error, want ...string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != "access.unavailable" || reported[0].Remediation == "" {
		t.Fatalf("refusal = %+v (%v)", reported, err)
	}
	text := reported[0].Message + " " + reported[0].Remediation
	for _, fragment := range want {
		if !strings.Contains(text, fragment) {
			t.Fatalf("refusal %q does not name %q", text, fragment)
		}
	}
	if strings.Contains(text, hostKey().PublicKey) {
		t.Fatalf("refusal carries key material: %q", text)
	}
}

func (r policyRig) requireNoSession(t *testing.T) {
	t.Helper()
	if sessions, ran := r.recorded(t, "sessions"); ran {
		t.Fatalf("a session ran: %s", sessions)
	}
	if entries, err := os.ReadDir(r.launcher.Scratch); err != nil || len(entries) != 0 {
		t.Fatalf("session material was created: %v (%v)", entries, err)
	}
}

func TestSessionRefusesAPinnedHostKeyThePolicyExcludes(t *testing.T) {
	rig := policyClient(t, "hostkeyalgorithms "+fipsStyleList+"\n", 0, fipsStylePolicy)
	_, err := rig.launcher.Run(context.Background(), keySession(), nil, os.Stderr, os.Stderr)
	requireRefusal(t, err, "Machine/rhel-01", "ssh-ed25519", rig.launcher.PolicyPath, "no session was opened")
	_, err = rig.launcher.HostKeyAlgorithms(context.Background(), keySession())
	requireRefusal(t, err, "Machine/rhel-01", "ssh-ed25519")
	rig.requireNoSession(t)
}

func TestSessionPinsOnlyTheIntersection(t *testing.T) {
	rig := policyClient(t, "hostkeyalgorithms "+fipsStyleList+"\n", 0, fipsStylePolicy)
	code, err := rig.launcher.Run(context.Background(), keyedSession("ssh-rsa"), nil, os.Stderr, os.Stderr)
	if err != nil || code != 0 {
		t.Fatalf("session = %d (%v)", code, err)
	}
	sessions, _ := rig.recorded(t, "sessions")
	if !strings.Contains(sessions, " HostKeyAlgorithms=rsa-sha2-512,rsa-sha2-256 ") || strings.Contains(sessions, ",ssh-rsa") {
		t.Fatalf("session arguments = %s", sessions)
	}
	pinned, err := rig.launcher.HostKeyAlgorithms(context.Background(), keyedSession("ssh-rsa"))
	if err != nil || pinned != "rsa-sha2-512,rsa-sha2-256" {
		t.Fatalf("admitted = %q (%v)", pinned, err)
	}
}

func TestSessionAcceptsAnECDSAKeyUnderFIPSPolicy(t *testing.T) {
	rig := policyClient(t, "hostkeyalgorithms "+fipsStyleList+"\n", 0, fipsStylePolicy)
	code, err := rig.launcher.Run(context.Background(), keyedSession("ecdsa-sha2-nistp256"), nil, os.Stderr, os.Stderr)
	if err != nil || code != 0 {
		t.Fatalf("session = %d (%v)", code, err)
	}
	sessions, _ := rig.recorded(t, "sessions")
	if strings.Count(sessions, "\n") != 1 || !strings.Contains(sessions, " HostKeyAlgorithms=ecdsa-sha2-nistp256 ") ||
		!strings.Contains(sessions, " StrictHostKeyChecking=yes ") {
		t.Fatalf("session arguments = %s", sessions)
	}
}

func TestSessionWithoutPolicyKeepsPinnedAlgorithms(t *testing.T) {
	for _, test := range []struct{ name, policy string }{
		{name: "absent backend"},
		{name: "no crypto directive", policy: "# generated\n\nServerAliveInterval 30\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			rig := policyClient(t, "", 1, test.policy)
			code, err := rig.launcher.Run(context.Background(), keySession(), nil, os.Stderr, os.Stderr)
			if err != nil || code != 0 {
				t.Fatalf("session = %d (%v)", code, err)
			}
			sessions, _ := rig.recorded(t, "sessions")
			if !strings.Contains(sessions, " HostKeyAlgorithms=ssh-ed25519 ") {
				t.Fatalf("session arguments = %s", sessions)
			}
			if evaluations, evaluated := rig.recorded(t, "evaluations"); evaluated {
				t.Fatalf("an absent policy was evaluated: %s", evaluations)
			}
		})
	}
}

func TestSessionPolicyEvaluationFailureRefuses(t *testing.T) {
	oversize := "hostkeyalgorithms " + fipsStyleList + "\n" +
		strings.Repeat("x", maxEvaluatedPolicyBytes+1-len("hostkeyalgorithms "+fipsStyleList+"\n"))
	for _, test := range []struct {
		name   string
		output string
		exit   int
		reason string
	}{
		{name: "non-zero exit", output: "hostkeyalgorithms " + fipsStyleList + "\n", exit: 1, reason: " -G failed"},
		{name: "malformed list", output: "hostkeyalgorithms ssh-ed25519;true\n",
			reason: " -G reported an unsupported host-key algorithm list"},
		{name: "missing list", output: "ciphers aes256-ctr\n", reason: " -G reported no host-key algorithm list"},
		{name: "oversize output", output: oversize, reason: " -G output exceeds 256 KiB"},
		{name: "empty member", output: "hostkeyalgorithms ecdsa-sha2-nistp256,,rsa-sha2-256\n",
			reason: " -G reported an unsupported host-key algorithm list"},
	} {
		t.Run(test.name, func(t *testing.T) {
			rig := policyClient(t, test.output, test.exit, fipsStylePolicy)
			session := keyedSession("ecdsa-sha2-nistp256")
			_, err := rig.launcher.Run(context.Background(), session, nil, os.Stderr, os.Stderr)
			requireRefusal(t, err, "cannot be evaluated", rig.launcher.Client+test.reason, "Machine/rhel-01",
				"update-crypto-policies --set")
			_, err = rig.launcher.HostKeyAlgorithms(context.Background(), session)
			requireRefusal(t, err, "cannot be evaluated", "Machine/rhel-01")
			rig.requireNoSession(t)
		})
	}
}

func TestThePolicyEvaluationReadsTheSessionsReducedConfiguration(t *testing.T) {
	rig := policyClient(t, "hostkeyalgorithms "+fipsStyleList+"\n", 0, fipsStylePolicy+"ServerAliveInterval 30\n")
	if _, err := rig.launcher.Run(context.Background(), keyedSession("ecdsa-sha2-nistp256"), nil, os.Stderr, os.Stderr); err != nil {
		t.Fatal(err)
	}
	reduced, err := cryptographicPolicy([]byte(fipsStylePolicy+"ServerAliveInterval 30\n"), true)
	if err != nil {
		t.Fatal(err)
	}
	if stdin, _ := rig.recorded(t, "policy-stdin"); stdin != string(reduced) {
		t.Fatalf("evaluated %q, want the reduced %q", stdin, reduced)
	}
	if evaluations, _ := rig.recorded(t, "evaluations"); evaluations != "-G -F /dev/stdin -o CanonicalizeHostname=no localhost\n" {
		t.Fatalf("evaluation arguments = %q", evaluations)
	}
	unsafe := policyClient(t, "hostkeyalgorithms "+fipsStyleList+"\n", 0, fipsStylePolicy+"ProxyCommand x\n")
	_, err = unsafe.launcher.Run(context.Background(), keyedSession("ecdsa-sha2-nistp256"), nil, os.Stderr, os.Stderr)
	requireRefusal(t, err, "ProxyCommand")
	if evaluations, evaluated := unsafe.recorded(t, "evaluations"); evaluated {
		t.Fatalf("an unsafe policy was evaluated: %s", evaluations)
	}
}
