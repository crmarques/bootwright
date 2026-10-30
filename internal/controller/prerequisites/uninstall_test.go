package prerequisites

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

type uninstallFixture struct {
	events     []string
	views      []UninstallView
	inspected  int
	busy       error
	installed  []string
	remaining  []string
	collateral []string
	previewed  []string
	eraseErr   error
	erasures   int
	eraseFrom  int
	removeErr  error
	stateErr   error
	decline    error
	plan       *UninstallPlan
	asked      string
}

func (f *uninstallFixture) record(event string) { f.events = append(f.events, event) }

func (f *uninstallFixture) ReadController(context.Context, string, func(StorageView) error) error {
	return errors.New("uninstall reads no controller record")
}

func (f *uninstallFixture) Uninstall(_ context.Context, callback func(UninstallTransaction) error) error {
	if f.busy != nil {
		return f.busy
	}
	view := f.views[min(f.inspected, len(f.views)-1)]
	f.inspected++
	return callback(uninstallTx{f, view})
}

type uninstallTx struct {
	f    *uninstallFixture
	view UninstallView
}

func (t uninstallTx) View() UninstallView { return t.view }

func (t uninstallTx) RemoveState(context.Context) ([]string, error) {
	t.f.record("remove-state")
	if t.f.stateErr != nil {
		return nil, t.f.stateErr
	}
	return []string{t.view.Root, t.view.Staging}, nil
}

type removalPackages struct {
	HostPackages
	f *uninstallFixture
}

func (p removalPackages) InstalledNames(_ context.Context, names []string) ([]string, error) {
	p.f.record("rpm -q " + strings.Join(names, " "))
	return slices.DeleteFunc(slices.Clone(names), func(name string) bool { return !slices.Contains(p.f.installed, name) }), nil
}

func (p removalPackages) EraseTest(_ context.Context, names []string) error {
	p.f.record("rpm --erase --test " + strings.Join(names, " "))
	p.f.erasures++
	if p.f.erasures < p.f.eraseFrom {
		return nil
	}
	return p.f.eraseErr
}

func (p removalPackages) RemovalPreview(_ context.Context, names []string) (HostPackagePreview, error) {
	p.f.record("dnf remove --assumeno")
	listed := append(slices.Clone(names), p.f.previewed...)
	return HostPackagePreview{Text: "Removing:\n " + strings.Join(listed, "  x86_64  1-1.fc44  @System\n ") + "\n"}, nil
}

func (p removalPackages) Remove(_ context.Context, names []string, output RunOutput) error {
	p.f.record("dnf remove")
	if p.f.removeErr != nil {
		return p.f.removeErr
	}
	p.f.installed = slices.DeleteFunc(p.f.installed, func(name string) bool {
		return (slices.Contains(names, name) || slices.Contains(p.f.collateral, name)) && !slices.Contains(p.f.remaining, name)
	})
	_, err := output.Write([]byte("Complete!\n"))
	return err
}

type uninstallPresenter struct {
	PlanPresenter
	f *uninstallFixture
}

func (p uninstallPresenter) PresentUninstallPlan(_ context.Context, plan UninstallPlan) error {
	p.f.record("plan")
	p.f.plan = &plan
	return nil
}

func (p uninstallPresenter) RemovalOutput() RunOutput { return memoryRun{} }

func (f *uninstallFixture) Confirm(_ context.Context, action, _ string) error {
	f.record("confirm")
	f.asked = action
	return f.decline
}

func (f *uninstallFixture) service() Service {
	return New(f, nil, nil, Ports{Packages: removalPackages{f: f}}, Options{Confirmer: f, Presenter: uninstallPresenter{f: f}})
}

func receiptView(packages ...ReceiptPackage) UninstallView {
	return UninstallView{
		Root: "/var/lib/bootwright", Staging: "/var/lib/bootwright-staging", RootExists: true, StagingSeen: true,
		Setup: SetupView{Exists: true, State: SetupState{Format: SetupStateFormat, Receipt: &HostReceipt{Packages: packages}}},
	}
}

func receipt(requirement, provider, by string) ReceiptPackage {
	return ReceiptPackage{Requirement: requirement, Provider: provider, InstalledBy: by, RequiredBy: []string{"setup"}, Repository: "fedora"}
}

func installReceipt() UninstallView {
	return receiptView(
		receipt("openssh-clients", "openssh-clients-0:9.9p1-3.fc44.x86_64", PresentBeforeBootwright),
		receipt("podman", "podman-5:5.6.1-1.fc44.x86_64", "setup-000001"),
		receipt("python3.13", "python3.13-0:3.13.9-1.fc44.x86_64", "setup-000002"),
	)
}

