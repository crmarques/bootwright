package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/runtime/sshtrust"
)

// hostTrustScanDeps fakes ssh-keyscan with a fixed address -> public-key map;
// scanning an address outside the map fails.
func hostTrustScanDeps(keys map[string]string) hostTrustDeps {
	return hostTrustDeps{
		lookPath: func(name string, _ []string) (string, error) {
			if name != "ssh-keyscan" {
				return "", errors.New("unexpected lookup " + name)
			}
			return "/usr/bin/ssh-keyscan", nil
		},
		scan: func(_ context.Context, address string, _ time.Duration) ([]sshtrust.ScannedKey, error) {
			publicKey, ok := keys[address]
			if !ok {
				return nil, errors.New("unexpected scan " + address)
			}
			fingerprint, err := sshtrust.FingerprintSHA256(publicKey)
			if err != nil {
				return nil, err
			}
			return []sshtrust.ScannedKey{{
				Address:           address,
				KeyType:           "ssh-ed25519",
				PublicKey:         publicKey,
				FingerprintSHA256: fingerprint,
				KnownHostsLine:    sshtrust.KnownHostsLine(address, "ssh-ed25519", publicKey),
			}}, nil
		},
	}
}

func TestOfferTrustOnFirstUseRecordsAcceptedKey(t *testing.T) {
	contextDir := t.TempDir()
	fingerprint, err := sshtrust.FingerprintSHA256(hostTrustKeyA)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	var stdout strings.Builder
	if err := offerTrustOnFirstUse(context.Background(), strings.NewReader("y\n"), &stdout, contextDir, hostTrustTestState(), hostTrustScanDeps(map[string]string{"provider-01.example.test": hostTrustKeyA})); err != nil {
		t.Fatalf("offerTrustOnFirstUse: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "SSH host trust (first use)") {
		t.Fatalf("output missing first-use section:\n%s", out)
	}
	prompt := "Trust ssh-ed25519 " + fingerprint + " for Machine/provider-01 at provider-01.example.test? [y/N]"
	if !strings.Contains(out, prompt) {
		t.Fatalf("output missing prompt %q:\n%s", prompt, out)
	}
	if !strings.Contains(out, "recorded ssh-ed25519 "+fingerprint) {
		t.Fatalf("output missing recorded status:\n%s", out)
	}
	storeFile := readTestFile(t, filepath.Join(contextDir, "trust", "ssh", "hosts.json"))
	if !strings.Contains(storeFile, `"provider-01"`) || !strings.Contains(storeFile, hostTrustKeyA) {
		t.Fatalf("trust store file missing accepted record:\n%s", storeFile)
	}
	store, err := sshtrust.Load(sshtrust.StorePathForContext(contextDir))
	if err != nil {
		t.Fatalf("load trust store: %v", err)
	}
	record, ok := store.Find("provider-01")
	if !ok {
		t.Fatal("trust store missing provider-01")
	}
	if record.PublicKey != hostTrustKeyA || record.FingerprintSHA256 != fingerprint {
		t.Fatalf("record = %+v", record)
	}
	if got := readTestFile(t, sshtrust.KnownHostsPathForContext(contextDir)); !strings.Contains(got, "provider-01.example.test ssh-ed25519 "+hostTrustKeyA) {
		t.Fatalf("known_hosts = %q", got)
	}
}

func TestOfferTrustOnFirstUseDeclineWritesNothing(t *testing.T) {
	for name, input := range map[string]string{"declined": "n\n", "eof": ""} {
		t.Run(name, func(t *testing.T) {
			contextDir := t.TempDir()
			var stdout strings.Builder
			if err := offerTrustOnFirstUse(context.Background(), strings.NewReader(input), &stdout, contextDir, hostTrustTestState(), hostTrustScanDeps(map[string]string{"provider-01.example.test": hostTrustKeyA})); err != nil {
				t.Fatalf("offerTrustOnFirstUse: %v", err)
			}
			if !strings.Contains(stdout.String(), "not trusted; run bootwright host trust to record it later") {
				t.Fatalf("output missing decline status:\n%s", stdout.String())
			}
			if _, err := os.Stat(sshtrust.StorePathForContext(contextDir)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("trust store written after decline: stat err = %v", err)
			}
			if _, err := os.Stat(sshtrust.KnownHostsPathForContext(contextDir)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("known_hosts written after decline: stat err = %v", err)
			}
		})
	}
}

func TestOfferTrustOnFirstUseSkipsRecordedHostWithoutScan(t *testing.T) {
	contextDir := t.TempDir()
	fingerprint, err := sshtrust.FingerprintSHA256(hostTrustKeyA)
	if err != nil {
		t.Fatalf("fingerprint: %v", err)
	}
	store := sshtrust.Store{}
	store.Upsert(sshtrust.HostRecord{
		Name:              "provider-01",
		Address:           "provider-01.example.test",
		KeyType:           "ssh-ed25519",
		PublicKey:         hostTrustKeyA,
		FingerprintSHA256: fingerprint,
		KnownHostsLine:    sshtrust.KnownHostsLine("provider-01.example.test", "ssh-ed25519", hostTrustKeyA),
	})
	if err := sshtrust.Save(sshtrust.DirForContext(contextDir), store); err != nil {
		t.Fatalf("seed trust store: %v", err)
	}
	deps := hostTrustDeps{
		lookPath: func(name string, _ []string) (string, error) {
			t.Fatalf("unexpected lookup %s for already-recorded host", name)
			return "", errors.New("unexpected lookup")
		},
		scan: func(_ context.Context, address string, _ time.Duration) ([]sshtrust.ScannedKey, error) {
			t.Fatalf("unexpected scan %s for already-recorded host", address)
			return nil, errors.New("unexpected scan")
		},
	}
	var stdout strings.Builder
	if err := offerTrustOnFirstUse(context.Background(), strings.NewReader("y\n"), &stdout, contextDir, hostTrustTestState(), deps); err != nil {
		t.Fatalf("offerTrustOnFirstUse: %v", err)
	}
	if stdout.String() != "" {
		t.Fatalf("already-recorded host produced output:\n%s", stdout.String())
	}
}

func TestOfferTrustOnFirstUseScanErrorWarnsAndContinues(t *testing.T) {
	contextDir := t.TempDir()
	deps := hostTrustDeps{
		lookPath: func(name string, _ []string) (string, error) {
			if name != "ssh-keyscan" {
				return "", errors.New("unexpected lookup " + name)
			}
			return "/usr/bin/ssh-keyscan", nil
		},
		scan: func(_ context.Context, _ string, _ time.Duration) ([]sshtrust.ScannedKey, error) {
			return nil, errors.New("connection refused")
		},
	}
	var stdout strings.Builder
	if err := offerTrustOnFirstUse(context.Background(), strings.NewReader("y\n"), &stdout, contextDir, hostTrustTestState(), deps); err != nil {
		t.Fatalf("offerTrustOnFirstUse: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "[WARN]") || !strings.Contains(out, "Machine/provider-01") || !strings.Contains(out, "connection refused") {
		t.Fatalf("output missing scan warning:\n%s", out)
	}
	if strings.Contains(out, "[y/N]") {
		t.Fatalf("unreachable host was still prompted:\n%s", out)
	}
	if _, err := os.Stat(sshtrust.StorePathForContext(contextDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("trust store written after scan error: stat err = %v", err)
	}
}

func TestOfferTrustOnFirstUseMissingKeyscanIsSilent(t *testing.T) {
	contextDir := t.TempDir()
	deps := hostTrustDeps{
		lookPath: func(string, []string) (string, error) {
			return "", errors.New("ssh-keyscan not found")
		},
		scan: func(_ context.Context, address string, _ time.Duration) ([]sshtrust.ScannedKey, error) {
			t.Fatalf("unexpected scan %s without ssh-keyscan", address)
			return nil, errors.New("unexpected scan")
		},
	}
	var stdout strings.Builder
	if err := offerTrustOnFirstUse(context.Background(), strings.NewReader("y\n"), &stdout, contextDir, hostTrustTestState(), deps); err != nil {
		t.Fatalf("offerTrustOnFirstUse: %v", err)
	}
	if stdout.String() != "" {
		t.Fatalf("missing ssh-keyscan produced output:\n%s", stdout.String())
	}
	if _, err := os.Stat(sshtrust.StorePathForContext(contextDir)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("trust store written without ssh-keyscan: stat err = %v", err)
	}
}

func TestTrustOnFirstUseFlagSurface(t *testing.T) {
	for _, args := range [][]string{
		{"preflight", "infra", "--help"},
		{"preflight", "all", "--help"},
		{"apply", "--help"},
	} {
		stdout, stderr, code := runCLI(t, args...)
		if code != 0 {
			t.Fatalf("bootwright %s exited %d, stderr=%q", strings.Join(args, " "), code, stderr)
		}
		if !strings.Contains(stdout, "--trust-on-first-use") {
			t.Fatalf("help for %s missing --trust-on-first-use:\n%s", strings.Join(args, " "), stdout)
		}
	}
	stdout, stderr, code := runCLI(t, "plan", "--help")
	if code != 0 {
		t.Fatalf("plan --help exited %d, stderr=%q", code, stderr)
	}
	if strings.Contains(stdout, "--trust-on-first-use") {
		t.Fatalf("plan is read-only and must not offer --trust-on-first-use:\n%s", stdout)
	}
}

func TestPreflightJSONAndDryRunNeverPrompt(t *testing.T) {
	initHostTrustTestContext(t)
	// Any unexpected first-use scan errors instead of reaching the network.
	setHostTrustTestDeps(t, nil)

	stdout, stderr, code := runCLI(t, "preflight", "infra", "--output", "json")
	if code == 0 {
		t.Fatal("preflight --output json without --dry-run unexpectedly succeeded")
	}
	if strings.Contains(stdout, "[y/N]") || strings.Contains(stdout, "SSH host trust (first use)") {
		t.Fatalf("json preflight reached the trust prompt:\n%s", stdout)
	}
	if !strings.Contains(stdout, "--dry-run") {
		t.Fatalf("json preflight error report missing --dry-run requirement:\nstdout=%s\nstderr=%s", stdout, stderr)
	}

	stdout, stderr, code = runCLI(t, "preflight", "infra", "--dry-run")
	if code == 0 {
		t.Fatal("preflight --dry-run with missing trust unexpectedly passed host checks")
	}
	if strings.Contains(stdout, "[y/N]") || strings.Contains(stdout, "SSH host trust (first use)") {
		t.Fatalf("dry-run preflight reached the trust prompt:\n%s", stdout)
	}
	if !strings.Contains(stdout+stderr, "bootwright host trust") {
		t.Fatalf("dry-run preflight did not fail closed with the host trust remediation:\nstdout=%s\nstderr=%s", stdout, stderr)
	}
}
