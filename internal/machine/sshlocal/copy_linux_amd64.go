//go:build linux && amd64

package sshlocal

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/machine"
	machineaccess "github.com/crmarques/bootwright/internal/machine/access"
	"github.com/crmarques/bootwright/internal/nativeexec"
)

//go:embed copy_worker.py
var copyWorkerSource string

const copyFrameLimit = 8 << 20

type Copier struct {
	qualify func(string) (string, error)
	SSH     Launcher
	Account func(context.Context) (machineaccess.CopyAccount, error)
}

type copyEntry struct {
	Path  string  `json:"path"`
	Kind  string  `json:"kind"`
	Mode  int     `json:"mode"`
	Size  int64   `json:"size"`
	Token []int64 `json:"token"`
}

type copyReply struct {
	OK      bool        `json:"ok"`
	Error   string      `json:"error"`
	Path    string      `json:"path"`
	Kind    string      `json:"kind"`
	Entries []copyEntry `json:"entries"`
	Parents []string    `json:"parents"`
}

type copyWorker struct {
	in     *os.File
	out    *bufio.Reader
	reader *os.File
	cancel context.CancelFunc
	done   <-chan error
	closed bool
}

func (w *copyWorker) send(request any) error {
	data, err := json.Marshal(request)
	if err != nil || len(data)+1 > copyFrameLimit {
		return errors.New("copy manifest exceeds the protocol limit")
	}
	_, err = w.in.Write(append(data, '\n'))
	return err
}

func (w *copyWorker) receive() (copyReply, error) {
	var data []byte
	for {
		part, more, err := w.out.ReadLine()
		if err != nil {
			return copyReply{}, errors.New("file worker connection ended")
		}
		if len(data)+len(part) > copyFrameLimit {
			return copyReply{}, errors.New("file worker response exceeds the protocol limit")
		}
		data = append(data, part...)
		if !more {
			break
		}
	}
	var reply copyReply
	if json.Unmarshal(data, &reply) != nil {
		return copyReply{}, errors.New("invalid file worker response")
	}
	if !reply.OK {
		allowed := []string{"manifest-too-large", "invalid-path", "invalid-entry", "source-root", "tree-too-large", "filesystem-crossing", "source-changed", "symlink-or-special-file", "missing-parent-directory", "parent-not-directory", "destination-type-conflict", "destination-exists", "destination-changed", "unsafe-destination-directory", "unsafe-staging-directory", "PermissionError", "FileNotFoundError", "NotADirectoryError", "OSError"}
		if !slices.Contains(allowed, reply.Error) {
			return reply, errors.New("file worker refused the operation")
		}
		return reply, errors.New(reply.Error)
	}
	return reply, nil
}

func (w *copyWorker) call(request any) (copyReply, error) {
	if err := w.send(request); err != nil {
		return copyReply{}, err
	}
	return w.receive()
}

func (w *copyWorker) close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	_ = w.in.Close()
	_ = w.reader.Close()
	timer := time.AfterFunc(terminationGrace, w.cancel)
	err := <-w.done
	timer.Stop()
	w.cancel()
	return err
}

func startCopyWorker(ctx context.Context, run func(context.Context, io.Reader, io.Writer) error) (*copyWorker, error) {
	inner, cancel := context.WithCancel(ctx)
	inRead, inWrite, err := os.Pipe()
	if err != nil {
		cancel()
		return nil, err
	}
	outRead, outWrite, err := os.Pipe()
	if err != nil {
		cancel()
		inRead.Close()
		inWrite.Close()
		return nil, err
	}
	done := make(chan error, 1)
	go func() {
		err := run(inner, inRead, outWrite)
		_ = inRead.Close()
		_ = outWrite.Close()
		done <- err
	}()
	return &copyWorker{in: inWrite, out: bufio.NewReader(outRead), reader: outRead, cancel: cancel, done: done}, nil
}

