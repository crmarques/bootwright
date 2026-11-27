package cli

import (
	"context"
	"io"
	"strings"

	"github.com/crmarques/bootwright/internal/workspace/catalogpurge"
	"github.com/spf13/cobra"
)

type CatalogPurgeService interface {
	Purge(context.Context, catalogpurge.Request) (catalogpurge.Result, error)
}

func catalogPurgeCommands() []commandSpec {
	commands := make([]commandSpec, 0, 2)
	for _, kind := range []catalogpurge.Kind{catalogpurge.Artifact, catalogpurge.Media} {
		commands = append(commands, available(commandSpec{
			path: "catalog " + string(kind) + " purge", contextFree: true, example: "--dry-run",
			short:   "Purge unused " + string(kind) + " catalog entries",
			long:    "Purge entries from this controller's catalog. By default, entries named in any saved context's input or reserved by an operation are kept. --force also deletes referenced and reserved entries, which can prevent later apply, retry or destroy until the same data is restored. It does not bypass active operations or store integrity checks. --dry-run only shows what would be deleted. Otherwise confirm the plan or use --yes.",
			flags:   []flagSpec{boolFlag("force", "Include referenced and reserved entries"), boolFlag("dry-run", "Show the purge plan without deleting anything"), confirmationFlag()},
			results: []jsonResult{catalogPurgePresentation{}}, render: renderCatalogPurge(), validate: validateCatalogPurge,
			invoke: func(s Services, ctx context.Context, values *requestValues, _ []string) (commandResult, error) {
				if s.CatalogPurge == nil {
					return commandResult{}, errMissingService
				}
				result, err := s.CatalogPurge.Purge(ctx, catalogpurge.Request{
					Kind: kind, Force: values.boolean("force"), DryRun: values.boolean("dry-run"), Yes: values.boolean("yes"),
				})
				if err != nil {
					return commandResult{}, err
				}
				return commandResult{artifact: presentCatalogPurge(result)}, nil
			},
		}))
	}
	return commands
}

func validateCatalogPurge(command *cobra.Command) string {
	if selectedStructured(command) && !boolValue(command.Flags(), "yes") && !boolValue(command.Flags(), "dry-run") {
		return structuredLabel(command) + " catalog purge requires --dry-run or --yes"
	}
	return ""
}

type catalogPurgePresentation struct {
	Catalog string            `json:"catalog"`
	Force   bool              `json:"force"`
	DryRun  bool              `json:"dryRun"`
	Entries []catalogPurgeRow `json:"entries"`
}

func (catalogPurgePresentation) documentedResult() {}

type catalogPurgeRow struct {
	Name         string   `json:"name"`
	Action       string   `json:"action"`
	ReferencedBy []string `json:"referencedBy"`
	ReservedBy   []string `json:"reservedBy"`
}

func presentCatalogPurge(result catalogpurge.Result) catalogPurgePresentation {
	plan := result.Plan
	report := catalogPurgePresentation{Catalog: string(plan.Kind), Force: plan.Force, DryRun: result.DryRun, Entries: make([]catalogPurgeRow, 0, len(plan.Entries))}
	for _, entry := range plan.Entries {
		action := "retained"
		if entry.Selected(plan.Force) {
			action = "deleted"
			if result.DryRun {
				action = "would delete"
			}
		}
		report.Entries = append(report.Entries, catalogPurgeRow{Name: entry.Name, Action: action,
			ReferencedBy: append([]string{}, entry.ReferencedBy...), ReservedBy: append([]string{}, entry.ReservedBy...)})
	}
	return report
}

func writeCatalogPurge(out io.Writer, result catalogPurgePresentation) error {
	var text display
	if result.DryRun {
		text.headline("", "Catalog purge plan")
	} else {
		text.headline("OK", "Catalog purge complete")
	}
	text.fields(field{Label: "Catalog", Value: result.Catalog})
	if result.Force {
		text.lines([]string{"Force includes referenced and reserved entries. Affected contexts may need the same data restored."})
	}
	if len(result.Entries) == 0 {
		text.lines([]string{"No registered entries."})
	}
	for _, row := range result.Entries {
		text.section(row.Name)
		fields := []field{{Label: "Action", Value: row.Action}}
		if len(row.ReferencedBy) != 0 {
			fields = append(fields, field{Label: "Referenced by", Value: strings.Join(row.ReferencedBy, ", ")})
		}
		if len(row.ReservedBy) != 0 {
			fields = append(fields, field{Label: "Reserved by", Value: strings.Join(row.ReservedBy, ", ")})
		}
		text.fields(fields...)
	}
	return text.writeTo(out)
}

func NewCatalogPurgePresenter(out io.Writer) func(context.Context, catalogpurge.Plan) error {
	return func(ctx context.Context, plan catalogpurge.Plan) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if out == nil {
			return catalogpurge.Failure(plan.Kind, "the catalog purge preview cannot be shown", "inspect the plan with --dry-run before using --yes")
		}
		return writeCatalogPurge(out, presentCatalogPurge(catalogpurge.Result{Plan: plan, DryRun: true}))
	}
}

func renderCatalogPurge() commandRenderer {
	return commandRenderer{
		structured: structuredShown(func(result commandResult) (jsonResult, []diagnostic, bool) {
			report, ok := result.artifact.(catalogPurgePresentation)
			return report, nil, ok
		}),
		human: func(r *Runner, call renderCall) (bool, error) {
			report, ok := call.result.artifact.(catalogPurgePresentation)
			if !ok {
				return false, nil
			}
			return true, writeCatalogPurge(r.config.Out, report)
		},
	}
}
