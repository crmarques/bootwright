package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/host/execution"
	"github.com/crmarques/bootwright/internal/host/safefs"
	"github.com/crmarques/bootwright/internal/host/shellquote"
	secret "github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/sshtrust"
	"github.com/crmarques/bootwright/internal/workspace"
)

type machineSSHDeps struct {
	lookPath func(string) (string, error)
	run      func(context.Context, sshInvocation) (int, error)
}

var defaultMachineSSHDeps = machineSSHDeps{
	lookPath: execution.LookPath,
	run:      runSSHInvocation,
}

type sshTarget struct {
	Address       string
	User          string
	Port          int
	KeyRef        v1alpha1.SecretRef
	KnownHostsRef v1alpha1.SecretRef
}

type sshInvocation struct {
	Path          string
	Args          []string
	Env           []string
	MaterialFiles []*os.File
}

const sshCryptoPolicyPath = "/etc/crypto-policies/back-ends/openssh.config"

func execSSHTarget(commandCtx context.Context, ctx workspace.Context, state v1alpha1.State, target sshTarget, extraArgs []string) error {
	sshPath, err := defaultMachineSSHDeps.lookPath("ssh")
	if err != nil {
		return failErr(1, fmt.Errorf("an ssh client is required for SSH access: %w", err))
	}
	invocation, err := prepareSSHInvocation(ctx, state, target, extraArgs, sshPath)
	if err != nil {
		return failErr(1, err)
	}
	defer closeSSHInvocationFiles(invocation)
	exitCode, err := defaultMachineSSHDeps.run(commandCtx, invocation)
	if err != nil {
		return failErr(1, fmt.Errorf("run ssh: %w", err))
	}
	if exitCode != 0 {
		return silentExit(exitCode)
	}
	return nil
}

func prepareSSHInvocation(ctx workspace.Context, state v1alpha1.State, target sshTarget, extraArgs []string, sshPath string) (sshInvocation, error) {
	preferredKeyPath, err := resolveSSHIDFile()
	if err != nil {
		return sshInvocation{}, err
	}
	resolver := secret.NewResolver(ctx.Name, ctx.SecretsDir, secret.NewIndex(state))
	var files []*os.File
	keyPath := ""
	if target.KeyRef.Name != "" {
		privateKeyInfo, err := resolver.StatMaterial(target.KeyRef.Name, secret.MaterialSSHPrivate)
		if err != nil {
			return sshInvocation{}, fmt.Errorf("stat SSH private key Secret %q: %w", target.KeyRef.Name, err)
		}
		if !privateKeyInfo.Mode().IsRegular() {
			return sshInvocation{}, fmt.Errorf("SSH private key Secret %q is not a regular file", target.KeyRef.Name)
		}
		if privateKeyInfo.Mode().Perm()&0o077 != 0 {
			return sshInvocation{}, fmt.Errorf("SSH private key Secret %q has mode %#o; remove all group and other permissions", target.KeyRef.Name, privateKeyInfo.Mode().Perm())
		}
		privateKey, err := resolver.ReadMaterial(target.KeyRef.Name, secret.MaterialSSHPrivate)
		if err != nil {
			return sshInvocation{}, fmt.Errorf("read SSH private key Secret %q: %w", target.KeyRef.Name, err)
		}
		defer clear(privateKey)
		keyFile, err := anonymousSSHMaterial(ctx.RunsDir, privateKey)
		if err != nil {
			return sshInvocation{}, err
		}
		files = append(files, keyFile)
		keyPath = sshParentFDPath(keyFile)
	}
	cryptoPolicy, err := loadSanitizedSSHPolicy(sshCryptoPolicyPath)
	if err != nil {
		closeSSHFiles(files)
		return sshInvocation{}, err
	}
	configFile, err := anonymousSSHMaterial(ctx.RunsDir, cryptoPolicy)
	if err != nil {
		closeSSHFiles(files)
		return sshInvocation{}, err
	}
	files = append(files, configFile)
	configPath := sshParentFDPath(configFile)
	knownHostsPath := ""
	strictHostKeyChecking := "ask"
	if target.KnownHostsRef.Name != "" {
		knownHosts, readErr := resolver.ReadMaterial(target.KnownHostsRef.Name, secret.MaterialPrimary)
		if readErr != nil {
			closeSSHFiles(files)
			return sshInvocation{}, fmt.Errorf("read SSH known-hosts Secret %q: %w", target.KnownHostsRef.Name, readErr)
		}
		knownHostsFile, materialErr := anonymousSSHMaterial(ctx.RunsDir, knownHosts)
		if materialErr != nil {
			closeSSHFiles(files)
			return sshInvocation{}, materialErr
		}
		files = append(files, knownHostsFile)
		knownHostsPath = sshParentFDPath(knownHostsFile)
		strictHostKeyChecking = "yes"
	} else {
		knownHostsPath, err = ensureSSHKnownHostsFile(ctx.BaseDir)
		if err != nil {
			closeSSHFiles(files)
			return sshInvocation{}, err
		}
	}
	return buildSSHInvocation(target, keyPath, preferredKeyPath, configPath, knownHostsPath, strictHostKeyChecking, extraArgs, sshPath, files), nil
}

