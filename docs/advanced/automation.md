---
title: Automation and CI
description: Wiring Bootwright into pipelines — the exit-code contract, which verbs emit JSON and what the JSON describes, and the flag set that guarantees an unattended run never prompts.
---

# Automation and CI

This page owns wiring Bootwright into pipelines: the exit-code contract, which
verbs emit machine-readable JSON, and the flag set that guarantees a run never
prompts. The
[CLI Contract in specs/state-model.md](https://github.com/crmarques/bootwright/blob/main/specs/state-model.md#cli-contract)
stays normative; this page renders it for pipeline authors.

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Success. |
| `1` | A load or run failure, or a fail-closed refusal. |
| `2` | A usage error — an unknown flag, an unknown `--authorize` token, or a bad flag combination. |
| `3` | `diff` only — the selected state is not proven in sync: drift, foreign ownership, a degraded probe, or never applied. |

The passthrough verbs — `container-cluster oc`/`kubectl`, `machine rsh`/`exec`,
and `cluster rsh`/`exec` — propagate the wrapped command's exit status verbatim
and are outside this contract.

## JSON output

Verbs that accept `--output json`, and what the JSON describes. On the mutating
verbs JSON is a **preview-only** format: `apply`, `destroy`, and the scoped
`preflight` targets accept it only together with `--dry-run` (exit `2`
otherwise).

| Verb(s) | Requires `--dry-run` | The JSON describes |
| --- | --- | --- |
| `validate` | no | The validation census: object counts, `diagnostics[]`, and advisories. |
| `status` | no | Context standing and the `nextSteps` spine. |
| `diff` / `diff --recorded` | no | The comparison report (exit `3` is still the sync signal). |
| `machine list`, `cluster list`, `cluster info`, `machine trust`, `secret list`, `secret check`, `secret encryption status`, `add-ons list`, `media list` | no | The listed inventory or check result of that verb. |
| `render`, `render effective`, `render installer`, `render storage` | no | The render report (for `render --output-dir`, the manifest of exported files). |
| `preflight infra\|clusters\|container-cluster\|storage-cluster\|all` | **yes** | The planned preflight command graph — **not** check results. Gate CI on the live preflight's exit code instead. |
| `plan` | always a preview | The planned task graph. |
| `apply`, `destroy` | **yes** | The plan — a real mutating run has no JSON mode. |

Two preflight targets differ: `preflight add-ons` has no `--dry-run` and its
`--output json` emits machine-readable pass/fail results from the live run, and
`preflight bastion` accepts neither flag.

When JSON was requested, errors are also emitted as a JSON report with `ok`,
`exitCode`, `error`, and `diagnostics[]`, so a pipeline never has to parse
human text to learn why a command failed.

## Running unattended

The flag set that guarantees no prompt:

- **`--yes`** answers the ordinary confirmation prompt and nothing else. It
  never authorizes a named risk — every `--authorize` token a run needs must be
  listed deliberately, per run (see
  [the two axes](operations.md#the-two-axes-intent-and-authorization)).
- **Pre-record host trust with `bootwright machine trust`.** Non-interactive
  runs (`--yes`, `--output json`) never record trust on first use: an unknown
  host key under automation is a hard failure, never auto-accepted. Interactive
  TOFU is the alternative outside pipelines, and only
  `machine trust --replace <machine>` may accept a changed key.
- **Become-password handling.** Run as root, or ensure passwordless sudo
  (non-interactive sudo is auto-detected before any prompt), or pass
  `--ask-become-pass=false` explicitly — the flag defaults to `false` as root
  and `true` otherwise.
- **`--output json`** where supported, for machine-readable results.

Gate a pipeline on convergence with `diff`: `diff --recorded` is the fast
offline check against the last recorded apply, `diff` (live) also catches
out-of-band changes, and exit `3` is the drift signal in both modes:

```bash
bootwright diff --recorded --output json > diff-report.json
case $? in
  0) echo "in sync" ;;
  3) echo "out of sync — see diff-report.json"; exit 1 ;;
  *) echo "diff failed"; exit 1 ;;
esac
```
