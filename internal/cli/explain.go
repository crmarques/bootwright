package cli

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/crmarques/bootwright/internal/diagnostics"
)

type explainedStop struct {
	Code      string `json:"code"`
	Level     string `json:"level"`
	Certainty string `json:"certainty"`
	Cause     string `json:"cause"`
	Fault     string `json:"fault,omitempty"`
	Recovery  string `json:"recovery"`
	Repeat    bool   `json:"repeatHelps"`
	Meaning   string `json:"meaning"`
	Remedy    string `json:"remedy"`
}

type explainPresentation struct {
	Stops []explainedStop `json:"stops"`
}

func (explainPresentation) documentedResult() {}

func explained(stop diagnostics.Stop) explainedStop {
	return explainedStop{Code: stop.Code, Level: string(stop.Level), Certainty: string(stop.Certainty), Cause: string(stop.Cause), Fault: string(stop.Fault),
		Recovery: string(stop.Recovery), Repeat: stop.Recovery == diagnostics.RecoveryRepeat, Meaning: stop.Meaning, Remedy: stop.Remedy}
}

func presentExplain(r *Runner, _, command *cobra.Command, path string) int {
	var stops []diagnostics.Stop
	if r.config.Stops != nil {
		stops = r.config.Stops()
	}
	slices.SortFunc(stops, func(a, b diagnostics.Stop) int { return strings.Compare(a.Code, b.Code) })
	code, _ := command.Flags().GetString("code")
	shown := explainPresentation{Stops: []explainedStop{}}
	for _, stop := range stops {
		if code == "" || stop.Code == code {
			shown.Stops = append(shown.Stops, explained(stop))
		}
	}
	if code != "" && len(shown.Stops) == 0 {
		return r.failure(command, path, "cli.usage", "no stop is named "+escapeDisplayLine(code)+"; bootwright explain lists every stop", 2, selectedStructured(command))
	}
	if structuredFormat(r.config.Presentation.Format) {
		if writeStructuredResult(r.config.Out, path, shown, nil, nil, 0) != nil {
			return 1
		}
		return 0
	}
	if writeExplain(r, shown, code != "") != nil {
		return 1
	}
	return 0
}

func writeExplain(r *Runner, shown explainPresentation, one bool) error {
	var text display
	if !one {
		rows := make([][]string, 0, len(shown.Stops))
		for _, stop := range shown.Stops {
			rows = append(rows, []string{stop.Code, stop.Certainty, stop.Recovery, stop.Meaning})
		}
		text.resourceTable([]string{"CODE", "CERTAINTY", "RECOVERY", "MEANING"}, rows, tableStyle{Kind: "stop", Identity: 0})
		return text.writeTo(r.config.Out)
	}
	stop := shown.Stops[0]
	text.headline("", stop.Code)
	cause := stop.Cause
	if stop.Fault != "" {
		cause += " (" + stop.Fault + ")"
	}
	repeat := "no"
	if stop.Repeat {
		repeat = "yes"
	}
	text.section("")
	text.fields(field{Label: "Meaning", Value: stop.Meaning}, field{Label: "Level", Value: stop.Level}, field{Label: "Certainty", Value: stop.Certainty},
		field{Label: "Cause", Value: cause}, field{Label: "Recovery", Value: stop.Recovery}, field{Label: "Repeat helps", Value: repeat},
		field{Label: "Remedy", Value: stop.Remedy})
	return text.writeTo(r.config.Out)
}
