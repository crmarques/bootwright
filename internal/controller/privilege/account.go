package privilege

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Account struct {
	UID, GID      int
	Home, Name    string
	Groups        []uint32
	SudoParentPID int
}

// Resolver acquires the invoking account from the account database only when
// Resolve is called.
type Resolver struct{}

type accountRequest struct {
	uid, euid                  int
	sudoUID, sudoGID, sudoUser string
	sudoCommand                string
	trustedParent              bool
}

var errAccount = errors.New("invoking account identity cannot be verified")

// accountRefusal is errAccount with the one fixed reason Admit names.
type accountRefusal struct{ reason string }

func (r accountRefusal) Error() string { return errAccount.Error() + ": " + r.reason }

func (r accountRefusal) Unwrap() error { return errAccount }

const (
	maxAccountGroups    = 1024
	unansweredDirectory = "the account database did not answer"
	disagreeingSudo     = "sudo's account metadata is incomplete or disagrees with the account database"
	reexecutionOrphaned = "the sudo parent of this re-execution is gone"
)

type passwdEntry struct {
	name     string
	uid, gid int
	home     string
}

// accountDirectory is the account database: the name service, or the account
// files where the system has no getent. A lookup fails with errNoEntry, with
// errMalformed for an answer that is not exactly one well-formed entry, or with
// any other error when the database did not answer.
type accountDirectory interface {
	byUID(ctx context.Context, uid int) (passwdEntry, error)
	byName(ctx context.Context, name string) (passwdEntry, error)
	groupsOf(ctx context.Context, name string) ([]uint32, error)
}

var (
	errNoEntry   = errors.New("no account entry")
	errMalformed = errors.New("malformed account answer")
)

// accountFrom resolves the invoking account. A root process whose sudo
// metadata no verified sudo parent supplied, such as a sudo -i or sudo -s
// shell, resolves as direct root: it is root already, so that grants nothing
// and only selects root's own context selection and files. A procfs command is
// what a Bootwright supervisor hands sudo, so that metadata belongs to an
// elevated child whose sudo is gone, which refuses.
func accountFrom(ctx context.Context, directory accountDirectory, request accountRequest) (Account, error) {
	if request.uid != request.euid {
		return Account{}, accountRefusal{"real and effective user differ"}
	}
	uid, manual := request.uid, false
	if request.euid == 0 && (request.sudoUID != "" || request.sudoGID != "" || request.sudoUser != "") {
		switch {
		case request.trustedParent:
			parsed, err := decimalID(request.sudoUID)
			if err != nil || request.sudoGID == "" || request.sudoUser == "" {
				return Account{}, accountRefusal{disagreeingSudo}
			}
			uid, manual = parsed, true
		case procfsReexecution(request.sudoCommand):
			return Account{}, accountRefusal{reexecutionOrphaned}
		}
	}
	ambiguous := "the account database answered ambiguously for UID " + strconv.Itoa(uid)
	entry, err := directory.byUID(ctx, uid)
	if err != nil {
		return Account{}, lookupRefusal(ctx, err, "the account database has no entry for UID "+strconv.Itoa(uid), ambiguous)
	}
	if entry.uid != uid || !accountName(entry.name) {
		return Account{}, accountRefusal{ambiguous}
	}
	if !accountHome(entry.home) {
		return Account{}, accountRefusal{"the account " + entry.name + " has no clean absolute home"}
	}
	inconsistent := "the account database answered inconsistently for " + entry.name
	named, err := directory.byName(ctx, entry.name)
	if err != nil {
		return Account{}, lookupRefusal(ctx, err, inconsistent, inconsistent)
	}
	if named != entry {
		return Account{}, accountRefusal{inconsistent}
	}
	if manual && (request.sudoUser != entry.name || request.sudoGID != strconv.Itoa(entry.gid)) {
		return Account{}, accountRefusal{disagreeingSudo}
	}
	groups, err := directory.groupsOf(ctx, entry.name)
	if err != nil {
		return Account{}, lookupRefusal(ctx, err, inconsistent, inconsistent)
	}
	set := map[uint32]bool{uint32(entry.gid): true}
	for _, gid := range groups {
		set[gid] = true
	}
	if len(set) > maxAccountGroups {
		return Account{}, accountRefusal{"the account " + entry.name + " is in more than 1024 groups"}
	}
	result := Account{UID: uid, GID: entry.gid, Name: entry.name, Home: entry.home}
	for gid := range set {
		result.Groups = append(result.Groups, gid)
	}
	sort.Slice(result.Groups, func(i, j int) bool { return result.Groups[i] < result.Groups[j] })
	return result, nil
}

