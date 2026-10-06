//go:build !linux || !amd64

package invokerfs

import (
	"context"
	"errors"
	"os"
)

var errUnsupported = errors.New("invoking-account file access requires Linux amd64")

type Session struct{}

func (*Opener) Begin(context.Context) (*Session, error) { return nil, errUnsupported }

func (*Session) Root() (*os.File, error) { return nil, errUnsupported }

func (*Session) OpenAt(*os.File, string, int) (*os.File, error) { return nil, errUnsupported }

func (*Session) OpenFile(string) (*os.File, error) { return nil, errUnsupported }

func (*Session) Close() error { return nil }

func ServeHelper(args []string) (bool, int) {
	return len(args) > 0 && args[0] == HelperMode, 1
}
