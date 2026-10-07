//go:build linux && amd64

package contextfs

import (
	"context"
	"errors"
	"os"
	"syscall"

	"github.com/crmarques/bootwright/internal/controller/prerequisites"
)

var _ prerequisites.StateRootInspector = (*Store)(nil)

// InspectStateRoot looks at the state root as a setup dry run may: without
// privilege, without reading a record unless this process owns the root, and
// without creating, repairing or changing anything. It reports a root this
// build cannot use with the refusal every store command would give it.
func (s *Store) InspectStateRoot(ctx context.Context) (prerequisites.StateRootInspection, error) {
	if err := ctx.Err(); err != nil {
		return prerequisites.StateRootInspection{}, err
	}
	path, err := s.rootPath()
	if err != nil {
		return prerequisites.StateRootInspection{}, err
	}
	uid, gid := uint32(0), uint32(0)
	if s.options.Owner != nil {
		if s.options.Root == "" {
			return prerequisites.StateRootInspection{}, state("test ownership requires an isolated explicit root")
		}
		uid, gid = s.options.Owner.UID, s.options.Owner.GID
	}
	owner := uint32(os.Geteuid()) == uid && uint32(os.Getegid()) == gid
	return s.inspectStateRoot(ctx, path, uid, gid, owner)
}

// inspectStateRoot judges the root at path for an owner uid:gid. Only the
// owner may list a private root, so for any other process a root of the right
// type, owner, mode and filesystem is as far as the inspection can see, and
// setup verifies its contents.
func (s *Store) inspectStateRoot(ctx context.Context, path string, uid, gid uint32, owner bool) (prerequisites.StateRootInspection, error) {
	inspection := prerequisites.StateRootInspection{Required: "a directory owned by " + ownerText(uid, gid) + " with mode 0700 on a local filesystem"}
	var stat syscall.Stat_t
	if err := syscall.Lstat(path, &stat); err != nil {
		if errors.Is(err, syscall.ENOENT) {
			inspection.Observed, inspection.Status = "absent; setup creates it", "ready"
			return inspection, nil
		}
		inspection.Observed, inspection.Status = "not inspectable without privilege; setup verifies it", "unverified"
		return inspection, nil
	}
	if !private(stat, syscall.S_IFDIR, uid, gid) {
		inspection.Observed, inspection.Status = fileKind(stat.Mode)+" owned by "+ownerText(stat.Uid, stat.Gid)+" with mode "+modeText(stat.Mode), "not-ready"
		inspection.Refusal = unsafeRoot(stat, uid, gid)
		return inspection, nil
	}
	var filesystem syscall.Statfs_t
	if err := syscall.Statfs(path, &filesystem); err != nil || !localFilesystem(filesystem.Type) {
		inspection.Observed, inspection.Status = "not on a qualified local filesystem", "not-ready"
		inspection.Refusal = state("state filesystem is not qualified for private durable storage")
		return inspection, nil
	}
	held := ownerText(stat.Uid, stat.Gid) + " " + modeText(stat.Mode)
	if !owner {
		inspection.Observed, inspection.Status = held+"; setup verifies its contents", "ready"
		return inspection, nil
	}
	if _, err := s.View(ctx); err != nil {
		if canceled := ctx.Err(); canceled != nil {
			return prerequisites.StateRootInspection{}, canceled
		}
		inspection.Observed, inspection.Status, inspection.Refusal = held+" holding state this build cannot read", "not-ready", err
		return inspection, nil
	}
	inspection.Observed, inspection.Status = held, "ready"
	return inspection, nil
}
