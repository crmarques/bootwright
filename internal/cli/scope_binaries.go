package cli

func resolvedOCPBinaryPairs(selected []Phase, hostStateDir string) []string {
	clustersSelected := false
	for _, p := range selected {
		if p.Name == "clusters" {
			clustersSelected = true
			break
		}
	}
	if !clustersSelected {
		return nil
	}
	path, err := defaultLookPath("openshift-install", openshiftInstallSearchDirs(hostStateDir))
	if err != nil {
		return nil
	}
	return []string{"bootwright_openshift_install=" + path}
}
