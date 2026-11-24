package account

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"os/exec"
	"testing"
)

const ownershipHarness = `
import io, json, os, pwd, sys
ns = {"__name__": "worker"}
exec(sys.argv[1], ns)
work = sys.argv[2]
real_floor = ns["regular_floor"]
real_directory = ns["directory"]
me = os.getuid()
mark = "bootwright-" + "a" * 64
other = "bootwright-" + "b" * 64
home = "/home/svc-" + "a" * 16
rule = b"Defaults:svc !requiretty\nsvc ALL=(root) NOPASSWD: ALL\n"
out = {}

def entry(gecos, directory, uid):
    return pwd.struct_passwd(("svc", "x", uid, uid, gecos, directory, "/bin/bash"))

def refusal(call):
    try:
        call()
        return "ok"
    except ValueError as error:
        return str(error)

def verdict(gecos, directory, uid):
    ns["regular_floor"] = lambda: 1000
    pwd.getpwnam = lambda name: entry(gecos, directory, uid)
    return refusal(lambda: ns["login"]("svc", mark, home))

def floor(lines):
    def opener(path, mode):
        if lines is None:
            raise FileNotFoundError(path)
        return io.BytesIO(lines)
    ns["regular_floor"] = real_floor
    ns["open"] = opener
    try:
        return real_floor()
    finally:
        del ns["open"]

def place(name, data=b"x", mode=0o644):
    path = os.path.join(work, name)
    with open(path, "wb") as stream:
        stream.write(data)
    os.chmod(path, mode)
    return path

def read(name, owner=(me,)):
    parent = os.open(work, os.O_RDONLY | os.O_DIRECTORY)
    try:
        return refusal(lambda: ns["content"](name, parent, owner))
    finally:
        os.close(parent)

def directory(name, mode, uid):
    path = os.path.join(work, name)
    os.mkdir(path)
    os.chmod(path, mode)
    parent = os.open(work, os.O_RDONLY | os.O_DIRECTORY)
    try:
        return refusal(lambda: os.close(ns["owned_directory"]("destroy", name, entry(mark, home, uid), parent)))
    finally:
        os.close(parent)

def retired(mode):
    path = os.path.join(work, ".bootwright-retired")
    os.mkdir(path)
    os.chmod(path, mode)
    ns["directory"] = lambda name, parent=None, owner=0: real_directory(name, parent=parent, owner=me)
    parent = os.open(work, os.O_RDONLY | os.O_DIRECTORY)
    try:
        return refusal(lambda: os.close(ns["retired_directory"](parent)))
    finally:
        os.close(parent)
        ns["directory"] = real_directory
        os.rmdir(path)

def destroy(account_survives=False, grant_survives=False, home_survives=False):
    calls = []
    state = {"present": True, "rule": True}
    def login(user, given, directory):
        return entry(mark, home, 1001) if state["present"] else None
    def content(name, parent, owner):
        return rule if state["rule"] else None
    def unlink(name, dir_fd=None):
        calls.append("unlink " + name)
        state["rule"] = grant_survives
    def execute(*args):
        calls.append(os.path.basename(args[0]))
        state["present"] = account_survives
    ns["login"] = login
    ns["content"] = content
    ns["grants"] = lambda mode, found, keys, directory: calls.append("keys " + mode) or True
    ns["quarantine_home"] = lambda directory, found: calls.append("archive")
    ns["execute"] = execute
    os.unlink = unlink
    os.fsync = lambda fd: None
    os.path.lexists = lambda path: home_survives
    result = refusal(lambda: calls.append(ns["reconcile"]("destroy", "svc", mark, b"key\n", None)))
    if result != "ok":
        calls.append(result)
    return ",".join(calls)

out["mark"] = verdict(mark, home, 1001)
out["other"] = verdict(other, home, 1001)
out["foreign"] = verdict("Service account", home, 1001)
out["home"] = verdict(mark, "/home/svc", 1001)
out["system"] = verdict(mark, home, 999)
out["floors"] = ",".join(str(floor(lines)) for lines in (b"UID_MIN 500\n", b"# none\n", None))
place("regular")
out["regular"] = read("regular")
out["absent"] = str(ns["content"]("missing", os.open(work, os.O_RDONLY), (me,)))
os.mkfifo(os.path.join(work, "fifo"))
out["fifo"] = read("fifo")
place("linked")
os.link(os.path.join(work, "linked"), os.path.join(work, "second"))
out["links"] = read("linked")
place("writable", mode=0o662)
out["writable"] = read("writable")
place("large", data=b"x" * 32769)
out["large"] = read("large")
out["limit"] = read("regular", owner=(me + 1,))
out["stranger"] = directory("stranger", 0o700, me + 1)
out["open directory"] = directory("open", 0o777, me)
out["exposed"] = retired(0o750)
out["private"] = retired(0o700)
out["removed"] = destroy()
out["account kept"] = destroy(account_survives=True)
out["grant kept"] = destroy(grant_survives=True)
out["home kept"] = destroy(home_survives=True)
print(json.dumps(out))
`

func TestTheEnrollmentProgramRemovesOnlyALoginItOwnsInOrder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the harness builds files this account owns, which root cannot tell from a foreign owner")
	}
	command := exec.CommandContext(t.Context(), "/usr/bin/python3", "-I", "-c", ownershipHarness, worker, t.TempDir())
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("harness failed: %v\n%s", err, stderr.String())
	}
	got := map[string]string{}
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("harness reported %q: %v", stdout.String(), err)
	}
	const order = "keys destroy,unlink 90-bootwright-svc,archive,userdel"
	want := map[string]string{
		"mark": "ok", "other": "other-operation", "foreign": "not-bootwright", "home": "home-differs", "system": "system-uid",
		"floors":  "500,1000,1000",
		"regular": "ok", "absent": "None", "fifo": "unsafe", "links": "unsafe", "writable": "unsafe", "large": "oversized", "limit": "unsafe",
		"stranger": "foreign", "open directory": "foreign",
		"exposed": "exposed", "private": "ok",
		"removed":      order + ",absent",
		"account kept": order + ",unproved",
		"grant kept":   order + ",unproved",
		"home kept":    order + ",unproved",
	}
	if !maps.Equal(got, want) {
		for name, expected := range want {
			if got[name] != expected {
				t.Errorf("%s: got %q, want %q", name, got[name], expected)
			}
		}
	}
}