func uninstallRefusal(t *testing.T, err error, code, message string) {
	t.Helper()
	reported := diagnostics.Of(err)
	if len(reported) != 1 || reported[0].Code != code || !strings.Contains(reported[0].Message, message) {
		t.Fatalf("refusal = %#v, want %s %q", reported, code, message)
	}
}

// An uninstall erases only the receipt's own packages, by name, that rpm still
// reports: it tests the erasure before the plan, previews it with dnf, asks,
// inspects, tests and previews again, removes the packages and checks them
// gone and the packages present before Bootwright still there, and only then
// removes the state. A package present before Bootwright is never
// named, and the plan says it stays.
func TestAnUninstallRemovesThePackagesBeforeTheState(t *testing.T) {
	f := &uninstallFixture{views: []UninstallView{installReceipt()}, installed: []string{"openssh-clients", "podman"}}
	result, err := f.service().Uninstall(context.Background(), UninstallRequest{})
	if err != nil {
		t.Fatalf("the uninstall failed: %#v", diagnostics.Of(err))
	}
	want := []string{
		"rpm -q podman python3.13", "rpm --erase --test podman", "dnf remove --assumeno", "plan", "confirm",
		"rpm -q podman python3.13", "rpm --erase --test podman", "dnf remove --assumeno", "rpm -q openssh-clients",
		"dnf remove", "rpm -q podman", "rpm -q openssh-clients", "remove-state",
	}
	if !slices.Equal(f.events, want) || f.asked != "controller uninstall" {
		t.Fatalf("events = %q (asked %q), want %q", f.events, f.asked, want)
	}
	if result.Outcome != "removed" || !slices.Equal(result.Packages, []string{"podman"}) ||
		!slices.Equal(result.Paths, []string{"/var/lib/bootwright", "/var/lib/bootwright-staging"}) ||
		!slices.Contains(result.Kept, "openssh-clients (present before bootwright)") {
		t.Fatalf("result = %+v", result)
	}
	ids := []string{}
	for _, action := range f.plan.Actions {
		ids = append(ids, action.ID)
	}
	if !slices.Equal(ids, []string{RemoveHostPackagesAction, RemoveStateAction}) || f.plan.Preview == nil ||
		!slices.Equal(f.plan.Kept, result.Kept) || slices.Contains(f.plan.Actions[0].Items, "openssh-clients") {
		t.Fatalf("plan = %+v", f.plan)
	}
}

// Each refusal the contract names happens before any package or state effect
// and before the plan is presented.
func TestAnUninstallRefusesBeforeAnyEffect(t *testing.T) {
	pending := installReceipt()
	pending.Setup.State.Pending = &PendingSetup{ID: "setup-000003"}
	foreign := installReceipt()
	foreign.Setup = SetupView{Exists: true, Foreign: "bootwright.setup-state-v0"}
	held := diagnostics.NewFailureWithRemediation("lifecycle.lease", "a staging extraction is held by another process", "", "retry")
	cases := map[string]struct {
		view          UninstallView
		busy, erase   error
		code, message string
	}{
		"a context":                {view: UninstallView{RootExists: true, Contexts: []string{"lab", "prod"}}, code: uninstallCode, message: "lab, prod"},
		"an image":                 {view: UninstallView{RootExists: true, Media: []string{"rhel.iso"}}, code: uninstallCode, message: "rhel.iso"},
		"a held staging stage":     {view: installReceipt(), busy: held, code: "lifecycle.lease", message: "staging extraction"},
		"an interrupted install":   {view: pending, code: uninstallCode, message: "(setup-000003) was interrupted"},
		"a foreign record":         {view: foreign, code: uninstallCode, message: "written by another Bootwright build"},
		"a dependent package":      {view: installReceipt(), erase: failure(uninstallCode, "other installed packages require podman: cockpit-podman", ""), code: uninstallCode, message: "cockpit-podman"},
		"an unreadable provider":   {view: receiptView(receipt("podman", "podman", "setup-000001")), code: uninstallCode, message: "cannot read"},
		"a store this build skips": {view: installReceipt(), busy: failure("context.state", "registry version 5", ""), code: "context.state", message: "registry version 5"},
	}
	for name, test := range cases {
		t.Run(name, func(t *testing.T) {
			f := &uninstallFixture{views: []UninstallView{test.view}, busy: test.busy, eraseErr: test.erase, installed: []string{"podman"}}
			_, err := f.service().Uninstall(context.Background(), UninstallRequest{})
			uninstallRefusal(t, err, test.code, test.message)
			if slices.ContainsFunc(f.events, func(event string) bool {
				return event == "plan" || event == "dnf remove" || event == "remove-state" || event == "dnf remove --assumeno"
			}) {
				t.Fatalf("a refused uninstall acted: %q", f.events)
			}
		})
	}
	f := &uninstallFixture{views: []UninstallView{{RootExists: true, Contexts: []string{"lab"}}}}
	_, err := f.service().Uninstall(context.Background(), UninstallRequest{})
	if remedy := diagnostics.Of(err)[0].Remediation; !strings.Contains(remedy, "bootwright context delete --name <name> --purge") {
		t.Fatalf("the context remedy = %q", remedy)
	}
}

