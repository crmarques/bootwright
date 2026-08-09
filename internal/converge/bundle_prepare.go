package converge

import (
	"github.com/crmarques/bootwright/internal/converge/bundle"
)

var workflowBundlePreparer = prepareWorkflowBundle

func PrepareWorkflowBundle(bundleDir, versionMarker string, skipExtract bool) (bundle.AnsibleBundleResult, error) {
	return workflowBundlePreparer(bundleDir, versionMarker, skipExtract)
}

func prepareWorkflowBundle(bundleDir, versionMarker string, skipExtract bool) (bundle.AnsibleBundleResult, error) {
	if skipExtract {
		return bundle.AnsibleBundleResult{Dir: bundleDir, Reused: true}, nil
	}
	return bundle.EnsureAnsibleBundle(bundleDir, versionMarker)
}

func SetWorkflowBundlePreparerForTest(preparer func(string, string, bool) (bundle.AnsibleBundleResult, error)) func() {
	previous := workflowBundlePreparer
	workflowBundlePreparer = preparer
	return func() { workflowBundlePreparer = previous }
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
