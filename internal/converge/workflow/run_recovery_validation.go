package workflow

import (
	"slices"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/internal/converge/remedy"
)

var recoveryValueFlags = map[string]bool{
	"--mode":                   true,
	"--authorize":              true,
	"--stage":                  true,
	"--through":                true,
	"--clusters":               true,
	"--machines":               true,
	"--reclaim-devices":        true,
	"--recover-ceph-ownership": true,
	"--output":                 true,
	"--context":                true,
	"--ssh-id-file":            true,
	"--ssh-user":               true,
}

var recoveryBooleanFlags = map[string]bool{
	"--yes":                      true,
	"--purge-history":            true,
	"--dry-run":                  true,
	"--verbose":                  true,
	"--ssh-ask-sudo-password":    true,
	"--ssh-user-for-provisioned": true,
}

var recoveryAssignedBooleanFlags = map[string]bool{
	"--ask-become-pass":    true,
	"--trust-on-first-use": true,
}

var recoveryAuthorizationTokens = map[string][]string{
	"apply":   {"all", "data-loss", "unowned-devices", "foreign-daemons"},
	"destroy": {"all", "data-loss", "protected", "installed-cluster-node", "unowned-vms", "unowned-networks", "unowned-devices", "unreachable-nodes", "unreadable-records", "shared-infra", "stale-input"},
}

type recoveryInvocation struct {
	verb  string
	flags map[string]string
	auth  []string
}

func (p RunRecoveryPlan) ValidFor(originalArgs []string) bool {
	if !p.Valid() {
		return false
	}
	original, ok := parseRecoveryInvocation(originalArgs)
	if !ok {
		return false
	}
	steps := make([]recoveryInvocation, 0, len(p.Steps))
	for _, step := range p.Steps {
		invocation, valid := parseRecoveryInvocation(step.Args)
		if !valid || invocation.flags["--context"] != original.flags["--context"] {
			return false
		}
		steps = append(steps, invocation)
	}
	request := p.Remedy()
	switch request.Action {
	case remedy.ActionRetrySameInvocation:
		return slices.Equal(p.Steps[0].Args, originalArgs)
	case remedy.ActionApplyAllConsumers:
		return original.verb == "apply" && validRecoverySameExcept(original, steps[0], "--clusters", "--machines") &&
			steps[0].flags["--clusters"] == recoveryTargetNames(request.Targets) && steps[0].flags["--machines"] == ""
	case remedy.ActionResumeControllerDNSMutation:
		return validControllerDNSRecovery(original, steps[0])
	case remedy.ActionReconcileSharedServiceThenRetrySameSelection:
		return original.verb == "apply" && slices.Equal(p.Steps[1].Args, originalArgs) &&
			validRecoverySameExcept(original, steps[0], "--mode", "--stage", "--through", "--clusters", "--machines", "--reclaim-devices") &&
			steps[0].flags["--mode"] == string(ApplyModeReconcile) && steps[0].flags["--stage"] == ApplyPhaseFabric && steps[0].flags["--through"] == "" &&
			steps[0].flags["--clusters"] == recoveryTargetNames(request.Targets) && steps[0].flags["--machines"] == "" && steps[0].flags["--reclaim-devices"] == ""
	case remedy.ActionReconcileSameSelection:
		return validSameSelectionRecovery(original, steps[0], ApplyModeReconcile, nil)
	case remedy.ActionReconcileContainerClusterThenRetrySameSelection:
		return slices.Equal(p.Steps[1].Args, originalArgs) && validClusterLifecycleRecovery(original, steps[0], "apply", request.Targets[0].Name, "clusters", ApplyModeReconcile, nil)
	case remedy.ActionRebuildSameSelection:
		return validSameSelectionRecovery(original, steps[0], ApplyModeRebuild, []string{"data-loss"})
	case remedy.ActionRegenerateClusterISO:
		return slices.Equal(p.Steps[1].Args, originalArgs) && validClusterLifecycleRecovery(original, steps[0], "apply", request.Targets[0].Name, ApplyPhaseDeps, ApplyModeRebuild, nil)
	case remedy.ActionDestroyAndReapplyCluster:
		return validClusterLifecycleRecovery(original, steps[0], "destroy", request.Targets[0].Name, "clusters", "", []string{"protected", "data-loss"}) &&
			validClusterLifecycleRecovery(original, steps[1], "apply", request.Targets[0].Name, "clusters", ApplyModeReconcile, []string{"data-loss"}) &&
			slices.Equal(p.Steps[2].Args, originalArgs)
	case remedy.ActionRebuildCluster:
		return validClusterLifecycleRecovery(original, steps[0], "apply", request.Targets[0].Name, "clusters", ApplyModeRebuild, []string{"data-loss"})
	case remedy.ActionDestroyProtectedLayersThenRebuildSameSelection:
		return validProtectedLayerRecovery(original, steps, request.Targets)
	default:
		return false
	}
}