func (c Copier) local(ctx context.Context) (*copyWorker, error) {
	if c.Account == nil {
		return nil, errors.New("invoking account is unavailable")
	}
	account, err := c.Account(ctx)
	if err != nil {
		return nil, errors.New("invoking account could not be verified")
	}
	if account.UID < 0 || account.GID < 0 || uint64(account.UID) > 1<<32-2 || uint64(account.GID) > 1<<32-2 || os.Geteuid() != 0 && account.UID != os.Geteuid() {
		return nil, errors.New("invoking account is invalid")
	}
	qualify := c.qualify
	if qualify == nil {
		qualify = nativeexec.Qualify
	}
	python, err := qualify("/usr/bin/python3")
	if err != nil {
		return nil, errors.New("a qualified /usr/bin/python3 is required")
	}
	return startCopyWorker(ctx, func(inner context.Context, in io.Reader, out io.Writer) error {
		command := exec.CommandContext(inner, python, "-I", "-B", "-c", copyWorkerSource)
		command.Stdin, command.Stdout, command.Stderr = in, out, io.Discard
		command.Env, command.Dir = []string{"LANG=C", "LC_ALL=C"}, "/"
		command.SysProcAttr = &syscall.SysProcAttr{}
		if os.Geteuid() == 0 {
			command.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(account.UID), Gid: uint32(account.GID), Groups: slices.Clone(account.Groups)}
		}
		command.Cancel = func() error { return command.Process.Signal(syscall.SIGTERM) }
		command.WaitDelay = terminationGrace
		return command.Run()
	})
}

func (c Copier) remote(ctx context.Context, session machine.Session) (*copyWorker, error) {
	session.Command = []string{"/usr/bin/python3", "-I", "-B", "-c", copyWorkerSource}
	return startCopyWorker(ctx, func(inner context.Context, in io.Reader, out io.Writer) error {
		code, err := c.SSH.Run(inner, session, in, out, io.Discard)
		if err != nil {
			return err
		}
		if code != 0 {
			return errors.New("remote file worker failed")
		}
		return nil
	})
}

type preparedCopy struct {
	copier      Copier
	session     machine.Session
	request     machineaccess.CopyRequest
	collect     bool
	entries     []copyEntry
	destination string
}

func (p *preparedCopy) Destination() string { return p.destination }
func (p *preparedCopy) Close() error        { return nil }

