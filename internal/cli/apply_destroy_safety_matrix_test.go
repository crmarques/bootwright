package cli

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/api/v1alpha1"
	extensionrecords "github.com/crmarques/bootwright/internal/addons/records"
	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/bundle"
	"github.com/crmarques/bootwright/internal/converge/workflow"
	"github.com/crmarques/bootwright/internal/ownership"
	"github.com/crmarques/bootwright/internal/preflight"
	secret "github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/sshtrust"
	desiredstate "github.com/crmarques/bootwright/internal/state/desired"
	"github.com/crmarques/bootwright/internal/workspace"
)

type safetyVerdict string
type remedyExpectation string

const (
	verdictUsageError  safetyVerdict = "usage-error"
	verdictRefusal     safetyVerdict = "fail-closed refusal"
	verdictAccepted    safetyVerdict = "accepted (no gate refusal)"
	verdictOutOfSync   safetyVerdict = "read-only out-of-sync report"
	verdictGateCleared safetyVerdict = "target safety gate cleared before downstream validation"
	verdictAuthorized  safetyVerdict = "authorized mutation (no gate refusal)"
	verdictNoChange    safetyVerdict = "successful idempotent no-op"
	verdictPrompted    safetyVerdict = "held at the interactive confirmation"
)

const (
	remedySameSelection remedyExpectation = "same-selection"
	remedyAlternative   remedyExpectation = "intentional-alternative"
	remedyExternal      remedyExpectation = "external-recovery"
)

var gateRefusalMarkers = []string{"refusing to", "refuses to", "fails closed", "does not authorize data loss"}

type safetyCase struct {
	name     string
	baseline string
	seed     func(t *testing.T, ctx workspace.Context)
	check    func(t *testing.T, ctx workspace.Context)
	args     []string
	verdict  safetyVerdict
	remedy   remedyExpectation
	want     []string
	deny     []string
}

type safetyClusterAvailabilityChecker struct{}

func (safetyClusterAvailabilityChecker) Available(context.Context, string) (bool, error) {
	return true, nil
}

