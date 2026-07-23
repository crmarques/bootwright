package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/host/execution"
	secret "github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/sshtrust"
)

func machineSSHTestState() v1alpha1.State {
	return v1alpha1.State{Machines: []v1alpha1.Machine{{
		Metadata: v1alpha1.Metadata{Name: "ceph-0"},
		Spec: v1alpha1.MachineSpec{
			OS:        v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(false)},
			Addresses: []v1alpha1.MachineAddress{{Name: "ssh", Address: "10.0.0.10"}},
			Access: v1alpha1.MachineAccess{SSH: &v1alpha1.MachineSSHSpec{
				AddressRef: v1alpha1.LocalObjectReference{Name: "ssh"},
				KeyRef:     v1alpha1.SecretRef{Name: "ceph-key"},
			}},
		},
	}}}
}

func indexOf(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}

func TestBuildMachineSSHInvocation(t *testing.T) {
	state := machineSSHTestState()
	target, err := machineSSHTarget(state, "ceph-0")
	if err != nil {
		t.Fatalf("machineSSHTarget: %v", err)
	}
	inv := buildSSHInvocation(target, "/base/secrets/ceph-key", "/base/ssh-policy", sshtrust.KnownHostsPathForContext("/base"), "ask", []string{"uptime"}, "/usr/bin/ssh", nil)
	if inv.Path != "/usr/bin/ssh" {
		t.Fatalf("path = %q", inv.Path)
	}
	if inv.Args[0] != "ssh" {
		t.Fatalf("argv[0] = %q, want ssh", inv.Args[0])
	}
	if i := indexOf(inv.Args, "-F"); i < 0 || inv.Args[i+1] != "/base/ssh-policy" {
		t.Fatalf("isolated SSH config missing in %v", inv.Args)
	}
	if i := indexOf(inv.Args, "-i"); i < 0 || inv.Args[i+1] != "/base/secrets/ceph-key" {
		t.Fatalf("key arg missing/wrong in %v", inv.Args)
	}
	if indexOf(inv.Args, "IdentityFile=none") < 0 {
		t.Fatalf("default identity reset missing in %v", inv.Args)
	}
	wantKH := "UserKnownHostsFile=" + sshtrust.KnownHostsPathForContext("/base")
	if indexOf(inv.Args, wantKH) < 0 {
		t.Fatalf("known-hosts arg %q missing in %v", wantKH, inv.Args)
	}
	if indexOf(inv.Args, "StrictHostKeyChecking=ask") < 0 {
		t.Fatalf("ask arg missing in %v", inv.Args)
	}
	if indexOf(inv.Args, "HashKnownHosts=no") < 0 {
		t.Fatalf("plain known-hosts arg missing in %v", inv.Args)
	}
	for _, want := range []string{
		"IdentityAgent=none",
		"PreferredAuthentications=publickey",
		"PubkeyAuthentication=yes",
		"PasswordAuthentication=no",
		"KbdInteractiveAuthentication=no",
		"GSSAPIAuthentication=no",
		"HostbasedAuthentication=no",
		"HostKeyAlias=10.0.0.10",
		"CheckHostIP=no",
		"GlobalKnownHostsFile=none",
		"KnownHostsCommand=none",
		"VerifyHostKeyDNS=no",
		"UpdateHostKeys=no",
		"ForwardAgent=no",
	} {
		if indexOf(inv.Args, want) < 0 {
			t.Fatalf("SSH isolation arg %q missing in %v", want, inv.Args)
		}
	}
	if indexOf(inv.Args, "BatchMode=yes") >= 0 {
		t.Fatalf("BatchMode must not be set for an interactive session: %v", inv.Args)
	}
	last := inv.Args[len(inv.Args)-2:]
	if last[0] != "root@10.0.0.10" || last[1] != "uptime" {
		t.Fatalf("tail = %v, want [root@10.0.0.10 uptime]", last)
	}
}

