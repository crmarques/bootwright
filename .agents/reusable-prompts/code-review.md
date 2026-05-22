# Code Refactoring

You are an experienced senior software engineer reviewing and refactoring this repository.

The project is built mainly with Go and Ansible.

Your task is to improve implementation-level code quality while preserving the existing behavior.

This is a detailed code refactoring review, not a broad architecture review. Focus on readability, maintainability, efficiency, reliability, security, error handling, and testability inside the existing architecture.

Review all relevant project material, including:
- Go source code
- Ansible playbooks, roles, tasks, vars, defaults, templates, and handlers
- scripts/
- Makefiles
- CI files
- examples/
- docs/specs only when needed to understand intended behavior

First, inspect the repository and build a practical understanding of how the project works.

Use commands such as these when useful:

```bash
git status --short
find . -maxdepth 4 -type f | sort
go list ./...
go test ./...
go vet ./...
```

For Ansible, inspect structure manually and run validation commands only if the required tools are already available. Do not install new tools unless explicitly allowed.

Useful Ansible checks, if available:

```bash
ansible-lint
ansible-playbook --syntax-check <playbook>
```

Do not make large architectural rewrites unless a small local refactor clearly improves code quality without changing behavior.

## Main goals

Improve the code so it is:
- easier to read
- easier to test
- safer by default
- less repetitive
- less error-prone
- more idiomatic for Go and Ansible
- more explicit about failures
- more robust when executed in real environments

## Review and refactor criteria

### 1. Readability

Look for:
- confusing function or variable names
- overly long functions
- deeply nested logic
- unclear control flow
- duplicated logic
- magic strings or magic constants
- unclear error messages
- code that requires too much context to understand
- comments that are outdated, missing, or misleading

Prefer:
- small, focused functions
- clear names
- explicit data structures
- simple control flow
- meaningful constants
- comments only where they clarify intent or non-obvious behavior

### 2. Go implementation quality

Review Go code for:
- idiomatic error handling
- useful error wrapping with context
- correct use of context.Context where applicable
- avoiding panics in normal execution paths
- avoiding global mutable state where practical
- separation between parsing, validation, execution, and output
- safe filesystem handling
- safe command execution
- avoiding unnecessary shell invocation
- avoiding duplicated command-building logic
- CLI commands that are as lean as possible: handle user inputs, select the
  target workflow, and orchestrate Ansible playbooks/roles without embedding
  target infrastructure configuration logic that belongs in those playbooks or
  roles
- predictable logging and output
- efficient string/file handling
- avoiding resource leaks
- proper defer usage
- testability of functions without real infrastructure

Prefer standard library solutions unless an existing dependency is already justified.

### 3. Ansible implementation quality

Review Ansible code for:
- idempotency
- readable task names
- safe variable defaults
- appropriate use of modules instead of shell/command
- shell/command usage with explicit `changed_when` and `failed_when`
- avoiding duplicated tasks
- clear variable naming
- secure handling of secrets
- avoiding leaking secrets in logs
- correct `no_log` usage for sensitive values
- safe file permissions
- clear handlers
- predictable role behavior
- avoiding hidden assumptions about local machine state
- clear support for dry-run/check mode where practical

### 4. Security

Look for:
- command injection risks
- unsafe shell usage
- unvalidated paths
- path traversal risks
- insecure temporary file handling
- credentials or tokens in logs
- credentials committed in examples
- weak file permissions
- unsafe defaults
- unsafe download/execution patterns
- TLS verification being disabled
- missing checksum validation for downloads
- excessive privileges
- sudo/root usage that could be narrowed
- unsafe cleanup commands such as broad `rm -rf`

Do not invent vulnerabilities. Only report issues supported by code evidence.

### 5. Efficiency

Look for:
- repeated expensive operations
- unnecessary subprocess calls
- inefficient file scans
- repeated parsing of the same data
- avoidable network calls
- unnecessary sleeps or polling
- loops that could be simplified
- Ansible tasks that repeatedly do work even when nothing changed

