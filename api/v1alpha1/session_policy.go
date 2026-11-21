package v1alpha1

type SessionAccessPolicy struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Metadata   PolicyMetadata    `yaml:"metadata"`
	Spec       SessionPolicySpec `yaml:"spec"`
}

type PolicyMetadata struct {
	Name   string            `yaml:"name"`
	Labels map[string]string `yaml:"labels,omitempty"`
}

type SessionPolicySpec struct {
	Grants []SessionGrant `yaml:"grants"`
}

type SessionGrant struct {
	Name       string          `yaml:"name"`
	Subjects   []PolicySubject `yaml:"subjects"`
	Targets    SessionTargets  `yaml:"targets"`
	RemoteUser string          `yaml:"remoteUser"`
	Exec       *SessionExec    `yaml:"exec,omitempty"`
	Rsh        *struct{}       `yaml:"rsh,omitempty"`
}

type PolicySubject struct {
	User  string `yaml:"user,omitempty"`
	Group string `yaml:"group,omitempty"`
}

type SessionTargets struct {
	MachineRefs []string `yaml:"machineRefs,omitempty"`
	ClusterRefs []string `yaml:"clusterRefs,omitempty"`
}

type SessionExec struct {
	Commands [][]string `yaml:"commands"`
}
