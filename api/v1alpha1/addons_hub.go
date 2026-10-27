package v1alpha1

type ACMConfig struct {
	Subscription PackageSubscription `json:"subscription"`
	MCE          MCESubscription     `json:"mce"`
	Hub          ACMHub              `json:"hub"`
	Timeout      string              `json:"timeout"`
}

type MCESubscription struct {
	Channel         string `json:"channel"`
	StartingCSV     string `json:"startingCSV"`
	Source          string `json:"source"`
	SourceNamespace string `json:"sourceNamespace"`
}

type ACMHub struct {
	AvailabilityConfig       string            `json:"availabilityConfig,omitempty"`
	DisableHubSelfManagement bool              `json:"disableHubSelfManagement"`
	NodeSelector             map[string]string `json:"nodeSelector,omitempty"`
	Tolerations              []HubToleration   `json:"tolerations,omitempty"`
}

type HubToleration struct {
	Key               string `json:"key,omitempty"`
	Operator          string `json:"operator,omitempty"`
	Value             string `json:"value,omitempty"`
	Effect            string `json:"effect,omitempty"`
	TolerationSeconds *int64 `json:"tolerationSeconds,omitempty"`
}

type GitOpsConfig struct {
	Subscription PackageSubscription `json:"subscription"`
	Bootstrap    *GitOpsBootstrap    `json:"bootstrap,omitempty"`
	Timeout      string              `json:"timeout"`
}

type GitOpsBootstrap struct {
	RepoURL              string        `json:"repoURL"`
	TargetRevision       string        `json:"targetRevision"`
	Path                 string        `json:"path"`
	DestinationNamespace string        `json:"destinationNamespace"`
	CredentialsRef       string        `json:"credentialsRef,omitempty"`
	Access               *GitOpsAccess `json:"access,omitempty"`
	Sync                 GitOpsSync    `json:"sync"`
}

type GitOpsAccess struct {
	ClusterRole string `json:"clusterRole"`
}
type GitOpsSync struct {
	Automated bool `json:"automated"`
	Prune     bool `json:"prune"`
	SelfHeal  bool `json:"selfHeal"`
}

func acmPackageSchema() *Shape {
	s := record(required("subscription", packageSubscriptionSchema()), defaulted("timeout", duration(), StringValue("45m")))
	s.Fields = append(s.Fields, required("mce", record(required("channel", nonempty()), required("startingCSV", nonempty()), field("source", dns()), field("sourceNamespace", name()))), field("hub", record(field("availabilityConfig", enumeration("Basic", "High")), defaulted("disableHubSelfManagement", boolean(), BoolValue(false)), field("nodeSelector", stringsMap()), field("tolerations", array(record(field("key", text()), field("operator", enumeration("Exists", "Equal")), field("value", text()), field("effect", enumeration("NoSchedule", "PreferNoSchedule", "NoExecute")), field("tolerationSeconds", integer("", ""))))))))
	return s
}

func gitopsPackageSchema() *Shape {
	s := record(required("subscription", packageSubscriptionSchema()), defaulted("timeout", duration(), StringValue("20m")))
	s.Fields = append(s.Fields, field("bootstrap", record(required("repoURL", nonempty()), required("targetRevision", nonempty()), required("path", nonempty()), required("destinationNamespace", name()), field("credentialsRef", secret("usernamePassword")), field("access", record(required("clusterRole", dns()))), field("sync", record(defaulted("automated", boolean(), BoolValue(true)), defaulted("prune", boolean(), BoolValue(false)), defaulted("selfHeal", boolean(), BoolValue(false)))))))
	return s
}
