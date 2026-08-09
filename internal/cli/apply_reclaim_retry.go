package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/crmarques/bootwright/internal/converge"
	"github.com/crmarques/bootwright/internal/converge/workflow"
)

func reclaimResolutionRefusal(err error, invocation *resolvedInvocation) error {
	var none *converge.ReclaimAllNoDeclaredDevicesError
	if !errors.As(err, &none) || invocation == nil {
		return err
	}
	if len(none.EffectiveUnboundedClusters) > 0 && sameNames(none.Clusters, none.EffectiveUnboundedClusters) {
		command, retryErr := invocation.applyUnboundedOSDReclaimRetry()
		if retryErr != nil {
			return fmt.Errorf("%w; could not construct the intentional retry: %v", err, retryErr)
		}
		return fmt.Errorf("%w; cluster(s) %s use a managed, effectively unbounded dataDevices.all selection, so re-run `%s` to authorize their fail-closed automatic reclaim", err, strings.Join(none.EffectiveUnboundedClusters, ", "), command.String())
	}
	command, retryErr := invocation.retry(retryIntent{})
	if retryErr != nil {
		return fmt.Errorf("%w; could not construct the exact retry: %v", err, retryErr)
	}
	return fmt.Errorf("%w; pin the intended disk with a static nodes[].devices, dataDevices.paths, or pathSpecs entry, then re-run `%s`; a narrowing host filter is selection, not permission to wipe its match", err, command.String())
}

func sameNames(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	names := make(map[string]bool, len(left))
	for _, name := range left {
		names[name] = true
	}
	for _, name := range right {
		if !names[name] {
			return false
		}
	}
	return true
}

func (i resolvedInvocation) applyUnboundedOSDReclaimRetry() (retryCommand, error) {
	if i.verb != invocationApply {
		return retryCommand{}, fmt.Errorf("cannot construct an OSD reclaim retry for %s", i.verb)
	}
	next := i
	next.flags.reclaimDevices = ""
	return next.retry(retryIntent{
		mode:                   workflow.ApplyModeRebuild,
		requiredAuthorizations: []string{authorizeDataLoss},
	})
}

func (i resolvedInvocation) applyRuntimeReclaimRetryTemplate(preservedDevices string) (retryCommand, []string, error) {
	if i.verb != invocationApply {
		return retryCommand{}, nil, fmt.Errorf("cannot construct an OSD reclaim template for %s", i.verb)
	}
	preserved := append([]string{}, converge.SplitReclaimDevices(preservedDevices)...)
	if converge.ReclaimDevicesRequestsAll(preservedDevices) {
		preserved = nil
	}
	for _, entry := range preserved {
		if entry == converge.ReclaimDevicesAll {
			return retryCommand{}, nil, fmt.Errorf("cannot combine %s with a runtime OSD reclaim path", converge.ReclaimDevicesAll)
		}
	}
	next := i
	next.flags.reclaimDevices = converge.ApplyReclaimInvocationSentinel
	command, err := next.retry(retryIntent{
		requiredAuthorizations: []string{authorizeDataLoss, authorizeUnownedDevices},
	})
	if err != nil {
		return retryCommand{}, nil, err
	}
	sentinelCount := 0
	reclaimValueCount := 0
	for index, arg := range command.args {
		sentinelCount += strings.Count(arg, converge.ApplyReclaimInvocationSentinel)
		if arg == "--reclaim-devices" && index+1 < len(command.args) && command.args[index+1] == converge.ApplyReclaimInvocationSentinel {
			reclaimValueCount++
		}
	}
	if sentinelCount != 1 || reclaimValueCount != 1 {
		return retryCommand{}, nil, fmt.Errorf("runtime OSD reclaim template must contain exactly one reclaim-device sentinel, got sentinel=%d operand=%d", sentinelCount, reclaimValueCount)
	}
	return command, preserved, nil
}