func TestApplyDestroySafetyMatrix(t *testing.T) {
	for _, tc := range safetyMatrixCases() {
		t.Run(tc.name, func(t *testing.T) {
			ctx := initSafetyBaselineContext(t, tc.baseline)
			if tc.seed != nil {
				tc.seed(t, ctx)
			}
			before := safetyDurableStateSnapshot(t, ctx)
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
				assertRefusalRemedy(t, tc.args, out, tc.remedy)
			case verdictAccepted:
				if code != 0 {
					t.Fatalf("want the run accepted past the safety gates (exit 0), got exit %d\n%s", code, out)
				}
			case verdictGateCleared:
				if code == 2 {
					t.Fatalf("want the target safety gate cleared before downstream validation, got usage exit 2\n%s", out)
				}
				for _, marker := range gateRefusalMarkers {
					if strings.Contains(out, marker) {
						t.Fatalf("the target safety gate was not cleared (%q):\n%s", marker, out)
					}
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
				if code != 0 {
					t.Fatalf("want an authorized successful mutation (exit 0), got exit %d\n%s", code, out)
				}
				for _, marker := range gateRefusalMarkers {
					if strings.Contains(out, marker) {
						t.Fatalf("run is authorized by its records and flags, but a safety gate refused it (%q):\n%s", marker, out)
					}
				}
			case verdictNoChange:
				if code != 0 {
					t.Fatalf("want a successful idempotent no-op (exit 0), got exit %d\n%s", code, out)
				}
				for _, marker := range gateRefusalMarkers {
					if strings.Contains(out, marker) {
						t.Fatalf("idempotent reapply was refused (%q):\n%s", marker, out)
					}
				}
			}
			after := safetyDurableStateSnapshot(t, ctx)
			switch tc.verdict {
			case verdictAuthorized:
				if maps.Equal(before, after) {
					t.Fatal("authorized mutation returned success without changing any context state or evidence")
				}
			case verdictNoChange:
				for _, path := range safetySnapshotDelta(before, after) {
					if !strings.HasPrefix(path, "runs/safety/") {
						t.Fatalf("idempotent apply changed durable target or lifecycle state at %s; only convergence evidence may be refreshed", path)
					}
				}
			case verdictUsageError, verdictRefusal, verdictAccepted, verdictGateCleared, verdictOutOfSync, verdictPrompted:
				if !maps.Equal(before, after) {
					t.Fatalf("%s changed desired, ownership, install, provider, convergence, or release state at %s; usage errors, refusals, prompts, diff, plan, and dry-runs may retain audit artifacts but must not change durable state", tc.verdict, strings.Join(safetySnapshotDelta(before, after), ", "))
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
			if tc.check != nil {
				tc.check(t, ctx)
			}
		})
	}
}

func safetyDurableStateSnapshot(t *testing.T, ctx workspace.Context) map[string]string {
	t.Helper()
	snapshot := map[string]string{}
	roots := []string{
		ctx.InputDir,
		ctx.SecretsDir,
		ctx.ManagedServicesDir,
		ctx.ProviderStateDir,
		ctx.OwnershipDir,
		filepath.Join(ctx.RunsDir, "safety"),
		filepath.Join(ctx.RunsDir, "substrate-release"),
	}
	clusters, err := os.ReadDir(ctx.ClustersDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("list cluster durable state: %v", err)
	}
	for _, cluster := range clusters {
		if !cluster.IsDir() {
			continue
		}
		base := filepath.Join(ctx.ClustersDir, cluster.Name())
		roots = append(roots,
			filepath.Join(base, "secrets"),
			filepath.Join(base, "runtime", workflow.ClusterInstallRecordFileName),
			filepath.Join(base, "runtime", workflow.ClusterConnectionFileName),
			filepath.Join(base, "runtime", "provider-state"),
			filepath.Join(base, "runtime", extensionrecords.RecordRelativeDir),
		)
	}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(ctx.BaseDir, path)
			if err != nil {
				return err
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			key := filepath.ToSlash(rel)
			switch {
			case info.Mode()&os.ModeSymlink != 0:
				target, err := os.Readlink(path)
				if err != nil {
					return err
				}
				snapshot[key] = info.Mode().String() + "\x00" + target
			case info.Mode().IsRegular():
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				digest := sha256.Sum256(data)
				snapshot[key] = fmt.Sprintf("%s\x00%x", info.Mode().String(), digest)
			}
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			t.Fatalf("snapshot durable context state: %v", err)
		}
	}
	return snapshot
}

func safetySnapshotDelta(before, after map[string]string) []string {
	var paths []string
	for path, value := range before {
		if after[path] != value {
			paths = append(paths, path)
		}
	}
	for path := range after {
		if _, found := before[path]; !found {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	return paths
}

func assertRefusalRemedy(t *testing.T, args []string, output string, expectation remedyExpectation) {
	t.Helper()
	commands := backtickedBootwrightCommands(output)
	if expectation == remedyExternal {
		if len(commands) > 0 {
			t.Fatalf("a command-free external recovery must not format a non-remedy Bootwright reference as an executable retry: %v\n%s", commands, output)
		}
		if !strings.Contains(output, "no bootwright retry command") {
			t.Fatalf("a command-free external recovery must state that no Bootwright retry performs the unsafe change:\n%s", output)
		}
		return
	}
	if len(commands) == 0 {
		t.Fatalf("a refusal must name a backticked exact Bootwright command, got:\n%s", output)
	}
	required := map[string]string{"--context": "matrix"}
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--context", "--clusters", "--machines":
			if i+1 < len(args) {
				required[args[i]] = args[i+1]
				i++
			}
		default:
			for _, name := range []string{"--context", "--clusters", "--machines"} {
				if value, found := strings.CutPrefix(args[i], name+"="); found {
					required[name] = value
				}
			}
		}
	}
	if expectation == remedyAlternative {
		delete(required, "--clusters")
		delete(required, "--machines")
	}
	for _, command := range commands {
		if !isTargetMutatingCommand(command) {
			continue
		}
		if !commandHasFlagValue(command, "--context", required["--context"]) {
			t.Fatalf("every executable remedy must preserve the resolved context; command=%q\n%s", command, output)
		}
		if slices.Contains(args, "--dry-run") && !strings.Contains(command, "--dry-run") {
			t.Fatalf("an alternative suggested by a dry-run must remain read-only; command=%q\n%s", command, output)
		}
	}
	if expectation == remedyAlternative {
		return
	}
	for _, command := range commands {
		matches := true
		for flag, value := range required {
			if !commandHasFlagValue(command, flag, value) {
				matches = false
				break
			}
		}
		if matches {
			return
		}
	}
	t.Fatalf("refusal retry widens or changes the resolved selection: no one exact command preserves %v; commands=%v\n%s", required, commands, output)
}

func isTargetMutatingCommand(command string) bool {
	return strings.HasPrefix(command, "bootwright apply ") || strings.HasPrefix(command, "bootwright destroy ") || strings.HasPrefix(command, "bootwright storage-cluster replace-arbiter ")
}

func commandHasFlagValue(command, flag, value string) bool {
	return strings.Contains(command, flag+" "+value) || strings.Contains(command, flag+"='"+value+"'") || strings.Contains(command, flag+" '"+value+"'")
}

func backtickedBootwrightCommands(output string) []string {
	parts := strings.Split(output, "`")
	var commands []string
	for i := 1; i < len(parts); i += 2 {
		command := strings.TrimSpace(parts[i])
		if strings.HasPrefix(command, "bootwright ") {
			commands = append(commands, command)
		}
	}
	return commands
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
		name: "apply/preview: machine release on installed cluster forecasts whole-cluster refusal",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedKubeVirtReadyHost(t, ctx, "dc1-metal-ocp")
			seedInstalledCluster(t, ctx, "dc1-child-ocp")
			if err := workflow.MarkSubstrateMachinesReleased(ctx.RunsDir, "dc1-child-ocp", []string{"dc1-child-ocp-infra-master-0"}, time.Now()); err != nil {
				t.Fatalf("MarkSubstrateMachinesReleased: %v", err)
			}
		},
		args:    []string{"apply", "--machines", "dc1-child-ocp-infra-master-0", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"a real run refuses", "dc1-child-ocp-infra-master-0", "bootwright destroy --clusters dc1-child-ocp --dry-run", "bootwright apply --mode reconcile --authorize data-loss --clusters dc1-child-ocp --dry-run"},
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
		name:    "destroy/preview: a machine-scoped teardown previews its machines, not the whole context",
		args:    []string{"destroy", "--machines", "dc1-metal-master-0", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"Will destroy", "dc1-metal-master-0 (Machine)", "left standing"},
		deny:    []string{"(ContainerCluster)", "(StorageCluster)"},
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
		remedy:  remedyAlternative,
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
		remedy:  remedyAlternative,
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
		name:    "apply/persistent context and SSH identity controls are part of the mutating flag matrix",
		args:    []string{"apply", "--context", "matrix", "--ssh-user", "operator", "--ssh-user-for-provisioned", "--ssh-ask-sudo-password=false", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"plan only"},
	}, {
		name:    "apply/persistent SSH identity file is validated before mutation",
		args:    []string{"apply", "--ssh-id-file", "/bootwright-matrix-missing-identity", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"--ssh-id-file", "no such file"},
	}, {
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
		remedy:  remedyAlternative,
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
		want:    []string{"re-run `bootwright apply --mode rebuild --authorize data-loss"},
		deny:    []string{"bootwright storage-cluster replace-arbiter --name"},
	}, {
		name: "replace-arbiter/refreshing the storage records leaves the next apply no drift to refuse",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedSuccessfulApply(t, ctx)
			seedArbiterConvergeRecordRefresh(t, ctx, seedRetargetedTiebreaker)
			seedRunnableSafetyMutation(t, ctx)
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
		want:    []string{"re-run `bootwright apply --mode rebuild --authorize data-loss"},
	}, {
		name:    "apply/adding an arbiter to a built cluster is refused as a day-1 shape, naming no command that refuses",
		seed:    seedEnabledStretchArbiter,
		args:    []string{"apply", "--stage", "clusters", "--clusters", safetyAdvancedCephCluster, "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		remedy:  remedyExternal,
		want:    []string{"fixed when the cluster is bootstrapped", "Bootwright has no path that moves a running cluster between the two shapes"},
		deny:    []string{"bootwright storage-cluster replace-arbiter --name", "re-run with `bootwright apply --mode rebuild`"},
	}, {
		name:    "apply/removing an arbiter from a built cluster is refused without naming a rebuild remedy",
		seed:    seedDisabledStretchArbiter,
		args:    []string{"apply", "--stage", "clusters", "--clusters", safetyAdvancedCephCluster, "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		remedy:  remedyExternal,
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
		name:    "replace-arbiter/output json without dry-run is a usage error before live discovery",
		args:    []string{"storage-cluster", "replace-arbiter", "--name", safetyAdvancedCephCluster, "--output", "json", "--yes", "--ask-become-pass=false"},
		verdict: verdictUsageError,
		want:    []string{"--output json is supported only with --dry-run"},
	}, {
		name:    "replace-arbiter/JSON preview carries every required authorization without mutation",
		seed:    seedLiveStretchClusterOffItsArbiter,
		args:    []string{"storage-cluster", "replace-arbiter", "--name", safetyAdvancedCephCluster, "--dry-run", "--output", "json", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{`"requiredAuthorizations"`, `"token": "` + authorizeSameSiteArbiter + `"`, `"token": "` + authorizeDegradedQuorum + `"`, `"token": "` + authorizeUnreachableNodes + `"`},
		deny:    []string{"Bootwright:", "Required authorizations"},
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
		want:    []string{"refusing to move the stretch tiebreaker", "--authorize " + authorizeSameSiteArbiter + "," + authorizeDegradedQuorum},
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
		verdict: verdictGateCleared,
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
		name: "destroy/unreadable-records: the token clears the refusal and still discloses the skipped record",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedUnreadableOwnershipRecord(t, ctx)
			seedSafetyWorkflowBundle(t)
		},
		args:    []string{"destroy", "--stage", "infra", "--clusters", "dc1-metal-ocp", "--authorize", "unreadable-records", "--yes", "--ask-become-pass=false"},
		verdict: verdictGateCleared,
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
		name:    "apply/unowned-devices: with no ownership record the token is what lets the reclaim act",
		args:    []string{"apply", "--through", "base", "--clusters", safetyAdvancedCephCluster, "--reclaim-devices", "/dev/sdb", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"--authorize " + authorizeUnownedDevices + ": " + authorizationRequired, "the state a successful destroy leaves"},
	}, {
		name:    "apply/unowned-devices: the token pair forecasts the post-destroy reclaim wipe",
		args:    []string{"apply", "--through", "base", "--clusters", safetyAdvancedCephCluster, "--reclaim-devices", "/dev/sdb", "--authorize", "data-loss,unowned-devices", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictAccepted,
		want:    []string{"reclaim-devices /dev/sdb on Ceph cluster(s) " + safetyAdvancedCephCluster},
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
		name: "destroy/stale-input: the token clears the refusal and discloses the skipped document",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedRetiredKindInStoredInput(t, ctx)
			seedSafetyWorkflowBundle(t)
		},
		args:    []string{"destroy", "--authorize", "stale-input,data-loss", "--yes", "--ask-become-pass=false"},
		verdict: verdictAuthorized,
		want:    []string{"Skipped input documents", "is NOT in the teardown work set"},
	}, {
		name:    "destroy/stale-input: a dry run refuses the stale input too, so whole-input validation still holds",
		seed:    seedRetiredKindInStoredInput,
		args:    []string{"destroy", "--dry-run", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"--authorize stale-input", "re-run `bootwright destroy --authorize stale-input --dry-run", "--context matrix"},
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
		name: "apply/enabling destroy protection after a successful apply is not drift",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedProtectionAfterApply(t, ctx)
			seedRunnableSafetyMutation(t, ctx)
			seedMatchingInstalledCluster(t, ctx, safetyAdvancedContainerOCP)
		},
		args:    []string{"apply", "--stage", "deps", "--clusters", safetyAdvancedContainerOCP, "--yes", "--ask-become-pass=false"},
		verdict: verdictNoChange,
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
		remedy:  remedyAlternative,
		want:    []string{"ceph-storage", "dc1-metal-ocp", "dc2-metal-ocp"},
	}, {
		name: "destroy/host cluster while its nested guest is installed",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedInstalledCluster(t, ctx, "dc1-child-ocp")
		},
		args:    []string{"destroy", "--clusters", "dc1-metal-ocp", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		remedy:  remedyAlternative,
		want:    []string{"dc1-child-ocp", "no --authorize token widens"},
	}, {
		name: "destroy/machines of a host cluster while its nested guest is installed",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedInstalledCluster(t, ctx, "dc1-child-ocp")
		},
		args:    []string{"destroy", "--machines", "dc1-metal-master-0", "--authorize", "installed-cluster-node", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		remedy:  remedyAlternative,
		want:    []string{"dc1-child-ocp"},
	}}
}

