//go:build linux && amd64

package nativelocal

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

// rpmQueryFormat prints one line per installed instance: its identity, then
// both signatures rpm records over it, the header-only one and the one over
// header and payload, each "(none)" when absent.
const rpmQueryFormat = "%{NAME}\t%{EPOCH}\t%{VERSION}\t%{RELEASE}\t%{ARCH}\t%{RSAHEADER:pgpsig}\t%{SIGPGP:pgpsig}\n"

// vendorKeyID is the key ID rpm prints for a signature of the RHEL 9 profile's
// vendor key: the low 64 bits of that key's fingerprint, as rpm's pgpsig
// format prints a signer.
var vendorKeyID = rhel9Signer[len(rhel9Signer)-16:]

// operatorQuery is what one rpm query printed and how it exited.
type operatorQuery struct {
	Stdout, Stderr []byte
	Exit           int
}

// operatorPlatform is the one platform whose installer-media tooling the
// operator installs from the host's own vendor-signed repositories (D106).
var operatorPlatform = prerequisites.Platform{OS: "rhel", Release: "9.8", Architecture: "amd64"}

// OperatorRoots reads, from a snapshot of the host's own package database,
// which of the named installer-media roots the operator installed, and whether
// a signature of the RHEL 9 vendor key covers every installed instance. It
// proves identity and signer, never file integrity, exactly as presence proves
// any root, and contacts no repository.
func (r *Resolver) OperatorRoots(ctx context.Context, platform prerequisites.Platform, names []string) (prerequisites.OperatorPresence, error) {
	if err := ctx.Err(); err != nil {
		return prerequisites.OperatorPresence{}, err
	}
	if r == nil {
		return prerequisites.OperatorPresence{}, failure("native dependency inspection is unavailable")
	}
	if platform != operatorPlatform {
		return prerequisites.OperatorPresence{}, failure("operator-installed roots are read only on a RHEL 9.8 controller")
	}
	if !operatorNames(names) {
		return prerequisites.OperatorPresence{}, failure("operator-installed roots must name the installer-media root packages")
	}
	if providedFile("/usr/bin/rpm", true) != nil {
		return prerequisites.OperatorPresence{}, failure("native readiness requires the provided OS rpm")
	}
	snapshot, err := r.stageDatabase(platform)
	if err != nil {
		return prerequisites.OperatorPresence{}, err
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	results := map[string]operatorQuery{}
	for _, name := range names {
		result, err := queryRPM(bounded, snapshot, name)
		if err != nil {
			if ctx.Err() != nil {
				return prerequisites.OperatorPresence{}, ctx.Err()
			}
			return prerequisites.OperatorPresence{}, err
		}
		results[name] = result
	}
	return operatorPresence(names, results, vendorKeyID)
}

// operatorNames admits only the installer-media root packages, each once, so
// nothing but a fixed package name ever reaches rpm's command line.
func operatorNames(names []string) bool {
	allowed := prerequisites.NativeRootNames()["installer-media"]
	if len(names) == 0 || len(names) > len(allowed) {
		return false
	}
	for index, name := range names {
		if !slices.Contains(allowed, name) || slices.Contains(names[:index], name) {
			return false
		}
	}
	return true
}

// queryRPM asks the provided rpm about one package in the snapshot, with the
// native helper's environment and, as root, the unprivileged identity the
// snapshot was granted to.
func queryRPM(ctx context.Context, snapshot, name string) (operatorQuery, error) {
	command := exec.CommandContext(ctx, "/usr/bin/rpm", "--dbpath", filepath.Join(snapshot, "var", "lib", "rpm"), "-q", "--queryformat", rpmQueryFormat, "--", name)
	command.Dir = "/"
	command.Env = helperEnvironment(filepath.Join(filepath.Dir(snapshot), "home"))
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if os.Geteuid() == 0 {
		command.SysProcAttr.Credential = &syscall.Credential{Uid: unprivilegedID, Gid: unprivilegedID, NoSetGroups: false}
	}
	command.WaitDelay = 2 * time.Second
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
	}
	output := &boundedBuffer{limit: 64 << 10}
	diagnostic := &boundedBuffer{limit: 64 << 10}
	command.Stdout, command.Stderr = output, diagnostic
	err := command.Run()
	if ctx.Err() != nil {
		return operatorQuery{}, failure("the provided rpm did not answer within its bound")
	}
	var exit *exec.ExitError
	if err != nil && (!errors.As(err, &exit) || !exit.Exited()) {
		return operatorQuery{}, failure("the provided rpm could not read the installed package database")
	}
	result := operatorQuery{Stdout: slices.Clone(output.Bytes()), Stderr: slices.Clone(diagnostic.Bytes())}
	if exit != nil {
		result.Exit = exit.ExitCode()
	}
	return result, nil
}