func parseRecoveryInvocation(args []string) (recoveryInvocation, bool) {
	if len(args) < 4 || args[0] != "bootwright" || args[1] != "apply" && args[1] != "destroy" {
		return recoveryInvocation{}, false
	}
	invocation := recoveryInvocation{verb: args[1], flags: map[string]string{}}
	for i := 2; i < len(args); i++ {
		arg := args[i]
		if flag, value, assigned := strings.Cut(arg, "="); assigned {
			if !recoveryAssignedBooleanFlags[flag] || invocation.flags[flag] != "" || value != "true" && value != "false" {
				return recoveryInvocation{}, false
			}
			invocation.flags[flag] = value
			continue
		}
		if recoveryBooleanFlags[arg] {
			if invocation.flags[arg] != "" {
				return recoveryInvocation{}, false
			}
			invocation.flags[arg] = "true"
			continue
		}
		if !recoveryValueFlags[arg] || invocation.flags[arg] != "" || i+1 >= len(args) || strings.TrimSpace(args[i+1]) == "" {
			return recoveryInvocation{}, false
		}
		i++
		invocation.flags[arg] = args[i]
	}
	if invocation.flags["--context"] == "" || invocation.flags["--dry-run"] != "" || invocation.flags["--output"] != "" {
		return recoveryInvocation{}, false
	}
	if invocation.verb == "apply" {
		if mode := ApplyMode(invocation.flags["--mode"]); !mode.Valid() || invocation.flags["--recover-ceph-ownership"] != "" || invocation.flags["--purge-history"] != "" {
			return recoveryInvocation{}, false
		}
		if !validRecoveryApplyRange(invocation.flags["--stage"], invocation.flags["--through"]) {
			return recoveryInvocation{}, false
		}
	} else if invocation.flags["--mode"] != "" || invocation.flags["--through"] != "" || invocation.flags["--reclaim-devices"] != "" || invocation.flags["--trust-on-first-use"] != "" || !validRecoveryDestroyStage(invocation.flags["--stage"]) {
		return recoveryInvocation{}, false
	}
	if invocation.flags["--clusters"] != "" && invocation.flags["--machines"] != "" || !validRecoveryCSV(invocation.flags["--clusters"]) || !validRecoveryCSV(invocation.flags["--machines"]) {
		return recoveryInvocation{}, false
	}
	auth, valid := recoveryCSV(invocation.flags["--authorize"])
	if !valid {
		return recoveryInvocation{}, false
	}
	for _, name := range auth {
		if !slices.Contains(recoveryAuthorizationTokens[invocation.verb], name) {
			return recoveryInvocation{}, false
		}
	}
	invocation.auth = auth
	return invocation, true
}

func validRecoverySameExcept(original, step recoveryInvocation, changed ...string) bool {
	if original.verb != step.verb {
		return false
	}
	allowed := map[string]bool{}
	for _, flag := range changed {
		allowed[flag] = true
	}
	for flag, value := range original.flags {
		if !allowed[flag] && step.flags[flag] != value {
			return false
		}
	}
	for flag, value := range step.flags {
		if !allowed[flag] && original.flags[flag] != value {
			return false
		}
	}
	return true
}

