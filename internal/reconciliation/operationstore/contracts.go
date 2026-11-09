package operationstore

import "context"

type Entry struct {
	Name      string
	Directory bool
	Size      int64
}

// Area is the Workspace-held filesystem capability this store publishes
// through. It is valid only inside its owning callback, confines every path to
// one subtree, the context's operation records or one of its log directories,
// and performs no interpretation of content.
// Read reports whether the object exists rather than treating absence as an
// error, because absence is ordinary evidence during recovery.
type Area interface {
	Read(ctx context.Context, path string, maximum int) ([]byte, bool, error)
	Entries(ctx context.Context, path string) ([]Entry, error)
	EnsureDirectory(ctx context.Context, path string) error
	// WriteExclusive refuses an existing destination, so a reused attempt
	// number can never overwrite durable evidence.
	WriteExclusive(ctx context.Context, path string, data []byte) error
	// Replace publishes atomically after proving the destination still holds
	// exactly expected. A nil expectation requires the destination to be absent.
	Replace(ctx context.Context, path string, data, expected []byte) error
	Append(ctx context.Context, path string, data []byte) error
	// RemoveDirectory removes one empty directory, durably when it returns. It
	// refuses a directory that holds anything, a record and the area itself,
	// and a directory that is already absent is removed.
	RemoveDirectory(ctx context.Context, path string) error
	// RemoveRecord removes one record while it holds exactly expected, durably
	// when it returns, and leaves the directories above it. It refuses a
	// directory and a record holding other bytes, and a record that is already
	// absent is removed.
	RemoveRecord(ctx context.Context, path string, expected []byte) error
	// Sync makes a directory durable and resolves every component of its path
	// as one, so it names a directory and never a record. A record is already
	// durable when WriteExclusive or Replace returns.
	Sync(ctx context.Context, path string) error
	// Location reports where this subtree is on the host, so a human result can
	// name a log an operator is able to open while it is still being written.
	// It is presentation material only: nothing resolves a path through it, and
	// an implementation with no host location reports the empty string.
	Location() string
	// Reference is this subtree's slash-separated path relative to the root
	// it lies under, the log root for a log area, which is how a structured
	// result names a log. Like Location it is presentation material only, and
	// an implementation that cannot place the subtree under its root reports
	// the empty string.
	Reference() string
}

// LogRetention is what a log area keeping many operations' logs also offers.
// Register calls both once the index names the new operation, best effort:
// logs are troubleshooting material, so neither changes a registration.
type LogRetention interface {
	Retain(ctx context.Context, keep []string)
	MarkCurrent(ctx context.Context, id string)
}
