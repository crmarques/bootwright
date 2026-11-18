package mirrorfill

import (
	"slices"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/infrastructureservices/artifactserver"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	"github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const (
	FileStall   = 2 * time.Minute
	fileBudget  = 10 * time.Minute
	runOverhead = 15 * time.Minute
)

func filesDeadline(request FilesRequest) time.Duration {
	return min(lifecycle.MaxDeadline, runOverhead+time.Duration(len(request.Files))*fileBudget)
}

type Files struct {
	fillCapability
	artifacts Artifacts
}

func NewFiles(runner Runner, artifacts Artifacts) Files {
	c := Files{artifacts: artifacts}
	c.fillCapability = fill[FilesRequest, FilesEvidence]{
		runner: runner, noun: "mirror files", kind: Kind, implementation: FilesImplementation, variable: "bootwright_mirror_files",
		quiescent:   "the controller stage that reads the mirror precedes its fill in removal",
		requests:    c.requestList,
		decode:      DecodeFilesRequest,
		canonical:   FilesRequest.Canonical,
		object:      func(request FilesRequest) string { return request.Identity.Server },
		placement:   func(request FilesRequest) machine.Placement { return request.Placement },
		definition:  filesDefinition,
		describe:    filesDescription,
		impacts:     filesImpacts,
		references:  FilesRequest.SecretReferences,
		classes:     func(request FilesRequest) secrets.Classes { return request.Placement.SecretClasses() },
		deadline:    filesDeadline,
		refusals:    filesRefusals,
		decodeProof: decodeFilesEvidence,
		proven:      func(evidence FilesEvidence) bool { return evidence.Postcondition },
		effect:      filesEffect,
		drift:       filesDrift,
		subject: func(request FilesRequest) string {
			return "the artifact mirror files of " + request.Identity.Environment
		},
		claimed: filesClaimed,
	}
	return c
}

func (c Files) WithInstalledHostKeys(hostKeys lifecycle.InstalledHostKey) Files {
	c.fillCapability = c.fillCapability.withHostKeys(hostKeys)
	return c
}

func (c Files) requestList(catalog api.Catalog, controllerMachine, contextName string) ([]FilesRequest, error) {
	request, found, err := c.Request(catalog, controllerMachine, contextName)
	if err != nil || !found {
		return nil, err
	}
	return []FilesRequest{request}, nil
}

func filesDefinition(verb reconciliation.Verb, request FilesRequest, canonical []byte) reconciliation.BlockDefinition {
	return reconciliation.BlockDefinition{
		ID: FilesBlockID(request.Identity.Environment), Description: filesDescription(verb, request), Stage: reconciliation.StageInfrastructureServices,
		Requires:  filesRequires(request),
		Exclusive: []string{infrastructureservices.PathReservationKey(request.Root).String()}, Impacts: filesImpacts(verb, request), Consumes: []string{},
		Kind: Kind, Object: request.Identity.Server, Family: Family, Implementation: FilesImplementation, ContentDigest: FilesContentDigest(), Request: canonical,
	}
}

func filesRequires(request FilesRequest) []reconciliation.ObjectRef {
	requires := []reconciliation.ObjectRef{{Kind: Kind, Object: request.Identity.Server, Family: managedservice.Family}}
	if request.Placement.Installed() {
		requires = append(requires, reconciliation.ObjectRef{Kind: string(api.Machine), Object: request.Placement.Machine, Family: machine.InstallFamily})
	}
	return requires
}

func (c Files) Request(catalog api.Catalog, controllerMachine, contextName string) (FilesRequest, bool, error) {
	mirror, declared := infrastructureservices.ArtifactMirrorOf(catalog)
	if !declared || !mirror.Managed || mirror.URL == "" {
		return FilesRequest{}, false, nil
	}
	if c.artifacts == nil {
		return FilesRequest{}, false, mirrorPlanningInputMissing.Fail("mirror planning requires the acquisition list", "")
	}
	artifacts, err := c.artifacts(catalog)
	if err != nil {
		return FilesRequest{}, false, err
	}
	server := mirror.Server
	machine, found := catalog.Find(api.Machine, server.Spec().Get("machineRef").Text())
	if !found {
		return FilesRequest{}, false, refusal("api.reference", "the artifact mirror's placement Machine is not in the selected graph", "declare it or correct machineRef on "+server.Identity())
	}
	placement, err := artifactserver.ServicePlacementFor(catalog, server, controllerMachine)
	if err != nil {
		return FilesRequest{}, false, err
	}
	egress, err := managedservice.EgressFor(catalog, machine)
	if err != nil {
		return FilesRequest{}, false, err
	}
	environment := catalog.OfKind(api.Environment)[0].Name()
	served := artifactserver.ServedRoot(contextName, server.Name())
	request := FilesRequest{
		Egress: egress, Files: []File{}, Identity: Identity{Context: contextName, Environment: environment, Server: server.Name()},
		Placement: placement, Root: served + "/" + infrastructureservices.MirrorPath + "/" + environment, ServedRoot: served, Version: FilesImplementation,
	}
	for _, artifact := range artifacts {
		path, beneath := strings.CutPrefix(artifact.Location, mirror.URL+"/")
		if !beneath {
			continue
		}
		request.Files = append(request.Files, File{Bytes: artifact.Bytes, Path: path, SHA256: artifact.SHA256, Source: artifact.Source})
	}
	slices.SortFunc(request.Files, func(x, y File) int { return strings.Compare(x.Path, y.Path) })
	request.Files = slices.CompactFunc(request.Files, func(x, y File) bool { return x == y })
	if len(request.Files) == 0 {
		return FilesRequest{}, false, nil
	}
	if err := request.validate(); err != nil {
		return FilesRequest{}, false, err
	}
	return request, true, nil
}

func filesDescription(verb reconciliation.Verb, request FilesRequest) string {
	if verb == reconciliation.Destroy {
		return "remove the artifact mirror files of " + request.Identity.Environment + " from " + request.Placement.Machine
	}
	return "fill the artifact mirror of " + request.Identity.Environment + " on " + request.Placement.Machine
}

func filesImpacts(verb reconciliation.Verb, request FilesRequest) []string {
	if verb == reconciliation.Destroy {
		return []string{"remove-content " + request.Root}
	}
	return []string{"publish-content " + request.Root}
}

func filesRefusals(contextName string, request FilesRequest) map[string]error {
	server := diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: Kind, Name: request.Identity.Server}
	refused := func(stop diagnostics.Stop, message, remediation string) error {
		return refusedOn(&server, stop, message, remediation)
	}
	return map[string]error{
		"file-digest-mismatch": refused(mirrorDigestMismatch, "a publisher served a client file for the artifact mirror on "+request.Placement.Machine+" whose SHA-256 is not the one this build pins, so it was not published",
			"verify the publisher and the route from "+request.Placement.Machine+" to it, then run bootwright apply --context "+contextName),
		"file-unreachable": refused(mirrorOriginUnreachable, "Machine/"+request.Placement.Machine+" could not read a client file for the artifact mirror from its publisher, so it was not published",
			"give "+request.Placement.Machine+" a route to the publishers bootwright render acquisitions --context "+contextName+" names, then run bootwright apply --context "+contextName),
	}
}

func (Files) PrecedesController(definition reconciliation.BlockDefinition) bool {
	_, err := DecodeFilesRequest(definition.Request)
	return err == nil
}

func filesClaimed(catalog api.Catalog) []string {
	mirror, declared := infrastructureservices.ArtifactMirrorOf(catalog)
	if !declared || !mirror.Managed {
		return nil
	}
	return []string{mirror.Server.Identity()}
}
