package mirrorfill

import (
	"slices"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	"github.com/crmarques/bootwright/internal/infrastructureservices/managedservice"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

const (
	imageBudget  = 20 * time.Minute
	SkopeoReason = "skopeo-missing"
)

func imagesDeadline(request ImagesRequest) time.Duration {
	return min(lifecycle.MaxDeadline, runOverhead+time.Duration(len(request.Images))*imageBudget)
}

type ImageFill struct {
	fillCapability
	images Images
}

func NewImages(runner Runner, images Images) ImageFill {
	c := ImageFill{images: images}
	c.fillCapability = fill[ImagesRequest, ImagesEvidence]{
		runner: runner, noun: "mirror images", kind: ImagesKind, implementation: ImagesImplementation, variable: "bootwright_mirror_images",
		quiescent:   "every consumer of the image mirror precedes its fill in removal",
		requests:    c.requestList,
		decode:      DecodeImagesRequest,
		canonical:   ImagesRequest.Canonical,
		object:      func(request ImagesRequest) string { return request.Identity.Registry },
		placement:   func(request ImagesRequest) machineref.Placement { return request.Placement },
		definition:  imagesDefinition,
		describe:    imagesDescription,
		impacts:     imagesImpacts,
		nothing:     imagesNothing,
		references:  ImagesRequest.SecretReferences,
		classes:     ImagesRequest.SecretClasses,
		deadline:    imagesDeadline,
		materials:   func(request ImagesRequest) []lifecycle.MaterialFile { return request.Mirror.Materials() },
		refusals:    imagesRefusals,
		decodeProof: decodeImagesEvidence,
		proven:      func(evidence ImagesEvidence) bool { return evidence.Postcondition },
		effect:      imagesEffect,
		drift:       imagesDrift,
		subject:     func(request ImagesRequest) string { return "the image mirror of " + request.Identity.Environment },
		removed: func(digest string) any {
			return ImagesEvidence{Absent: true, Images: []ImageEvidence{}, Postcondition: true, Request: digest, Version: ImagesEvidenceVersion}
		},
		claimed: imagesClaimed,
	}
	return c
}

func (c ImageFill) WithInstalledHostKeys(hostKeys lifecycle.InstalledHostKey) ImageFill {
	c.fillCapability = c.fillCapability.withHostKeys(hostKeys)
	return c
}

func (c ImageFill) requestList(catalog api.Catalog, controllerMachine, contextName string) ([]ImagesRequest, error) {
	request, found, err := c.Request(catalog, controllerMachine, contextName)
	if err != nil || !found {
		return nil, err
	}
	return []ImagesRequest{request}, nil
}

func imagesDefinition(verb reconciliation.Verb, request ImagesRequest, canonical []byte) reconciliation.BlockDefinition {
	return reconciliation.BlockDefinition{
		ID: ImagesBlockID(request.Identity.Environment), Description: imagesDescription(verb, request), Stage: reconciliation.StageInfrastructureServices,
		Requires: placedRequires(request.Identity.Registry, request.Placement),
		Impacts:  imagesImpacts(verb, request), Consumes: []string{},
		Kind: ImagesKind, Object: request.Identity.Registry, Family: Family, Implementation: ImagesImplementation, ContentDigest: ImagesContentDigest(), Request: canonical,
	}
}

func placedRequires(registry string, placement machineref.Placement) []reconciliation.ObjectRef {
	requires := []reconciliation.ObjectRef{{Kind: ImagesKind, Object: registry, Family: managedservice.Family}}
	if placement.Installed() {
		requires = append(requires, reconciliation.ObjectRef{Kind: string(api.Machine), Object: placement.Machine, Family: machineref.InstallFamily})
	}
	return requires
}

func (c ImageFill) Request(catalog api.Catalog, controllerMachine, contextName string) (ImagesRequest, bool, error) {
	mirror, declared := infrastructureservices.ImageMirrorOf(catalog)
	if !declared || !mirror.Managed || mirror.Location == "" {
		return ImagesRequest{}, false, nil
	}
	if c.images == nil {
		return ImagesRequest{}, false, mirrorPlanningInputMissing.Fail("mirror planning requires the acquisition list", "")
	}
	images, err := c.images(catalog)
	if err != nil || len(images) == 0 {
		return ImagesRequest{}, false, err
	}
	host, err := imageMirrorHost(catalog, mirror, controllerMachine)
	if err != nil {
		return ImagesRequest{}, false, err
	}
	request := ImagesRequest{
		Egress: host.egress, Identity: ImagesIdentity{Context: contextName, Environment: catalog.OfKind(api.Environment)[0].Name(), Registry: host.registry.Name()},
		Images: slices.Clone(images), Mirror: host.source, Placement: host.placement, Version: ImagesImplementation,
	}
	slices.SortFunc(request.Images, func(x, y Image) int { return strings.Compare(x.Source, y.Source) })
	request.Images = slices.Compact(request.Images)
	if err := request.validate(); err != nil {
		return ImagesRequest{}, false, err
	}
	return request, true, nil
}

func imagesDescription(verb reconciliation.Verb, request ImagesRequest) string {
	if verb == reconciliation.Destroy {
		return "release the image mirror of " + request.Identity.Environment + " on " + request.Placement.Machine + ", whose Registry's removal deletes its images"
	}
	return "fill the image mirror of " + request.Identity.Environment + " on " + request.Placement.Machine
}

func imagesNothing(request ImagesRequest) string {
	return "the removal of Registry " + request.Identity.Registry + " deletes the images this fill pushed into it"
}

func imagesImpacts(verb reconciliation.Verb, request ImagesRequest) []string {
	if verb == reconciliation.Destroy {
		return []string{}
	}
	impacts := make([]string, 0, len(request.Images))
	for _, image := range request.Images {
		impacts = append(impacts, "publish-content "+request.Mirror.Host+"/"+image.Repository+"@"+image.Digest)
	}
	return impacts
}

func imagesRefusals(contextName string, request ImagesRequest) map[string]error {
	registry := diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: ImagesKind, Name: request.Identity.Registry}
	refused := func(stop diagnostics.Stop, message, remediation string) error {
		return refusedOn(&registry, stop, message, remediation)
	}
	machine := "Machine/" + request.Placement.Machine
	return map[string]error{
		SkopeoReason: refused(mirrorSkopeoMissing, machine+" has no working /usr/bin/skopeo, so the image mirror of "+request.Identity.Environment+" cannot copy its images; this attempt copied nothing",
			"install the skopeo package on "+machine+" with its package manager and repeat the apply, or place Registry/"+request.Identity.Registry+" on a Machine that has it"),
		"image-digest-mismatch": refused(mirrorDigestMismatch, "the image mirror on "+request.Placement.Machine+" reports a digest other than the pinned one for an image it was given, so the fill stopped",
			"verify the Registry and the route from "+request.Placement.Machine+" to the image's origin, then run bootwright apply --context "+contextName),
		"image-unreachable": refused(mirrorOriginUnreachable, machine+" could not copy an image from its origin into the image mirror",
			"give "+request.Placement.Machine+" a route to the origins bootwright render acquisitions --context "+contextName+" names, then run bootwright apply --context "+contextName),
		"mirror-unreadable": refused(mirrorUnreadable, machine+" could not read the image mirror at "+request.Mirror.Host+" with its credential and trust anchor",
			"verify Registry/"+request.Identity.Registry+" is ready and answers its credential, then run bootwright apply --context "+contextName),
	}
}

func imagesClaimed(catalog api.Catalog) []string {
	mirror, declared := infrastructureservices.ImageMirrorOf(catalog)
	if !declared || !mirror.Managed {
		return nil
	}
	return []string{mirror.Registry.Identity()}
}
