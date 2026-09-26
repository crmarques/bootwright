//go:build linux && amd64

package ansiblerunner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/crmarques/bootwright/ansible"
	"github.com/crmarques/bootwright/internal/controller/prerequisites"
	"github.com/crmarques/bootwright/internal/diagnostics"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// leaveAnOrphan completes its run but leaves a descendant in a session of its
// own that holds nothing but the job lock it inherited, as an Ansible worker
// left by a supervisor killed on its own would. It prints that descendant.
func leaveAnOrphan(result, authorization *os.File) {
	syscall.CloseOnExec(3)
	syscall.CloseOnExec(4)
	orphan := exec.Command(os.Args[0], "-test.run=^TestLifecycleAdapterChild$", "--", "lifecycle-child-member")
	orphan.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if orphan.Start() != nil {
		os.Exit(20)
	}
	fmt.Printf("orphan %d\n", orphan.Process.Pid)
	_, _ = result.Write([]byte(`{"phase":"loaded"}` + "\n"))
	_, _ = authorization.Read(make([]byte, 16))
	_, _ = result.Write([]byte(`{"phase":"completed","outcome":"changed","evidence":{}}` + "\n"))
	os.Exit(0)
}

// completing speaks the protocol and completes, holding nothing afterwards.
func completing() *exec.Cmd {
	return exec.Command("/bin/sh", "-c", `printf '{"phase":"loaded"}\n' >&3; read -r reply <&4; `+
		`printf '{"phase":"completed","outcome":"changed","evidence":{}}\n' >&3`)
}

// sweepingRunner owns the run directories it creates, in parents of its own.
func sweepingRunner(t *testing.T, adapter func() *exec.Cmd) Runner {
	return Runner{
		jobParent: t.TempDir(), scratchParent: t.TempDir(), owner: uint32(os.Geteuid()), drain: 200 * time.Millisecond,
		playbooks: map[string]string{"artifact-server-nginx-v1/apply": "apply.yml"},
		command:   func(string, ...string) *exec.Cmd { return adapter() },
	}
}

func adapterRequest(t *testing.T, output prerequisites.RunOutput) lifecycle.RunRequest {
	bundle := t.TempDir()
	if err := os.Mkdir(filepath.Join(bundle, "automation"), 0700); err != nil {
		t.Fatal(err)
	}
	return lifecycle.RunRequest{
		Implementation: "artifact-server-nginx-v1", Operation: "apply", Variable: "bootwright_artifact_server",
		Digest:    "sha256:" + strings.Repeat("5f", 32),
		Canonical: []byte(`{}`), Placement: machineref.Placement{Connection: "local", Machine: "controller"},
		Launch: prerequisites.PythonLaunch{Loader: "/qualified/loader"},
		Bundle: prerequisites.BundleLocation{Path: bundle}, Area: embeddedArea{files: ansible.Assets()}, Output: output,
	}
}

func runNames(t *testing.T, parent string) []string {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), jobPrefix) {
			names = append(names, entry.Name())
		}
	}
	return names
}

