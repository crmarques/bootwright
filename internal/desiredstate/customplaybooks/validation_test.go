package customplaybooks

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
)

func TestReservedPlaybooksNormalizeWithoutExecutionDefaults(t *testing.T) {
	o := playbook("example")
	normalized, issues := Normalize(o, api.NewCatalog(nil))
	if len(issues) != 0 || o.Spec().Has("enabled") || !normalized.Spec().Get("enabled").Bool() || normalized.Spec().Has("timeout") || normalized.Spec().Has("order") {
		t.Fatal("reserved normalization changed unrelated defaults", issues)
	}
	for _, enabled := range []bool{true, false} {
		candidate := normalized.WithSpec(normalized.Spec().With("enabled", api.BoolValue(enabled)))
		candidate, _ = Normalize(candidate, api.NewCatalog(nil))
		if candidate.Spec().Get("enabled").Bool() != enabled {
			t.Fatal("explicit disabled state lost")
		}
		issues := Validate(candidate, api.NewCatalog([]api.Object{candidate}))
		if len(issues) != 1 || issues[0].Code != "api.deferred" {
			t.Fatal("valid reserved declaration did not report deferred execution", issues)
		}
	}
}

func TestContentPathsAreLexicalAndContained(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		spec  api.Value
		valid bool
	}{
		{"co-located", m("playbook", s("payload/playbooks/setup.YML"), "rolesPath", s("payload/roles"), "collectionsPath", s("payload/collections")), true},
		{"external", m("source", m("path", s("/synthetic-content/nonexistent")), "playbook", s("setup.yaml"), "rolesPath", s("roles-local"), "collectionsPath", s("collections-local")), true},
		{"missing playbook segment", m("playbook", s("setup.yaml")), false},
		{"wrong exact segment", m("playbook", s("my-playbooks/setup.yaml")), false},
		{"escape", m("playbook", s("playbooks/../setup.yaml")), false},
		{"absolute entrypoint", m("playbook", s("/playbooks/setup.yaml")), false},
		{"wrong suffix", m("playbook", s("playbooks/setup.json")), false},
		{"roles dependency root", m("rolesPath", s("roles/vendor")), false},
		{"collections dependency root", m("collectionsPath", s("collections/node_modules")), false},
		{"unclean source", m("source", m("path", s("/synthetic/../content"))), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidateContent(tc.spec, "spec", api.NewCatalog(nil), true); (len(got) == 0) != tc.valid {
				t.Fatalf("validation = %v, valid = %v", got, tc.valid)
			}
		})
	}
}

func TestGitSourceTransportAndCredentialTypes(t *testing.T) {
	secrets := api.NewCatalog([]api.Object{
		object(api.Secret, "token", m("type", s("token"))),
		object(api.Secret, "userpass", m("type", s("usernamePassword"))),
		object(api.Secret, "key", m("type", s("sshKeyPair"))),
	})
	cases := []struct {
		url, credential string
		valid           bool
	}{
		{"https://git.example.test/repository.git", "token", true},
		{"https://git.example.test/repository.git", "userpass", true},
		{"https://git.example.test/repository.git", "key", false},
		{"ssh://git@git.example.test/repository.git", "key", true},
		{"git@git.example.test:repository.git", "key", true},
		{"ssh://git@git.example.test:2222/repository.git", "token", false},
		{"file:///synthetic-content/repository", "", true},
		{"/synthetic-content/repository", "", true},
		{"file:///synthetic-content/repository", "key", false},
		{"http://git.example.test/repository.git", "", false},
		{"https://user:synthetic-password@git.example.test/repository.git", "", false},
		{"https://git.example.test/repository.git?credential=synthetic-password", "", false},
		{"https://git.example.test/repository.git#main", "", false},
		{"ssh://git:synthetic-password@git.example.test/repository.git", "", false},
		{"file://git.example.test/repository", "", false},
		{"relative/repository", "", false},
		{"ssh://git@git.example.test:70000/repository", "", false},
	}
	for i, tc := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			git := m("url", s(tc.url), "ref", s("release/v1"))
			if tc.credential != "" {
				git = git.With("secretRef", s(tc.credential))
			}
			issues := validateGit(git, "spec.source.git", secrets)
			if (len(issues) == 0) != tc.valid || strings.Contains(fmt.Sprint(issues), "synthetic-password") {
				t.Fatalf("validation = %v, valid = %v", issues, tc.valid)
			}
		})
	}
	for _, ref := range []string{"main", "v1.0.0", "refs/heads/release/v1", strings.Repeat("a", 40)} {
		if !validGitRef(ref) {
			t.Fatal("valid Git ref rejected")
		}
	}
	for _, ref := range []string{"", "@", "-main", "a..b", ".hidden", "a.lock", "a@{b", "a/b.", "a//b", "a b", "a~1", "a\nb", "a\\b"} {
		if validGitRef(ref) {
			t.Fatal("invalid Git ref accepted")
		}
	}
}

