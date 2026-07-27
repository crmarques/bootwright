package v1alpha1

type PlaybookSource struct {
	Path string `yaml:"path,omitempty" json:"path,omitempty"`
}

func PlaybookSourceIsSet(source *PlaybookSource) bool {
	return source != nil && source.Path != ""
}
