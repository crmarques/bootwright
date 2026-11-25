package agentinstall

import (
	"strings"
	"testing"
	"time"

	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
)

func TestInstallerLogIsSelectedByTheFrozenLocalBlock(t *testing.T) {
	placement := installerLogPlacement{
		Identity:  Identity{Block: MediaBlockID("demo"), Context: "lab", Cluster: "demo"},
		Placement: machine.Placement{Connection: machine.ConnectionLocal, Machine: "controller"},
		WorkRoot:  WorkRoot("lab", "demo"),
	}
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{
		ID: placement.Identity.Block, Object: "demo", Implementation: MediaImplementation,
	}}
	encode := func() {
		t.Helper()
		var err error
		request := MediaRequest{Identity: placement.Identity, Placement: placement.Placement, WorkRoot: placement.WorkRoot, Version: mediaRequestVersion}
		block.Request, err = request.Canonical()
		if err != nil {
			t.Fatal(err)
		}
	}
	encode()
	source, ok := InstallerLogForBlock("lab", block)
	note, valid := installerLogGroupNote("apply", MediaImplementation, placement.WorkRoot)("build-image", installerLogNote)
	if !ok || !valid || !strings.Contains(note, "/clusters/demo/.openshift_install.log") || !source.Active("build-image", "running "+note) {
		t.Fatalf("source = %v, log notice = %q", ok, note)
	}
	if source.Active("wait-install", "running "+note) || source.Active("build-image", "running "+note+"/foreign") {
		t.Fatal("an unrelated group or a different path activated the local reader")
	}
	for _, alter := range []func(){
		func() { placement.Placement.Connection = machine.ConnectionSSH },
		func() { placement.Identity.Context = "other" },
		func() { placement.WorkRoot = WorkRoot("other", "demo") },
	} {
		before := placement
		alter()
		encode()
		if _, ok := InstallerLogForBlock("lab", block); ok {
			t.Errorf("admitted placement %+v", placement)
		}
		placement = before
	}
	block.Request = []byte(`{"version":"unexpected"}`)
	if _, ok := InstallerLogForBlock("lab", block); ok {
		t.Fatal("read a media log from an unsupported frozen request")
	}
}

func TestInstallerLogWaitsUseTheSameFrozenPrivateFile(t *testing.T) {
	work := WorkRoot("lab", "demo")
	request := InstallRequest{
		Identity:  Identity{Block: InstallBlockID("demo"), Context: "lab", Cluster: "demo"},
		Placement: machine.Placement{Connection: machine.ConnectionLocal, Machine: "controller"},
		WorkRoot:  work, Version: installRequestVersion,
	}
	data, err := request.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	block := reconciliation.Block{BlockDefinition: reconciliation.BlockDefinition{
		ID: request.Identity.Block, Object: request.Identity.Cluster, Implementation: InstallImplementation, Request: data,
	}}
	source, ok := InstallerLogForBlock("lab", block)
	if !ok || source.Path() != work+"/.openshift_install.log" {
		t.Fatalf("selected source %q, found %t", source.Path(), ok)
	}
	note := installerLogGroupNote("apply", InstallImplementation, work)
	for _, group := range []string{"wait-bootstrap", "wait-install"} {
		location, valid := note(group, installerLogNote)
		if !valid || !source.Active(group, "running "+location) {
			t.Fatalf("%s is missing its log notice: %q", group, location)
		}
	}
	if location, valid := note("wait-install", "foreign-location"); valid || location != "" {
		t.Fatalf("adapter supplied its own location: %q", location)
	}
	if installerLogGroupNote("observe", InstallImplementation, work) != nil {
		t.Fatal("read-only observation announced a native installer wait")
	}
	block.Request = []byte(`{"version":"unexpected"}`)
	if _, ok := InstallerLogForBlock("lab", block); ok {
		t.Fatal("read an install log from an unsupported frozen request")
	}
	block.Request = data
	block.Implementation = "different-tool"
	if _, ok := InstallerLogForBlock("lab", block); ok {
		t.Fatal("another implementation was able to open an installer log")
	}
}

func TestInstallerLogProjectsOnlyPublicProgress(t *testing.T) {
	source := InstallerLog{implementation: InstallImplementation}
	since := time.Date(2026, 11, 8, 9, 0, 1, 0, time.UTC)
	const fresh = `time="2026-11-08T09:00:01Z" level=`
	for _, test := range []struct{ line, want string }{
		{fresh + `info msg="Waiting up to 40m for the Kubernetes API at https://user:password@api.example.test"`, "Waiting for the Kubernetes API"},
		{fresh + `warning msg="my-pull-secret" password=my-pull-secret`, "Installer reported a warning; see the native log for details"},
		{fresh + `error msg="my-pull-secret"`, "Installer reported an error; see the native log for details"},
		{fresh + `info msg="Login to the console with user: kubeadmin, password: my-pull-secret"`, ""},
		{fresh + `debug msg="loaded my-pull-secret"`, ""},
		{`time="2026-11-08T09:00:00Z" level=info msg="Install complete!"`, ""},
		{`unrecognized level=info msg="Install complete!"`, ""},
		{"Install complete!", ""},
		{fresh + `info`, ""},
		{fresh + `trace msg="Install complete!"`, ""},
		{fresh + `info msg="Bootstrap configMap status is complete"`, "OpenShift bootstrap is complete"},
		{fresh + `info msg="Install complete!"`, "OpenShift installation is complete"},
		{fresh + `info msg="Host: node.example.test reached installation stage writing image"`, "An agent host reached a new installation stage"},
		{fresh + `info msg="Waiting up to 40m for bootstrapping to complete"`, "Waiting for OpenShift bootstrap"},
		{fresh + `info msg="Waiting up to 40m for the cluster operators"`, "Waiting for the cluster operators"},
		{fresh + `info msg="Waiting up to 40m for the cluster to initialize"`, "Waiting for the cluster to initialize"},
	} {
		if got := source.Project([]byte(test.line), since); got != test.want {
			t.Errorf("project %q = %q, want %q", test.line, got, test.want)
		}
	}
	source.implementation = MediaImplementation
	for _, test := range []struct{ message, want string }{
		{"Downloading base ISO from https://user:password@iso.example.test", "Downloading the base ISO"},
		{"Extracting base ISO from release payload", "Extracting the base ISO"},
		{"Creating Agent Installer ISO using configuration", "Creating the agent installer ISO"},
		{"Generated ISO at /native/private/agent.iso", "Generated the agent installer ISO"},
		{"Read unsupported private material", ""},
	} {
		if got := source.Project([]byte(fresh+`info msg="`+test.message+`"`), since); got != test.want {
			t.Errorf("media projection of %q = %q, want %q", test.message, got, test.want)
		}
	}
}
