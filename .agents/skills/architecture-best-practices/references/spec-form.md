# Spec Form

The shape of new and touched spec text. The
[conventions](../../../../specs/index.md#conventions) own placement and the
Not yet met form; this page owns how a section and a rule read, so that a
coding agent can implement from one reading and a reviewer can test it.
It is guidance for writers and reviewers; rewriting text that a change does
not touch waits for its own slice.

## A section

- Its heading names what the section decides, as a noun phrase.
- It opens with one sentence of scope: the actor, the trigger, and what it does
  not cover, with a link to the section that does.
- An effect section says what happens on failure, timeout, cancellation, a
  crash between two steps and an unknown outcome, and what a continuation runs
  again; or it links the engine rule it follows and states only its own proofs.
- Refusals are rows of code, condition and remedy, or rows of the page's
  Refusal table.
- An effect section closes with two lines: `Code:` naming the package or the
  symbol, and `Held by:` naming the tests a reviewer has read.
- A heading that several sections of one page share gets a qualifier when
  something links to it.

## A rule

- One rule per sentence or list item, in the present tense.
- An explicit actor and an observable outcome: the refusal code, the state, the
  record or the output.
- A closed quantifier ("every", "only", "never") holds for the whole page; when
  a later case breaks it, scope the sentence in the same edit.
- An enumeration is a list; a case analysis is a decision table; a protocol is
  numbered steps.
- A bound names its value and its Go constant.
- No pronoun reaches back across sentences; no "see above".
- No history, date, slice id or "currently" in a rule; availability only in the
  Not yet met form.
- Rationale is one closing `Why:` sentence, or lives in the decision. A decision
  id appears only in a closing `Basis:` line, an item id only in a Not yet met
  paragraph.
- A rule another page owns is linked, never restated. A table that the code
  also holds is held by a test, or replaced by a link to the code.

## Models in the corpus

- [Lock or lease refusal](../../../../specs/cli.md#lifecycle-behavior): one
  refusal code, fail-closed, with the safe retry named.
- [Managed Network bound](../../../../specs/api/networks.md): an exact count
  rule, a refusal code and place, and the test that holds it.
- [SSH ingress forms](../../../../specs/api/machines.md): a closed table of
  forms with every refused case.
- [Kubeconfig export](../../../../specs/cli.md#resource-inspection-and-explicit-access):
  an ordered table of checks with code, exit and next action.
- [Controller refusal table](../../../../specs/api/environment.md): path,
  reason and remedy per row, held by `TestControllerRefusalTableMatchesUnsupported`.
