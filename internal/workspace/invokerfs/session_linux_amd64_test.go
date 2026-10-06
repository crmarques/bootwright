//go:build linux && amd64

package invokerfs

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const stallMode = "__invokerfs_stall"

func TestMain(m *testing.M) {
	if handled, code := ServeHelper(os.Args[1:]); handled {
		os.Exit(code)
	}
	if len(os.Args) == 2 && os.Args[1] == stallMode {
		stall(helperSocket)
		os.Exit(0)
	}
	if reader, writer, err := os.Pipe(); err == nil {
		reader.Close()
		writer.Close()
	}
	os.Exit(m.Run())
}

func stall(socket int) {
	var hello [handshakeBytes]byte
	if _, _, _, _, err := syscall.Recvmsg(socket, hello[:], nil, 0); err == nil && reply(socket, 0, -1) == nil {
		time.Sleep(time.Minute)
	}
}

type mode struct {
	name  string
	begin func(*testing.T) *Session
}

func modes() []mode {
	return []mode{{"direct", directSession}, {"protocol", protocolSession}, {"spawned", spawnedSession}}
}

func directSession(t *testing.T) *Session {
	t.Helper()
	var opener *Opener
	session, err := opener.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func invoker() Account { return Account{UID: os.Getuid(), GID: os.Getgid()} }

func protocolSession(t *testing.T) *Session {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("the helper never serves root")
	}
	local, remote, err := socketPair()
	if err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		serve([]string{HelperMode}, int(remote.Fd()))
		remote.Close()
	}()
	link, err := connect(context.Background(), local, invoker(), requestDeadline, func() {}, exited)
	if err != nil {
		t.Fatal(err)
	}
	session := newSession(link)
	t.Cleanup(func() { session.Close() })
	return session
}

func testLaunch(helper string, deadline time.Duration) launch {
	return launch{deadline: deadline, command: func(account Account, socket *os.File) *exec.Cmd {
		command := helperCommand(account, socket)
		command.Args = []string{helperExecutable, helper}
		command.SysProcAttr.Credential = nil
		return command
	}}
}

func spawnedSession(t *testing.T) *Session {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("the helper never serves root")
	}
	link, err := spawn(context.Background(), invoker(), testLaunch(HelperMode, requestDeadline))
	if err != nil {
		t.Fatal(err)
	}
	session := newSession(link)
	t.Cleanup(func() { session.Close() })
	return session
}

func tempDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func walk(t *testing.T, session *Session, path string) *os.File {
	t.Helper()
	dir, err := session.Root()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range strings.Split(strings.TrimPrefix(path, "/"), "/") {
		next, err := session.OpenAt(dir, name, pathOnly|syscall.O_DIRECTORY)
		dir.Close()
		if err != nil {
			t.Fatalf("open %s beneath its parent: %v", name, err)
		}
		dir = next
	}
	t.Cleanup(func() { dir.Close() })
	return dir
}

