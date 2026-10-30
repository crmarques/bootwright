//go:build linux && amd64

package hostdnf

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

const (
	rpmPath       = "/usr/bin/rpm"
	dnfPath       = "/usr/bin/dnf"
	previewLimit  = 64 << 10
	errorLimit    = 64 << 10
	tailLimit     = 8 << 10
	maxRequests   = 64
	queryTimeout  = 30 * time.Second
	repoTimeout   = 2 * time.Minute
	previewTime   = 20 * time.Minute
	installTime   = time.Hour
	queryFormat   = `%{NAME}\t%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}\n`
	provideFormat = `[\t%{PROVIDENEVRS}\n]`
	weakDeps      = "--setopt=install_weak_deps=False"
)

var (
	corePackage    = regexp.MustCompile(`^(glibc[A-Za-z0-9._+-]*|libgcc|kernel[A-Za-z0-9._+-]*|systemd[A-Za-z0-9._+-]*|openssl-libs)$`)
	problemLine    = regexp.MustCompile(`^Problem( [0-9]+)?:`)
	providerFormat = regexp.MustCompile(`^([A-Za-z0-9._+-]+)\t([0-9]+:[A-Za-z0-9._+~^-]+\.([A-Za-z0-9_]+))$`)
)

type command struct {
	path string
	args []string
	env  []string
}

type runner func(ctx context.Context, c command, stdout, stderr io.Writer) (int, error)

type Packages struct {
	rpm, dnf string
	base     []string
	proxy    []string
	run      runner
	qualify  func(string) (string, error)
}

var _ prerequisites.HostPackages = (*Packages)(nil)

func New(route controller.Route) (*Packages, error) {
	if _, err := route.Selector(); err != nil {
		return nil, err
	}
	return withProxy(route.HTTPSProxy(), route.NoProxy()), nil
}

func ForEgress(egress prerequisites.SetupEgress) (*Packages, error) {
	if _, err := controller.NewProxySelector(egress.HTTPProxy, egress.HTTPSProxy, egress.NoProxy); err != nil {
		return nil, failure("the controller Machine's proxy route is outside the qualified grammar", "")
	}
	return withProxy(egress.HTTPSProxy, egress.NoProxy), nil
}

func withProxy(endpoint string, bypass []string) *Packages {
	var proxy []string
	if endpoint != "" {
		proxy = append(proxy, "https_proxy="+endpoint)
		if len(bypass) != 0 {
			proxy = append(proxy, "no_proxy="+strings.Join(bypass, ","))
		}
	}
	return &Packages{
		rpm: rpmPath, dnf: dnfPath, base: []string{"LC_ALL=C", "PATH=/usr/sbin:/usr/bin"}, proxy: proxy,
		run: execute, qualify: qualifiedSystemExecutable,
	}
}

type provider struct{ name, evra, arch string }

func (p provider) nevra() string { return p.name + "-" + p.evra }

type query struct {
	present   bool
	providers []provider
	err       error
}

func (p *Packages) Missing(ctx context.Context, requirements []string) ([]string, error) {
	if err := validRequirements(requirements, true); err != nil {
		return nil, err
	}
	results, err := p.queries(ctx, requirements)
	if err != nil {
		return nil, err
	}
	missing := []string{}
	for index, result := range results {
		if !result.present {
			missing = append(missing, requirements[index])
		}
	}
	return missing, nil
}

func (p *Packages) queries(ctx context.Context, requirements []string) ([]query, error) {
	rpm, err := p.qualify(p.rpm)
	if err != nil {
		return nil, unqualified("rpm")
	}
	results := make([]query, len(requirements))
	var group sync.WaitGroup
	for index, requirement := range requirements {
		group.Add(1)
		go func() {
			defer group.Done()
			results[index] = p.query(ctx, rpm, requirement)
		}()
	}
	group.Wait()
	for _, result := range results {
		if result.err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, result.err
		}
	}
	return results, nil
}

