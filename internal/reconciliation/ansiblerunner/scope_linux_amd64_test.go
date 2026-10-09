//go:build linux && amd64

package ansiblerunner

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

func TestAnInvocationsCopiedRunnersProceedBesideTheirOwnJobs(t *testing.T) {
	runner := sweepingRunner(t, completing)
	ctx, release := runner.BeginInvocation(context.Background())
	defer release()
	first := runHeldIn(t, ctx, runner, adapterRequest(t, &bytes.Buffer{}))
	second := runHeldIn(t, ctx, runner, adapterRequest(t, &bytes.Buffer{}))
	if jobs, scratch := runNames(t, runner.jobParent), runNames(t, runner.scratchParent); len(jobs) != 2 || len(scratch) != 2 {
		t.Fatalf("siblings hold jobs %v and scratch %v", jobs, scratch)
	}
	if _, err := runner.Run(ctx, adapterRequest(t, &bytes.Buffer{})); err != nil {
		t.Fatal(err)
	}
	other, closeOther := runner.BeginInvocation(context.Background())
	defer closeOther()
	for _, foreign := range []context.Context{context.Background(), other} {
		_, err := runner.Run(foreign, adapterRequest(t, &bytes.Buffer{}))
		if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "lifecycle.adapter-running" {
			t.Fatalf("foreign invocation passed live ownership: %v", err)
		}
	}
	if err := first(); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if jobs := runNames(t, runner.jobParent); len(jobs) != 1 {
		t.Fatalf("releasing a sibling left jobs %v", jobs)
	}
	if err := second(); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	release()
	if _, err := runner.Run(ctx, adapterRequest(t, &bytes.Buffer{})); !errors.Is(err, context.Canceled) {
		t.Fatalf("released invocation admitted a run: %v", err)
	}
}

func TestAnEndedOwnedJobStillRefusesWhileItsDescendantHoldsIt(t *testing.T) {
	runner := sweepingRunner(t, completing)
	ctx, release := runner.BeginInvocation(context.Background())
	defer release()
	runner.invocation = ctx.Value(invocationKey{}).(*invocationScope)
	request := adapterRequest(t, &bytes.Buffer{})
	job, lock, holder, err := runner.claimRun(request)
	if err != nil {
		t.Fatal(err)
	}
	fd, err := syscall.Dup(int(lock.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	descendant := os.NewFile(uintptr(fd), "descendant")
	defer descendant.Close()
	runner.release(job, "", lock, holder)
	_, err = runner.Run(ctx, request)
	if found := diagnostics.Of(err); len(found) != 1 || found[0].Code != "lifecycle.adapter-running" || !strings.Contains(found[0].Message, "ended, but") {
		t.Fatalf("released ownership exempted a descendant: %v", err)
	}
	descendant.Close()
	if _, err := runner.Run(ctx, request); err != nil {
		t.Fatal(err)
	}
}

func TestOwnedJobsRequireTheirOriginalFilesAndReadableAttribution(t *testing.T) {
	for _, changed := range []string{lockName, holderName, recordName} {
		t.Run(changed, func(t *testing.T) {
			runner := sweepingRunner(t, completing)
			ctx, release := runner.BeginInvocation(context.Background())
			defer release()
			runner.invocation = ctx.Value(invocationKey{}).(*invocationScope)
			request := adapterRequest(t, &bytes.Buffer{})
			job, lock, holder, err := runner.claimRun(request)
			if err != nil {
				t.Fatal(err)
			}
			defer runner.release(job, "", lock, holder)
			path := filepath.Join(job, changed)
			if changed == recordName {
				if err := os.WriteFile(path, []byte(`{"context":"alpha","unknown":true}`), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Rename(path, path+".old"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, nil, 0600); err != nil {
					t.Fatal(err)
				}
				defer holdLock(t, path)()
			}
			if found := diagnostics.Of(runner.sweep(request.Context)); len(found) != 1 || found[0].Code != "lifecycle.adapter-running" {
				t.Fatalf("changed %s retained trusted ownership: %+v", changed, found)
			}
		})
	}
}

func TestSiblingClaimsSweepsAndReleasesDoNotRefuseEachOther(t *testing.T) {
	runner := sweepingRunner(t, completing)
	ctx, release := runner.BeginInvocation(context.Background())
	defer release()
	runner.invocation = ctx.Value(invocationKey{}).(*invocationScope)
	request := adapterRequest(t, &bytes.Buffer{})
	var workers sync.WaitGroup
	for range 3 {
		workers.Go(func() {
			for range 100 {
				job, lock, holder, err := runner.claimRun(request)
				if err != nil {
					t.Error(err)
					return
				}
				if err := runner.sweep(request.Context); err != nil {
					t.Error(err)
				}
				runner.release(job, "", lock, holder)
			}
		})
	}
	workers.Wait()
	if jobs := runNames(t, runner.jobParent); len(jobs) != 0 {
		t.Fatalf("settled siblings retained jobs %v", jobs)
	}
}

func TestTheRunnerPassesItsPrivateScratchOutsideTheFrozenRequest(t *testing.T) {
	var job string
	var written []byte
	runner := requestReader(t, &job, &written)
	command := runner.command
	var child *exec.Cmd
	runner.command = func(path string, arguments ...string) *exec.Cmd {
		child = command(path, arguments...)
		return child
	}
	request := adapterRequest(t, &bytes.Buffer{})
	if _, err := runner.Run(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	values := unmarked(t, decodeExact(t, written), "$").(map[string]any)
	scratch, ok := values["bootwright_run_scratch"].(string)
	if !ok || filepath.Dir(scratch) != runner.scratchParent || !strings.Contains(strings.Join(child.Env, "\n"), "TMPDIR="+scratch+"\n") {
		t.Fatalf("adapter scratch %v does not match its private process scratch", values["bootwright_run_scratch"])
	}
	if string(request.Canonical) != `{}` {
		t.Fatal("runner changed frozen request")
	}
}
