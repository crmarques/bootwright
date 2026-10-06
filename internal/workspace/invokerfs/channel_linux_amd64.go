//go:build linux && amd64

package invokerfs

import (
	"context"
	"encoding/binary"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"
)

const (
	requestDeadline  = 10 * time.Second
	helperExecutable = "/proc/self/exe"
)

type launch struct {
	command  func(Account, *os.File) *exec.Cmd
	deadline time.Duration
}

var production = launch{command: helperCommand, deadline: requestDeadline}

// helperCommand runs this same program, which the child resolves through
// procfs after its credential change, so root never resolves a pathname the
// invoking account controls.
func helperCommand(account Account, socket *os.File) *exec.Cmd {
	return &exec.Cmd{
		Path:       helperExecutable,
		Args:       []string{helperExecutable, HelperMode},
		Env:        []string{"LANG=C", "LC_ALL=C"},
		Dir:        "/",
		ExtraFiles: []*os.File{socket},
		SysProcAttr: &syscall.SysProcAttr{
			Credential: &syscall.Credential{Uid: uint32(account.UID), Gid: uint32(account.GID), Groups: append([]uint32(nil), account.Groups...)},
			Pdeathsig:  syscall.SIGKILL,
		},
		WaitDelay: time.Second,
	}
}

// channel is root's end of one helper. The helper's replies are untrusted:
// root takes from it only a descriptor, which the receiving adapter proves.
type channel struct {
	socket   *os.File
	raw      syscall.RawConn
	deadline time.Duration
	ctx      context.Context
	kill     func()
	exited   <-chan struct{}
	stop     func() bool
	failure  error
}

func spawn(ctx context.Context, account Account, l launch) (*channel, error) {
	local, remote, err := socketPair()
	if err != nil {
		return nil, errHelper
	}
	command := l.command(account, remote)
	started := make(chan error, 1)
	exited := make(chan struct{})
	go supervise(command, started, exited)
	err = <-started
	remote.Close()
	if err != nil {
		local.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errHelper
	}
	process := command.Process
	return connect(ctx, local, account, l.deadline, func() { process.Kill() }, exited)
}

// supervise starts and reaps the helper from one locked thread, because the
// kernel sends Pdeathsig when the creating thread exits (golang/go#27505).
// The helper's exit status is untrusted and carries nothing, so Wait's error
// is not consulted.
func supervise(command *exec.Cmd, started chan<- error, exited chan<- struct{}) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := command.Start(); err != nil {
		started <- err
		return
	}
	started <- nil
	command.Wait()
	close(exited)
}

func socketPair() (*os.File, *os.File, error) {
	pair, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_SEQPACKET|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	if err := syscall.SetNonblock(pair[0], true); err != nil {
		syscall.Close(pair[0])
		syscall.Close(pair[1])
		return nil, nil, err
	}
	return os.NewFile(uintptr(pair[0]), "invokerfs"), os.NewFile(uintptr(pair[1]), "invokerfs helper"), nil
}

func connect(ctx context.Context, socket *os.File, account Account, deadline time.Duration, kill func(), exited <-chan struct{}) (*channel, error) {
	raw, err := socket.SyscallConn()
	if err != nil {
		socket.Close()
		kill()
		<-exited
		return nil, errHelper
	}
	c := &channel{socket: socket, raw: raw, deadline: deadline, ctx: ctx, kill: kill, exited: exited}
	c.stop = context.AfterFunc(ctx, c.abort)
	var hello [handshakeBytes]byte
	binary.LittleEndian.PutUint32(hello[:4], uint32(account.UID))
	binary.LittleEndian.PutUint32(hello[4:], uint32(account.GID))
	_, errno, err := c.exchange(hello[:], nil, false)
	if err == nil && errno != 0 {
		err = c.fail()
	}
	if err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

func (c *channel) open(r request, parent *os.File) (*os.File, error) {
	fd, errno, err := c.exchange(r.encode(), parent, true)
	if err != nil {
		return nil, err
	}
	if errno != 0 {
		return nil, &openError{errno: errno}
	}
	return os.NewFile(uintptr(fd), r.label()), nil
}

// close lets the helper exit on end of input, then kills whatever remains
// after a second, and returns once the helper is reaped.
func (c *channel) close() error {
	c.stop()
	err := c.socket.Close()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	select {
	case <-c.exited:
	case <-timer.C:
		c.kill()
		<-c.exited
	}
	return err
}

func (c *channel) abort() {
	c.kill()
	c.socket.SetDeadline(time.Unix(1, 0))
}

func (c *channel) fail() error {
	c.kill()
	if err := c.ctx.Err(); err != nil {
		c.failure = err
		return err
	}
	c.failure = errHelper
	return errHelper
}

func (c *channel) exchange(message []byte, parent *os.File, descriptor bool) (int, syscall.Errno, error) {
	if c.failure != nil {
		return -1, 0, c.failure
	}
	if c.socket.SetDeadline(time.Now().Add(c.deadline)) != nil || c.ctx.Err() != nil {
		return -1, 0, c.fail()
	}
	if c.send(message, parent) != nil {
		return -1, 0, c.fail()
	}
	fd, errno, ok := c.receive(descriptor)
	if !ok {
		return -1, 0, c.fail()
	}
	return fd, errno, nil
}

func (c *channel) send(message []byte, parent *os.File) error {
	var rights []byte
	if parent != nil {
		rights = syscall.UnixRights(int(parent.Fd()))
	}
	var sent error
	err := c.raw.Write(func(fd uintptr) bool {
		sent = syscall.Sendmsg(int(fd), message, rights, nil, syscall.MSG_NOSIGNAL)
		return sent != syscall.EAGAIN
	})
	runtime.KeepAlive(parent)
	if err != nil {
		return err
	}
	return sent
}

// receive accepts one reply: a success carrying exactly the one descriptor
// asked for, or a failure carrying none. Anything else closes every
// descriptor it brought and ends the session.
func (c *channel) receive(descriptor bool) (int, syscall.Errno, bool) {
	var body [replyBytes + 1]byte
	oob := make([]byte, syscall.CmsgSpace(4))
	var n, oobn, flags int
	var got error
	err := c.raw.Read(func(fd uintptr) bool {
		n, oobn, flags, _, got = syscall.Recvmsg(int(fd), body[:], oob, syscall.MSG_CMSG_CLOEXEC)
		return got != syscall.EAGAIN
	})
	if err != nil || got != nil {
		return -1, 0, false
	}
	fds, refused := received(oob[:oobn])
	want := 0
	if descriptor {
		want = 1
	}
	errno := syscall.Errno(binary.LittleEndian.Uint32(body[:replyBytes]))
	switch {
	case refused, flags&(syscall.MSG_TRUNC|syscall.MSG_CTRUNC) != 0, n != replyBytes, errno >= maxErrno,
		errno == 0 && len(fds) != want, errno != 0 && len(fds) != 0:
		closeAll(fds)
		return -1, 0, false
	}
	if errno != 0 || !descriptor {
		return -1, errno, true
	}
	return fds[0], 0, true
}
