package ceph

var adapterBoundaries = []string{"request", "artifact", "ownership", "evidence", "material", "transport", "remote-refusal", "adapter-exception", "native-command", "native-timeout", "native-output", "unknown", "foreign-cluster"}

func boundaryRefusals(subject, remedy string, extra ...string) map[string]error {
	out := map[string]error{}
	for _, reason := range append(append([]string{}, adapterBoundaries...), extra...) {
		out[reason] = failure("lifecycle.unknown", subject+" could not prove its "+reason+" boundary", remedy)
	}
	return out
}
