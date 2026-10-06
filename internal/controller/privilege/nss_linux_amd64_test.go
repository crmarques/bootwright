//go:build linux && amd64

package privilege

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

const carol = "carol:*:1152:1022:Carol:/home/carol:/bin/bash"

// fakeGetent writes a getent whose body answers each query "<database> <key>"
// with its shell commands and exits 2 for any other, as getent does for a
// missing key; the key is its third argument, after "--".
func fakeGetent(t *testing.T, answers map[string]string) string {
	t.Helper()
	body := "case \"$1 $3\" in\n"
	for query, answer := range answers {
		body += "'" + query + "') " + answer + " ;;\n"
	}
	return writeGetent(t, body+"*) exit 2 ;;\nesac")
}

func writeGetent(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "getent")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

// printed is the shell command that prints line as getent's one answer.
func printed(line string) string { return "printf '%s\\n' '" + line + "'" }

// initgroups is getent's initgroups line: the name padded to 21 columns, then
// each GID after a space (glibc nss/getent.c).
func initgroups(name string, gids ...string) string {
	line := fmt.Sprintf("%-21s", name)
	for _, gid := range gids {
		line += " " + gid
	}
	return printed(line)
}

func admitted(path string) (string, error) { return path, nil }

func localFiles() (accountDirectory, error) {
	return accountFiles{passwd: []byte("root:x:0:0::/files-root:/bin/sh\n"), groups: []byte("root:x:0:\n")}, nil
}

func carolsAnswers() map[string]string {
	return map[string]string{
		"passwd 1152":      printed(carol),
		"passwd carol":     printed(carol),
		"initgroups carol": initgroups("carol", "1000", "10814"),
	}
}

// An SSSD, LDAP or AD account exists only in the name service, so the account
// files never hold it; getent resolves it with its supplementary groups.
func TestADirectoryAccountResolvesThroughTheNameService(t *testing.T) {
	directory, err := accountDirectoryAt(fakeGetent(t, carolsAnswers()), admitted, localFiles)
	if err != nil {
		t.Fatal(err)
	}
	account, err := accountFrom(context.Background(), directory, accountRequest{uid: 1152, euid: 1152})
	if err != nil {
		t.Fatalf("a directory account was refused: %v", err)
	}
	want := Account{UID: 1152, GID: 1022, Name: "carol", Home: "/home/carol", Groups: []uint32{1000, 1022, 10814}}
	if !reflect.DeepEqual(account, want) {
		t.Fatalf("account = %+v, want %+v", account, want)
	}
}

func TestNameServiceAnswersThatAreAmbiguousOrInconsistentRefuse(t *testing.T) {
	many := []string{}
	for gid := 2000; gid < 3100; gid++ {
		many = append(many, fmt.Sprint(gid))
	}
	for _, test := range []struct {
		name    string
		answers map[string]string
		script  string
		reason  string
	}{
		{name: "two passwd lines", answers: map[string]string{"passwd 1152": printed(carol) + "; " + printed(carol)}, reason: "the account database answered ambiguously for UID 1152"},
		{name: "an entry for another UID", answers: map[string]string{"passwd 1152": printed("carol:*:1153:1022:Carol:/home/carol:/bin/bash")}, reason: "the account database answered ambiguously for UID 1152"},
		{name: "the name with another UID", answers: map[string]string{"passwd carol": printed("carol:*:1153:1022:Carol:/home/carol:/bin/bash")}, reason: "the account database answered inconsistently for carol"},
		{name: "the name with another GID", answers: map[string]string{"passwd carol": printed("carol:*:1152:1023:Carol:/home/carol:/bin/bash")}, reason: "the account database answered inconsistently for carol"},
		{name: "the name with another home", answers: map[string]string{"passwd carol": printed("carol:*:1152:1022:Carol:/srv/carol:/bin/bash")}, reason: "the account database answered inconsistently for carol"},
		{name: "the name without an entry", answers: map[string]string{"passwd carol": "exit 2"}, reason: "the account database answered inconsistently for carol"},
		{name: "an unclean home", answers: map[string]string{"passwd 1152": printed("carol:*:1152:1022:Carol:/home/../carol:/bin/bash")}, reason: "the account carol has no clean absolute home"},
		{name: "a relative home", answers: map[string]string{"passwd 1152": printed("carol:*:1152:1022:Carol:home/carol:/bin/bash")}, reason: "the account carol has no clean absolute home"},
		{name: "a name that reads as an option", answers: map[string]string{"passwd 1152": printed("-carol:*:1152:1022:Carol:/home/carol:/bin/bash")}, reason: "the account database answered ambiguously for UID 1152"},
		{name: "a name with a terminal escape", answers: map[string]string{"passwd 1152": printed("car\x1b]0;x\x07ol:*:1152:1022:Carol:/home/carol:/bin/bash")}, reason: "the account database answered ambiguously for UID 1152"},
		{name: "a name that is not UTF-8", answers: map[string]string{"passwd 1152": printed("car\x9bol:*:1152:1022:Carol:/home/carol:/bin/bash")}, reason: "the account database answered ambiguously for UID 1152"},
		{name: "a name over 256 bytes", answers: map[string]string{"passwd 1152": printed(strings.Repeat("c", 257) + ":*:1152:1022:Carol:/home/carol:/bin/bash")}, reason: "the account database answered ambiguously for UID 1152"},
		{name: "a field too many", answers: map[string]string{"passwd 1152": printed("carol:*:1152:1022:Carol:x:/home/carol:/bin/bash")}, reason: "the account database answered ambiguously for UID 1152"},
		{name: "a non-decimal primary GID", answers: map[string]string{"passwd 1152": printed("carol:*:1152:+1022:Carol:/home/carol:/bin/bash")}, reason: "the account database answered ambiguously for UID 1152"},
		{name: "groups of another name", answers: map[string]string{"initgroups carol": initgroups("dave", "1000")}, reason: "the account database answered inconsistently for carol"},
		{name: "groups of a name it begins", answers: map[string]string{"initgroups carol": printed("carol1 1000")}, reason: "the account database answered inconsistently for carol"},
		{name: "a non-decimal supplementary GID", answers: map[string]string{"initgroups carol": initgroups("carol", "1000", "010814")}, reason: "the account database answered inconsistently for carol"},
		{name: "a supplementary GID that is not a number", answers: map[string]string{"initgroups carol": initgroups("carol", "1000", "wheel")}, reason: "the account database answered inconsistently for carol"},
		{name: "more than 1024 groups", answers: map[string]string{"initgroups carol": initgroups("carol", many...)}, reason: "the account carol is in more than 1024 groups"},
		{name: "no entry", script: "exit 2", reason: "the account database has no entry for UID 1152"},
		{name: "a failed lookup", script: "exit 1", reason: "the account database did not answer"},
		{name: "a lookup past its bound", script: "exec sleep 5", reason: "the account database did not answer"},
		{name: "an answer over 64 KiB", script: "i=0; while [ $i -lt 1100 ]; do printf '%064d' 0; i=$((i+1)); done; echo", reason: "the account database did not answer"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := ""
			if test.script != "" {
				path = writeGetent(t, test.script)
			} else {
				answers := carolsAnswers()
				for query, answer := range test.answers {
					answers[query] = answer
				}
				path = fakeGetent(t, answers)
			}
			started := time.Now()
			account, err := accountFrom(context.Background(), nameService{executable: path, bound: 300 * time.Millisecond}, accountRequest{uid: 1152, euid: 1152})
			var refusal accountRefusal
			if !errors.As(err, &refusal) || !errors.Is(err, errAccount) || refusal.reason != test.reason {
				t.Fatalf("account %+v, error %v; want the refusal %q", account, err, test.reason)
			}
			if elapsed := time.Since(started); elapsed > 4*time.Second {
				t.Fatalf("the refusal took %v", elapsed)
			}
		})
	}
}