Do not optimize prematurely. Prioritize simple, measurable improvements.

### 6. Reliability and error handling

Look for:
- ignored errors
- vague errors
- missing validation before execution
- fragile assumptions about directories, binaries, permissions, or environment variables
- missing cleanup on failure
- partial-state problems
- retry logic that is missing, excessive, or unsafe
- failures that would be hard for users to diagnose

Prefer:
- clear validation before side effects
- actionable error messages
- structured cleanup
- explicit preflight checks
- deterministic behavior

### 7. Tests

Review tests and recommend or add tests for:
- parsing
- validation
- command construction
- filesystem behavior
- error cases
- security-sensitive behavior
- Ansible-generated output
- dry-run behavior, if present

Do not require real OpenShift clusters, external infrastructure, or internet access for unit tests.

## How to work

Start with a report before changing code unless explicitly instructed to directly edit.

If asked to make changes:
1. Make small, focused commits or patches.
2. Preserve behavior.
3. Avoid broad rewrites.
4. Keep public interfaces compatible unless explicitly allowed to change them.
5. Run relevant tests/checks.
6. Explain what changed and why.

When making code changes, prefer this sequence:
1. Fix high-confidence safety or correctness problems.
2. Reduce duplication.
3. Improve naming and readability.
4. Improve error handling.
5. Add or adjust tests.
6. Improve efficiency where there is clear evidence.

## Output format for review-only mode

Produce this report:

# Code Refactoring Review Summary

## 1. Executive Summary
Summarize the most important code-quality risks and improvement opportunities.

## 2. High-Value Refactoring Targets
For each target, include:
- Severity: Critical, High, Medium, or Low
- Area: Go, Ansible, Scripts, CI, Tests, Security, etc.
- Evidence: concrete file paths and functions/tasks when possible
- Problem
- Why it matters
- Recommended refactor
- Risk of change: Low, Medium, or High

## 3. Readability Improvements
List concrete readability improvements with file paths.

## 4. Security Improvements
List concrete security improvements with file paths.
Only include issues supported by repository evidence.

## 5. Error Handling and Reliability Improvements
List concrete improvements for validation, diagnostics, cleanup, and failure behavior.

## 6. Efficiency Improvements
List practical performance or efficiency improvements.
Avoid speculative micro-optimizations.

## 7. Go-Specific Refactoring Suggestions
Focus on implementation-level Go improvements.
Include examples of better patterns where useful.

## 8. Ansible-Specific Refactoring Suggestions
Focus on implementation-level Ansible improvements.
Include examples of better task patterns where useful.

## 9. Test Gaps
Identify missing tests and propose specific test cases.

## 10. Suggested Refactoring Plan
Create a staged plan:
- Phase 1: safe readability and duplication cleanup
- Phase 2: error handling, validation, and tests
- Phase 3: security hardening and efficiency improvements

## 11. Patch Plan
If changes are requested, list the exact files you would modify first and why.

## 12. Open Questions
List only questions that block safe refactoring.

## Output format for edit mode

If explicitly asked to modify the repository, produce:

# Refactoring Changes Applied

## Summary
Briefly describe the changes.

## Files Changed
List changed files and purpose.

## Behavior Compatibility
Explain why behavior should remain compatible.

## Tests/Checks Run
List commands run and results.

## Remaining Recommendations
List important follow-up improvements not included in this pass.

Important constraints:
- Do not invent facts. Base findings on repository evidence.
- Do not perform broad architectural redesign; that belongs to a separate architecture review.
- Do not change behavior unless explicitly requested or clearly necessary to fix a bug.
- Do not introduce new dependencies without strong justification.
- Do not remove functionality.
- Do not hide errors.
- Do not weaken security for convenience.
- Do not store or print secrets.
- Prefer small, reviewable changes.
- Be practical and prioritize changes that a maintainer can safely accept.
