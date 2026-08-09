package cli

import (
	"io"

	"github.com/crmarques/bootwright/internal/cli/output"
	"github.com/crmarques/bootwright/internal/storage/arbiter"
)

type replaceArbiterInputRewrite struct {
	Path     string `json:"path"`
	FromNode string `json:"fromNode"`
	ToNode   string `json:"toNode"`
	Machine  string `json:"machine"`
	Site     string `json:"site"`
}

type replaceArbiterDryRunReport struct {
	Context                string                      `json:"context"`
	Target                 string                      `json:"target"`
	Action                 string                      `json:"action"`
	DryRun                 bool                        `json:"dryRun"`
	PlanOnly               bool                        `json:"planOnly"`
	Plan                   arbiter.Plan                `json:"plan"`
	InputRewrite           *replaceArbiterInputRewrite `json:"inputRewrite"`
	Order                  []string                    `json:"order"`
	RequiredAuthorizations []requiredAuthorization     `json:"requiredAuthorizations"`
}

func writeReplaceArbiterDryRunJSON(stdout io.Writer, contextName string, plan arbiter.Plan, promotion arbiter.Promotion, required []requiredAuthorization) error {
	var rewrite *replaceArbiterInputRewrite
	if !promotion.Empty() {
		rewrite = &replaceArbiterInputRewrite{
			Path:     promotion.RelPath,
			FromNode: promotion.FromNode,
			ToNode:   promotion.ToNode,
			Machine:  promotion.Machine,
			Site:     promotion.Site,
		}
	}
	order := []string{}
	if !plan.Settled {
		order = []string{
			"prepare and install Machine/" + plan.DesiredMachine + ", then Ceph on it",
			"deploy mon." + plan.DesiredMon + " with its stretch location and wait for quorum",
			"move the stretch tiebreaker to mon." + plan.DesiredMon,
			arbiterRetirementStep(plan),
		}
	}
	return output.JSON(stdout, replaceArbiterDryRunReport{
		Context:                contextName,
		Target:                 plan.Cluster,
		Action:                 "storage-cluster replace-arbiter",
		DryRun:                 true,
		PlanOnly:               true,
		Plan:                   plan,
		InputRewrite:           rewrite,
		Order:                  order,
		RequiredAuthorizations: emptyWhenNil(required),
	})
}
