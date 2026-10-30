package prerequisites

import (
	"context"
	"regexp"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

type UninstallRequest struct {
	SkipConfirmation bool
	KeepPackages     bool
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
	FinishRemovalAction      = "finish-removal"
	uninstallInvocation      = "bootwright controller uninstall"
	uninstallCode            = "controller.uninstall"
)

type UninstallView struct {
	Root        string
	Staging     string
	RootExists  bool
	StagingSeen bool
	Leftovers   []string
	Contexts    []string
	Media       []string
	Setup       SetupView
}

var receiptEpoch = regexp.MustCompile(`^[0-9]+:`)

type uninstallInspection struct {
	view     UninstallView
	packages []string
	before   []string
	kept     []string
}

func (i uninstallInspection) empty() bool {
	return !i.view.RootExists && !i.view.StagingSeen && len(i.view.Leftovers) == 0
}

func (i uninstallInspection) same(other uninstallInspection) bool {
	return i.view.RootExists == other.view.RootExists && i.view.StagingSeen == other.view.StagingSeen &&
		slices.Equal(i.view.Leftovers, other.view.Leftovers) && slices.Equal(i.packages, other.packages) && slices.Equal(i.before, other.before) && slices.Equal(i.kept, other.kept)
}

func (s Service) Uninstall(ctx context.Context, request UninstallRequest) (*UninstallResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	store, stores := s.storage.(UninstallStore)
	presenter, presents := s.options.Presenter.(UninstallPresenter)
	removal, removes := s.ports.Packages.(HostPackageRemoval)
	if !stores || !presents || !request.KeepPackages && !removes {
		return nil, failure(uninstallCode, "controller uninstall is not configured", "")
	}
	var planned uninstallInspection
	err := store.Uninstall(ctx, func(tx UninstallTransaction) error {
		var err error
		planned, err = s.inspectUninstall(ctx, tx.View(), request.KeepPackages, removal)
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
		fresh, err := s.inspectUninstall(ctx, tx.View(), request.KeepPackages, removal)
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

func (s Service) inspectUninstall(ctx context.Context, view UninstallView, keep bool, removal HostPackageRemoval) (uninstallInspection, error) {
	inspection := uninstallInspection{view: view}
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
	added, before, err := receiptNames(view.Setup, keep)
	if err != nil {
		return inspection, err
	}
	inspection.before = before
	if keep {
		inspection.kept = added
	} else if len(added) != 0 {
		installed, err := removal.InstalledNames(ctx, added)
		if err != nil {
			return inspection, err
		}
		if len(installed) != 0 {
			if err := removal.EraseTest(ctx, installed); err != nil {
				return inspection, err
			}
		}
		inspection.packages = installed
	}
	return inspection, nil
}

func receiptNames(setup SetupView, keep bool) ([]string, []string, error) {
	refuse := func(message string) ([]string, []string, error) {
		return nil, nil, failure(uninstallCode, message+", so the host packages it added are not known",
			"run bootwright controller install, then run "+uninstallInvocation+" again; or pass --keep-packages to leave every host package")
	}
	if !keep && setup.Foreign != "" {
		return refuse("the install record was written by another Bootwright build")
	}
	if !keep && setup.State.Pending != nil {
		return refuse("an earlier install (" + setup.State.Pending.ID + ") was interrupted before it recorded what it installed")
	}
	added, before := []string{}, []string{}
	if setup.Foreign != "" || setup.State.Receipt == nil {
		return added, before, nil
	}
	for _, item := range setup.State.Receipt.Packages {
		name, ok := providerName(item.Provider)
		if !ok {
			if keep {
				continue
			}
			return refuse("the install record names a package this build cannot read")
		}
		target := &added
		if item.InstalledBy == PresentBeforeBootwright || item.InstalledBy == "" {
			target = &before
		}
		if !slices.Contains(*target, name) {
			*target = append(*target, name)
		}
	}
	added = slices.DeleteFunc(added, func(name string) bool { return slices.Contains(before, name) })
	slices.Sort(added)
	slices.Sort(before)
	return added, before, nil
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
	preview, err := removal.RemovalPreview(ctx, inspected.packages)
	if err != nil {
		return HostPackagePreview{}, err
	}
	named := previewNames(preview, inspected.before)
	if len(named) != 0 {
		return HostPackagePreview{}, failure(uninstallCode, "dnf would also remove packages present before Bootwright: "+strings.Join(named, ", "),
			"pass --keep-packages to leave every host package, or remove the packages install added yourself")
	}
	return preview, nil
}

func previewNames(preview HostPackagePreview, before []string) []string {
	if preview.Truncated {
		return slices.Clone(before)
	}
	named := []string{}
	for _, field := range strings.Fields(preview.Text) {
		for _, name := range before {
			if field == name || strings.HasPrefix(field, name+".") || strings.HasPrefix(field, name+"-") && len(field) > len(name)+1 &&
				field[len(name)+1] >= '0' && field[len(name)+1] <= '9' {
				if !slices.Contains(named, name) {
					named = append(named, name)
				}
			}
		}
	}
	slices.Sort(named)
	return named
}

func (s Service) removePackages(ctx context.Context, removal HostPackageRemoval, inspected uninstallInspection, output RunOutput) error {
	if _, err := s.removalPreview(ctx, removal, inspected); err != nil {
		return err
	}
	kept := []string{}
	if len(inspected.before) != 0 {
		var err error
		if kept, err = removal.InstalledNames(ctx, inspected.before); err != nil {
			return err
		}
	}
	names := inspected.packages
	if err := removal.Remove(ctx, names, output); err != nil {
		return err
	}
	still, err := removal.InstalledNames(ctx, names)
	if err != nil {
		return err
	}
	if len(still) != 0 {
		return failure(uninstallCode, "dnf did not remove "+strings.Join(still, ", "),
			"review the host's dnf configuration, then run "+uninstallInvocation+" again")
	}
	if len(kept) != 0 {
		remaining, err := removal.InstalledNames(ctx, kept)
		if err != nil {
			return err
		}
		if gone := slices.DeleteFunc(slices.Clone(kept), func(name string) bool { return slices.Contains(remaining, name) }); len(gone) != 0 {
			return failure(uninstallCode, "dnf also removed packages present before Bootwright: "+strings.Join(gone, ", "),
				"reinstall them with dnf, then run "+uninstallInvocation+" again to remove the state")
		}
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
	var paths []string
	if i.view.RootExists {
		paths = append(paths, i.view.Root)
	}
	if i.view.StagingSeen {
		paths = append(paths, i.view.Staging)
	}
	if len(paths) != 0 {
		text := "Remove Bootwright's state on this host: "
		if i.view.RootExists {
			text += i.view.Root + " (the install record, runtimes, wheel cache, controller record and tools)"
			if i.view.StagingSeen {
				text += " and "
			}
		}
		if i.view.StagingSeen {
			text += i.view.Staging
		}
		plan.Actions = append(plan.Actions, UninstallAction{ID: RemoveStateAction, Text: text, Items: paths})
	}
	if len(i.view.Leftovers) != 0 {
		plan.Actions = append(plan.Actions, UninstallAction{ID: FinishRemovalAction,
			Text: "Finish removing what an interrupted uninstall renamed: " + strings.Join(i.view.Leftovers, ", "), Items: slices.Clone(i.view.Leftovers)})
	}
	return plan
}

func (i uninstallInspection) keptText() []string {
	kept := []string{}
	for _, name := range i.before {
		kept = append(kept, name+" (present before bootwright)")
	}
	for _, name := range i.kept {
		kept = append(kept, name+" (install added it; --keep-packages keeps it)")
	}
	return append(kept, "the host packages a controller stage installed", "the bootwright executable")
}
