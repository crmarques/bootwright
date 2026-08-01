#!/usr/bin/env python3
"""Select the check stages and Go packages a change actually requires.

Emits shell assignments consumed by the scoped Make gates. Intent raises the
floor of what runs; it never narrows what the changed files demand.
"""

from __future__ import annotations

import argparse
import os
from pathlib import Path
import subprocess
import sys


FULL_GO_PACKAGES = "./..."

WIDEN_ALL_PATHS = {
    "Makefile",
    "go.mod",
    "go.sum",
    "scripts/select-checks.py",
    "scripts/test_select_checks.py",
}

PROSE_READER_PACKAGES = (
    "./internal/repo/checks/...",
    "./internal/state/desired/...",
    "./internal/cli/...",
)

FIXTURE_READER_PACKAGES = (
    "./internal/cli/...",
    "./internal/converge/workflow/...",
    "./internal/render/...",
    "./internal/state/desired/...",
    "./internal/repo/checks/...",
)

ANSIBLE_READER_PACKAGES = (
    "./internal/repo/checks/...",
    "./internal/converge/bundle/...",
    "./internal/repo/bundlecheck/...",
)

FEATURE_FLOOR_PACKAGES = (
    "./api/v1alpha1/...",
    "./internal/state/desired/...",
    "./internal/state/graph/...",
    "./internal/render/...",
    "./internal/cli/...",
    "./internal/converge/...",
    "./internal/repo/checks/...",
)

GO_STAGES = ("check-gofmt", "check-go-source-visibility", "cli-file-size-check")


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--tier", required=True, choices=("scoped", "feature"))
    parser.add_argument("--base", default="main")
    parser.add_argument("--root", default=".", type=Path)
    parser.add_argument("--max-changed", default=250, type=int)
    args = parser.parse_args()

    root = args.root.resolve()
    changed, widen_reason = collect_changed(root, args.base, args.max_changed)
    if widen_reason:
        emit(FULL_GO_PACKAGES, all_stages(), widen_reason)
        return 0

    if not changed:
        emit("", (), "no change against %s; nothing to check" % args.base)
        return 0

    forced = sorted(p for p in changed if p in WIDEN_ALL_PATHS)
    if forced:
        emit(FULL_GO_PACKAGES, all_stages(), "build definition changed (%s)" % ", ".join(forced))
        return 0

    domains = classify(root, changed)
    stages = sorted(stages_for(domains))
    packages = packages_for(root, changed, domains, args.tier)
    summary = "tier=%s changed=%d domains=%s" % (args.tier, len(changed), ",".join(sorted(domains)) or "none")
    emit(" ".join(packages), stages, summary)
    return 0


def collect_changed(root: Path, base: str, max_changed: int) -> tuple[set[str], str]:
    if not (root / ".git").exists():
        return set(), "not a git worktree; running the full gate"
    if run(root, ["git", "rev-parse", "--verify", "--quiet", base + "^{commit}"]) is None:
        return set(), "base ref %r does not resolve; running the full gate" % base

    paths: set[str] = set()
    committed = run(root, ["git", "diff", "--name-only", "--diff-filter=d", base + "...HEAD"])
    if committed is None:
        return set(), "cannot diff against %s; running the full gate" % base
    paths.update(line for line in committed.splitlines() if line)

    worktree = run(root, ["git", "status", "--porcelain", "--untracked-files=all"])
    if worktree is None:
        return set(), "cannot read the working tree status; running the full gate"
    for line in worktree.splitlines():
        if len(line) < 4:
            continue
        entry = line[3:]
        if " -> " in entry:
            entry = entry.split(" -> ", 1)[1]
        entry = entry.strip().strip('"')
        if entry and (root / entry).exists():
            paths.add(entry)

    if len(paths) > max_changed:
        return set(), "%d changed paths exceeds --max-changed=%d; running the full gate" % (len(paths), max_changed)
    return paths, ""


def classify(root: Path, changed: set[str]) -> set[str]:
    domains: set[str] = set()
    for path in changed:
        if path.endswith(".go"):
            domains.add("go")
        if path.startswith("api/"):
            domains.add("api")
        if path.startswith("ansible/"):
            domains.add("ansible")
        if path.startswith("scripts/") and path.endswith(".py"):
            domains.add("python")
        if path.startswith("docs/") or path == "mkdocs.yml":
            domains.add("docs")
        if path.startswith("specs/"):
            domains.add("specs")
        if path == "README.md":
            domains.add("readme")
        if path.startswith(".agents/") or path in ("AGENTS.md", "CLAUDE.md"):
            domains.add("agents")
        if path.startswith("examples/") or path.startswith("test/"):
            domains.add("fixtures")
        if path.startswith("add-ons/"):
            domains.add("addons")
        if path.endswith("Containerfile") or path == ".dockerignore":
            domains.add("container")
        if path.startswith(".github/workflows/"):
            domains.add("workflows")
        if has_shell_shebang(root / path):
            domains.add("shell")
    return domains


