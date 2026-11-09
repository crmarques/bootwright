package cli

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
)

// The uninstall plan names each action by its identifier, dnf's preview
// verbatim and what stays, and a declined prompt refuses under the
// uninstall's own code with the command repeated with --yes.
func TestAnUninstallPlanAndPromptNameWhatTheyRemove(t *testing.T) {
	var out bytes.Buffer
	presenter := NewControllerPresenter(WithPresentation(&out, PresentationOptions{Format: "text"}), io.Discard, nil)
	err := presenter.PresentUninstallPlan(context.Background(), prerequisites.UninstallPlan{
		Actions: []prerequisites.UninstallAction{
			{ID: prerequisites.RemoveHostPackagesAction, Text: "Remove the host packages install added: podman"},
			{ID: prerequisites.RemoveStateAction, Text: "Remove Bootwright's state on this host: /var/lib/bootwright"},
		},
		Preview: &prerequisites.HostPackagePreview{Text: "Removing:\n podman  x86_64  5:5.6.1-1.fc44\n"},
		Kept:    []string{"openssh-clients (present before bootwright)", "the bootwright executable"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Controller uninstall", "dnf preview", " podman  x86_64  5:5.6.1-1.fc44",
		"remove-host-packages: Remove the host packages install added: podman", "remove-state: ", "Stays on this host", "openssh-clients (present before bootwright)"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("the plan lacks %q:\n%s", want, out.String())
		}
	}
	answers := strings.NewReader("n\n")
	read := func(_ context.Context, p []byte) (int, error) { return answers.Read(p) }
	var prompt bytes.Buffer
	refusal := NewConfirmation(read, &prompt, func() (bool, error) { return true, nil }).Repeating([]string{"controller", "uninstall", "--keep-packages"}).
		Confirm(context.Background(), "controller uninstall", "this host")
	reported := diagnostics.Of(refusal)
	if prompt.String() != "Confirm controller uninstall on this host? [y/N] " || len(reported) != 1 || reported[0].Code != "controller.uninstall" ||
		!strings.Contains(reported[0].Remediation, "controller uninstall --keep-packages") || !strings.Contains(reported[0].Remediation, "--yes") {
		t.Fatalf("prompt %q, refusal %+v", prompt.String(), reported)
	}
}

// A discard removes no package, so it conflicts with --keep-packages before
// any service runs.
func TestADiscardWithKeepPackagesIsAUsageError(t *testing.T) {
	code, out, errOut, record := runRecorded([]string{"controller", "uninstall", "--discard-unreadable", "--keep-packages"})
	if code != 2 || out != "" || record.calls != 0 ||
		!strings.HasPrefix(errOut, "  error  cli.usage\n    --discard-unreadable conflicts with --keep-packages; a discard removes no package\n") {
		t.Fatalf("exit=%d stdout=%q stderr=%q calls=%d", code, out, errOut, record.calls)
	}
}

// A discard's plan names its action and what stays, the packages it never
// removes among them, and shows no dnf preview.
func TestADiscardPlanNamesWhatItDiscardsAndWhatStays(t *testing.T) {
	var out bytes.Buffer
	presenter := NewControllerPresenter(WithPresentation(&out, PresentationOptions{Format: "text"}), io.Discard, nil)
	err := presenter.PresentUninstallPlan(context.Background(), prerequisites.UninstallPlan{
		Actions: []prerequisites.UninstallAction{{ID: prerequisites.DiscardAction,
			Text: "Discard what this build cannot read or attribute, with everything beneath it: /var/lib/bootwright (a store this build cannot read)"}},
		Kept: []string{"every host package: --discard-unreadable removes none", "the bootwright executable"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"discard: Discard what this build cannot read or attribute", "Stays on this host", "every host package: --discard-unreadable removes none"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("the plan lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "dnf preview") {
		t.Fatalf("a discard plan shows a dnf preview:\n%s", out.String())
	}
}