func TestExtraVarsRejectControlAndCredentialKeysSafely(t *testing.T) {
	t.Parallel()
	for _, key := range []string{"ANSIBLE_HOST", "ansible_connection", "bootwright_state", "inventory_file", "ssh_common_args", "become_user", "interpreter_python", "hostvars", "groups", "remote_user", "delegate_to"} {
		issues := ValidateExtraVars(m(key, s("synthetic-private-marker")), "spec.extraVars")
		if len(issues) == 0 || strings.Contains(fmt.Sprint(issues), key) || strings.Contains(fmt.Sprint(issues), "synthetic-private-marker") {
			t.Fatal("unsafe control-variable handling", issues)
		}
	}
	for _, key := range []string{"password", "adminPassword", "access_token", "client-secret", "private.key", "apiKey", "credentials"} {
		issues := ValidateExtraVars(m("nested", l(m(key, s("synthetic-private-marker")))), "spec.extraVars")
		if len(issues) == 0 || strings.Contains(fmt.Sprint(issues), "synthetic-private-marker") {
			t.Fatal("inline credential accepted or disclosed", issues)
		}
	}
	allowed := m("featureEnabled", api.BoolValue(false), "createSecret", api.BoolValue(false), "password", s(""), "threshold", api.NumberValue("0.125"), "largeInteger", api.IntegerValue("999999999999999999999999999999999"), "passwordRef", s("credential-name"), "nested", l(m("tokenRefs", api.StringList("one"))))
	if issues := ValidateExtraVars(allowed, "spec.extraVars"); len(issues) != 0 {
		t.Fatal("ordinary native variables rejected", issues)
	}
}

func TestTargetsAndTagRestrictionsApplyWhenDisabled(t *testing.T) {
	base := playbook("example")
	cases := []api.Value{
		base.Spec().With("target", m()),
		base.Spec().With("target", m("hostGroups", api.StringList("localhost"))),
		base.Spec().With("target", m("hostGroups", api.StringList("127.0.0.1"))),
		base.Spec().With("target", m("hostGroups", api.StringList("bootwright_ocp_hosts"))),
		base.Spec().With("target", m("hostGroups", api.StringList("bootwright_controller_hosts"))),
		base.Spec().With("tags", api.StringList("setup", "check")).With("skipTags", api.StringList("check")),
	}
	for _, spec := range cases {
		o := base.WithSpec(spec.With("enabled", api.BoolValue(false)))
		issues := Validate(o, api.NewCatalog([]api.Object{o}))
		if len(issues) == 0 || hasCode(issues, "api.deferred") {
			t.Fatal("invalid disabled declaration accepted", issues)
		}
	}
	ordered := base.WithSpec(base.Spec().With("tags", api.StringList("z", "a")))
	normalized, _ := Normalize(ordered, api.NewCatalog(nil))
	if !slices.Equal(normalized.Spec().Get("tags").Strings(), []string{"z", "a"}) {
		t.Fatal("tag order changed")
	}
}

func TestEnabledCapabilityOrdering(t *testing.T) {
	provider := playbook("provider").WithSpec(playbook("provider").Spec().With("provides", api.StringList("available")))
	consumer := playbook("consumer").WithSpec(playbook("consumer").Spec().With("requires", api.StringList("available")))
	valid := api.NewCatalog([]api.Object{consumer, provider})
	if issues := Validate(consumer, valid); len(issues) != 1 || issues[0].Code != "api.deferred" {
		t.Fatal(issues)
	}
	if !hasCode(Validate(consumer, api.NewCatalog([]api.Object{consumer})), "api.reference") {
		t.Fatal("missing provider accepted")
	}
	disabled := provider.WithSpec(provider.Spec().With("enabled", api.BoolValue(false)))
	if !hasCode(Validate(consumer, api.NewCatalog([]api.Object{consumer, disabled})), "api.reference") {
		t.Fatal("disabled provider participated")
	}
	disabledConsumer := consumer.WithSpec(consumer.Spec().With("enabled", api.BoolValue(false)))
	if issues := Validate(disabledConsumer, api.NewCatalog([]api.Object{disabledConsumer})); len(issues) != 1 || issues[0].Code != "api.deferred" {
		t.Fatal("disabled requirement entered graph", issues)
	}
	otherAnchor := provider.WithSpec(provider.Spec().With("gates", s("machines")))
	if !hasCode(Validate(consumer, api.NewCatalog([]api.Object{consumer, otherAnchor})), "api.reference") {
		t.Fatal("different anchor satisfied requirement")
	}
	duplicate := playbook("duplicate").WithSpec(provider.Spec())
	if !hasCode(Validate(provider, api.NewCatalog([]api.Object{provider, duplicate})), "api.duplicate") {
		t.Fatal("duplicate capability providers accepted")
	}
	provider = provider.WithSpec(provider.Spec().With("requires", api.StringList("reverse")))
	consumer = consumer.WithSpec(consumer.Spec().With("provides", api.StringList("reverse")))
	if !hasCode(Validate(consumer, api.NewCatalog([]api.Object{provider, consumer})), "api.invariant") {
		t.Fatal("capability cycle accepted")
	}
	lateProvider := playbook("late").WithSpec(playbook("late").Spec().Without("gates").With("follows", s("base")).With("provides", api.StringList("available")))
	if !hasCode(Validate(consumer, api.NewCatalog([]api.Object{consumer, lateProvider})), "api.invariant") {
		t.Fatal("gate depended on post-anchor capability")
	}
}