// A name of 256 bytes is the longest the account database may answer.
func TestANameOfTheLongestAdmittedLengthResolves(t *testing.T) {
	name := strings.Repeat("c", 256)
	entry := name + ":*:1152:1022:Carol:/home/carol:/bin/bash"
	answers := map[string]string{"passwd 1152": printed(entry), "passwd " + name: printed(entry), "initgroups " + name: initgroups(name, "1000")}
	account, err := accountFrom(context.Background(), nameService{executable: fakeGetent(t, answers), bound: 5 * time.Second}, accountRequest{uid: 1152, euid: 1152})
	if err != nil || account.Name != name {
		t.Fatalf("a 256-byte name resolved %+v, %v", account, err)
	}
}

// getent runs with nothing of the invoking environment, from the root
// directory, and with its key after "--", so neither a locale, a preload nor a
// key that looks like an option changes what it answers.
func TestGetentRunsWithAFixedEnvironmentAndTerminatedOptions(t *testing.T) {
	t.Setenv("BOOTWRIGHT_GETENT_CANARY", "leaked")
	t.Setenv("LC_ALL", "de_DE.UTF-8")
	body := `[ "$2" = "--" ] || exit 3
[ "$(/usr/bin/tr '\0' ' ' < /proc/$$/environ)" = "LANG=C LC_ALL=C " ] || exit 3
[ "$PWD" = / ] || exit 3
case "$1 $3" in
'passwd 1152'|'passwd carol') ` + printed(carol) + ` ;;
'initgroups carol') ` + initgroups("carol", "1000") + ` ;;
*) exit 2 ;;
esac`
	account, err := accountFrom(context.Background(), nameService{executable: writeGetent(t, body), bound: 5 * time.Second}, accountRequest{uid: 1152, euid: 1152})
	if err != nil || account.UID != 1152 || !reflect.DeepEqual(account.Groups, []uint32{1000, 1022}) {
		t.Fatalf("account %+v, error %v", account, err)
	}
}

// Only a system without getent reads the account files: a getent that exists
// but is not root's own executable could answer anything, so it refuses
// rather than fall back.
func TestAnAbsentGetentFallsBackAndAnUnqualifiedOneRefuses(t *testing.T) {
	directory, err := accountDirectoryAt(filepath.Join(t.TempDir(), "getent"), qualifiedSystemExecutable, localFiles)
	if err != nil {
		t.Fatalf("an absent getent refused: %v", err)
	}
	if account, err := accountFrom(context.Background(), directory, accountRequest{}); err != nil || account.Home != "/files-root" {
		t.Fatalf("an absent getent resolved %+v, %v; want the account files", account, err)
	}
	for name, path := range map[string]string{
		"a getent the invoking account owns": fakeGetent(t, carolsAnswers()),
		"a getent that is a dangling link":   danglingLink(t),
	} {
		t.Run(name, func(t *testing.T) {
			files := func() (accountDirectory, error) {
				t.Error("an unqualified getent fell back to the account files")
				return localFiles()
			}
			_, err := accountDirectoryAt(path, qualifiedSystemExecutable, files)
			var refusal accountRefusal
			if !errors.As(err, &refusal) || refusal.reason != "the system getent is not a root-owned system executable" {
				t.Fatalf("an unqualified getent = %v", err)
			}
		})
	}
}

func danglingLink(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "getent")
	if err := os.Symlink(filepath.Join(t.TempDir(), "absent"), path); err != nil {
		t.Fatal(err)
	}
	return path
}
