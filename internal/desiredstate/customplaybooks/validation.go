package customplaybooks

import (
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func Normalize(o api.Object, _ api.Catalog) (api.Object, []api.Issue) {
	if o.Kind() == api.CustomPlaybook {
		o = o.WithSpec(o.Spec().Default("enabled", api.BoolValue(true)))
	}
	return o, nil
}

func ValidateAuthored(o api.Object, c api.Catalog) []api.Issue {
	return ValidatePartial(o, c)
}

// ValidatePartial checks only contradictions already present in unused defaults.
func ValidatePartial(o api.Object, _ api.Catalog) []api.Issue {
	if o.Kind() != api.CustomPlaybook {
		return nil
	}
	issues := validateContent(o.Spec(), "spec", api.NewCatalog(nil), true, true)
	issues = append(issues, validateTargetsAndTags(o.Spec(), true)...)
	if len(issues) > 999 {
		return issues[:999]
	}
	return issues
}

func Validate(o api.Object, c api.Catalog) []api.Issue {
	if o.Kind() != api.CustomPlaybook {
		return nil
	}
	s := o.Spec()
	issues := ValidateContent(s, "spec", c, true)
	issues = append(issues, validateTargetsAndTags(s, false)...)
	if enabled(o) {
		issues = append(issues, validateOrdering(o, c)...)
	}
	if len(issues) == 0 {
		issues = append(issues, issue("api.deferred", "spec", "CustomPlaybook is a reserved declaration and cannot execute."))
	}
	if len(issues) > 999 {
		return issues[:999]
	}
	return issues
}

func validateTargetsAndTags(s api.Value, partial bool) []api.Issue {
	issues := []api.Issue{}
	target := s.Get("target")
	if !partial && target.Present() && target.Get("clusters").Len()+target.Get("machines").Len()+target.Get("hostGroups").Len() == 0 {
		issues = append(issues, issue("api.invariant", "spec.target", "At least one target list must be non-empty."))
	}
	for i, group := range target.Get("hostGroups").Items() {
		if len(issues) >= 999 {
			return issues[:999]
		}
		if slices.Contains([]string{"localhost", "127.0.0.1", "bootwright_ocp_hosts", "bootwright_controller_hosts"}, group.Text()) {
			issues = append(issues, issue("api.value", itemPath("spec.target.hostGroups", i), "Controller inventory identities are forbidden."))
		}
	}
	tags := map[string]bool{}
	for _, tag := range s.Get("tags").Strings() {
		tags[tag] = true
	}
	for i, tag := range s.Get("skipTags").Items() {
		if len(issues) >= 999 {
			return issues[:999]
		}
		if tags[tag.Text()] {
			issues = append(issues, issue("api.invariant", itemPath("spec.skipTags", i), "Tags and skipped tags must not overlap."))
		}
	}
	if len(issues) > 999 {
		return issues[:999]
	}
	return issues
}

func ValidateContent(spec api.Value, prefix string, c api.Catalog, allowGit bool) []api.Issue {
	return validateContent(spec, prefix, c, allowGit, false)
}

// ValidatePartialContent validates an incomplete content declaration without
// treating an omitted source as a final choice of co-located content.
func ValidatePartialContent(spec api.Value, prefix string, allowGit bool) []api.Issue {
	return validateContent(spec, prefix, api.NewCatalog(nil), allowGit, true)
}

func validateContent(spec api.Value, prefix string, c api.Catalog, allowGit, partial bool) []api.Issue {
	issues := []api.Issue{}
	external := spec.Has("source")
	for _, field := range []string{"playbook", "rolesPath", "collectionsPath"} {
		value := spec.Get(field)
		if !value.Present() {
			continue
		}
		segment := map[string]string{"playbook": "playbooks", "rolesPath": "roles", "collectionsPath": "collections"}[field]
		if !api.ValidLexical("relative-path", value.Text()) || !partial && !external && !slices.Contains(strings.Split(value.Text(), "/"), segment) {
			issues = append(issues, issue("api.value", prefix+"."+field, "Content paths must be clean, contained and use their co-located payload segment."))
		}
		if field == "playbook" && !yamlSuffix(value.Text()) {
			issues = append(issues, issue("api.value", prefix+"."+field, "A playbook path must have a YAML suffix."))
		}
		if field != "playbook" && slices.Contains([]string{"vendor", "node_modules"}, path.Base(value.Text())) {
			issues = append(issues, issue("api.value", prefix+"."+field, "Roles and collections directories cannot use excluded dependency-root names."))
		}
	}
	if source := spec.Get("source", "path"); source.Present() && !api.ValidLexical("absolute-path", source.Text()) {
		issues = append(issues, issue("api.value", prefix+".source.path", "An external content root must be a clean absolute path."))
	}
	if git := spec.Get("source", "git"); git.Present() {
		if !allowGit {
			issues = append(issues, issue("api.invariant", prefix+".source.git", "Add-on steps cannot select Git content."))
		} else {
			issues = append(issues, validateGit(git, prefix+".source.git", c)...)
		}
	}
	issues = append(issues, ValidateExtraVars(spec.Get("extraVars"), prefix+".extraVars")...)
	return issues
}

func ValidateExtraVars(value api.Value, field string) []api.Issue {
	issues := []api.Issue{}
	for _, entry := range value.Fields() {
		key := strings.ToLower(entry.Name)
		if strings.HasPrefix(key, "ansible_") || strings.HasPrefix(key, "bootwright_") || strings.HasPrefix(key, "inventory_") || strings.HasPrefix(key, "ssh_") || strings.HasPrefix(key, "become_") || strings.HasPrefix(key, "interpreter_") || slices.Contains([]string{"hostvars", "groups", "group_names", "inventory", "connection", "remote_user", "become", "delegate_to", "hosts", "playbook_dir"}, key) {
			issues = append(issues, issue("api.invariant", field, "Extra variables cannot override connection, inventory, interpreter, SSH, privilege or Bootwright controls."))
			break
		}
	}
	if containsSecretValue(value) {
		issues = append(issues, issue("api.invariant", field, "Inline credential values are forbidden; use declared Secret references."))
	}
	return issues
}

var secretKeySeparators = strings.NewReplacer("_", "", "-", "", ".", "")

func containsSecretValue(value api.Value) bool {
	stack := []api.Value{value}
	for len(stack) != 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, field := range current.Fields() {
			key := strings.ToLower(secretKeySeparators.Replace(field.Name))
			if !strings.HasSuffix(key, "ref") && !strings.HasSuffix(key, "refs") {
				for _, suffix := range []string{"password", "passwd", "token", "secret", "privatekey", "apikey", "accesskey", "secretkey", "credentials"} {
					if strings.HasSuffix(key, suffix) && (field.Value.Type() == api.String && field.Value.Text() != "" || field.Value.Type() == api.Integer || field.Value.Type() == api.Number || field.Value.Len() > 0) {
						return true
					}
				}
			}
			stack = append(stack, field.Value)
		}
		stack = append(stack, current.Items()...)
	}
	return false
}

