package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
	"github.com/crmarques/bootwright/internal/workspace"
)

type safetyVerdict string

const (
	verdictUsageError safetyVerdict = "usage-error"
	verdictRefusal    safetyVerdict = "fail-closed refusal"
	verdictAccepted   safetyVerdict = "accepted (no gate refusal)"
	verdictOutOfSync  safetyVerdict = "read-only out-of-sync report"
	verdictAuthorized safetyVerdict = "authorized mutation (no gate refusal)"
	verdictPrompted   safetyVerdict = "held at the interactive confirmation"
)

var gateRefusalMarkers = []string{"refusing to", "refuses to", "fails closed", "does not authorize data loss"}

type safetyCase struct {
	name     string
	baseline string
	seed     func(t *testing.T, ctx workspace.Context)
	args     []string
	verdict  safetyVerdict
	want     []string
	deny     []string
}

func TestApplyDestroySafetyMatrix(t *testing.T) {
	for _, tc := range safetyMatrixCases() {
		t.Run(tc.name, func(t *testing.T) {
			ctx := initSafetyBaselineContext(t, tc.baseline)
			if tc.seed != nil {
				tc.seed(t, ctx)
			}
			stdout, stderr, code := runCLI(t, tc.args...)
			out := stdout + stderr
			switch tc.verdict {
			case verdictUsageError:
				if code != 2 {
					t.Fatalf("want usage error (exit 2), got exit %d\n%s", code, out)
				}
			case verdictRefusal:
				if code == 0 {
					t.Fatalf("want a fail-closed refusal, got exit 0\n%s", out)
				}
				if !strings.Contains(out, "bootwright ") {
					t.Errorf("a refusal must name the exact command to proceed intentionally; got:\n%s", out)
				}
			case verdictAccepted:
				if code != 0 {
					t.Fatalf("want the run accepted past the safety gates (exit 0), got exit %d\n%s", code, out)
				}
			case verdictOutOfSync:
				if code != 3 {
					t.Fatalf("want the read-only out-of-sync exit code 3, got exit %d\n%s", code, out)
				}
				assertNoRuntimeRecords(t, ctx)
			case verdictPrompted:
				if code == 0 {
					t.Fatalf("want the run held at an interactive confirmation, got exit 0\n%s", out)
				}
			case verdictAuthorized:
				for _, marker := range gateRefusalMarkers {
					if strings.Contains(out, marker) {
						t.Fatalf("run is authorized by its records and flags, but a safety gate refused it (%q):\n%s", marker, out)
					}
				}
			}
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("output must contain %q; got:\n%s", want, out)
				}
			}
			for _, deny := range tc.deny {
				if strings.Contains(out, deny) {
					t.Errorf("output must not contain %q; got:\n%s", deny, out)
				}
			}
		})
	}
}

func safetyMatrixCases() []safetyCase {
	return append(append(append(append(append(append(append(
		safetyFlagCoherenceCases(),
		safetyAuthorizationTokenCases()...),
		safetyBlanketAuthorizationCases()...),
		safetyStorageDataLossCases()...),
		safetyScopeClosureCases()...),
		safetyStartingStateCases()...),
		safetyReplaceArbiterCases()...),
		safetyPreviewAuthorizationCases()...)
}

