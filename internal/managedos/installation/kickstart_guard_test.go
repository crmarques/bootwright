package installation

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

// guardedInstallation is the physical installation with one enabled
// repository, so every string an Installation carries has a value to replace.
func guardedInstallation() Installation {
	input := physicalInstallation()
	input.Repositories = []Repository{{
		BaseURL: "https://mirror.example.test/extras", Enabled: true, GPGCheck: true,
		GPGKeyURL: "https://mirror.example.test/RPM-GPG-KEY", ID: "extras", Name: "Extra packages",
		Proxy: "http://proxy.example.test:3128",
	}}
	return input
}

// kickstartString is one string an Installation carries: its name, whether it
// is an element of a comma-joined list, and how to set it on a copy.
type kickstartString struct {
	name string
	set  func(*Installation, string)
}

// kickstartStrings walks Installation by reflection and returns every string
// field, []string element and Repository string field. Any other field kind
// fails the test, so a field the guard has never seen cannot be added quietly.
func kickstartStrings(t *testing.T) []kickstartString {
	t.Helper()
	var found []kickstartString
	installation := reflect.TypeFor[Installation]()
	for index := range installation.NumField() {
		field := installation.Field(index)
		switch {
		case field.Type.Kind() == reflect.String:
			found = append(found, kickstartString{field.Name, func(input *Installation, value string) {
				reflect.ValueOf(input).Elem().Field(index).SetString(value)
			}})
		case field.Type == reflect.TypeFor[[]string]():
			found = append(found, kickstartString{field.Name, func(input *Installation, value string) {
				reflect.ValueOf(input).Elem().Field(index).Set(reflect.ValueOf([]string{value}))
			}})
		case field.Type == reflect.TypeFor[[]Repository]():
			repository := reflect.TypeFor[Repository]()
			for inner := range repository.NumField() {
				switch repository.Field(inner).Type.Kind() {
				case reflect.String:
					found = append(found, kickstartString{field.Name + "." + repository.Field(inner).Name, func(input *Installation, value string) {
						reflect.ValueOf(&input.Repositories[0]).Elem().Field(inner).SetString(value)
					}})
				case reflect.Bool:
				default:
					t.Fatalf("Repository.%s is a %s the Kickstart guard does not know", repository.Field(inner).Name, repository.Field(inner).Type)
				}
			}
		case field.Type.Kind() == reflect.Bool || field.Type.Kind() == reflect.Int:
		default:
			t.Fatalf("Installation.%s is a %s the Kickstart guard does not know", field.Name, field.Type)
		}
	}
	return found
}

func expectGuardRefusal(t *testing.T, input Installation, what string) {
	t.Helper()
	rendered, err := RenderKickstart(input)
	reported := diagnostics.Of(err)
	if rendered != "" || len(reported) != 1 || reported[0].Code != "api.value" ||
		!strings.HasSuffix(reported[0].Message, " holds a character a Kickstart directive cannot carry") ||
		!strings.HasPrefix(reported[0].Remediation, "correct ") {
		t.Fatalf("%s rendered %q with %#v, want the guard's api.value refusal", what, rendered, reported)
	}
}

// Anaconda splits the file wherever Python's str.splitlines sees a line break,
// so a value holding any of them would start a directive of its own. Every
// string the Kickstart carries refuses all of them, whether or not it is
// rendered today.
func TestEveryKickstartValueRefusesALineBreak(t *testing.T) {
	if _, err := RenderKickstart(guardedInstallation()); err != nil {
		t.Fatal(err)
	}
	fields := kickstartStrings(t)
	names := []string{}
	for _, field := range fields {
		names = append(names, field.name)
	}
	for _, required := range []string{"Address", "Channel", "Formats", "Gateway", "PackageSource", "EnabledServices", "Packages", "Repositories.BaseURL", "Repositories.GPGKeyURL", "Repositories.ID", "Repositories.Name", "Repositories.Proxy"} {
		if !slices.Contains(names, required) {
			t.Fatalf("the walk did not visit %s: %v", required, names)
		}
	}
	breaks := []string{"\n", "\r", "\v", "\f", "\x1c", "\x1d", "\x1e", "\u0085", "\x85", "\u2028", "\u2029", "\x00"}
	for _, field := range fields {
		for _, separator := range breaks {
			input := guardedInstallation()
			field.set(&input, "a"+separator+"b")
			expectGuardRefusal(t, input, fmt.Sprintf("%s holding %q", field.name, separator))
		}
	}
	// Every value interpolated today is also one token, whose rule refuses
	// Unicode whitespace as well, so the line-break set is held on its own:
	// a value a later directive carries whole still refuses every break.
	for _, r := range "\n\r\v\f\x1c\x1d\x1e\u0085\u2028\u2029\x00" {
		if !kickstartLineBreak(r) {
			t.Errorf("%q is not a Kickstart line break", r)
		}
	}
}

