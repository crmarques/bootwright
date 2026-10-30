//go:build linux && amd64

package hostdnf

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

func uninstallMessage(t *testing.T, err error) (string, string) {
	t.Helper()
	found := diagnostics.Of(err)
	if len(found) != 1 || found[0].Code != "controller.uninstall" {
		t.Fatalf("not one controller.uninstall refusal: %#v", found)
	}
	return found[0].Message, found[0].Remediation
}

// Presence is rpm's query by exact package name: an absent package is exit 1
// with rpm's own sentence, and a match by anything but the name is unreadable.
func TestAnUninstallReadsPresenceByExactPackageName(t *testing.T) {
	p, double := packagesWith(t, func(c command) reply {
		switch name := c.args[len(c.args)-1]; name {
		case "podman":
			return reply{stdout: "podman\n"}
		case "lorax":
			return reply{exit: 1, stdout: "package lorax is not installed\n"}
		default:
			return reply{stdout: "something-else\n"}
		}
	})
	installed, err := p.InstalledNames(context.Background(), []string{"podman", "lorax"})
	if err != nil || !slices.Equal(installed, []string{"podman"}) {
		t.Fatalf("installed = %q (%v)", installed, err)
	}
	for _, c := range double.calls {
		name := c.args[len(c.args)-1]
		if c.path != "/usr/bin/rpm" || !slices.Equal(c.args, []string{"-q", "--queryformat", `%{NAME}\n`, "--", name}) || strings.Join(c.env, " ") != baseEnv {
			t.Errorf("unexpected command %s %q %q", c.path, c.args, c.env)
		}
	}
	if _, err := p.InstalledNames(context.Background(), []string{"xorriso"}); err == nil {
		t.Fatal("a match by another name was read as installed")
	}
	for _, refused := range [][]string{{"podman", "podman"}, {"-podman"}, {"python(abi)"}, {"podman = 5"}} {
		if _, err := p.InstalledNames(context.Background(), refused); err == nil {
			t.Errorf("names %q were admitted", refused)
		}
	}
}

// The erase test is rpm --erase --test over exactly the names, and a failed
// dependency names each installed package that requires one of them.
func TestAnUninstallEraseTestNamesTheDependents(t *testing.T) {
	answer := reply{}
	p, double := packagesWith(t, func(command) reply { return answer })
	if err := p.EraseTest(context.Background(), []string{"podman", "python3.13"}); err != nil {
		t.Fatal(err)
	}
	if c := double.calls[0]; c.path != "/usr/bin/rpm" || !slices.Equal(c.args, []string{"--erase", "--test", "--", "podman", "python3.13"}) || strings.Join(c.env, " ") != baseEnv {
		t.Fatalf("erase test = %s %q %q", c.path, c.args, c.env)
	}
	answer = reply{exit: 1, stderr: "error: Failed dependencies:\n\tpodman is needed by (installed) cockpit-podman-99-1.fc44.noarch\n" +
		"\tpodman >= 5 is needed by (installed) toolbox-0.1-1.fc44.x86_64\n\tpodman is needed by (installed) toolbox-0.1-1.fc44.x86_64\n"}
	message, remedy := uninstallMessage(t, p.EraseTest(context.Background(), []string{"podman"}))
	if !strings.HasSuffix(message, ": cockpit-podman-99-1.fc44.noarch, toolbox-0.1-1.fc44.x86_64") ||
		remedy != "remove the dependent packages first, or pass --keep-packages" {
		t.Fatalf("refusal = %q, %q", message, remedy)
	}
	answer = reply{exit: 1, stderr: "error: can't create transaction lock on /var/lib/rpm/.rpm.lock\n"}
	if message, _ := uninstallMessage(t, p.EraseTest(context.Background(), []string{"podman"})); !strings.Contains(message, "transaction lock") {
		t.Fatalf("other failure = %q", message)
	}
}

