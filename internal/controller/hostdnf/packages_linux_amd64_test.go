//go:build linux && amd64

package hostdnf

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

const (
	rpmQuery       = `%{NAME}\t%{EPOCHNUM}:%{VERSION}-%{RELEASE}.%{ARCH}\n`
	rpmProvideList = `[\t%{PROVIDENEVRS}\n]`
	baseEnv        = "LC_ALL=C PATH=/usr/sbin:/usr/bin"
)

type reply struct {
	exit           int
	stdout, stderr string
	err            error
}

type fake struct {
	mu      sync.Mutex
	calls   []command
	respond func(command) reply
}

func (f *fake) run(_ context.Context, c command, stdout, stderr io.Writer) (int, error) {
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()
	r := f.respond(c)
	_, _ = io.WriteString(stdout, r.stdout)
	_, _ = io.WriteString(stderr, r.stderr)
	return r.exit, r.err
}

func (f *fake) called(subcommand string) []command {
	var found []command
	for _, c := range f.calls {
		if c.path == "/usr/bin/dnf" && c.args[0] == subcommand || c.path == "/usr/bin/rpm" && subcommand == "rpm" {
			found = append(found, c)
		}
	}
	return found
}

func packagesWith(t *testing.T, respond func(command) reply) (*Packages, *fake) {
	t.Helper()
	double := &fake{respond: respond}
	p, err := New(controller.Route{})
	if err != nil {
		t.Fatal(err)
	}
	p.run = double.run
	p.qualify = func(path string) (string, error) { return path, nil }
	return p, double
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func providedEverywhere(c command) reply {
	name := c.args[len(c.args)-1]
	if strings.ContainsAny(name, " =<>") {
		return reply{exit: 1, stdout: "no package provides " + name + "\n"}
	}
	if name != "python(abi)" {
		return reply{stdout: name + "\t0:1.2.3-4.fc44.x86_64\n"}
	}
	header := "python3.13\t0:3.13.9-1.fc44.x86_64\n"
	if c.args[3] != rpmQuery+rpmProvideList {
		return reply{stdout: header + header}
	}
	listed := header + "\tpython(abi) = 3.13\n\tpython(abi) = 3.13\n\tpython3.13 = 3.13.9-1.fc44\n"
	return reply{stdout: listed + listed}
}

func message(t *testing.T, err error) (string, string) {
	t.Helper()
	found := diagnostics.Of(err)
	if len(found) != 1 || found[0].Code != "controller.setup" {
		t.Fatalf("not one controller.setup refusal: %v", err)
	}
	return found[0].Message, found[0].Remediation
}

func TestPresenceIsRpmWhatprovidesWithoutVersions(t *testing.T) {
	p, double := packagesWith(t, providedEverywhere)
	missing, err := p.Missing(context.Background(), []string{"podman", "python(abi) = 3.13"})
	if err != nil || len(missing) != 0 {
		t.Fatal(missing, err)
	}
	want := map[string]string{"podman": rpmQuery, "python(abi)": rpmQuery + rpmProvideList}
	for _, c := range double.calls {
		name := c.args[len(c.args)-1]
		format, ok := want[name]
		if c.path != "/usr/bin/rpm" || !ok || !slices.Equal(c.args, []string{"-q", "--whatprovides", "--queryformat", format, "--", name}) {
			t.Errorf("unexpected command %s %q", c.path, c.args)
		}
		if strings.Join(c.env, " ") != baseEnv {
			t.Errorf("rpm environment %q", c.env)
		}
		delete(want, name)
	}
	if len(want) != 0 {
		t.Fatalf("requirements never queried: %v", want)
	}
}

func TestAVersionedCapabilityNeedsAProviderListingItExactly(t *testing.T) {
	answers := map[string]reply{
		"satisfied": {stdout: "python3\t0:3.14.8-1.fc44.x86_64\n\tpython(abi) = 3.14\n\tpython3 = 3.14.8-1.fc44\n" +
			"python3\t0:3.14.8-1.fc44.x86_64\n\tpython(abi) = 3.14\n\tpython3 = 3.14.8-1.fc44\n"},
		"another minor": {stdout: "python3.13\t0:3.13.9-1.fc44.x86_64\n\tpython(abi) = 3.13\n"},
		"no provider":   {exit: 1, stdout: "no package provides python(abi)\n"},
	}
	wants := map[string][]string{"satisfied": {}, "another minor": {"python(abi) = 3.14"}, "no provider": {"python(abi) = 3.14"}}
	for name, answer := range answers {
		t.Run(name, func(t *testing.T) {
			p, double := packagesWith(t, func(command) reply { return answer })
			missing, err := p.Missing(context.Background(), []string{"python(abi) = 3.14"})
			if err != nil || !slices.Equal(missing, wants[name]) {
				t.Fatal(missing, err)
			}
			if args := double.calls[0].args; args[len(args)-1] != "python(abi)" {
				t.Fatalf("rpm was asked for %q", args)
			}
		})
	}
	t.Run("a provide line before any package", func(t *testing.T) {
		p, _ := packagesWith(t, func(command) reply { return reply{stdout: "\tpython(abi) = 3.14\n"} })
		if missing, err := p.Missing(context.Background(), []string{"python(abi) = 3.14"}); err == nil {
			t.Fatalf("answered %v", missing)
		}
	})
	t.Run("the install recheck finds the interpreter and records it once", func(t *testing.T) {
		p, _ := packagesWith(t, func(c command) reply {
			if c.path == "/usr/bin/dnf" && c.args[0] == "repoquery" {
				return reply{stdout: "python3.13 x86_64 updates\n"}
			}
			if c.path == "/usr/bin/dnf" {
				return reply{}
			}
			return providedEverywhere(c)
		})
		evidence, err := p.Install(context.Background(), []string{"python(abi) = 3.13"}, nil)
		if err != nil || len(evidence) != 1 || evidence[0] != (prerequisites.HostPackageEvidence{Requirement: "python(abi) = 3.13", Package: "python3.13-0:3.13.9-1.fc44.x86_64", Repository: "updates"}) {
			t.Fatal(evidence, err)
		}
	})
}

func TestAMissingRequirementIsExitOne(t *testing.T) {
	absent := func(c command) reply {
		requirement := c.args[len(c.args)-1]
		if requirement == "xorriso" {
			return reply{exit: 1, stdout: "no package provides xorriso\n"}
		}
		return providedEverywhere(c)
	}
	p, _ := packagesWith(t, absent)
	missing, err := p.Missing(context.Background(), []string{"lorax", "xorriso", "podman"})
	if err != nil || !slices.Equal(missing, []string{"xorriso"}) {
		t.Fatal(missing, err)
	}
	for name, answer := range map[string]reply{
		"database error": {exit: 1, stdout: "", stderr: "error: cannot open Packages database in /var/lib/rpm\n"},
		"other exit":     {exit: 2, stdout: "no package provides xorriso\n"},
		"stray stderr":   {exit: 1, stdout: "no package provides xorriso\n", stderr: "warning\n"},
		"no output":      {},
		"bad line":       {stdout: "xorriso\t1.5\n"},
		"run failure":    {err: errors.New("signal: killed")},
	} {
		t.Run(name, func(t *testing.T) {
			p, _ := packagesWith(t, func(command) reply { return answer })
			if missing, err := p.Missing(context.Background(), []string{"xorriso"}); err == nil {
				t.Fatalf("answered %v for an rpm that did not answer", missing)
			}
		})
	}
}

func TestThePreviewIsBoundedDisplayText(t *testing.T) {
	for _, test := range []struct{ name, stdout, stderr string }{
		{"dnf5", "dnf5-install-preview.stdout", "dnf5-install-preview.stderr"},
		{"dnf4", "dnf4-install-preview.stdout", "dnf4-install-preview.stderr"},
	} {
		t.Run(test.name, func(t *testing.T) {
			out, errText := fixture(t, test.stdout), fixture(t, test.stderr)
			p, double := packagesWith(t, func(command) reply { return reply{exit: 1, stdout: out, stderr: errText} })
			preview, err := p.Preview(context.Background(), []string{"lorax", "xorriso"})
			if err != nil || preview.Text != out || preview.Truncated || len(preview.Notes) != 0 {
				t.Fatalf("preview %+v: %v", preview, err)
			}
			call := double.calls[0]
			want := []string{"install", "--assumeno", "--setopt=install_weak_deps=False", "--", "lorax", "xorriso"}
			if call.path != "/usr/bin/dnf" || !slices.Equal(call.args, want) {
				t.Fatalf("command %s %q", call.path, call.args)
			}
		})
	}
	t.Run("long and hostile", func(t *testing.T) {
		long := "\x1b[2Jpackage\a\n" + strings.Repeat("x", 200<<10)
		p, _ := packagesWith(t, func(command) reply { return reply{exit: 1, stdout: long, stderr: "Operation aborted by the user.\n"} })
		preview, err := p.Preview(context.Background(), []string{"lorax"})
		if err != nil || len(preview.Text) != 64<<10 || !preview.Truncated || strings.ContainsAny(preview.Text, "\x1b\a") || !strings.HasPrefix(preview.Text, "?[2Jpackage?\n") {
			t.Fatalf("preview of %d bytes, truncated %v: %v", len(preview.Text), preview.Truncated, err)
		}
	})
	t.Run("nothing to do", func(t *testing.T) {
		p, _ := packagesWith(t, func(command) reply { return reply{stdout: "Nothing to do.\n"} })
		if preview, err := p.Preview(context.Background(), []string{"lorax"}); err != nil || preview.Text != "Nothing to do.\n" {
			t.Fatal(preview, err)
		}
	})
	t.Run("an unexplained failure", func(t *testing.T) {
		for name, answer := range map[string]reply{
			"exit one":   {exit: 1, stderr: "Failed to download metadata for repo 'updates'\n"},
			"exit two":   {exit: 2, stderr: "Operation aborted by the user.\n"},
			"not run":    {err: errors.New("fork failed")},
			"empty exit": {exit: 1},
		} {
			p, _ := packagesWith(t, func(command) reply { return answer })
			if preview, err := p.Preview(context.Background(), []string{"lorax"}); err == nil {
				t.Errorf("%s: previewed %+v", name, preview)
			}
		}
	})
}

func TestAPreviewNoMatchNamesTheRequirement(t *testing.T) {
	for _, test := range []struct{ name, stderr, line string }{
		{"dnf5", "dnf5-no-match.stderr", "No match for argument: nosuchpkgxyz"},
		{"dnf4", "dnf4-no-match.stderr", "Error: Unable to find a match: nosuchpkgxyz"},
	} {
		t.Run(test.name, func(t *testing.T) {
			text := fixture(t, test.stderr)
			p, _ := packagesWith(t, func(command) reply { return reply{exit: 1, stderr: text} })
			_, err := p.Preview(context.Background(), []string{"xorriso", "nosuchpkgxyz"})
			got, remedy := message(t, err)
			if !strings.Contains(got, "provides nosuchpkgxyz:") || strings.Contains(got, "xorriso") || !strings.HasSuffix(got, test.line) || !strings.Contains(remedy, "dnf configuration") {
				t.Fatalf("%q / %q", got, remedy)
			}
		})
	}
	t.Run("a longer name does not name its prefix", func(t *testing.T) {
		p, _ := packagesWith(t, func(command) reply {
			return reply{exit: 1, stderr: "No match for argument: lorax-templates-generic\n"}
		})
		_, err := p.Preview(context.Background(), []string{"lorax", "lorax-templates-generic"})
		if got, _ := message(t, err); !strings.Contains(got, "provides lorax-templates-generic:") {
			t.Fatalf("%q", got)
		}
	})
	for _, test := range []struct{ name, stderr, line string }{
		{"dnf5", "dnf5-problem.stderr", "- nothing provides libburn.so.4()(64bit) needed by libisoburn-1.5.8-2.fc44.x86_64 from updates"},
		{"dnf4", "dnf4-problem.stderr", "- nothing provides libburn.so.4()(64bit) needed by libisoburn-1.5.4-5.el9.x86_64"},
	} {
		t.Run(test.name+" dependency", func(t *testing.T) {
			text := fixture(t, test.stderr)
			p, _ := packagesWith(t, func(command) reply { return reply{exit: 1, stderr: text} })
			_, err := p.Preview(context.Background(), []string{"xorriso"})
			got, _ := message(t, err)
			if !strings.Contains(got, "install of xorriso:") || !strings.HasSuffix(got, test.line) {
				t.Fatalf("%q", got)
			}
		})
	}
}

func TestThePreviewNotesCorePackagesWithoutRefusing(t *testing.T) {
	out := fixture(t, "dnf5-core-preview.stdout")
	p, _ := packagesWith(t, func(command) reply { return reply{exit: 1, stdout: out, stderr: "Operation aborted by the user.\n"} })
	preview, err := p.Preview(context.Background(), []string{"python(abi) = 3.13"})
	want := "this install also updates core packages (glibc, glibc-common, libgcc, python3.13, python3.13-libs); dnf applies it as for any package install"
	if err != nil || preview.Text != out || !slices.Equal(preview.Notes, []string{want}) {
		t.Fatalf("%+v: %v", preview, err)
	}
	plain := fixture(t, "dnf5-install-preview.stdout")
	p, _ = packagesWith(t, func(command) reply { return reply{exit: 1, stdout: plain, stderr: "Operation aborted by the user.\n"} })
	if preview, err := p.Preview(context.Background(), []string{"python(abi) = 3.13"}); err != nil || len(preview.Notes) != 0 {
		t.Fatalf("a preview without core packages carried notes: %+v: %v", preview, err)
	}
}

func TestInstallNeverPassesAllowErasingOrNoGpgcheck(t *testing.T) {
	p, double := packagesWith(t, func(c command) reply {
		if c.args[0] == "repoquery" {
			return reply{stdout: "podman x86_64 updates\n"}
		}
		return providedEverywhere(c)
	})
	if _, err := p.Preview(context.Background(), []string{"podman", "lorax"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Install(context.Background(), []string{"podman", "lorax"}, nil); err != nil {
		t.Fatal(err)
	}
	install := double.called("install")
	want := []string{"install", "--assumeyes", "--setopt=install_weak_deps=False", "--", "podman", "lorax"}
	if len(install) != 2 || !slices.Equal(install[1].args, want) {
		t.Fatalf("%q", install)
	}
	for _, c := range double.calls {
		for _, argument := range c.args {
			for _, forbidden := range []string{"allowerasing", "nogpgcheck", "repofrompath", "enablerepo", "releasever", "disablerepo", "exclude", "skip-broken", "nobest"} {
				if strings.Contains(argument, forbidden) {
					t.Errorf("%s was passed in %q", forbidden, c.args)
				}
			}
		}
	}
	before := len(double.calls)
	for _, hostile := range []string{"--allowerasing", "-y", "a b", "$(id)", ""} {
		if _, err := p.Install(context.Background(), []string{hostile}, nil); err == nil {
			t.Errorf("%q reached dnf", hostile)
		}
	}
	if _, err := p.Install(context.Background(), nil, nil); err == nil {
		t.Error("an empty install reached dnf")
	}
	if _, err := p.Install(context.Background(), []string{"podman", "podman"}, nil); err == nil {
		t.Error("a repeated requirement reached dnf")
	}
	if len(double.calls) != before {
		t.Fatalf("refused requests ran %d commands", len(double.calls)-before)
	}
}

func TestInstallRechecksPresenceAndRecordsProviderAndRepository(t *testing.T) {
	var installed bool
	var mu sync.Mutex
	respond := func(c command) reply {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case c.path == "/usr/bin/dnf" && c.args[0] == "install":
			installed = true
			return reply{stdout: "Installing: lorax\n", stderr: "Transaction complete\n"}
		case c.path == "/usr/bin/dnf":
			return reply{stdout: "lorax x86_64 updates\nxorriso x86_64 fedora\nlorax noarch @System\n"}
		case !installed:
			return reply{exit: 1, stdout: "no package provides " + c.args[len(c.args)-1] + "\n"}
		case c.args[len(c.args)-1] == "lorax":
			return reply{stdout: "lorax\t0:44.7-1.fc44.x86_64\n"}
		}
		return reply{stdout: "xorriso\t1:1.5.8-2.fc44.x86_64\nlibisoburn\t0:1.5.8-2.fc44.x86_64\n"}
	}
	p, double := packagesWith(t, respond)
	missing, err := p.Missing(context.Background(), []string{"lorax", "xorriso"})
	if err != nil || len(missing) != 2 {
		t.Fatal(missing, err)
	}
	var log bytes.Buffer
	evidence, err := p.Install(context.Background(), missing, &log)
	if err != nil {
		t.Fatal(err)
	}
	if log.String() != "Installing: lorax\nTransaction complete\n" {
		t.Fatalf("setup log %q", log.String())
	}
	got := []string{}
	for _, e := range evidence {
		got = append(got, e.Requirement+"|"+e.Package+"|"+e.Repository)
	}
	want := []string{
		"lorax|lorax-0:44.7-1.fc44.x86_64|updates",
		"xorriso|xorriso-1:1.5.8-2.fc44.x86_64|fedora",
		"xorriso|libisoburn-0:1.5.8-2.fc44.x86_64|",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("evidence %q", got)
	}
	if repoquery := double.called("repoquery"); len(repoquery) != 1 || !slices.Equal(repoquery[0].args, []string{"repoquery", "--installed", "--qf", `%{name} %{arch} %{from_repo}\n`, "--", "lorax", "xorriso", "libisoburn"}) {
		t.Fatalf("%q", repoquery)
	}

	t.Run("a requirement dnf did not provide", func(t *testing.T) {
		p, _ := packagesWith(t, func(c command) reply {
			if c.path == "/usr/bin/dnf" {
				return reply{}
			}
			return reply{exit: 1, stdout: "no package provides " + c.args[len(c.args)-1] + "\n"}
		})
		_, err := p.Install(context.Background(), []string{"lorax"}, nil)
		if got, _ := message(t, err); !strings.Contains(got, "nothing provides lorax") {
			t.Fatalf("%q", got)
		}
	})
	t.Run("a failed install names dnf's last line and rechecks nothing", func(t *testing.T) {
		p, double := packagesWith(t, func(command) reply {
			return reply{exit: 1, stderr: "Downloading\nError: Transaction failed: Rpm transaction failed.\n\n"}
		})
		_, err := p.Install(context.Background(), []string{"lorax"}, nil)
		got, _ := message(t, err)
		if !strings.HasSuffix(got, "Error: Transaction failed: Rpm transaction failed.") || len(double.calls) != 1 {
			t.Fatalf("%q after %d commands", got, len(double.calls))
		}
	})
	t.Run("an unanswering repository query leaves the repository empty", func(t *testing.T) {
		p, _ := packagesWith(t, func(c command) reply {
			if c.path == "/usr/bin/dnf" && c.args[0] == "repoquery" {
				return reply{exit: 1}
			}
			return providedEverywhere(c)
		})
		evidence, err := p.Install(context.Background(), []string{"lorax"}, nil)
		if err != nil || len(evidence) != 1 || evidence[0].Repository != "" || evidence[0].Package != "lorax-0:1.2.3-4.fc44.x86_64" {
			t.Fatal(evidence, err)
		}
	})
	t.Run("a canceled install is the context's error", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		p, _ := packagesWith(t, func(command) reply { cancel(); return reply{err: errors.New("signal: terminated")} })
		if _, err := p.Install(ctx, []string{"lorax"}, nil); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	})
}

func TestDnfGetsACleanEnvironmentAndOnlyTheAdmittedRoute(t *testing.T) {
	for name, value := range map[string]string{
		"HTTPS_PROXY": "http://ambient.example:1", "HTTP_PROXY": "http://ambient.example:2", "ALL_PROXY": "socks5://ambient.example:3",
		"https_proxy": "http://ambient.example:1", "NO_PROXY": "ambient", "DNF_VAR_x": "1", "LD_PRELOAD": "/tmp/x.so", "PYTHONPATH": "/tmp", "RPM_CONFIGDIR": "/tmp",
	} {
		t.Setenv(name, value)
	}
	routes := map[string]map[string]string{
		"direct": {},
		"proxy":  {"HTTPS_PROXY": "http://proxy.example:3128"},
		"bypass": {"HTTPS_PROXY": "http://proxy.example:3128", "NO_PROXY": ".internal,10.0.0.0/8"},
	}
	wants := map[string]string{
		"direct": baseEnv, "proxy": baseEnv + " https_proxy=http://proxy.example:3128",
		"bypass": baseEnv + " https_proxy=http://proxy.example:3128 no_proxy=.internal,10.0.0.0/8",
	}
	for name, environment := range routes {
		t.Run(name, func(t *testing.T) {
			route, err := controller.RouteFromEnvironment(func(key string) (string, bool) { value, ok := environment[key]; return value, ok })
			if err != nil {
				t.Fatal(err)
			}
			double := &fake{respond: func(c command) reply {
				if c.path == "/usr/bin/dnf" {
					return reply{exit: 1, stderr: "Operation aborted by the user.\n"}
				}
				return providedEverywhere(c)
			}}
			p, err := New(route)
			if err != nil {
				t.Fatal(err)
			}
			p.run = double.run
			p.qualify = func(path string) (string, error) { return path, nil }
			if _, err := p.Missing(context.Background(), []string{"podman"}); err != nil {
				t.Fatal(err)
			}
			if _, err := p.Preview(context.Background(), []string{"podman"}); err != nil {
				t.Fatal(err)
			}
			for _, c := range double.calls {
				want := wants[name]
				if c.path == "/usr/bin/rpm" {
					want = baseEnv
				}
				if got := strings.Join(c.env, " "); got != want {
					t.Errorf("%s environment %q, want %q", c.path, got, want)
				}
			}
		})
	}
	t.Run("a route that carries credentials is refused", func(t *testing.T) {
		route, err := controller.RouteFromEnvironment(func(key string) (string, bool) {
			return "http://user:secret@proxy.example:3128", key == "HTTPS_PROXY"
		})
		if err == nil {
			_, err = New(route)
		}
		if err == nil {
			t.Fatal("credentials reached the environment")
		}
	})
	t.Run("the real runner passes exactly the given environment", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		exit, err := execute(context.Background(), command{path: "/usr/bin/env", env: []string{"LC_ALL=C", "https_proxy=http://p:1"}}, &stdout, &stderr)
		if err != nil || exit != 0 || stdout.String() != "LC_ALL=C\nhttps_proxy=http://p:1\n" {
			t.Fatalf("%d %q %q: %v", exit, stdout.String(), stderr.String(), err)
		}
		exit, err = execute(context.Background(), command{path: "/usr/bin/cat", env: []string{"LC_ALL=C"}}, &stdout, &stderr)
		if err != nil || exit != 0 {
			t.Fatalf("cat did not see end of input: %d %v", exit, err)
		}
		if exit, err = execute(context.Background(), command{path: "/usr/bin/false"}, &stdout, &stderr); err != nil || exit != 1 {
			t.Fatalf("%d %v", exit, err)
		}
		if _, err = execute(context.Background(), command{path: "/nonexistent/dnf"}, &stdout, &stderr); err == nil {
			t.Fatal("a missing executable ran")
		}
	})
}

