package workflow

import "fmt"

type controllerDNSDependencyClass string

const (
	controllerDNSBefore  controllerDNSDependencyClass = "before"
	controllerDNSBarrier controllerDNSDependencyClass = "barrier"
	controllerDNSAfter   controllerDNSDependencyClass = "after"
	controllerDNSDynamic controllerDNSDependencyClass = "dynamic"
)

var controllerDNSDependencyClasses = map[string]controllerDNSDependencyClass{
	ApplyTaskKindProvider:                 controllerDNSBefore,
	ApplyTaskKindInfraComponentServices:   controllerDNSBefore,
	ApplyTaskKindControllerNameResolution: controllerDNSBarrier,
	ApplyTaskKindMachineInfraPrepare:      controllerDNSAfter,
	ApplyTaskKindClusterInstall:           controllerDNSAfter,
	ApplyTaskKindMachineInfraFinalize:     controllerDNSAfter,
	ApplyTaskKindManagedMachineOS:         controllerDNSAfter,
	ApplyTaskKindMachineRegistration:      controllerDNSAfter,
	ApplyTaskKindMachineRepositories:      controllerDNSAfter,
	ApplyTaskKindStorageNodeAccess:        controllerDNSAfter,
	ApplyTaskKindStorageInfra:             controllerDNSAfter,
	ApplyTaskKindClusterISO:               controllerDNSAfter,
	ApplyTaskKindHostVirtctl:              controllerDNSAfter,
	ApplyTaskKindNodeBoot:                 controllerDNSAfter,
	ApplyTaskKindBootstrapWait:            controllerDNSAfter,
	ApplyTaskKindInstallWait:              controllerDNSAfter,
	ApplyTaskKindStorageCluster:           controllerDNSAfter,
	ApplyTaskKindClusterAddon:             controllerDNSAfter,
	ApplyTaskKindNodeConfigApply:          controllerDNSAfter,
	ApplyTaskKindPlaybook:                 controllerDNSDynamic,
}

func enforceControllerDNSDependencies(graph *ActivityGraph) error {
	var barriers []string
	for _, activity := range graph.ActivitySnapshot() {
		class, ok := controllerDNSDependencyClasses[activity.Task.Entry.Kind]
		if !ok {
			return fmt.Errorf("apply task kind %q has no controller-DNS dependency classification", activity.Task.Entry.Kind)
		}
		if class == controllerDNSBarrier {
			barriers = append(barriers, activity.ID)
		}
	}
	if len(barriers) == 0 {
		return nil
	}
	for _, activity := range graph.ActivitySnapshot() {
		if controllerDNSDependencyClasses[activity.Task.Entry.Kind] != controllerDNSAfter {
			continue
		}
		for _, barrier := range barriers {
			if err := graph.AddDependency(activity.ID, barrier); err != nil {
				return err
			}
		}
	}
	return nil
}