func TestExternalSourcesCarryOnlyLexicalPaths(t *testing.T) {
	o := playbook("example").WithSpec(playbook("example").Spec().With("source", m("git", m("url", s("file:///synthetic-content/repository%20directory"), "ref", s("main")))))
	if got := ExternalSourcePaths(o); !reflect.DeepEqual(got, []SourcePath{{Field: "$.spec.source.git.url", Path: "/synthetic-content/repository directory"}}) {
		t.Fatal(got)
	}
	addon := object(api.ClusterAddon, "example", m("steps", l(m("source", m("path", s("/synthetic-content/addon"))))))
	if got := ExternalSourcePaths(addon); !reflect.DeepEqual(got, []SourcePath{{Field: "$.spec.steps[0].source.path", Path: "/synthetic-content/addon"}}) {
		t.Fatal(got)
	}
	o = o.WithSpec(o.Spec().With("source", m("git", m("url", s("https://git.example.test/repository"), "ref", s("main")))))
	if got := ExternalSourcePaths(o); len(got) != 0 {
		t.Fatal("remote source became a local path", got)
	}
}

func FuzzContentSpelling(f *testing.F) {
	for _, seed := range []string{"playbooks/setup.yaml", "file:///synthetic-content/repository", "ssh://git@git.example.test/repository", "../escape", "\x00"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if len(value) > 4096 {
			t.Skip()
		}
		spec := m("source", m("git", m("url", s(value), "ref", s(value))), "playbook", s(value), "rolesPath", s(value), "collectionsPath", s(value))
		first := ValidateContent(spec, "spec", api.NewCatalog(nil), true)
		second := ValidateContent(spec, "spec", api.NewCatalog(nil), true)
		if !reflect.DeepEqual(first, second) {
			t.Fatal("non-deterministic lexical checks")
		}
	})
}

func TestPartialPlaybooksDeferAbsentSourceAndGraph(t *testing.T) {
	partial := object(api.CustomPlaybook, "", m("playbook", s("setup.yaml"), "target", m(), "requires", api.StringList("not-yet-declared")))
	if issues := ValidatePartial(partial, api.NewCatalog(nil)); len(issues) != 0 {
		t.Fatal("incomplete defaults rejected", issues)
	}
	if issues := ValidateAuthored(partial, api.NewCatalog(nil)); len(issues) != 0 {
		t.Fatal("authored source cannot inherit defaults", issues)
	}
	for _, spec := range []api.Value{
		m("playbook", s("../escape.yaml")),
		m("target", m("hostGroups", api.StringList("localhost"))),
		m("source", m("git", m("url", s("/synthetic-content/repository"), "secretRef", s("credential")))),
		m("tags", api.StringList("overlap"), "skipTags", api.StringList("overlap")),
	} {
		issues := ValidatePartial(partial.WithSpec(spec), api.NewCatalog(nil))
		if len(issues) == 0 {
			t.Fatal("contradictory defaults accepted")
		}
		for _, issue := range issues {
			if !strings.HasPrefix(issue.Field, "$.spec") {
				t.Fatal("non-canonical source field", issue.Field)
			}
		}
	}
}

func m(fields ...any) api.Value {
	values := make([]api.FieldValue, 0, len(fields)/2)
	for i := 0; i < len(fields); i += 2 {
		values = append(values, api.FieldValue{Name: fields[i].(string), Value: fields[i+1].(api.Value)})
	}
	return api.MapValue(values...)
}

func s(value string) api.Value { return api.StringValue(value) }

func l(values ...api.Value) api.Value { return api.ListValue(values...) }

func object(kind api.Kind, name string, spec api.Value) api.Object {
	return api.NewObject(kind, name, api.Value{}, spec)
}

func playbook(name string) api.Object {
	return object(api.CustomPlaybook, name, m("gates", s("base"), "playbook", s("playbooks/setup.yaml"), "target", m("hostGroups", api.StringList("workers"))))
}

func hasCode(issues []api.Issue, code string) bool {
	return slices.ContainsFunc(issues, func(issue api.Issue) bool { return issue.Code == code })
}
