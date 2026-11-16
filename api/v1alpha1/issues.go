package v1alpha1

const MaxIssues = 999

func AppendIssues(issues []Issue, more ...Issue) []Issue {
	return append(issues, more[:min(len(more), max(MaxIssues-len(issues), 0))]...)
}

func BoundIssues(issues []Issue) []Issue {
	return issues[:min(len(issues), MaxIssues)]
}

func ReferenceIssue(field, message, remediation string) Issue {
	return Issue{Code: "api.reference", Field: field, Message: message, Remediation: remediation}
}