func validateGit(git api.Value, prefix string, c api.Catalog) []api.Issue {
	issues := []api.Issue{}
	kind, _, valid := gitLocation(git.Get("url").Text())
	if git.Has("url") && !valid {
		issues = append(issues, issue("api.value", prefix+".url", "Git sources require HTTPS, SSH or a clean absolute local repository location."))
	}
	if git.Has("ref") && !validGitRef(git.Get("ref").Text()) {
		issues = append(issues, issue("api.value", prefix+".ref", "Git source ref must be a valid branch, tag or commit reference."))
	}
	if git.Has("subdir") && !api.ValidLexical("relative-path", git.Get("subdir").Text()) {
		issues = append(issues, issue("api.value", prefix+".subdir", "Git source subdirectory must remain below its content root."))
	}
	if git.Has("secretRef") && valid {
		if kind == "local" {
			issues = append(issues, issue("api.invariant", prefix+".secretRef", "Local Git repositories cannot select credentials."))
		} else if secret, found := c.Find(api.Secret, git.Get("secretRef").Text()); found {
			typeName := secret.Spec().Get("type").Text()
			if kind == "ssh" && typeName != "sshKeyPair" || kind == "https" && typeName != "token" && typeName != "usernamePassword" {
				issues = append(issues, issue("api.reference", prefix+".secretRef", "Git credentials have the wrong Secret type for the source transport."))
			}
		}
	}
	return issues
}

func gitLocation(value string) (string, string, bool) {
	if api.ValidLexical("absolute-path", value) {
		return "local", value, true
	}
	if strings.TrimSpace(value) != value || strings.ContainsAny(value, "\x00\r\n\t ") {
		return "", "", false
	}
	u, err := url.Parse(value)
	if err == nil && u.Opaque == "" && u.RawQuery == "" && u.Fragment == "" {
		switch u.Scheme {
		case "https":
			return "https", "", api.ValidLexical("https-url", value) && u.Path != ""
		case "ssh":
			password := false
			if u.User != nil {
				_, password = u.User.Password()
			}
			portValid := u.Port() == ""
			if port, parseErr := strconv.Atoi(u.Port()); parseErr == nil {
				portValid = port > 0 && port <= 65535
			}
			return "ssh", "", !password && api.ValidLexical("host", u.Hostname()) && portValid && u.Path != ""
		case "file":
			return "local", u.Path, u.User == nil && u.Host == "" && api.ValidLexical("absolute-path", u.Path)
		}
	}
	if !strings.Contains(value, "://") {
		host, repository, found := strings.Cut(value, ":")
		if _, after, hasUser := strings.Cut(host, "@"); hasUser {
			host = after
		}
		if found && api.ValidLexical("host", host) && repository != "" && !strings.HasPrefix(repository, "-") {
			return "ssh", "", true
		}
	}
	return "", "", false
}

