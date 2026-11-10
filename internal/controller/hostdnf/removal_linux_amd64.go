//go:build linux && amd64

package hostdnf

import (
	"context"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

const (
	uninstallCode  = "controller.uninstall"
	snapshotFormat = `%{NAME}\t%{EPOCHNUM}:%{VERSION}-%{RELEASE}\t%{ARCH}\t%{INSTALLTIME}\n`
	snapshotLimit  = 8 << 20
	maxSnapshot    = 65536
	maxRemovals    = prerequisites.MaxAddedPackages
	noCleanup      = "--setopt=clean_requirements_on_remove=False"
)

var (
	packageArgument = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+-]*-[A-Za-z0-9._+~^]+-[A-Za-z0-9._+~^]+\.[A-Za-z0-9_]+$`)
	neededBy        = regexp.MustCompile(`^\s*\S.* is needed by \(installed\) (\S+)\s*$`)
)

var _ prerequisites.HostPackageRemoval = (*Packages)(nil)

func (p *Packages) Snapshot(ctx context.Context) ([]prerequisites.InstalledPackage, error) {
	rpm, err := p.qualify(p.rpm)
	if err != nil {
		return nil, unqualifiedFor(uninstallCode, "rpm")
	}
	querying, cancel := context.WithTimeout(ctx, repoTimeout)
	defer cancel()
	stdout, stderr := &capped{limit: snapshotLimit}, &capped{limit: errorLimit}
	args := []string{"-qa", "--queryformat", snapshotFormat}
	exit, err := p.run(querying, command{path: rpm, args: args, env: p.rpmEnvironment()}, stdout, stderr)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	unreadable := diagnostics.NewFailureWithRemediation("controller.state", "rpm could not list the installed packages", "", "check that the RPM database is readable")
	if err != nil || exit != 0 || stdout.truncated || stderr.Len() != 0 {
		return nil, unreadable
	}
	installed := []prerequisites.InstalledPackage{}
	for _, line := range strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n") {
		fields := strings.Split(line, "\t")
		if len(fields) != 4 || len(installed) == maxSnapshot {
			return nil, unreadable
		}
		if fields[0] == "gpg-pubkey" && fields[2] == "(none)" {
			continue
		}
		installTime, err := strconv.ParseInt(fields[3], 10, 64)
		item := prerequisites.InstalledPackage{NEVRA: fields[0] + "-" + fields[1] + "." + fields[2], InstallTime: installTime}
		if err != nil || !prerequisites.ValidInstalledPackage(item) {
			return nil, unreadable
		}
		installed = append(installed, item)
	}
	return installed, nil
}

func (p *Packages) EraseTest(ctx context.Context, names []string) error {
	if err := validNames(names); err != nil {
		return err
	}
	rpm, err := p.qualify(p.rpm)
	if err != nil {
		return unqualifiedFor(uninstallCode, "rpm")
	}
	erasing, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	stdout, stderr := &capped{limit: errorLimit}, &capped{limit: errorLimit}
	args := append([]string{"--erase", "--test", "--"}, names...)
	exit, err := p.run(erasing, command{path: rpm, args: args, env: p.rpmEnvironment()}, stdout, stderr)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return removalFailure("rpm could not run to test the removal", "check that /usr/bin/rpm runs")
	}
	if exit == 0 {
		return nil
	}
	if dependents := dependentsOf(stderr.String() + "\n" + stdout.String()); len(dependents) != 0 {
		return removalFailure("other installed packages require "+strings.Join(names, ", ")+": "+strings.Join(dependents, ", "),
			"remove the dependent packages first, or pass --keep-packages")
	}
	return removalFailure("rpm refused the removal of "+strings.Join(names, ", ")+": "+lastError(stderr.String(), stdout.String()),
		"resolve what rpm reports, or pass --keep-packages")
}

func dependentsOf(text string) []string {
	dependents := []string{}
	for _, line := range strings.Split(text, "\n") {
		match := neededBy.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		dependent := sanitize(match[1])
		if !slices.Contains(dependents, dependent) {
			dependents = append(dependents, dependent)
		}
	}
	slices.Sort(dependents)
	return dependents
}

func (p *Packages) RemovalPreview(ctx context.Context, names []string) (prerequisites.HostPackagePreview, error) {
	if err := validNames(names); err != nil {
		return prerequisites.HostPackagePreview{}, err
	}
	dnf, err := p.qualify(p.dnf)
	if err != nil {
		return prerequisites.HostPackagePreview{}, unqualifiedFor(uninstallCode, "dnf")
	}
	previewing, cancel := context.WithTimeout(ctx, previewTime)
	defer cancel()
	stdout, stderr := &capped{limit: previewLimit}, &capped{limit: errorLimit}
	args := append([]string{"remove", "--assumeno", noCleanup, "--"}, names...)
	exit, err := p.run(previewing, command{path: dnf, args: args, env: p.dnfEnvironment()}, stdout, stderr)
	if ctx.Err() != nil {
		return prerequisites.HostPackagePreview{}, ctx.Err()
	}
	if previewing.Err() != nil {
		return prerequisites.HostPackagePreview{}, removalFailure("dnf did not finish the removal preview within 20 minutes", "check the host's dnf configuration")
	}
	if err != nil {
		return prerequisites.HostPackagePreview{}, removalFailure("dnf could not run to preview the removal", "check that /usr/bin/dnf runs")
	}
	if exit != 0 && (exit != 1 || !strings.Contains(stderr.String(), "Operation aborted")) {
		return prerequisites.HostPackagePreview{}, removalFailure("dnf could not preview the removal: "+lastError(stderr.String(), stdout.String()), "fix the host's dnf configuration")
	}
	return prerequisites.HostPackagePreview{Text: sanitize(stdout.String()), Truncated: stdout.truncated}, nil
}

func (p *Packages) Remove(ctx context.Context, names []string, output prerequisites.RunOutput) error {
	if err := validNames(names); err != nil {
		return err
	}
	dnf, err := p.qualify(p.dnf)
	if err != nil {
		return unqualifiedFor(uninstallCode, "dnf")
	}
	removing, cancel := context.WithTimeout(ctx, installTime)
	defer cancel()
	stream := &stream{out: output, tail: &tail{limit: tailLimit}}
	args := append([]string{"remove", "--assumeyes", noCleanup, "--"}, names...)
	exit, err := p.run(removing, command{path: dnf, args: args, env: p.dnfEnvironment()}, stream, stream)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil || exit != 0 {
		return removalFailure("dnf did not complete the removal: "+lastError(stream.tail.String(), ""), "review the host's dnf configuration, then run bootwright controller uninstall again")
	}
	return nil
}

func validNames(names []string) error {
	if len(names) == 0 || len(names) > maxRemovals {
		return removalFailure("the host package removal is empty or too large", "")
	}
	for index, name := range names {
		if len(name) > 256 || !packageArgument.MatchString(name) || slices.Contains(names[:index], name) {
			return removalFailure("a host package to remove is outside the package grammar", "")
		}
	}
	return nil
}

func removalFailure(message, remediation string) error {
	return diagnostics.NewFailureWithRemediation(uninstallCode, message, "", remediation)
}

func unqualifiedFor(code, tool string) error {
	return diagnostics.NewFailureWithRemediation(code, tool+" is not a root-owned system executable", "",
		"restore the host's "+tool+" under /usr/bin with root ownership and no group or other write access")
}
