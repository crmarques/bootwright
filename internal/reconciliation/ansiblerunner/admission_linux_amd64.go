//go:build linux && amd64

package ansiblerunner

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

func (r Runner) checkExistingRuns(request lifecycle.RunRequest) error {
	if !request.Admission {
		if err := r.ensureScratchParent(true); err != nil {
			return err
		}
		return r.sweepLocked(request.Context)
	}
	jobs, err := os.OpenRoot(r.jobParent)
	if err != nil {
		return runnerStorage.Fail("private adapter invocation storage is unavailable", "")
	}
	defer jobs.Close()
	dirs := runDirectories{jobs: jobs, owner: r.owner}
	names, err := dirs.matching(jobs, jobSuffix)
	if err != nil {
		return r.unswept()
	}
	for _, suffix := range slices.Sorted(maps.Keys(names)) {
		name := jobPrefix + suffix
		free, state, held, err := dirs.seize(name, nil)
		if err != nil {
			return r.unswept()
		}
		if free != nil {
			free.close()
		}
		if state == jobHeld {
			if err := r.heldRefusal(request.Context, filepath.Join(r.jobParent, name, lockName), held); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *Runner) admissionAutomation(ctx context.Context, job string, request *lifecycle.RunRequest) (stagedAutomation, error) {
	if _, err := os.Lstat(r.scratchParent); errors.Is(err, os.ErrNotExist) {
		r.stagingParent, r.scratchParent = "", job
	}
	if err := r.ensureScratchParent(false); err != nil {
		return stagedAutomation{}, err
	}
	automation, err := r.stageAdmission(ctx, job)
	if err == nil {
		request.Launch.Environment = append(slices.Clone(request.Launch.Environment), "HOME="+job, "ANSIBLE_HOME="+filepath.Join(job, "ansible"))
	}
	return automation, err
}

func (r Runner) stageAdmission(ctx context.Context, job string) (stagedAutomation, error) {
	stager, ok := r.stager.(interface {
		StageForAdmission(context.Context, string) (prerequisites.StagedAutomation, error)
	})
	if !ok {
		return stagedAutomation{}, runnerBoundaryUnavailable.Fail("job-scoped automation staging is unavailable", setupRemediation)
	}
	return verifiedAutomation(stager.StageForAdmission(ctx, job))
}
