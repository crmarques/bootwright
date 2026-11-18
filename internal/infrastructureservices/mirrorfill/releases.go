package mirrorfill

import (
	"slices"
	"strings"
	"time"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/diagnostics"
	"github.com/crmarques/bootwright/internal/infrastructureservices"
	machineref "github.com/crmarques/bootwright/internal/machine"
	"github.com/crmarques/bootwright/internal/reconciliation"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/crmarques/bootwright/internal/secrets"
)

const releaseBudget = 4 * time.Hour

func releaseDeadline() time.Duration { return min(lifecycle.MaxDeadline, runOverhead+releaseBudget) }

type ReleaseFill struct {
	fillCapability
	releases Releases
	auth     ReleaseAuth
}

func NewReleases(runner Runner, releases Releases, auth ReleaseAuth) ReleaseFill {
	c := ReleaseFill{releases: releases, auth: auth}
	c.fillCapability = fill[ReleaseRequest, ReleaseEvidence]{
		runner: runner, noun: "mirror release", kind: ReleaseKind, implementation: ReleaseImplementation, variable: "bootwright_mirror_release",
		quiescent:   "the cluster that installs from the copied release precedes it in removal",
		requests:    c.Requests,
		decode:      DecodeReleaseRequest,
		canonical:   ReleaseRequest.Canonical,
		object:      func(request ReleaseRequest) string { return request.Identity.Cluster },
		placement:   func(request ReleaseRequest) machineref.Placement { return request.Placement },
		definition:  releaseDefinition,
		describe:    releaseDescription,
		impacts:     releaseImpacts,
		references:  ReleaseRequest.SecretReferences,
		classes:     ReleaseRequest.SecretClasses,
		deadline:    func(ReleaseRequest) time.Duration { return releaseDeadline() },
		materials:   func(request ReleaseRequest) []lifecycle.MaterialFile { return request.Mirror.Materials() },
		refusals:    releaseRefusals,
		lend:        c.lendRelease,
		decodeProof: decodeReleaseEvidence,
		proven:      func(evidence ReleaseEvidence) bool { return evidence.Postcondition },
		effect:      releaseEffect,
		drift:       releaseDrift,
		subject:     func(request ReleaseRequest) string { return "the release copy of " + request.Identity.Cluster },
		removed: func(digest string) any {
			return ReleaseEvidence{Absent: true, Components: []ComponentEvidence{}, Postcondition: true, Request: digest, Version: ReleaseEvidenceVersion}
		},
		claimed: c.claimedClusters,
	}
	return c
}

func (c ReleaseFill) WithInstalledHostKeys(hostKeys lifecycle.InstalledHostKey) ReleaseFill {
	c.fillCapability = c.fillCapability.withHostKeys(hostKeys)
	return c
}

func (c ReleaseFill) lendRelease(execution lifecycle.Execution, request ReleaseRequest) (lifecycle.Execution, []lifecycle.MaterialFile, func(), error) {
	lent, err := c.lendReleaseAuth(execution, request)
	if err != nil {
		return execution, nil, nil, err
	}
	material := lifecycle.MaterialFile{Name: "release-auth", Part: secrets.ValuePart, Secret: releaseAuthMaterial, Variable: "releaseAuth"}
	return lent, []lifecycle.MaterialFile{material}, func() { lent.Material[releaseAuthMaterial].Clear() }, nil
}

func releaseDefinition(verb reconciliation.Verb, request ReleaseRequest, canonical []byte) reconciliation.BlockDefinition {
	return reconciliation.BlockDefinition{
		ID: ReleaseBlockID(request.Identity.Cluster), Description: releaseDescription(verb, request), Stage: reconciliation.StageInfrastructureServices,
		Requires:  placedRequires(request.Identity.Registry, request.Placement),
		Exclusive: []string{"registry-push:" + request.Identity.Registry}, Impacts: releaseImpacts(verb, request), Consumes: []string{},
		Kind: ReleaseKind, Object: request.Identity.Cluster, Family: Family, Implementation: ReleaseImplementation, ContentDigest: ReleaseContentDigest(), Request: canonical,
	}
}