func validSameSelectionRecovery(original, step recoveryInvocation, mode ApplyMode, requiredAuth []string) bool {
	return original.verb == "apply" && validRecoverySameExcept(original, step, "--mode", "--authorize") && step.flags["--mode"] == string(mode) && validRecoveryAuthorizations(original, step, requiredAuth)
}

func validControllerDNSRecovery(original, step recoveryInvocation) bool {
	if original.verb != "apply" || !validRecoverySameExcept(original, step, "--mode", "--stage", "--through") {
		return false
	}
	wantMode := original.flags["--mode"]
	if wantMode == string(ApplyModeCreate) {
		wantMode = string(ApplyModeReconcile)
	}
	wantStage := original.flags["--stage"]
	wantThrough := original.flags["--through"]
	phases, ok := recoveryApplyRange(wantStage, wantThrough)
	if !ok {
		return false
	}
	if !slices.Contains(phases, ApplyPhaseFabric) {
		wantStage = ApplyPhaseFabric
		wantThrough = phases[len(phases)-1]
	}
	return step.flags["--mode"] == wantMode && step.flags["--stage"] == wantStage && step.flags["--through"] == wantThrough
}

func validClusterLifecycleRecovery(original, step recoveryInvocation, verb, cluster, stage string, mode ApplyMode, requiredAuth []string) bool {
	changed := map[string]bool{
		"--mode": true, "--authorize": true, "--stage": true, "--through": true,
		"--clusters": true, "--machines": true, "--reclaim-devices": true,
		"--recover-ceph-ownership": true, "--purge-history": true, "--trust-on-first-use": true,
	}
	if !validRecoveryCrossVerbExcept(original, step, verb, changed) || step.flags["--stage"] != stage || step.flags["--through"] != "" ||
		step.flags["--clusters"] != strings.TrimSpace(cluster) || step.flags["--machines"] != "" || step.flags["--reclaim-devices"] != "" ||
		step.flags["--recover-ceph-ownership"] != "" || step.flags["--purge-history"] != "" || !validRecoveryAuthorizations(original, step, requiredAuth) {
		return false
	}
	if verb == "apply" {
		return step.flags["--mode"] == string(mode) && step.flags["--trust-on-first-use"] == original.flags["--trust-on-first-use"]
	}
	return step.flags["--mode"] == "" && step.flags["--trust-on-first-use"] == ""
}

func validProtectedLayerRecovery(original recoveryInvocation, steps []recoveryInvocation, targets []remedy.Target) bool {
	if original.verb != "apply" {
		return false
	}
	machineRoots, clusterRoots, valid := protectedLayerRecoveryTargetRoots(targets)
	if !valid {
		return false
	}
	type layerRecovery struct {
		stage string
		roots []string
	}
	var layers []layerRecovery
	if len(clusterRoots) > 0 {
		layers = append(layers, layerRecovery{stage: "clusters", roots: clusterRoots})
	}
	if len(machineRoots) > 0 {
		layers = append(layers, layerRecovery{stage: "infra", roots: machineRoots})
	}
	implicitSelection := original.flags["--clusters"] == "" && original.flags["--machines"] == ""
	for i, layer := range layers {
		changed := map[string]bool{
			"--mode": true, "--authorize": true, "--stage": true, "--through": true,
			"--reclaim-devices": true, "--recover-ceph-ownership": true, "--purge-history": true, "--trust-on-first-use": true,
		}
		if implicitSelection {
			changed["--clusters"] = true
			changed["--machines"] = true
		}
		step := steps[i]
		required := []string{"protected"}
		if layer.stage == "clusters" {
			required = append(required, "data-loss")
		}
		if !validRecoveryCrossVerbExcept(original, step, "destroy", changed) || step.flags["--stage"] != layer.stage || step.flags["--through"] != "" ||
			step.flags["--mode"] != "" || step.flags["--reclaim-devices"] != "" || step.flags["--recover-ceph-ownership"] != "" ||
			step.flags["--purge-history"] != "" || step.flags["--trust-on-first-use"] != "" || !validRecoveryAuthorizations(original, step, required) {
			return false
		}
		if implicitSelection && (step.flags["--clusters"] != strings.Join(layer.roots, ",") || step.flags["--machines"] != "") {
			return false
		}
	}
	return validSameSelectionRecovery(original, steps[len(steps)-1], ApplyModeRebuild, []string{"data-loss"})
}