func safetyPreviewAuthorizationCases() []safetyCase {
	return []safetyCase{{
		name:    "destroy/preview: a dry run names the data-loss token its real counterpart refuses on",
		args:    []string{"destroy", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"Required authorizations", "--authorize " + authorizeDataLoss, authorizationRequired},
	}, {
		name:    "destroy/preview: the non-dry counterpart refuses on exactly that token",
		args:    []string{"destroy", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"--authorize " + authorizeDataLoss},
	}, {
		name:    "apply/preview: the json dialect names the tokenless refusal its real run makes",
		seed:    seedUnreadableOwnershipRecord,
		args:    []string{"apply", "--stage", "infra", "--clusters", "dc1-metal-ocp", "--dry-run", "--output", "json", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"\"refusals\"", "could not be read", "no authorization skips this on apply"},
	}, {
		name:    "destroy/preview: the json dialect discloses the history purge the text dialect warns about",
		args:    []string{"destroy", "--purge-history", "--dry-run", "--output", "json", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"\"purgeHistory\""},
	}, {
		name: "destroy/preview: a dry run forecasts installed-cluster-node as required instead of refusing",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedInstalledCluster(t, ctx, "dc1-metal-ocp")
		},
		args:    []string{"destroy", "--machines", "dc1-metal-master-0", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"--authorize " + authorizeInstalledClusterNode + ": " + authorizationRequired},
		deny:    []string{"refusing to destroy machine(s)"},
	}, {
		name:    "destroy/preview: a dry run forecasts shared-infra as required instead of refusing",
		args:    []string{"destroy", "--stage", "infra", "--clusters", safetyAdvancedCephCluster, "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"--authorize " + authorizeSharedInfra + ": " + authorizationRequired},
		deny:    []string{"refusing to"},
	}, {
		name:    "destroy/preview: a dry run forecasts protected alongside data-loss",
		seed:    seedProtectedEnvironment,
		args:    []string{"destroy", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"Required authorizations", "--authorize " + authorizeProtected + ": " + authorizationRequired, "--authorize " + authorizeDataLoss},
	}, {
		name:    "destroy/preview: a supplied protected token still appears, as satisfied",
		seed:    seedProtectedEnvironment,
		args:    []string{"destroy", "--dry-run", "--authorize", authorizeProtected, "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"--authorize " + authorizeProtected + ": " + authorizationSatisfied},
	}, {
		name:    "destroy/preview: a token already on the command line is satisfied, not required",
		args:    []string{"destroy", "--dry-run", "--authorize", authorizeDataLoss, "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"--authorize " + authorizeDataLoss + ": " + authorizationSatisfied},
	}, {
		name:    "destroy/preview: a host-decided token is disclosed as may be required, never omitted",
		args:    []string{"destroy", "--stage", "infra", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"--authorize " + authorizeUnownedVMs + ": " + authorizationMaybe, "--authorize " + authorizeUnreachableNodes},
	}, {
		name:    "destroy/preview: the JSON preview carries the same tokens",
		args:    []string{"destroy", "--dry-run", "--output", "json", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"requiredAuthorizations", "\"token\": \"" + authorizeDataLoss + "\"", "\"status\": \"" + authorizationRequired + "\""},
	}, {
		name:    "destroy/preview: --purge-history discloses the history a real run would delete",
		args:    []string{"destroy", "--purge-history", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"purge history", "a real run of this destroy would also delete"},
	}, {
		name:    "apply/preview: a dry run names the data-loss token its real counterpart refuses on",
		seed:    seedSuccessfulApply,
		args:    []string{"apply", "--through", "base", "--clusters", safetyAdvancedCephCluster, "--reclaim-devices", "/dev/sdb", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"Required authorizations", "--authorize " + authorizeDataLoss + ": " + authorizationRequired},
	}, {
		name:    "apply/preview: the non-dry counterpart refuses on exactly that token",
		seed:    seedSuccessfulApply,
		args:    []string{"apply", "--through", "base", "--clusters", safetyAdvancedCephCluster, "--reclaim-devices", "/dev/sdb", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"--yes does not authorize data loss", "--authorize " + authorizeDataLoss},
	}, {
		name:    "apply/preview: --reclaim-devices all expands to the declared devices in the forecast",
		seed:    seedSuccessfulApply,
		args:    []string{"apply", "--through", "base", "--clusters", safetyAdvancedCephCluster, "--reclaim-devices", "all", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"Required authorizations", "--authorize " + authorizeDataLoss + ": " + authorizationRequired, "reclaim-devices /dev/sdb", "every declared OSD device"},
	}, {
		name:    "apply/reclaim-devices all in the non-dry counterpart refuses with the expanded devices",
		seed:    seedSuccessfulApply,
		args:    []string{"apply", "--through", "base", "--clusters", safetyAdvancedCephCluster, "--reclaim-devices", "all", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"--yes does not authorize data loss", "reclaim-devices /dev/sdb", "--authorize " + authorizeDataLoss},
	}, {
		name:    "apply/reclaim-devices all mixed with a path is a usage error",
		args:    []string{"apply", "--through", "base", "--clusters", safetyAdvancedCephCluster, "--reclaim-devices", "all,/dev/sdb", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"--reclaim-devices lists all together", "bootwright apply --reclaim-devices all"},
	}, {
		name:    "apply/preview: host-decided apply tokens are disclosed as may be required",
		args:    []string{"apply", "--stage", "base", "--clusters", safetyAdvancedCephCluster, "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"--authorize " + authorizeForeignDaemons + ": " + authorizationMaybe},
	}, {
		name:    "apply/preview: the JSON preview carries the same tokens",
		args:    []string{"apply", "--stage", "deps", "--clusters", safetyAdvancedCephCluster, "--reclaim-devices", "/dev/sdb", "--dry-run", "--output", "json", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"requiredAuthorizations", "\"token\": \"" + authorizeUnownedDevices + "\""},
	}}
}

func safetyBlanketAuthorizationCases() []safetyCase {
	return []safetyCase{{
		name:    "destroy/all: the blanket token clears the data-loss refusal and discloses what it stood in for",
		args:    []string{"destroy", "--authorize", "all", "--ask-become-pass=false"},
		verdict: verdictPrompted,
		want:    []string{"Continue with destroy?", "authorize all", "stood in for", authorizeUnownedVMs, authorizeDataLoss},
		deny:    []string{"--yes does not authorize data loss", "Confirm this DESTRUCTIVE action"},
	}, {
		name:    "destroy/all: the expansion reaches every per-token extra var of the run",
		args:    []string{"destroy", "--stage", "infra", "--authorize", "all", "--dry-run", "--output", "json", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"bootwright_destroy_authorize_unowned_vms=true", "bootwright_destroy_authorize_unowned_networks=true", "bootwright_destroy_skip_unreachable=true"},
	}, {
		name: "destroy/all: the blanket token still does not widen the selection",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedInstalledCluster(t, ctx, "dc1-child-ocp")
		},
		args:    []string{"destroy", "--clusters", "dc1-metal-ocp", "--authorize", "all", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"dc1-child-ocp", "no --authorize token widens"},
	}, {
		name:    "apply/all: a dry-run consumes none of it and names it",
		args:    []string{"apply", "--stage", "deps", "--clusters", safetyAdvancedCephCluster, "--reclaim-devices", "/dev/sdb", "--authorize", "all", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"--authorize all is not consumed by a dry-run"},
	}, {
		name: "apply/all: the blanket token clears no refusal that has no token of its own",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedInstalledCluster(t, ctx, "dc1-metal-ocp-old")
		},
		args:    []string{"apply", "--stage", "clusters", "--clusters", "dc1-metal-ocp", "--authorize", "all", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"dc1-metal-ocp-old", "the signature of a rename"},
	}, {
		name:    "apply/all: the blanket token does not clear the tokenless unreadable-ownership refusal",
		seed:    seedUnreadableOwnershipRecord,
		args:    []string{"apply", "--stage", "infra", "--clusters", "dc1-metal-ocp", "--authorize", "all", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"could not be read", "no authorization skips this on apply"},
	}}
}

func safetyFlagCoherenceCases() []safetyCase {
	return []safetyCase{{
		name:    "apply/retired --expect-new is an unknown flag/greenfield/full",
		args:    []string{"apply", "--expect-new", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"unknown flag"},
	}, {
		name:    "apply/retired --converge-drifted is an unknown flag/greenfield/full",
		args:    []string{"apply", "--converge-drifted", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"unknown flag"},
	}, {
		name:    "apply/retired --confirm-data-loss is an unknown flag/greenfield/full",
		args:    []string{"apply", "--confirm-data-loss", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"unknown flag"},
	}, {
		name:    "destroy/retired --force is an unknown flag/any/full",
		args:    []string{"destroy", "--force", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"unknown flag"},
	}, {
		name:    "destroy/retired --include-unowned is an unknown flag/any/full",
		args:    []string{"destroy", "--include-unowned", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"unknown flag"},
	}, {
		name:    "destroy/retired --skip-unreachable is an unknown flag/any/full",
		args:    []string{"destroy", "--skip-unreachable", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"unknown flag"},
	}, {
		name:    "apply/retired --mode value/greenfield/full",
		args:    []string{"apply", "--mode", "override", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"--mode must be one of", "create", "reconcile", "rebuild"},
	}, {
		name:    "apply/unknown --authorize token/greenfield/full",
		args:    []string{"apply", "--authorize", "force", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"is not an authorization token", "data-loss"},
	}, {
		name:    "apply/a destroy-only --authorize token is a usage error naming the verb that owns it",
		args:    []string{"apply", "--authorize", "protected", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"is not a risk apply can authorize", "bootwright destroy --authorize protected"},
	}, {
		name:    "plan/a destroy-only --authorize token is a usage error",
		args:    []string{"plan", "--authorize", "unreachable-nodes"},
		verdict: verdictUsageError,
		want:    []string{"is not a risk apply can authorize"},
	}, {
		name:    "destroy/unknown --authorize token in a comma list/any/full",
		args:    []string{"destroy", "--authorize", "data-loss,everything", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"is not an authorization token"},
	}, {
		name:    "apply/machines+clusters/greenfield/mixed",
		args:    []string{"apply", "--machines", "dc1-metal-master-0", "--clusters", "dc1-metal-ocp", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"mutually exclusive"},
	}, {
		name:    "apply/reclaim-devices outside deps/greenfield/stage add-ons",
		args:    []string{"apply", "--stage", "add-ons", "--reclaim-devices", "/dev/sdb", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"deps phase", "--stage deps"},
	}, {
		name:    "apply/rebuild skipping the device gate/greenfield/stage base",
		args:    []string{"apply", "--mode", "rebuild", "--stage", "base", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"--mode rebuild --through base"},
	}, {
		name:    "destroy/sub-phase stage/any/fabric",
		args:    []string{"destroy", "--stage", "fabric", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"apply-only"},
	}, {
		name:    "destroy/purge-history with the artifact-server literal/any/infra",
		args:    []string{"destroy", "--stage", "infra", "--clusters", "artifact-server", "--purge-history", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"--purge-history"},
	}, {
		name:    "apply/json without dry-run/greenfield/full",
		args:    []string{"apply", "--output", "json", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"--dry-run"},
	}}
}

func safetyReplaceArbiterCases() []safetyCase {
	return []safetyCase{{
		name:    "apply/same-site-arbiter is not a risk apply can authorize",
		args:    []string{"apply", "--authorize", "same-site-arbiter", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"same-site-arbiter", "replace-arbiter"},
	}, {
		name:    "destroy/degraded-quorum is not a risk destroy can authorize",
		args:    []string{"destroy", "--authorize", "degraded-quorum", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"degraded-quorum", "replace-arbiter"},
	}, {
		name:    "replace-arbiter/a candidate bound to another cluster is refused before the cluster is read",
		args:    []string{"storage-cluster", "replace-arbiter", "--name", safetyAdvancedCephCluster, "--new-arbiter-machine", "dc2-child-ocp-infra-master-0", "--authorize", "same-site-arbiter,degraded-quorum", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"dc2-child-ocp", "ceph-arbiter"},
	}, {
		name:    "replace-arbiter/an unknown machine is refused before the cluster is read",
		args:    []string{"storage-cluster", "replace-arbiter", "--name", safetyAdvancedCephCluster, "--new-arbiter-machine", "no-such-machine", "--authorize", "degraded-quorum", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"no-such-machine", "matches no Machine"},
	}, {
		name:    "replace-arbiter/a machine without the arbiter capability is refused",
		args:    []string{"storage-cluster", "replace-arbiter", "--name", safetyAdvancedCephCluster, "--new-arbiter-machine", "bastion", "--authorize", "same-site-arbiter", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"bastion", "ceph-node"},
	}, {
		name: "apply/a re-authored stretch tiebreaker routes to replace-arbiter, never to rebuild",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedSuccessfulApply(t, ctx)
			seedRetargetedTiebreaker(t, ctx)
		},
		args:    []string{"apply", "--stage", "clusters", "--clusters", safetyAdvancedCephCluster, "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"bootwright storage-cluster replace-arbiter --name " + safetyAdvancedCephCluster, "spec.ceph.topology.stretch.tiebreaker"},
		deny:    []string{"re-run with `bootwright apply --mode rebuild`"},
	}, {
		name: "apply/a tiebreaker change alongside another structural change keeps routing to rebuild",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedSuccessfulApply(t, ctx)
			seedRetargetedTiebreaker(t, ctx)
			seedRenamedStretchRule(t, ctx)
		},
		args:    []string{"apply", "--stage", "clusters", "--clusters", safetyAdvancedCephCluster, "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"re-run with `bootwright apply --mode rebuild`"},
		deny:    []string{"bootwright storage-cluster replace-arbiter --name"},
	}, {
		name: "replace-arbiter/refreshing the storage records leaves the next apply no drift to refuse",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedSuccessfulApply(t, ctx)
			seedArbiterConvergeRecordRefresh(t, ctx, seedRetargetedTiebreaker)
		},
		args:    []string{"apply", "--stage", "clusters", "--clusters", safetyAdvancedCephCluster, "--yes", "--ask-become-pass=false"},
		verdict: verdictAuthorized,
		deny:    []string{"not a safe in-place reconcile", "bootwright storage-cluster replace-arbiter --name"},
	}, {
		name: "replace-arbiter/the record refresh never rebaselines a change the arbiter run did not converge",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedSuccessfulApply(t, ctx)
			seedRenamedStretchRule(t, ctx)
			seedArbiterConvergeRecordRefresh(t, ctx, seedRetargetedTiebreaker)
		},
		args:    []string{"apply", "--stage", "clusters", "--clusters", safetyAdvancedCephCluster, "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"re-run with `bootwright apply --mode rebuild`"},
	}, {
		name:    "apply/adding an arbiter to a built cluster is refused as a day-1 shape, naming no command that refuses",
		seed:    seedEnabledStretchArbiter,
		args:    []string{"apply", "--stage", "clusters", "--clusters", safetyAdvancedCephCluster, "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"fixed when the cluster is bootstrapped", "Bootwright has no path that moves a running cluster between the two shapes"},
		deny:    []string{"bootwright storage-cluster replace-arbiter --name", "re-run with `bootwright apply --mode rebuild`"},
	}, {
		name:    "apply/removing an arbiter from a built cluster is refused without naming a rebuild remedy",
		seed:    seedDisabledStretchArbiter,
		args:    []string{"apply", "--stage", "clusters", "--clusters", safetyAdvancedCephCluster, "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"fixed when the cluster is bootstrapped", "destroys all OSD data"},
		deny:    []string{"bootwright storage-cluster replace-arbiter --name", "re-run with `bootwright apply --mode rebuild`"},
	}, {
		name:    "replace-arbiter/an active apply run is refused before the input is rewritten",
		seed:    seedUndecodableRunLease,
		args:    []string{"storage-cluster", "replace-arbiter", "--name", safetyAdvancedCephCluster, "--new-arbiter-machine", "ceph-arbiter", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"rm -f"},
		deny:    []string{"now declares tiebreaker", "input-history"},
	}, {
		name:    "replace-arbiter/preview: a dry run names every gate the real run consults, in one refusal-free pass",
		seed:    seedLiveStretchClusterOffItsArbiter,
		args:    []string{"storage-cluster", "replace-arbiter", "--name", safetyAdvancedCephCluster, "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want: []string{"Required authorizations",
			"--authorize " + authorizeSameSiteArbiter + ": " + authorizationRequired,
			"--authorize " + authorizeDegradedQuorum + ": " + authorizationRequired,
			"--authorize " + authorizeUnreachableNodes + ": " + authorizationMaybe},
		deny: []string{"refusing to"},
	}, {
		name:    "replace-arbiter/preview: the non-dry counterpart refuses on the first token the preview named",
		seed:    seedLiveStretchClusterOffItsArbiter,
		args:    []string{"storage-cluster", "replace-arbiter", "--name", safetyAdvancedCephCluster, "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"refusing to", "--authorize " + authorizeSameSiteArbiter},
	}, {
		name:    "replace-arbiter/preview: the non-dry counterpart then refuses on the second token the preview named",
		seed:    seedLiveStretchClusterOffItsArbiter,
		args:    []string{"storage-cluster", "replace-arbiter", "--name", safetyAdvancedCephCluster, "--authorize", authorizeSameSiteArbiter, "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"refusing to move the stretch tiebreaker", "--authorize " + authorizeDegradedQuorum},
	}, {
		name:    "replace-arbiter/preview: a supplied token reads satisfied and still previews the gate it clears",
		seed:    seedLiveStretchClusterOffItsArbiter,
		args:    []string{"storage-cluster", "replace-arbiter", "--name", safetyAdvancedCephCluster, "--authorize", authorizeDegradedQuorum, "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want: []string{"--authorize " + authorizeDegradedQuorum + ": " + authorizationSatisfied,
			"--authorize " + authorizeSameSiteArbiter + ": " + authorizationRequired},
	}}
}

func safetyStorageClusterInputPath(t *testing.T, ctx workspace.Context) string {
	t.Helper()
	files, err := desiredstate.LoadedInputFiles(ctx.InputPaths)
	if err != nil {
		t.Fatalf("resolve selected input files: %v", err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		body := string(data)
		if strings.Contains(body, "kind: "+v1alpha1.KindStorageCluster) && strings.Contains(body, "name: "+safetyAdvancedCephCluster) {
			return file
		}
	}
	t.Fatalf("no %s input file declares %s", v1alpha1.KindStorageCluster, safetyAdvancedCephCluster)
	return ""
}

func rewriteSafetyInput(t *testing.T, path string, replacements [][2]string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body := string(data)
	for _, pair := range replacements {
		next := strings.Replace(body, pair[0], pair[1], 1)
		if next == body {
			t.Fatalf("%s does not contain %q", path, pair[0])
		}
		body = next
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func seedRetargetedTiebreaker(t *testing.T, ctx workspace.Context) {
	t.Helper()
	rewriteSafetyInput(t, safetyStorageClusterInputPath(t, ctx), [][2]string{
		{"node: node-07", "node: node-08"},
		{"- name: node-07", "- name: node-08"},
	})
}

func seedRenamedStretchRule(t *testing.T, ctx workspace.Context) {
	t.Helper()
	rewriteSafetyInput(t, safetyStorageClusterInputPath(t, ctx), [][2]string{
		{"        failureDomain: datacenter", "        failureDomain: datacenter\n        ruleName: stretch-rule-alt"},
	})
}

const (
	safetyArbiterTiebreakerBlock = "        tiebreaker:\n          node: node-07\n"
	safetyArbiterNodeBlock       = "      - name: node-07\n        machineRef: ceph-arbiter\n        roles:\n        - mon\n"
)

func seedRetiredStretchArbiter(t *testing.T, ctx workspace.Context) {
	t.Helper()
	rewriteSafetyInput(t, safetyStorageClusterInputPath(t, ctx), [][2]string{
		{safetyArbiterTiebreakerBlock, ""},
		{safetyArbiterNodeBlock, ""},
	})
}

func seedRestoredStretchArbiter(t *testing.T, ctx workspace.Context) {
	t.Helper()
	rewriteSafetyInput(t, safetyStorageClusterInputPath(t, ctx), [][2]string{
		{"        failureDomain: datacenter\n", "        failureDomain: datacenter\n" + safetyArbiterTiebreakerBlock},
	})
	path := safetyStorageClusterInputPath(t, ctx)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	body := string(data)
	if !strings.HasSuffix(body, "\n") {
		body += "\n"
	}
	if err := os.WriteFile(path, []byte(body+safetyArbiterNodeBlock), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func seedEnabledStretchArbiter(t *testing.T, ctx workspace.Context) {
	t.Helper()
	seedRetiredStretchArbiter(t, ctx)
	seedSuccessfulApply(t, ctx)
	seedRestoredStretchArbiter(t, ctx)
}

func seedDisabledStretchArbiter(t *testing.T, ctx workspace.Context) {
	t.Helper()
	seedSuccessfulApply(t, ctx)
	seedRetiredStretchArbiter(t, ctx)
}

func seedArbiterConvergeRecordRefresh(t *testing.T, ctx workspace.Context, rewrite func(*testing.T, workspace.Context)) {
	t.Helper()
	before, err := desiredstate.LoadNormalizeValidateInputFiles(ctx.InputPaths)
	if err != nil {
		t.Fatalf("load desired state before the rewrite: %v", err)
	}
	rewrite(t, ctx)
	after, err := desiredstate.LoadNormalizeValidateInputFiles(ctx.InputPaths)
	if err != nil {
		t.Fatalf("load desired state after the rewrite: %v", err)
	}
	if _, err := refreshArbiterConvergeRecords(ctx, before, after, safetyAdvancedCephCluster); err != nil {
		t.Fatalf("refreshArbiterConvergeRecords: %v", err)
	}
}

func safetyAuthorizationTokenCases() []safetyCase {
	return []safetyCase{{
		name:    "destroy/data-loss: --yes alone no longer authorizes an OSD zap",
		args:    []string{"destroy", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"ceph-storage", "--yes does not authorize data loss", "--authorize data-loss"},
	}, {
		name:    "destroy/data-loss: the interactive data-loss prompt still stands without --yes",
		args:    []string{"destroy", "--ask-become-pass=false"},
		verdict: verdictPrompted,
		want:    []string{"Confirm this DESTRUCTIVE action (accept data loss)?"},
		deny:    []string{"--yes does not authorize data loss"},
	}, {
		name:    "destroy/data-loss: the token clears the data-loss refusal and the prompt",
		args:    []string{"destroy", "--authorize", "data-loss", "--ask-become-pass=false"},
		verdict: verdictPrompted,
		want:    []string{"Continue with destroy?"},
		deny:    []string{"--yes does not authorize data loss", "Confirm this DESTRUCTIVE action"},
	}, {
		name:    "apply/data-loss: an unused token is a warning, never an authorization",
		args:    []string{"apply", "--stage", "clusters", "--clusters", "ceph-storage", "--authorize", "data-loss", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"--authorize data-loss had no effect"},
		deny:    []string{"will DESTROY data"},
	}, {
		name:    "destroy/protected: a protected Environment refuses and names its token",
		seed:    seedProtectedEnvironment,
		args:    []string{"destroy", "--stage", "clusters", "--clusters", "dc1-metal-ocp", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"destroyProtection=protected", "--authorize protected"},
	}, {
		name:    "destroy/protected: data-loss does not stand in for protected",
		seed:    seedProtectedEnvironment,
		args:    []string{"destroy", "--stage", "clusters", "--clusters", "dc1-metal-ocp", "--authorize", "data-loss", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"--authorize protected"},
	}, {
		name: "destroy/installed-cluster-node: a node of an installed cluster refuses and names its token",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedInstalledCluster(t, ctx, "dc1-metal-ocp")
		},
		args:    []string{"destroy", "--machines", "dc1-metal-master-0", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"dc1-metal-ocp", "--authorize installed-cluster-node"},
	}, {
		name:    "destroy/unreadable-records: a corrupt ownership record refuses and names its token",
		seed:    seedUnreadableOwnershipRecord,
		args:    []string{"destroy", "--stage", "infra", "--clusters", "dc1-metal-ocp", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"could not be read", "--authorize unreadable-records"},
	}, {
		name:    "destroy/unreadable-records: the token clears the refusal and still discloses the skipped record",
		seed:    seedUnreadableOwnershipRecord,
		args:    []string{"destroy", "--stage", "infra", "--clusters", "dc1-metal-ocp", "--authorize", "unreadable-records", "--yes", "--ask-become-pass=false"},
		verdict: verdictAuthorized,
		want:    []string{"Skipped ownership records"},
	}, {
		name:    "apply/unreadable-records: a corrupt ownership record refuses before the rename-orphan gate reads it",
		seed:    seedUnreadableOwnershipRecord,
		args:    []string{"apply", "--stage", "infra", "--clusters", "dc1-metal-ocp", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"could not be read", "Repair or remove the reported record file(s)"},
		deny:    []string{"--authorize unreadable-records to"},
	}, {
		name:    "apply/unreadable-records: a dry run forecasts the refusal its real counterpart makes",
		seed:    seedUnreadableOwnershipRecord,
		args:    []string{"apply", "--stage", "infra", "--clusters", "dc1-metal-ocp", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"a real run refuses before any prompt", "could not be read"},
	}, {
		name:    "destroy/unowned-networks: the token arms the wider blast radius only when asked for",
		args:    []string{"destroy", "--stage", "infra", "--authorize", "unowned-vms,unowned-networks", "--dry-run", "--output", "json", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"bootwright_destroy_authorize_unowned_vms=true", "bootwright_destroy_authorize_unowned_networks=true"},
	}, {
		name:    "destroy/shared-infra: a storage consumer conflict refuses and names its token",
		args:    []string{"destroy", "--stage", "infra", "--clusters", "ceph-storage", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"ceph-storage", "--authorize shared-infra"},
	}, {
		name:    "destroy/unowned-vms: the token is inert outside the machine layer and says so",
		args:    []string{"destroy", "--stage", "clusters", "--authorize", "unowned-vms", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"--authorize unowned-vms had no effect"},
	}, {
		name:    "destroy/unowned-networks: authorizing VMs never authorizes networks",
		args:    []string{"destroy", "--stage", "infra", "--authorize", "unowned-vms", "--dry-run", "--output", "json", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"bootwright_destroy_authorize_unowned_vms=true"},
		deny:    []string{"bootwright_destroy_authorize_unowned_networks=true"},
	}, {
		name:    "destroy/unowned-devices: the token arms the unowned-signature device wipe",
		args:    []string{"destroy", "--stage", "clusters", "--authorize", "data-loss,unowned-devices", "--dry-run", "--output", "json", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"bootwright_ceph_authorize_unowned_devices=true"},
	}, {
		name:    "destroy/unowned-devices: authorizing VMs never authorizes devices",
		args:    []string{"destroy", "--stage", "clusters", "--authorize", "data-loss", "--dry-run", "--output", "json", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		deny:    []string{"bootwright_ceph_authorize_unowned_devices=true"},
	}, {
		name:    "apply/unowned-devices: apply accepts the token and a dry-run consumes none of it",
		args:    []string{"apply", "--stage", "deps", "--clusters", safetyAdvancedCephCluster, "--reclaim-devices", "/dev/sdb", "--authorize", "data-loss,unowned-devices", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"--authorize unowned-devices is not consumed by a dry-run"},
		deny:    []string{"bootwright_ceph_authorize_unowned_devices=true"},
	}, {
		name:    "apply/foreign-daemons: apply accepts the token and a dry-run consumes none of it",
		args:    []string{"apply", "--stage", "base", "--clusters", safetyAdvancedCephCluster, "--authorize", "foreign-daemons", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"--authorize foreign-daemons is not consumed by a dry-run"},
		deny:    []string{"bootwright_ceph_authorize_foreign_daemons=true"},
	}, {
		name:    "destroy/foreign-daemons: an apply-only token is a usage error naming what clears it on destroy",
		args:    []string{"destroy", "--stage", "clusters", "--authorize", "foreign-daemons", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"is not a risk destroy can authorize", "cephadm rm-cluster --force --fsid"},
	}, {
		name:    "destroy/unreachable-nodes: the token alone arms the skip, with no second flag",
		args:    []string{"destroy", "--stage", "infra", "--authorize", "unreachable-nodes", "--dry-run", "--output", "json", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"bootwright_destroy_skip_unreachable=true"},
	}, {
		name:    "destroy/stale-input: a retired field in the stored input refuses and names its token",
		seed:    seedRetiredFieldInStoredInput,
		args:    []string{"destroy", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"no longer decode or validate against this build", "bootwright context update", "--authorize stale-input"},
	}, {
		name:    "destroy/stale-input: a retired kind in the stored input refuses and names its token",
		seed:    seedRetiredKindInStoredInput,
		args:    []string{"destroy", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"unsupported kind", "--authorize stale-input"},
	}, {
		name:    "destroy/stale-input: the token clears the refusal and discloses the skipped document",
		seed:    seedRetiredKindInStoredInput,
		args:    []string{"destroy", "--authorize", "stale-input,data-loss", "--yes", "--ask-become-pass=false"},
		verdict: verdictAuthorized,
		want:    []string{"Skipped input documents", "is NOT in the teardown work set"},
	}, {
		name:    "destroy/stale-input: a dry run refuses the stale input too, so whole-input validation still holds",
		seed:    seedRetiredKindInStoredInput,
		args:    []string{"destroy", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"--authorize stale-input"},
	}, {
		name:    "destroy/stale-input: the token lets a dry run preview the blast radius before a real run",
		seed:    seedRetiredKindInStoredInput,
		args:    []string{"destroy", "--dry-run", "--authorize", "stale-input", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"Skipped input documents", "is NOT in the teardown work set"},
	}, {
		name:    "apply/stale-input is a destroy-only token and apply refuses it by name",
		args:    []string{"apply", "--authorize", "stale-input", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"stale-input", "context update"},
	}, {
		name:    "destroy/an undecodable run lease refuses before the confirmation prompt, naming the file",
		seed:    seedUndecodableRunLease,
		args:    []string{"destroy", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"current.lease.json", "rm -f"},
		deny:    []string{"Continue with destroy?", "Confirm this DESTRUCTIVE action"},
	}, {
		name:    "destroy/stale-input: a clean input reports the token had no effect",
		args:    []string{"destroy", "--stage", "clusters", "--authorize", "stale-input", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"--authorize stale-input had no effect"},
	}}
}

func seedUndecodableRunLease(t *testing.T, ctx workspace.Context) {
	t.Helper()
	if err := os.MkdirAll(ctx.RunsDir, 0o700); err != nil {
		t.Fatalf("create runs dir: %v", err)
	}
	if err := os.WriteFile(workflow.LeasePath(ctx.RunsDir), []byte("{ truncated"), 0o600); err != nil {
		t.Fatalf("write truncated run lease: %v", err)
	}
}

func seedRetiredFieldInStoredInput(t *testing.T, ctx workspace.Context) {
	t.Helper()
	appendStoredInputDocument(t, ctx, "apiVersion: "+v1alpha1.APIVersion+"\nkind: "+v1alpha1.KindMachineImage+"\nmetadata:\n  name: stale-image\nspec:\n  retiredFieldFromAnOlderSchema: true\n")
}

func seedRetiredKindInStoredInput(t *testing.T, ctx workspace.Context) {
	t.Helper()
	appendStoredInputDocument(t, ctx, "apiVersion: "+v1alpha1.APIVersion+"\nkind: Playbook\nmetadata:\n  name: retired-kind\nspec:\n  path: ./nope.yaml\n")
}

func appendStoredInputDocument(t *testing.T, ctx workspace.Context, body string) {
	t.Helper()
	files, err := desiredstate.LoadedInputFiles(ctx.InputPaths)
	if err != nil {
		t.Fatalf("resolve selected input files: %v", err)
	}
	target := ""
	for _, file := range files {
		if filepath.Base(file) != "environment.yaml" {
			target = file
			break
		}
	}
	if target == "" {
		t.Fatalf("no non-Environment input file among %v", files)
	}
	existing, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read %s: %v", target, err)
	}
	if err := os.WriteFile(target, append(append(existing, []byte("\n---\n")...), []byte(body)...), 0o600); err != nil {
		t.Fatalf("append stale input document to %s: %v", target, err)
	}
}

func safetyStorageDataLossCases() []safetyCase {
	return []safetyCase{{
		name:     "destroy/data-loss: an infra-stage teardown of a provider-backed Ceph cluster refuses under --yes alone",
		baseline: safetyBaselineVirtualCeph,
		args:     []string{"destroy", "--stage", "infra", "--authorize", "protected", "--yes", "--ask-become-pass=false"},
		verdict:  verdictRefusal,
		want:     []string{safetyVirtualCephCluster, "--yes does not authorize data loss", "--authorize data-loss"},
	}, {
		name:     "destroy/data-loss: an infra-stage teardown of a provider-backed Ceph cluster prompts for data loss interactively",
		baseline: safetyBaselineVirtualCeph,
		args:     []string{"destroy", "--stage", "infra", "--authorize", "protected", "--ask-become-pass=false"},
		verdict:  verdictPrompted,
		want:     []string{"Confirm this DESTRUCTIVE action (accept data loss)?"},
	}, {
		name:     "destroy/data-loss: the token clears the infra-stage refusal",
		baseline: safetyBaselineVirtualCeph,
		args:     []string{"destroy", "--stage", "infra", "--authorize", "protected,data-loss", "--ask-become-pass=false"},
		verdict:  verdictPrompted,
		want:     []string{"Continue with destroy?"},
		deny:     []string{"--yes does not authorize data loss"},
	}, {
		name:     "destroy/data-loss: a machine-scoped teardown of a provider-backed OSD host refuses under --yes alone",
		baseline: safetyBaselineVirtualCeph,
		args:     []string{"destroy", "--machines", safetyVirtualCephOSDNode, "--authorize", "protected,installed-cluster-node", "--yes", "--ask-become-pass=false"},
		verdict:  verdictRefusal,
		want:     []string{safetyVirtualCephCluster, "--authorize data-loss"},
	}, {
		name:    "destroy/data-loss: retained bare-metal OSD hosts keep the infra teardown out of the data-loss gate",
		args:    []string{"destroy", "--stage", "infra", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"their disks are not wiped here"},
		deny:    []string{"ALL OSD DATA"},
	}, {
		name: "destroy/installed-cluster-node: a node of a provisioned StorageCluster refuses and names its token",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedStorageOwnership(t, ctx, safetyAdvancedCephCluster)
		},
		args:    []string{"destroy", "--machines", safetyAdvancedCephOSDNode, "--authorize", "shared-infra", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{safetyAdvancedCephCluster, "--authorize installed-cluster-node"},
	}, {
		name:    "apply/enabling destroy protection after a successful apply is not drift",
		seed:    seedProtectionAfterApply,
		args:    []string{"apply", "--stage", "clusters", "--clusters", safetyAdvancedContainerOCP, "--yes", "--ask-become-pass=false"},
		verdict: verdictAuthorized,
		deny:    []string{"not a safe in-place reconcile", "would reinstall the cluster"},
	}, {
		name:    "apply/re-applying the identical desired state reports no drift at all",
		seed:    seedSuccessfulApply,
		args:    []string{"diff", "--recorded"},
		verdict: verdictAccepted,
		deny:    []string{"drift", "missing"},
	}, {
		name:    "apply/a scoped diff --recorded after a whole-fleet apply reports no drift (scope-invariant hashes)",
		seed:    seedSuccessfulApply,
		args:    []string{"diff", "--recorded", "--clusters", safetyAdvancedContainerOCP},
		verdict: verdictAccepted,
		deny:    []string{"drift"},
	}, {
		name:    "apply/enabling destroy protection after a successful apply keeps diff --recorded in sync",
		seed:    seedProtectionAfterApply,
		args:    []string{"diff", "--recorded"},
		verdict: verdictAccepted,
		deny:    []string{"drift"},
	}}
}

func safetyScopeClosureCases() []safetyCase {
	return []safetyCase{{
		name:    "apply/dry-run/greenfield/full graph is a read-only preview",
		args:    []string{"apply", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		deny:    []string{"DESTROY"},
	}, {
		name:    "plan/greenfield/one DC previews without mutating",
		args:    []string{"plan", "--clusters", "dc1-metal-ocp,dc1-child-ocp", "--ask-become-pass=false"},
		verdict: verdictAccepted,
	}, {
		name:    "diff/recorded/greenfield/full reports never-applied and writes no records",
		args:    []string{"diff", "--recorded"},
		verdict: verdictOutOfSync,
		want:    []string{"ceph-storage", "never applied"},
	}, {
		name:    "destroy/dry-run/greenfield/one DC leaves the stretched Ceph out of the work set",
		args:    []string{"destroy", "--stage", "clusters", "--clusters", "dc1-metal-ocp,dc1-child-ocp", "--dry-run", "--output", "json", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"bootwright_destroy_storage_scope="},
		deny:    []string{"bootwright_destroy_storage_scope=ceph-storage"},
	}, {
		name:    "destroy/stretched Ceph alone while both DCs still consume it",
		args:    []string{"destroy", "--stage", "clusters", "--clusters", "ceph-storage", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"ceph-storage", "dc1-metal-ocp", "dc2-metal-ocp"},
	}, {
		name: "destroy/host cluster while its nested guest is installed",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedInstalledCluster(t, ctx, "dc1-child-ocp")
		},
		args:    []string{"destroy", "--clusters", "dc1-metal-ocp", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"dc1-child-ocp", "no --authorize token widens"},
	}, {
		name: "destroy/machines of a host cluster while its nested guest is installed",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedInstalledCluster(t, ctx, "dc1-child-ocp")
		},
		args:    []string{"destroy", "--machines", "dc1-metal-master-0", "--authorize", "installed-cluster-node", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"dc1-child-ocp"},
	}}
}

func safetyStartingStateCases() []safetyCase {
	return []safetyCase{{
		name: "apply/mode create over an existing install record",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedInstalledCluster(t, ctx, "dc1-metal-ocp")
		},
		args:    []string{"apply", "--stage", "clusters", "--clusters", "dc1-metal-ocp", "--mode", "create", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"dc1-metal-ocp"},
	}, {
		name: "apply/cluster-wide substrate release authorizes and discloses the rebuild",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedKubeVirtReadyHost(t, ctx, "dc1-metal-ocp")
			if err := workflow.MarkSubstrateReleased(ctx.RunsDir, "dc1-child-ocp", time.Now()); err != nil {
				t.Fatalf("MarkSubstrateReleased: %v", err)
			}
		},
		args:    []string{"apply", "--stage", "infra", "--clusters", "dc1-child-ocp", "--yes", "--ask-become-pass=false"},
		verdict: verdictAuthorized,
		want:    []string{"destroyed substrate", "dc1-child-ocp", "re-creates the released machines"},
	}, {
		name: "apply/machine-granular substrate release names only the released machine",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedKubeVirtReadyHost(t, ctx, "dc1-metal-ocp")
			if err := workflow.MarkSubstrateMachinesReleased(ctx.RunsDir, "dc1-child-ocp", []string{"dc1-child-ocp-infra-master-0"}, time.Now()); err != nil {
				t.Fatalf("MarkSubstrateMachinesReleased: %v", err)
			}
		},
		args:    []string{"apply", "--machines", "dc1-child-ocp-infra-master-0", "--yes", "--ask-become-pass=false"},
		verdict: verdictAuthorized,
		want:    []string{"destroyed substrate", "machine(s) dc1-child-ocp-infra-master-0"},
		deny:    []string{"dc1-child-ocp-infra-master-1"},
	}, {
		name: "apply/no release leaves an unreleased machine unauthorized for rebuild",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedKubeVirtReadyHost(t, ctx, "dc1-metal-ocp")
		},
		args:    []string{"apply", "--machines", "dc1-child-ocp-infra-master-0", "--yes", "--ask-become-pass=false"},
		verdict: verdictAuthorized,
		deny:    []string{"destroyed substrate"},
	}, {
		name: "apply/renamed ContainerCluster orphaning a provisioned one",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedInstalledCluster(t, ctx, "dc1-metal-ocp-old")
		},
		args:    []string{"apply", "--stage", "clusters", "--clusters", "dc1-metal-ocp", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"dc1-metal-ocp-old", "the signature of a rename"},
	}, {
		name: "apply/renamed StorageCluster orphaning an owned Ceph cluster",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedStorageOwnership(t, ctx, "ceph-storage-old")
		},
		args:    []string{"apply", "--stage", "clusters", "--clusters", "ceph-storage", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"ceph-storage-old", "orphan the old Ceph cluster"},
	}}
}

const (
	safetyBaselineAdvanced     = "baremetal-redfish-multidc-virtualized-odf-ceph"
	safetyBaselineVirtualCeph  = "ceph-ibm-libvirt-lab"
	safetyVirtualCephCluster   = "ceph-ibm"
	safetyVirtualCephOSDNode   = "ceph-1"
	safetyAdvancedCephCluster  = "ceph-storage"
	safetyAdvancedCephOSDNode  = "ceph-dc1-0"
	safetyAdvancedContainerOCP = "dc1-metal-ocp"
)

func initSafetyBaselineContext(t *testing.T, example string) workspace.Context {
	t.Helper()
	if example == "" {
		example = safetyBaselineAdvanced
	}
	setTestHomeAndRoot(t)
	dir := filepath.Join("..", "..", "examples", example)
	if stdout, stderr, code := runCLI(t, "context", "init", "--name", "matrix", "-f", dir); code != 0 {
		t.Fatalf("context init exited %d, stdout=%q stderr=%q", code, stdout, stderr)
	}
	ctx, err := workspace.ResolveExistingContext("matrix")
	if err != nil {
		t.Fatal(err)
	}
	return ctx
}

func seedSuccessfulApply(t *testing.T, ctx workspace.Context) {
	t.Helper()
	state, err := desiredstate.LoadNormalizeValidateInputFiles(ctx.InputPaths)
	if err != nil {
		t.Fatalf("load desired state: %v", err)
	}
	tasks, err := workflow.PlanApplyTasksChecked(converge.AllScope.ApplyTarget(), state)
	if err != nil {
		t.Fatalf("plan apply tasks: %v", err)
	}
	now := time.Now().UTC()
	for _, task := range tasks {
		if err := workflow.MarkApplyTaskConvergeSafety(ctx.RunsDir, ctx.Name, "seeded-run", task, workflow.ConvergeSafetyStatusReconciled, now); err != nil {
			t.Fatalf("MarkApplyTaskConvergeSafety(%s): %v", task.Entry.ID, err)
		}
	}
	for _, cluster := range state.StorageClusters {
		if err := workflow.MarkStorageSubObjectsConvergeSafety(ctx.RunsDir, ctx.Name, "seeded-run", state, cluster.Metadata.Name, workflow.ConvergeSafetyStatusReconciled, now); err != nil {
			t.Fatalf("MarkStorageSubObjectsConvergeSafety(%s): %v", cluster.Metadata.Name, err)
		}
	}
}

func seedProtectionAfterApply(t *testing.T, ctx workspace.Context) {
	t.Helper()
	seedSuccessfulApply(t, ctx)
	seedProtectedEnvironment(t, ctx)
}

func assertNoRuntimeRecords(t *testing.T, ctx workspace.Context) {
	t.Helper()
	if workflow.HasConvergeSafetyRecords(ctx.RunsDir) {
		t.Error("a read-only command must not write convergence-safety records")
	}
	for _, dir := range []string{"substrate-release", "current"} {
		if _, err := os.Stat(filepath.Join(ctx.RunsDir, dir)); err == nil {
			t.Errorf("a read-only command must not write runs/%s", dir)
		}
	}
}

func seedInstalledCluster(t *testing.T, ctx workspace.Context, cluster string) {
	t.Helper()
	now := time.Now().UTC()
	if err := workflow.SaveClusterInstallRecord(ctx.ClustersDir, workflow.ClusterInstallRecord{
		Cluster:     cluster,
		DesiredHash: "sha256:seeded",
		HashSchema:  workflow.ConvergeHashSchema,
		Status:      workflow.ClusterInstallStatusInstalled,
		Phase:       workflow.ClusterInstallPhaseComplete,
		UpdatedAt:   now,
		InstalledAt: &now,
	}); err != nil {
		t.Fatalf("SaveClusterInstallRecord(%s): %v", cluster, err)
	}
	kubeconfig := filepath.Join(ctx.ClustersDir, cluster, "secrets", "kubeconfig")
	if err := os.MkdirAll(filepath.Dir(kubeconfig), 0o700); err != nil {
		t.Fatalf("mkdir kubeconfig dir: %v", err)
	}
	if err := os.WriteFile(kubeconfig, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}
}

func seedKubeVirtReadyHost(t *testing.T, ctx workspace.Context, cluster string) {
	t.Helper()
	seedInstalledCluster(t, ctx, cluster)
	if err := extensionrecords.SaveRecord(ctx.ClustersDir, extensionrecords.Record{
		Cluster:   cluster,
		Extension: "openshift-virtualization",
		Status:    extensionrecords.RecordStatusReady,
		Phase:     extensionrecords.RecordPhaseComplete,
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveRecord(%s): %v", cluster, err)
	}
}

func seedStorageOwnership(t *testing.T, ctx workspace.Context, cluster string) {
	t.Helper()
	if err := ownership.SaveResource(ctx.OwnershipDir, ownership.ResourceRecord{
		Kind:      string(ownership.KindStorageCluster),
		Name:      cluster,
		Owner:     ownership.Owner,
		Context:   ctx.Name,
		Cluster:   cluster,
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("SaveResource(%s): %v", cluster, err)
	}
}

func seedProtectedEnvironment(t *testing.T, ctx workspace.Context) {
	t.Helper()
	path := filepath.Join(ctx.InputDir, "environment.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read context environment: %v", err)
	}
	body := strings.Replace(string(data),
		"  domains:",
		"  safety:\n    destroyProtection: "+v1alpha1.EnvironmentDestroyProtectionProtected+"\n  domains:", 1)
	if body == string(data) {
		t.Fatal("context environment did not contain spec.domains")
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write protected environment: %v", err)
	}
}

func seedUnreadableOwnershipRecord(t *testing.T, ctx workspace.Context) {
	t.Helper()
	seedStorageOwnership(t, ctx, "ceph-storage")
	corrupt := filepath.Join(ctx.OwnershipDir, ownership.ResourceDirName, "corrupt.json")
	if err := os.MkdirAll(filepath.Dir(corrupt), 0o700); err != nil {
		t.Fatalf("mkdir ownership resources dir: %v", err)
	}
	if err := os.WriteFile(corrupt, []byte("{ not json"), 0o600); err != nil {
		t.Fatalf("write corrupt ownership record: %v", err)
	}
}