func (c ReleaseFill) Requests(catalog api.Catalog, controllerMachine, contextName string) ([]ReleaseRequest, error) {
	mirror, declared := infrastructureservices.ImageMirrorOf(catalog)
	if !declared || !mirror.Managed || mirror.Location == "" {
		return nil, nil
	}
	if c.releases == nil {
		return nil, mirrorPlanningInputMissing.Fail("mirror planning requires the cluster releases", "")
	}
	releases, err := c.releases(catalog)
	if err != nil || len(releases) == 0 {
		return nil, err
	}
	host, err := imageMirrorHost(catalog, mirror, controllerMachine)
	if err != nil {
		return nil, err
	}
	environment := catalog.OfKind(api.Environment)[0].Name()
	requests := make([]ReleaseRequest, 0, len(releases))
	for _, release := range releases {
		if err := release.Refusal(); err != nil {
			return nil, err
		}
		request := ReleaseRequest{
			Client: release.Client, Egress: host.egress, Mirror: host.source, Placement: host.placement, PullSecretRef: release.PullSecretRef, Version: ReleaseImplementation,
			Identity: ReleaseIdentity{Cluster: release.Cluster, Context: contextName, Environment: environment, Registry: host.registry.Name()},
			Release: ReleaseImage{Components: infrastructureservices.OpenShiftComponentRepository, Digest: release.Digest,
				Repository: infrastructureservices.OpenShiftReleaseRepository, Tag: ReleaseTag(release.Version), Version: release.Version},
		}
		if err := request.validate(); err != nil {
			return nil, err
		}
		requests = append(requests, request)
	}
	slices.SortFunc(requests, func(x, y ReleaseRequest) int { return strings.Compare(x.Identity.Cluster, y.Identity.Cluster) })
	return requests, nil
}

func releaseDescription(verb reconciliation.Verb, request ReleaseRequest) string {
	if verb == reconciliation.Destroy {
		return "release the copy of " + request.Identity.Cluster + "'s release in the image mirror on " + request.Placement.Machine + ", whose Registry's removal deletes it"
	}
	return "copy the release " + request.Release.Version + " of " + request.Identity.Cluster + " into the image mirror on " + request.Placement.Machine
}

func releaseImpacts(verb reconciliation.Verb, request ReleaseRequest) []string {
	if verb == reconciliation.Destroy {
		return []string{}
	}
	return []string{"publish-content " + request.Mirror.Host + "/" + request.Release.Components, "publish-content " + request.Mirror.Host + "/" + request.Release.Repository + ":" + request.Release.Tag}
}

func releaseRefusals(contextName string, request ReleaseRequest) map[string]error {
	cluster := diagnostics.ObjectIdentity{APIVersion: api.APIVersion, Kind: ReleaseKind, Name: request.Identity.Cluster}
	refused := func(stop diagnostics.Stop, message, remediation string) error {
		return refusedOn(&cluster, stop, message, remediation)
	}
	machine, repeat := "Machine/"+request.Placement.Machine, "bootwright apply --context "+contextName
	return map[string]error{
		"client-unreachable": refused(mirrorOriginUnreachable, machine+" could not read the release's oc or its published checksums from "+request.Client.URL+", so the release was not copied",
			"give "+request.Placement.Machine+" a route to the origins bootwright render acquisitions --context "+contextName+" names, then run "+repeat),
		"client-digest-mismatch": refused(mirrorDigestMismatch, "the oc archive "+machine+" read from "+request.Client.URL+" does not match the SHA-256 its publisher lists, so it was not run",
			"verify the publisher and the route from "+request.Placement.Machine+" to it, then run "+repeat),
		"release-unreachable": refused(mirrorOriginUnreachable, machine+" could not copy the release "+request.Release.Version+" from "+request.Release.Repository+" into the image mirror at "+request.Mirror.Host,
			"verify that Secret/"+request.PullSecretRef+" holds a quay.io entry that reads the release, that "+request.Placement.Machine+" reaches quay.io and Registry/"+request.Identity.Registry+" answers its credential, then run "+repeat),
		"release-incomplete": refused(mirrorReleaseIncomplete, "the image mirror at "+request.Mirror.Host+" lacks the release "+request.Release.Version+" or one of its components after the copy, so the cluster would not install from it",
			"verify the free space of Registry/"+request.Identity.Registry+"'s storage on "+request.Placement.Machine+", then run "+repeat),
		"release-digest-mismatch": refused(mirrorDigestMismatch, "the image mirror at "+request.Mirror.Host+" holds the release "+request.Release.Tag+" at another digest than the pinned one, so the fill stopped",
			"verify spec.distribution.release.image on OpenShiftCluster/"+request.Identity.Cluster+" names the release "+request.Release.Version+", then run "+repeat),
		"mirror-unreadable": refused(mirrorUnreadable, machine+" could not read the image mirror at "+request.Mirror.Host+" with its credential and trust anchor",
			"verify Registry/"+request.Identity.Registry+" is ready and answers its credential, then run "+repeat),
	}
}

func (c ReleaseFill) claimedClusters(catalog api.Catalog) []string {
	mirror, declared := infrastructureservices.ImageMirrorOf(catalog)
	if !declared || !mirror.Managed || c.releases == nil {
		return nil
	}
	releases, err := c.releases(catalog)
	if err != nil {
		return nil
	}
	claimed := make([]string, 0, len(releases))
	for _, release := range releases {
		claimed = append(claimed, api.NewObject(api.OpenShiftCluster, release.Cluster, api.Value{}, api.Value{}).Identity())
	}
	return claimed
}
