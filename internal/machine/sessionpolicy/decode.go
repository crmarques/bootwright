package sessionpolicy

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"go.yaml.in/yaml/v3"
)

const MaxBytes = 64 << 10

var objectName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func Decode(data []byte, contextName string) (api.SessionAccessPolicy, error) {
	var policy api.SessionAccessPolicy
	if len(data) > MaxBytes || !utf8.Valid(data) {
		return policy, errors.New("policy must be UTF-8 and at most 64 KiB")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	var node, extra yaml.Node
	if decoder.Decode(&node) != nil || decoder.Decode(&extra) != io.EOF || !plainTree(&node) {
		return policy, errors.New("policy requires one YAML document with string scalars and no aliases, anchors or nulls")
	}
	decoder = yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&policy); err != nil {
		return policy, errors.New("policy contains a duplicate field, unknown field or invalid field shape")
	}
	if policy.APIVersion != api.APIVersion || policy.Kind != "SessionAccessPolicy" || policy.Metadata.Name != contextName || !validName(contextName) {
		return policy, errors.New("policy must be a bootwright.io/v1alpha1 SessionAccessPolicy named for the selected context")
	}
	if policy.Spec.Grants == nil || len(policy.Spec.Grants) > 128 {
		return policy, errors.New("spec.grants must be an explicit list of at most 128 grants")
	}
	names := make(map[string]bool)
	subjects := make(map[api.PolicySubject]bool)
	for _, grant := range policy.Spec.Grants {
		if !validName(grant.Name) || names[grant.Name] {
			return policy, errors.New("grants require distinct DNS-label names")
		}
		names[grant.Name] = true
		if err := validateGrant(grant); err != nil {
			return policy, fmt.Errorf("grant %s %w", grant.Name, err)
		}
		for _, subject := range grant.Subjects {
			subjects[subject] = true
		}
	}
	if len(subjects) > 64 {
		return policy, errors.New("policy may contain at most 64 distinct subjects")
	}
	return policy, nil
}

func plainTree(root *yaml.Node) bool {
	stack := []*yaml.Node{root}
	for len(stack) > 0 {
		node := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if node.Anchor != "" || node.Kind == yaml.AliasNode {
			return false
		}
		switch node.Kind {
		case yaml.ScalarNode:
			if node.Tag != "!!str" {
				return false
			}
		case yaml.MappingNode:
			if node.Tag != "!!map" {
				return false
			}
		case yaml.SequenceNode:
			if node.Tag != "!!seq" {
				return false
			}
		case yaml.DocumentNode:
		default:
			return false
		}
		stack = append(stack, node.Content...)
	}
	return true
}

func validateGrant(grant api.SessionGrant) error {
	if len(grant.Subjects) == 0 || len(grant.Targets.MachineRefs)+len(grant.Targets.ClusterRefs) == 0 ||
		!api.ValidLexical(api.LexicalPOSIXUser, grant.RemoteUser) || (grant.Exec == nil) == (grant.Rsh == nil) {
		return errors.New("requires subjects, targets, a remoteUser and exactly one of exec or rsh")
	}
	for _, subject := range grant.Subjects {
		if (subject.User == "") == (subject.Group == "") || !validPrincipalName(subject.User+subject.Group) {
			return errors.New("requires each subject to have one valid user or group name")
		}
	}
	for _, refs := range [][]string{grant.Targets.MachineRefs, grant.Targets.ClusterRefs} {
		for _, name := range refs {
			if !validName(name) {
				return errors.New("requires DNS-label target names")
			}
		}
	}
	if grant.Exec == nil {
		return nil
	}
	if len(grant.Exec.Commands) == 0 {
		return errors.New("requires at least one exec command")
	}
	for _, argv := range grant.Exec.Commands {
		if len(argv) == 0 || !path.IsAbs(argv[0]) || path.Clean(argv[0]) != argv[0] || argv[0] == "/" {
			return errors.New("requires an absolute executable path in every command vector")
		}
		for _, arg := range argv {
			if strings.ContainsFunc(arg, unicode.IsControl) {
				return errors.New("requires command words without control characters")
			}
		}
	}
	return nil
}

func validName(name string) bool { return len(name) <= 63 && objectName.MatchString(name) }

func validPrincipalName(name string) bool {
	return len(name) <= 256 && name == strings.TrimSpace(name) && !strings.HasPrefix(name, "-") &&
		strings.Trim(name, "0123456789") != "" && !strings.ContainsRune(name, ':') &&
		!strings.ContainsFunc(name, unicode.IsControl)
}
