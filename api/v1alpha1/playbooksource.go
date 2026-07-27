package v1alpha1

type PlaybookSource struct {
	Path string             `yaml:"path,omitempty" json:"path,omitempty"`
	Git  *PlaybookGitSource `yaml:"git,omitempty" json:"git,omitempty"`
}

type PlaybookGitSource struct {
	URL       string     `yaml:"url" json:"url"`
	Ref       string     `yaml:"ref" json:"ref"`
	Subdir    string     `yaml:"subdir,omitempty" json:"subdir,omitempty"`
	SecretRef *SecretRef `yaml:"secretRef,omitempty" json:"secretRef,omitempty"`
}

func PlaybookSourceIsSet(source *PlaybookSource) bool {
	return source != nil && (source.Path != "" || source.Git != nil)
}

func PlaybookSourceIsGit(source *PlaybookSource) bool {
	return source != nil && source.Git != nil
}
