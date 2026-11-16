"""Measure statement coverage and hold it to floors that only rise.

`make cover` runs the Go tests with a cover profile, where each package is
measured by its own tests, and the collection's plain unit loop under
coverage_recorder.py, then runs `check`, which writes the measured figures
and fails when a Go package or a kind of collection plugin is below its floor
in coverage-floors.json, when a floor could rise by more than TOLERANCE, or
when the floors and the measured names differ. `refresh`, which
`make cover-floors` runs on the last measurement, rewrites the floors: it
raises each to the measured figure rounded down to a tenth less MARGIN, adds
the new names and removes the gone ones, and never lowers a floor.

Usage: coverage_floors.py check|refresh --root ROOT --go-profile PROFILE
       --collection-lines DIRECTORY --floors FLOORS [--summary SUMMARY]
"""

import argparse
import ast
import json
import re
import sys
from pathlib import Path

COLLECTION = "ansible/collections/ansible_collections/bootwright/core"

# Floors are kept in tenths of a percentage point. A refreshed floor sits
# MARGIN below the measured figure, which absorbs the noise of a test whose
# path varies from run to run; a measured figure may exceed what its floor
# would be by TOLERANCE before the floor must rise, so a floor only rises.
MARGIN = 5
TOLERANCE = 5

BLOCK = re.compile(r"^(.+):(\d+)\.(\d+),(\d+)\.(\d+) (\d+) (\d+)$")


def module_path(root: Path) -> str:
    """Return the module path the root's go.mod declares."""
    for line in (root / "go.mod").read_text(encoding="utf-8").splitlines():
        if line.startswith("module "):
            return line.split()[1]
    raise SystemExit(f"{root / 'go.mod'} declares no module")


def go_packages(profile: str, module: str) -> dict[str, tuple[int, int]]:
    """Return each package's statements and covered statements in a profile.

    A package is named by its directory relative to the module, the module's
    own package by ".". A block reported twice counts once, covered when any
    report covered it.
    """
    blocks: dict[tuple[str, ...], tuple[int, bool]] = {}
    for line in profile.splitlines():
        match = BLOCK.match(line.strip())
        if not match:
            continue
        key = match.groups()[:5]
        statements, count = int(match.group(6)), int(match.group(7))
        previous = blocks.get(key)
        blocks[key] = (statements, count > 0 or (previous is not None and previous[1]))
    packages: dict[str, tuple[int, int]] = {}
    for (name, *_), (statements, covered) in blocks.items():
        directory = name.rsplit("/", 1)[0]
        if directory == module:
            package = "."
        elif directory.startswith(module + "/"):
            package = directory[len(module) + 1 :]
        else:
            raise SystemExit(f"{name} in the cover profile is outside {module}")
        total, hit = packages.get(package, (0, 0))
        packages[package] = (total + statements, hit + (statements if covered else 0))
    return packages


def statement_spans(tree: ast.AST) -> list[tuple[int, int, int]]:
    """Return a (first, last, fallback) line span per statement of a module.

    A statement ran when the recorder saw a line of its span: a simple
    statement's every line; a definition's decorators and header; a compound
    statement's header, or, as a header such as `try:` executes no
    instruction, its first body statement's first line, the fallback. A
    docstring, a global or nonlocal declaration and a __future__ import
    execute nothing and are no statements here.
    """
    spans = []
    for node in ast.walk(tree):
        if not isinstance(node, ast.stmt):
            continue
        if isinstance(node, (ast.Global, ast.Nonlocal)):
            continue
        if isinstance(node, ast.ImportFrom) and node.module == "__future__":
            continue
        if (
            isinstance(node, ast.Expr)
            and isinstance(node.value, ast.Constant)
            and isinstance(node.value.value, str)
        ):
            continue
        body = getattr(node, "body", None)
        if isinstance(body, list) and body and isinstance(body[0], ast.stmt):
            first = min(
                [node.lineno]
                + [decorator.lineno for decorator in getattr(node, "decorator_list", [])]
            )
            spans.append((first, max(first, body[0].lineno - 1), body[0].lineno))
        else:
            spans.append((node.lineno, node.end_lineno or node.lineno, 0))
    return spans


def file_coverage(source: str, seen: set[int]) -> tuple[int, int]:
    """Return a Python source's statements and those whose span was seen."""
    spans = statement_spans(ast.parse(source))
    covered = sum(
        1
        for first, last, fallback in spans
        if fallback in seen or any(line in seen for line in range(first, last + 1))
    )
    return len(spans), covered


def recorded_lines(directory: Path) -> dict[str, set[int]]:
    """Merge every process's recorded lines; refuse when none were recorded."""
    files = sorted(directory.glob("lines.*.json"))
    if not files:
        raise SystemExit(
            f"{directory} holds no recorded lines; the recorder did not run"
        )
    merged: dict[str, set[int]] = {}
    for path in files:
        for name, lines in json.loads(path.read_text(encoding="utf-8")).items():
            merged.setdefault(name, set()).update(lines)
    return merged


