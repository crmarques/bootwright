package comparisonlocal

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/nativeartifacts/rendering"
)

const HelperMode = "__bootwright_tree"

const (
	actionPublish = "publish"
	actionScratch = "scratch"
	actionRemove  = "remove"
	actionRender  = "render"
	actionResolve = "resolve"
)

const (
	maximumTree      = 64 << 20
	maximumRequest   = maximumTree + 64<<10
	maximumResponse  = 16 << 10
	maximumPath      = 4096
	maximumText      = 2048
	helperDeadline   = time.Minute
	helperExecutable = "/proc/self/exe"
)

var errHelper = errors.New("the invoking account's file helper failed")

type helperRequest struct {
	UID       int                  `json:"uid"`
	GID       int                  `json:"gid"`
	Action    string               `json:"action"`
	Directory string               `json:"directory"`
	Files     []rendering.TreeFile `json:"files"`
	Private   bool                 `json:"private,omitempty"`
}

type helperResponse struct {
	Directory   string `json:"directory"`
	Failed      bool   `json:"failed"`
	Message     string `json:"message,omitempty"`
	Remediation string `json:"remediation,omitempty"`
}

func (s Service) perform(ctx context.Context, who identity, request helperRequest) (string, error) {
	if treeBytes(request.Files) > maximumTree {
		return "", refuse("the comparison files exceed the 64 MiB a comparison tree may hold", "narrow the diff to fewer objects")
	}
	if !who.delegate {
		return local(ctx, s.Trees, request)
	}
	request.UID, request.GID = who.account.UID, who.account.GID
	data, err := json.Marshal(request)
	if err != nil || len(data) > maximumRequest {
		return "", refuse("the comparison request exceeds what the invoking account's helper accepts", "narrow the diff to fewer objects")
	}
	helper := spawnHelper
	if s.helper != nil {
		helper = s.helper
	}
	operation, cancel := context.WithTimeout(ctx, helperDeadline)
	defer cancel()
	output, err := helper(operation, who.account, data)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", refuse(errHelper.Error(), "repeat the command, and report this if it persists")
	}
	return decodeResponse(request.Action, output)
}

func spawnHelper(ctx context.Context, account Account, data []byte) ([]byte, error) {
	command := helperCommand(ctx, account, data)
	output := &boundedBuffer{}
	command.Stdout, command.Stderr = output, io.Discard
	if started, err := supervise(command); !started || err != nil || output.overflow {
		return nil, errHelper
	}
	return output.data, nil
}

func helperCommand(ctx context.Context, account Account, data []byte) *exec.Cmd {
	command := exec.CommandContext(ctx, helperExecutable, HelperMode)
	command.Env = []string{"LANG=C", "LC_ALL=C"}
	command.Dir = "/"
	command.Stdin = bytes.NewReader(data)
	command.SysProcAttr = processAttributes(accountCredential(account))
	command.WaitDelay = time.Second
	return command
}

func decodeResponse(action string, data []byte) (string, error) {
	var response helperResponse
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&response) != nil || decoder.Decode(new(any)) != io.EOF {
		return "", refuse("the invoking account's file helper returned no usable response", "repeat the command, and report this if it persists")
	}
	if response.Failed {
		message, remedy := "the comparison files cannot be written under the invoking account", "pass a directory this account can write"
		if boundedText(response.Message) {
			message = response.Message
			if boundedText(response.Remediation) {
				remedy = response.Remediation
			}
		}
		return "", refuse(message, remedy)
	}
	if (action == actionScratch && !scratchPath(response.Directory)) || ((action == actionPublish || action == actionRender || action == actionResolve) && !absolutePath(response.Directory)) {
		return "", refuse("the invoking account's file helper returned no usable directory", "repeat the command, and report this if it persists")
	}
	return response.Directory, nil
}