// operatorPresence is what the queries of names prove. A package with no
// installed instance is Missing, one with any instance no vendor signature
// covers is Foreign, and each instance of the rest is installed.
func operatorPresence(names []string, results map[string]operatorQuery, keyID string) (prerequisites.OperatorPresence, error) {
	presence := prerequisites.OperatorPresence{Installed: []prerequisites.NativeRootPresence{}, Missing: []string{}, Foreign: []string{}}
	for _, name := range names {
		result, found := results[name]
		if !found {
			return prerequisites.OperatorPresence{}, failure("operator-installed root inspection is incomplete")
		}
		instances, signed, installed, err := parseOperatorQuery(name, result, keyID)
		switch {
		case err != nil:
			return prerequisites.OperatorPresence{}, err
		case !installed:
			presence.Missing = append(presence.Missing, name)
		case !signed:
			presence.Foreign = append(presence.Foreign, name)
		default:
			for _, instance := range instances {
				presence.Installed = append(presence.Installed, prerequisites.NativeRootPresence{Key: "installer-media", Package: instance})
			}
		}
	}
	presence.Ready = len(presence.Missing) == 0 && len(presence.Foreign) == 0
	return presence, nil
}

// parseOperatorQuery reads one package's query. rpm exits nonzero and prints
// exactly "package <name> is not installed" on standard output, with nothing on
// standard error, for a package with no instance; any other refusal is a
// database it could not read, never an absence. Every instance line must name
// the package and carry the identity and signature fields the query format
// asks for.
func parseOperatorQuery(name string, result operatorQuery, keyID string) ([]prerequisites.NativeIdentity, bool, bool, error) {
	unreadable := failure("the provided rpm reported unreadable package evidence")
	if result.Exit != 0 {
		if string(result.Stdout) == "package "+name+" is not installed\n" && len(result.Stderr) == 0 {
			return nil, false, false, nil
		}
		return nil, false, false, failure("the provided rpm could not read the installed package database")
	}
	text := string(result.Stdout)
	if !strings.HasSuffix(text, "\n") {
		return nil, false, false, unreadable
	}
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) > 16 {
		return nil, false, false, unreadable
	}
	signed := true
	instances := make([]prerequisites.NativeIdentity, 0, len(lines))
	for _, line := range lines {
		fields := strings.Split(line, "\t")
		if len(fields) != 7 || fields[0] != name || !operatorText(fields[2]) || !operatorText(fields[3]) || !operatorText(fields[4]) {
			return nil, false, false, unreadable
		}
		epoch := 0
		if fields[1] != "(none)" {
			value, err := strconv.Atoi(fields[1])
			if err != nil || value < 0 || value > 2147483647 || strconv.Itoa(value) != fields[1] {
				return nil, false, false, unreadable
			}
			epoch = value
		}
		vendor, err := vendorSigned(fields[5:], keyID)
		if err != nil {
			return nil, false, false, err
		}
		signed = signed && vendor
		instances = append(instances, prerequisites.NativeIdentity{Name: name, Epoch: epoch, Version: fields[2], Release: fields[3], Architecture: fields[4]})
	}
	return instances, signed, true, nil
}

// vendorSigned reports whether these signatures prove the vendor key: at least
// one is present and every present one names it. rpm prints a present
// signature as "<algorithm>/<hash>, <date>, Key ID <key ID>".
func vendorSigned(signatures []string, keyID string) (bool, error) {
	present := 0
	for _, signature := range signatures {
		if signature == "(none)" {
			continue
		}
		index := strings.LastIndex(signature, ", Key ID ")
		if index <= 0 || !strings.Contains(signature[:index], "/") {
			return false, failure("the provided rpm reported an unreadable package signature")
		}
		signer := signature[index+len(", Key ID "):]
		if len(signer) < 16 || len(signer) > 64 || strings.Trim(signer, "0123456789abcdef") != "" {
			return false, failure("the provided rpm reported an unreadable package signature")
		}
		if signer != keyID {
			return false, nil
		}
		present++
	}
	return present != 0, nil
}

func operatorText(value string) bool {
	return value != "" && len(value) <= 128 && !strings.ContainsAny(value, " \t\r\n\x00") && value != "(none)"
}