func safetyStartingStateCases() []safetyCase {
	return []safetyCase{{
		name: "apply/mode create over an existing install record",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedInstalledCluster(t, ctx, "dc1-metal-ocp")
			seedRunnableSafetyMutation(t, ctx)
		},
		args:    []string{"apply", "--stage", "clusters", "--clusters", "dc1-metal-ocp", "--mode", "create", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"dc1-metal-ocp"},
	}, {
		name: "apply/preboot installer skew names ISO regeneration and exact resume",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedRunnableSafetyMutation(t, ctx)
			seedClusterInstallLifecycle(t, ctx, safetyAdvancedContainerOCP, workflow.ClusterInstallStatusInstalling, workflow.ClusterInstallPhaseISOCreated, "4.20.0", time.Now().Add(-time.Hour))
		},
		args:    []string{"apply", "--stage", "clusters", "--clusters", safetyAdvancedContainerOCP, "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"regenerate only this cluster's agent ISO", "--mode rebuild", "--stage deps", "--clusters " + safetyAdvancedContainerOCP, "then resume the original selected work", "--context matrix"},
	}, {
		name: "apply/expired postboot resume names scoped destroy and reapply sequence",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedRunnableSafetyMutation(t, ctx)
			seedClusterInstallLifecycle(t, ctx, safetyAdvancedContainerOCP, workflow.ClusterInstallStatusFailed, workflow.ClusterInstallPhaseNodesBooted, safetyDeclaredInstallerVersion(t, ctx, safetyAdvancedContainerOCP), time.Now().Add(-workflow.ClusterInstallResumeCeiling-time.Minute))
		},
		args:    []string{"apply", "--stage", "clusters", "--clusters", safetyAdvancedContainerOCP, "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"deliberately reset only this cluster's incomplete install", "bootwright destroy --authorize protected,data-loss", "bootwright apply --mode reconcile --authorize data-loss", "--stage clusters", "--clusters " + safetyAdvancedContainerOCP, "--context matrix"},
	}, {
		name: "apply/completed installer skew names scoped data-loss rebuild",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedRunnableSafetyMutation(t, ctx)
			seedClusterInstallLifecycle(t, ctx, safetyAdvancedContainerOCP, workflow.ClusterInstallStatusInstalled, workflow.ClusterInstallPhaseComplete, "4.20.0", time.Now().Add(-time.Hour))
			seedClusterKubeconfig(t, ctx.Name, ctx.ClustersDir, safetyAdvancedContainerOCP)
		},
		args:    []string{"apply", "--stage", "clusters", "--clusters", safetyAdvancedContainerOCP, "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"rebuild only this installed cluster", "bootwright apply --mode rebuild --authorize data-loss", "--stage clusters", "--clusters " + safetyAdvancedContainerOCP, "--context matrix"},
	}, {
		name: "apply/uncertain node boot phase names an exact scoped rebuild",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedRunnableSafetyMutation(t, ctx)
			seedClusterInstallLifecycle(t, ctx, safetyAdvancedContainerOCP, workflow.ClusterInstallStatusInstalling, workflow.ClusterInstallPhaseBooting, safetyDeclaredInstallerVersion(t, ctx, safetyAdvancedContainerOCP), time.Now().Add(-time.Hour))
		},
		args:    []string{"apply", "--stage", "clusters", "--clusters", safetyAdvancedContainerOCP, "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"node boot completion is uncertain", "bootwright apply --mode rebuild --authorize data-loss", "--stage clusters", "--clusters " + safetyAdvancedContainerOCP, "--context matrix"},
	}, {
		name: "apply/unrecognized install phase names an exact scoped rebuild",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedRunnableSafetyMutation(t, ctx)
			seedClusterInstallLifecycle(t, ctx, safetyAdvancedContainerOCP, workflow.ClusterInstallStatusInstalling, workflow.ClusterInstallPhase("future-phase"), safetyDeclaredInstallerVersion(t, ctx, safetyAdvancedContainerOCP), time.Now().Add(-time.Hour))
		},
		args:    []string{"apply", "--stage", "clusters", "--clusters", safetyAdvancedContainerOCP, "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"unrecognized install phase", "bootwright apply --mode rebuild --authorize data-loss", "--stage clusters", "--clusters " + safetyAdvancedContainerOCP, "--context matrix"},
	}, {
		name: "apply/cluster-wide substrate release authorizes and discloses the rebuild",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedKubeVirtReadyHost(t, ctx, "dc1-metal-ocp")
			seedRunnableSafetyMutation(t, ctx)
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
			seedRunnableSafetyMutation(t, ctx)
			if err := workflow.MarkSubstrateMachinesReleased(ctx.RunsDir, "dc1-child-ocp", []string{"dc1-child-ocp-infra-master-0"}, time.Now()); err != nil {
				t.Fatalf("MarkSubstrateMachinesReleased: %v", err)
			}
		},
		args:    []string{"apply", "--machines", "dc1-child-ocp-infra-master-0", "--yes", "--ask-become-pass=false"},
		verdict: verdictAuthorized,
		want:    []string{"destroyed substrate", "machine(s) dc1-child-ocp-infra-master-0"},
		deny:    []string{"dc1-child-ocp-infra-master-1"},
	}, {
		name: "apply/machine-granular release on installed ContainerCluster fails closed",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedKubeVirtReadyHost(t, ctx, "dc1-metal-ocp")
			seedInstalledCluster(t, ctx, "dc1-child-ocp")
			if err := workflow.MarkSubstrateMachinesReleased(ctx.RunsDir, "dc1-child-ocp", []string{"dc1-child-ocp-infra-master-0"}, time.Now()); err != nil {
				t.Fatalf("MarkSubstrateMachinesReleased: %v", err)
			}
		},
		args:    []string{"apply", "--machines", "dc1-child-ocp-infra-master-0", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		remedy:  remedyAlternative,
		want:    []string{"initial-install workflow cannot recover individual cluster nodes", "dc1-child-ocp-infra-master-0", "release remains recorded", "bootwright destroy --yes --clusters dc1-child-ocp", "bootwright apply --mode reconcile --authorize data-loss --yes --clusters dc1-child-ocp"},
		check: func(t *testing.T, ctx workspace.Context) {
			released, err := workflow.ReleasedSubstrateClusters(ctx.RunsDir)
			if err != nil || strings.Join(released, ",") != "dc1-child-ocp" {
				t.Fatalf("refused apply must preserve the machine-scoped release: released=%v err=%v", released, err)
			}
			if _, found, err := workflow.LoadRunLedger(ctx.RunsDir); err != nil || found {
				t.Fatalf("refusal must happen before a mutating run ledger is created: found=%v err=%v", found, err)
			}
		},
	}, {
		name: "apply/no release leaves an unreleased machine unauthorized for rebuild",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedKubeVirtReadyHost(t, ctx, "dc1-metal-ocp")
			seedRunnableSafetyMutation(t, ctx)
		},
		args:    []string{"apply", "--machines", "dc1-child-ocp-infra-master-0", "--yes", "--ask-become-pass=false"},
		verdict: verdictAuthorized,
		deny:    []string{"destroyed substrate"},
	}, {
		name:     "apply/a destroy-released bare-metal managed-OS machine requires the data-loss acknowledgment",
		baseline: safetyBaselineBareMetalManagedOS,
		seed: func(t *testing.T, ctx workspace.Context) {
			if err := workflow.MarkSubstrateMachinesReleased(ctx.RunsDir, safetyBareMetalManagedOSCluster, []string{safetyBareMetalManagedOSMachine}, time.Now()); err != nil {
				t.Fatalf("MarkSubstrateMachinesReleased: %v", err)
			}
		},
		args:    []string{"apply", "--machines", safetyBareMetalManagedOSMachine, "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		want:    []string{"reinstall destroy-released bare-metal machine(s) " + safetyBareMetalManagedOSMachine, "still-running OS wiped", "--authorize data-loss", "bootwright apply --authorize data-loss --yes --machines " + safetyBareMetalManagedOSMachine},
	}, {
		name:     "apply/a released libvirt machine substrate hosting an installed KubeVirt tenant refuses a host-only rebuild",
		baseline: safetyBaselineLibvirtKubeVirtHost,
		seed:     seedReleasedKubeVirtHostWithInstalledTenant,
		args:     []string{"apply", "--stage", "infra", "--clusters", safetyLibvirtKubeVirtHostCluster, "--yes", "--ask-become-pass=false"},
		verdict:  verdictRefusal,
		want:     []string{safetyLibvirtKubeVirtHostCluster, safetyLibvirtKubeVirtTenantCluster, "left out of scope", "bootwright destroy --stage clusters --clusters " + safetyLibvirtKubeVirtTenantCluster},
	}, {
		name: "apply/renamed ContainerCluster orphaning a provisioned one",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedInstalledCluster(t, ctx, "dc1-metal-ocp-old")
		},
		args:    []string{"apply", "--stage", "clusters", "--clusters", "dc1-metal-ocp", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		remedy:  remedyAlternative,
		want:    []string{"dc1-metal-ocp-old", "the signature of a rename"},
	}, {
		name: "apply/renamed StorageCluster orphaning an owned Ceph cluster",
		seed: func(t *testing.T, ctx workspace.Context) {
			seedStorageOwnership(t, ctx, "ceph-storage-old")
		},
		args:    []string{"apply", "--stage", "clusters", "--clusters", "ceph-storage", "--yes", "--ask-become-pass=false"},
		verdict: verdictRefusal,
		remedy:  remedyAlternative,
		want:    []string{"ceph-storage-old", "orphan the old Ceph cluster"},
	}}
}

