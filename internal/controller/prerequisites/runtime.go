package prerequisites

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

const RuntimeFormat = "bootwright.controller.runtime-v1"

var ErrNoPythonInterpreter = errors.New("no qualified Python interpreter of a locked ABI is installed")

var ErrGlibcBelowFloor = errors.New("the host's glibc is below the locked wheels' floor")

type PythonInterpreter struct {
	Path    string   `json:"path"`
	ABI     string   `json:"abi"`
	Version string   `json:"version"`
	Glibc   string   `json:"glibc"`
	Stdlib  []string `json:"stdlib"`
}

type Runtime struct {
	Digest      string            `json:"digest"`
	LockDigest  string            `json:"lockDigest"`
	Interpreter PythonInterpreter `json:"interpreter"`
	AnsibleCore string            `json:"ansibleCore"`
}

type RuntimeWheel struct {
	Filename string
	Size     int64
	Cached   bool
}

const (
	OriginPresent  = "present"
	OriginCache    = "cache"
	OriginMirror   = "mirror"
	OriginDownload = "download"
)

type WheelExport struct {
	Directory string
	Files     []ExportedWheel
}

type ExportedWheel struct {
	Filename string
	SHA256   string
	Size     int64
	Origin   string
}

// RuntimeLock is the embedded lock as setup reads it: its identity, the
// interpreter ABIs and floors it admits, and every file it names.
type RuntimeLock struct {
	Digest      string
	AnsibleCore string
	ABIs        []string
	GlibcFloor  string
	Tested      []TestedRelease
	Files       []LockedFile
}

type TestedRelease struct {
	ID       string
	Versions []string
}

type LockedFile struct {
	ABI      string `json:"abi,omitempty"`
	Filename string `json:"filename"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
}

const RuntimeClosureFormat = "bootwright.controller.execution-closure-v2"

func RuntimeClosureDigest(lockDigest, abi string) string {
	sum := sha256.Sum256([]byte(RuntimeClosureFormat + "\x00" + lockDigest + "\x00" + abi))
	return hex.EncodeToString(sum[:])
}

func RuntimeDigest(lockDigest, abi, interpreter string) string {
	sum := sha256.Sum256([]byte(RuntimeFormat + "\x00" + lockDigest + "\x00" + abi + "\x00" + interpreter))
	return hex.EncodeToString(sum[:])
}