def has_shell_shebang(path: Path) -> bool:
    if not path.is_file():
        return False
    try:
        with path.open("rb") as handle:
            first = handle.readline(128).decode("utf-8", "replace").strip()
    except OSError:
        return False
    if not first.startswith("#!"):
        return False
    return first.endswith("/sh") or first.endswith("/bash") or "env sh" in first or "env bash" in first


def stages_for(domains: set[str]) -> set[str]:
    stages: set[str] = set()
    if "go" in domains or "api" in domains:
        stages.update(GO_STAGES)
    if "ansible" in domains:
        stages.update(("ansible-syntax-check", "sync-bundle"))
    if "python" in domains:
        stages.add("python-test")
    if "shell" in domains:
        stages.add("shellcheck-check")
    if "docs" in domains:
        stages.add("docs-check")
    if domains & {"docs", "specs", "readme", "fixtures", "addons", "agents"}:
        stages.add("stale-term-check")
    if "container" in domains:
        stages.add("containerfile-pin-check")
    if "workflows" in domains:
        stages.add("workflow-yaml-check")
    return stages


def packages_for(root: Path, changed: set[str], domains: set[str], tier: str) -> list[str]:
    selected: set[str] = set()
    if domains & {"go", "api"}:
        selected.update(go_closure(root, changed))
    if domains & {"docs", "specs", "readme"}:
        selected.update(PROSE_READER_PACKAGES)
    elif "agents" in domains:
        selected.add("./internal/repo/checks/...")
    if domains & {"fixtures", "addons"}:
        selected.update(FIXTURE_READER_PACKAGES)
    if "ansible" in domains:
        selected.update(ANSIBLE_READER_PACKAGES)
    if "container" in domains:
        selected.add("./internal/repo/checks/...")
    if "addons" in domains:
        selected.add("./internal/addons/...")
    if tier == "feature" and selected:
        selected.update(FEATURE_FLOOR_PACKAGES)
    return sorted(selected)


def go_closure(root: Path, changed: set[str]) -> set[str]:
    dirs = sorted({str(Path(p).parent) for p in changed if p.endswith(".go")})
    if not dirs:
        return set()

    listing = run(root, ["go", "list", "-e", "-f", "{{.ImportPath}}"] + ["./" + d for d in dirs])
    if listing is None:
        return {FULL_GO_PACKAGES}
    seeds = {line for line in listing.splitlines() if line and not line.startswith("_")}
    if not seeds:
        return set()

    graph = run(
        root,
        [
            "go",
            "list",
            "-e",
            "-f",
            '{{.ImportPath}}\t{{join .Deps " "}} {{join .TestImports " "}} {{join .XTestImports " "}}',
            "./...",
        ],
    )
    if graph is None:
        return {FULL_GO_PACKAGES}

    selected = set(seeds)
    for line in graph.splitlines():
        if "\t" not in line:
            continue
        importer, deps = line.split("\t", 1)
        if seeds & set(deps.split()):
            selected.add(importer)
    return {package_pattern(p) for p in selected}


def package_pattern(import_path: str) -> str:
    module = module_path()
    if module and import_path == module:
        return "."
    if module and import_path.startswith(module + "/"):
        return "./" + import_path[len(module) + 1 :]
    return import_path


_MODULE_CACHE: list[str] = []


def module_path() -> str:
    if _MODULE_CACHE:
        return _MODULE_CACHE[0]
    value = run(Path.cwd(), ["go", "list", "-m"]) or ""
    _MODULE_CACHE.append(value.strip())
    return _MODULE_CACHE[0]


def all_stages() -> tuple[str, ...]:
    return (
        "ansible-syntax-check",
        "check-go-source-visibility",
        "check-gofmt",
        "cli-file-size-check",
        "containerfile-pin-check",
        "docs-check",
        "python-test",
        "shellcheck-check",
        "stale-term-check",
        "sync-bundle",
        "workflow-yaml-check",
    )


def run(root: Path, command: list[str]) -> str | None:
    try:
        completed = subprocess.run(
            command,
            cwd=root,
            check=False,
            capture_output=True,
            text=True,
            env={**os.environ, "GIT_OPTIONAL_LOCKS": "0"},
        )
    except OSError:
        return None
    if completed.returncode != 0:
        return None
    return completed.stdout


def emit(packages: str, stages, summary: str) -> None:
    sys.stdout.write("BOOTWRIGHT_SELECTION_PACKAGES=%s\n" % shell_quote(packages))
    sys.stdout.write("BOOTWRIGHT_SELECTION_STAGES=%s\n" % shell_quote(" ".join(stages)))
    sys.stdout.write("BOOTWRIGHT_SELECTION_SUMMARY=%s\n" % shell_quote(summary))


def shell_quote(value: str) -> str:
    return "'" + value.replace("'", "'\\''") + "'"


if __name__ == "__main__":
    raise SystemExit(main())
