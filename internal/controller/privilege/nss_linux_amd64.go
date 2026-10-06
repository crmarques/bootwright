//go:build linux && amd64

package privilege

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// systemGetent is the one name-service client Resolve runs, pinned so PATH
// never selects it. Through NSS it reaches SSSD, LDAP and AD accounts as well
// as local ones.
const systemGetent = "/usr/bin/getent"

const nameServiceBound = 10 * time.Second

// accountDirectoryAt selects the name service through the getent at path once
// qualify proves it, or files only where nothing exists at path: a getent that
// exists but does not qualify refuses rather than fall back.
func accountDirectoryAt(path string, qualify func(string) (string, error), files func() (accountDirectory, error)) (accountDirectory, error) {
	if _, err := os.Lstat(path); errors.Is(err, syscall.ENOENT) {
		return files()
	}
	executable, err := qualify(path)
	if err != nil {
		return nil, accountRefusal{"the system getent is not a root-owned system executable"}
	}
	return nameService{executable: executable, bound: nameServiceBound}, nil
}

// nameService answers through glibc's getent (nss/getent.c): passwd prints
// one passwd(5) line for a key, looked up by UID when the key is decimal, and
// exits 2 when there is none; initgroups prints the key padded to 21 columns
// and then the supplementary GIDs without the primary one, and exits 0 even
// for an unknown name, so only passwd proves that an account exists.
type nameService struct {
	executable string
	bound      time.Duration
}

func (s nameService) byUID(ctx context.Context, uid int) (passwdEntry, error) {
	return s.passwd(ctx, strconv.Itoa(uid))
}

func (s nameService) byName(ctx context.Context, name string) (passwdEntry, error) {
	return s.passwd(ctx, name)
}

func (s nameService) passwd(ctx context.Context, key string) (passwdEntry, error) {
	answer, err := s.lookup(ctx, "passwd", key)
	if err != nil {
		return passwdEntry{}, err
	}
	line, single := singleLine(answer)
	fields := strings.Split(line, ":")
	if !single || len(fields) != 7 {
		return passwdEntry{}, errMalformed
	}
	return passwdFields(fields)
}

func (s nameService) groupsOf(ctx context.Context, name string) ([]uint32, error) {
	answer, err := s.lookup(ctx, "initgroups", name)
	if err != nil {
		return nil, err
	}
	line, single := singleLine(answer)
	gids, named := strings.CutPrefix(line, name)
	if !single || !named || gids != "" && gids[0] != ' ' || strings.Trim(gids, " 0123456789") != "" {
		return nil, errMalformed
	}
	var groups []uint32
	for _, field := range strings.Fields(gids) {
		gid, err := decimalID(field)
		if err != nil {
			return nil, errMalformed
		}
		groups = append(groups, uint32(gid))
	}
	return groups, nil
}

// lookup runs one bounded getent query with a fixed environment, no input and
// discarded diagnostics; "--" keeps a key from being read as an option.
func (s nameService) lookup(ctx context.Context, database, key string) ([]byte, error) {
	bounded, cancel := context.WithTimeout(ctx, s.bound)
	defer cancel()
	command := exec.CommandContext(bounded, s.executable, database, "--", key)
	command.Env = []string{"LANG=C", "LC_ALL=C"}
	command.Dir = "/"
	var answer limitedOutput
	command.Stdout = &answer
	command.WaitDelay = time.Second
	err := command.Run()
	var exit *exec.ExitError
	switch {
	case answer.overflow:
	case err == nil:
		return answer.data, nil
	case errors.As(err, &exit) && exit.ExitCode() == 2:
		return nil, errNoEntry
	}
	return nil, errors.New("the name service did not answer")
}

func singleLine(answer []byte) (string, bool) {
	line, terminated := strings.CutSuffix(string(answer), "\n")
	return line, terminated && !strings.Contains(line, "\n")
}
