package desiredstate

import (
	"fmt"

	"github.com/crmarques/bootwright/api/v1alpha1"
)

func validateEnvironmentRegistries(env v1alpha1.Environment) []string {
	registries := env.Spec.Registries
	if registries == nil {
		return nil
	}
	var errs []string
	owner := fmt.Sprintf("Environment/%s spec.registries", env.Metadata.Name)
	for _, src := range registries.ImageDigestSources {
		errs = append(errs, validateImageDigestSource(fmt.Sprintf("%s.imageDigestSources[%s]", owner, src.Source), src)...)
	}
	return errs
}

func validateImageDigestSource(owner string, src v1alpha1.ImageDigestSource) []string {
	var errs []string
	if src.Source == "" {
		errs = append(errs, fmt.Sprintf("%s.source is required", owner))
	}
	if len(src.Mirrors) == 0 {
		errs = append(errs, fmt.Sprintf("%s.mirrors is required", owner))
	}
	switch src.SourcePolicy {
	case "", v1alpha1.ImageSourcePolicyNever, v1alpha1.ImageSourcePolicyAllow:
	default:
		errs = append(errs, fmt.Sprintf("%s.sourcePolicy %q must be one of {%s, %s}",
			owner, src.SourcePolicy,
			v1alpha1.ImageSourcePolicyNever, v1alpha1.ImageSourcePolicyAllow))
	}
	return errs
}
