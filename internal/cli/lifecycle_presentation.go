package cli

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/crmarques/bootwright/internal/reconciliation/lifecycle"
)

// NewLifecyclePresenters shares a frozen preview; it never schedules work.
func NewLifecyclePresenters(out io.Writer, columns func() int, jsonMode bool) (*LifecyclePlanPresenter, InvocationProgress) {
	plan := NewLifecyclePlanPresenter(out)
	if jsonMode {
		return plan, silentProgress{}
	}
	progress := NewLifecycleProgressPresenter(out, columns)
	model := &lifecyclePresentation{}
	plan.model, progress.model = model, model
	progress.progress.overview = func() []string { return model.rows(presentationFor(out)) }
	if presentationFor(out).Progress == "never" {
		return plan, quietInvocationProgress{presenter: progress}
	}
	return plan, progress
}

// lifecyclePresentation owns copies read by concurrent events and heartbeat.
type lifecyclePresentation struct {
	mu     sync.Mutex
	verb   string
	steps  []lifecycle.PlanStep
	states map[string]string
}

func (p *lifecyclePresentation) seed(result lifecycle.PlanResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.verb = result.Verb
	p.steps = make([]lifecycle.PlanStep, len(result.Steps))
	p.states = make(map[string]string, len(result.Steps))
	for i, step := range result.Steps {
		p.steps[i] = step
		p.steps[i].After = append([]int(nil), step.After...)
		p.steps[i].Impacts = append([]string(nil), step.Impacts...)
		p.steps[i].Consumes = append([]string(nil), step.Consumes...)
		p.states[step.ID] = step.State
	}
}

func (p *lifecyclePresentation) report(event lifecycle.ProgressEvent) {
	if event.Phase != lifecycle.EffectPhase || event.Group != "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, known := p.states[event.Block]; known {
		p.states[event.Block] = event.Status
	}
}

func (p *lifecyclePresentation) rows(opts PresentationOptions) []string {
	if opts.Format != "" && opts.Format != "text" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.steps) == 0 {
		return nil
	}
	done := 0
	for _, step := range p.steps {
		if p.states[step.ID] == "done" {
			done++
		}
	}
	filled := done * 10 / len(p.steps)
	on, off := "■", "□"
	if opts.ASCII {
		on, off = "#", "-"
	}
	meter := styleAccent(strings.Repeat(on, filled), opts) + styleMuted(strings.Repeat(off, 10-filled), opts)
	rows := []string{displayIndent + meter + fmt.Sprintf(" %d%%", done*100/len(p.steps)) + styleMuted(fmt.Sprintf(" · %d/%d blocks done", done, len(p.steps)), opts)}
	if opts.Detail != "full" {
		return rows
	}
	waiting := 0
	// Keep the live block bounded; the static full plan retains every edge.
	const visibleWaits = 3
	for _, step := range p.steps {
		state := p.states[step.ID]
		if step.Selection == lifecycle.StepNotSelected || (state != "pending" && state != "") {
			continue
		}
		var waits []string
		for _, position := range step.After {
			if position > 0 && position <= len(p.steps) && p.states[p.steps[position-1].ID] != "done" {
				waits = append(waits, fmt.Sprint(position))
			}
		}
		if len(waits) != 0 {
			waiting++
			if waiting <= visibleWaits {
				rows = append(rows, displayIndent+styleMuted(escapeDisplayLine(step.Description)+" · waiting on steps "+strings.Join(waits, ", "), opts))
			}
		}
	}
	if waiting > visibleWaits {
		rows = append(rows, displayIndent+styleMuted(fmt.Sprintf("%d more waiting on dependencies", waiting-visibleWaits), opts))
	}
	return rows
}

// lifecycleSteps preserves frozen order and always discloses impacts and authorizations.
func (d *display) lifecycleSteps(result lifecycle.PlanResult) {
	d.custom(func(r *displayRenderer) {
		previousStage := ""
		for index, planned := range result.Steps {
			if planned.Stage != previousStage {
				if index != 0 {
					r.gap()
				}
				count := 1
				for next := index + 1; next < len(result.Steps) && result.Steps[next].Stage == planned.Stage; next++ {
					count++
				}
				r.line(displayIndent + styleAccent(escapeDisplayLine(lifecycleStageTitle(planned.Stage)), r.options) + styleMuted(fmt.Sprintf(" · %d %s", count, plural(count, "block", "blocks")), r.options))
				previousStage = planned.Stage
			}
			marker := "○"
			if r.options.ASCII {
				marker = "o"
			}
			state := planned.State
			if state == "" {
				state = "pending"
			}
			detail := state
			if selected := selectionMarker(planned); selected != "" {
				detail += " · " + selected
			}
			if len(planned.Consumes) != 0 {
				detail += " · " + strings.Join(planned.Consumes, ", ")
			}
			if r.options.Detail == "full" {
				if waiting := waitMarker(planned); waiting != "" {
					detail += " · " + waiting
				}
			}
			action := lifecycleActionLabel(planned.Description, planned.Object, r.options)
			r.styledParagraph(action+styleMuted(" · "+escapeDisplayLine(detail), r.options), fmt.Sprintf("    %s %d. ", marker, index+1), "plain")
			for _, impact := range planned.Impacts {
				r.paragraph(impact, "      ", "muted")
			}
		}
	})
}

func selectedProgressColumns(out io.Writer, columns func() int) func() int {
	opts := presentationFor(out)
	if opts.Progress == "never" || opts.Dumb {
		return nil
	}
	if columns == nil && opts.Progress != "always" {
		return nil
	}
	return func() int {
		if columns != nil {
			if width := columns(); width > 0 {
				if opts.Width > 0 {
					return min(width, opts.Width)
				}
				return width
			}
		}
		if opts.Progress == "always" {
			return opts.columns() + progressWrapGuard
		}
		return 0
	}

}

// Keep log locations available even when an operator suppresses progress.
type quietInvocationProgress struct{ presenter *LifecycleProgressPresenter }

func (quietInvocationProgress) ReportProgress(context.Context, lifecycle.ProgressEvent) {}
func (p quietInvocationProgress) ReportLogLocation(ctx context.Context, location string) {
	p.presenter.ReportLogLocation(ctx, location)
}
func (p quietInvocationProgress) Finish() { p.presenter.Finish() }

func (p *lifecyclePresentation) action(id string) (string, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, step := range p.steps {
		if step.ID == id {
			return step.Description, step.Object
		}
	}
	return "", ""
}

// lifecycleOutcomes retains durable order regardless of callback order.
func (d *display) lifecycleOutcomes(blocks []lifecycle.BlockResult) {
	d.custom(func(r *displayRenderer) {
		previousStage := ""
		for _, block := range blocks {
			if block.Stage != previousStage {
				r.line(displayIndent + styleAccent(escapeDisplayLine(lifecycleStageTitle(block.Stage)), r.options))
				previousStage = block.Stage
			}
			r.styledParagraph(escapeDisplayLine(block.Description), "    "+styleState(blockStatusToken(block.State), r.options)+" ", "plain")
		}
	})
}

func lifecycleStageTitle(stage string) string {
	switch stage {
	case "controller":
		return "Controller"
	case "infrastructure-services":
		return "Infrastructure"
	case "substrates":
		return "Substrates"
	case "machines":
		return "Machines"
	case "add-ons":
		return "Add-ons"
	case "clusters":
		return "Clusters"
	}
	return stage
}
