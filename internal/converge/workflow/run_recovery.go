package workflow

import (
	"errors"
	"slices"
	"sort"
	"strings"

	"github.com/crmarques/bootwright/internal/converge/remedy"
)

type RunRecoveryTarget struct {
	Role remedy.TargetRole `json:"role"`
	Name string            `json:"name"`
}

type RunRecoveryRequest struct {
	Action  remedy.Action       `json:"action"`
	Targets []RunRecoveryTarget `json:"targets,omitempty"`
}

type RunRecoveryStep struct {
	Args []string `json:"args"`
}

type RunRecoveryPlan struct {
	Request RunRecoveryRequest `json:"request"`
	Steps   []RunRecoveryStep  `json:"steps,omitempty"`
}

type RunRecoveryResolver func(remedy.Request) (RunRecoveryPlan, error)

func NewRunRecoveryPlan(request remedy.Request, steps ...[]string) RunRecoveryPlan {
	plan := RunRecoveryPlan{Request: runRecoveryRequest(request)}
	for _, args := range steps {
		plan.Steps = append(plan.Steps, RunRecoveryStep{Args: append([]string(nil), args...)})
	}
	return plan
}

func UnresolvedRunRecoveryPlan(request remedy.Request) RunRecoveryPlan {
	return RunRecoveryPlan{Request: runRecoveryRequest(request)}
}

func (p RunRecoveryPlan) Clone() RunRecoveryPlan {
	return NewRunRecoveryPlan(p.Remedy(), p.stepArgs()...)
}

func (p RunRecoveryPlan) Remedy() remedy.Request {
	targets := make([]remedy.Target, 0, len(p.Request.Targets))
	for _, target := range p.Request.Targets {
		targets = append(targets, remedy.Target{Role: target.Role, Name: target.Name})
	}
	return remedy.Request{Action: p.Request.Action, Targets: targets}
}

func (p RunRecoveryPlan) Matches(request remedy.Request) bool {
	actual := p.Remedy()
	return actual.Action == request.Action && slices.Equal(actual.Targets, request.Targets)
}

func (p RunRecoveryPlan) ValidRequest() bool {
	return ValidRunRecoveryRequest(p.Remedy())
}

func ValidRunRecoveryRequest(request remedy.Request) bool {
	switch request.Action {
	case remedy.ActionRetrySameInvocation, remedy.ActionReconcileSameSelection, remedy.ActionRebuildSameSelection:
		return validNamedRecoveryTargets(request.Targets, remedy.TargetRoleContainerCluster, 0, -1)
	case remedy.ActionApplyAllConsumers, remedy.ActionResumeControllerDNSMutation, remedy.ActionReconcileSharedServiceThenRetrySameSelection:
		return validNamedRecoveryTargets(request.Targets, remedy.TargetRoleClusterRoot, 1, -1)
	case remedy.ActionReconcileContainerClusterThenRetrySameSelection, remedy.ActionRegenerateClusterISO, remedy.ActionDestroyAndReapplyCluster, remedy.ActionRebuildCluster:
		return validNamedRecoveryTargets(request.Targets, remedy.TargetRoleContainerCluster, 1, 1)
	case remedy.ActionDestroyProtectedLayersThenRebuildSameSelection:
		return validProtectedLayerRecoveryTargets(request.Targets)
	default:
		return false
	}
}

func (p RunRecoveryPlan) Valid() bool {
	if !p.ValidRequest() {
		return false
	}
	var verbs []string
	switch p.Request.Action {
	case remedy.ActionRetrySameInvocation:
		if len(p.Steps) != 1 || !validRecoveryStep(p.Steps[0], "apply", "destroy") {
			return false
		}
		return true
	case remedy.ActionApplyAllConsumers, remedy.ActionResumeControllerDNSMutation, remedy.ActionReconcileSameSelection, remedy.ActionRebuildSameSelection, remedy.ActionRebuildCluster:
		verbs = []string{"apply"}
	case remedy.ActionReconcileSharedServiceThenRetrySameSelection, remedy.ActionReconcileContainerClusterThenRetrySameSelection, remedy.ActionRegenerateClusterISO:
		verbs = []string{"apply", "apply"}
	case remedy.ActionDestroyAndReapplyCluster:
		verbs = []string{"destroy", "apply", "apply"}
	case remedy.ActionDestroyProtectedLayersThenRebuildSameSelection:
		machineRoots, clusterRoots, _ := protectedLayerRecoveryTargetRoots(p.Remedy().Targets)
		layerCount := 0
		if len(clusterRoots) > 0 {
			layerCount++
		}
		if len(machineRoots) > 0 {
			layerCount++
		}
		verbs = make([]string, layerCount+1)
		for i := range verbs[:len(verbs)-1] {
			verbs[i] = "destroy"
		}
		verbs[len(verbs)-1] = "apply"
	default:
		return false
	}
	if len(p.Steps) != len(verbs) {
		return false
	}
	for i, verb := range verbs {
		if !validRecoveryStep(p.Steps[i], verb) {
			return false
		}
	}
	return true
}

func (p RunRecoveryPlan) stepArgs() [][]string {
	steps := make([][]string, 0, len(p.Steps))
	for _, step := range p.Steps {
		steps = append(steps, append([]string(nil), step.Args...))
	}
	return steps
}

