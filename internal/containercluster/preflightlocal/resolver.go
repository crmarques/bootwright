package preflightlocal

import (
	"context"
	"errors"
	"time"

	"github.com/crmarques/bootwright/internal/nativeexec"
)

type Resolver struct{}

func (Resolver) Read(ctx context.Context, argv []string) ([]byte, error) {
	if len(argv) != 3 || argv[0] != "getent" || argv[1] != "ahosts" {
		return nil, errors.New("unsupported resolver command")
	}
	executable, err := nativeexec.Qualify("/usr/bin/getent")
	if err != nil {
		return nil, errors.New("the system executable is not root-owned below qualified directories")
	}
	output := nativeexec.Head(65536)
	result := nativeexec.Run(ctx, nativeexec.Command{
		Path: executable, Args: argv[1:], Env: []string{"PATH=/usr/bin:/bin", "LC_ALL=C"},
		Stdout: output, WaitDelay: time.Second,
	})
	if !result.Succeeded() || output.Overflowed() {
		return nil, errors.New("the bounded controller resolver command did not complete")
	}
	return output.Bytes(), nil
}
