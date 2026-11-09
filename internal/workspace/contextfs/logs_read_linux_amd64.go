//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"syscall"

	"github.com/crmarques/bootwright/internal/reconciliation/logview"
	"github.com/crmarques/bootwright/internal/reconciliation/operationstore"
	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

var _ logview.Workspace = (*Store)(nil)

type logsView struct {
	context *directory
	records *operationArea
	logs    *operationArea
	runs    *operationArea
}

func (v *logsView) Records() operationstore.Area { return v.records }

func (v *logsView) Logs() operationstore.Area { return v.logs }

func (v *logsView) Open(ctx context.Context, target string) (logview.LogFile, bool, error) {
	file, found, err := openAreaLog(ctx, v.logs, target)
	if !found {
		return nil, found, err
	}
	return file, found, err
}

func (v *logsView) Mutating(ctx context.Context) (bool, error) {
	if err := v.logs.available(ctx, false); err != nil {
		return false, err
	}
	return probeLock(v.context.file, syscall.LOCK_SH, v.context.path)
}

func probeLock(file *os.File, how int, location string) (bool, error) {
	err := syscall.Flock(int(file.Fd()), how|syscall.LOCK_NB)
	if errors.Is(err, syscall.EWOULDBLOCK) {
		return true, nil
	}
	if err == nil {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	}
	if err != nil {
		return false, state("the lock on " + location + " could not be tested")
	}
	return false, nil
}

type logFile struct{ file *os.File }

func (f *logFile) ReadRange(offset int64, maximum int) ([]byte, error) {
	data := make([]byte, max(maximum, 0))
	read, err := f.file.ReadAt(data, offset)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return data[:read], err
}

func (f *logFile) Size() (int64, error) {
	stat, err := statHandle(f.file)
	return stat.Size, err
}

func (f *logFile) Close() error { return f.file.Close() }

type heldOutput struct {
	*logFile
	name      string
	reference string
	held      func(context.Context) (bool, error)
}

func (o *heldOutput) Name() string { return o.name }

func (o *heldOutput) Reference() string { return o.reference }

func (o *heldOutput) Held(ctx context.Context) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return o.held(ctx)
}

func unsafeLogFile(location string) error {
	return contexts.StateErrorWithRemediation(
		"the log file "+location+" is not one this store wrote: it must be a regular file of the log root's owner that no group or other may read, with one link, reached without a symbolic link",
		"remove "+location+", then repeat the command")
}

func openAreaLog(ctx context.Context, area *operationArea, target string) (*logFile, bool, error) {
	if err := area.available(ctx, false); err != nil {
		return nil, false, err
	}
	parts, err := operationPath(target, 1)
	if err != nil {
		return nil, false, err
	}
	parent, name, release, err := area.descend(ctx, parts, false)
	if errors.Is(err, syscall.ENOENT) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer release()
	return openHeldLog(parent, name)
}

func openHeldLog(parent *directory, name string) (*logFile, bool, error) {
	location := filepath.Join(parent.path, name)
	file, err := openRelative(parent, name, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ENOENT) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, unsafeLogFile(location)
	}
	stat, err := statHandle(file)
	if err != nil || stat.Mode&syscall.S_IFMT != syscall.S_IFREG || stat.Uid != parent.identity.Uid || stat.Gid != parent.identity.Gid ||
		stat.Mode&077 != 0 || stat.Nlink != 1 || stat.Dev != parent.identity.Dev {
		file.Close()
		return nil, false, unsafeLogFile(location)
	}
	return &logFile{file: file}, true, nil
}

func (s *Store) ReadLogs(ctx context.Context, name string, callback func(logview.View) error) error {
	if callback == nil {
		return state("log inspection callback is missing")
	}
	return s.readLogs(ctx, name, func(view *logsView) error { return callback(view) })
}

func (s *Store) readLogs(ctx context.Context, name string, callback func(*logsView) error) error {
	if !contextName(name) {
		return state("log inspection requires an explicit context name")
	}
	root, err := s.openRoot(ctx, false, nil)
	if errors.Is(err, syscall.ENOENT) {
		return contexts.AbsentContext(name)
	}
	if err != nil {
		return safeError(err)
	}
	defer root.file.Close()
	if err := lockShared(root); err != nil {
		return err
	}
	defer syscall.Flock(int(root.file.Fd()), syscall.LOCK_UN)
	registry, exists, err := readRegistry(ctx, root)
	if err != nil {
		return safeError(err)
	}
	if !exists {
		return contexts.AbsentContext(name)
	}
	if err := verifyMappings(ctx, root, registry); err != nil {
		return safeError(err)
	}
	var record *contexts.Record
	for index := range registry.Contexts {
		if registry.Contexts[index].Name == name {
			record = &registry.Contexts[index]
		}
	}
	if record == nil {
		return contexts.AbsentContext(name)
	}
	container, err := openDirectory(root, "contexts")
	if err != nil {
		return safeError(err)
	}
	defer container.file.Close()
	dir, err := openDirectory(container, record.Name)
	if err != nil {
		return safeError(err)
	}
	defer dir.file.Close()
	if record.DirectoryInode != 0 && (dir.identity.Ino != record.DirectoryInode || uint64(dir.identity.Dev) != record.DirectoryDevice) {
		return state("context directory was replaced")
	}
	active := true
	defer func() { active = false }()
	live := func() bool { return active }
	area := func(subtree string, logged bool) *operationArea {
		return &operationArea{store: s, subtree: subtree, context: dir, name: record.Name, active: live, readOnly: true, logged: logged}
	}
	view := &logsView{context: dir, records: area("operations", false), logs: area(operationLogs, true), runs: area(runsSubtree, true)}
	return safeError(callback(view))
}

