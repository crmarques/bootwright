package inputfs

import (
	"context"
	"os"
)

// Reader acquires desired-state input through Files, which opens each path
// under the invoking account's credentials. A Reader bound to no Files opens
// with this process's own credentials.
type Reader struct {
	Files Files
}

// Files begins one acquisition's access to operator-named input paths.
type Files interface {
	Begin(context.Context) (FileSession, error)
}

// FileSession opens "/" and then one name at a time beneath a directory it
// issued, never following a link at that name. The reader proves every
// descriptor it receives.
type FileSession interface {
	Root() (*os.File, error)
	OpenAt(*os.File, string, int) (*os.File, error)
	Close() error
}
