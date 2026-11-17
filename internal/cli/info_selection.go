package cli

// An omitted selector means all; names match exactly after the shared
// whitespace and duplicate normalization.
func selectedInfoNames(values *requestValues) []string {
	raw := values.text("name")
	if !values.flags.Changed("name") {
		return nil
	}
	return append([]string{}, normalizeNames(raw)...)
}
