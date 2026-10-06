//go:build linux && amd64

package invokerfs

import (
	"encoding/binary"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	opRoot byte = iota + 1
	opOpenAt
	opOpenFile
)

const (
	helperSocket   = 3
	headerBytes    = 8
	handshakeBytes = 8
	replyBytes     = 4
	maxErrno       = 4096
)

// request is one open, encoded as raw bytes rather than text so that a name
// reaches openat(2) exactly as the operator spelled it, whatever its encoding.
type request struct {
	op    byte
	flags int
	name  string
}

func (r request) valid() bool {
	switch r.op {
	case opRoot:
		return r.flags == 0 && r.name == ""
	case opOpenAt:
		return r.flags&^openAtFlags == 0 && validName(r.name)
	case opOpenFile:
		return r.flags == 0 && validPath(r.name)
	}
	return false
}

func validName(name string) bool {
	return name != "" && name != ".." && len(name) <= maxName && !strings.ContainsAny(name, "/\x00")
}

func validPath(path string) bool {
	return len(path) <= maxPath && filepath.IsAbs(path) && filepath.Clean(path) == path && !strings.ContainsRune(path, 0)
}

func (r request) parents() int {
	if r.op == opOpenAt {
		return 1
	}
	return 0
}

func (r request) label() string {
	if r.op == opRoot {
		return "/"
	}
	return r.name
}

func (r request) perform(parent int) (int, syscall.Errno) {
	switch r.op {
	case opRoot:
		return openRoot()
	case opOpenAt:
		return openAt(parent, r.name, r.flags)
	case opOpenFile:
		return openFile(r.name)
	}
	return -1, syscall.EINVAL
}

func (r request) encode() []byte {
	message := make([]byte, headerBytes+len(r.name))
	message[0] = r.op
	binary.LittleEndian.PutUint32(message[4:headerBytes], uint32(r.flags))
	copy(message[headerBytes:], r.name)
	return message
}

func decodeRequest(message []byte) (request, bool) {
	if len(message) < headerBytes || len(message) > headerBytes+maxPath || message[1] != 0 || message[2] != 0 || message[3] != 0 {
		return request{}, false
	}
	r := request{op: message[0], flags: int(binary.LittleEndian.Uint32(message[4:headerBytes])), name: string(message[headerBytes:])}
	return r, r.valid()
}

// ServeHelper accepts only the private open protocol on descriptor 3, under
// the account that named the paths. It executes nothing, writes no file and
// keeps no descriptor between requests.
func ServeHelper(args []string) (bool, int) {
	if len(args) == 0 || args[0] != HelperMode {
		return false, 0
	}
	return true, serve(args, helperSocket)
}

func serve(args []string, socket int) int {
	if len(args) != 1 || args[0] != HelperMode || !seqpacket(socket) {
		return 1
	}
	uid, gid, ok := handshake(socket)
	if !ok || uid == 0 || syscall.Getuid() != uid || syscall.Geteuid() != uid || syscall.Getgid() != gid || syscall.Getegid() != gid {
		return 1
	}
	if reply(socket, 0, -1) != nil {
		return 1
	}
	for {
		if code, done := answer(socket); done {
			return code
		}
	}
}

func seqpacket(socket int) bool {
	kind, err := syscall.GetsockoptInt(socket, syscall.SOL_SOCKET, syscall.SO_TYPE)
	if err != nil || kind != syscall.SOCK_SEQPACKET {
		return false
	}
	domain, err := syscall.GetsockoptInt(socket, syscall.SOL_SOCKET, syscall.SO_DOMAIN)
	return err == nil && domain == syscall.AF_UNIX
}

func handshake(socket int) (int, int, bool) {
	var message [handshakeBytes + 1]byte
	n, fds, ok := receiveRequest(socket, message[:])
	closeAll(fds)
	if !ok || n != handshakeBytes || len(fds) != 0 {
		return 0, 0, false
	}
	return int(binary.LittleEndian.Uint32(message[:4])), int(binary.LittleEndian.Uint32(message[4:])), true
}

func answer(socket int) (int, bool) {
	var message [headerBytes + maxPath + 1]byte
	n, fds, ok := receiveRequest(socket, message[:])
	defer closeAll(fds)
	if ok && n == 0 && len(fds) == 0 {
		return 0, true
	}
	r, valid := decodeRequest(message[:n])
	if !ok || !valid || len(fds) != r.parents() {
		return 1, true
	}
	parent := -1
	if len(fds) == 1 {
		parent = fds[0]
	}
	fd, errno := r.perform(parent)
	err := reply(socket, errno, fd)
	if fd >= 0 {
		syscall.Close(fd)
	}
	if err != nil {
		return 1, true
	}
	return 0, false
}

func receiveRequest(socket int, message []byte) (int, []int, bool) {
	oob := make([]byte, syscall.CmsgSpace(4))
	for {
		n, oobn, flags, _, err := syscall.Recvmsg(socket, message, oob, syscall.MSG_CMSG_CLOEXEC)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return 0, nil, false
		}
		fds, refused := received(oob[:oobn])
		if refused || flags&(syscall.MSG_TRUNC|syscall.MSG_CTRUNC) != 0 {
			return 0, fds, false
		}
		return n, fds, true
	}
}

func reply(socket int, errno syscall.Errno, fd int) error {
	var body [replyBytes]byte
	binary.LittleEndian.PutUint32(body[:], uint32(errno))
	var rights []byte
	if fd >= 0 {
		rights = syscall.UnixRights(fd)
	}
	for {
		if err := syscall.Sendmsg(socket, body[:], rights, nil, syscall.MSG_NOSIGNAL); err != syscall.EINTR {
			return err
		}
	}
}

// received returns every descriptor a message carried, so a caller can close
// all of them, and whether it carried anything other than descriptors.
func received(oob []byte) ([]int, bool) {
	if len(oob) == 0 {
		return nil, false
	}
	messages, err := syscall.ParseSocketControlMessage(oob)
	if err != nil {
		return nil, true
	}
	var fds []int
	refused := false
	for index := range messages {
		rights, err := syscall.ParseUnixRights(&messages[index])
		if err != nil {
			refused = true
			continue
		}
		fds = append(fds, rights...)
	}
	return fds, refused
}

func closeAll(fds []int) {
	for _, fd := range fds {
		syscall.Close(fd)
	}
}