func local(ctx context.Context, trees Trees, request helperRequest) (string, error) {
	switch request.Action {
	case actionResolve:
		if !absolutePath(request.Directory) {
			return "", refuse("the artifact path is not absolute", "pass an absolute path this account can reach")
		}
		return trees.Resolve(request.Directory)
	case actionRender:
		if !absolutePath(request.Directory) || len(request.Files) != 1 || request.Files[0].Path != rendering.EffectiveStateFile {
			return "", refuse("the artifact publication request is invalid", "repeat the render command")
		}
		return trees.Publish(ctx, request.Directory, request.Files[0].Path, request.Files[0].Data, request.Private)
	case actionPublish:
		if !absolutePath(request.Directory) {
			return "", refuse("the path "+request.Directory+" is not absolute", "pass a path this account can reach")
		}
		resolved, err := trees.Resolve(request.Directory)
		if err != nil {
			return "", err
		}
		return trees.PublishTree(ctx, resolved, request.Files)
	case actionScratch:
		if request.Directory != "" && !absolutePath(request.Directory) {
			return "", refuse("the temporary parent "+request.Directory+" is not absolute", "report this; the private directory needs an absolute parent")
		}
		scratch, err := os.MkdirTemp(request.Directory, scratchPrefix)
		if err != nil {
			return "", refuse("a private directory for the comparison cannot be created: "+err.Error(), "make a temporary directory writable by this account")
		}
		if _, err := trees.PublishTree(ctx, scratch, request.Files); err != nil {
			_ = os.Remove(scratch)
			return "", err
		}
		return scratch, nil
	case actionRemove:
		if !scratchPath(request.Directory) {
			return "", refuse("the path "+request.Directory+" is not a private comparison directory", "report this; only a private comparison directory is removed")
		}
		if err := os.RemoveAll(request.Directory); err != nil {
			return "", refuse(request.Directory+" cannot be removed: "+err.Error(), "remove it by hand")
		}
		return request.Directory, nil
	}
	return "", refuse("the comparison request "+request.Action+" is unknown", "report this")
}

func ServeHelper(ctx context.Context, args []string, input io.Reader, output io.Writer, trees Trees) (bool, int) {
	if len(args) == 0 || args[0] != HelperMode {
		return false, 0
	}
	if len(args) != 1 || os.Getuid() != os.Geteuid() || os.Getuid() == 0 || os.Getgid() != os.Getegid() {
		return true, 1
	}
	data, err := io.ReadAll(io.LimitReader(input, maximumRequest+1))
	if err != nil || len(data) > maximumRequest {
		return true, 1
	}
	var request helperRequest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil || decoder.Decode(new(any)) != io.EOF || request.UID != os.Getuid() || request.GID != os.Getegid() {
		return true, 1
	}
	directory, err := local(ctx, trees, request)
	response := helperResponse{Directory: directory, Failed: err != nil}
	if reported := diagnostics.Of(err); len(reported) == 1 {
		response.Message, response.Remediation = reported[0].Message, reported[0].Remediation
	}
	if json.NewEncoder(output).Encode(response) != nil {
		return true, 1
	}
	return true, 0
}

func treeBytes(files []rendering.TreeFile) int {
	if files == nil {
		return len("null")
	}
	size := len("[]")
	for index, file := range files {
		if index > 0 {
			size++
		}
		path, _ := json.Marshal(file.Path)
		size += len(`{"Path":,"Data":}`) + len(path)
		if file.Data == nil {
			size += len("null")
		} else {
			size += base64.StdEncoding.EncodedLen(len(file.Data)) + len(`""`)
		}
	}
	return size
}

func absolutePath(path string) bool {
	return path != "" && len(path) <= maximumPath && filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsRune(path, 0)
}

func scratchPath(path string) bool {
	return absolutePath(path) && strings.HasPrefix(filepath.Base(path), scratchPrefix)
}

func boundedText(text string) bool {
	if text == "" || len(text) > maximumText || !utf8.ValidString(text) {
		return false
	}
	for _, r := range text {
		if r < ' ' || (r >= 0x7f && r <= 0x9f) {
			return false
		}
	}
	return true
}

type boundedBuffer struct {
	data     []byte
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	n := len(p)
	if remaining := maximumResponse - len(b.data); n > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	b.data = append(b.data, p...)
	return n, nil
}