func descriptors(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func fileStat(t *testing.T, file *os.File) syscall.Stat_t {
	t.Helper()
	var stat syscall.Stat_t
	if err := syscall.Fstat(int(file.Fd()), &stat); err != nil {
		t.Fatal(err)
	}
	return stat
}

func fcntl(t *testing.T, file *os.File, command int) int {
	t.Helper()
	value, _, errno := syscall.Syscall(syscall.SYS_FCNTL, file.Fd(), uintptr(command), 0)
	if errno != 0 {
		t.Fatal(errno)
	}
	return int(value)
}

func TestEveryOpPassesExactlyOneDescriptorOfTheNamedFile(t *testing.T) {
	for _, m := range modes() {
		t.Run(m.name, func(t *testing.T) {
			session := m.begin(t)
			dir := tempDir(t)
			path := filepath.Join(dir, "image.iso")
			if err := os.WriteFile(path, []byte("installer"), 0600); err != nil {
				t.Fatal(err)
			}
			parent, held := walk(t, session, filepath.Dir(dir)), walk(t, session, dir)
			for _, row := range []struct {
				name, path string
				open       func() (*os.File, error)
			}{
				{"root", "/", session.Root},
				{"a directory beneath a parent", dir, func() (*os.File, error) {
					return session.OpenAt(parent, filepath.Base(dir), pathOnly|syscall.O_DIRECTORY)
				}},
				{"a file beneath a parent", path, func() (*os.File, error) {
					return session.OpenAt(held, "image.iso", syscall.O_RDONLY|syscall.O_NONBLOCK)
				}},
				{"a whole path", path, func() (*os.File, error) { return session.OpenFile(path) }},
			} {
				before := descriptors(t)
				file, err := row.open()
				if err != nil || file == nil {
					t.Fatalf("%s: %v", row.name, err)
				}
				if after := descriptors(t); m.name != "protocol" && after != before+1 {
					t.Errorf("%s added %d descriptors, want 1", row.name, after-before)
				}
				named, err := os.Lstat(row.path)
				if err != nil {
					t.Fatal(err)
				}
				want := named.Sys().(*syscall.Stat_t)
				if got := fileStat(t, file); got.Dev != want.Dev || got.Ino != want.Ino {
					t.Errorf("%s returned a descriptor of another object", row.name)
				}
				file.Close()
			}
		})
	}
}

func TestOpenAtRefusesWritableFlagsEscapingNamesAndForeignParents(t *testing.T) {
	refused := func(t *testing.T, session *Session, parent *os.File, name string, flags int) {
		t.Helper()
		file, err := session.OpenAt(parent, name, flags)
		if file != nil || !errors.Is(err, syscall.EINVAL) {
			t.Errorf("OpenAt(%q, %#x) = %v, %v; want EINVAL and no descriptor", name, flags, file, err)
		}
	}
	foreign, err := os.Open("/")
	if err != nil {
		t.Fatal(err)
	}
	defer foreign.Close()
	exercise := func(t *testing.T, session *Session, root *os.File) {
		for _, flags := range []int{syscall.O_WRONLY, syscall.O_RDWR, syscall.O_CREAT, syscall.O_TRUNC, syscall.O_APPEND, 0x40000000, -1} {
			refused(t, session, root, "tmp", flags)
		}
		for _, name := range []string{"", "..", "a/b", "a\x00b", strings.Repeat("a", 256)} {
			refused(t, session, root, name, syscall.O_RDONLY)
		}
		refused(t, session, foreign, "tmp", pathOnly|syscall.O_DIRECTORY)
		refused(t, session, nil, "tmp", pathOnly|syscall.O_DIRECTORY)
		closed, err := session.Root()
		if err != nil {
			t.Fatal(err)
		}
		closed.Close()
		refused(t, session, closed, "tmp", pathOnly|syscall.O_DIRECTORY)
	}
	t.Run("direct", func(t *testing.T) {
		session := directSession(t)
		root, err := session.Root()
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		exercise(t, session, root)
		if file, err := session.OpenAt(root, ".", pathOnly|syscall.O_DIRECTORY); err != nil {
			t.Errorf("the current directory name was refused: %v", err)
		} else {
			file.Close()
		}
	})
	t.Run("helper", func(t *testing.T) {
		session, peer := fakeSession(t, time.Second)
		root := answered(t, peer, session.Root, "/")
		defer root.Close()
		closedAnswer := make(chan error, 1)
		go func() { closedAnswer <- fakeAnswer(peer, "/") }()
		exercise(t, session, root)
		if err := <-closedAnswer; err != nil {
			t.Fatal(err)
		}
		var pending [64]byte
		if n, _, _, _, err := syscall.Recvmsg(peer, pending[:], nil, syscall.MSG_DONTWAIT); err != syscall.EAGAIN {
			t.Fatalf("a refused open reached the helper: %d bytes, %v", n, err)
		}
	})
}

// fakeSession is root's end of a helper the test plays itself, so a test can
// see what reaches the helper and send replies no helper would.
func fakeSession(t *testing.T, deadline time.Duration) (*Session, int) {
	t.Helper()
	local, remote, err := socketPair()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { remote.Close() })
	peer := int(remote.Fd())
	acknowledged := make(chan error, 1)
	go func() {
		var hello [handshakeBytes]byte
		_, _, _, _, err := syscall.Recvmsg(peer, hello[:], nil, 0)
		if err == nil {
			err = reply(peer, 0, -1)
		}
		acknowledged <- err
	}()
	exited := make(chan struct{})
	close(exited)
	link, err := connect(context.Background(), local, invoker(), deadline, func() {}, exited)
	if err != nil {
		t.Fatal(err)
	}
	if err := <-acknowledged; err != nil {
		t.Fatal(err)
	}
	session := newSession(link)
	t.Cleanup(func() { session.Close() })
	return session, peer
}