func ensureSSHKnownHostsFile(contextDir string) (string, error) {
	path := sshtrust.KnownHostsPathForContext(contextDir)
	if path == "" {
		return "", errors.New("SSH known-hosts path is required")
	}
	if err := safefs.EnsureDir(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	for {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			if err := safefs.WriteNewFile(path, nil, 0o600); err != nil {
				if errors.Is(err, os.ErrExist) {
					continue
				}
				return "", err
			}
			return path, nil
		}
		if err != nil {
			return "", fmt.Errorf("stat SSH known-hosts file %s: %w", path, err)
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("SSH known-hosts path %s is not a regular file", path)
		}
		if err := os.Chmod(path, 0o600); err != nil {
			return "", fmt.Errorf("chmod SSH known-hosts file %s: %w", path, err)
		}
		return path, nil
	}
}

func buildSSHInvocation(target sshTarget, keyPath, preferredKeyPath, configPath, knownHostsPath, strictHostKeyChecking string, extraArgs []string, sshPath string, files []*os.File) sshInvocation {
	args := []string{
		"ssh",
		"-F", configPath,
	}
	if preferredKeyPath != "" {
		args = append(args, "-i", preferredKeyPath)
	}
	if keyPath != "" {
		args = append(args,
			"-o", "IdentityFile=none",
			"-i", keyPath,
			"-o", "IdentitiesOnly=yes",
			"-o", "IdentityAgent=none",
			"-o", "PreferredAuthentications=publickey",
			"-o", "PubkeyAuthentication=yes",
			"-o", "PasswordAuthentication=no",
			"-o", "KbdInteractiveAuthentication=no",
		)
	}
	if target.Port != 0 {
		args = append(args, "-p", strconv.Itoa(target.Port))
	}
	args = append(args,
		"-o", "GSSAPIAuthentication=no",
		"-o", "HostbasedAuthentication=no",
		"-o", "HostKeyAlias="+target.Address,
		"-o", "CheckHostIP=no",
		"-o", "GlobalKnownHostsFile=none",
		"-o", "KnownHostsCommand=none",
		"-o", "VerifyHostKeyDNS=no",
		"-o", "UpdateHostKeys=no",
		"-o", "ForwardAgent=no",
	)
	if knownHostsPath != "" {
		args = append(args,
			"-o", "UserKnownHostsFile="+knownHostsPath,
			"-o", "StrictHostKeyChecking="+strictHostKeyChecking,
			"-o", "HashKnownHosts=no",
		)
	}
	address := target.Address
	if target.User != "" {
		address = target.User + "@" + address
	}
	args = append(args, address)
	for _, extraArg := range extraArgs {
		args = append(args, shellquote.QuoteWord(extraArg))
	}
	return sshInvocation{Path: sshPath, Args: args, Env: filteredSSHEnvironment(os.Environ()), MaterialFiles: files}
}

