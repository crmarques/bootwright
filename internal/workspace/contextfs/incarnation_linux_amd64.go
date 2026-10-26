//go:build linux && amd64

package contextfs

import (
	"fmt"
	"strconv"

	"golang.org/x/sys/unix"

	"github.com/crmarques/bootwright/internal/workspace/contexts"
)

// getInodeGeneration is FS_IOC_GETVERSION, _IOR('v', 1, long) in
// linux/fs.h, which golang.org/x/sys/unix does not name.
const getInodeGeneration = 0x80087601

// directoryBirth is what tells a context directory from a later one a
// filesystem gave the same freed inode: its birth time, or its inode
// generation where the filesystem reports no birth time.
type directoryBirth struct {
	born       bool
	seconds    int64
	nanos      uint32
	generation uint32
}

// incarnation names the context directory the registry holds, which a
// recreated context of the same name never shares; a directory that reports
// neither a birth time nor a generation refuses rather than name only an
// inode a filesystem reuses.
func incarnation(record contexts.Record, dir *directory) (string, error) {
	if record.DirectoryInode == 0 {
		return "", nil
	}
	birth, err := birthOf(int(dir.file.Fd()))
	if err != nil {
		return "", err
	}
	return formatIncarnation(record.DirectoryDevice, record.DirectoryInode, birth), nil
}

func birthOf(fd int) (directoryBirth, error) {
	var stat unix.Statx_t
	if err := unix.Statx(fd, "", unix.AT_EMPTY_PATH, unix.STATX_BTIME, &stat); err == nil && stat.Mask&unix.STATX_BTIME != 0 {
		return directoryBirth{born: true, seconds: stat.Btime.Sec, nanos: stat.Btime.Nsec}, nil
	}
	generation, err := unix.IoctlGetInt(fd, getInodeGeneration)
	if err != nil {
		return directoryBirth{}, state("the context directory's filesystem reports neither a birth time nor an inode generation, so a context recreated under the same name cannot be told apart from it")
	}
	return directoryBirth{generation: uint32(generation)}, nil
}

func formatIncarnation(device, inode uint64, birth directoryBirth) string {
	prefix := strconv.FormatUint(device, 10) + ":" + strconv.FormatUint(inode, 10) + ":"
	if birth.born {
		return prefix + "b" + strconv.FormatInt(birth.seconds, 10) + "." + fmt.Sprintf("%09d", birth.nanos)
	}
	return prefix + "g" + strconv.FormatUint(uint64(birth.generation), 10)
}
