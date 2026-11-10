package prerequisites

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

type UninstallRequest struct {
	SkipConfirmation  bool
	KeepPackages      bool
	DiscardUnreadable bool
}

type UninstallResult struct {
	Outcome  string
	Packages []string
	Paths    []string
	Kept     []string
}

type UninstallPlan struct {
	Actions []UninstallAction
	Preview *HostPackagePreview
	Kept    []string
}

type UninstallAction struct {
	ID    string
	Text  string
	Items []string
}

const (
	RemoveHostPackagesAction = "remove-host-packages"
	RemoveStateAction        = "remove-state"
	DiscardAction            = "discard"
	FinishRemovalAction      = "finish-removal"
	uninstallInvocation      = "bootwright controller uninstall"
	discardInvocation        = uninstallInvocation + " --discard-unreadable"
	uninstallCode            = "controller.uninstall"
)

type UninstallView struct {
	Root        string
	Staging     string
	Logs        string
	Hosted      string
	Images      string
	RootExists  bool
	StagingSeen bool
	LogsSeen    bool
	HostedSeen  bool
	ImagesSeen  bool
	GuestDisks  []string
	Unreadable  error
	Leftovers   []string
	Contexts    []string
	Media       []string
	Setup       SetupView
}

var receiptEpoch = regexp.MustCompile(`^[0-9]+:`)

type uninstallInspection struct {
	view     UninstallView
	discard  bool
	packages []string
	before   []string
	kept     []InstalledPackage
}

func (i uninstallInspection) empty() bool {
	v := i.view
	return !v.RootExists && !v.StagingSeen && !v.LogsSeen && !v.HostedSeen && !v.ImagesSeen && len(v.Leftovers) == 0
}

func (i uninstallInspection) same(other uninstallInspection) bool {
	a, b := i.view, other.view
	return a.RootExists == b.RootExists && a.StagingSeen == b.StagingSeen && a.LogsSeen == b.LogsSeen && a.HostedSeen == b.HostedSeen &&
		a.ImagesSeen == b.ImagesSeen && (a.Unreadable == nil) == (b.Unreadable == nil) && slices.Equal(a.GuestDisks, b.GuestDisks) &&
		slices.Equal(a.Leftovers, b.Leftovers) && slices.Equal(i.packages, other.packages) && slices.Equal(i.before, other.before) && slices.Equal(i.kept, other.kept)
}