// --keep-packages runs no rpm or dnf at all, so neither an interrupted nor a
// foreign install record refuses, and every package install added stays.
func TestAnUninstallWithKeepPackagesTouchesNoPackage(t *testing.T) {
	pending := installReceipt()
	pending.Setup.State.Pending = &PendingSetup{ID: "setup-000003"}
	foreign := installReceipt()
	foreign.Setup = SetupView{Exists: true, Foreign: unrecognizedFormat}
	for name, view := range map[string]UninstallView{"a receipt": installReceipt(), "a pending intent": pending, "a foreign record": foreign} {
		t.Run(name, func(t *testing.T) {
			f := &uninstallFixture{views: []UninstallView{view}, installed: []string{"podman"}}
			service := New(f, nil, nil, Ports{}, Options{Confirmer: f, Presenter: uninstallPresenter{f: f}})
			result, err := service.Uninstall(context.Background(), UninstallRequest{KeepPackages: true})
			if err != nil {
				t.Fatalf("the uninstall failed: %#v", diagnostics.Of(err))
			}
			if !slices.Equal(f.events, []string{"plan", "confirm", "remove-state"}) || len(result.Packages) != 0 || f.plan.Preview != nil {
				t.Fatalf("events = %q, result %+v", f.events, result)
			}
			if name == "a receipt" && !slices.Contains(result.Kept, "podman (install added it; --keep-packages keeps it)") {
				t.Fatalf("kept = %q", result.Kept)
			}
		})
	}
}

const unrecognizedFormat = "content this build does not recognize"

// A declined or unanswerable confirmation, and a host that moved between the
// plan and the effect, remove nothing.
func TestADeclinedOrMovedUninstallRemovesNothing(t *testing.T) {
	declined := &uninstallFixture{views: []UninstallView{installReceipt()}, installed: []string{"podman"},
		decline: failure(uninstallCode, "uninstall confirmation requires an interactive terminal", "")}
	if _, err := declined.service().Uninstall(context.Background(), UninstallRequest{}); err == nil ||
		slices.Contains(declined.events, "dnf remove") || slices.Contains(declined.events, "remove-state") {
		t.Fatalf("a declined uninstall acted: %q (%v)", declined.events, err)
	}
	moved := installReceipt()
	moved.Leftovers = []string{"/var/lib/.bootwright-uninstall-" + strings.Repeat("a", 32)}
	f := &uninstallFixture{views: []UninstallView{installReceipt(), moved}, installed: []string{"podman"}}
	_, err := f.service().Uninstall(context.Background(), UninstallRequest{})
	uninstallRefusal(t, err, "controller.conflict", "changed after the uninstall plan was confirmed")
	if slices.Contains(f.events, "dnf remove") || slices.Contains(f.events, "remove-state") {
		t.Fatalf("a moved host was acted on: %q", f.events)
	}
}

// A removal dnf reports done while rpm still lists a package removes no
// state, so the receipt that names what to remove survives for the next run.
func TestAnUnfinishedPackageRemovalRemovesNoState(t *testing.T) {
	f := &uninstallFixture{views: []UninstallView{installReceipt()}, installed: []string{"podman"}, remaining: []string{"podman"}}
	_, err := f.service().Uninstall(context.Background(), UninstallRequest{SkipConfirmation: true})
	uninstallRefusal(t, err, uninstallCode, "dnf did not remove podman")
	if slices.Contains(f.events, "remove-state") || slices.Contains(f.events, "confirm") {
		t.Fatalf("events = %q", f.events)
	}
}

// A host with no state root, no staging directory and no renamed tree has
// nothing to do; one with only an interrupted uninstall's trees finishes them.
func TestAnUninstallOfAnEmptyHostHasNothingToDo(t *testing.T) {
	f := &uninstallFixture{views: []UninstallView{{Root: "/var/lib/bootwright", Staging: "/var/lib/bootwright-staging"}}}
	result, err := f.service().Uninstall(context.Background(), UninstallRequest{})
	if err != nil || result.Outcome != "unchanged" || len(f.events) != 0 {
		t.Fatalf("result = %+v, events %q (%v)", result, f.events, err)
	}
	leftover := "/var/lib/.bootwright-uninstall-" + strings.Repeat("b", 32)
	f = &uninstallFixture{views: []UninstallView{{Root: "/var/lib/bootwright", Leftovers: []string{leftover}}}}
	if _, err := f.service().Uninstall(context.Background(), UninstallRequest{SkipConfirmation: true}); err != nil {
		t.Fatalf("the finishing uninstall failed: %#v", diagnostics.Of(err))
	}
	if len(f.plan.Actions) != 1 || f.plan.Actions[0].ID != FinishRemovalAction || !slices.Equal(f.plan.Actions[0].Items, []string{leftover}) {
		t.Fatalf("plan = %+v", f.plan)
	}
}

