//go:build linux && amd64

package hostdnf

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
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

// The snapshot is rpm's listing of every installed package with its install
// time; the imported signing keys are no package, and a line outside the
// format makes the whole listing unreadable.
func TestASnapshotListsEveryInstalledPackageWithItsInstallTime(t *testing.T) {
	listing := "podman\t5:5.6.1-1.fc44\tx86_64\t1762790000\ngpg-pubkey\t0:105ef944-65ca83d1\t(none)\t1700000000\nlibnet\t0:1.3-5.fc44\tx86_64\t1700000001\n"
	p, double := packagesWith(t, func(command) reply { return reply{stdout: listing} })
	installed, err := p.Snapshot(context.Background())
	want := []prerequisites.InstalledPackage{{NEVRA: "podman-5:5.6.1-1.fc44.x86_64", InstallTime: 1762790000}, {NEVRA: "libnet-0:1.3-5.fc44.x86_64", InstallTime: 1700000001}}
	if err != nil || !slices.Equal(installed, want) {
		t.Fatalf("snapshot = %+v (%v), want %+v", installed, err, want)
	}
	if c := double.calls[0]; c.path != "/usr/bin/rpm" || !slices.Equal(c.args, []string{"-qa", "--queryformat", snapshotFormat}) || strings.Join(c.env, " ") != baseEnv {
		t.Fatalf("snapshot ran %s %q %q", c.path, c.args, c.env)
	}
	if prerequisites.PackageArgument(installed[0]) != "podman-5.6.1-1.fc44.x86_64" {
		t.Fatalf("argument = %q", prerequisites.PackageArgument(installed[0]))
	}
	for _, broken := range []string{"podman 5.6.1\n", "podman\t5:5.6.1-1.fc44\tx86_64\tsoon\n"} {
		unreadable, _ := packagesWith(t, func(command) reply { return reply{stdout: broken} })
		if _, err := unreadable.Snapshot(context.Background()); err == nil {
			t.Errorf("listing %q was read", broken)
		}
	}
}

func TestARemovalNamesOnlyInstalledPackageArguments(t *testing.T) {
	p, _ := packagesWith(t, func(command) reply { return reply{} })
	for _, refused := range [][]string{{}, {"podman"}, {"podman-5.6.1-1.fc44.x86_64", "podman-5.6.1-1.fc44.x86_64"}, {"-podman-1-1.x86_64"}, {"python(abi)-3-1.x86_64"}} {
		if err := p.EraseTest(context.Background(), refused); err == nil {
			t.Errorf("arguments %q were admitted", refused)
		}
	}
}

// The erase test is rpm --erase --test over exactly the names, and a failed
// dependency names each installed package that requires one of them.
func TestAnUninstallEraseTestNamesTheDependents(t *testing.T) {
	answer := reply{}
	p, double := packagesWith(t, func(command) reply { return answer })
	if err := p.EraseTest(context.Background(), []string{"podman-5.6.1-1.fc44.x86_64", "python3.13-3.13.9-1.fc44.x86_64"}); err != nil {
		t.Fatal(err)
	}
	if c := double.calls[0]; c.path != "/usr/bin/rpm" || !slices.Equal(c.args, []string{"--erase", "--test", "--", "podman-5.6.1-1.fc44.x86_64", "python3.13-3.13.9-1.fc44.x86_64"}) || strings.Join(c.env, " ") != baseEnv {
		t.Fatalf("erase test = %s %q %q", c.path, c.args, c.env)
	}
	answer = reply{exit: 1, stderr: "error: Failed dependencies:\n\tpodman is needed by (installed) cockpit-podman-99-1.fc44.noarch\n" +
		"\tpodman >= 5 is needed by (installed) toolbox-0.1-1.fc44.x86_64\n\tpodman is needed by (installed) toolbox-0.1-1.fc44.x86_64\n"}
	message, remedy := uninstallMessage(t, p.EraseTest(context.Background(), []string{"podman-5.6.1-1.fc44.x86_64"}))
	if !strings.HasSuffix(message, ": cockpit-podman-99-1.fc44.noarch, toolbox-0.1-1.fc44.x86_64") ||
		remedy != "remove the dependent packages first, or pass --keep-packages" {
		t.Fatalf("refusal = %q, %q", message, remedy)
	}
	answer = reply{exit: 1, stderr: "error: can't create transaction lock on /var/lib/rpm/.rpm.lock\n"}
	if message, _ := uninstallMessage(t, p.EraseTest(context.Background(), []string{"podman-5.6.1-1.fc44.x86_64"})); !strings.Contains(message, "transaction lock") {
		t.Fatalf("other failure = %q", message)
	}
}