func validRecoveryCrossVerbExcept(original, step recoveryInvocation, verb string, changed map[string]bool) bool {
	if step.verb != verb {
		return false
	}
	for flag, value := range original.flags {
		if !changed[flag] && step.flags[flag] != value {
			return false
		}
	}
	for flag, value := range step.flags {
		if !changed[flag] && original.flags[flag] != value {
			return false
		}
	}
	return true
}

func validRecoveryAuthorizations(original, step recoveryInvocation, required []string) bool {
	want := map[string]bool{}
	for _, name := range original.auth {
		if original.verb == step.verb {
			want[name] = true
			continue
		}
		if name == "all" {
			for _, expanded := range recoveryAuthorizationTokens[original.verb] {
				if expanded != "all" && slices.Contains(recoveryAuthorizationTokens[step.verb], expanded) {
					want[expanded] = true
				}
			}
			continue
		}
		if slices.Contains(recoveryAuthorizationTokens[step.verb], name) {
			want[name] = true
		}
	}
	if !want["all"] {
		for _, name := range required {
			want[name] = true
		}
	}
	got := map[string]bool{}
	for _, name := range step.auth {
		got[name] = true
	}
	if len(got) != len(want) {
		return false
	}
	for name := range want {
		if !got[name] {
			return false
		}
	}
	return true
}

func recoveryTargetNames(targets []remedy.Target) string {
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, strings.TrimSpace(target.Name))
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

func validRecoveryCSV(value string) bool {
	if value == "" {
		return true
	}
	_, valid := recoveryCSV(value)
	return valid
}

func recoveryCSV(value string) ([]string, bool) {
	if value == "" {
		return nil, true
	}
	seen := map[string]bool{}
	values := strings.Split(value, ",")
	for i := range values {
		values[i] = strings.TrimSpace(values[i])
		if values[i] == "" || seen[values[i]] {
			return nil, false
		}
		seen[values[i]] = true
	}
	return values, true
}

func validRecoveryApplyRange(stage, through string) bool {
	_, ok := recoveryApplyRange(stage, through)
	return ok
}

func recoveryApplyRange(stage, through string) ([]string, bool) {
	phases := []string{ApplyPhaseFabric, ApplyPhaseMachines, ApplyPhaseDeps, ApplyPhaseBase, ApplyPhaseAddons}
	start := 0
	if stage != "" {
		start = recoveryStageIndex(stage, phases)
		if start < 0 {
			return nil, false
		}
	}
	end := len(phases) - 1
	if through != "" {
		end = recoveryThroughIndex(through, phases)
		if end < 0 {
			return nil, false
		}
	} else if stage != "" {
		end = recoveryThroughIndex(stage, phases)
	}
	if start > end {
		return nil, false
	}
	return append([]string(nil), phases[start:end+1]...), true
}

func recoveryStageIndex(stage string, phases []string) int {
	switch stage {
	case "infra":
		return 0
	case "clusters":
		return 2
	default:
		return slices.Index(phases, stage)
	}
}

func recoveryThroughIndex(through string, phases []string) int {
	switch through {
	case "infra":
		return 1
	case "clusters", "end":
		return len(phases) - 1
	default:
		return slices.Index(phases, through)
	}
}

func validRecoveryDestroyStage(stage string) bool {
	return stage == "" || stage == "infra" || stage == "clusters"
}