// A context's controller stage forwards the route its controller Machine
// selected, exactly as setup forwards the admitted ambient one, and refuses a
// route outside the grammar before dnf could see it.
func TestTheStageRouteReachesDnfAsSetupsDoes(t *testing.T) {
	for name, check := range map[string]struct {
		egress prerequisites.SetupEgress
		want   string
	}{
		"direct": {prerequisites.SetupEgress{NoProxy: []string{}}, baseEnv},
		"bypass": {prerequisites.SetupEgress{HTTPSProxy: "http://proxy.example:3128", NoProxy: []string{".internal", "10.0.0.0/8"}}, baseEnv + " https_proxy=http://proxy.example:3128 no_proxy=.internal,10.0.0.0/8"},
	} {
		t.Run(name, func(t *testing.T) {
			p, err := ForEgress(check.egress)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(p.dnfEnvironment(), " "); got != check.want {
				t.Fatalf("dnf environment %q, want %q", got, check.want)
			}
		})
	}
	for name, egress := range map[string]prerequisites.SetupEgress{
		"credentials":     {HTTPSProxy: "http://user:secret@proxy.example:3128"},
		"HTTP proxy only": {HTTPProxy: "http://proxy.example:3128"},
	} {
		if _, err := ForEgress(egress); err == nil {
			t.Errorf("%s: the route was admitted", name)
		}
	}
}

