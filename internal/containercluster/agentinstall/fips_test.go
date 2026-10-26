package agentinstall

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"strings"
	"testing"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	controllerscope "github.com/crmarques/bootwright/internal/controller"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/secrets"
	"github.com/crmarques/bootwright/internal/secrets/material"

	"golang.org/x/crypto/ssh"
)

func TestFIPSProjectionAndBoundEd25519RefuseBeforeNativeExecution(t *testing.T) {
	objects := singleNodeCatalog().Objects()
	for i, object := range objects {
		if object.Kind() == api.ContainerCluster {
			objects[i] = object.WithSpec(object.Spec().WithPath(api.BoolValue(true), "security", "fips", "enabled"))
		}
	}
	media, install, _ := onlyRequests(t, api.NewCatalog(objects))
	if media.InstallConfig["fips"] != true || media.Tool.Kind != "openshift-install-fips" || install.Tool.Kind != "openshift-install-fips" {
		t.Fatalf("FIPS projection missing: %#v %#v", media.Tool, install.Tool)
	}
	canonical, err := media.Canonical()
	if err != nil {
		t.Fatal(err)
	}
	matchesGolden(t, "media-request-fips", canonical)
	execution, _ := mediaExecution(t, testDigest)
	execution.Block.Request = canonical
	execution.LocateTool = func(_ context.Context, tool controllerscope.InstalledTool) (string, error) {
		if tool.Kind != "openshift-install-fips" || tool.Executable != "openshift-install-fips" {
			t.Fatalf("wrong native tool: %#v", tool)
		}
		return "/synthetic/openshift-install-fips", nil
	}
	runner := &fakeRunner{}
	_, err = NewMedia(runner, material.AuthorizedKey).Apply(t.Context(), execution)
	ds := diagnostics.Of(err)
	if len(ds) != 1 || ds[0].Code != "controller.unsupported" || !strings.Contains(ds[0].Message, "Secret/"+media.SSHKeyRef) || len(runner.requests) != 0 {
		t.Fatalf("FIPS key refusal did not precede native execution: %v, calls=%d", err, len(runner.requests))
	}
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public, err := ssh.NewPublicKey(&private.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	execution.Material[media.SSHKeyRef] = secrets.NewMaterial(map[secrets.Part][]byte{secrets.PublicKeyPart: ssh.MarshalAuthorizedKey(public)})
	_, err = NewMedia(runner, material.AuthorizedKey).run(t.Context(), execution, "apply", media)
	if err != nil || len(runner.requests) != 1 {
		t.Fatalf("FIPS ECDSA key did not reach native runner: %v", err)
	}
}

func TestFIPSInstallWaitsResolveTheFIPSExecutable(t *testing.T) {
	_, request, _ := onlyRequests(t, singleNodeCatalog())
	request.Tool.Kind = "openshift-install-fips"
	execution, _ := mediaExecution(t, testDigest)
	executables := []string{}
	execution.LocateTool = func(_ context.Context, tool controllerscope.InstalledTool) (string, error) {
		executables = append(executables, tool.Executable)
		return "/synthetic/" + tool.Executable, nil
	}
	runner := &fakeRunner{}
	_, err := NewInstall(runner).run(t.Context(), execution, "observe", request)
	if err != nil || len(executables) != 2 || executables[0] != "openshift-install-fips" || executables[1] != "oc" || len(runner.requests) != 1 {
		t.Fatalf("wrong wait tools: %v %v", executables, err)
	}
}