func TestBuildMachineSSHInvocationUserOverride(t *testing.T) {
	state := machineSSHTestState()
	state.Machines[0].Spec.Access.SSH.User = "core"
	target, err := machineSSHTarget(state, "ceph-0")
	if err != nil {
		t.Fatalf("machineSSHTarget: %v", err)
	}
	inv := buildSSHInvocation(target, "/base/secrets/ceph-key", "/base/ssh-policy", sshtrust.KnownHostsPathForContext("/base"), "ask", nil, "/usr/bin/ssh", nil)
	if inv.Args[len(inv.Args)-1] != "core@10.0.0.10" {
		t.Fatalf("target = %q, want core@10.0.0.10", inv.Args[len(inv.Args)-1])
	}
}

func TestBuildSSHInvocationQuotesExtraArgsForRemoteReassembly(t *testing.T) {
	state := machineSSHTestState()
	target, err := machineSSHTarget(state, "ceph-0")
	if err != nil {
		t.Fatalf("machineSSHTarget: %v", err)
	}
	extraArgs := []string{"sudo", "podman", "ps", "--format", "{{.Names}} {{.Ports}}"}
	inv := buildSSHInvocation(target, "/base/secrets/ceph-key", "/base/ssh-policy", sshtrust.KnownHostsPathForContext("/base"), "ask", extraArgs, "/usr/bin/ssh", nil)

	tail := inv.Args[len(inv.Args)-len(extraArgs):]
	remoteCommand := strings.Join(tail, " ")
	script := `sudo() { podman "$@"; }; podman() { printf '%s\n' "$@"; }; ` + remoteCommand
	out, err := exec.Command("/bin/sh", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("sh -c %q: %v\n%s", script, err, out)
	}
	got := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	want := extraArgs[1:]
	if len(got) != len(want) {
		t.Fatalf("remote argv = %q, want %q (remote command was %q)", got, want, remoteCommand)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("remote argv[%d] = %q, want %q (remote command was %q)", i, got[i], want[i], remoteCommand)
		}
	}
}

func TestBuildMachineSSHInvocationErrors(t *testing.T) {
	if _, err := machineSSHTarget(machineSSHTestState(), "ghost"); err == nil || !strings.Contains(err.Error(), "unknown Machine") {
		t.Fatalf("unknown machine err = %v", err)
	}

	noSSH := v1alpha1.State{Machines: []v1alpha1.Machine{{
		Metadata: v1alpha1.Metadata{Name: "m"},
		Spec:     v1alpha1.MachineSpec{OS: v1alpha1.MachineOSSpec{Provided: v1alpha1.BoolPtr(true)}},
	}}}
	if _, err := machineSSHTarget(noSSH, "m"); err == nil || !strings.Contains(err.Error(), "no SSH access") {
		t.Fatalf("no-ssh err = %v", err)
	}

	noAddr := machineSSHTestState()
	noAddr.Machines[0].Spec.Access.SSH.AddressRef = v1alpha1.LocalObjectReference{Name: "missing"}
	if _, err := machineSSHTarget(noAddr, "ceph-0"); err == nil || !strings.Contains(err.Error(), "no resolvable SSH address") {
		t.Fatalf("no-address err = %v", err)
	}
}

func TestMachineSSHCLIExecsClient(t *testing.T) {
	initHostTrustTestContext(t)

	var gotPath string
	var gotArgs []string
	previous := defaultMachineSSHDeps
	defaultMachineSSHDeps = machineSSHDeps{
		lookPath: func(string) (string, error) { return "/usr/bin/ssh", nil },
		run: func(_ context.Context, invocation sshInvocation) (int, error) {
			gotPath = invocation.Path
			gotArgs = invocation.Args
			if len(invocation.MaterialFiles) != 2 {
				t.Fatalf("material files = %d, want 2", len(invocation.MaterialFiles))
			}
			if _, err := invocation.MaterialFiles[0].Stat(); err != nil {
				t.Fatalf("private key file is unavailable during SSH run: %v", err)
			}
			return 0, nil
		},
	}
	t.Cleanup(func() { defaultMachineSSHDeps = previous })

	_, stderr, code := runCLI(t, "machine", "exec", "--name", "provider-01", "--", "uptime")
	if code != 0 {
		t.Fatalf("machine exec exited %d, stderr=%q", code, stderr)
	}
	if gotPath != "/usr/bin/ssh" {
		t.Fatalf("exec path = %q", gotPath)
	}
	if indexOf(gotArgs, "core@provider-01.example.test") < 0 {
		t.Fatalf("target missing in argv %v", gotArgs)
	}
	if indexOf(gotArgs, "StrictHostKeyChecking=ask") < 0 {
		t.Fatalf("context-managed trust did not require explicit first-use confirmation: %v", gotArgs)
	}
	if gotArgs[len(gotArgs)-1] != "uptime" {
		t.Fatalf("trailing command = %q, want uptime", gotArgs[len(gotArgs)-1])
	}
}