func TestAnUnsafeDnfExecutableIsRefused(t *testing.T) {
	directory := t.TempDir()
	open := filepath.Join(directory, "open")
	if err := os.Mkdir(open, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(open, 0o777); err != nil {
		t.Fatal(err)
	}
	write := func(parent, name string, mode os.FileMode) string {
		path := filepath.Join(parent, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		return path
	}
	writable := write(directory, "writable", 0o777)
	grouped := write(directory, "grouped", 0o775)
	inOpen := write(open, "in-open-directory", 0o755)
	link := filepath.Join(directory, "link")
	if err := os.Symlink(writable, link); err != nil {
		t.Fatal(err)
	}
	unsafe := []string{writable, grouped, inOpen, link, directory, filepath.Join(directory, "missing")}
	for _, path := range unsafe {
		t.Run(filepath.Base(path), func(t *testing.T) {
			double := &fake{respond: func(command) reply { t.Error("an unqualified executable ran"); return reply{} }}
			p, err := New(controller.Route{})
			if err != nil {
				t.Fatal(err)
			}
			p.run, p.rpm, p.dnf = double.run, path, path
			if _, err := p.Missing(context.Background(), []string{"podman"}); err == nil {
				t.Error("rpm was trusted")
			} else if got, _ := message(t, err); !strings.Contains(got, "rpm is not a root-owned system executable") {
				t.Error(got)
			}
			if _, err := p.Preview(context.Background(), []string{"podman"}); err == nil {
				t.Error("dnf was trusted for a preview")
			} else if got, remedy := message(t, err); !strings.Contains(got, "dnf is not a root-owned system executable") || !strings.Contains(remedy, "/usr/bin") {
				t.Error(got, remedy)
			}
			if _, err := p.Install(context.Background(), []string{"podman"}, nil); err == nil {
				t.Error("dnf was trusted for an install")
			}
			if err := p.Qualify(context.Background()); err == nil {
				t.Error("setup admitted an unqualified rpm")
			}
		})
	}
}

func TestPresentNamesOnlyThePresentProvidersAndTheirRepository(t *testing.T) {
	p, double := packagesWith(t, func(c command) reply {
		switch {
		case c.path == "/usr/bin/dnf":
			return reply{stdout: "podman x86_64 updates\n"}
		case c.args[len(c.args)-1] == "podman":
			return reply{stdout: "podman\t5:5.8.1-1.fc44.x86_64\n"}
		}
		return reply{exit: 1, stdout: "no package provides " + c.args[len(c.args)-1] + "\n"}
	})
	if err := p.Qualify(context.Background()); err != nil {
		t.Fatal(err)
	}
	evidence, err := p.Present(context.Background(), []string{"openssh-clients", "podman"})
	if err != nil || len(evidence) != 1 || evidence[0].Requirement != "podman" || evidence[0].Package != "podman-5:5.8.1-1.fc44.x86_64" || evidence[0].Repository != "updates" {
		t.Fatal(evidence, err)
	}
	if len(double.called("install")) != 0 {
		t.Fatal("presence ran an install")
	}
}

// A ready host's presence read is rpm alone: no dnf runs, so no repository
// metadata can load.
func TestInstalledReadsProvidersWithoutDnf(t *testing.T) {
	p, double := packagesWith(t, func(c command) reply {
		if c.path == "/usr/bin/rpm" && c.args[len(c.args)-1] == "podman" {
			return reply{stdout: "podman\t5:5.8.1-1.fc44.x86_64\n"}
		}
		return reply{exit: 1, stdout: "no package provides " + c.args[len(c.args)-1] + "\n"}
	})
	evidence, err := p.Installed(context.Background(), []string{"openssh-clients", "podman"})
	if err != nil || len(evidence) != 1 || evidence[0].Requirement != "podman" || evidence[0].Package != "podman-5:5.8.1-1.fc44.x86_64" || evidence[0].Repository != "" {
		t.Fatal(evidence, err)
	}
	if len(double.called("rpm")) != 2 || len(double.calls) != 2 {
		t.Fatalf("calls = %+v", double.calls)
	}
}
