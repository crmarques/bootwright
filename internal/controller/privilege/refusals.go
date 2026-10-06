package privilege

import "strings"

const (
	authenticateRemedy = "authenticate to sudo, or run Bootwright as root"
	environmentRemedy  = "add the SETENV tag to the sudoers rule that runs Bootwright, or run Bootwright as root"
	passwordRemedy     = "run sudo -v in this terminal, then repeat the command; or run Bootwright as root"
	policyRemedy       = "the sudo policy must permit Bootwright's re-execution through /proc/<pid>/exe: grant ALL or a /proc/[0-9]*/exe rule " +
		"(a rule naming the Bootwright binary does not match it); ask the policy's administrator, or run Bootwright as root"
	executionRemedy = "root cannot execute the Bootwright executable where it is (a network home with root squash?); " +
		"copy it to a local directory such as /usr/local/bin and run it from there"
)

// policyRefusals are the phrases of the sudoers policy's own denials
// (log_denial in plugins/sudoers/logging.c), which no credential overcomes.
var policyRefusals = []string{
	"is not allowed to execute", "is not in the sudoers file", "may not run sudo on", "is not allowed to run sudo on",
}

// executionRefusal opens the line sudo writes when it cannot execute the
// command it authorized (policy_close in src/sudo.c).
const executionRefusal = "unable to execute "

// refusalRemedy names the fix of the first held reason that has one, so a
// warning sudo printed first never hides the refusal behind it.
func refusalRemedy(reasons []string) string {
	for _, reason := range reasons {
		switch {
		case strings.HasPrefix(reason, environmentRefusal):
			return environmentRemedy
		case strings.Contains(reason, "a password is required"):
			return passwordRemedy
		case containsAny(reason, policyRefusals):
			return policyRemedy
		case strings.HasPrefix(reason, executionRefusal):
			return executionRemedy
		}
	}
	return authenticateRemedy
}

func containsAny(text string, phrases []string) bool {
	for _, phrase := range phrases {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}