const (
	safetyBaselineAdvanced             = "baremetal-redfish-multidc-virtualized-odf-ceph"
	safetyBaselineVirtualCeph          = "ceph-ibm-libvirt-lab"
	safetyBaselineBareMetalManagedOS   = "ceph-ibm-baremetal-redfish"
	safetyBaselineLibvirtKubeVirtHost  = "sno-libvirt-redfish"
	safetyVirtualCephCluster           = "ceph-ibm"
	safetyVirtualCephOSDNode           = "ceph-1"
	safetyBareMetalManagedOSCluster    = "ceph-ibm"
	safetyBareMetalManagedOSMachine    = "ceph-1"
	safetyLibvirtKubeVirtHostCluster   = "sno-libvirt"
	safetyLibvirtKubeVirtTenantCluster = "sno-kubevirt-child"
	safetyAdvancedCephCluster          = "ceph-storage"
	safetyAdvancedCephOSDNode          = "ceph-dc1-0"
	safetyAdvancedContainerOCP         = "dc1-metal-ocp"
)

func seedReleasedKubeVirtHostWithInstalledTenant(t *testing.T, ctx workspace.Context) {
	t.Helper()
	appendStoredInputDocument(t, ctx, kubeVirtHostSafetyDocuments)
	seedInstalledCluster(t, ctx, safetyLibvirtKubeVirtTenantCluster)
	if err := workflow.MarkSubstrateReleased(ctx.RunsDir, safetyLibvirtKubeVirtHostCluster, time.Now()); err != nil {
		t.Fatalf("MarkSubstrateReleased: %v", err)
	}
}

