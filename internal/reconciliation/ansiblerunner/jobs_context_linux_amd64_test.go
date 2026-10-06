//go:build linux && amd64

package ansiblerunner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// blocking speaks the protocol, reports one group running and then waits on
// its authorization channel, so it runs until its invocation is cancelled.
func blocking() *exec.Cmd {
	return exec.Command("/bin/sh", "-c", `printf '{"phase":"loaded"}\n' >&3; read -r reply <&4; `+
		`printf '{"phase":"group","group":"wait","status":"running"}\n' >&3; read -r reply <&4`)
}

// runHeld starts a run of request whose adapter blocks, and returns once that
// adapter is running inside its job, with what ends the run and reports how
// it ended.
func runHeld(t *testing.T, runner Runner, request lifecycle.RunRequest) func() error {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	running := make(chan struct{})
	var once sync.Once
	request.Progress = func(context.Context, string, string) { once.Do(func() { close(running) }) }
	runner.command = func(string, ...string) *exec.Cmd { return blocking() }
	ended := make(chan error, 1)
	go func() {
		_, err := runner.Run(ctx, request)
		ended <- err
	}()
	stop := sync.OnceValue(func() error {
		cancel()
		return <-ended
	})
	t.Cleanup(func() { _ = stop() })
	select {
	case <-running:
	case <-time.After(10 * time.Second):
		t.Fatalf("the held run's adapter never reported: %v", stop())
	}
	return stop
}

// plantContextJob leaves a job of contextName as a killed invocation of this
// build would: its record names the context, it has its holder, and a bound
// secret file is still in it.
func plantContextJob(t *testing.T, parent, name, contextName string) string {
	return plant(t, filepath.Join(parent, name), map[string]string{
		lockName: "", holderName: "", "id": "PRIVATE KEY",
		recordName: `{"context":"` + contextName + `","block":"artifact-server-lab","description":"Serve ArtifactServer/artifact-server-lab",` +
			`"implementation":"artifact-server-nginx-v1","operation":"apply","machine":"controller","requestDigest":"sha256:5f"}`,
	})
}

// holdLock holds a planted job's lock, as a process still running in it does,
// until the test ends or the returned function lets it go.
func holdLock(t *testing.T, lock string) func() {
	t.Helper()
	file, err := os.Open(lock)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		t.Fatal(err)
	}
	release := sync.OnceFunc(func() { file.Close() })
	t.Cleanup(release)
	return release
}

// Two contexts' bounded runs proceed together: one context's adapter still
// running in its job refuses no run of another context, which completes and
// leaves the running job alone.
func TestTwoContextsRunsProceedTogether(t *testing.T) {
	runner := sweepingRunner(t, completing)
	stop := runHeld(t, runner, adapterRequest(t, &bytes.Buffer{}))
	held := runNames(t, runner.jobParent)
	if len(held) != 1 {
		t.Fatalf("the running adapter holds %v", held)
	}
	started := 0
	runner.command = func(string, ...string) *exec.Cmd { started++; return completing() }
	beta := adapterRequest(t, &bytes.Buffer{})
	beta.Context = "beta"
	if _, err := runner.Run(context.Background(), beta); err != nil || started != 1 {
		t.Fatalf("beside context alpha's running adapter, a run of beta started %d adapters and ended with %v", started, err)
	}
	if now := runNames(t, runner.jobParent); !slices.Equal(now, held) {
		t.Fatalf("the run of beta left %v, want alpha's running job %v alone", now, held)
	}
	if err := stop(); !errors.Is(err, context.Canceled) {
		t.Fatalf("the held run ended with %v", err)
	}
}