func (p *Packages) query(ctx context.Context, rpm, requirement string) query {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()
	name, _, versioned := strings.Cut(requirement, " = ")
	format := queryFormat
	if versioned {
		format += provideFormat
	}
	stdout, stderr := &capped{limit: errorLimit}, &capped{limit: errorLimit}
	args := []string{"-q", "--whatprovides", "--queryformat", format, "--", name}
	exit, err := p.run(ctx, command{path: rpm, args: args, env: p.rpmEnvironment()}, stdout, stderr)
	unreadable := query{err: failure("rpm could not report what provides "+requirement, "Check that the RPM database is readable")}
	switch {
	case err != nil || stdout.truncated:
		return unreadable
	case exit == 1 && stdout.String() == "no package provides "+name+"\n" && stderr.Len() == 0:
		return query{}
	case exit != 0 || stderr.Len() != 0 || stdout.Len() == 0:
		return unreadable
	}
	var listed []provider
	satisfies := map[int]bool{}
	for _, line := range strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n") {
		if provides, ok := strings.CutPrefix(line, "\t"); ok && versioned && len(listed) != 0 {
			satisfies[len(listed)-1] = satisfies[len(listed)-1] || provides == requirement
			continue
		}
		fields := providerFormat.FindStringSubmatch(line)
		if fields == nil || len(listed) == 16 {
			return unreadable
		}
		listed = append(listed, provider{name: fields[1], evra: fields[2], arch: fields[3]})
	}
	result := query{}
	for index, installed := range listed {
		if (!versioned || satisfies[index]) && !slices.Contains(result.providers, installed) {
			result.providers = append(result.providers, installed)
		}
	}
	result.present = len(result.providers) != 0
	return result
}

func (p *Packages) rpmEnvironment() []string { return slices.Clone(p.base) }

func (p *Packages) dnfEnvironment() []string { return append(slices.Clone(p.base), p.proxy...) }

func (p *Packages) Preview(ctx context.Context, missing []string) (prerequisites.HostPackagePreview, error) {
	if err := validRequirements(missing, false); err != nil {
		return prerequisites.HostPackagePreview{}, err
	}
	dnf, err := p.qualify(p.dnf)
	if err != nil {
		return prerequisites.HostPackagePreview{}, unqualified("dnf")
	}
	previewing, cancel := context.WithTimeout(ctx, previewTime)
	defer cancel()
	stdout, stderr := &capped{limit: previewLimit}, &capped{limit: errorLimit}
	args := append([]string{"install", "--assumeno", weakDeps, "--"}, missing...)
	exit, err := p.run(previewing, command{path: dnf, args: args, env: p.dnfEnvironment()}, stdout, stderr)
	if ctx.Err() != nil {
		return prerequisites.HostPackagePreview{}, ctx.Err()
	}
	if previewing.Err() != nil {
		return prerequisites.HostPackagePreview{}, failure("dnf did not finish the install preview within 20 minutes", "Check the host's repository reachability")
	}
	if err != nil {
		return prerequisites.HostPackagePreview{}, failure("dnf could not run to preview the install", "Check that /usr/bin/dnf runs")
	}
	diagnostic := stderr.String()
	if exit != 0 {
		if refusal := classify(diagnostic, stdout.String(), missing); refusal != nil {
			return prerequisites.HostPackagePreview{}, refusal
		}
		if exit != 1 || !strings.Contains(diagnostic, "Operation aborted") {
			return prerequisites.HostPackagePreview{}, failure("dnf could not preview the install: "+lastError(diagnostic, stdout.String()), "Fix the host's dnf configuration")
		}
	}
	text := sanitize(stdout.String())
	preview := prerequisites.HostPackagePreview{Text: text, Truncated: stdout.truncated}
	if names := coreNames(text, missing); len(names) != 0 {
		preview.Notes = []string{"this install also updates core packages (" + strings.Join(names, ", ") + "); dnf applies it as for any package install"}
	}
	return preview, nil
}

func (p *Packages) Install(ctx context.Context, missing []string, output prerequisites.RunOutput) ([]prerequisites.HostPackageEvidence, error) {
	if err := validRequirements(missing, false); err != nil {
		return nil, err
	}
	dnf, err := p.qualify(p.dnf)
	if err != nil {
		return nil, unqualified("dnf")
	}
	installing, cancel := context.WithTimeout(ctx, installTime)
	defer cancel()
	stream := &stream{out: output, tail: &tail{limit: tailLimit}}
	args := append([]string{"install", "--assumeyes", weakDeps, "--"}, missing...)
	exit, err := p.run(installing, command{path: dnf, args: args, env: p.dnfEnvironment()}, stream, stream)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil || exit != 0 {
		return nil, failure("dnf did not complete the install: "+lastError(stream.tail.String(), ""), "Fix the host's dnf configuration (dnf's output is in this run's log)")
	}
	results, err := p.queries(ctx, missing)
	if err != nil {
		return nil, err
	}
	var evidence []prerequisites.HostPackageEvidence
	repositories := p.repositories(ctx, dnf, results)
	for index, result := range results {
		if !result.present {
			return nil, failure("dnf reported success but nothing provides "+missing[index], "Check that the host's repositories provide it")
		}
		for _, installed := range result.providers {
			evidence = append(evidence, prerequisites.HostPackageEvidence{Requirement: missing[index], Package: installed.nevra(), Repository: repositories[installed.name+" "+installed.arch]})
		}
	}
	return evidence, nil
}

