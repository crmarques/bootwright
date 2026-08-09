package cli

import (
	"errors"
	"strings"
	"testing"
)

func TestMutatingJSONWithoutDryRunWinsBeforeOtherIntentValidation(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
		want string
	}{
		{
			name: "apply",
			run: func() error {
				_, _, err := resolveScopeApplyIntent(outputJSON, false, false, "invalid-mode", []string{"invalid-token"})
				return err
			},
			want: "--output json is supported only with --dry-run for apply",
		},
		{
			name: "destroy",
			run: func() error {
				_, err := resolveScopeDestroyIntent(outputJSON, false, []string{"invalid-token"})
				return err
			},
			want: "--output json is supported only with --dry-run for destroy",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			var exit *exitError
			if !errors.As(err, &exit) || exit.code != 2 || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("intent error = %v, want exit 2 containing %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "invalid-mode") || strings.Contains(err.Error(), "invalid-token") {
				t.Fatalf("later intent validation ran before the JSON/dry-run contract: %v", err)
			}
		})
	}
}

func TestRawMutatingJSONIntentIsRejectedBeforeRootDispatch(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{name: "apply needs dry-run", args: []string{"apply", "--output", "json"}, wantErr: "only with --dry-run for apply"},
		{name: "destroy explicit false needs dry-run", args: []string{"--context", "matrix", "destroy", "--dry-run=false", "--output=json"}, wantErr: "only with --dry-run for destroy"},
		{name: "apply JSON cannot prompt", args: []string{"--ssh-ask-sudo-password", "apply", "--dry-run", "--output=json"}, wantErr: "--ssh-ask-sudo-password cannot be used"},
		{name: "last prompt flag wins", args: []string{"apply", "--ssh-ask-sudo-password=false", "--ssh-ask-sudo-password", "--dry-run", "--output=json"}, wantErr: "--ssh-ask-sudo-password cannot be used"},
		{name: "apply dry-run is valid", args: []string{"apply", "--dry-run=true", "--output", "json"}},
		{name: "plan is already read-only", args: []string{"plan", "--output", "json"}},
		{name: "forwarded flags are payload", args: []string{"cluster", "exec", "--name", "ocp", "--", "apply", "--output", "json", "--ssh-ask-sudo-password"}},
		{name: "invalid bool belongs to cobra", args: []string{"destroy", "--dry-run=maybe", "--output", "json"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := rejectMutatingJSONIntentArgs(tc.args)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("raw intent error = %v, want none", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("raw intent error = %v, want %q", err, tc.wantErr)
			}
		})
	}
}
