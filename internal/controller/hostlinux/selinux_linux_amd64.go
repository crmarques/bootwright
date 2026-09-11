//go:build linux && amd64

package hostlinux

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

const maxSELinuxPolicyBytes = 32 << 20

type selinuxObservation struct {
	fs     *heldFilesystem
	root   *os.File
	status [20]byte
}

func (f *heldFilesystem) selinuxPolicy(ctx context.Context, mode string) (*selinuxObservation, error) {
	if mode == "" {
		return nil, nil
	}
	root, err := f.kernelRoot(ctx, "/sys/fs/selinux", unix.SELINUX_MAGIC)
	if err != nil {
		return nil, err
	}
	observation := &selinuxObservation{fs: f, root: root}
	status, err := observation.readStatus(ctx)
	if err == nil {
		err = observation.enforcing(ctx)
	}
	if err == nil {
		err = observation.policy(ctx)
	}
	if err != nil {
		observation.close()
		return nil, err
	}
	observation.status = status
	if err := observation.verify(ctx); err != nil {
		observation.close()
		return nil, err
	}
	return observation, nil
}

func (s *selinuxObservation) close() { s.root.Close() }

func (s *selinuxObservation) verify(ctx context.Context) error {
	if err := s.enforcing(ctx); err != nil {
		return err
	}
	status, err := s.readStatus(ctx)
	if err != nil || status != s.status {
		return errEvidence
	}
	return nil
}

func (s *selinuxObservation) enforcing(ctx context.Context) error {
	data, err := s.small(ctx, "enforce", 1)
	if err != nil || !bytes.Equal(data, []byte("1")) {
		return errEvidence
	}
	return nil
}

// SELinux status v1 is five native-endian u32 values on the qualified amd64
// platform. Equal, even sequence observations bracket the entire runtime proof,
// including the kernel policy snapshot and all regular files and aliases.
func (s *selinuxObservation) readStatus(ctx context.Context) ([20]byte, error) {
	var result [20]byte
	first, err := s.small(ctx, "status", int64(len(result)))
	if err != nil || len(first) != len(result) {
		return result, errEvidence
	}
	second, err := s.small(ctx, "status", int64(len(result)))
	if err != nil || !bytes.Equal(first, second) || binary.LittleEndian.Uint32(first[0:4]) != 1 || binary.LittleEndian.Uint32(first[4:8])&1 != 0 || binary.LittleEndian.Uint32(first[8:12]) != 1 || binary.LittleEndian.Uint32(first[12:16]) == 0 || binary.LittleEndian.Uint32(first[16:20]) > 1 {
		return result, errEvidence
	}
	copy(result[:], first)
	return result, nil
}

// The kernel has already admitted this policy. Its bounded readable snapshot
// and unchanged load generation establish active operator-owned policy; local
// modules and generated contexts are deliberately not publisher byte pins.
func (s *selinuxObservation) policy(ctx context.Context) error {
	file, before, err := s.open(ctx, "policy")
	if err != nil {
		return err
	}
	defer file.Close()
	if before.Size <= 0 || before.Size > maxSELinuxPolicyBytes {
		return errEvidence
	}
	n, err := io.Copy(io.Discard, io.LimitReader(contextReader{ctx: ctx, reader: file}, before.Size+1))
	var after unix.Stat_t
	if err != nil || n != before.Size || unix.Fstat(int(file.Fd()), &after) != nil || !stable(before, after) {
		return errEvidence
	}
	return ctx.Err()
}

func (s *selinuxObservation) small(ctx context.Context, name string, maximum int64) ([]byte, error) {
	file, before, err := s.open(ctx, name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(contextReader{ctx: ctx, reader: file}, maximum+1))
	var after unix.Stat_t
	if err != nil || int64(len(data)) > maximum || unix.Fstat(int(file.Fd()), &after) != nil || !stable(before, after) {
		return nil, errEvidence
	}
	return data, ctx.Err()
}

// Opening SELinux policy materializes a per-descriptor kernel snapshot and may
// update the virtual inode size. Its first size check must therefore occur
// after open. No regular-file path resolver or pre-open size assumption applies.
func (s *selinuxObservation) open(ctx context.Context, name string) (*os.File, unix.Stat_t, error) {
	if err := ctx.Err(); err != nil {
		return nil, unix.Stat_t{}, err
	}
	if name != "policy" && name != "status" && name != "enforce" {
		return nil, unix.Stat_t{}, errEvidence
	}
	fd, err := unix.Openat(int(s.root.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK|unix.O_NOATIME, 0)
	if err != nil {
		return nil, unix.Stat_t{}, err
	}
	file := os.NewFile(uintptr(fd), "/sys/fs/selinux/"+name)
	var stat unix.Stat_t
	if unix.Fstat(fd, &stat) != nil || !s.fs.regular(stat) || s.fs.filesystemType(file, "/sys/fs/selinux/"+name) != unix.SELINUX_MAGIC {
		file.Close()
		return nil, unix.Stat_t{}, errEvidence
	}
	return file, stat, nil
}