func (p *Packages) Qualify(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := p.qualify(p.rpm); err != nil {
		return unqualified("rpm")
	}
	if _, err := p.qualify(p.dnf); err != nil {
		return unqualified("dnf")
	}
	return nil
}

func (p *Packages) Installed(ctx context.Context, requirements []string) ([]prerequisites.HostPackageEvidence, error) {
	return p.evidence(ctx, requirements, false)
}

func (p *Packages) Present(ctx context.Context, requirements []string) ([]prerequisites.HostPackageEvidence, error) {
	return p.evidence(ctx, requirements, true)
}

func (p *Packages) evidence(ctx context.Context, requirements []string, origin bool) ([]prerequisites.HostPackageEvidence, error) {
	if err := validRequirements(requirements, true); err != nil {
		return nil, err
	}
	results, err := p.queries(ctx, requirements)
	if err != nil {
		return nil, err
	}
	repositories := map[string]string{}
	if dnf, err := p.qualify(p.dnf); err == nil && origin {
		repositories = p.repositories(ctx, dnf, results)
	}
	evidence := []prerequisites.HostPackageEvidence{}
	for index, result := range results {
		for _, installed := range result.providers {
			evidence = append(evidence, prerequisites.HostPackageEvidence{Requirement: requirements[index], Package: installed.nevra(), Repository: repositories[installed.name+" "+installed.arch]})
		}
	}
	return evidence, ctx.Err()
}

func (p *Packages) repositories(ctx context.Context, dnf string, results []query) map[string]string {
	var names []string
	for _, result := range results {
		for _, installed := range result.providers {
			if !slices.Contains(names, installed.name) {
				names = append(names, installed.name)
			}
		}
	}
	repositories := map[string]string{}
	if len(names) == 0 {
		return repositories
	}
	ctx, cancel := context.WithTimeout(ctx, repoTimeout)
	defer cancel()
	stdout, stderr := &capped{limit: errorLimit}, &capped{limit: errorLimit}
	args := append([]string{"repoquery", "--installed", "--qf", `%{name} %{arch} %{from_repo}\n`, "--"}, names...)
	if exit, err := p.run(ctx, command{path: dnf, args: args, env: p.dnfEnvironment()}, stdout, stderr); err != nil || exit != 0 {
		return repositories
	}
	for _, line := range strings.Split(stdout.String(), "\n") {
		if fields := strings.Fields(line); len(fields) == 3 && fields[2] != "(none)" && fields[2] != "@System" && printable(fields[2]) {
			repositories[fields[0]+" "+fields[1]] = fields[2]
		}
	}
	return repositories
}

func validRequirements(requirements []string, allowEmpty bool) error {
	if len(requirements) == 0 && !allowEmpty || len(requirements) > maxRequests {
		return failure("the host package request is empty or too large", "")
	}
	for index, requirement := range requirements {
		if !prerequisites.ValidHostRequirement(requirement) || slices.Contains(requirements[:index], requirement) {
			return failure("a host package requirement is outside the name and capability grammar", "")
		}
	}
	return nil
}

func classify(diagnostic, standard string, missing []string) error {
	var matched []string
	problem := false
	for _, line := range strings.Split(diagnostic+"\n"+standard, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "No match for argument:") || strings.Contains(line, "Unable to find a match") {
			matched = append(matched, line)
		}
		problem = problem || problemLine.MatchString(line)
	}
	last := lastError(diagnostic, standard)
	if len(matched) != 0 {
		named := []string{}
		for _, requirement := range missing {
			if slices.ContainsFunc(matched, func(line string) bool { return namesArgument(line, requirement) }) {
				named = append(named, requirement)
			}
		}
		if len(named) == 0 {
			named = missing
		}
		return failure("no package in the host's repositories provides "+strings.Join(named, ", ")+": "+last,
			"Enable the repository that provides it in the host's dnf configuration")
	}
	if problem {
		return failure("dnf cannot resolve the install of "+strings.Join(missing, ", ")+": "+last,
			"Resolve the dependency problem in the host's dnf configuration")
	}
	return nil
}

