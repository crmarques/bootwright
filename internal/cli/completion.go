package cli

import (
	"fmt"
	"slices"
	"strings"

	api "github.com/crmarques/bootwright/api/v1alpha1"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	completionRequest               = "__bootwright_complete"
	completionRequestNoDescriptions = "__bootwright_complete_no_desc"
	secretEncryptionTypeCatalog     = "secret-encryption-types"
)

type completionCatalog struct {
	secretEncryptionTypes func() []string
}

func configureCompletion(root *cobra.Command, catalog completionCatalog) error {
	for _, name := range []string{completionRequest, completionRequestNoDescriptions} {
		root.AddCommand(&cobra.Command{
			Use: name, Hidden: true, DisableFlagParsing: true,
			RunE: func(cmd *cobra.Command, args []string) error {
				for _, candidate := range completionCandidatesWithCatalog(root, args, cmd.Name() == completionRequest, catalog) {
					if _, err := fmt.Fprintln(cmd.OutOrStdout(), candidate); err != nil {
						return err
					}
				}
				_, err := fmt.Fprintf(cmd.OutOrStdout(), ":%d\n", cobra.ShellCompDirectiveNoFileComp|cobra.ShellCompDirectiveKeepOrder)
				return err
			},
		})
	}
	return nil
}

func completionCandidates(root *cobra.Command, words []string, descriptions bool) []string {
	return completionCandidatesWithCatalog(root, words, descriptions, completionCatalog{})
}

func completionCandidatesWithCatalog(root *cobra.Command, words []string, descriptions bool, catalog completionCatalog) []string {
	if len(words) == 0 {
		return nil
	}
	completeWords, incompleteWord := words[:len(words)-1], words[len(words)-1]
	cmd := root
	helpMode := false
	localFlagUsed := false
	var awaiting *pflag.Flag
	for _, token := range completeWords {
		if awaiting != nil {
			awaiting = nil
			continue
		}
		if token == "--" {
			return nil
		}
		if strings.HasPrefix(token, "-") {
			flagCommand := cmd
			if helpMode {
				flagCommand = root
			}
			flag, _, attached := completionFlag(flagCommand, token)
			if flag == nil {
				return nil
			}
			if cmd.LocalNonPersistentFlags().Lookup(flag.Name) != nil && !helpMode {
				localFlagUsed = true
			}
			if flag.NoOptDefVal == "" && !attached {
				awaiting = flag
			}
			continue
		}
		if cmd == root && token == "help" && !helpMode {
			helpMode = true
			continue
		}
		if localFlagUsed {
			return nil
		}
		var next *cobra.Command
		for _, child := range cmd.Commands() {
			if !child.Hidden && child.Name() == token {
				next = child
				break
			}
		}
		if next == nil {
			return nil
		}
		cmd = next
	}

	if awaiting != nil {
		return completionValues(awaiting, incompleteWord, "", catalog)
	}
	if strings.HasPrefix(incompleteWord, "-") {
		flagCommand := cmd
		if helpMode {
			flagCommand = root
		}
		if flag, value, attached := completionFlag(flagCommand, incompleteWord); flag != nil && attached {
			return completionValues(flag, value, strings.TrimSuffix(incompleteWord, value), catalog)
		}
		var candidates []string
		for _, flag := range completionFlags(flagCommand) {
			for _, name := range []string{"--" + flag.Name, "-" + flag.Shorthand} {
				if name != "-" && strings.HasPrefix(name, incompleteWord) {
					candidates = append(candidates, completionDescription(name, flag.Usage, descriptions))
				}
			}
		}
		slices.Sort(candidates)
		return candidates
	}
	if localFlagUsed {
		return nil
	}
	var candidates []string
	for _, child := range cmd.Commands() {
		if !child.Hidden && strings.HasPrefix(child.Name(), incompleteWord) {
			candidates = append(candidates, completionDescription(child.Name(), child.Short, descriptions))
		}
	}
	slices.Sort(candidates)
	return candidates
}

func completionFlags(cmd *cobra.Command) []*pflag.Flag {
	flags := make(map[string]*pflag.Flag)
	collect := func(flag *pflag.Flag) {
		if !flag.Hidden && flag.Deprecated == "" {
			flags[flag.Name] = flag
		}
	}
	cmd.LocalNonPersistentFlags().VisitAll(collect)
	for parent := cmd; parent != nil; parent = parent.Parent() {
		parent.PersistentFlags().VisitAll(collect)
	}
	result := make([]*pflag.Flag, 0, len(flags))
	for _, flag := range flags {
		result = append(result, flag)
	}
	return result
}

func completionFlag(cmd *cobra.Command, token string) (*pflag.Flag, string, bool) {
	for _, flag := range completionFlags(cmd) {
		long := "--" + flag.Name
		if token == long || (flag.Shorthand != "" && token == "-"+flag.Shorthand) {
			return flag, "", false
		}
		if value, ok := strings.CutPrefix(token, long+"="); ok {
			return flag, value, true
		}
		if flag.Shorthand != "" {
			if value, ok := strings.CutPrefix(token, "-"+flag.Shorthand+"="); ok {
				return flag, value, true
			}
			if value, ok := strings.CutPrefix(token, "-"+flag.Shorthand); ok && value != "" && flag.NoOptDefVal == "" {
				return flag, value, true
			}
		}
	}
	return nil, "", false
}

func completionValues(flag *pflag.Flag, prefix, attached string, catalog completionCatalog) []string {
	values := flag.Annotations["bootwright.enum"]
	dynamic := false
	if names := flag.Annotations["bootwright.catalog"]; len(names) == 1 && names[0] == secretEncryptionTypeCatalog && catalog.secretEncryptionTypes != nil {
		values = catalog.secretEncryptionTypes()
		dynamic = true
	}
	if flag.Value.Type() == "bool" {
		values = []string{"0", "1", "F", "FALSE", "False", "T", "TRUE", "True", "f", "false", "t", "true"}
	}
	var candidates []string
	for _, value := range values {
		if strings.HasPrefix(value, prefix) && (!dynamic || api.ValidLexical("name", value)) {
			candidates = append(candidates, attached+value)
		}
	}
	slices.Sort(candidates)
	return slices.Compact(candidates)
}

func completionDescription(candidate, description string, enabled bool) string {
	if !enabled || description == "" {
		return candidate
	}
	return candidate + "\t" + strings.Join(strings.Fields(description), " ")
}