func TestMachineRshExecsInteractiveShell(t *testing.T) {
	initHostTrustTestContext(t)

	var gotArgs []string
	previous := defaultMachineSSHDeps
	defaultMachineSSHDeps = machineSSHDeps{
		lookPath: func(string) (string, error) { return "/usr/bin/ssh", nil },
		run: func(_ context.Context, invocation sshInvocation) (int, error) {
			gotArgs = invocation.Args
			return 0, nil
		},
	}
	t.Cleanup(func() { defaultMachineSSHDeps = previous })

	_, stderr, code := runCLI(t, "machine", "rsh", "--name", "provider-01")
	if code != 0 {
		t.Fatalf("machine rsh exited %d, stderr=%q", code, stderr)
	}
	if gotArgs[len(gotArgs)-1] != "core@provider-01.example.test" {
		t.Fatalf("rsh argv should end at the target, got %v", gotArgs)
	}
}

func TestMachineRshRejectsTrailingCommand(t *testing.T) {
	initHostTrustTestContext(t)
	_, stderr, code := runCLI(t, "machine", "rsh", "--name", "provider-01", "--", "uptime")
	if code != 2 {
		t.Fatalf("machine rsh with a command should exit 2, got %d", code)
	}
	if !strings.Contains(stderr, "machine exec") {
		t.Fatalf("rsh error should point at machine exec: %q", stderr)
	}
}

func TestMachineExecRequiresCommand(t *testing.T) {
	initHostTrustTestContext(t)
	_, stderr, code := runCLI(t, "machine", "exec", "--name", "provider-01")
	if code != 2 {
		t.Fatalf("machine exec without a command should exit 2, got %d", code)
	}
	if !strings.Contains(stderr, "requires a command after --") {
		t.Fatalf("exec error should explain the missing command: %q", stderr)
	}
}

func TestMachineRshRequiresName(t *testing.T) {
	initHostTrustTestContext(t)
	if _, _, code := runCLI(t, "machine", "rsh"); code == 0 {
		t.Fatal("machine rsh without --name should fail")
	}
}

