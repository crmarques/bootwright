package machine

import api "github.com/crmarques/bootwright/api/v1alpha1"

const ProvidedUpdateFamily = "os-provided-update"

func ProvidedUpdateRelease(object api.Object) string {
	return object.Spec().Get("os", "provided", "updates", "release").Text()
}

func ProvidedUpdatePolicy(object api.Object) string {
	return object.Spec().Get("os", "provided", "updates", "policy").Text()
}

func validateProvidedUpdate(object api.Object, catalog api.Catalog) []api.Issue {
	if ProvidedUpdateRelease(object) == "" {
		return nil
	}
	const field = "$.spec.os.provided.updates"
	controller := controllerForPlacement(catalog)
	if object.Name() == controller {
		return []api.Issue{invariant(field, "provided OS updates can reboot a remote Machine but cannot reboot the controller", "remove the Machine's update policy and maintain the controller separately")}
	}
	if _, err := PlacementFor(object, controller); err != nil {
		return []api.Issue{invariant(field, "provided OS updates require pinned lifecycle SSH access", "author spec.access.lifecycle or a pinned root login in spec.access.ssh")}
	}
	return nil
}
