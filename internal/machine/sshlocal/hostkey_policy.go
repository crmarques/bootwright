package sshlocal

import (
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/crmarques/bootwright/internal/machine"
)

const maxEvaluatedPolicyBytes = 256 << 10

var algorithmList = regexp.MustCompile(`^[A-Za-z0-9@.,_-]+$`)

var (
	errNoAlgorithmList          = errors.New("reported no host-key algorithm list")
	errUnsupportedAlgorithmList = errors.New("reported an unsupported host-key algorithm list")
)

func evaluatedHostKeyAlgorithms(output []byte) ([]string, error) {
	for _, line := range strings.Split(string(output), "\n") {
		name, value := line, ""
		if index := strings.IndexAny(line, " \t"); index >= 0 {
			name, value = line[:index], strings.TrimLeft(line[index:], " \t")
		}
		if name != "hostkeyalgorithms" {
			continue
		}
		if !algorithmList.MatchString(value) {
			return nil, errUnsupportedAlgorithmList
		}
		members := strings.Split(value, ",")
		if slices.Contains(members, "") {
			return nil, errUnsupportedAlgorithmList
		}
		return members, nil
	}
	return nil, errNoAlgorithmList
}

func admittedAlgorithms(session machine.Session, admitted []string, policyPath string) (string, error) {
	var kept []string
	for _, candidate := range strings.Split(session.HostKey.Algorithms(), ",") {
		if slices.Contains(admitted, candidate) {
			kept = append(kept, candidate)
		}
	}
	if len(kept) == 0 {
		return "", excludedHostKey(session, policyPath)
	}
	return strings.Join(kept, ","), nil
}

func excludedHostKey(session machine.Session, policyPath string) error {
	return failure("the "+session.HostKey.Type+" host key Machine/"+session.Machine+
		" presents is excluded by this host's SSH crypto policy ("+policyPath+"); no session was opened",
		"connect from a host whose SSH crypto policy admits "+session.HostKey.Type+
			", or give Machine/"+session.Machine+" a host key of a type that policy admits")
}

func unevaluatedPolicy(session machine.Session, policyPath, reason string) error {
	return failure("this host's SSH crypto policy ("+policyPath+") cannot be evaluated: "+reason+
		"; no session to Machine/"+session.Machine+" was opened",
		"regenerate "+policyPath+" with update-crypto-policies --set <policy> "+
			"(update-crypto-policies --show names the current one), or report the platform that ships it")
}