def collection_kinds(
    plugins: Path, seen: dict[str, set[int]]
) -> dict[str, tuple[int, int]]:
    """Return each plugin kind's statements and covered statements."""
    kinds: dict[str, tuple[int, int]] = {}
    for path in sorted(plugins.rglob("*.py")):
        relative = path.relative_to(plugins).as_posix()
        statements, covered = file_coverage(
            path.read_text(encoding="utf-8"), seen.get(relative, set())
        )
        kind = relative.split("/", 1)[0]
        total, hit = kinds.get(kind, (0, 0))
        kinds[kind] = (total + statements, hit + covered)
    return kinds


def tenths(statements: int, covered: int) -> int:
    """Return the share of covered statements in tenths of a point, rounded down."""
    return 1000 if statements == 0 else (covered * 1000) // statements


def target(measured: int) -> int:
    """Return the floor a measured figure in tenths holds."""
    return max(0, measured - MARGIN)


def violations(measured: dict[str, int], floors: dict[str, float], kind: str) -> list[str]:
    """Return each way the measured figures, in tenths, break the floors."""
    found = []
    for name in sorted(set(floors) - set(measured)):
        found.append(
            f"{kind} {name} has a floor but was not measured; run make cover-floors"
        )
    for name in sorted(measured):
        figure = measured[name]
        if name not in floors:
            found.append(
                f"{kind} {name} measures {figure / 10:.1f}% and has no floor; "
                "run make cover-floors"
            )
            continue
        floor = round(floors[name] * 10)
        if figure < floor:
            found.append(
                f"{kind} {name} measures {figure / 10:.1f}%, below its floor of "
                f"{floor / 10:.1f}%; restore its tests"
            )
        elif target(figure) - floor > TOLERANCE:
            found.append(
                f"{kind} {name} measures {figure / 10:.1f}%, more than "
                f"{(MARGIN + TOLERANCE) / 10:.1f} points above its floor of "
                f"{floor / 10:.1f}%; run make cover-floors to raise it to "
                f"{target(figure) / 10:.1f}%"
            )
    return found


def refreshed(measured: dict[str, int], floors: dict[str, float]) -> dict[str, float]:
    """Return the floors raised to the measured figures, never lowered."""
    return {
        name: max(round(floors.get(name, 0) * 10), target(figure)) / 10
        for name, figure in sorted(measured.items())
    }


def measure(arguments: argparse.Namespace) -> dict[str, dict[str, tuple[int, int]]]:
    """Read both measurements into statements and covered statements per name."""
    root = Path(arguments.root)
    profile = Path(arguments.go_profile).read_text(encoding="utf-8")
    go = go_packages(profile, module_path(root))
    if not go:
        raise SystemExit(f"{arguments.go_profile} reports no package")
    collection = collection_kinds(
        root / COLLECTION / "plugins", recorded_lines(Path(arguments.collection_lines))
    )
    return {"go": go, "collection": collection}


def main(argv: list[str]) -> int:
    parser = argparse.ArgumentParser(prog="coverage_floors.py")
    parser.add_argument("action", choices=("check", "refresh"))
    parser.add_argument("--root", required=True)
    parser.add_argument("--go-profile", required=True)
    parser.add_argument("--collection-lines", required=True)
    parser.add_argument("--floors", required=True)
    parser.add_argument("--summary")
    arguments = parser.parse_args(argv)
    counts = measure(arguments)
    measured = {
        section: {name: tenths(*count) for name, count in names.items()}
        for section, names in counts.items()
    }
    floors_path = Path(arguments.floors)
    floors = json.loads(floors_path.read_text(encoding="utf-8"))
    if arguments.summary:
        Path(arguments.summary).write_text(
            json.dumps(
                {
                    section: {
                        name: {
                            "statements": count[0],
                            "covered": count[1],
                            "percent": measured[section][name] / 10,
                        }
                        for name, count in sorted(names.items())
                    }
                    for section, names in counts.items()
                },
                indent=2,
            )
            + "\n",
            encoding="utf-8",
        )
    for section, names in counts.items():
        statements = sum(count[0] for count in names.values())
        covered = sum(count[1] for count in names.values())
        print(
            f"cover: {section}: {len(names)} measured, "
            f"{tenths(statements, covered) / 10:.1f}% of {statements} statements"
        )
    if arguments.action == "refresh":
        raised = {
            section: refreshed(measured[section], floors.get(section, {}))
            for section in ("go", "collection")
        }
        floors_path.write_text(json.dumps(raised, indent=2) + "\n", encoding="utf-8")
        print(f"cover: wrote {floors_path}")
        return 0
    found = violations(measured["go"], floors.get("go", {}), "Go package")
    found += violations(
        measured["collection"], floors.get("collection", {}), "collection plugin kind"
    )
    for violation in found:
        print("cover: " + violation, file=sys.stderr)
    if found:
        return 1
    print("cover: every measured name holds its floor")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