func lookupRefusal(ctx context.Context, err error, absent, malformed string) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	switch {
	case errors.Is(err, errNoEntry):
		return accountRefusal{absent}
	case errors.Is(err, errMalformed):
		return accountRefusal{malformed}
	}
	return accountRefusal{unansweredDirectory}
}

// procfsReexecution reports whether sudo's command, its path and then its
// arguments joined by spaces, runs /proc/<pid>/exe.
func procfsReexecution(command string) bool {
	path, _, _ := strings.Cut(command, " ")
	pid, found := strings.CutPrefix(path, "/proc/")
	pid, exe := strings.CutSuffix(pid, "/exe")
	return found && exe && pid != "" && strings.Trim(pid, "0123456789") == ""
}

// accountName admits a name that is safe to report and to compare with sudo's:
// no colon and no control character, which also excludes NUL and LF.
func accountName(name string) bool {
	if name == "" || len(name) > 256 || name[0] == '-' || !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		if r == ':' || unicode.IsControl(r) {
			return false
		}
	}
	return true
}

func accountHome(home string) bool {
	return filepath.IsAbs(home) && filepath.Clean(home) == home && len(home) <= 4096 && !strings.ContainsRune(home, 0)
}

// accountFiles answers from /etc/passwd and /etc/group, where two lines
// carrying one UID, or one name carrying two UIDs, are ambiguous.
type accountFiles struct{ passwd, groups []byte }

func (f accountFiles) byUID(_ context.Context, uid int) (passwdEntry, error) {
	key := strconv.Itoa(uid)
	return f.entry(func(fields []string) bool { return fields[2] == key })
}

func (f accountFiles) byName(_ context.Context, name string) (passwdEntry, error) {
	return f.entry(func(fields []string) bool { return fields[0] == name })
}

func (f accountFiles) entry(matches func([]string) bool) (passwdEntry, error) {
	var result passwdEntry
	found := false
	for _, line := range strings.Split(string(f.passwd), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) != 7 || !matches(fields) {
			continue
		}
		entry, err := passwdFields(fields)
		if err != nil || found {
			return passwdEntry{}, errMalformed
		}
		result, found = entry, true
	}
	if !found {
		return passwdEntry{}, errNoEntry
	}
	return result, nil
}

func (f accountFiles) groupsOf(_ context.Context, name string) ([]uint32, error) {
	var groups []uint32
	for _, line := range strings.Split(string(f.groups), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) != 4 {
			continue
		}
		for _, member := range strings.Split(fields[3], ",") {
			if member != name {
				continue
			}
			gid, err := decimalID(fields[2])
			if err != nil {
				return nil, errMalformed
			}
			groups = append(groups, uint32(gid))
		}
	}
	return groups, nil
}

func passwdFields(fields []string) (passwdEntry, error) {
	uid, uidErr := decimalID(fields[2])
	gid, gidErr := decimalID(fields[3])
	if uidErr != nil || gidErr != nil {
		return passwdEntry{}, errMalformed
	}
	return passwdEntry{name: fields[0], uid: uid, gid: gid, home: fields[5]}, nil
}

func decimalID(value string) (int, error) {
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil || strconv.FormatUint(n, 10) != value {
		return 0, errAccount
	}
	return int(n), nil
}
