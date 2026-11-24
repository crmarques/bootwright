"""Refuse a text-only spec commit that loses a literal; run: python3 -I -B spec_literals.py COMMIT."""

from __future__ import annotations

from collections import Counter
import re
import shutil
import subprocess
import sys

GIT = shutil.which("git")
MOVED = "Literal-moved:"
SPAN = re.compile(r"(?<!`)(`+)(?!`)(.+?)(?<!`)\1(?!`)", re.DOTALL)
FENCE = re.compile(r"^\s*(`{3,}|~{3,})")
UNITS = (
    "KiB|MiB|GiB|TiB|KB|MB|GB|TB|bytes?|B|ns|us|ms|milliseconds?|seconds?|minutes?|min"
    "|hours?|days?|weeks?|s|%"
)
SPELLING = {
    "byte": "bytes", "second": "s", "seconds": "s", "millisecond": "ms", "milliseconds": "ms",
    "minute": "min", "minutes": "min", "hour": "hours", "day": "days", "week": "weeks",
}
QUANTITY = re.compile(rf"(?<![\w.])(\d[\d,._]*)\s?({UNITS})(?![\w%])")
MOVE = re.compile(rf"^{MOVED}\s+(.+?)\s+->\s+(\S.*)$")
PAGE = re.compile(r"specs/\S+/\*")
DOCUMENTATION = ("specs/milestones/", "specs/deferred/", "audits/", ".agents/", "docs/")


def git(*args: str, check: bool = True) -> bytes:
    if GIT is None:
        sys.stderr.write("spec_literals.py: git is not on PATH\n")
        raise SystemExit(2)
    done = subprocess.run(
        [GIT, *args], stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False
    )
    if done.returncode != 0:
        if check:
            sys.stderr.write(done.stderr.decode("utf-8", "replace"))
            raise SystemExit(2)
        return b""
    return done.stdout


def is_normative(path: str) -> bool:
    """Say whether a repository path is a normative spec page."""
    return (
        path.startswith("specs/")
        and not path.startswith("specs/milestones/")
        and not path.startswith("specs/deferred/")
    )


def in_scope(path: str) -> bool:
    """Say whether a path is a normative page or documentation the rule tolerates beside one."""
    return is_normative(path) or path.startswith(DOCUMENTATION) or path.endswith(".md")


def quantity(number: str, unit: str) -> str:
    unit = SPELLING.get(unit, unit)
    return number + unit if unit == "%" else f"{number} {unit}"


def canonical(literal: str) -> str:
    """Spell a literal one way: a number with its unit, or a span's words."""
    literal = literal.strip()
    if len(literal) > 1 and literal[0] == literal[-1] == "`":
        literal = literal[1:-1]
    literal = " ".join(literal.split())
    found = QUANTITY.fullmatch(literal)
    return quantity(*found.groups()) if found else literal


def literals(text: str) -> Counter[str]:
    """Count the backticked spans and the numbers with units outside fences."""
    found: Counter[str] = Counter()
    paragraph: list[str] = []
    fence = ""

    def flush() -> None:
        if not paragraph:
            return
        prose = "\n".join(paragraph)
        paragraph.clear()
        for span in SPAN.finditer(prose):
            found[canonical(span.group(2))] += 1
        for amount in QUANTITY.finditer(prose):
            found[quantity(*amount.groups())] += 1

    for line in text.splitlines():
        opening = FENCE.match(line)
        if fence:
            if opening and opening.group(1)[0] == fence[0] and len(opening.group(1)) >= len(fence) \
                    and not line.strip().strip(fence[0]):
                fence = ""
        elif opening:
            flush()
            fence = opening.group(1)
        elif line.strip():
            paragraph.append(line)
        else:
            flush()
    flush()
    del found[""]
    return found


def pages(commit: str, paths: list[str]) -> dict[str, str]:
    """Read the named files of a commit with one process."""
    if not paths:
        return {}
    request = "".join(f"{commit}:{path}\n" for path in paths).encode("utf-8", "surrogateescape")
    done = subprocess.run(
        [GIT, "cat-file", "--batch"], input=request,
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=True,
    )
    out, position, result = done.stdout, 0, {}
    for path in paths:
        end = out.index(b"\n", position)
        header = out[position:end].split()
        position = end + 1
        if len(header) == 3 and header[1] == b"blob":
            size = int(header[2])
            result[path] = out[position:position + size].decode("utf-8", "replace")
            position += size + 1
    return result


def total(pages_: dict[str, str]) -> Counter[str]:
    counts: Counter[str] = Counter()
    for text in pages_.values():
        counts.update(literals(text))
    return counts


def lost(before: dict[str, str], after: dict[str, str], moved_pages: frozenset[str] = frozenset()) -> list[str]:
    """Name the literals that occur fewer times in the later pages than in the earlier.

    A page in moved_pages is declared moved as a whole, so its earlier text is not held.
    """
    earlier = total({path: text for path, text in before.items() if path not in moved_pages})
    fell = earlier - total(after)
    return sorted(fell)


def declared(body: str) -> tuple[set[str], set[str]]:
    """Read the literals and the pages a commit body says it moved, each with a destination."""
    literals_, pages_ = set(), set()
    for line in body.splitlines():
        found = MOVE.match(line.strip())
        if not found:
            continue
        what = canonical(found.group(1))
        if PAGE.fullmatch(what):
            pages_.add(what[: -len("/*")])
        else:
            literals_.add(what)
    return literals_, pages_


def check(commit: str) -> list[str]:
    """Return one refusal per literal a text-only normative commit drops."""
    changed = [
        path for path in git(
            "diff-tree", "--no-commit-id", "--name-only", "-r", "-z", "--root", commit
        ).decode("utf-8", "surrogateescape").split("\0") if path
    ]
    if not changed or not all(in_scope(path) for path in changed):
        return []
    normative = [path for path in changed if is_normative(path)]
    parent = git("rev-parse", "--verify", "-q", f"{commit}^", check=False).strip()
    if not normative or not parent:
        return []
    after = pages(commit, normative)
    before = pages(parent.decode(), normative)
    body = git("log", "-1", "--format=%b", commit).decode("utf-8", "replace")
    moved, moved_pages = declared(body)
    return [
        f"{commit}: drops the literal `{literal}` from the normative pages; keep it, "
        f"or add a body line '{MOVED} {literal} -> <where it went>'"
        for literal in lost(before, after, frozenset(moved_pages)) if literal not in moved
    ]


def main(argv: list[str]) -> int:
    if len(argv) != 1:
        sys.stderr.write("usage: spec_literals.py COMMIT\n")
        return 2
    refusals = check(argv[0])
    for refusal in refusals:
        sys.stderr.write(refusal + "\n")
    return 1 if refusals else 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