var sshPolicyCryptoDirectives = map[string]bool{
	"ciphers":                     true,
	"macs":                        true,
	"kexalgorithms":               true,
	"gssapikexalgorithms":         true,
	"hostkeyalgorithms":           true,
	"pubkeyacceptedalgorithms":    true,
	"pubkeyacceptedkeytypes":      true,
	"hostbasedacceptedalgorithms": true,
	"hostbasedkeytypes":           true,
	"casignaturealgorithms":       true,
	"requiredrsasize":             true,
	"rsaminsize":                  true,
	"rekeylimit":                  true,
}

var sshPolicyUnsafeDirectives = map[string]bool{
	"identityfile":        true,
	"certificatefile":     true,
	"identityagent":       true,
	"pkcs11provider":      true,
	"securitykeyprovider": true,
	"addkeystoagent":      true,
	"hostname":            true,
	"port":                true,
	"user":                true,
	"proxycommand":        true,
	"proxyjump":           true,
	"localcommand":        true,
	"permitlocalcommand":  true,
	"knownhostscommand":   true,
	"localforward":        true,
	"remoteforward":       true,
	"dynamicforward":      true,
	"include":             true,
	"match":               true,
}

func loadSanitizedSSHPolicy(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read OpenSSH crypto policy %s: %w", path, err)
	}
	var lines []string
	for number, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key := strings.Fields(line)[0]
		if i := strings.IndexByte(key, '='); i >= 0 {
			key = key[:i]
		}
		switch {
		case sshPolicyCryptoDirectives[strings.ToLower(key)]:
			lines = append(lines, line)
		case sshPolicyUnsafeDirectives[strings.ToLower(key)]:
			return nil, fmt.Errorf("OpenSSH crypto policy %s:%d contains unsupported directive %q", path, number+1, key)
		}
	}
	if len(lines) == 0 {
		return nil, nil
	}
	return []byte(strings.Join(lines, "\n") + "\n"), nil
}

func filteredSSHEnvironment(environ []string) []string {
	var out []string
	for _, entry := range environ {
		key, _, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		switch {
		case key == "TERM",
			key == "COLORTERM",
			key == "NO_COLOR":
			out = append(out, entry)
		}
	}
	return out
}

func anonymousSSHMaterial(dir string, data []byte) (*os.File, error) {
	file, err := os.CreateTemp(dir, ".ssh-material-")
	if err != nil {
		return nil, fmt.Errorf("create temporary SSH material: %w", err)
	}
	path := file.Name()
	cleanup := func() {
		_ = file.Close()
		_ = os.Remove(path)
	}
	if err := file.Chmod(0o600); err != nil {
		cleanup()
		return nil, fmt.Errorf("chmod temporary SSH material: %w", err)
	}
	if err := os.Remove(path); err != nil {
		cleanup()
		return nil, fmt.Errorf("unlink temporary SSH material: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		cleanup()
		return nil, fmt.Errorf("write temporary SSH material: %w", err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		cleanup()
		return nil, fmt.Errorf("rewind temporary SSH material: %w", err)
	}
	return file, nil
}

func sshParentFDPath(file *os.File) string {
	return fmt.Sprintf("/proc/%d/fd/%d", os.Getpid(), file.Fd())
}

func closeSSHInvocationFiles(invocation sshInvocation) {
	closeSSHFiles(invocation.MaterialFiles)
}

func closeSSHFiles(files []*os.File) {
	for _, file := range files {
		_ = file.Close()
	}
}

func runSSHInvocation(ctx context.Context, invocation sshInvocation) (int, error) {
	err := execution.OSRunner{}.Run(ctx, execution.Command{
		Name:           invocation.Path,
		Args:           invocation.Args[1:],
		Env:            invocation.Env,
		Stdin:          os.Stdin,
		Stdout:         os.Stdout,
		Stderr:         os.Stderr,
		GracefulCancel: true,
		WaitDelay:      5 * time.Second,
	})
	if err == nil {
		return 0, nil
	}
	if code, ok := execution.ExitCode(err); ok {
		return code, nil
	}
	return 0, err
}
