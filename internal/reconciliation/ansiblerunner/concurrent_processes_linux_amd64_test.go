//go:build linux && amd64

package ansiblerunner

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/dnsserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/ntpserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/proxy"
	"github.com/crmarques/bootwright/internal/managedos/installation"
	"github.com/crmarques/bootwright/internal/substrate/libvirt"
)

// The compiled graph has its own composition test. Here real adapter processes
// report arrival and block on private pipes, proving admission and isolation at
// the process boundary for each concurrent wave of a three-Machine operation.
func TestThreeMachineWavesRunInOneInvocationWithPrivateScratch(t *testing.T) {
	waves := [][]string{
		{proxy.Implementation, dnsserver.Implementation, ntpserver.Implementation, artifactserver.Implementation},
		{libvirt.MachineImplementation, libvirt.MachineImplementation, libvirt.MachineImplementation},
		{installation.TreeImplementation},
		{installation.Implementation, installation.Implementation, installation.Implementation},
	}
	for _, verb := range []string{"apply", "destroy"} {
		t.Run(verb, func(t *testing.T) {
			runner := sweepingRunner(t, completing)
			ordered := slices.Clone(waves)
			if verb == "destroy" {
				slices.Reverse(ordered)
			}
			for _, wave := range ordered {
				for _, implementation := range wave {
					runner.playbooks[implementation+"/"+verb] = "controlled.yml"
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			ctx, release := runner.BeginInvocation(ctx)
			defer release()
			seenScratch := map[string]bool{}
			for _, wave := range ordered {
				runControlledWave(t, ctx, runner, wave, verb, seenScratch)
			}
			if len(seenScratch) != 11 {
				t.Fatalf("eleven adapters used %d distinct scratch directories", len(seenScratch))
			}
		})
	}
}

func runControlledWave(t *testing.T, ctx context.Context, runner Runner, implementations []string, verb string, seen map[string]bool) {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	type heldProcess struct {
		input, release *os.File
		scratch        string
	}
	var mu sync.Mutex
	var held []heldProcess
	defer func() {
		cancel()
		workers.Wait()
		for _, process := range held {
			process.input.Close()
			process.release.Close()
		}
	}()
	runner.command = func(_ string, args ...string) *exec.Cmd {
		input, release, err := os.Pipe()
		if err != nil {
			t.Error(err)
			return completing()
		}
		var scratch string
		for _, argument := range args {
			if file, ok := strings.CutPrefix(argument, "@"); ok {
				data, err := os.ReadFile(file)
				var values map[string]json.RawMessage
				if err != nil || json.Unmarshal(data, &values) != nil {
					t.Error("adapter variables are unreadable")
				}
				var marked struct {
					Value string `json:"__ansible_unsafe"`
				}
				if json.Unmarshal(values["bootwright_run_scratch"], &marked) != nil {
					t.Error("adapter scratch is unavailable")
				}
				scratch = marked.Value
			}
		}
		mu.Lock()
		held = append(held, heldProcess{input, release, scratch})
		mu.Unlock()
		command := exec.Command("/bin/sh", "-c", `printf '{"phase":"loaded"}\n' >&3; read -r reply <&4; `+
			`printf '{"group":"wait","phase":"group","status":"running"}\n' >&3; read -r release; `+
			`printf '{"evidence":{"absent":false},"outcome":"changed","phase":"completed"}\n' >&3`)
		command.Stdin = input
		return command
	}
	arrived, done := make(chan struct{}, len(implementations)), make(chan error, len(implementations))
	for _, implementation := range implementations {
		request := adapterRequest(t, &bytes.Buffer{})
		request.Implementation, request.Operation = implementation, verb
		request.Progress = func(context.Context, string, string) { arrived <- struct{}{} }
		workers.Go(func() {
			_, err := runner.Run(ctx, request)
			done <- err
		})
	}
	for range implementations {
		select {
		case <-arrived:
		case err := <-done:
			t.Fatalf("adapter ended before every sibling reached the barrier: %v", err)
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	mu.Lock()
	processes := slices.Clone(held)
	mu.Unlock()
	if len(processes) != len(implementations) {
		t.Fatalf("started %d processes for %d siblings", len(processes), len(implementations))
	}
	for _, process := range processes {
		stat, err := os.Stat(process.scratch)
		if err != nil || !stat.IsDir() || stat.Mode().Perm() != 0700 || filepath.Dir(process.scratch) != runner.scratchParent || seen[process.scratch] {
			t.Fatalf("adapter scratch %q is shared or not private: %v", process.scratch, err)
		}
		seen[process.scratch] = true
	}
	for _, process := range processes {
		process.release.Close()
	}
	for range implementations {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if jobs, scratch := runNames(t, runner.jobParent), runNames(t, runner.scratchParent); len(jobs)+len(scratch) != 0 {
		t.Fatalf("settled wave retained jobs %v or scratch %v", jobs, scratch)
	}
}