const kubeVirtHostSafetyDocuments = `apiVersion: bootwright.io/v1alpha1
kind: ClusterAddon
metadata:
  name: safety-openshift-virtualization
spec:
  type: olm
  provides:
    - kubevirt
  olm:
    namespace:
      name: openshift-cnv
      create: true
    operatorGroup:
      name: kubevirt-hyperconverged-group
      targetNamespaces:
        - openshift-cnv
    subscription:
      name: hco-operatorhub
      package: kubevirt-hyperconverged
      channel: stable
      source: redhat-operators
  readiness:
    checks:
      - csvSucceeded:
          namespace: openshift-cnv
          subscription: hco-operatorhub
---
apiVersion: bootwright.io/v1alpha1
kind: ClusterAddonBinding
metadata:
  name: sno-libvirt-safety-addons
spec:
  clusterRef: sno-libvirt
  addonRefs:
    - safety-openshift-virtualization
---
apiVersion: bootwright.io/v1alpha1
kind: InfraProvider
metadata:
  name: safety-kubevirt-provider
spec:
  type: kubevirt
  networkAttachments:
    - name: safety-child-net
      kubevirt:
        networkRef:
          apiGroup: k8s.ovn.org
          kind: ClusterUserDefinedNetwork
          name: safety-child-net
          namespace: safety-child
  kubevirt:
    namespace: safety-child
    hostClusterRef: sno-libvirt
    machineProfiles:
      - name: child
        cpu: 4
        memoryMiB: 8192
        diskGiB: 80
---
apiVersion: bootwright.io/v1alpha1
kind: NetworkConfig
metadata:
  name: safety-child-net
spec:
  machineNetwork:
    - cidr: 192.168.133.0/24
  template:
    networkConfig:
      interfaces:
        - name: primary
          type: ethernet
          state: up
          ipv4:
            enabled: true
            dhcp: false
          ipv6:
            enabled: false
      routes:
        config:
          - destination: 0.0.0.0/0
            next-hop-address: 192.168.133.1
            next-hop-interface: primary
            table-id: 254
---
apiVersion: bootwright.io/v1alpha1
kind: Machine
metadata:
  name: sno-kubevirt-child-master-0
spec:
  capabilities:
    - openshift-node
  substrate:
    providerRef: safety-kubevirt-provider
    profileRef: child
  os:
    provided: false
  network:
    config:
      networkConfigRef: safety-child-net
      interfaceAddresses:
        - interface: primary
          addressRef: ip
          prefixLength: 24
  addresses:
    - name: ip
      address: 192.168.133.30
---
apiVersion: bootwright.io/v1alpha1
kind: ContainerCluster
metadata:
  name: sno-kubevirt-child
spec:
  distribution:
    release:
      version: 4.21.15
  install:
    nodeSSH:
      keyPairRef: sno-libvirt-cluster-admin-ssh-key
    endpoints:
      api:
        source:
          type: node
      ingress:
        source:
          type: node
  nodes:
    - name: master-0
      role: master
      machineRef: sno-kubevirt-child-master-0
`

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

