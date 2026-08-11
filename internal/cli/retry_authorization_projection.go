package cli

import "slices"

func authorizationsAcceptedByVerb(names []string, sourceVerb, targetVerb invocationVerb) []string {
	accepted := authorizationTokenNamesForVerb(string(targetVerb))
	var out []string
	for _, name := range names {
		if name == authorizeAll && sourceVerb != targetVerb {
			for _, expanded := range authorizationTokenNamesForVerb(string(sourceVerb)) {
				if expanded != authorizeAll && slices.Contains(accepted, expanded) && !slices.Contains(out, expanded) {
					out = append(out, expanded)
				}
			}
			continue
		}
		if slices.Contains(accepted, name) && !slices.Contains(out, name) {
			out = append(out, name)
		}
	}
	return out
}
