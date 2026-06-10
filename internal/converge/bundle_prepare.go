package converge

import (
	"github.com/crmarques/bootwright/internal/converge/bundle"
)

// PrepareWorkflowBundle resolves or extracts the embedded Ansible bundle.
// The bundle directory and version marker are owned by the CLI (the marker is
// stamped from build-time version metadata), so both arrive as parameters.
func PrepareWorkflowBundle(bundleDir, versionMarker string, skipExtract bool) (bundle.AnsibleBundleResult, error) {
	if skipExtract {
		return bundle.AnsibleBundleResult{Dir: bundleDir, Reused: true}, nil
	}
	return bundle.EnsureAnsibleBundle(bundleDir, versionMarker)
}

func PrepareInitialBundle(bundleDir, versionMarker string) (bundle.AnsibleBundleResult, bool, error) {
	result, err := PrepareWorkflowBundle(bundleDir, versionMarker, false)
	if err == nil {
		return result, false, nil
	}
	if !bundle.IsEmptyAnsibleBundle(err) {
		return bundle.AnsibleBundleResult{}, false, err
	}
	return bundle.AnsibleBundleResult{Dir: bundleDir, Reused: true}, true, nil
}