func seedSafetyWorkflowBundle(t *testing.T) {
	t.Helper()
	dir, err := resolveBundleDir()
	if err != nil {
		t.Fatalf("resolve workflow bundle: %v", err)
	}
	restore := converge.SetWorkflowBundlePreparerForTest(func(_ string, _ string, _ bool) (bundle.AnsibleBundleResult, error) {
		return bundle.AnsibleBundleResult{Dir: dir, Reused: true}, nil
	})
	t.Cleanup(restore)
}

func seedRunnableSafetyMutation(t *testing.T, ctx workspace.Context) {
	t.Helper()
	seedSafetyWorkflowBundle(t)
	state, err := desiredstate.LoadNormalizeValidateInputFiles(ctx.InputPaths)
	if err != nil {
		t.Fatalf("load runnable safety state: %v", err)
	}
	idx := secret.NewIndex(state)
	store := secret.NewContextStore(ctx.Name, ctx.SecretsDir)
	for _, declaration := range state.Secrets {
		for _, entry := range secretPathEntriesForSecret(declaration, idx, ctx.SecretsDir) {
			body := safetySecretBody(declaration.Spec.Type, entry.role)
			if entry.externalSource {
				if err := os.MkdirAll(filepath.Dir(entry.path), 0o700); err != nil {
					t.Fatalf("create external secret fixture directory: %v", err)
				}
				if err := os.WriteFile(entry.path, body, 0o600); err != nil {
					t.Fatalf("write external secret fixture %s: %v", entry.path, err)
				}
				continue
			}
			if err := store.Write(secret.MaterialKey{Name: entry.name, Role: entry.role}, body); err != nil {
				t.Fatalf("write context secret fixture %s/%s: %v", entry.name, entry.role, err)
			}
		}
	}
	fingerprint, err := sshtrust.FingerprintSHA256("QUJDRA==")
	if err != nil {
		t.Fatalf("build safety host-key fingerprint: %v", err)
	}
	trust := sshtrust.Store{}
	for _, machine := range sshtrust.ManagedTrustMachines(state, controllerLocalityPolicy) {
		address := v1alpha1.MachineSSHAddress(machine)
		trust.Hosts = append(trust.Hosts, sshtrust.HostRecord{
			Name:              machine.Metadata.Name,
			Address:           address,
			KeyType:           "ssh-ed25519",
			PublicKey:         "QUJDRA==",
			FingerprintSHA256: fingerprint,
			KnownHostsLine:    sshtrust.KnownHostsLine(address, "ssh-ed25519", "QUJDRA=="),
		})
	}
	if err := sshtrust.Save(sshtrust.DirForContext(ctx.BaseDir), trust); err != nil {
		t.Fatalf("write safety host trust fixture: %v", err)
	}
	previous := preflight.DefaultDeps
	deps := previous
	deps.CommandOutputLocalRoot = func(_ string, args ...string) ([]byte, error) {
		return []byte(`{"gitVersion":"v1","request":"` + strings.Join(args, " ") + `"}`), nil
	}
	preflight.DefaultDeps = deps
	t.Cleanup(func() { preflight.DefaultDeps = previous })
	previousChecker := applyClusterAvailabilityChecker
	applyClusterAvailabilityChecker = safetyClusterAvailabilityChecker{}
	t.Cleanup(func() { applyClusterAvailabilityChecker = previousChecker })
}