func TestMachineSSHRejectsBroadPrivateKeyMode(t *testing.T) {
	initHostTrustTestContext(t)
	keyPath := filepath.Join(os.Getenv("HOME"), ".ssh", "bootwright-ssh-key")
	if err := os.Chmod(keyPath, 0o644); err != nil {
		t.Fatalf("chmod test key: %v", err)
	}
	previous := defaultMachineSSHDeps
	defaultMachineSSHDeps = machineSSHDeps{
		lookPath: func(string) (string, error) { return "/usr/bin/ssh", nil },
		run: func(context.Context, sshInvocation) (int, error) {
			t.Fatal("SSH ran with a broadly-readable private key")
			return 0, nil
		},
	}
	t.Cleanup(func() { defaultMachineSSHDeps = previous })
	_, stderr, code := runCLI(t, "machine", "rsh", "--name", "provider-01")
	if code != 1 {
		t.Fatalf("machine rsh exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "remove all group and other permissions") {
		t.Fatalf("private key mode error = %q", stderr)
	}
}

func TestSSHInvocationClosesPrivateMaterial(t *testing.T) {
	ctx := initHostTrustTestContext(t)
	state, err := loadDesiredState(&commonFlags{ctx: ctx})
	if err != nil {
		t.Fatalf("loadDesiredState: %v", err)
	}
	target, err := machineSSHTarget(state, "provider-01")
	if err != nil {
		t.Fatalf("machineSSHTarget: %v", err)
	}
	invocation, err := prepareSSHInvocation(ctx, state, target, nil, "/usr/bin/ssh")
	if err != nil {
		t.Fatalf("prepareSSHInvocation: %v", err)
	}
	file := invocation.MaterialFiles[0]
	if _, err := file.Stat(); err != nil {
		t.Fatalf("private material is unavailable before cleanup: %v", err)
	}
	closeSSHInvocationFiles(invocation)
	if _, err := file.Stat(); err == nil {
		t.Fatal("private material file remained open after cleanup")
	}
}

func TestPrepareSSHInvocationDecryptsExplicitKnownHosts(t *testing.T) {
	ctx := initHostTrustTestContext(t)
	state, err := loadDesiredState(&commonFlags{ctx: ctx})
	if err != nil {
		t.Fatalf("loadDesiredState: %v", err)
	}
	const knownHostsName = "provider-known-hosts"
	knownHosts := []byte("provider-01.example.test ssh-ed25519 AAAATEST\n")
	state.Secrets = append(state.Secrets, v1alpha1.Secret{
		Metadata: v1alpha1.Metadata{Name: knownHostsName},
		Spec: v1alpha1.SecretSpec{
			Type:   v1alpha1.SecretTypeOpaque,
			Source: v1alpha1.SecretSource{ContextStore: &v1alpha1.SecretContextStoreSource{}},
		},
	})
	writeTestContextSecret(t, ctx.Name, ctx.SecretsDir, knownHostsName, secret.MaterialPrimary, knownHosts)
	envelope, err := os.ReadFile(filepath.Join(ctx.SecretsDir, knownHostsName))
	if err != nil {
		t.Fatalf("read encrypted known-hosts envelope: %v", err)
	}
	if bytes.Contains(envelope, knownHosts) {
		t.Fatal("known-hosts context material was stored as plaintext")
	}
	target, err := machineSSHTarget(state, "provider-01")
	if err != nil {
		t.Fatalf("machineSSHTarget: %v", err)
	}
	target.KnownHostsRef = v1alpha1.SecretRef{Name: knownHostsName}
	invocation, err := prepareSSHInvocation(ctx, state, target, nil, "/usr/bin/ssh")
	if err != nil {
		t.Fatalf("prepareSSHInvocation: %v", err)
	}
	if len(invocation.MaterialFiles) != 3 {
		closeSSHInvocationFiles(invocation)
		t.Fatalf("material files = %d, want 3", len(invocation.MaterialFiles))
	}
	knownHostsFile := invocation.MaterialFiles[2]
	got := make([]byte, len(knownHosts))
	if _, err := knownHostsFile.ReadAt(got, 0); err != nil {
		closeSSHInvocationFiles(invocation)
		t.Fatalf("read decrypted known-hosts material: %v", err)
	}
	if !bytes.Equal(got, knownHosts) {
		closeSSHInvocationFiles(invocation)
		t.Fatalf("known-hosts material = %q, want %q", got, knownHosts)
	}
	wantPath := "UserKnownHostsFile=" + sshParentFDPath(knownHostsFile)
	if indexOf(invocation.Args, wantPath) < 0 || indexOf(invocation.Args, "StrictHostKeyChecking=yes") < 0 {
		closeSSHInvocationFiles(invocation)
		t.Fatalf("explicit known-hosts options missing in %v", invocation.Args)
	}
	closeSSHInvocationFiles(invocation)
	for i, file := range invocation.MaterialFiles {
		if _, err := file.Stat(); err == nil {
			t.Fatalf("material file %d remained open after cleanup", i)
		}
	}
}

func TestEnsureSSHKnownHostsFileConcurrent(t *testing.T) {
	contextDir := t.TempDir()
	start := make(chan struct{})
	errs := make(chan error, 32)
	var group sync.WaitGroup
	for range 32 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			_, err := ensureSSHKnownHostsFile(contextDir)
			errs <- err
		}()
	}
	close(start)
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("ensureSSHKnownHostsFile: %v", err)
		}
	}
	path := sshtrust.KnownHostsPathForContext(contextDir)
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatalf("stat known-hosts file: %v", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("known-hosts mode = %v, want regular 0600", info.Mode())
	}
}