// Every value but the package source and a repository's name is one Kickstart
// token: shlex splits on
// whitespace, reads quotes and backslashes and ends the line at '#', and a
// leading '%' opens or closes a section. An element of a comma-joined list
// also refuses the comma.
func TestASingleTokenKickstartValueRefusesSeparatorsAndSections(t *testing.T) {
	listed := []string{"AdditionalLocale", "DisabledServices", "EnabledServices", "Nameservers", "NTPServers"}
	for _, field := range kickstartStrings(t) {
		if field.name == "PackageSource" || field.name == "Repositories.Name" {
			continue
		}
		for _, value := range []string{"a b", "a\u00a0b", "%post", "a#b", "a'b", `a"b`, `a\b`} {
			input := guardedInstallation()
			field.set(&input, value)
			expectGuardRefusal(t, input, fmt.Sprintf("%s holding %q", field.name, value))
		}
		if slices.Contains(listed, field.name) {
			input := guardedInstallation()
			field.set(&input, "a,b")
			expectGuardRefusal(t, input, field.name+" holding a comma")
		}
	}
	input := guardedInstallation()
	input.Repositories[0].BaseURL = "https://mirror.example.test/extras?arch=x86_64,noarch"
	if _, err := RenderKickstart(input); err != nil {
		t.Fatalf("a comma in a value no list joins was refused: %v", err)
	}
	input = guardedInstallation()
	input.PackageSource = "url --url=http://192.0.2.1/a b"
	expectGuardRefusal(t, input, "a package source of two tokens")
	for _, source := range []string{"cdrom", "url --url=http://192.0.2.1:8080/os/rhel-9-8/tree"} {
		input = guardedInstallation()
		input.PackageSource = source
		rendered, err := RenderKickstart(input)
		if err != nil {
			t.Fatalf("the package source %q was refused: %v", source, err)
		}
		requireLine(t, rendered, source)
	}
}

func expectEmptyRefusal(t *testing.T, input Installation, class string) {
	t.Helper()
	rendered, err := RenderKickstart(input)
	reported := diagnostics.Of(err)
	if rendered != "" || len(reported) != 1 || reported[0].Code != "api.value" ||
		reported[0].Message != class+" is empty, which its Kickstart directive cannot carry" ||
		!strings.HasPrefix(reported[0].Remediation, "correct ") {
		t.Fatalf("an empty %s rendered %q with %#v, want the guard's empty-value refusal", class, rendered, reported)
	}
}

// An empty package source renders no install source line at all, so the
// installer would prompt for one; the guard refuses it like admission would.
func TestTheGuardRefusesAnEmptyPackageSource(t *testing.T) {
	input := guardedInstallation()
	input.PackageSource = ""
	expectEmptyRefusal(t, input, "the package source")
	input = guardedInstallation()
	input.Packages = []string{""}
	expectEmptyRefusal(t, input, "a package entry")
	input = guardedInstallation()
	input.Repositories[0].ID = ""
	expectEmptyRefusal(t, input, "a repository ID")
}

// A %packages entry starting with '-' excludes a package instead of
// installing it, which admission's package grammar already refuses.
func TestTheGuardRefusesAPackageEntryStartingWithADash(t *testing.T) {
	input := guardedInstallation()
	input.Packages = []string{"-chrony"}
	expectGuardRefusal(t, input, "a package entry starting with a dash")
	input.Packages = []string{"chrony-tools"}
	if _, err := RenderKickstart(input); err != nil {
		t.Fatalf("a package with an inner dash was refused: %v", err)
	}
}

// A repository's name is the one line after name= in its .repo file, so it may
// hold spaces and '#' but never a line break.
func TestARepositoryNameIsOneLine(t *testing.T) {
	input := guardedInstallation()
	input.Repositories[0].Name = "Extra packages #1 (Bootwright's)"
	rendered, err := RenderKickstart(input)
	if err != nil {
		t.Fatal(err)
	}
	requireLine(t, rendered, "name=Extra packages #1 (Bootwright's)")
	for _, value := range []string{"a\nb", "a\u2028b", "a\x00b"} {
		input = guardedInstallation()
		input.Repositories[0].Name = value
		expectGuardRefusal(t, input, fmt.Sprintf("a repository name holding %q", value))
	}
}

// A repository ID names its section and its file beneath /etc/yum.repos.d, so
// it holds no slash and is never a directory reference.
func TestARepositoryIDHoldsNoSlash(t *testing.T) {
	for _, value := range []string{"x/y", "../x", ".", ".."} {
		input := guardedInstallation()
		input.Repositories[0].ID = value
		expectGuardRefusal(t, input, fmt.Sprintf("a repository ID %q", value))
	}
}