// A run of the same context refuses while its adapter runs, naming the held
// job's context, what it is doing, its operation and its Machine. The
// invocation that started it still holds its holder, so the only remedy is to
// wait: no lock is named and no process is to be ended.
func TestASameContextRunRefusesNamingTheHeldJob(t *testing.T) {
	runner := sweepingRunner(t, completing)
	stop := runHeld(t, runner, adapterRequest(t, &bytes.Buffer{}))
	started := false
	runner.command = func(string, ...string) *exec.Cmd { started = true; return completing() }
	_, err := runner.Run(context.Background(), adapterRequest(t, &bytes.Buffer{}))
	want := []diagnostics.Diagnostic{{
		Severity: "error", Code: "lifecycle.adapter-running",
		Message:     "context alpha is still running Serve ArtifactServer/artifact-server-lab (adapter operation apply on Machine controller)",
		Remediation: "wait for it to end, then repeat the command",
	}}
	if reported := diagnostics.Of(err); !reflect.DeepEqual(reported, want) || started {
		t.Fatalf("beside its own running adapter, a run of alpha started %t and reported %+v, want %+v", started, reported, want)
	}
	if err := stop(); !errors.Is(err, context.Canceled) {
		t.Fatalf("the held run ended with %v", err)
	}
	if jobs := runNames(t, runner.jobParent); len(jobs) != 0 {
		t.Fatalf("the ended run left %v", jobs)
	}
}

// Another context's job never passes for one an earlier build started while
// it is being claimed or removed: its record appears whole and only after its
// holder, and it goes before its holder. Context alpha claims and releases
// jobs as fast as it can while context beta sweeps, and no sweep refuses.
func TestASweepBesideAnotherContextsClaimsAndReleasesNeverRefuses(t *testing.T) {
	runner := sweepingRunner(t, completing)
	alpha := adapterRequest(t, &bytes.Buffer{})
	const claims = 2000
	claimed := make(chan error, 1)
	go func() {
		for range claims {
			job, err := os.MkdirTemp(runner.jobParent, jobPrefix)
			if err == nil {
				err = os.Chmod(job, 0700)
			}
			if err != nil {
				claimed <- err
				return
			}
			lock, holder, err := claim(job, alpha)
			if err != nil {
				claimed <- err
				return
			}
			runner.release(job, "", lock, holder)
		}
		claimed <- nil
	}()
	sweeps, refusals := 0, map[string]int{}
	for {
		select {
		case err := <-claimed:
			if err != nil || len(refusals) != 0 || sweeps == 0 {
				t.Fatalf("beside %d claims of alpha (%v), %d sweeps of beta refused %v", claims, err, sweeps, refusals)
			}
			return
		default:
		}
		sweeps++
		if err := runner.sweep("beta"); err != nil {
			for _, reported := range diagnostics.Of(err) {
				refusals[reported.Code+": "+reported.Message]++
			}
		}
	}
}