func TestEnsureSSHKnownHostsFileRejectsSymlink(t *testing.T) {
	contextDir := t.TempDir()
	path := sshtrust.KnownHostsPathForContext(contextDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create trust directory: %v", err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, nil, 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := os.Symlink(target, path); err != nil {
		t.Fatalf("create known-hosts symlink: %v", err)
	}
	if _, err := ensureSSHKnownHostsFile(contextDir); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("symlink error = %v", err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("symlink target mode = %o, want unchanged 0644", info.Mode().Perm())
	}
}

func TestRunSSHInvocationMapsAnonymousMaterialFD(t *testing.T) {
	file, err := anonymousSSHMaterial(t.TempDir(), []byte("secret\n"))
	if err != nil {
		t.Fatalf("anonymousSSHMaterial: %v", err)
	}
	defer file.Close()
	exitCode, err := runSSHInvocation(context.Background(), sshInvocation{
		Path:          "/bin/sh",
		Args:          []string{"sh", "-c", `IFS= read -r value < ` + sshParentFDPath(file) + `; test "$value" = secret`},
		Env:           os.Environ(),
		MaterialFiles: []*os.File{file},
	})
	if err != nil {
		t.Fatalf("runSSHInvocation: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
}

func TestOpenSSHReadsParentAnonymousMaterial(t *testing.T) {
	sshKeygen, err := execution.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen is unavailable")
	}
	privateKey, _, err := secret.SSHKeyPairPEM(v1alpha1.GeneratedSSHKeyPairSpec{
		Type: v1alpha1.SSHKeyPairTypeEd25519,
	})
	if err != nil {
		t.Fatalf("SSHKeyPairPEM: %v", err)
	}
	file, err := anonymousSSHMaterial(t.TempDir(), privateKey)
	if err != nil {
		t.Fatalf("anonymousSSHMaterial: %v", err)
	}
	defer file.Close()
	err = execution.OSRunner{}.Run(context.Background(), execution.Command{
		Name:   sshKeygen,
		Args:   []string{"-y", "-f", sshParentFDPath(file)},
		Env:    os.Environ(),
		Stdout: io.Discard,
	})
	if err != nil {
		t.Fatalf("ssh-keygen: %v", err)
	}
}

func TestOpenSSHConfigUsesOnlyParentAnonymousMaterial(t *testing.T) {
	sshPath, err := execution.LookPath("ssh")
	if err != nil {
		t.Skip("ssh is unavailable")
	}
	file, err := anonymousSSHMaterial(t.TempDir(), []byte("private\n"))
	if err != nil {
		t.Fatalf("anonymousSSHMaterial: %v", err)
	}
	defer file.Close()
	keyPath := sshParentFDPath(file)
	policy, err := loadSanitizedSSHPolicy(sshCryptoPolicyPath)
	if err != nil {
		t.Fatalf("loadSanitizedSSHPolicy: %v", err)
	}
	configFile, err := anonymousSSHMaterial(t.TempDir(), policy)
	if err != nil {
		t.Fatalf("anonymousSSHMaterial config: %v", err)
	}
	defer configFile.Close()
	invocation := buildSSHInvocation(sshTarget{
		Address: "example.test",
		User:    "core",
	}, keyPath, sshParentFDPath(configFile), filepath.Join(t.TempDir(), "known_hosts"), "ask", nil, sshPath, []*os.File{file, configFile})
	args := append([]string{"-G"}, invocation.Args[1:]...)
	output, err := execution.OSRunner{}.Output(context.Background(), execution.Command{
		Name: sshPath,
		Args: args,
		Env:  invocation.Env,
	})
	if err != nil {
		t.Fatalf("ssh -G: %v", err)
	}
	config := string(output)
	if !strings.Contains(config, "identityfile none") || !strings.Contains(config, "identityfile "+keyPath) {
		t.Fatalf("ssh identity config missing selected parent fd:\n%s", config)
	}
	if strings.Contains(config, "/.ssh/id_") {
		t.Fatalf("ssh config retained default identities:\n%s", config)
	}
	for _, want := range []string{
		"identityagent none",
		"preferredauthentications publickey",
		"pubkeyauthentication true",
		"passwordauthentication no",
		"kbdinteractiveauthentication no",
		"gssapiauthentication no",
		"hostbasedauthentication no",
		"hostkeyalias example.test",
		"checkhostip no",
		"globalknownhostsfile none",
		"verifyhostkeydns false",
		"updatehostkeys false",
		"hashknownhosts no",
		"forwardagent no",
	} {
		if !strings.Contains(config, want) {
			t.Fatalf("ssh config missing %q:\n%s", want, config)
		}
	}
}

func TestLoadSanitizedSSHPolicyRejectsIdentityAndTransportDirectives(t *testing.T) {
	for _, directive := range []string{
		"IdentityFile /tmp/system-key",
		"CertificateFile /tmp/system-cert",
		"ProxyCommand false",
		"LocalForward 8080 example.test:80",
	} {
		t.Run(strings.Fields(directive)[0], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "policy")
			if err := os.WriteFile(path, []byte("Ciphers aes256-gcm@openssh.com\n"+directive+"\n"), 0o600); err != nil {
				t.Fatalf("write policy: %v", err)
			}
			if _, err := loadSanitizedSSHPolicy(path); err == nil || !strings.Contains(err.Error(), "unsupported directive") {
				t.Fatalf("unsafe policy directive error = %v", err)
			}
		})
	}
}

