package cli

import (
	"slices"
	"strconv"
	"strings"

	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/spf13/cobra"
)

type commandValidator func(*cobra.Command) string

func firstRefusal(validators ...commandValidator) commandValidator {
	return func(command *cobra.Command) string {
		for _, validate := range validators {
			if message := validate(command); message != "" {
				return message
			}
		}
		return ""
	}
}

func validateContextName(command *cobra.Command) string {
	if command.Flags().Lookup("name") != nil && !dnsLabel(stringValue(command.Flags(), "name")) {
		return "--name must be a lowercase DNS label"
	}
	return ""
}

func validateContextFile(command *cobra.Command) string {
	if len(arrayValue(command.Flags(), "file")) > 1 {
		return "at most one --file occurrence is allowed"
	}
	return ""
}

func validateContextUpdate(command *cobra.Command) string {
	flags := command.Flags()
	if len(arrayValue(flags, "file")) == 0 && stringValue(flags, "input-dir") == "" {
		return "context update requires --file or --input-dir"
	}
	return ""
}

func validateContextDelete(command *cobra.Command) string {
	if !boolValue(command.Flags(), "purge") {
		return "context delete requires --purge=true"
	}
	return ""
}

func validateUninstall(command *cobra.Command) string {
	return validateUninstallFlags(command.Flags())
}

func validateLifecycleFlags(command *cobra.Command) string {
	return validateAuthorizations(command, false)
}

func validateDestroyFlags(command *cobra.Command) string {
	return validateAuthorizations(command, true)
}

func validatePlanFlags(command *cobra.Command) string {
	tokens, refusal := authorizationTokens(command)
	if refusal != "" {
		return refusal
	}
	for _, token := range tokens {
		if !lifecycle.IsEraseAuthorization(token) {
			return "plan accepts only --authorize erase=<machine>:metadataClear or erase=<machine>:retain; data-loss and rebind=<secret> are consumed by apply and destroy"
		}
	}
	_, err := lifecycle.ParseEraseRelaxations(tokens)
	return refusalText(err)
}

func validateAuthorizations(command *cobra.Command, erase bool) string {
	flags := command.Flags()
	parallelism, err := strconv.Atoi(stringValue(flags, "parallelism"))
	if err != nil || parallelism < 1 {
		return "--parallelism must be a positive integer"
	}
	tokens, refusal := authorizationTokens(command)
	if refusal != "" {
		return refusal
	}
	var relaxing, others []string
	for _, token := range tokens {
		if lifecycle.IsEraseAuthorization(token) {
			relaxing = append(relaxing, token)
		} else {
			others = append(others, token)
		}
	}
	if len(relaxing) != 0 && !erase {
		return "--authorize erase=<machine>:<method> relaxes a removal and applies only to destroy and plan, not " + command.Name()
	}
	if _, err := lifecycle.ParseEraseRelaxations(relaxing); err != nil {
		return err.Error()
	}
	if !lifecycle.ValidAuthorizations(others) {
		return "--authorize accepts data-loss or data-loss=<digest> and rebind=<secret>, each at most once"
	}
	for _, occurrence := range arrayValue(flags, "unregistered") {
		for _, name := range strings.Split(occurrence, ",") {
			if strings.TrimSpace(name) == "" {
				return "--unregistered must not contain an empty member"
			}
		}
	}
	return ""
}

func authorizationTokens(command *cobra.Command) ([]string, string) {
	var tokens []string
	for _, occurrence := range arrayValue(command.Flags(), "authorize") {
		for _, token := range strings.Split(occurrence, ",") {
			if token = strings.TrimSpace(token); token == "" {
				return nil, "--authorize must not contain an empty member"
			}
			tokens = append(tokens, token)
		}
	}
	return tokens, ""
}

func refusalText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func validateSecretSet(command *cobra.Command) string {
	return validateSecretInput(command.Flags())
}

func validateMediaName(command *cobra.Command) string {
	if !media.ValidName(stringValue(command.Flags(), "name")) {
		return "--name must be a portable installer-image basename ending in .iso"
	}
	return ""
}

func validateMediaAdd(command *cobra.Command) string {
	return firstRefusal(validateMediaName, func(command *cobra.Command) string { return validateMediaSource(command.Flags()) })(command)
}

func validatePreflight(command *cobra.Command) string {
	flags := command.Flags()
	if stages := normalizeNames(stringValue(flags, "stage")); flags.Changed("stage") && len(stages) == 0 {
		return "--stage must name at least one stage"
	} else if len(stages) != 0 && !slices.Contains(stages, "clusters") && stringValue(flags, "clusters") != "" {
		return "--clusters requires the clusters stage"
	}
	return ""
}

func validateMachineList(command *cobra.Command) string {
	flags := command.Flags()
	if boolValue(flags, "short") && selectedJSON(command) {
		return "--short conflicts with " + structuredLabel(command) + " output"
	}
	if boolValue(flags, "short") && boolValue(flags, "power-status") {
		return "--short conflicts with --power-status"
	}
	return ""
}

func validatePowerConfirmation(command *cobra.Command) string {
	if selectedJSON(command) && !boolValue(command.Flags(), "yes") {
		return structuredLabel(command) + " power changes require --yes"
	}
	return ""
}

func validateMachineTrustCommand(command *cobra.Command) string {
	return validateMachineTrust(command, command.Flags())
}

func validateMachineNames(command *cobra.Command) string {
	flags := command.Flags()
	names := normalizeNames(stringValue(flags, "name"))
	if len(names) == 0 {
		return "--name must name at least one machine"
	}
	normalizeScalar(flags, "name", strings.Join(names, ","))
	return ""
}

func validateMachineName(command *cobra.Command) string {
	if strings.Contains(stringValue(command.Flags(), "name"), ",") {
		return "machine rsh opens one interactive session; name one machine"
	}
	return ""
}

func validateInfoNames(noun string) commandValidator {
	return func(command *cobra.Command) string {
		flags := command.Flags()
		if !flags.Changed("name") {
			return ""
		}
		names := normalizeNames(stringValue(flags, "name"))
		if len(names) == 0 {
			return "--name must name at least one " + noun
		}
		normalizeScalar(flags, "name", strings.Join(names, ","))
		return ""
	}
}

func validateRenderAcquisitions(command *cobra.Command) string {
	flags := command.Flags()
	if stringValue(flags, "input-dir") != "" && flags.Changed("context") {
		return "--input-dir conflicts with --context"
	}
	return ""
}

func validateRenderEffective(command *cobra.Command) string {
	flags := command.Flags()
	if boolValue(flags, "origins") && stringValue(flags, "output-dir") != "" {
		return "--origins conflicts with --output-dir"
	}
	return validateRenderDirectories(flags)
}
