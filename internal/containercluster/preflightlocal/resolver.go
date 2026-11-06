package preflightlocal

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"time"

	"github.com/crmarques/bootwright/internal/nativeexec"
)

type Resolver struct{}

type capture struct{ bytes.Buffer }

func (c *capture) Write(data []byte) (int, error) {
	if c.Len()+len(data) > 65536 {
		return 0, errors.New("resolver output exceeded its bound")
	}
	return c.Buffer.Write(data)
}
func (Resolver) Read(ctx context.Context, argv []string) ([]byte, error) {
	if len(argv) != 3 || argv[0] != "getent" || argv[1] != "ahosts" {
		return nil, errors.New("unsupported resolver command")
	}
	executable, err := nativeexec.Qualify("/usr/bin/getent")
	if err != nil {
		return nil, errors.New("the system executable is not root-owned below qualified directories")
	}
	command := exec.CommandContext(ctx, executable, argv[1:]...)
	command.Dir = "/"
	command.WaitDelay = time.Second
	command.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C"}
	output := &capture{}
	command.Stdout = output
	command.Stderr = io.Discard
	if err := command.Run(); err != nil {
		return nil, errors.New("the bounded controller resolver command did not complete")
	}
	return output.Bytes(), nil
}