func (c Copier) Prepare(ctx context.Context, session machine.Session, request machineaccess.CopyRequest, collect bool) (machineaccess.PreparedCopy, error) {
	local, err := filepath.Abs(request.Source.Path)
	if request.Source.Remote {
		local, err = filepath.Abs(request.Destination.Path)
	}
	if err != nil {
		return nil, errors.New("local path could not be resolved")
	}
	if request.Source.Remote {
		if strings.HasSuffix(request.Destination.Path, "/") && local != "/" {
			local += "/"
		}
		request.Destination.Path = local
	} else {
		request.Source.Path = local
	}
	p := &preparedCopy{copier: c, session: session, request: request, collect: collect}
	source, destination, _, err := p.prepare(ctx)
	if source != nil {
		defer source.close()
	}
	if destination != nil {
		defer destination.close()
	}
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (p *preparedCopy) prepare(ctx context.Context) (*copyWorker, *copyWorker, []string, error) {
	local, err := p.copier.local(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	remote, err := p.copier.remote(ctx, p.session)
	if err != nil {
		local.close()
		return nil, nil, nil, err
	}
	source, destination := local, remote
	if p.request.Source.Remote {
		source, destination = remote, local
	}
	manifest, err := source.call(map[string]any{"op": "scan", "path": p.request.Source.Path})
	if err != nil {
		return source, destination, nil, err
	}
	if err := validCopyManifest(manifest); err != nil {
		return source, destination, nil, err
	}
	if p.entries != nil && !reflect.DeepEqual(p.entries, manifest.Entries) {
		return source, destination, nil, errors.New("source changed after copy preflight")
	}
	p.entries = manifest.Entries
	base := path.Base(manifest.Path)
	root := p.request.Destination.Path
	parents := []string{}
	if p.collect {
		info, e := destination.call(map[string]any{"op": "inspect", "path": root})
		if e != nil {
			return source, destination, nil, e
		}
		if info.Kind != "directory" && info.Kind != "missing" {
			return source, destination, nil, errors.New("several-machine downloads require a local directory")
		}
		parents = append(parents, root)
		root = path.Join(root, p.session.Machine)
		parents = append(parents, root)
		root = path.Join(root, base)
	} else {
		info, e := destination.call(map[string]any{"op": "inspect", "path": root})
		if e != nil {
			return source, destination, nil, e
		}
		if info.Kind == "directory" || strings.HasSuffix(p.request.Destination.Path, "/") {
			if info.Kind != "directory" && info.Kind != "missing" {
				return source, destination, nil, errors.New("destination requires a directory")
			}
			parents = append(parents, root)
			root = path.Join(root, base)
		}
	}
	reply, err := destination.call(map[string]any{"op": "prepare", "path": root, "parents": parents, "entries": p.entries, "overwrite": p.request.Overwrite})
	if err == nil {
		if !path.IsAbs(reply.Path) || path.Clean(reply.Path) != reply.Path || p.destination != "" && p.destination != reply.Path {
			err = errors.New("destination changed after copy preflight")
		} else {
			p.destination = reply.Path
			parents = reply.Parents
		}
	}
	return source, destination, parents, err
}

func validCopyManifest(reply copyReply) error {
	if len(reply.Entries) == 0 || len(reply.Entries) > 8192 || !path.IsAbs(reply.Path) || path.Clean(reply.Path) != reply.Path || path.Base(reply.Path) == "/" {
		return errors.New("invalid source manifest")
	}
	seen := map[string]string{}
	var total int64
	for i, entry := range reply.Entries {
		if entry.Path != "" && (path.IsAbs(entry.Path) || path.Clean(entry.Path) != entry.Path || entry.Path == ".." || strings.HasPrefix(entry.Path, "../")) || entry.Kind != "file" && entry.Kind != "directory" || entry.Size < 0 || entry.Mode < 0 || entry.Mode > 0o777 || seen[entry.Path] != "" || len(entry.Path) > 4096 || !utf8.ValidString(entry.Path) || strings.ContainsRune(entry.Path, 0) || strings.Count(entry.Path, "/") >= 64 || i == 0 && entry.Path != "" {
			return errors.New("invalid source manifest")
		}
		parent := path.Dir(entry.Path)
		if parent == "." {
			parent = ""
		}
		if i > 0 && seen[parent] != "directory" {
			return errors.New("invalid source manifest order")
		}
		if entry.Size > math.MaxInt64-total {
			return errors.New("source byte count exceeds the result limit")
		}
		total += entry.Size
		seen[entry.Path] = entry.Kind
	}
	return nil
}

func (p *preparedCopy) Run(ctx context.Context) (progress machineaccess.CopyProgress, err error) {
	source, destination, parents, err := p.prepare(ctx)
	if source != nil {
		defer source.close()
	}
	if destination != nil {
		defer destination.close()
	}
	if err != nil {
		return progress, err
	}
	for _, parent := range parents {
		if _, err = destination.call(map[string]any{"op": "directory", "path": parent}); err != nil {
			return progress, err
		}
	}
	for _, entry := range p.entries {
		if err = ctx.Err(); err != nil {
			return progress, err
		}
		if entry.Kind == "directory" {
			_, err = destination.call(map[string]any{"op": "directory", "path": path.Join(p.destination, entry.Path)})
		} else {
			progress.Unknown, err = copyOne(source, destination, entry)
			if err == nil {
				progress.Files++
				progress.Bytes += entry.Size
			}
		}
		if err != nil {
			return progress, err
		}
	}
	_, err = destination.call(map[string]any{"op": "finish"})
	return progress, err
}

func copyOne(source, destination *copyWorker, entry copyEntry) (bool, error) {
	if _, err := source.call(map[string]any{"op": "read", "path": entry.Path}); err != nil {
		return false, err
	}
	if err := destination.send(map[string]any{"op": "begin", "path": entry.Path}); err != nil {
		return false, err
	}
	if _, err := destination.receive(); err != nil {
		return false, err
	}
	if _, err := io.CopyN(destination.in, source.out, entry.Size); err != nil {
		return false, errors.New("file stream did not complete")
	}
	if _, err := source.receive(); err != nil {
		return false, err
	}
	if _, err := destination.receive(); err != nil {
		return false, err
	}
	if err := destination.send(map[string]any{"op": "commit"}); err != nil {
		return true, err
	}
	_, err := destination.receive()
	return err != nil, err
}
