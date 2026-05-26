package execution

import (
	"reflect"
	"testing"
)

func TestLocalRootCommandArgs(t *testing.T) {
	got := LocalRootCommandArgs("BOOTWRIGHT_REGISTRY", "/tmp/registry.yaml", "/home/operator", "/usr/bin", "/usr/local/bin/bootwright", []string{"BOOTWRIGHT_AUTH=prompted"}, []string{"check", "syntax"})
	want := []string{
		"env",
		"BOOTWRIGHT_REGISTRY=/tmp/registry.yaml",
		InternalEnv + "=1",
		CallerHomeEnv + "=/home/operator",
		CallerPathEnv + "=/usr/bin",
		"BOOTWRIGHT_AUTH=prompted",
		"/usr/local/bin/bootwright",
		"check",
		"syntax",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LocalRootCommandArgs = %v, want %v", got, want)
	}
}

func TestExecutionPoliciesNamePrivilegeBoundaries(t *testing.T) {
	tests := []struct {
		policy     Policy
		user       User
		needsRoot  bool
		usesBecome bool
	}{
		{policy: CallerRead, user: UserCaller},
		{policy: LocalRootState, user: UserLocalRoot, needsRoot: true},
		{policy: RemoteSSH, user: UserRemoteSSH},
		{policy: RemoteBecome, user: UserRemoteBecome, usesBecome: true},
		{policy: ControllerBecome, user: UserControllerBecome, usesBecome: true},
	}
	for _, tt := range tests {
		if tt.policy.User != tt.user || tt.policy.NeedsRoot != tt.needsRoot || tt.policy.UsesBecome != tt.usesBecome {
			t.Fatalf("%s = %+v, want user=%s needsRoot=%t usesBecome=%t", tt.policy.Name, tt.policy, tt.user, tt.needsRoot, tt.usesBecome)
		}
	}
}
