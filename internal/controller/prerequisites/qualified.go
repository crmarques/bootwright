package prerequisites

import (
	"slices"
	"strings"
)

// QualifiedAnsibleMinor is the one ansible-core minor this build's automation
// is qualified against. Setup resolves its latest stable patch and never a
// newer minor; the collection's requires_ansible, the check lock and the
// development guide state the same minor, and qualifying another one moves
// every statement together.
const QualifiedAnsibleMinor = "2.21"

// QualifiedControllerPythons are the CPython minors the qualified ansible-core
// minor supports as a controller: ansible-core 2.21's
// CONTROLLER_PYTHON_VERSIONS in
// https://github.com/ansible/ansible/blob/stable-2.21/test/lib/ansible_test/_util/target/common/constants.py
// Setup resolves the latest patch of the newest of them.
func QualifiedControllerPythons() []string {
	return []string{"3.12", "3.13", "3.14"}
}

// QualifiedControllerPython reports a stable CPython release whose minor the
// qualified ansible-core minor supports as a controller.
func QualifiedControllerPython(version string) bool {
	if len(version) > 80 || !bootstrapVersion.MatchString(version) {
		return false
	}
	return slices.Contains(QualifiedControllerPythons(), version[:strings.LastIndex(version, ".")])
}

// ValidateQualifiedAnsibleVersion admits only a stable release of the
// qualified minor. It selects and supersedes; it never judges a record, so a
// resolution an earlier build recorded stays readable.
func ValidateQualifiedAnsibleVersion(version string) error {
	if len(version) <= 80 && bootstrapVersion.MatchString(version) && strings.HasPrefix(version, QualifiedAnsibleMinor+".") {
		return nil
	}
	return failure("controller.unsupported", "controller automation is qualified for stable ansible-core "+QualifiedAnsibleMinor+" only", "use a Bootwright build that qualifies this ansible-core minor")
}