func safetySecretBody(secretType string, role secret.MaterialRole) []byte {
	switch role {
	case secret.MaterialSSHPrivate, secret.MaterialTLSKey:
		return []byte("test-private-key\n")
	case secret.MaterialSSHPublic:
		return []byte("ssh-ed25519 QUJDRA== bootwright-safety-test\n")
	}
	switch secretType {
	case v1alpha1.SecretTypeDockerConfigJSON:
		return []byte(`{"auths":{"registry.example":{"auth":"dGVzdDp0ZXN0"}}}`)
	case v1alpha1.SecretTypeUsernamePassword:
		return []byte("test:password\n")
	case v1alpha1.SecretTypeCABundle, v1alpha1.SecretTypeTLSCertificate:
		return []byte("test-certificate\n")
	default:
		return []byte("test-secret\n")
	}
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
		Cluster:          cluster,
		DesiredHash:      "sha256:seeded",
		HashSchema:       workflow.ConvergeHashSchema,
		Status:           workflow.ClusterInstallStatusInstalled,
		Phase:            workflow.ClusterInstallPhaseComplete,
		InstallerVersion: safetyDeclaredInstallerVersion(t, ctx, cluster),
		UpdatedAt:        now,
		InstalledAt:      &now,
	}); err != nil {
		t.Fatalf("SaveClusterInstallRecord(%s): %v", cluster, err)
	}
	seedClusterKubeconfig(t, ctx.Name, ctx.ClustersDir, cluster)
}