// plant leaves a run directory as a killed invocation would, with its files.
func plant(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	for name, content := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// plantJob leaves a job whose lock no process holds, with a bound secret file
// and Ansible's temporary tree in it.
func plantJob(t *testing.T, parent, name string) string {
	return plant(t, filepath.Join(parent, name), map[string]string{
		lockName: "", recordName: `{"implementation":"artifact-server-nginx-v1"}`, "id": "PRIVATE KEY",
		"local/ansible-local-1/AnsiballZ_setup.py": "module",
	})
}

// lockHeld reports whether any process holds a job's lock. The lock is free
// only once the last thread of its last holder has gone, which can be after
// that holder's leader is already a zombie.
func lockHeld(t *testing.T, lock string) bool {
	t.Helper()
	file, err := os.Open(lock)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil && !errors.Is(err, syscall.EWOULDBLOCK) {
		t.Fatal(err)
	}
	return err != nil
}

func codeOf(err error) (string, string) {
	for _, reported := range diagnostics.Of(err) {
		return reported.Code, reported.Remediation
	}
	return "", ""
}

// An adapter's descendant that outlives its run still holds the job lock it
// inherited, as an Ansible worker left by a supervisor killed on its own does.
// Its job, which records what it is for, and its scratch stay. The next run
// refuses naming the lock and starts no adapter, yet still removes a free stale
// job and scratch whose job is gone. The first run after the descendant ends
// removes both.
func TestAnAdapterStillRunningRefusesTheNextRun(t *testing.T) {
	adapter := func() *exec.Cmd {
		return exec.Command(os.Args[0], "-test.run=^TestLifecycleAdapterChild$", "--", "lifecycle-child-orphaning")
	}
	started := 0
	runner := sweepingRunner(t, func() *exec.Cmd { started++; return adapter() })
	var output bytes.Buffer
	request := adapterRequest(t, &output)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if _, err := runner.Run(ctx, request); err != nil {
		t.Fatalf("the adapter that left a descendant failed: %v (%q)", err, output.String())
	}
	var orphan int
	if _, err := fmt.Sscanf(output.String(), "orphan %d\n", &orphan); err != nil {
		t.Fatalf("the adapter left no descendant: %q (%v)", output.String(), err)
	}
	t.Cleanup(func() { _ = syscall.Kill(orphan, syscall.SIGKILL) })
	jobs, scratch := runNames(t, runner.jobParent), runNames(t, runner.scratchParent)
	if len(jobs) != 1 || len(scratch) != 1 {
		t.Fatalf("run directories = %v and %v; a job whose lock is still held must stay", jobs, scratch)
	}
	job := filepath.Join(runner.jobParent, jobs[0])
	for _, name := range []string{lockName, recordName} {
		if info, err := os.Lstat(filepath.Join(job, name)); err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			t.Fatalf("the job's %s is not a private regular file: %v", name, err)
		}
	}
	var record jobRecord
	data, err := os.ReadFile(filepath.Join(job, recordName))
	if err == nil {
		err = json.Unmarshal(data, &record)
	}
	if err != nil {
		t.Fatalf("the job's record is unreadable: %v", err)
	}
	if want := (jobRecord{
		Implementation: request.Implementation, Operation: request.Operation,
		Machine: request.Placement.Machine, RequestDigest: request.Digest,
	}); record != want {
		t.Fatalf("the job records %+v, not %+v", record, want)
	}
	// A refused run still sweeps: a free stale job listed after the held one,
	// with its secret file and scratch, and scratch whose job is gone.
	plantJob(t, runner.jobParent, "bootwright-run-999999999")
	plant(t, filepath.Join(runner.scratchParent, "bootwright-run-scratch-999999999-1"), map[string]string{"snapshot": "state"})
	plant(t, filepath.Join(runner.scratchParent, "bootwright-run-scratch-999999998-1"), map[string]string{"snapshot": "state"})
	adapter = completing
	_, err = runner.Run(ctx, request)
	code, remediation := codeOf(err)
	if code != "lifecycle.adapter-running" || started != 1 {
		t.Fatalf("beside a running adapter, a run started %d adapters and ended with %v", started, err)
	}
	lock := filepath.Join(job, lockName)
	if !strings.Contains(remediation, lock) {
		t.Fatalf("the remedy %q does not name the held lock %s", remediation, lock)
	}
	if now, nowScratch := runNames(t, runner.jobParent), runNames(t, runner.scratchParent); !slices.Equal(now, jobs) || !slices.Equal(nowScratch, scratch) {
		t.Fatalf("the refused run left %v and %v; only the held job %v and its scratch %v may stay", now, nowScratch, jobs, scratch)
	}
	_ = syscall.Kill(orphan, syscall.SIGKILL)
	for deadline := time.Now().Add(5 * time.Second); lockHeld(t, lock); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the orphaned descendant did not let its lock go")
		}
	}
	if _, err := runner.Run(ctx, request); err != nil {
		t.Fatalf("the run after the descendant ended failed: %v", err)
	}
	if jobs, scratch := runNames(t, runner.jobParent), runNames(t, runner.scratchParent); len(jobs)+len(scratch) != 0 {
		t.Fatalf("run directories left behind: %v and %v", jobs, scratch)
	}
}

