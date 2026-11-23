package account

import (
	"bytes"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const repeat = ", then repeat the operation"

var (
	workerReport = regexp.MustCompile(`^bootwright-enrollment-refused ([a-z-]{1,24}) (E[A-Z0-9]{1,15}|[a-z][a-z-]{0,23}|exit-[0-9]{1,3}|signal-[0-9]{1,2})$`)
	statusReason = regexp.MustCompile(`^(exit|signal)-([0-9]+)$`)
)

var (
	errWorkerUnnamed = &cause{"the enrollment program refused for {login} without a reason this build recognizes",
		remedyDefect}
	errRequestRejected = &cause{"rejected the enrollment request this build sent", "report this as a Bootwright defect"}
	errRemoteFailed    = &cause{"remote sudo or the enrollment program failed for {login} without reporting a reason",
		"check the sudo entries for {login} in the host's journal (journalctl -t sudo)" + repeat}
	errProgramUnstartable = &cause{"could not start the enrollment program for {login}: /usr/bin/env, /usr/bin/timeout or /usr/bin/python3 is missing or cannot run",
		"install coreutils and python3 on the host" + repeat}
)

type refusalKey struct{ step, reason string }

var (
	causeRuleFile = &cause{"has a file /etc/sudoers.d/90-bootwright-{managed} that is not the regular root-owned grant Bootwright writes",
		"remove that file once nothing needs it" + repeat}
	causeKeysForeign = &cause{"has authorized keys for {managed} that Bootwright did not write",
		"remove /home/{managed}-*/.ssh/authorized_keys once nothing needs it" + repeat}
	causeHomeForeign = &cause{"has a home for {managed} that the account does not own or that other users can write",
		"remove /home/{managed}-* once nothing needs it" + repeat}
	causeAbandoned = &cause{"has an authorized key file under /home/{managed}-* but no account {managed}",
		"remove what an abandoned operation left under /home/{managed}-*" + repeat}
	causeUnproved = &cause{"could not prove the account {managed}, its home and its sudo rule removed",
		"inspect them on the host and remove what remains" + repeat}
)

var hostStates = map[refusalKey]*cause{
	{"account", "other-operation"}: {"already has the account {managed}, which another Bootwright operation created and this context does not record",
		"if no context still manages it, remove the account with userdel, then /etc/sudoers.d/90-bootwright-{managed} and /home/{managed}-*, and repeat the operation; otherwise continue the operation that created it"},
	{"account", "not-bootwright"}: {"already has an account named {managed} that Bootwright did not create",
		"declare another spec.access.lifecycle.user for this Machine, or remove that account once nothing uses it"},
	{"account", "home-differs"}: {"has the account {managed} that this operation created, but with another home",
		"restore the account's home or remove the account with userdel" + repeat},
	{"account", "system-uid"}: {"has the account {managed} with a UID below the host's UID_MIN, which Bootwright refuses as a system account",
		"remove the account with userdel" + repeat},
	{"account", "unlisted"}: {"created the account {managed}, but the host's name service does not return it",
		"check that /etc/nsswitch.conf reads local files for passwd and that no name-service cache hides new accounts" + repeat},
	{"rule", "foreign"}: {"has a sudo rule /etc/sudoers.d/90-bootwright-{managed} that Bootwright did not write",
		"remove that file once nothing needs it" + repeat},
	{"rule", "orphan"}: {"has the sudo rule /etc/sudoers.d/90-bootwright-{managed} but no account {managed}",
		"remove what an abandoned operation left, that file and /home/{managed}-*" + repeat},
	{"rule", "unsafe"}:          causeRuleFile,
	{"rule", "oversized"}:       causeRuleFile,
	{"rule-publish", "foreign"}: causeRuleFile,
	{"rule-publish", "unsafe"}:  causeRuleFile,
	{"keys", "exists"}:          causeAbandoned,
	{"keys", "foreign"}:         causeKeysForeign,
	{"keys", "unsafe"}:          causeKeysForeign,
	{"keys", "oversized"}:       causeKeysForeign,
	{"home", "exists"}: {"has a home under /home/{managed}-* but no account {managed}",
		"remove what an abandoned operation left there" + repeat},
	{"home", "foreign"}:          causeHomeForeign,
	{"ssh-directory", "foreign"}: causeHomeForeign,
	{"archive", "foreign"}:       causeHomeForeign,
	{"home-root", "unsafe"}: {"has a /home that is not a root-owned directory closed to other users",
		"make /home a root-owned directory that group and others cannot write" + repeat},
	{"rule-root", "unsafe"}: {"has an /etc/sudoers.d that is not a root-owned directory closed to other users",
		"make /etc/sudoers.d a root-owned directory that group and others cannot write" + repeat},
	{"archive", "conflict"}: {"holds an archived home of {managed} under /home/.bootwright-retired that conflicts with its current home",
		"inspect /home/.bootwright-retired and remove the stale copy" + repeat},
	{"archive", "exposed"}: {"has a /home/.bootwright-retired that other users can open",
		"make /home/.bootwright-retired accessible to root only" + repeat},
	{"archive", "unsafe"}: {"has a /home or /home/.bootwright-retired that is not a root-owned directory closed to other users",
		"make both root-owned directories that group and others cannot write" + repeat},
	{"removal", "unproved"}: causeUnproved,
}

var stepActions = map[string]string{
	"home-root":     "open /home",
	"rule-root":     "open /etc/sudoers.d",
	"lock":          "lock /etc/sudoers.d",
	"account":       "read the account {managed}",
	"rule":          "read /etc/sudoers.d/90-bootwright-{managed}",
	"keys":          "update the authorized key of {managed}",
	"home":          "create the home of {managed} under /home",
	"ssh-directory": "create the .ssh directory of {managed}",
	"labels":        "restore the SELinux labels of the home of {managed} with restorecon",
	"useradd":       "create the account {managed} with useradd",
	"rule-publish":  "write /etc/sudoers.d/90-bootwright-{managed}",
	"archive":       "archive the home of {managed} under /home/.bootwright-retired",
	"userdel":       "remove the account {managed} with userdel",
	"removal":       "prove the account {managed} removed",
}

var stepTools = map[string]string{"useradd": "useradd", "userdel": "userdel", "labels": "restorecon"}

var errnoWords = map[string]string{
	"EACCES":       "permission denied",
	"EPERM":        "operation not permitted",
	"EROFS":        "read-only file system",
	"EOPNOTSUPP":   "the file system does not support it",
	"ENOTSUP":      "the file system does not support it",
	"ENOSPC":       "no space left on the device",
	"EDQUOT":       "disk quota exceeded",
	"ELOOP":        "a symbolic link stands where a directory or file is required",
	"ENOENT":       "a required file or directory is missing",
	"EEXIST":       "a file is already there",
	"EAGAIN":       "another process holds it",
	"ENOTDIR":      "a path component is not a directory",
	"EISDIR":       "a directory stands where a file is required",
	"EIO":          "input/output error",
	"ESTALE":       "the file handle is stale, as on a remounted network file system",
	"EBUSY":        "the resource is busy",
	"ETXTBSY":      "the file is busy",
	"EINVAL":       "an argument was rejected",
	"ENOTEMPTY":    "the directory is not empty",
	"EXDEV":        "the paths are on different file systems",
	"ETIMEDOUT":    "the operation timed out",
	"ENAMETOOLONG": "a file name is too long",
	"EMLINK":       "too many links",
	"ENOMEM":       "the host is out of memory",
	"EUCLEAN":      "the file system needs repair",
	"EMFILE":       "too many open files",
	"ENFILE":       "too many open files on the host",
	"EINTR":        "the operation was interrupted",
}

var exitMeanings = map[string]map[int]string{
	"useradd": {
		1:  "it could not update the password file",
		2:  "it rejected its command syntax",
		3:  "it rejected an argument",
		4:  "the UID is already in use",
		6:  "the group does not exist",
		9:  "the user or group name {managed} is already in use",
		10: "it could not update the group file",
		12: "it could not create the home directory",
		14: "it could not update the SELinux user mapping",
	},
	"userdel": {
		1:  "it could not update the password file",
		2:  "it rejected its command syntax",
		6:  "the user does not exist",
		8:  "the user is logged in",
		10: "it could not update the group file",
		12: "it could not remove the home directory",
	},
}

const (
	remedyHome     = "Bootwright keeps the managed account's home under /home; make /home a local file system where root can create and own files, not an automount or a root-squashing NFS export" + repeat
	remedySudoers  = "make /etc/sudoers.d a root-owned directory on a local file system root can write" + repeat
	remedyDefect   = "report this as a Bootwright defect, with the host's OS release"
	remedyHomeLink = "remove the symbolic link under /home/{managed}-* that stands where Bootwright expects a directory or file" + repeat
	remedyRuleLink = "remove the symbolic link /etc/sudoers.d/90-bootwright-{managed}" + repeat
)

var stepRemedies = map[string]string{
	"home":          remedyHome,
	"ssh-directory": remedyHome,
	"keys":          remedyHome,
	"archive":       remedyHome,
	"home-root":     remedyHome,
	"rule-root":     remedySudoers,
	"rule":          remedySudoers,
	"rule-publish":  remedySudoers,
	"lock":          remedySudoers,
	"useradd":       "read the useradd entries in the host's journal (journalctl -t useradd)" + repeat,
	"userdel":       "read the userdel entries in the host's journal (journalctl -t userdel)" + repeat,
	"labels":        "resolve why restorecon fails for /home on the host" + repeat,
	"account":       "check the host's name service" + repeat,
	"removal":       causeUnproved.remedy,
}

var reasonRemedies = map[refusalKey]string{
	{"home-root", "ELOOP"}:     "make /home a real directory, not a symbolic link" + repeat,
	{"home", "ELOOP"}:          remedyHomeLink,
	{"ssh-directory", "ELOOP"}: remedyHomeLink,
	{"keys", "ELOOP"}:          remedyHomeLink,
	{"rule", "ELOOP"}:          remedyRuleLink,
	{"rule-publish", "ELOOP"}:  remedyRuleLink,
	{"lock", "EAGAIN"}:         "another Bootwright enrollment on this host holds the lock; wait for it to finish" + repeat,
	{"useradd", "exit-9"}:      "remove the leftover user or group {managed} once nothing uses it" + repeat,
	{"useradd", "timeout"}:     "useradd waits on the host's name service; check that SSSD or LDAP answers promptly" + repeat,
	{"userdel", "exit-8"}:      "end the sessions of {managed}" + repeat,
}

func (o observation) workerRefusal() error {
	text := o.output.Bytes()
	if o.marker != "" {
		_, text, _ = bytes.Cut(text, []byte(o.marker))
		if _, afterPrompt, prompted := bytes.Cut(text, []byte(sudoChallenge)); prompted {
			text = afterPrompt
		}
	}
	match := workerReport.FindStringSubmatch(lastLine(text))
	if match == nil {
		return errWorkerUnnamed
	}
	return workerCause(match[1], match[2])
}

func lastLine(text []byte) string {
	for _, line := range slices.Backward(bytes.Split(text, []byte{'\n'})) {
		if trimmed := bytes.Trim(line, " \r"); len(trimmed) != 0 {
			return string(trimmed)
		}
	}
	return ""
}

func workerCause(step, reason string) error {
	if step == "request" {
		return errRequestRejected
	}
	action, known := stepActions[step]
	if !known {
		return errWorkerUnnamed
	}
	if reason == "internal" {
		return &cause{"the enrollment program failed unexpectedly while it tried to " + action, remedyDefect}
	}
	key := refusalKey{step, reason}
	if named, found := hostStates[key]; found {
		return named
	}
	words, found := systemWords(step, reason)
	if !found && step == "removal" {
		return causeUnproved
	}
	if !found {
		return errWorkerUnnamed
	}
	remedy, specific := reasonRemedies[key]
	if !specific {
		remedy = stepRemedies[step]
	}
	return &cause{"could not " + action + ": " + words, remedy}
}

func systemWords(step, reason string) (string, bool) {
	if strings.HasPrefix(reason, "E") {
		if words, known := errnoWords[reason]; known {
			return words + " (" + reason + ")", true
		}
		return "a system error this build does not name", true
	}
	tool, tooled := stepTools[step]
	if reason == "timeout" && tooled {
		return tool + " did not finish within 15 seconds", true
	}
	match := statusReason.FindStringSubmatch(reason)
	if match == nil || !tooled {
		return "", false
	}
	number, err := strconv.Atoi(match[2])
	if err != nil {
		return "", false
	}
	if match[1] == "signal" {
		return tool + " was ended by signal " + strconv.Itoa(number), true
	}
	words := tool + " exited " + strconv.Itoa(number)
	if meaning, known := exitMeanings[tool][number]; known {
		words += " (" + meaning + ")"
	}
	return words, true
}