func seedMatchingInstalledCluster(t *testing.T, ctx workspace.Context, cluster string) {
	t.Helper()
	state, err := desiredstate.LoadNormalizeValidateInputFiles(ctx.InputPaths)
	if err != nil {
		t.Fatalf("load desired state for installed cluster: %v", err)
	}
	desiredHash, structuralHash, err := workflow.ComputeClusterInstallHashes(ctx.Name, state, cluster, ctx.SecretsDir)
	if err != nil {
		t.Fatalf("compute install hashes for %s: %v", cluster, err)
	}
	now := time.Now().UTC()
	if err := workflow.SaveClusterInstallRecord(ctx.ClustersDir, workflow.ClusterInstallRecord{
		Cluster:          cluster,
		DesiredHash:      desiredHash,
		StructuralHash:   structuralHash,
		HashSchema:       workflow.ConvergeHashSchema,
		Status:           workflow.ClusterInstallStatusInstalled,
		Phase:            workflow.ClusterInstallPhaseComplete,
		InstallerVersion: safetyDeclaredInstallerVersion(t, ctx, cluster),
		UpdatedAt:        now,
		InstalledAt:      &now,
	}); err != nil {
		t.Fatalf("save matching install record for %s: %v", cluster, err)
	}
	seedClusterKubeconfig(t, ctx.Name, ctx.ClustersDir, cluster)
}

func seedClusterInstallLifecycle(t *testing.T, ctx workspace.Context, cluster string, status workflow.ClusterInstallStatus, phase workflow.ClusterInstallPhase, installerVersion string, startedAt time.Time) {
	t.Helper()
	state, err := desiredstate.LoadNormalizeValidateInputFiles(ctx.InputPaths)
	if err != nil {
		t.Fatalf("load desired state for install lifecycle: %v", err)
	}
	desiredHash, structuralHash, err := workflow.ComputeClusterInstallHashes(ctx.Name, state, cluster, ctx.SecretsDir)
	if err != nil {
		t.Fatalf("compute install lifecycle hashes for %s: %v", cluster, err)
	}
	now := time.Now().UTC()
	record := workflow.ClusterInstallRecord{
		Cluster:          cluster,
		DesiredHash:      desiredHash,
		StructuralHash:   structuralHash,
		HashSchema:       workflow.ConvergeHashSchema,
		Status:           status,
		Phase:            phase,
		InstallerVersion: installerVersion,
		StartedAt:        startedAt.UTC(),
		UpdatedAt:        now,
	}
	if status == workflow.ClusterInstallStatusInstalled {
		record.InstalledAt = &now
	}
	if err := workflow.SaveClusterInstallRecord(ctx.ClustersDir, record); err != nil {
		t.Fatalf("save install lifecycle record for %s: %v", cluster, err)
	}
}

func safetyDeclaredInstallerVersion(t *testing.T, ctx workspace.Context, cluster string) string {
	t.Helper()
	state, err := desiredstate.LoadNormalizeValidateInputFiles(ctx.InputPaths)
	if err != nil {
		t.Fatalf("load desired state for installer version: %v", err)
	}
	for _, candidate := range state.ContainerClusters {
		if candidate.Metadata.Name == cluster {
			return candidate.Spec.Distribution.Release.Version
		}
	}
	return ""
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