// A job whose lock no process holds is what a killed invocation left, bound
// secret file included. The next run removes it with its scratch, and scratch
// whose job is gone, as after a reboot empties /run. A link inside goes as
// itself; what it names stays.
func TestAStaleRunDirectoryIsRemovedWithItsSecretFiles(t *testing.T) {
	runner := sweepingRunner(t, completing)
	kept := plant(t, t.TempDir(), map[string]string{"keep": "not the runner's"})
	job := plantJob(t, runner.jobParent, "bootwright-run-4242")
	if err := os.Symlink(kept, filepath.Join(job, "local", "escape")); err != nil {
		t.Fatal(err)
	}
	plant(t, filepath.Join(runner.scratchParent, "bootwright-run-scratch-4242-7"), map[string]string{"image/boot.iso": "image"})
	plant(t, filepath.Join(runner.scratchParent, "bootwright-run-scratch-5151-9"), map[string]string{"snapshot": "state"})
	var output bytes.Buffer
	if _, err := runner.Run(context.Background(), adapterRequest(t, &output)); err != nil {
		t.Fatalf("the run beside a stale job failed: %v", err)
	}
	if jobs, scratch := runNames(t, runner.jobParent), runNames(t, runner.scratchParent); len(jobs)+len(scratch) != 0 {
		t.Fatalf("stale run directories left behind: %v and %v", jobs, scratch)
	}
	if _, err := os.Stat(filepath.Join(kept, "keep")); err != nil {
		t.Fatalf("removing a stale job followed a link inside it: %v", err)
	}
}