func (s *Store) OpenRun(ctx context.Context, name, run string) (logview.Stream, bool, error) {
	number, valid := outputNumber(run+outputSuffix, runPrefix)
	if canonical, err := outputName(runPrefix, number); !valid || err != nil || canonical != run+outputSuffix {
		return nil, false, state("a bounded run is named run-NNNNNN")
	}
	var output *heldOutput
	err := s.readLogs(ctx, name, func(view *logsView) error {
		file, found, err := openAreaLog(ctx, view.runs, run+outputSuffix)
		if err != nil || !found {
			return err
		}
		location := filepath.Join(view.runs.Location(), run+outputSuffix)
		output = &heldOutput{logFile: file, name: run, reference: path.Join(view.runs.Reference(), run+outputSuffix),
			held: func(context.Context) (bool, error) { return probeLock(file.file, syscall.LOCK_EX, location) }}
		return nil
	})
	if err != nil || output == nil {
		return nil, false, err
	}
	return output, true, nil
}

func (s *Store) OpenSetup(ctx context.Context, setup string) (logview.Stream, bool, error) {
	if _, valid := setupRunNumber(setup); setup != "" && !valid {
		return nil, false, state("a controller install output is named install-NNNNNN")
	}
	root, err := s.openLogRoot(ctx, false)
	if errors.Is(err, syscall.ENOENT) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer root.file.Close()
	host, err := s.logDirectory(ctx, root, logController, false)
	if errors.Is(err, syscall.ENOENT) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer host.file.Close()
	return s.openSetupOutput(ctx, host, setup, nil)
}

func (s *Store) openSetupOutput(ctx context.Context, host *directory, setup string, listed func()) (logview.Stream, bool, error) {
	for listing := 0; ; listing++ {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		selected := setup
		if selected == "" {
			newest, err := newestSetupOutput(host)
			if err != nil || newest == "" {
				return nil, false, err
			}
			selected = newest
			if listed != nil {
				listed()
			}
		}
		file, found, err := openHeldLog(host, selected+outputSuffix)
		if err != nil {
			return nil, false, err
		}
		if !found && setup == "" && listing == 0 {
			continue
		}
		if !found {
			return nil, false, nil
		}
		return &heldOutput{logFile: file, name: selected, reference: path.Join(logController, selected+outputSuffix), held: s.setupHeld}, true, nil
	}
}

func newestSetupOutput(host *directory) (string, error) {
	names, err := directoryNames(host, maxOperationEntries)
	if err != nil {
		return "", setupRunAccessError(host.path, err)
	}
	highest := 0
	for _, name := range names {
		if number, valid := outputNumber(name, installRunPrefix); valid {
			highest = max(highest, number)
		}
	}
	if highest == 0 {
		return "", nil
	}
	return setupRunName(highest), nil
}

func (s *Store) setupHeld(ctx context.Context) (bool, error) {
	root, err := s.openRoot(ctx, false, nil)
	if errors.Is(err, syscall.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, safeError(err)
	}
	defer root.file.Close()
	dir, err := openControllerEntry(root, installSubtree)
	if errors.Is(err, syscall.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, safeError(err)
	}
	defer dir.close()
	file, err := openRelative(dir, setupLockName, syscall.O_RDONLY|syscall.O_NONBLOCK, 0)
	if errors.Is(err, syscall.ENOENT) {
		return false, nil
	}
	if err != nil {
		return false, safeError(err)
	}
	defer file.Close()
	location := filepath.Join(dir.path, setupLockName)
	if stat, err := statHandle(file); err != nil || !private(stat, syscall.S_IFREG, dir.identity.Uid, dir.identity.Gid) || stat.Dev != dir.identity.Dev {
		return false, state("setup lock type, owner or permissions is unsafe: " + location)
	}
	return probeLock(file, syscall.LOCK_SH, location)
}
