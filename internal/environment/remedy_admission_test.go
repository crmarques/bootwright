package environment_test

import (
	"slices"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/environment"
)

func field(name string, value api.Value) api.FieldValue {
	return api.FieldValue{Name: name, Value: value}
}

func text(name, value string) api.FieldValue { return field(name, api.StringValue(value)) }

func controllerEnvironment() api.Object {
	return api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
		field("controller", api.MapValue(text("machineRef", "ctl"))),
	))
}

func controllerMachine(provided, local, runtime bool) api.Object {
	spec := []api.FieldValue{
		field("os", api.MapValue(field("provided", api.BoolValue(provided)))),
	}
	if local {
		spec = append(spec, field("access", api.MapValue(field("local", api.BoolValue(true)))))
	}
	if runtime {
		spec = append(spec, field("capabilities", api.ListValue(api.StringValue("container-runtime"))))
	}
	return api.NewObject(api.Machine, "ctl", api.Value{}, api.MapValue(spec...))
}

func rescueEnvironment(version string, endpoint api.Value) api.Object {
	rescue := []api.FieldValue{text("imageRef", "media")}
	if version != "" {
		rescue = append(rescue, field("os", api.MapValue(text("version", version))))
	}
	if endpoint.Present() {
		rescue = append(rescue, field("artifactServerEndpoint", endpoint))
	}
	return api.NewObject(api.Environment, "lab", api.Value{}, api.MapValue(
		field("lifecycle", api.MapValue(field("rescue", api.MapValue(rescue...)))),
	))
}

func artifactServer(retention, machineRef string) api.Object {
	spec := []api.FieldValue{
		text("management", "managed"),
		field("endpoints", api.ListValue(api.MapValue(text("name", "e")))),
		text("retention", retention),
	}
	if machineRef != "" {
		spec = append(spec, text("machineRef", machineRef))
	}
	return api.NewObject(api.ArtifactServer, "artifacts", api.Value{}, api.MapValue(spec...))
}

func TestEveryEnvironmentRefusalNamesItsStep(t *testing.T) {
	const controllerField = "$.spec.controller.machineRef"
	endpoint := api.MapValue(text("serverRef", "artifacts"), text("endpointRef", "e"))
	media := func(bootMedia string) api.Object {
		return api.NewObject(api.MachineImage, "media", api.Value{}, api.MapValue(text("bootMedia", bootMedia)))
	}
	cluster := api.NewObject(api.ContainerCluster, "apps", api.Value{}, api.MapValue(
		field("nodes", api.ListValue(api.MapValue(text("machineRef", "ctl")))),
	))
	other := api.NewObject(api.Machine, "other", api.Value{}, api.MapValue(
		field("access", api.MapValue(field("local", api.BoolValue(true)))),
	))
	env := controllerEnvironment()
	for _, row := range []struct {
		name                        string
		environment                 api.Object
		others                      []api.Object
		field, message, remediation string
	}{
		{"not OS-ready", env, []api.Object{controllerMachine(false, true, true)}, controllerField,
			"controller must reference an OS-ready Machine with os.provided: true",
			"set spec.os.provided to true on Machine/ctl, or set spec.controller.machineRef to an OS-ready Machine"},
		{"not local", env, []api.Object{controllerMachine(true, false, true)}, controllerField,
			"controller must reference the local Machine with access.local: true and no SSH access",
			"give Machine/ctl access.local: true and no spec.access.ssh"},
		{"no container runtime", env, []api.Object{controllerMachine(true, true, false)}, controllerField,
			"controller Machine must declare the container-runtime capability",
			"add container-runtime to spec.capabilities on Machine/ctl"},
		{"another local Machine", env, []api.Object{controllerMachine(true, true, true), other}, controllerField,
			"only the controller Machine may declare local access; Machine/other also declares access.local: true",
			"remove spec.access.local from Machine/other"},
		{"cluster node", env, []api.Object{controllerMachine(true, true, true), cluster}, controllerField,
			"controller must remain outside cluster node membership; it is a node of ContainerCluster/apps",
			"remove Machine/ctl from the nodes of ContainerCluster/apps, or set spec.controller.machineRef to a Machine outside cluster membership"},
		{"rescue version", rescueEnvironment("8.10", api.Value{}), []api.Object{media("local-media:rhel-8.10-x86_64-boot.iso")}, "$.spec.lifecycle.rescue.os.version",
			"rescue requires a RHEL 9 version",
			"set spec.lifecycle.rescue.os.version to a RHEL 9 version, 9.<minor>"},
		{"ephemeral artifact server", rescueEnvironment("", endpoint), []api.Object{media("local-media:rhel-9.8-x86_64-boot.iso"), artifactServer("ephemeral", "")},
			"$.spec.lifecycle.rescue.artifactServerEndpoint.serverRef",
			"rescue requires a persistent artifact server",
			"set spec.retention to persistent on ArtifactServer/artifacts"},
		{"artifact server on a Machine without an OS", rescueEnvironment("", endpoint),
			[]api.Object{media("local-media:rhel-9.8-x86_64-boot.iso"), artifactServer("persistent", "host"), api.NewObject(api.Machine, "host", api.Value{}, api.MapValue(
				field("os", api.MapValue(field("provided", api.BoolValue(false)))),
			))},
			"$.spec.lifecycle.rescue.artifactServerEndpoint.serverRef",
			"rescue artifact server requires an OS-ready Machine",
			"set spec.machineRef on ArtifactServer/artifacts to a Machine with os.provided: true"},
	} {
		t.Run(row.name, func(t *testing.T) {
			issues := environment.Validate(row.environment, api.NewCatalog(append([]api.Object{row.environment}, row.others...)))
			found := false
			for _, issue := range issues {
				if issue.Remediation == "" {
					t.Errorf("a refusal names no step: %+v", issue)
				}
				if issue.Field == row.field && issue.Message == row.message {
					found = true
					if issue.Code != "api.invariant" || issue.Remediation != row.remediation {
						t.Errorf("refusal = %+v, want remediation %q", issue, row.remediation)
					}
				}
			}
			if !found {
				t.Fatalf("no refusal %q at %s in %+v", row.message, row.field, issues)
			}
		})
	}
}

func TestARemoteRescueImageIsNotRefusedForItsChecksumAgain(t *testing.T) {
	env := rescueEnvironment("9.8", api.Value{})
	image := api.NewObject(api.MachineImage, "media", api.Value{}, api.MapValue(text("bootMedia", "https://example.test/a.iso")))
	issues := environment.Validate(env, api.NewCatalog([]api.Object{env, image}))
	if slices.ContainsFunc(issues, func(issue api.Issue) bool { return issue.Field == "$.spec.lifecycle.rescue.imageRef" }) {
		t.Fatalf("issues = %+v, want none at the rescue imageRef", issues)
	}
}
