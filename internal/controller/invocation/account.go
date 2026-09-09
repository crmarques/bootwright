// Package invocation implements the local account and sudo process boundaries.
package invocation

import (
	"errors"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type Account struct {
	UID, GID      int
	Home, Name    string
	Groups        []uint32
	SudoParentPID int
}

// Resolver acquires local account data only when Resolve is called.
type Resolver struct{}

type accountRequest struct {
	uid, euid                  int
	sudoUID, sudoGID, sudoUser string
	trustedParent              bool
}

var errAccount = errors.New("invoking account identity cannot be verified")

func accountFrom(passwd, groups []byte, request accountRequest) (Account, error) {
	uid := request.uid
	manual := request.sudoUID != "" || request.sudoGID != "" || request.sudoUser != ""
	if request.uid != request.euid {
		return Account{}, errAccount
	}
	if request.euid == 0 && manual {
		var err error
		uid, err = decimalID(request.sudoUID)
		if err != nil || request.sudoGID == "" || request.sudoUser == "" || !request.trustedParent {
			return Account{}, errAccount
		}
	}
	var result Account
	found := false
	for _, line := range strings.Split(string(passwd), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) != 7 || fields[2] != strconv.Itoa(uid) {
			continue
		}
		gid, err := decimalID(fields[3])
		if found || err != nil || fields[0] == "" || !filepath.IsAbs(fields[5]) || filepath.Clean(fields[5]) != fields[5] || len(fields[5]) > 4096 || strings.ContainsRune(fields[5], 0) {
			return Account{}, errAccount
		}
		result = Account{UID: uid, GID: gid, Name: fields[0], Home: fields[5]}
		found = true
	}
	if !found {
		return Account{}, errAccount
	}
	for _, line := range strings.Split(string(passwd), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) == 7 && fields[0] == result.Name && fields[2] != strconv.Itoa(result.UID) {
			return Account{}, errAccount
		}
	}
	if request.euid == 0 && manual && (request.sudoUser != result.Name || request.sudoGID != strconv.Itoa(result.GID)) {
		return Account{}, errAccount
	}
	set := map[uint32]bool{uint32(result.GID): true}
	for _, line := range strings.Split(string(groups), "\n") {
		fields := strings.Split(line, ":")
		if len(fields) != 4 {
			continue
		}
		for _, name := range strings.Split(fields[3], ",") {
			if name == result.Name {
				gid, err := decimalID(fields[2])
				if err != nil {
					return Account{}, errAccount
				}
				set[uint32(gid)] = true
			}
		}
	}
	if len(set) > 1024 {
		return Account{}, errAccount
	}
	for gid := range set {
		result.Groups = append(result.Groups, gid)
	}
	sort.Slice(result.Groups, func(i, j int) bool { return result.Groups[i] < result.Groups[j] })
	return result, nil
}

func decimalID(value string) (int, error) {
	n, err := strconv.ParseUint(value, 10, 32)
	if err != nil || strconv.FormatUint(n, 10) != value {
		return 0, errAccount
	}
	return int(n), nil
}
