package privilege

import "testing"

func listing(defaults, specific, options string) []byte {
	report := "Matching Defaults entries for operator on host:\n    " + defaults + "\n\n"
	if specific != "" {
		report += "Runas and Command-specific defaults for operator:\n    " + specific + "\n\n"
	}
	report += "User operator may run the following commands on host:\n\nSudoers entry: /etc/sudoers\n    RunAsUsers: ALL\n"
	if options != "" {
		report += "    Options: " + options + "\n"
	}
	return []byte(report + "    Commands:\n\tALL\n")
}

func TestInputLoggingIsFoundWhereverSudoListsIt(t *testing.T) {
	for _, test := range []struct {
		name   string
		report []byte
		want   string
	}{
		{"matching defaults", listing("!visiblepw, env_reset,\n    log_input, secure_path=/usr/sbin\\:/usr/bin", "", ""), "log_input"},
		{"matching defaults stdin", listing("env_reset, log_stdin", "", ""), "log_stdin"},
		{"negated", listing("env_reset, !log_input", "", ""), ""},
		{"output only", listing("env_reset, log_output", "", "!authenticate, log_output"), ""},
		{"quoted value", listing("env_reset, secure_path=\"/x/log_input\"", "", ""), ""},
		{"rule options", listing("env_reset", "", "!authenticate, log_input"), "log_input"},
		{"command-specific defaults", listing("env_reset", "Defaults!/usr/bin/x log_stdin", ""), "log_stdin"},
		{"runas binding with several members", listing("env_reset", "Defaults>root, admin log_input", ""), "log_input"},
		{"command binding with several members", listing("env_reset", "Defaults!/usr/bin/vi, /usr/local/bin/bootwright log_stdin", ""), "log_stdin"},
		{"command member with arguments", listing("env_reset", "Defaults!/usr/local/bin/bootwright secret set log_stdin", ""), "log_stdin"},
		{"several members, one with arguments", listing("env_reset", "Defaults!/usr/bin/vi /etc/x, /usr/local/bin/bootwright log_stdin", ""), "log_stdin"},
		{"later option after a member with arguments", listing("env_reset", "Defaults!/usr/bin/vi /etc/x !lecture, log_input", ""), "log_input"},
		{"negated after a member with arguments", listing("env_reset", "Defaults!/usr/local/bin/bootwright secret set !log_stdin, !lecture", ""), ""},
		{"quoted value with a blank", listing("env_reset", "Defaults>root env_keep=\"A log_input\"", ""), ""},
		{"negated in a several-member binding", listing("env_reset", "Defaults>root, admin !log_input", ""), ""},
		{"unbalanced list", listing("env_reset, secure_path=\"/x, log_input", "", ""), ""},
		{"empty", nil, ""},
	} {
		if got := inputLogging(test.report); got != test.want {
			t.Errorf("%s: inputLogging = %q, want %q\n%s", test.name, got, test.want, test.report)
		}
	}
}