func defaultRunRecoveryPlan(opts RunOptions) RunRecoveryPlan {
	if opts.InterruptionRecovery.Request.Action != "" {
		return opts.InterruptionRecovery.Clone()
	}
	if len(opts.InvocationArgs) == 0 {
		return RunRecoveryPlan{}
	}
	return NewRunRecoveryPlan(remedy.Request{Action: remedy.ActionRetrySameInvocation}, opts.InvocationArgs)
}

func taskInterruptionRunRecoveryPlan(opts RunOptions, task ApplyTask) RunRecoveryPlan {
	request := remedy.Request{Action: remedy.ActionRetrySameInvocation}
	switch {
	case task.ExecutionClass == ApplyTaskExecutionLiveProof:
	case task.Entry.Kind == ApplyTaskKindControllerNameResolution:
		request.Action = remedy.ActionResumeControllerDNSMutation
		request.Targets = append([]remedy.Target(nil), task.FailureRemedy.Targets...)
	case opts.ApplyMode == ApplyModeCreate:
		request.Action = remedy.ActionReconcileSameSelection
	}
	return resolveRunRecoveryPlan(opts, request)
}

func resolveRunRecoveryPlan(opts RunOptions, request remedy.Request) RunRecoveryPlan {
	if opts.ResolveRecovery != nil {
		plan, err := opts.ResolveRecovery(request)
		if err != nil || !plan.Matches(request) || !plan.ValidFor(opts.InvocationArgs) {
			return UnresolvedRunRecoveryPlan(request)
		}
		return plan.Clone()
	}
	if opts.InterruptionRecovery.Matches(request) && opts.InterruptionRecovery.ValidFor(opts.InvocationArgs) {
		return opts.InterruptionRecovery.Clone()
	}
	if request.Action == remedy.ActionRetrySameInvocation && len(opts.InvocationArgs) > 0 {
		return NewRunRecoveryPlan(request, opts.InvocationArgs)
	}
	return UnresolvedRunRecoveryPlan(request)
}

func failureRunRecoveryPlan(opts RunOptions, current RunRecoveryPlan, err error) RunRecoveryPlan {
	var remedial remedy.Error
	if !errors.As(err, &remedial) {
		return current
	}
	request := remedial.Remedy()
	return resolveRunRecoveryPlan(opts, request)
}

func runRecoveryRequest(request remedy.Request) RunRecoveryRequest {
	targets := make([]RunRecoveryTarget, 0, len(request.Targets))
	for _, target := range request.Targets {
		targets = append(targets, RunRecoveryTarget{Role: target.Role, Name: target.Name})
	}
	return RunRecoveryRequest{Action: request.Action, Targets: targets}
}

func validNamedRecoveryTargets(targets []remedy.Target, role remedy.TargetRole, min, max int) bool {
	if len(targets) < min || max >= 0 && len(targets) > max {
		return false
	}
	seen := map[string]bool{}
	for _, target := range targets {
		name := strings.TrimSpace(target.Name)
		if target.Role != role || name == "" || seen[name] {
			return false
		}
		seen[name] = true
	}
	return true
}

func validProtectedLayerRecoveryTargets(targets []remedy.Target) bool {
	_, _, valid := protectedLayerRecoveryTargetRoots(targets)
	return valid
}

func protectedLayerRecoveryTargetRoots(targets []remedy.Target) ([]string, []string, bool) {
	seenLayers := map[remedy.TargetRole]bool{}
	seenRoots := map[remedy.TargetRole]map[string]bool{
		remedy.TargetRoleMachineLayerRoot: {},
		remedy.TargetRoleClusterLayerRoot: {},
	}
	var machineRoots, clusterRoots []string
	for _, target := range targets {
		name := strings.TrimSpace(target.Name)
		switch target.Role {
		case remedy.TargetRoleMachineLayer, remedy.TargetRoleClusterLayer:
			if name != "" || seenLayers[target.Role] {
				return nil, nil, false
			}
			seenLayers[target.Role] = true
		case remedy.TargetRoleMachineLayerRoot, remedy.TargetRoleClusterLayerRoot:
			if name == "" || seenRoots[target.Role][name] {
				return nil, nil, false
			}
			seenRoots[target.Role][name] = true
			if target.Role == remedy.TargetRoleMachineLayerRoot {
				machineRoots = append(machineRoots, name)
			} else {
				clusterRoots = append(clusterRoots, name)
			}
		default:
			return nil, nil, false
		}
	}
	if len(machineRoots) == 0 != !seenLayers[remedy.TargetRoleMachineLayer] || len(clusterRoots) == 0 != !seenLayers[remedy.TargetRoleClusterLayer] || len(machineRoots)+len(clusterRoots) == 0 {
		return nil, nil, false
	}
	sort.Strings(machineRoots)
	sort.Strings(clusterRoots)
	return machineRoots, clusterRoots, true
}

func validRecoveryStep(step RunRecoveryStep, verbs ...string) bool {
	return len(step.Args) >= 2 && step.Args[0] == "bootwright" && slices.Contains(verbs, step.Args[1])
}
