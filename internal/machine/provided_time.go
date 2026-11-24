package machine

import api "github.com/crmarques/bootwright/api/v1alpha1"

const ProvidedTimeFamily = "os-provided-time"

func ProvidedTimeSelections(object api.Object) []api.Value {
	return object.Spec().Get("os", "provided", "ntp").Items()
}

func ProvidedTime(object api.Object, controller string) []api.Value {
	selections := ProvidedTimeSelections(object)
	if len(selections) == 0 || !api.MachineProvided(object) || object.Name() == controller {
		return nil
	}
	if _, err := PlacementFor(object, controller); err != nil {
		return nil
	}
	return selections
}

func validateProvidedTime(object api.Object, catalog api.Catalog) []api.Issue {
	if len(ProvidedTimeSelections(object)) == 0 {
		return nil
	}
	const field = "$.spec.os.provided.ntp"
	controller := ControllerName(catalog)
	if object.Name() == controller {
		return []api.Issue{invariant(field, "provided time synchronization converges a remote Machine; the controller keeps its own time service", "remove spec.os.provided.ntp and maintain the controller's time service separately")}
	}
	if _, err := PlacementFor(object, controller); err != nil {
		return []api.Issue{invariant(field, "provided time synchronization requires pinned lifecycle SSH access", "author spec.access.lifecycle or a pinned root login in spec.access.ssh")}
	}
	return nil
}