func (s Service) Uninstall(ctx context.Context, request UninstallRequest) (*UninstallResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store, stores := s.storage.(UninstallStore)
	presenter, presents := s.options.Presenter.(UninstallPresenter)
	removal, removes := s.ports.Packages.(HostPackageRemoval)
	if request.DiscardUnreadable && request.KeepPackages {
		return nil, failure(uninstallCode, "--discard-unreadable removes no package, so it takes no --keep-packages", "repeat with one of them")
	}
	keep := request.KeepPackages || request.DiscardUnreadable
	if !stores || !presents || s.ports.Owned == nil || !keep && !removes {
		return nil, failure(uninstallCode, "controller uninstall is not configured", "")
	}
	var planned uninstallInspection
	err := store.Uninstall(ctx, func(tx UninstallTransaction) error {
		var err error
		planned, err = s.inspectUninstall(ctx, tx.View(), request, removal)
		return err
	})
	if err != nil {
		return nil, err
	}
	if planned.empty() {
		return &UninstallResult{Outcome: "unchanged", Packages: []string{}, Paths: []string{}, Kept: []string{}}, nil
	}
	plan := planned.plan()
	if len(planned.packages) != 0 {
		preview, err := s.removalPreview(ctx, removal, planned)
		if err != nil {
			return nil, err
		}
		plan.Preview = &preview
	}
	if err := presenter.PresentUninstallPlan(ctx, plan); err != nil {
		return nil, err
	}
	if !request.SkipConfirmation {
		if s.options.Confirmer == nil {
			return nil, failure(uninstallCode, "controller uninstall requires confirmation", "review the plan and repeat with --yes")
		}
		if err := s.options.Confirmer.Confirm(ctx, "controller uninstall", "this host"); err != nil {
			return nil, err
		}
	}
	result := &UninstallResult{Outcome: "removed", Packages: []string{}, Kept: slices.Clone(plan.Kept)}
	err = store.Uninstall(ctx, func(tx UninstallTransaction) error {
		fresh, err := s.inspectUninstall(ctx, tx.View(), request, removal)
		if err != nil {
			return err
		}
		if !planned.same(fresh) {
			return failure("controller.conflict", "the host changed after the uninstall plan was confirmed", "run "+uninstallInvocation+" again")
		}
		if len(fresh.packages) != 0 {
			if err := s.removePackages(ctx, removal, fresh, presenter.RemovalOutput()); err != nil {
				return err
			}
			result.Packages = slices.Clone(fresh.packages)
		}
		result.Paths, err = tx.RemoveState(ctx)
		if err != nil && len(result.Packages) != 0 {
			return stateAfterPackages(result.Packages, err)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s Service) inspectUninstall(ctx context.Context, view UninstallView, request UninstallRequest, removal HostPackageRemoval) (uninstallInspection, error) {
	inspection := uninstallInspection{view: view, discard: request.DiscardUnreadable}
	if view.Unreadable != nil && !request.DiscardUnreadable {
		return inspection, view.Unreadable
	}
	if len(view.Contexts) != 0 {
		return inspection, failure(uninstallCode, "this host holds contexts: "+strings.Join(view.Contexts, ", "),
			"destroy each environment (bootwright destroy --context <name>) and delete each context (bootwright context delete --name <name> --purge), then run "+uninstallInvocation+" again")
	}
	if len(view.Media) != 0 {
		remedies := make([]string, 0, len(view.Media))
		for _, image := range view.Media {
			remedies = append(remedies, "bootwright media delete --name "+image)
		}
		return inspection, failure(uninstallCode, "the media store holds installer media the operator supplied: "+strings.Join(view.Media, ", "),
			"run "+strings.Join(remedies, "; run ")+"; then run "+uninstallInvocation+" again")
	}
	keep := request.KeepPackages || request.DiscardUnreadable
	added, before, err := receiptNames(view.Setup, keep)
	if err != nil {
		return inspection, err
	}
	if inspection.empty() {
		return inspection, nil
	}
	if err := s.ownedObjectsGone(ctx); err != nil {
		return inspection, err
	}
	if len(view.GuestDisks) != 0 && !request.DiscardUnreadable {
		return inspection, failure(uninstallCode, "guest disks no removal took back remain under "+view.Images+": "+strings.Join(view.GuestDisks, ", "),
			"they may hold data: copy what you need from them, then run "+discardInvocation+" to remove them with the rest")
	}
	inspection.before = before
	switch {
	case request.KeepPackages:
		inspection.kept = added
	case request.DiscardUnreadable:
	case len(added) != 0:
		snapshot, err := removal.Snapshot(ctx)
		if err != nil {
			return inspection, err
		}
		installed := stillAdded(snapshot, added)
		if len(installed) != 0 {
			if err := removal.EraseTest(ctx, installed); err != nil {
				return inspection, err
			}
		}
		inspection.packages = installed
	}
	return inspection, nil
}

func stillAdded(snapshot, added []InstalledPackage) []string {
	ours := map[string]bool{}
	for _, item := range added {
		ours[packageIdentity(item)] = true
	}
	installed := []string{}
	for _, item := range snapshot {
		if argument := PackageArgument(item); argument != "" && ours[packageIdentity(item)] && !slices.Contains(installed, argument) {
			installed = append(installed, argument)
		}
	}
	slices.Sort(installed)
	return installed
}

func (s Service) ownedObjectsGone(ctx context.Context) error {
	owned, err := s.ports.Owned.OwnedObjects(ctx)
	if err != nil {
		return err
	}
	if len(owned) == 0 {
		return nil
	}
	named := make([]string, 0, len(owned))
	for _, object := range owned {
		named = append(named, object.Kind+" "+object.Name)
	}
	slices.Sort(named)
	return failure(uninstallCode, "Bootwright-owned objects remain on this host: "+strings.Join(named, ", "),
		"destroy the context that owns each with a build that reads it (bootwright destroy --context <name>), or remove each by hand, then run "+uninstallInvocation+" again")
}

func receiptNames(setup SetupView, keep bool) ([]InstalledPackage, []string, error) {
	refuse := func(message string) ([]InstalledPackage, []string, error) {
		return nil, nil, failure(uninstallCode, message+", so the host packages it added are not known",
			"run bootwright controller install, then run "+uninstallInvocation+" again; or pass --keep-packages to leave every host package")
	}
	if !keep && setup.Foreign != "" {
		return refuse("the install record was written by another Bootwright build")
	}
	if !keep && setup.State.Pending != nil {
		return refuse("an earlier install (" + setup.State.Pending.ID + ") was interrupted before it recorded what it installed")
	}
	before := []string{}
	if setup.Foreign != "" || setup.State.Receipt == nil {
		return []InstalledPackage{}, before, nil
	}
	for _, item := range setup.State.Receipt.Packages {
		name, ok := providerName(item.Provider)
		if ok && (item.InstalledBy == PresentBeforeBootwright || item.InstalledBy == "") && !slices.Contains(before, name) {
			before = append(before, name)
		}
	}
	for _, item := range setup.State.Receipt.Added {
		if !ValidInstalledPackage(item) && !keep {
			return refuse("the install record names a package this build cannot read")
		}
	}
	slices.Sort(before)
	return MergeAdded(setup.State.Receipt.Added), before, nil
}

func providerName(provider string) (string, bool) {
	release := strings.LastIndex(provider, "-")
	if release <= 0 {
		return "", false
	}
	version := strings.LastIndex(provider[:release], "-")
	if version <= 0 || !receiptEpoch.MatchString(provider[version+1:release]) {
		return "", false
	}
	name := provider[:version]
	return name, len(name) <= 128 && ValidHostRequirement(name) && !strings.ContainsAny(name, "() =")
}

func (s Service) removalPreview(ctx context.Context, removal HostPackageRemoval, inspected uninstallInspection) (HostPackagePreview, error) {
	return removal.RemovalPreview(ctx, inspected.packages)
}

func (s Service) removePackages(ctx context.Context, removal HostPackageRemoval, inspected uninstallInspection, output RunOutput) error {
	before, err := removal.Snapshot(ctx)
	if err != nil {
		return err
	}
	names := inspected.packages
	if err := removal.Remove(ctx, names, output); err != nil {
		return err
	}
	after, err := removal.Snapshot(ctx)
	if err != nil {
		return err
	}
	present := map[string]bool{}
	for _, item := range after {
		present[PackageArgument(item)] = true
	}
	still, gone := []string{}, []string{}
	for _, name := range names {
		if present[name] {
			still = append(still, name)
		}
	}
	for _, item := range before {
		if argument := PackageArgument(item); !present[argument] && !slices.Contains(names, argument) && !slices.Contains(gone, argument) {
			gone = append(gone, argument)
		}
	}
	slices.Sort(gone)
	if len(still) != 0 {
		return failure(uninstallCode, "dnf did not remove "+strings.Join(still, ", "),
			"review the host's dnf configuration, then run "+uninstallInvocation+" again")
	}
	if len(gone) != 0 {
		return failure(uninstallCode, "dnf also removed packages no install transaction added: "+strings.Join(gone, ", "),
			"reinstall them with dnf, then run "+uninstallInvocation+" again to remove the state")
	}
	return nil
}

func stateAfterPackages(removed []string, err error) error {
	reason := "the state could not be removed"
	if reported := diagnostics.Of(err); len(reported) != 0 {
		reason = reported[0].Message
	}
	return failure(uninstallCode, "removed the host packages "+strings.Join(removed, ", ")+" but not the state: "+reason,
		"resolve what the message names, then run "+uninstallInvocation+" again to remove the state")
}

func (i uninstallInspection) plan() UninstallPlan {
	plan := UninstallPlan{Kept: i.keptText()}
	if len(i.packages) != 0 {
		plan.Actions = append(plan.Actions, UninstallAction{ID: RemoveHostPackagesAction,
			Text: "Remove the host packages install added: " + strings.Join(i.packages, ", "), Items: slices.Clone(i.packages)})
	}
	v := i.view
	var paths, texts []string
	add := func(seen bool, path, text string) {
		if seen {
			paths = append(paths, path)
			texts = append(texts, path+text)
		}
	}
	store := " (the install record, runtimes, wheel cache, controller record and tools)"
	if v.Unreadable != nil {
		store = " (a store this build cannot read: " + refusalText(v.Unreadable) + ")"
	}
	add(v.RootExists, v.Root, store)
	add(v.StagingSeen, v.Staging, "")
	add(v.LogsSeen, v.Logs, " (operation logs and run output)")
	add(v.HostedSeen, v.Hosted, " (what contexts realized on this host, physical-erase receipts included)")
	disks := " (empty)"
	if len(v.GuestDisks) != 0 {
		disks = " (guest disks no removal took back: " + strings.Join(v.GuestDisks, ", ") + ")"
	}
	add(v.ImagesSeen, v.Images, disks)
	if len(paths) != 0 {
		action := UninstallAction{ID: RemoveStateAction, Text: "Remove Bootwright's state on this host: " + strings.Join(texts, "; "), Items: paths}
		if i.discard {
			action = UninstallAction{ID: DiscardAction, Text: "Discard what this build cannot read or attribute, with everything beneath it: " + strings.Join(texts, "; "), Items: paths}
		}
		plan.Actions = append(plan.Actions, action)
	}
	if len(v.Leftovers) != 0 {
		plan.Actions = append(plan.Actions, UninstallAction{ID: FinishRemovalAction,
			Text: "Finish removing what an interrupted uninstall renamed: " + strings.Join(v.Leftovers, ", "), Items: slices.Clone(v.Leftovers)})
	}
	return plan
}

func refusalText(err error) string {
	if reported := diagnostics.Of(err); len(reported) != 0 {
		return reported[0].Message
	}
	return "it holds another build's state"
}

func (i uninstallInspection) keptText() []string {
	kept := []string{}
	for _, name := range i.before {
		kept = append(kept, name+" (present before bootwright)")
	}
	for _, item := range i.kept {
		kept = append(kept, item.NEVRA+" (install added it; --keep-packages keeps it)")
	}
	if i.discard {
		kept = append(kept, "every host package: --discard-unreadable removes none")
	}
	return append(kept, "the host packages present before Bootwright", "the host packages a controller stage installed", "the bootwright executable",
		"what a context realized on an SSH placement host, which no uninstall reaches")
}
