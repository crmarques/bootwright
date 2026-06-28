package render

import (
	"fmt"
	"os"

	"github.com/crmarques/bootwright/internal/host/safefs"
)

// File modes for everything the renderer writes. Rendered directories and
// runtime work dirs are owner-only because they hold rendered install-config
// copies; the YAML files themselves are written 0600 for the same reason.
const (
	localDirMode  os.FileMode = 0o700
	localFileMode os.FileMode = 0o600
)

// FileSystem is the side-effect surface render uses to materialize
// state to disk. The default implementation calls os.* directly; tests
// can substitute a recording implementation to assert security
// invariants (every directory is followed by an explicit Chmod, every
// file is written with localFileMode) without touching real disk.
//
// Mode arguments are honoured by both the os and recording
// implementations; callers MUST pass the documented constants
// (localDirMode / localFileMode) so the security boundary stays
// audit-able from one place.
type FileSystem interface {
	MkdirAll(path string, mode os.FileMode) error
	Chmod(path string, mode os.FileMode) error
	WriteAtomic(path string, data []byte, mode os.FileMode) error
	RemoveAll(path string) error
}

// osFS is the default FileSystem; calls into os/* directly and uses
// the atomic-rename pattern for file writes so half-written content
// never appears at the final path.
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

// defaultFS is the FileSystem the package's exported entry points use
// when callers don't supply their own. Tests use AllOn /
// ResolveInstallerOn to inject a substitute.
var defaultFS FileSystem = osFS{}

// ensureLocalDir creates `dir` with localDirMode and tightens the mode
// even when the directory already existed. MkdirAll is a no-op (and
// leaves the existing mode untouched) when the path exists; without
// the explicit Chmod a directory created by a user umask of 0022
// would silently be 0755 and expose subsequent secret material. Keep
// this helper so the security boundary is named in one place.
func ensureLocalDir(fs FileSystem, dir string) error {
	if err := fs.MkdirAll(dir, localDirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if err := fs.Chmod(dir, localDirMode); err != nil {
		return fmt.Errorf("chmod %s: %w", dir, err)
	}
	return nil
}