// dnf previews the removal answering no, bounded like the install preview,
// and removes answering yes, streaming what it prints; neither gets an
// option that weakens dnf's own checks.
func TestAnUninstallRunsDnfRemoveAsInstallRunsDnf(t *testing.T) {
	large := strings.Repeat("x", previewLimit+10)
	p, double := packagesWith(t, func(c command) reply {
		if c.args[1] == "--assumeno" {
			return reply{exit: 1, stdout: large, stderr: "Operation aborted by the user.\n"}
		}
		return reply{stdout: "Removed: podman\nComplete!\n"}
	})
	preview, err := p.RemovalPreview(context.Background(), []string{"podman"})
	if err != nil || !preview.Truncated || len(preview.Text) != previewLimit {
		t.Fatalf("preview = %d bytes, truncated %v (%v)", len(preview.Text), preview.Truncated, err)
	}
	var streamed bytes.Buffer
	if err := p.Remove(context.Background(), []string{"podman", "lorax"}, &streamed); err != nil {
		t.Fatal(err)
	}
	removals := double.called("remove")
	if len(removals) != 2 || !slices.Equal(removals[0].args, []string{"remove", "--assumeno", "--", "podman"}) ||
		!slices.Equal(removals[1].args, []string{"remove", "--assumeyes", "--", "podman", "lorax"}) || streamed.String() != "Removed: podman\nComplete!\n" {
		t.Fatalf("removals = %+v, streamed %q", removals, streamed.String())
	}
	for _, c := range removals {
		if strings.Join(c.env, " ") != baseEnv {
			t.Errorf("dnf environment %q", c.env)
		}
	}
	failing, _ := packagesWith(t, func(command) reply { return reply{exit: 1, stderr: "Error: transaction failed\n"} })
	if message, _ := uninstallMessage(t, failing.Remove(context.Background(), []string{"podman"}, nil)); !strings.Contains(message, "transaction failed") {
		t.Fatalf("failed removal = %q", message)
	}
	unsafe, _ := packagesWith(t, func(command) reply { return reply{} })
	unsafe.qualify = func(string) (string, error) { return "", errUnqualifiedForTest }
	if message, _ := uninstallMessage(t, unsafe.Remove(context.Background(), []string{"podman"}, nil)); !strings.Contains(message, "dnf is not a root-owned system executable") {
		t.Fatalf("unqualified dnf = %q", message)
	}
}

var errUnqualifiedForTest = diagnostics.NewFailure("controller.uninstall", "unqualified", "")

// The removal and its preview run dnf with the admitted context-free route,
// as install does, and rpm with none.
func TestAnUninstallHandsDnfTheAdmittedRoute(t *testing.T) {
	route, err := controller.RouteFromEnvironment(func(key string) (string, bool) {
		value, ok := map[string]string{"HTTPS_PROXY": "http://proxy.example:3128", "NO_PROXY": ".internal"}[key]
		return value, ok
	})
	if err != nil {
		t.Fatal(err)
	}
	double := &fake{respond: func(c command) reply {
		if c.path == "/usr/bin/dnf" && c.args[1] == "--assumeno" {
			return reply{exit: 1, stderr: "Operation aborted.\n"}
		}
		return reply{}
	}}
	p, err := New(route)
	if err != nil {
		t.Fatal(err)
	}
	p.run, p.qualify = double.run, func(path string) (string, error) { return path, nil }
	if _, err := p.RemovalPreview(context.Background(), []string{"podman"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Remove(context.Background(), []string{"podman"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := p.EraseTest(context.Background(), []string{"podman"}); err != nil {
		t.Fatal(err)
	}
	for _, c := range double.calls {
		want := baseEnv
		if c.path == "/usr/bin/dnf" {
			want += " https_proxy=http://proxy.example:3128 no_proxy=.internal"
		}
		if strings.Join(c.env, " ") != want {
			t.Errorf("%s %q environment %q, want %q", c.path, c.args, c.env, want)
		}
	}
}