// claimJob creates and claims a job of request as a run does, with scratch
// paired with it that holds files, and returns all three with the job's locks.
func claimJob(t *testing.T, runner Runner, request lifecycle.RunRequest, files int) (string, string, *os.File, *os.File) {
	t.Helper()
	job, err := os.MkdirTemp(runner.jobParent, jobPrefix)
	if err == nil {
		err = os.Chmod(job, 0700)
	}
	if err != nil {
		t.Fatal(err)
	}
	lock, holder, err := claim(job, request)
	if err != nil {
		t.Fatal(err)
	}
	scratch, err := os.MkdirTemp(runner.scratchParent, scratchPrefix+strings.TrimPrefix(filepath.Base(job), jobPrefix)+"-")
	if err != nil {
		t.Fatal(err)
	}
	for n := range files {
		if err := os.WriteFile(filepath.Join(scratch, strconv.Itoa(n)), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return job, scratch, lock, holder
}

// A run never takes another context's sweep removing its own ended job for
// processes that job left. Context beta claims and releases one job at a
// time, each with scratch to remove, and sweeps before the next, as one apply
// runs its blocks, while context alpha sweeps as fast as it can; no sweep of
// either refuses.
func TestARunBesideAnotherContextsSweepsNeverRefusesOverItsEndedJob(t *testing.T) {
	runner := sweepingRunner(t, completing)
	beta := adapterRequest(t, &bytes.Buffer{})
	beta.Context = "beta"
	done := make(chan struct{})
	swept := make(chan map[string]int, 1)
	go func() {
		refusals := map[string]int{}
		for {
			select {
			case <-done:
				swept <- refusals
				return
			default:
			}
			for _, reported := range diagnostics.Of(runner.sweep("alpha")) {
				refusals[reported.Code+": "+reported.Message]++
			}
		}
	}()
	const runs = 1000
	refusals, contested := map[string]int{}, 0
	for range runs {
		job, scratch, lock, holder := claimJob(t, runner, beta, 32)
		runner.release(job, scratch, lock, holder)
		if _, err := os.Lstat(job); err == nil {
			contested++
		}
		for _, reported := range diagnostics.Of(runner.sweep("beta")) {
			refusals[reported.Code+": "+reported.Message]++
		}
	}
	close(done)
	if alpha := <-swept; len(refusals)+len(alpha) != 0 {
		t.Fatalf("over %d runs of beta, %d of whose ended jobs another sweep was removing, beta's sweeps refused %v and alpha's %v",
			runs, contested, refusals, alpha)
	}
	if contested == 0 {
		// On one processor alpha's sweep runs only between beta's removals,
		// so the race this test is for cannot happen there.
		if processors := runtime.GOMAXPROCS(0); processors < 2 {
			t.Skipf("on %d processor, no sweep of alpha caught one of beta's %d ended jobs being removed", processors, runs)
		}
		t.Fatalf("over %d runs of beta, no sweep of alpha caught one of its ended jobs being removed", runs)
	}
}

// plantAnsibleScratch leaves scratch whose job is gone, as a reboot that
// empties /run leaves it, holding the nested trees Ansible writes beneath an
// adapter's TMPDIR.
func plantAnsibleScratch(t *testing.T, parent, name string) string {
	t.Helper()
	files := map[string]string{}
	for tree := range 4 {
		for task := range 4 {
			for module := range 4 {
				files[fmt.Sprintf("ansible-local-%d/ansible-tmp-%d/AnsiballZ_%d.py", tree, task, module)] = "module"
			}
		}
	}
	return plant(t, filepath.Join(parent, name), files)
}

// Two contexts' sweeps that find one scratch whose job is gone never empty it
// together: the one that holds the scratch's own lock removes it, and the
// other leaves it to that one. Started together over one orphaned nested
// scratch, again and again, no sweep refuses and the scratch is gone after
// both.
func TestTwoContextsSweepsOverOneOrphanedScratchNeverRefuse(t *testing.T) {
	if processors := runtime.GOMAXPROCS(0); processors < 2 {
		t.Skipf("on %d processor two sweeps seldom run at once, so their race goes unexercised", processors)
	}
	runner := sweepingRunner(t, completing)
	const trials = 500
	refusals := map[string]int{}
	for trial := range trials {
		scratch := plantAnsibleScratch(t, runner.scratchParent, scratchPrefix+strconv.Itoa(trial+1)+"-1")
		start := make(chan struct{})
		var group sync.WaitGroup
		swept := make([]error, 2)
		for n, contextName := range []string{"alpha", "beta"} {
			group.Go(func() {
				<-start
				swept[n] = runner.sweep(contextName)
			})
		}
		close(start)
		group.Wait()
		for _, err := range swept {
			for _, reported := range diagnostics.Of(err) {
				refusals[reported.Code+": "+reported.Message]++
			}
		}
		if _, err := os.Lstat(scratch); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("trial %d: the orphaned scratch both sweeps found stayed: %v", trial, err)
		}
	}
	if len(refusals) != 0 {
		t.Fatalf("over %d trials, two contexts' sweeps over one orphaned scratch refused %v", trials, refusals)
	}
}

// A scratch whose job is gone and that another sweep holds for removal is
// left to that sweep: a run neither refuses nor empties it too, and the first
// run after that sweep lets it go removes it.
func TestAnOrphanedScratchAnotherSweepIsRemovingRefusesNoRun(t *testing.T) {
	started := 0
	runner := sweepingRunner(t, func() *exec.Cmd { started++; return completing() })
	scratch := plantAnsibleScratch(t, runner.scratchParent, scratchPrefix+"7070-1")
	before := trees(t, scratch)
	dirs, err := runner.openRunDirectories()
	if err != nil {
		t.Fatal(err)
	}
	defer dirs.close()
	root, _, ok := dirs.directory(dirs.scratch, filepath.Base(scratch))
	if !ok {
		t.Fatal("the planted scratch is not the runner's")
	}
	defer root.Close()
	lock, locked, err := lockDirectory(root)
	if !locked {
		t.Fatalf("no sweep could take the orphaned scratch: %v", err)
	}
	letGo := sync.OnceFunc(func() { lock.Close() })
	t.Cleanup(letGo)
	if _, err := runner.Run(context.Background(), adapterRequest(t, &bytes.Buffer{})); err != nil || started != 1 {
		t.Fatalf("beside a scratch another sweep is removing, a run started %d adapters and reported %+v", started, diagnostics.Of(err))
	}
	if after := trees(t, scratch); !slices.Equal(after, before) {
		t.Fatalf("the run emptied the scratch another sweep holds from %v to %v", before, after)
	}
	letGo()
	if _, err := runner.Run(context.Background(), adapterRequest(t, &bytes.Buffer{})); err != nil {
		t.Fatalf("the run after the other sweep let the scratch go ended with %v", err)
	}
	if scratches := runNames(t, runner.scratchParent); len(scratches) != 0 {
		t.Fatalf("the orphaned scratch no sweep held any more stayed: %v", scratches)
	}
}

// A job of a run's own context that another sweep is removing holds none of
// its processes. That sweep holds the job's lock shared, which it can only
// once no process of the job runs, and the job directory's lock exclusively,
// so a run of that context, like one of any other, neither refuses nor
// removes the job too; the first run after that sweep lets it go removes it.
func TestAJobAnotherSweepIsRemovingRefusesNoRun(t *testing.T) {
	started := 0
	runner := sweepingRunner(t, func() *exec.Cmd { started++; return completing() })
	beta := adapterRequest(t, &bytes.Buffer{})
	beta.Context = "beta"
	job, scratch, lock, holder := claimJob(t, runner, beta, 1)
	// Its invocation has ended as release ends it, and another sweep has
	// taken it for removal.
	lock.Close()
	holder.Close()
	dirs, err := runner.openRunDirectories()
	if err != nil {
		t.Fatal(err)
	}
	defer dirs.close()
	seized, state, found, err := dirs.seize(filepath.Base(job), nil)
	if seized == nil {
		t.Fatalf("no sweep could take beta's ended job: %v, %+v, %v", state, found, err)
	}
	letGo := sync.OnceFunc(seized.close)
	t.Cleanup(letGo)
	for _, contextName := range []string{"beta", "alpha"} {
		request := adapterRequest(t, &bytes.Buffer{})
		request.Context = contextName
		if _, err := runner.Run(context.Background(), request); err != nil {
			t.Fatalf("beside its job another sweep is removing, a run of %s reported %+v", contextName, diagnostics.Of(err))
		}
	}
	if jobs, scratches := runNames(t, runner.jobParent), runNames(t, runner.scratchParent); started != 2 ||
		!slices.Equal(jobs, []string{filepath.Base(job)}) || !slices.Equal(scratches, []string{filepath.Base(scratch)}) {
		t.Fatalf("the runs started %d adapters and left %v and %v; the job another sweep holds and its scratch must stay", started, jobs, scratches)
	}
	letGo()
	if _, err := runner.Run(context.Background(), beta); err != nil {
		t.Fatalf("the run after the other sweep let the job go ended with %v", err)
	}
	if jobs, scratches := runNames(t, runner.jobParent), runNames(t, runner.scratchParent); len(jobs)+len(scratches) != 0 {
		t.Fatalf("the job no sweep held any more stayed: %v and %v", jobs, scratches)
	}
}

// What a job found held says about itself stands only while its lock is
// still held once its record and holder are read. Its processes may let it go
// in between, and then a sweep may remove its holder, or its invocation may
// have freed its holder only by ending. Read under its lock, a job of beta
// whose invocation ended names itself and the processes it left; read after
// its lock was let go, with or without its holder, it is held by nothing.
func TestAHeldJobSaysWhatItIsOnlyWhileItsLockIsStillHeld(t *testing.T) {
	runner := sweepingRunner(t, completing)
	job := plantContextJob(t, runner.jobParent, jobPrefix+"6161", "beta")
	dirs, err := runner.openRunDirectories()
	if err != nil {
		t.Fatal(err)
	}
	defer dirs.close()
	read := func() (holding, bool) {
		t.Helper()
		root, _, ok := dirs.directory(dirs.jobs, filepath.Base(job))
		if !ok {
			t.Fatal("the planted job is not the runner's")
		}
		defer root.Close()
		record, _, recorded := dirs.file(root, recordName)
		lock, _, locked := dirs.file(root, lockName)
		if !recorded || !locked {
			t.Fatalf("the planted job's record (%t) or lock (%t) is not the runner's", recorded, locked)
		}
		defer record.Close()
		defer lock.Close()
		found, held, err := dirs.holding(root, record, lock)
		if err != nil {
			t.Fatal(err)
		}
		return found, held
	}
	letGo := holdLock(t, filepath.Join(job, lockName))
	if found, held := read(); !held || !found.attributed || found.alive || found.record.Context != "beta" {
		t.Fatalf("read under its lock, beta's ended job says %+v, held %t", found, held)
	}
	letGo()
	if found, held := read(); held || found != (holding{}) {
		t.Fatalf("read after its lock was let go, beta's job says %+v, held %t", found, held)
	}
	if err := os.Remove(filepath.Join(job, holderName)); err != nil {
		t.Fatal(err)
	}
	if found, held := read(); held || found != (holding{}) {
		t.Fatalf("read after its lock was let go and its holder removed, beta's job says %+v, held %t", found, held)
	}
}

// A held job its record cannot attribute to a context and its holder, as one
// an earlier build started, refuses a run of every context, naming its lock;
// once nothing holds it, the next run removes it.
func TestAJobAnEarlierBuildStartedRefusesEveryContext(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"a record naming no context and no holder": {
			lockName: "", recordName: `{"implementation":"artifact-server-nginx-v1","operation":"apply","machine":"controller","requestDigest":"sha256:5f"}`,
		},
		"a record naming a context but no holder": {
			lockName: "", recordName: `{"context":"beta","block":"","description":"","implementation":"artifact-server-nginx-v1","operation":"apply","machine":"controller","requestDigest":"sha256:5f"}`,
		},
		"an unreadable record": {lockName: "", holderName: "", recordName: "not a record"},
		"a record with a field it does not know": {
			lockName: "", holderName: "", recordName: `{"context":"beta","implementation":"artifact-server-nginx-v1","pid":7}`,
		},
		"a record naming an invalid context": {
			lockName: "", holderName: "", recordName: `{"context":"../beta","implementation":"artifact-server-nginx-v1"}`,
		},
		"a record past its bound": {
			lockName: "", holderName: "", recordName: `{"context":"beta","description":"` + strings.Repeat("d", maxRecordBytes) + `"}`,
		},
	} {
		t.Run(name, func(t *testing.T) {
			started := 0
			runner := sweepingRunner(t, func() *exec.Cmd { started++; return completing() })
			job := plant(t, filepath.Join(runner.jobParent, jobPrefix+"777"), files)
			lock := filepath.Join(job, lockName)
			release := holdLock(t, lock)
			want := []diagnostics.Diagnostic{{
				Severity: "error", Code: "lifecycle.adapter-running",
				Message:     "an earlier lifecycle adapter still runs and holds its job lock",
				Remediation: "wait for it to end, or end the processes that hold " + lock + ", then repeat the command",
			}}
			for _, contextName := range []string{"alpha", "beta"} {
				request := adapterRequest(t, &bytes.Buffer{})
				request.Context = contextName
				_, err := runner.Run(context.Background(), request)
				if reported := diagnostics.Of(err); !reflect.DeepEqual(reported, want) || started != 0 {
					t.Fatalf("a run of %s started %d adapters and reported %+v, want %+v", contextName, started, reported, want)
				}
			}
			release()
			if _, err := runner.Run(context.Background(), adapterRequest(t, &bytes.Buffer{})); err != nil || started != 1 {
				t.Fatalf("the run after the job was let go started %d adapters and ended with %v", started, err)
			}
			if jobs := runNames(t, runner.jobParent); len(jobs) != 0 {
				t.Fatalf("the job no process held stayed: %v", jobs)
			}
		})
	}
}