func TestLoadSanitizedSSHPolicyKeepsCryptoDirectives(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy")
	want := "Ciphers aes256-gcm@openssh.com\nRequiredRSASize 2048\n"
	if err := os.WriteFile(path, []byte("# policy\n"+want), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	got, err := loadSanitizedSSHPolicy(path)
	if err != nil {
		t.Fatalf("loadSanitizedSSHPolicy: %v", err)
	}
	if string(got) != want {
		t.Fatalf("sanitized policy = %q, want %q", got, want)
	}
}

func TestLoadSanitizedSSHPolicyDropsBenignDirectives(t *testing.T) {
	path := filepath.Join(t.TempDir(), "policy")
	want := "Ciphers aes256-gcm@openssh.com\n"
	if err := os.WriteFile(path, []byte(want+"GSSAPIKeyExchange yes\n"), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	got, err := loadSanitizedSSHPolicy(path)
	if err != nil {
		t.Fatalf("loadSanitizedSSHPolicy: %v", err)
	}
	if string(got) != want {
		t.Fatalf("sanitized policy = %q, want %q", got, want)
	}
}

func TestFilteredSSHEnvironmentDropsHelperAndLoaderOverrides(t *testing.T) {
	got := filteredSSHEnvironment([]string{
		"TERM=xterm-256color",
		"COLORTERM=truecolor",
		"LANG=en_US.UTF-8",
		"LC_ALL=C",
		"LC_TIME=pt_BR.UTF-8",
		"TZ=America/Sao_Paulo",
		"SSH_ASKPASS=/tmp/askpass",
		"SSH_ASKPASS_REQUIRE=force",
		"DISPLAY=:0",
		"SSH_AUTH_SOCK=/tmp/agent",
		"OPENSSL_CONF=/tmp/openssl.conf",
		"OPENSSL_MODULES=/tmp/modules",
		"LD_PRELOAD=/tmp/inject.so",
		"PATH=/tmp/bin",
	})
	if strings.Join(got, "\n") != "TERM=xterm-256color\nCOLORTERM=truecolor" {
		t.Fatalf("filtered SSH environment = %q", got)
	}
}
