//go:build linux

package nativefs

import (
	"context"
	"errors"
	"io"
	"os"
	"syscall"
)

func Stat(file *os.File) (syscall.Stat_t, error) {
	var stat syscall.Stat_t
	err := syscall.Fstat(int(file.Fd()), &stat)
	return stat, err
}

func SameIdentity(a, b syscall.Stat_t) bool {
	return a.Dev == b.Dev && a.Ino == b.Ino && a.Mode == b.Mode && a.Uid == b.Uid && a.Gid == b.Gid
}

func SameStable(a, b syscall.Stat_t) bool {
	return SameIdentity(a, b) && a.Nlink == b.Nlink && a.Size == b.Size && a.Mtim == b.Mtim && a.Ctim == b.Ctim
}

func Denied(err error) bool {
	return errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM)
}

func DeniedToRoot(err error) bool {
	var root interface{ DeniedToRoot() bool }
	return errors.As(err, &root) && root.DeniedToRoot()
}

var ErrReadCount = errors.New("file read returned an invalid count")

var ErrInvalidBound = errors.New("a bounded read needs a bound of at least zero")

func ReadBounded(ctx context.Context, file *os.File, maximum int) ([]byte, error) {
	if maximum < 0 {
		return nil, ErrInvalidBound
	}
	data := make([]byte, 0, min(maximum, 32768))
	buffer := make([]byte, min(maximum, 32768))
	defer clear(buffer)
	for len(data) < maximum {
		if err := ctx.Err(); err != nil {
			clear(data)
			return nil, err
		}
		room := min(len(buffer), maximum-len(data))
		n, err := file.Read(buffer[:room])
		if n < 0 || n > room {
			clear(data)
			return nil, ErrReadCount
		}
		data = append(data, buffer[:n]...)
		if errors.Is(err, io.EOF) {
			return data, nil
		}
		if err != nil {
			clear(data)
			return nil, err
		}
		if n == 0 {
			clear(data)
			return nil, ErrReadCount
		}
	}
	return data, nil
}