// A run that names no valid context could hold a job no sweep attributes, so
// it refuses before the runner touches its run directories: no job or scratch
// is created, no adapter starts, and a stale job the run would first have
// swept is left as it was.
func TestARunNamingNoContextRefusesBeforeAnyJobExists(t *testing.T) {
	for _, contextName := range []string{"", "Alpha", "../alpha", "alpha beta", " alpha", strings.Repeat("a", 64)} {
		t.Run(contextName, func(t *testing.T) {
			started := false
			runner := sweepingRunner(t, func() *exec.Cmd { started = true; return completing() })
			plantJob(t, runner.jobParent, jobPrefix+"4242")
			before := trees(t, runner.jobParent, runner.scratchParent)
			request := adapterRequest(t, &bytes.Buffer{})
			request.Context = contextName
			_, err := runner.Run(context.Background(), request)
			want := []diagnostics.Diagnostic{{Severity: "error", Code: "lifecycle.state", Message: "the adapter run names no context"}}
			if reported := diagnostics.Of(err); !reflect.DeepEqual(reported, want) || started {
				t.Fatalf("a run naming %q started %t and reported %+v", contextName, started, reported)
			}
			if after := trees(t, runner.jobParent, runner.scratchParent); !slices.Equal(after, before) {
				t.Fatalf("the refused run changed its run directories from %v to %v", before, after)
			}
		})
	}
}