func TestAReceiptProviderNamesItsPackage(t *testing.T) {
	for provider, want := range map[string]string{
		"podman-5:5.6.1-1.fc44.x86_64":                      "podman",
		"libvirt-daemon-driver-qemu-0:11.0.0-1.fc44.x86_64": "libvirt-daemon-driver-qemu",
		"python3.13-0:3.13.9-1.fc44.x86_64":                 "python3.13",
		"podman":                                            "",
		"podman-5.6.1-1.fc44.x86_64":                        "",
	} {
		name, ok := providerName(provider)
		if name != want && ok || ok != (want != "") {
			t.Errorf("providerName(%q) = %q, %v; want %q", provider, name, ok, want)
		}
	}
}

// dnf's resolution naming a package present before Bootwright refuses before
// the plan, and one dnf removed anyway fails before the state is removed.
func TestAnUninstallNeverRemovesAPackagePresentBeforeBootwright(t *testing.T) {
	f := &uninstallFixture{views: []UninstallView{installReceipt()}, installed: []string{"openssh-clients", "podman"}, previewed: []string{"openssh-clients"}}
	_, err := f.service().Uninstall(context.Background(), UninstallRequest{SkipConfirmation: true})
	uninstallRefusal(t, err, uninstallCode, "dnf would also remove packages present before Bootwright: openssh-clients")
	if slices.Contains(f.events, "plan") || slices.Contains(f.events, "dnf remove") {
		t.Fatalf("events = %q", f.events)
	}
	f = &uninstallFixture{views: []UninstallView{installReceipt()}, installed: []string{"openssh-clients", "podman"}, collateral: []string{"openssh-clients"}}
	_, err = f.service().Uninstall(context.Background(), UninstallRequest{SkipConfirmation: true})
	uninstallRefusal(t, err, uninstallCode, "dnf also removed packages present before Bootwright: openssh-clients")
	if slices.Contains(f.events, "remove-state") {
		t.Fatalf("events = %q", f.events)
	}
	truncated := previewNames(HostPackagePreview{Text: "Removing:\n", Truncated: true}, []string{"openssh-clients"})
	named := previewNames(HostPackagePreview{Text: " openssh-clients.x86_64 9.9p1-3\n openssh-9.9p1-3.fc44\n podman-docker\n"}, []string{"openssh", "openssh-clients", "podman"})
	if !slices.Equal(truncated, []string{"openssh-clients"}) || !slices.Equal(named, []string{"openssh", "openssh-clients"}) {
		t.Fatalf("truncated %q, named %q", truncated, named)
	}
}

// A dependent installed between the plan and the effect fails the second
// erase test, and a dnf removal that fails removes no state.
func TestAnUninstallThatFailsBeforeTheStateRemovesNoState(t *testing.T) {
	dependent := &uninstallFixture{views: []UninstallView{installReceipt()}, installed: []string{"podman"}, eraseFrom: 2,
		eraseErr: failure(uninstallCode, "other installed packages require podman: toolbox", "")}
	_, err := dependent.service().Uninstall(context.Background(), UninstallRequest{SkipConfirmation: true})
	uninstallRefusal(t, err, uninstallCode, "toolbox")
	failed := &uninstallFixture{views: []UninstallView{installReceipt()}, installed: []string{"podman"},
		removeErr: failure(uninstallCode, "dnf did not complete the removal: transaction failed", "")}
	_, err = failed.service().Uninstall(context.Background(), UninstallRequest{SkipConfirmation: true})
	uninstallRefusal(t, err, uninstallCode, "transaction failed")
	for _, f := range []*uninstallFixture{dependent, failed} {
		if slices.Contains(f.events, "remove-state") || f == dependent && slices.Contains(f.events, "dnf remove") {
			t.Fatalf("events = %q", f.events)
		}
	}
}

// State that cannot be removed after the packages were names the packages
// already gone, so the output says what changed.
func TestAnUninstallWhoseStateFailsNamesTheRemovedPackages(t *testing.T) {
	f := &uninstallFixture{views: []UninstallView{installReceipt()}, installed: []string{"podman"},
		stateErr: failure("context.state", "the state root was replaced while uninstall inspected it", "")}
	_, err := f.service().Uninstall(context.Background(), UninstallRequest{SkipConfirmation: true})
	uninstallRefusal(t, err, uninstallCode, "removed the host packages podman but not the state: the state root was replaced")
}