func fakeAnswer(peer int, path string) error {
	var message [headerBytes + maxPath]byte
	if _, _, _, _, err := syscall.Recvmsg(peer, message[:], nil, 0); err != nil {
		return err
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)
	return reply(peer, 0, fd)
}

func answered(t *testing.T, peer int, open func() (*os.File, error), path string) *os.File {
	t.Helper()
	sent := make(chan error, 1)
	go func() { sent <- fakeAnswer(peer, path) }()
	file, err := open()
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	if err != nil {
		t.Fatal(err)
	}
	return file
}

func TestNoOpFollowsTheFinalLink(t *testing.T) {
	for _, m := range modes() {
		t.Run(m.name, func(t *testing.T) {
			session := m.begin(t)
			dir := tempDir(t)
			if err := os.WriteFile(filepath.Join(dir, "image.iso"), []byte("installer"), 0600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(dir, "link.iso")
			if err := os.Symlink("image.iso", link); err != nil {
				t.Fatal(err)
			}
			held := walk(t, session, dir)
			if file, err := session.OpenAt(held, "link.iso", syscall.O_RDONLY|syscall.O_NONBLOCK); file != nil || !errors.Is(err, syscall.ELOOP) {
				t.Errorf("OpenAt followed the final link: %v", err)
			}
			file, err := session.OpenFile(link)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if stat := fileStat(t, file); stat.Mode&syscall.S_IFMT != syscall.S_IFLNK {
				t.Errorf("OpenFile returned mode %#o, want the link itself", stat.Mode)
			}
		})
	}
}

func TestOpenFileNeverReadOpensANonRegularFile(t *testing.T) {
	for _, m := range modes() {
		t.Run(m.name, func(t *testing.T) {
			session := m.begin(t)
			dir := tempDir(t)
			fifo := filepath.Join(dir, "fifo")
			if err := syscall.Mkfifo(fifo, 0600); err != nil {
				t.Fatal(err)
			}
			socket := filepath.Join(dir, "socket")
			listener, err := syscall.Socket(syscall.AF_UNIX, syscall.SOCK_STREAM|syscall.SOCK_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer syscall.Close(listener)
			if err := syscall.Bind(listener, &syscall.SockaddrUnix{Name: socket}); err != nil {
				t.Fatal(err)
			}
			opened := make(chan *os.File, 1)
			go func() {
				file, _ := session.OpenFile(fifo)
				opened <- file
			}()
			select {
			case file := <-opened:
				if file != nil {
					file.Close()
				}
			case <-time.After(2 * time.Second):
				if writer, err := os.OpenFile(fifo, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
					writer.Close()
				}
				t.Fatal("opening a FIFO without a writer blocked")
			}
			for _, row := range []struct {
				path string
				kind uint32
			}{{fifo, syscall.S_IFIFO}, {os.DevNull, syscall.S_IFCHR}, {dir, syscall.S_IFDIR}, {socket, syscall.S_IFSOCK}} {
				file, err := session.OpenFile(row.path)
				if err != nil {
					t.Fatalf("%s: %v", row.path, err)
				}
				if stat := fileStat(t, file); stat.Mode&syscall.S_IFMT != row.kind {
					t.Errorf("%s: mode %#o, want %#o", row.path, stat.Mode&syscall.S_IFMT, row.kind)
				}
				if flags := fcntl(t, file, syscall.F_GETFL); flags&pathOnly == 0 {
					t.Errorf("%s was opened for I/O (flags %#x), not as a path", row.path, flags)
				}
				file.Close()
			}
		})
	}
}

func TestReceivedDescriptorsAreCloseOnExec(t *testing.T) {
	for _, m := range modes() {
		t.Run(m.name, func(t *testing.T) {
			session := m.begin(t)
			dir := tempDir(t)
			path := filepath.Join(dir, "image.iso")
			if err := os.WriteFile(path, nil, 0600); err != nil {
				t.Fatal(err)
			}
			held := walk(t, session, dir)
			root, err := session.Root()
			if err != nil {
				t.Fatal(err)
			}
			beneath, err := session.OpenAt(held, "image.iso", syscall.O_RDONLY)
			if err != nil {
				t.Fatal(err)
			}
			whole, err := session.OpenFile(path)
			if err != nil {
				t.Fatal(err)
			}
			for _, file := range []*os.File{root, held, beneath, whole} {
				if fcntl(t, file, syscall.F_GETFD)&syscall.FD_CLOEXEC == 0 {
					t.Errorf("%s survives exec", file.Name())
				}
			}
			root.Close()
			beneath.Close()
			whole.Close()
		})
	}
}

func TestClientRefusesExtraTruncatedOrMalformedReplies(t *testing.T) {
	sendable := make([]int, 3)
	for index := range sendable {
		fd, err := syscall.Open(os.DevNull, syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer syscall.Close(fd)
		sendable[index] = fd
	}
	encoded := func(errno syscall.Errno) []byte {
		var body [replyBytes]byte
		body[0], body[1] = byte(errno), byte(errno>>8)
		return body[:]
	}
	for _, row := range []struct {
		name string
		body []byte
		fds  []int
	}{
		{"two descriptors", encoded(0), sendable[:2]},
		{"a descriptor with a failure", encoded(syscall.ENOENT), sendable[:1]},
		{"a truncated control message", encoded(0), sendable},
		{"an undecodable body", []byte{0, 0, 0}, sendable[:1]},
		{"an oversized body", make([]byte, 64), sendable[:1]},
		{"a success without a descriptor", encoded(0), nil},
		{"an impossible error number", encoded(maxErrno), nil},
	} {
		t.Run(row.name, func(t *testing.T) {
			session, peer := fakeSession(t, time.Second)
			before := descriptors(t)
			sent := make(chan error, 1)
			go func() {
				var message [headerBytes + maxPath]byte
				if _, _, _, _, err := syscall.Recvmsg(peer, message[:], nil, 0); err != nil {
					sent <- err
					return
				}
				var rights []byte
				if len(row.fds) != 0 {
					rights = syscall.UnixRights(row.fds...)
				}
				sent <- syscall.Sendmsg(peer, row.body, rights, nil, 0)
			}()
			file, err := session.OpenFile("/")
			if err := <-sent; err != nil {
				t.Fatal(err)
			}
			if file != nil || !errors.Is(err, errHelper) {
				t.Fatalf("the reply was accepted: %v, %v", file, err)
			}
			if after := descriptors(t); after != before {
				t.Errorf("refusing the reply left %d descriptors behind", after-before)
			}
			if file, err := session.OpenFile("/"); file != nil || !errors.Is(err, errHelper) {
				t.Errorf("the session outlived a refused reply: %v", err)
			}
		})
	}
}

func TestHelperServesOnlyTheAccountItRunsAs(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the helper never serves root")
	}
	hello := func(uid, gid int) []byte {
		var message [handshakeBytes]byte
		for index := 0; index < 4; index++ {
			message[index], message[4+index] = byte(uid>>(8*index)), byte(gid>>(8*index))
		}
		return message[:]
	}
	for _, row := range []struct {
		name  string
		args  []string
		kind  int
		hello []byte
	}{
		{"another account", []string{HelperMode}, syscall.SOCK_SEQPACKET, hello(os.Getuid()+1, os.Getgid())},
		{"another group", []string{HelperMode}, syscall.SOCK_SEQPACKET, hello(os.Getuid(), os.Getgid()+1)},
		{"root", []string{HelperMode}, syscall.SOCK_SEQPACKET, hello(0, os.Getgid())},
		{"extra arguments", []string{HelperMode, "/"}, syscall.SOCK_SEQPACKET, hello(os.Getuid(), os.Getgid())},
		{"a stream socket", []string{HelperMode}, syscall.SOCK_STREAM, hello(os.Getuid(), os.Getgid())},
		{"a short handshake", []string{HelperMode}, syscall.SOCK_SEQPACKET, hello(os.Getuid(), os.Getgid())[:7]},
	} {
		t.Run(row.name, func(t *testing.T) {
			pair, err := syscall.Socketpair(syscall.AF_UNIX, row.kind|syscall.SOCK_CLOEXEC, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer syscall.Close(pair[0])
			if err := syscall.Sendmsg(pair[0], row.hello, nil, nil, 0); err != nil {
				t.Fatal(err)
			}
			if err := syscall.Shutdown(pair[0], syscall.SHUT_WR); err != nil {
				t.Fatal(err)
			}
			code := serve(row.args, pair[1])
			syscall.Close(pair[1])
			if code != 1 {
				t.Errorf("serve exited %d, want 1", code)
			}
			var answer [16]byte
			n, _, _, _, err := syscall.Recvmsg(pair[0], answer[:], nil, 0)
			if n > 0 || err != nil && err != syscall.ECONNRESET {
				t.Errorf("the helper replied %d bytes (%v)", n, err)
			}
		})
	}
	t.Run("its own account", func(t *testing.T) {
		pair, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_SEQPACKET|syscall.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer syscall.Close(pair[1])
		if err := syscall.Sendmsg(pair[0], hello(os.Getuid(), os.Getgid()), nil, nil, 0); err != nil {
			t.Fatal(err)
		}
		exited := make(chan int, 1)
		go func() { exited <- serve([]string{HelperMode}, pair[1]) }()
		var answer [16]byte
		n, _, _, _, err := syscall.Recvmsg(pair[0], answer[:], nil, 0)
		syscall.Close(pair[0])
		if n != replyBytes || err != nil || answer != [16]byte{} {
			t.Errorf("the helper did not acknowledge its own account: %d bytes (%v)", n, err)
		}
		if code := <-exited; code != 0 {
			t.Errorf("serve exited %d at end of input, want 0", code)
		}
	})
}

func TestHelperCommandIsTheRunningBinaryUnderTheInvokingCredentials(t *testing.T) {
	socket, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer socket.Close()
	groups := []uint32{10, 1001}
	command := helperCommand(Account{UID: 1000, GID: 1001, Groups: groups}, socket)
	groups[0] = 0
	if command.Path != "/proc/self/exe" || strings.Join(command.Args, " ") != "/proc/self/exe "+HelperMode {
		t.Errorf("the helper runs %s %q, want this running binary", command.Path, command.Args)
	}
	if strings.Join(command.Env, " ") != "LANG=C LC_ALL=C" || command.Dir != "/" || command.Stdin != nil || command.Stdout != nil || command.Stderr != nil {
		t.Errorf("the helper inherits environment %q, directory %q or standard streams", command.Env, command.Dir)
	}
	if len(command.ExtraFiles) != 1 || command.ExtraFiles[0] != socket || command.WaitDelay != time.Second {
		t.Errorf("the helper holds %d extra files and waits %s", len(command.ExtraFiles), command.WaitDelay)
	}
	attributes := command.SysProcAttr
	if attributes == nil || attributes.Credential == nil || attributes.Pdeathsig != syscall.SIGKILL {
		t.Fatalf("the helper runs without the invoking credentials or a parent-death signal: %+v", attributes)
	}
	credential := attributes.Credential
	if credential.Uid != 1000 || credential.Gid != 1001 || len(credential.Groups) != 2 || credential.Groups[0] != 10 || credential.Groups[1] != 1001 || credential.NoSetGroups {
		t.Errorf("the helper runs as %+v", credential)
	}
	if production.deadline != 10*time.Second {
		t.Errorf("each request is bounded by %s, want 10s", production.deadline)
	}
}

func TestDirectSessionsNeverResolveTheAccount(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("a root process resolves the invoking account")
	}
	opener := New(func(context.Context) (Account, error) {
		t.Fatal("a process that is not root resolved the invoking account")
		return Account{}, nil
	})
	for _, candidate := range []*Opener{opener, nil} {
		session, err := candidate.Begin(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if link, ok := session.link.(direct); !ok || link.root {
			t.Errorf("a process that is not root began %#v", session.link)
		}
		session.Close()
	}
}

func TestOnlyADirectRootSessionReportsADenialToRoot(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode-0 file")
	}
	path := filepath.Join(tempDir(t), "image.iso")
	if err := os.WriteFile(path, nil, 0); err != nil {
		t.Fatal(err)
	}
	type denial interface{ DeniedToRoot() bool }
	for _, row := range []struct {
		link link
		want bool
	}{{direct{root: true}, true}, {direct{}, false}} {
		session := newSession(row.link)
		_, err := session.OpenFile(path)
		var denied denial
		if !errors.Is(err, syscall.EACCES) || !errors.As(err, &denied) || denied.DeniedToRoot() != row.want {
			t.Errorf("%#v: %v, want EACCES denied to root %t", row.link, err, row.want)
		}
		_, err = session.OpenFile(filepath.Join(filepath.Dir(path), "absent.iso"))
		if !errors.Is(err, syscall.ENOENT) || !errors.As(err, &denied) || denied.DeniedToRoot() {
			t.Errorf("%#v: an absent file was reported as a denial: %v", row.link, err)
		}
		session.Close()
	}
	session := protocolSession(t)
	_, err := session.OpenFile(path)
	var denied denial
	if !errors.Is(err, syscall.EACCES) || !errors.As(err, &denied) || denied.DeniedToRoot() {
		t.Errorf("a helper's denial was reported as root's: %v", err)
	}
}

func TestAStalledOrCancelledHelperEndsTheSession(t *testing.T) {
	await := func(t *testing.T, link *channel, result <-chan error, within time.Duration) error {
		t.Helper()
		select {
		case err := <-result:
			return err
		case <-time.After(within):
			link.kill()
			t.Fatal("the call outlived its bound")
		}
		return nil
	}
	reaped := func(t *testing.T, link *channel) {
		t.Helper()
		select {
		case <-link.exited:
		case <-time.After(2 * time.Second):
			link.kill()
			t.Fatal("the helper was not killed and reaped")
		}
	}
	t.Run("stalled", func(t *testing.T) {
		link, err := spawn(context.Background(), invoker(), testLaunch(stallMode, 200*time.Millisecond))
		if err != nil {
			t.Fatal(err)
		}
		session := newSession(link)
		defer session.Close()
		result := make(chan error, 1)
		started := time.Now()
		go func() {
			_, err := session.Root()
			result <- err
		}()
		if err := await(t, link, result, 5*time.Second); !errors.Is(err, errHelper) || time.Since(started) > 2*time.Second {
			t.Errorf("a stalled helper failed after %s with %v", time.Since(started), err)
		}
		reaped(t, link)
		if _, err := session.Root(); !errors.Is(err, errHelper) {
			t.Errorf("the session outlived its stalled helper: %v", err)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		link, err := spawn(ctx, invoker(), testLaunch(stallMode, requestDeadline))
		if err != nil {
			t.Fatal(err)
		}
		session := newSession(link)
		defer session.Close()
		result := make(chan error, 1)
		go func() {
			_, err := session.Root()
			result <- err
		}()
		time.Sleep(100 * time.Millisecond)
		cancel()
		started := time.Now()
		if err := await(t, link, result, 5*time.Second); !errors.Is(err, context.Canceled) || time.Since(started) > 2*time.Second {
			t.Errorf("cancellation ended the call after %s with %v", time.Since(started), err)
		}
		reaped(t, link)
	})
}

func TestRootHelperOpensWithTheInvokingCredentials(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("only root starts a helper under another account")
	}
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	opener := New(func(context.Context) (Account, error) { return Account{UID: 65534, GID: 65534}, nil })
	session, err := opener.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	file, err := session.OpenFile(path)
	var denied interface{ DeniedToRoot() bool }
	if file != nil || !errors.Is(err, syscall.EACCES) || !errors.As(err, &denied) || denied.DeniedToRoot() {
		t.Fatalf("the helper opened a root-only file or blamed root: %v", err)
	}
}
