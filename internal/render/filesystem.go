package render

import (
	"fmt"
	"os"

	"github.com/crmarques/bootwright/internal/host/safefs"
)

const (
	localDirMode    os.FileMode = 0o700
	localFileMode   os.FileMode = 0o600
	localScriptMode os.FileMode = 0o755
)

type FileSystem interface {
	MkdirAll(path string, mode os.FileMode) error
	Chmod(path string, mode os.FileMode) error
	WriteAtomic(path string, data []byte, mode os.FileMode) error
	RemoveAll(path string) error
}

type osFS struct{}

func (osFS) MkdirAll(path string, mode os.FileMode) error {
	return os.MkdirAll(path, mode)
}

func (osFS) Chmod(path string, mode os.FileMode) error {
	return os.Chmod(path, mode)
}

func (osFS) WriteAtomic(path string, data []byte, mode os.FileMode) error {
	return safefs.AtomicWriteFile(path, data, mode)
}

func (osFS) RemoveAll(path string) error {
	return os.RemoveAll(path)
}

var defaultFS FileSystem = osFS{}

func ensureLocalDir(fs FileSystem, dir string) error {
	if err := fs.MkdirAll(dir, localDirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := fs.Chmod(dir, localDirMode); err != nil {
		return fmt.Errorf("chmod %s: %w", dir, err)
	}
	return nil
}