func namesArgument(line, requirement string) bool {
	index := strings.LastIndex(line, ": ")
	if index < 0 {
		return false
	}
	arguments := strings.TrimSpace(line[index+2:])
	return arguments == requirement || slices.Contains(strings.Fields(arguments), requirement)
}

func lastError(texts ...string) string {
	for _, text := range texts {
		lines := strings.Split(text, "\n")
		for index := len(lines) - 1; index >= 0; index-- {
			line := strings.TrimSpace(lines[index])
			if line == "" || strings.HasPrefix(line, "You can try") || strings.HasPrefix(line, "--") ||
				strings.HasPrefix(line, "(try to") || strings.HasPrefix(line, "Operation aborted") || strings.HasPrefix(line, "Error: Operation aborted") {
				continue
			}
			line = sanitize(line)
			if len(line) > 240 {
				line = strings.ToValidUTF8(line[:240], "") + "..."
			}
			return line
		}
	}
	return "dnf printed no error"
}

func coreNames(text string, missing []string) []string {
	python := ""
	for _, requirement := range missing {
		if abi, ok := prerequisites.PythonRequirementABI(requirement); ok {
			python = "python" + abi
		}
	}
	var names []string
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		core := corePackage.MatchString(name) || python != "" && (name == python || strings.HasPrefix(name, python+"-"))
		if core && !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

func sanitize(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || r >= 0x20 && r != 0x7f && !(r >= 0x80 && r < 0xa0) && r != utf8.RuneError {
			return r
		}
		return '?'
	}, text)
}

func printable(text string) bool {
	return text != "" && len(text) <= 128 && sanitize(text) == text && !strings.ContainsAny(text, " \t\n")
}

func failure(message, correction string) error {
	if correction == "" {
		return diagnostics.NewFailure("controller.install", message, "")
	}
	return &prerequisites.ScopedFailure{Code: "controller.install", Message: message, Correction: correction}
}

func unqualified(tool string) error {
	return failure(tool+" is not a root-owned system executable", "Restore the host's "+tool+" under /usr/bin with root ownership and no group or other write access")
}

type capped struct {
	data      []byte
	limit     int
	truncated bool
}

func (c *capped) Write(p []byte) (int, error) {
	room := c.limit - len(c.data)
	if len(p) > room {
		c.truncated = true
		p = p[:max(room, 0)]
	}
	c.data = append(c.data, p...)
	return len(p), nil
}

func (c *capped) String() string { return string(c.data) }
func (c *capped) Len() int       { return len(c.data) }

type tail struct {
	data  []byte
	limit int
}

func (t *tail) Write(p []byte) (int, error) {
	t.data = append(t.data, p...)
	if len(t.data) > t.limit {
		t.data = t.data[len(t.data)-t.limit:]
	}
	return len(p), nil
}

func (t *tail) String() string { return string(t.data) }

type stream struct {
	out  prerequisites.RunOutput
	tail *tail
}

// A log sink that fails must not abort a transaction dnf has begun.
func (s *stream) Write(p []byte) (int, error) {
	s.tail.Write(p)
	if s.out != nil {
		_, _ = s.out.Write(p)
	}
	return len(p), nil
}

func execute(ctx context.Context, c command, stdout, stderr io.Writer) (int, error) {
	cmd := exec.CommandContext(ctx, c.path, c.args...)
	cmd.Dir = "/"
	cmd.Env = c.env
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = 30 * time.Second
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	err := cmd.Run()
	if err == nil {
		return 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.Exited() {
		return exit.ExitCode(), nil
	}
	return -1, err
}

func qualifiedSystemExecutable(path string) (string, error) {
	path, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Mode().Perm()&0111 == 0 {
		return "", errors.New("unqualified executable")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Uid != 0 {
		return "", errors.New("unqualified executable")
	}
	for parent := filepath.Dir(path); ; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
			return "", errors.New("unqualified executable")
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Uid != 0 {
			return "", errors.New("unqualified executable")
		}
		if parent == "/" {
			return path, nil
		}
	}
}