// dnf previews the removal answering no, bounded like the install preview,
// and removes answering yes, streaming what it prints; both disable dependency
// cleanup, so dnf removes exactly the packages named, and neither gets an
// option that weakens dnf's own checks.
func TestAnUninstallRunsDnfRemoveAsInstallRunsDnf(t *testing.T) {
	large := strings.Repeat("x", previewLimit+10)
	p, double := packagesWith(t, func(c command) reply {
		if c.args[1] == "--assumeno" {
			return reply{exit: 1, stdout: large, stderr: "Operation aborted by the user.\n"}
		}
		return reply{stdout: "Removed: podman\nComplete!\n"}
	})
	preview, err := p.RemovalPreview(context.Background(), []string{"podman-5.6.1-1.fc44.x86_64"})
	if err != nil || !preview.Truncated || len(preview.Text) != previewLimit {
		t.Fatalf("preview = %d bytes, truncated %v (%v)", len(preview.Text), preview.Truncated, err)
	}
	var streamed bytes.Buffer
	if err := p.Remove(context.Background(), []string{"podman-5.6.1-1.fc44.x86_64", "lorax-44.1-1.fc44.x86_64"}, &streamed); err != nil {
		t.Fatal(err)
	}
	removals := double.called("remove")
	if len(removals) != 2 || !slices.Equal(removals[0].args, []string{"remove", "--assumeno", "--setopt=clean_requirements_on_remove=False", "--", "podman-5.6.1-1.fc44.x86_64"}) ||
		!slices.Equal(removals[1].args, []string{"remove", "--assumeyes", "--setopt=clean_requirements_on_remove=False", "--", "podman-5.6.1-1.fc44.x86_64", "lorax-44.1-1.fc44.x86_64"}) || streamed.String() != "Removed: podman\nComplete!\n" {
		t.Fatalf("removals = %+v, streamed %q", removals, streamed.String())
	}
	for _, c := range removals {
		if strings.Join(c.env, " ") != baseEnv {
			t.Errorf("dnf environment %q", c.env)
		}
	}
	failing, _ := packagesWith(t, func(command) reply { return reply{exit: 1, stderr: "Error: transaction failed\n"} })
	if message, _ := uninstallMessage(t, failing.Remove(context.Background(), []string{"podman-5.6.1-1.fc44.x86_64"}, nil)); !strings.Contains(message, "transaction failed") {
		t.Fatalf("failed removal = %q", message)
	}
	unsafe, _ := packagesWith(t, func(command) reply { return reply{} })
	unsafe.qualify = func(string) (string, error) { return "", errUnqualifiedForTest }
	if message, _ := uninstallMessage(t, unsafe.Remove(context.Background(), []string{"podman-5.6.1-1.fc44.x86_64"}, nil)); !strings.Contains(message, "dnf is not a root-owned system executable") {
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
	if _, err := p.RemovalPreview(context.Background(), []string{"podman-5.6.1-1.fc44.x86_64"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Remove(context.Background(), []string{"podman-5.6.1-1.fc44.x86_64"}, nil); err != nil {
		t.Fatal(err)
	}
	if err := p.EraseTest(context.Background(), []string{"podman-5.6.1-1.fc44.x86_64"}); err != nil {
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
