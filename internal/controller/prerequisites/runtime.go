package prerequisites

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

const RuntimeFormat = "bootwright.controller.runtime-v1"

var ErrNoPythonInterpreter = errors.New("no qualified Python interpreter of a locked ABI is installed")

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

func RuntimeDigest(lockDigest, abi, interpreter string) string {
	sum := sha256.Sum256([]byte(RuntimeFormat + "\x00" + lockDigest + "\x00" + abi + "\x00" + interpreter))
	return hex.EncodeToString(sum[:])
}