func validGitRef(value string) bool {
	if value == "" || value == "@" || strings.HasPrefix(value, "-") || strings.HasSuffix(value, ".") || strings.Contains(value, "..") || strings.Contains(value, "@{") || strings.ContainsAny(value, " ~^:?*[\\") {
		return false
	}
	for _, r := range value {
		if r < 32 || r == 127 {
			return false
		}
	}
	for _, component := range strings.Split(value, "/") {
		if component == "" || strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".lock") {
			return false
		}
	}
	return true
}

type SourcePath struct{ Field, Path string }

// ExternalSourcePaths returns decoded lexical paths for the compiler's
// source-root containment check. It does not inspect any content location.
func ExternalSourcePaths(o api.Object) []SourcePath {
	out := []SourcePath{}
	collect := func(spec api.Value, prefix string) {
		if value := spec.Get("source", "path"); value.Type() == api.String {
			out = append(out, SourcePath{Field: prefix + ".source.path", Path: value.Text()})
		}
		if value := spec.Get("source", "git", "url"); value.Type() == api.String {
			kind, local, valid := gitLocation(value.Text())
			if valid && kind == "local" {
				out = append(out, SourcePath{Field: prefix + ".source.git.url", Path: local})
			}
		}
	}
	if o.Kind() == api.CustomPlaybook {
		collect(o.Spec(), "$.spec")
	} else if o.Kind() == api.ClusterAddon {
		for i, step := range o.Spec().Get("steps").Items() {
			collect(step, itemPath("$.spec.steps", i))
		}
	}
	return out
}

func enabled(o api.Object) bool { return !o.Spec().Has("enabled") || o.Spec().Get("enabled").Bool() }

func anchor(o api.Object) string {
	if o.Spec().Has("gates") {
		return o.Spec().Get("gates").Text()
	}
	return o.Spec().Get("follows").Text()
}

func validateOrdering(o api.Object, c api.Catalog) []api.Issue {
	issues := []api.Issue{}
	providers := map[string][]api.Object{}
	for _, candidate := range c.OfKind(api.CustomPlaybook) {
		if enabled(candidate) && anchor(candidate) == anchor(o) {
			seen := map[string]bool{}
			for _, capability := range candidate.Spec().Get("provides").Strings() {
				if !seen[capability] {
					providers[capability] = append(providers[capability], candidate)
					seen[capability] = true
				}
			}
		}
	}
	for _, capability := range o.Spec().Get("provides").Strings() {
		if len(issues) >= 999 {
			return issues[:999]
		}
		if len(providers[capability]) > 1 {
			issues = append(issues, issue("api.duplicate", "spec.provides", "Enabled playbooks at an anchor cannot provide the same capability more than once."))
		}
	}
	visited := map[string]bool{}
	queued := map[string]bool{o.Name(): true}
	pending := []api.Object{o}
	for len(pending) != 0 {
		current := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if visited[current.Name()] {
			continue
		}
		visited[current.Name()] = true
		for _, requirement := range current.Spec().Get("requires").Strings() {
			if len(issues) >= 999 {
				return issues[:999]
			}
			matches := providers[requirement]
			if current.Name() == o.Name() && len(matches) == 0 {
				issues = append(issues, issue("api.reference", "spec.requires", "A required capability has no enabled provider at this anchor."))
			}
			for _, provider := range matches {
				if len(issues) >= 999 {
					return issues[:999]
				}
				if provider.Name() == o.Name() {
					return append(issues, issue("api.invariant", "spec.requires", "Enabled playbook capability requirements form a cycle."))
				}
				if current.Spec().Has("gates") && provider.Spec().Has("follows") {
					issues = append(issues, issue("api.invariant", "spec.requires", "A gating playbook cannot require a capability produced after its anchor."))
				}
				if !queued[provider.Name()] {
					pending = append(pending, provider)
					queued[provider.Name()] = true
				}
			}
		}
	}
	return issues
}

func yamlSuffix(value string) bool {
	ext := strings.ToLower(path.Ext(value))
	return ext == ".yaml" || ext == ".yml"
}

func itemPath(field string, index int) string { return field + "[" + strconv.Itoa(index) + "]" }

func issue(code, field, message string) api.Issue {
	if !strings.HasPrefix(field, "$") {
		field = "$." + field
	}
	return api.Issue{Code: code, Field: field, Message: message}
}
