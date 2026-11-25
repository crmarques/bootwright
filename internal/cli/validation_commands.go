package cli

import (
	"strconv"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/crmarques/bootwright/internal/managedos/media"
	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
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
	if command.Flags().Changed("name") && !dnsLabel(stringValue(command.Flags(), "name")) {
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

func missingContextUpdateInput(flags *pflag.FlagSet) bool {
	return !hasFiles(flags) && !hasInputDir(flags)
}

func validateContextDelete(command *cobra.Command) string {
	if command.Flags().Changed("purge") && !boolValue(command.Flags(), "purge") {
		return "context delete requires --purge=true"
	}
	return ""
}

func validateUninstall(command *cobra.Command) string {
	return validateUninstallFlags(command.Flags())
}

func validateLifecycleFlags(command *cobra.Command) string {
	flags := command.Flags()
	if flags.Changed("clusters") && flags.Changed("machines") {
		return "--clusters and --machines are mutually exclusive"
	}
	for _, flag := range []string{"clusters", "machines"} {
		if !flags.Changed(flag) {
			continue
		}
		names := normalizeNames(stringValue(flags, flag))
		if len(names) == 0 {
			return "--" + flag + " must name at least one target"
		}
		for _, name := range names {
			if !api.ValidLexical(api.LexicalName, name) {
				return "--" + flag + " names must be lowercase DNS labels"
			}
		}
	}
	if dryRunSelected(command.Flags()) && len(arrayValue(command.Flags(), "authorize")) != 0 {
		return "--authorize is used when executing apply"
	}
	return validateAuthorizations(command, false)
}

func validateDestroyFlags(command *cobra.Command) string {
	if command.Flags().Changed("stage") {
		tokens, _ := authorizationTokens(command)
		for _, token := range tokens {
			if strings.HasPrefix(token, "abandon=") {
				return "--authorize abandon=<machine> leaves steps in place, which a staged destroy never does; repeat without one of them"
			}
		}
	}
	if dryRunSelected(command.Flags()) {
		if problem := validatePlanFlags(command); problem != "" {
			return problem
		}
	}
	return validateAuthorizations(command, true)
}

func validatePlanFlags(command *cobra.Command) string {
	tokens, refusal := authorizationTokens(command)
	if refusal != "" {
		return refusal
	}
	for _, token := range tokens {
		if !lifecycle.IsRelaxationAuthorization(token) {
			return "destroy --dry-run accepts only --authorize erase=<machine>:metadataClear, erase=<machine>:retain or abandon=<machine>; data-loss and rebind=<secret> are consumed by apply and destroy"
		}
	}
	_, err := lifecycle.ParseRelaxations(tokens)
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
		if lifecycle.IsRelaxationAuthorization(token) {
			relaxing = append(relaxing, token)
		} else {
			others = append(others, token)
		}
	}
	if len(relaxing) != 0 && !erase {
		return "--authorize erase=<machine>:<method> and abandon=<machine> relax a removal and apply only to destroy and destroy --dry-run, not " + command.Name()
	}
	if _, err := lifecycle.ParseRelaxations(relaxing); err != nil {
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
	if command.Flags().Changed("name") && !media.ValidName(stringValue(command.Flags(), "name")) {
		return "--name must be a portable installer-image basename ending in .iso"
	}
	return ""
}

func missingMediaSource(flags *pflag.FlagSet) bool {
	return !flags.Changed("from-file") && !flags.Changed("from-url") && !flags.Changed("from-url-file")
}

func validateMediaAdd(command *cobra.Command) string {
	return firstRefusal(validateMediaName, func(command *cobra.Command) string { return validateMediaSource(command.Flags()) })(command)
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

func validateEjectConfirmation(command *cobra.Command) string {
	if selectedJSON(command) && !boolValue(command.Flags(), "yes") {
		return structuredLabel(command) + " media ejection requires --yes"
	}
	return ""
}

func validateMachineTrustCommand(command *cobra.Command) string {
	return validateMachineTrust(command, command.Flags())
}

func validateMachineNames(command *cobra.Command) string {
	flags := command.Flags()
	names := normalizeNames(stringValue(flags, "name"))
	if flags.Changed("name") && len(names) == 0 {
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