// A run whose job record a sweep could not read back, one carrying a control
// character a refusal would print or one past the record's bound, refuses the
// same way, before any job exists.
func TestARunWhoseIdentityCannotBeRecordedRefusesBeforeAnyJobExists(t *testing.T) {
	for name, change := range map[string]func(*lifecycle.RunRequest){
		"a terminal escape in its description": func(request *lifecycle.RunRequest) { request.Description = "Serve \x1b[2J" },
		"a line break in its block":            func(request *lifecycle.RunRequest) { request.Block = "artifact\nserver" },
		"invalid text in its description":      func(request *lifecycle.RunRequest) { request.Description = "Serve \xff" },
		"a record past its bound":              func(request *lifecycle.RunRequest) { request.Description = strings.Repeat("d", maxRecordBytes) },
	} {
		t.Run(name, func(t *testing.T) {
			started := false
			runner := sweepingRunner(t, func() *exec.Cmd { started = true; return completing() })
			plantJob(t, runner.jobParent, jobPrefix+"4242")
			before := trees(t, runner.jobParent, runner.scratchParent)
			request := adapterRequest(t, &bytes.Buffer{})
			change(&request)
			_, err := runner.Run(context.Background(), request)
			want := []diagnostics.Diagnostic{{Severity: "error", Code: "lifecycle.state", Message: "the adapter run's identity cannot be recorded in its job"}}
			if reported := diagnostics.Of(err); !reflect.DeepEqual(reported, want) || started {
				t.Fatalf("the run started %t and reported %+v", started, reported)
			}
			if after := trees(t, runner.jobParent, runner.scratchParent); !slices.Equal(after, before) {
				t.Fatalf("the refused run changed its run directories from %v to %v", before, after)
			}
		})
	}
}

// The sweep stays host-wide for what no process holds: a run of one context
// removes another context's unheld job, with the secret file in it, and its
// scratch, exactly as it removes its own.
func TestTheSweepRemovesAnotherContextsUnheldJob(t *testing.T) {
	runner := sweepingRunner(t, completing)
	plantContextJob(t, runner.jobParent, jobPrefix+"5252", "beta")
	plant(t, filepath.Join(runner.scratchParent, scratchPrefix+"5252-3"), map[string]string{"image/boot.iso": "image"})
	if _, err := runner.Run(context.Background(), adapterRequest(t, &bytes.Buffer{})); err != nil {
		t.Fatalf("the run of alpha beside beta's unheld job failed: %v", err)
	}
	if jobs, scratch := runNames(t, runner.jobParent), runNames(t, runner.scratchParent); len(jobs)+len(scratch) != 0 {
		t.Fatalf("another context's unheld job and its scratch stayed: %v and %v", jobs, scratch)
	}
}