// Only the runner's own entries are swept: exact names, private directories of
// its owner, whose lock and record are private regular files with one link. A
// link is never followed, even to a stale-looking job it could reach, and a job
// without its record is still being created. None of them is removed, and none
// refuses.
func TestTheSweepNeverFollowsALinkOrTouchesWhatIsNotItsOwn(t *testing.T) {
	runner := sweepingRunner(t, completing)
	plantJob(t, runner.jobParent, "decoy")
	plant(t, filepath.Join(runner.scratchParent, "decoy"), map[string]string{"snapshot": "state"})
	for _, link := range []string{filepath.Join(runner.jobParent, "bootwright-run-11"), filepath.Join(runner.scratchParent, "bootwright-run-scratch-12-1")} {
		if err := os.Symlink("decoy", link); err != nil {
			t.Fatal(err)
		}
	}
	linked := plantJob(t, runner.jobParent, "bootwright-run-13")
	plant(t, linked, map[string]string{"lock-target": ""})
	if err := os.Remove(filepath.Join(linked, lockName)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("lock-target", filepath.Join(linked, lockName)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(plantJob(t, runner.jobParent, "bootwright-run-14"), recordName)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(plantJob(t, runner.jobParent, "bootwright-run-15"), 0755); err != nil {
		t.Fatal(err)
	}
	plantJob(t, runner.jobParent, "bootwright-run-007")
	plantJob(t, runner.jobParent, "bootwright-run-4294967296")
	// Scratch names that are not exactly a job's and a random part, none of
	// which has a job, in a parent anyone may write to.
	for _, name := range []string{"17", "17-x", "017-1", "17-1-1", "17-01", "17-4294967296", "17-"} {
		plant(t, filepath.Join(runner.scratchParent, scratchPrefix+name), map[string]string{"snapshot": "state"})
	}
	// A lock or record with a second link elsewhere is not the job's alone.
	for job, name := range map[string]string{"bootwright-run-18": lockName, "bootwright-run-19": recordName} {
		elsewhere := plant(t, filepath.Join(runner.jobParent, "elsewhere-"+name), map[string]string{name: ""})
		if err := os.Remove(filepath.Join(plantJob(t, runner.jobParent, job), name)); err != nil {
			t.Fatal(err)
		}
		if err := os.Link(filepath.Join(elsewhere, name), filepath.Join(runner.jobParent, job, name)); err != nil {
			t.Fatal(err)
		}
	}
	before := trees(t, runner.jobParent, runner.scratchParent)
	var output bytes.Buffer
	if _, err := runner.Run(context.Background(), adapterRequest(t, &output)); err != nil {
		t.Fatalf("entries that are not the runner's refused the run: %v", err)
	}
	if after := trees(t, runner.jobParent, runner.scratchParent); !slices.Equal(before, after) {
		t.Fatalf("the sweep changed what is not its own:\nbefore %v\nafter  %v", before, after)
	}

	t.Run("owned by another identity", func(t *testing.T) {
		foreign := sweepingRunner(t, completing)
		foreign.owner++
		job := plantJob(t, foreign.jobParent, "bootwright-run-16")
		if _, err := foreign.Run(context.Background(), adapterRequest(t, &output)); err != nil {
			t.Fatalf("a foreign entry refused the run: %v", err)
		}
		if _, err := os.Stat(filepath.Join(job, "id")); err != nil {
			t.Fatalf("the sweep removed an entry another identity owns: %v", err)
		}
	})
}

// An invocation removes its job only through the lock it created. A lock file
// replaced while its adapter ran says nothing of the processes that hold the
// original, so the job and its scratch stay for them, and the next run decides
// on the lock it then finds.
func TestAnInvocationKeepsAJobWhoseLockWasReplaced(t *testing.T) {
	runner := sweepingRunner(t, completing)
	runner.command = func(_ string, arguments ...string) *exec.Cmd {
		job := filepath.Dir(arguments[slices.Index(arguments, "-i")+1])
		return exec.Command("/bin/sh", "-c", `printf '{"phase":"loaded"}\n' >&3; read -r reply <&4; `+
			`rm "$1/lock" && (umask 077 && : >"$1/lock") && `+
			`printf '{"phase":"completed","outcome":"changed","evidence":{}}\n' >&3`, "adapter", job)
	}
	var output bytes.Buffer
	if _, err := runner.Run(context.Background(), adapterRequest(t, &output)); err != nil {
		t.Fatalf("the adapter that replaced its lock failed: %v (%q)", err, output.String())
	}
	jobs, scratch := runNames(t, runner.jobParent), runNames(t, runner.scratchParent)
	if len(jobs) != 1 || len(scratch) != 1 {
		t.Fatalf("run directories = %v and %v; a job whose lock is not the one its invocation made must stay", jobs, scratch)
	}
	runner.command = func(string, ...string) *exec.Cmd { return completing() }
	if _, err := runner.Run(context.Background(), adapterRequest(t, &output)); err != nil {
		t.Fatalf("the run after it failed: %v", err)
	}
	if jobs, scratch := runNames(t, runner.jobParent), runNames(t, runner.scratchParent); len(jobs)+len(scratch) != 0 {
		t.Fatalf("run directories left behind: %v and %v", jobs, scratch)
	}
}

// A job the sweep cannot remove whole, here one holding a tree deeper than a
// sweep descends, keeps its record and lock whatever order its entries are
// listed in, so every later run fails closed rather than passing it as a job
// still being created.
func TestAJobThatCannotBeRemovedKeepsFailingClosed(t *testing.T) {
	runner := sweepingRunner(t, completing)
	job, last := plantJobListedLast(t, runner.jobParent, "bootwright-run-4545")
	nest(t, last, maxRunTreeDepth)
	var output bytes.Buffer
	for range 2 {
		_, err := runner.Run(context.Background(), adapterRequest(t, &output))
		if code, _ := codeOf(err); code != "lifecycle.state" {
			t.Fatalf("a stale job deeper than a sweep descends ended with %v", err)
		}
	}
	for _, name := range []string{recordName, lockName} {
		if _, err := os.Lstat(filepath.Join(job, name)); err != nil {
			t.Fatalf("the job that could not be removed lost its %s: %v", name, err)
		}
	}
}

// nest makes a chain of directories so many levels beneath a directory that is
// itself one level beneath a job.
func nest(t *testing.T, dir string, levels int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(append([]string{dir}, slices.Repeat([]string{"d"}, levels)...)...), 0700); err != nil {
		t.Fatal(err)
	}
}

// plantJobListedLast plants a stale job holding a directory that the job's
// listing puts after both its record and its lock, and returns both. The
// listing order is the filesystem's own: name hash order on ext4, newest first
// on tmpfs, oldest first on others. Trees are made before and after the record
// and lock until one is listed after them.
func plantJobListedLast(t *testing.T, parent, name string) (string, string) {
	t.Helper()
	job := filepath.Join(parent, name)
	trees := func(batch int) {
		for n := range 16 {
			if err := os.MkdirAll(filepath.Join(job, fmt.Sprintf("tree-%d-%d", batch, n)), 0700); err != nil {
				t.Fatal(err)
			}
		}
	}
	trees(0)
	plantJob(t, parent, name)
	var names []string
	for batch := 1; batch <= 64; batch++ {
		listing, err := os.Open(job)
		if err != nil {
			t.Fatal(err)
		}
		names, err = listing.Readdirnames(-1)
		listing.Close()
		if err != nil {
			t.Fatal(err)
		}
		last := max(slices.Index(names, recordName), slices.Index(names, lockName))
		for _, entry := range names[last+1:] {
			if strings.HasPrefix(entry, "tree-") {
				return job, filepath.Join(job, entry)
			}
		}
		trees(batch)
	}
	// A filesystem that lists both after every tree tried still gets a job
	// that cannot be removed; only the order goes unexercised.
	t.Logf("the listing %v puts the record and lock after every tree", names)
	return job, filepath.Join(job, "tree-0-0")
}

// A sweep reads no more than it declares: so many run directories in one
// parent, and so many levels and entries in one stale tree, each bound
// inclusive. Past any of them it fails closed, and a job it could not finish
// keeps its record and lock.
func TestTheSweepRefusesBeyondItsBounds(t *testing.T) {
	var output bytes.Buffer
	t.Run("run directories", func(t *testing.T) {
		runner := sweepingRunner(t, completing)
		for n := range maxRunDirectories {
			if err := os.Mkdir(filepath.Join(runner.jobParent, jobPrefix+strconv.Itoa(n+1)), 0700); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := runner.Run(context.Background(), adapterRequest(t, &output)); err != nil {
			t.Fatalf("a parent holding as many run directories as the bound refused: %v", err)
		}
		if err := os.Mkdir(filepath.Join(runner.jobParent, jobPrefix+strconv.Itoa(maxRunDirectories+1)), 0700); err != nil {
			t.Fatal(err)
		}
		_, err := runner.Run(context.Background(), adapterRequest(t, &output))
		if code, _ := codeOf(err); code != "lifecycle.state" {
			t.Fatalf("a parent holding more run directories than the bound ended with %v", err)
		}
	})
	t.Run("tree depth", func(t *testing.T) {
		runner := sweepingRunner(t, completing)
		nest(t, filepath.Join(plantJob(t, runner.jobParent, "bootwright-run-4848"), "tree"), maxRunTreeDepth-1)
		if _, err := runner.Run(context.Background(), adapterRequest(t, &output)); err != nil {
			t.Fatalf("a stale job as deep as the bound refused: %v", err)
		}
		if jobs := runNames(t, runner.jobParent); len(jobs) != 0 {
			t.Fatalf("a stale job within the bound stayed: %v", jobs)
		}
		job := plantJob(t, runner.jobParent, "bootwright-run-4949")
		nest(t, filepath.Join(job, "tree"), maxRunTreeDepth)
		_, err := runner.Run(context.Background(), adapterRequest(t, &output))
		if code, _ := codeOf(err); code != "lifecycle.state" {
			t.Fatalf("a stale job deeper than the bound ended with %v", err)
		}
		for _, name := range []string{recordName, lockName} {
			if _, err := os.Lstat(filepath.Join(job, name)); err != nil {
				t.Fatalf("the job beyond the bound lost its %s: %v", name, err)
			}
		}
	})
	t.Run("tree entries", func(t *testing.T) {
		runner := sweepingRunner(t, completing)
		runner.treeEntries = 8
		// Eight entries in all, spread so no one directory reaches the bound.
		files := map[string]string{
			lockName: "", recordName: `{}`, "id": "PRIVATE KEY",
			"local/ansible-local-1/AnsiballZ_setup.py": "module", "tmp/a": "",
		}
		plant(t, filepath.Join(runner.jobParent, "bootwright-run-4646"), files)
		if _, err := runner.Run(context.Background(), adapterRequest(t, &output)); err != nil {
			t.Fatalf("a stale job holding as many entries as the bound refused: %v", err)
		}
		if jobs := runNames(t, runner.jobParent); len(jobs) != 0 {
			t.Fatalf("a stale job within the bound stayed: %v", jobs)
		}
		files["tmp/b"] = ""
		job := plant(t, filepath.Join(runner.jobParent, "bootwright-run-4747"), files)
		_, err := runner.Run(context.Background(), adapterRequest(t, &output))
		if code, _ := codeOf(err); code != "lifecycle.state" {
			t.Fatalf("a stale job holding more entries than the bound ended with %v", err)
		}
		for _, name := range []string{recordName, lockName} {
			if _, err := os.Lstat(filepath.Join(job, name)); err != nil {
				t.Fatalf("the job beyond the bound lost its %s: %v", name, err)
			}
		}
	})
}

// A mount is never emptied. One a killed adapter left inside its job makes
// every sweep fail closed; one standing where a job would is not the runner's.
func TestTheSweepNeverEmptiesAMount(t *testing.T) {
	mount := func(t *testing.T, dir string) {
		t.Helper()
		if err := syscall.Mount("tmpfs", dir, "tmpfs", 0, "mode=0700"); err != nil {
			t.Skipf("mounting a filesystem needs privilege this test does not have: %v", err)
		}
		t.Cleanup(func() { _ = syscall.Unmount(dir, syscall.MNT_DETACH) })
	}
	var output bytes.Buffer
	t.Run("inside a job", func(t *testing.T) {
		runner := sweepingRunner(t, completing)
		mounted := filepath.Join(plantJob(t, runner.jobParent, "bootwright-run-4343"), "mounted")
		if err := os.Mkdir(mounted, 0700); err != nil {
			t.Fatal(err)
		}
		mount(t, mounted)
		plant(t, mounted, map[string]string{"host-file": "another filesystem's"})
		// The job keeps its record, so the next run fails closed too.
		for range 2 {
			_, err := runner.Run(context.Background(), adapterRequest(t, &output))
			if code, _ := codeOf(err); code != "lifecycle.state" {
				t.Fatalf("a stale job holding a mount ended with %v", err)
			}
		}
		if _, err := os.Stat(filepath.Join(mounted, "host-file")); err != nil {
			t.Fatalf("the sweep emptied a mount: %v", err)
		}
	})
	t.Run("as a job", func(t *testing.T) {
		runner := sweepingRunner(t, completing)
		job := filepath.Join(runner.jobParent, "bootwright-run-4444")
		if err := os.Mkdir(job, 0700); err != nil {
			t.Fatal(err)
		}
		mount(t, job)
		plantJob(t, runner.jobParent, "bootwright-run-4444")
		if _, err := runner.Run(context.Background(), adapterRequest(t, &output)); err != nil {
			t.Fatalf("a mount standing where a job would refused the run: %v", err)
		}
		if _, err := os.Stat(filepath.Join(job, "id")); err != nil {
			t.Fatalf("the sweep emptied a mount: %v", err)
		}
	})
}

// trees lists every path beneath the parents without following a link.
func trees(t *testing.T, parents ...string) []string {
	t.Helper()
	var paths []string
	for _, parent := range parents {
		err := filepath.WalkDir(parent, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			paths = append(paths, path+" "+entry.Type().String())
			return nil
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
	}
	return paths
}
