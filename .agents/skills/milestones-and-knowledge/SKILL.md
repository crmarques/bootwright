---
name: milestones-and-knowledge
description: Record Bootwright delivery state and lessons, such as adding, moving, splitting or closing a milestone item, writing a delivered record or decision, adding a Not yet met line, or adding or retiring a knowledge page.
---

# Milestones And Knowledge

Use [code-implementation](../code-implementation/SKILL.md) for the commit.
[Milestones](../../../specs/milestones.md) owns status, scope rules and
[identifiers](../../../specs/milestones.md#identifiers); the
[knowledge index](../../knowledge/index.md) owns its conventions in its closing
paragraphs. Recording is a tracked edit: in a review-only task, report the
proposed record instead of writing it.

## Read narrowly

Read the Status table, the item's row and its `### B<n>` section. Search
`specs/milestones/delivered.md` and `specs/milestones/backlog.md` by ID; never
read them whole.

## Items

- Number, place, move, split, drop and complete items as the identifiers and
  scope rules say, and fill every column the Items table header names.
- Item detail holds the bounded outcome, a link to the owning spec section,
  exit evidence and any deferral reason. Never restate a spec rule in an item.

## Not yet met

A spec line in the exact form the
[conventions](../../../specs/index.md#conventions) give, linking one open item.
It states what code does not yet do. It never links a delivered record and
never holds acceptance or baseline state; those belong on the milestone page.

## Decisions and delivered records

- A decision is appended only when the owner decides; a clause a session takes
  is marked as pending the owner's review.
- A delivered record names the slice, items, commits, gates with results and
  any limitation; it links the spec clause instead of restating the rule.

## Knowledge pages

One page per durable lesson, as the knowledge index's closing paragraphs say:
the observation, the decision it informs, the applicable implementation or
version, links to code and evidence, and a link to the owning spec. Index it by
searchable symptom, symbol or error text. Retire a page when its basis changes.

## Check

`make docs-check` holds milestone structure, Not yet met links and index links.
