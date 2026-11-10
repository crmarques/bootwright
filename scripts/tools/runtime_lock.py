"""Generate, verify and report on the controller's Python runtime lock.

The lock, internal/controller/runtimelock/lock.json, names every wheel the
controller venv installs, per Python ABI, with its URL, size and sha256.
Versions are chosen here, when a person regenerates the lock, never at setup.
The venv installs without dependency resolution, so the lock leaves out the
distributions ansible-core declares that no controller path imports.
The license table beside it, licenses.json, names each locked distribution's
SPDX license, read from the wheel's own metadata when this tool regenerates the
lock and held to it again by verify, so a build offline still knows them.
This tool uses the standard library only; pip runs from its own wheel.
"""

from __future__ import annotations

import argparse
import concurrent.futures
import email.parser
import hashlib
import json
import os
import re
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import zipfile
from pathlib import Path

FORMAT = "bootwright.controller.runtime-lock-v1"
LICENSES_FORMAT = "bootwright.controller.runtime-licenses-v1"
PYPI = "https://pypi.org"
FILES = "https://files.pythonhosted.org"
MAX_GLIBC = (2, 34)
MAX_SIZE = 1 << 30
WORKERS = 4
DEFAULT_LOCK = (
    Path(__file__).resolve().parents[2] / "internal/controller/runtimelock/lock.json"
)
# A classifier names a license without ambiguity only when one SPDX id stands
# for the whole family; "BSD License" does not, so such a wheel is reviewed.
CLASSIFIERS = {
    "License :: OSI Approved :: MIT License": "MIT",
    "License :: OSI Approved :: Apache Software License": "Apache-2.0",
    "License :: OSI Approved :: ISC License (ISCL)": "ISC",
    "License :: OSI Approved :: Mozilla Public License 2.0 (MPL 2.0)": "MPL-2.0",
}
SPDX_EXPRESSION = re.compile(r"[A-Za-z0-9.+-]+( (AND|OR|WITH) [A-Za-z0-9.+-]+)*")
LEGACY_GLIBC = {
    "manylinux1_x86_64": 5,
    "manylinux2010_x86_64": 12,
    "manylinux2014_x86_64": 17,
}
# pip expands no manylinux platform into the older ones, so each is named,
# oldest first: where a release ships several builds, the oldest glibc wins.
PLATFORMS = (
    "manylinux1_x86_64",
    "manylinux2010_x86_64",
    "manylinux2014_x86_64",
    *(f"manylinux_2_{minor}_x86_64" for minor in range(5, MAX_GLIBC[1] + 1)),
)
# ansible-core imports these only behind an ImportError guard, for vault, an
# in-memory SSH key, NTLM channel binding and ansible-galaxy, none of which a
# controller playbook uses; cffi and pycparser come only with cryptography.
# The collection's units suite runs every playbook and imports every plugin
# with nothing but the lock's distributions importable.
OMITTED = ("cffi", "cryptography", "pycparser", "resolvelib")
PASSED_ENVIRONMENT = (
    "HTTP_PROXY",
    "HTTPS_PROXY",
    "NO_PROXY",
    "http_proxy",
    "https_proxy",
    "no_proxy",
    "SSL_CERT_FILE",
    "SSL_CERT_DIR",
)


class ToolError(Exception):
    """A failure the person running the tool can act on."""


def normalize(name: str) -> str:
    """Return the PEP 503 form of a distribution name."""
    return re.sub(r"[-_.]+", "-", name).lower()


def release_key(version: str) -> tuple[int, ...] | None:
    """Return a plain release's numbers, or None for any other version."""
    if not re.fullmatch(r"[0-9]+(\.[0-9]+)*", version):
        return None
    return tuple(int(part) for part in version.split("."))


def rewritten(url: str, root: str, replacement: str) -> str:
    """Return the url with its leading root replaced, as a mirror serves it."""
    if replacement == root:
        return url
    if not url.startswith(root + "/"):
        raise ToolError(f"{url} is not under {root}")
    return replacement.rstrip("/") + url[len(root) :]


def open_url(url: str):
    """Open a URL, trying again when the network fails; a file is tried once."""
    attempts = 3 if url.startswith("https:") else 1
    for attempt in range(1, attempts + 1):
        try:
            return urllib.request.urlopen(url, timeout=60)
        except urllib.error.HTTPError as error:
            if error.code < 500 or attempt == attempts:
                raise ToolError(f"{url}: {error}") from error
        except OSError as error:
            if attempt == attempts:
                raise ToolError(f"{url}: {error}") from error
        time.sleep(2 ** (attempt - 1))
    raise AssertionError("unreachable")


def fetch_json(url: str) -> dict:
    """Return the JSON document at a URL."""
    with open_url(url) as response:
        try:
            return json.load(response)
        except ValueError as error:
            raise ToolError(f"{url} is not JSON: {error}") from error


def download(url: str, destination: Path | None = None) -> tuple[str, int]:
    """Stream a URL, returning its sha256 and size and keeping it if asked."""
    digest, size = hashlib.sha256(), 0
    sink = destination.open("wb") if destination else None
    try:
        with open_url(url) as response:
            while chunk := response.read(1 << 20):
                size += len(chunk)
                if size > MAX_SIZE:
                    raise ToolError(f"{url} is larger than {MAX_SIZE} bytes")
                digest.update(chunk)
                if sink:
                    sink.write(chunk)
    finally:
        if sink:
            sink.close()
    return digest.hexdigest(), size


class Sources:
    """Where the index and the files are read from; mirrors serve tests."""

    def __init__(self, pypi: str, files: str):
        self.pypi, self.files = pypi.rstrip("/"), files.rstrip("/")

    def index(self, name: str, version: str | None = None) -> dict:
        path = f"/pypi/{name}/{version}/json" if version else f"/pypi/{name}/json"
        return fetch_json(self.pypi + path)

    def file_url(self, url: str) -> str:
        return rewritten(url, FILES, self.files)


def check_pythonhosted(url: str) -> None:
    """Refuse a URL outside the one host the lock may name."""
    parsed = urllib.parse.urlsplit(url)
    if (
        parsed.scheme != "https"
        or parsed.netloc != "files.pythonhosted.org"
        or not parsed.path.startswith("/packages/")
        or parsed.query
        or parsed.fragment
    ):
        raise ToolError(f"{url} is not an https://files.pythonhosted.org/packages/ URL")


def filename_of(url: str) -> str:
    """Return the file name a package URL ends in."""
    return urllib.parse.unquote(urllib.parse.urlsplit(url).path.rsplit("/", 1)[-1])


def glibc_needed(filename: str) -> int | None:
    """Return the glibc 2.N minor a wheel needs, or None for a pure wheel."""
    stem = filename.removesuffix(".whl")
    parts = stem.split("-")
    if len(parts) != 5 or not filename.endswith(".whl"):
        raise ToolError(f"{filename} is not a wheel name of five parts")
    platform = parts[4]
    if platform == "any":
        return None
    minors = []
    for tag in platform.split("."):
        match = re.fullmatch(r"manylinux_2_([0-9]+)_x86_64", tag)
        if match:
            minors.append(int(match.group(1)))
        elif tag in LEGACY_GLIBC:
            minors.append(LEGACY_GLIBC[tag])
        else:
            raise ToolError(f"{filename} has the platform tag {tag}, not manylinux x86_64")
    return min(minors)


def pip_environment(home: Path) -> dict[str, str]:
    """Return the environment pip runs in: nothing but the network settings."""
    environment = {
        "PATH": "/usr/bin:/bin",
        "HOME": str(home),
        "LC_ALL": "C.UTF-8",
        "PIP_CONFIG_FILE": os.devnull,
    }
    for name in PASSED_ENVIRONMENT:
        if name in os.environ:
            environment[name] = os.environ[name]
    return environment


def ansible_requirement(version: str) -> str:
    """Return the pip requirement for a full version or for a minor line."""
    parts = version.split(".")
    if len(parts) == 3 and release_key(version):
        return f"ansible-core=={version}"
    if len(parts) == 2 and release_key(version):
        return f"ansible-core>={version},<{parts[0]}.{int(parts[1]) + 1}"
    raise ToolError(f"--ansible-core {version} is neither X.Y nor X.Y.Z")


def pip_report(wheel: Path, abi: str, requirement: str, area: Path) -> dict:
    """Run pip from its wheel to resolve one ABI without installing anything."""
    minor = abi.split(".")[1]
    report = area / f"report-{abi}.json"
    command = [
        sys.executable,
        "-I",
        f"{wheel}/pip",
        "install",
        "--isolated",
        "--dry-run",
        "--ignore-installed",
        "--only-binary=:all:",
        *(argument for platform in PLATFORMS for argument in ("--platform", platform)),
        "--implementation",
        "cp",
        "--python-version",
        abi,
        "--abi",
        f"cp3{minor}",
        "--no-input",
        "--disable-pip-version-check",
        "--quiet",
        "--target",
        str(area / f"target-{abi}"),
        "--report",
        str(report),
        requirement,
        "urllib3",
    ]
    done = subprocess.run(
        command,
        env=pip_environment(area),
        stdin=subprocess.DEVNULL,
        capture_output=True,
        text=True,
        check=False,
    )
    if done.returncode != 0:
        raise ToolError(f"pip failed for Python {abi}: {done.stderr.strip()}")
    return json.loads(report.read_text(encoding="utf-8"))


def report_files(report: dict, abi: str) -> list[dict]:
    """Take each resolved file's identity and hash from a pip report, leaving
    out the omitted distributions."""
    files = []
    for entry in report.get("install", []):
        info = entry.get("download_info", {})
        sha256 = info.get("archive_info", {}).get("hashes", {}).get("sha256")
        metadata = entry.get("metadata", {})
        if normalize(metadata.get("name", "")) in OMITTED:
            continue
        if not sha256 or "url" not in info:
            raise ToolError(f"Python {abi}: {metadata.get('name')} has no hashed archive")
        check_pythonhosted(info["url"])
        files.append(
            {
                "name": normalize(metadata["name"]),
                "version": metadata["version"],
                "filename": filename_of(info["url"]),
                "url": info["url"],
                "sha256": sha256,
            }
        )
    names = {file["name"] for file in files}
    if not {"ansible-core", "urllib3"} <= names:
        raise ToolError(f"Python {abi}: the report lacks ansible-core or urllib3")
    return sorted(files, key=lambda file: file["name"])


def installer_file(sources: Sources, spec: str, area: Path) -> tuple[dict, Path]:
    """Fetch the installer wheel by the sha256 the index publishes for it."""
    name, _, version = spec.partition("==")
    if name != "pip" or not release_key(version):
        raise ToolError(f"--installer {spec} is not pip==X.Y[.Z]")
    filename = f"pip-{version}-py3-none-any.whl"
    for entry in sources.index(name, version).get("urls", []):
        if entry.get("filename") == filename and entry.get("packagetype") == "bdist_wheel":
            check_pythonhosted(entry["url"])
            path = area / filename
            sha256, size = download(sources.file_url(entry["url"]), path)
            if sha256 != entry["digests"]["sha256"] or size != entry["size"]:
                raise ToolError(f"{filename} differs from the index's size or sha256")
            file = {
                "name": name,
                "version": version,
                "filename": filename,
                "url": entry["url"],
                "sha256": sha256,
                "size": size,
            }
            return file, path
    raise ToolError(f"the index has no {filename}")


def confirm(sources: Sources, files: dict[str, dict], keep: Path) -> dict[str, int]:
    """Download every file into keep and return its size, refusing a different sha256."""
    sizes = {}

    def one(url: str) -> tuple[str, int]:
        sha256, size = download(sources.file_url(url), kept(keep, files[url]))
        if sha256 != files[url]["sha256"]:
            raise ToolError(f"{files[url]['filename']} hashes to {sha256}, not {files[url]['sha256']}")
        return url, size

    with concurrent.futures.ThreadPoolExecutor(WORKERS) as pool:
        for url, size in pool.map(one, sorted(files)):
            sizes[url] = size
    return sizes


def kept(keep: Path, file: dict) -> Path:
    """Return where a downloaded file stays while its license is read."""
    path = keep / file["sha256"] / file["filename"]
    path.parent.mkdir(parents=True, exist_ok=True)
    return path


def wheel_license(wheel: Path) -> tuple[str, str] | None:
    """Return the SPDX expression a wheel declares and where it says so, or None
    when its metadata names none without ambiguity."""
    try:
        with zipfile.ZipFile(wheel) as archive:
            names = [name for name in archive.namelist() if name.endswith(".dist-info/METADATA") and name.count("/") == 1]
            if len(names) != 1:
                raise ToolError(f"{wheel.name} holds {len(names)} METADATA files, not one")
            message = email.parser.BytesParser().parsebytes(archive.read(names[0]), headersonly=True)
    except (OSError, zipfile.BadZipFile) as error:
        raise ToolError(f"{wheel.name} is no readable wheel: {error}") from error
    expression = message.get("License-Expression")
    if expression is not None:
        if not SPDX_EXPRESSION.fullmatch(expression):
            raise ToolError(f"{wheel.name} declares the License-Expression {expression!r}, which is no plain SPDX expression")
        return expression, "License-Expression"
    declared = [value for value in message.get_all("Classifier", []) if value.startswith("License ::")]
    named = {CLASSIFIERS[value] for value in declared if value in CLASSIFIERS}
    if len(named) == 1 and all(value in CLASSIFIERS for value in declared):
        return named.pop(), "Classifier"
    return None


def read_licenses(path: Path) -> dict[tuple[str, str], dict]:
    """Read the license table, or an empty one where there is none yet."""
    if not path.exists():
        return {}
    try:
        document = json.loads(path.read_text(encoding="utf-8"))
        if document["format"] != LICENSES_FORMAT:
            raise ToolError(f"{path} is not a {LICENSES_FORMAT} table")
        return {(entry["name"], entry["version"]): entry for entry in document["distributions"]}
    except (OSError, ValueError, KeyError, TypeError) as error:
        raise ToolError(f"cannot read the license table {path}: {error}") from error


def reviewed(entry: dict | None) -> bool:
    """Say whether a person recorded the entry, as an ambiguous wheel needs."""
    return bool(entry) and str(entry.get("source", "")).startswith("reviewed:") and bool(entry.get("license"))


def license_table(wheels: dict[tuple[str, str], Path], previous: dict[tuple[str, str], dict]) -> str:
    """Return the table text naming every distribution's license: what its
    metadata declares, else what a person recorded for that exact release."""
    entries = []
    for (name, version), wheel in sorted(wheels.items()):
        declared = wheel_license(wheel)
        if declared is not None:
            entries.append({"name": name, "version": version, "license": declared[0], "source": declared[1]})
        elif reviewed(previous.get((name, version))):
            entries.append({key: previous[(name, version)][key] for key in ("name", "version", "license", "source")})
        else:
            raise ToolError(
                f"{wheel.name} declares no License-Expression and no unambiguous license classifier; read its license text, "
                f'record {{"name": "{name}", "version": "{version}", "license": <SPDX id>, "source": "reviewed: <why>"}} '
                "in the license table and run again"
            )
    return json.dumps({"format": LICENSES_FORMAT, "distributions": entries}, indent=2) + "\n"


def parse_tested(values: list[str]) -> list[dict]:
    """Read repeated id:v1,v2 arguments."""
    tested = []
    for value in values:
        release, _, versions = value.partition(":")
        listed = versions.split(",")
        if (
            not re.fullmatch(r"[a-z0-9][a-z0-9_-]*", release)
            or not all(release_key(version) for version in listed)
            or any(entry["id"] == release for entry in tested)
        ):
            raise ToolError(f"--tested {value} is not a distinct id:version[,version]")
        tested.append({"id": release, "versions": listed})
    return tested


def previous_tested(output: Path) -> list[dict]:
    """Return the tested releases of the lock about to be replaced."""
    if not output.exists():
        return []
    try:
        tested = json.loads(output.read_text(encoding="utf-8"))["tested"]
    except (ValueError, KeyError, TypeError) as error:
        raise ToolError(f"{output} is no lock to carry tested releases from: {error}") from error
    return tested


def parse_abis(value: str) -> list[str]:
    """Read the ABI list, newest first."""
    abis = value.split(",")
    if not all(re.fullmatch(r"3\.[0-9]{1,3}", abi) for abi in abis) or len(set(abis)) != len(abis):
        raise ToolError(f"--python {value} is not a list of distinct 3.N versions")
    return sorted(abis, key=lambda abi: -int(abi.split(".")[1]))


def generate(arguments: argparse.Namespace) -> int:
    """Resolve, download and write the lock."""
    abis = parse_abis(arguments.python)
    requirement = ansible_requirement(arguments.ansible_core)
    sources = Sources(arguments.pypi, arguments.files)
    output = Path(arguments.output)
    tested = parse_tested(arguments.tested) if arguments.tested else previous_tested(output)
    recorded = json.loads(Path(arguments.report).read_text(encoding="utf-8")) if arguments.report else None
    licenses = Path(arguments.licenses) if arguments.licenses else output.with_name("licenses.json")
    with tempfile.TemporaryDirectory(prefix="runtime-lock-") as scratch:
        area = Path(scratch)
        installer, wheel = installer_file(sources, arguments.installer, area)
        if recorded is not None:
            missing = [abi for abi in abis if abi not in recorded]
            if missing:
                raise ToolError(f"--report holds no report for Python {', '.join(missing)}")
            reports = {abi: recorded[abi] for abi in abis}
        else:
            with concurrent.futures.ThreadPoolExecutor(len(abis)) as pool:
                resolved = pool.map(lambda abi: pip_report(wheel, abi, requirement, area), abis)
                reports = dict(zip(abis, resolved))
        per_abi = {abi: report_files(reports[abi], abi) for abi in abis}
        unique = {file["url"]: file for files in per_abi.values() for file in files}
        sizes = confirm(sources, unique, area / "kept")
        wheels = {(file["name"], file["version"]): kept(area / "kept", file) for file in unique.values()}
        wheels[(installer["name"], installer["version"])] = wheel
        table = license_table(wheels, read_licenses(licenses))
    versions = {file["version"] for files in per_abi.values() for file in files if file["name"] == "ansible-core"}
    if len(versions) != 1:
        raise ToolError(f"the ABIs resolve ansible-core to {sorted(versions)}")
    floor = max(
        (glibc_needed(file["filename"]) or 0 for files in per_abi.values() for file in files),
        default=0,
    )
    if not floor:
        raise ToolError("no binary wheel was locked, so there is no glibc floor")
    if (2, floor) > MAX_GLIBC:
        raise ToolError(f"the files need glibc 2.{floor}, above 2.{MAX_GLIBC[1]}")
    lock = {
        "format": FORMAT,
        "ansibleCore": versions.pop(),
        "pythonABIs": abis,
        "glibcFloor": f"2.{floor}",
        "tested": tested,
        "installer": installer,
        "abis": {
            abi: [{**file, "size": sizes[file["url"]]} for file in per_abi[abi]]
            for abi in sorted(abis)
        },
    }
    write_atomically(licenses, table)
    write_atomically(output, json.dumps(lock, indent=2) + "\n")
    return 0


def write_atomically(path: Path, text: str) -> None:
    """Replace a file without ever leaving half of it."""
    path.parent.mkdir(parents=True, exist_ok=True)
    handle, name = tempfile.mkstemp(dir=path.parent, prefix=".runtime-lock-")
    try:
        with os.fdopen(handle, "w", encoding="utf-8") as sink:
            sink.write(text)
        os.replace(name, path)
    except BaseException:
        Path(name).unlink(missing_ok=True)
        raise


def load_lock(path: str) -> dict:
    """Read a lock far enough to walk its files."""
    try:
        lock = json.loads(Path(path).read_text(encoding="utf-8"))
        if lock["format"] != FORMAT:
            raise ToolError(f"{path} is not a {FORMAT} lock")
        return lock
    except (OSError, ValueError, KeyError, TypeError) as error:
        raise ToolError(f"cannot read the lock {path}: {error}") from error


def locked_files(lock: dict) -> list[dict]:
    """Return the installer and every ABI's files."""
    return [lock["installer"], *(file for files in lock["abis"].values() for file in files)]


def verify(arguments: argparse.Namespace) -> int:
    """Download every locked file and compare its size, sha256 and license."""
    sources = Sources(arguments.pypi, arguments.files)
    lock_path = Path(arguments.lock)
    files = {file["url"]: file for file in locked_files(load_lock(arguments.lock))}
    licenses = read_licenses(Path(arguments.licenses) if arguments.licenses else lock_path.with_name("licenses.json"))
    failures = [
        f"{name} {version} is in the license table and not in the lock"
        for name, version in sorted(licenses.keys() - {(file["name"], file["version"]) for file in files.values()})
    ]

    with tempfile.TemporaryDirectory(prefix="runtime-lock-") as scratch:
        keep = Path(scratch)

        def one(url: str) -> None:
            file = files[url]
            try:
                check_pythonhosted(url)
                sha256, size = download(sources.file_url(url), kept(keep, file))
            except ToolError as error:
                failures.append(f"{file['filename']}: {error}")
                return
            if sha256 != file["sha256"] or size != file["size"]:
                failures.append(
                    f"{file['filename']}: got {size} bytes with sha256 {sha256}, "
                    f"the lock has {file['size']} and {file['sha256']}"
                )
                return
            entry = licenses.get((file["name"], file["version"]))
            try:
                declared = wheel_license(kept(keep, file))
            except ToolError as error:
                failures.append(f"{file['filename']}: {error}")
                return
            if entry is None:
                failures.append(f"{file['filename']}: the license table has no entry for {file['name']} {file['version']}")
            elif declared is None and not reviewed(entry):
                failures.append(f"{file['filename']}: declares no license, so the table's entry must be a reviewed one")
            elif declared is not None and (declared[0], declared[1]) != (entry.get("license"), entry.get("source")):
                failures.append(
                    f"{file['filename']}: declares {declared[0]} in {declared[1]}, the license table has "
                    f"{entry.get('license')} from {entry.get('source')}"
                )

        with concurrent.futures.ThreadPoolExecutor(WORKERS) as pool:
            list(pool.map(one, sorted(files)))
    for failure in sorted(failures):
        print(f"runtime-lock: {failure}", file=sys.stderr)
    if failures:
        return 1
    print(f"verified {len(files)} files")
    return 0


def newest_release(index: dict, minor_line: tuple[int, ...] | None) -> str | None:
    """Return the newest unyanked plain release, within a line if one is given."""
    best = None
    for version, uploads in index.get("releases", {}).items():
        key = release_key(version)
        if key is None or not uploads or all(upload.get("yanked") for upload in uploads):
            continue
        if minor_line is not None and key[: len(minor_line)] != minor_line:
            continue
        if best is None or key > best[0]:
            best = (key, version)
    return best[1] if best else None


def outdated(arguments: argparse.Namespace) -> int:
    """Print newer releases of what the lock holds; it never fails on them."""
    sources = Sources(arguments.pypi, arguments.files)
    lock = load_lock(arguments.lock)
    locked: dict[str, set[str]] = {}
    for file in locked_files(lock):
        locked.setdefault(file["name"], set()).add(file["version"])

    def one(name: str) -> str | None:
        try:
            index = sources.index(name)
        except ToolError as error:
            return f"- {name}: could not check ({error})"
        line = None
        if name == "ansible-core":
            line = tuple(int(part) for part in lock["ansibleCore"].split(".")[:2])
        newest = newest_release(index, line)
        current = max(locked[name], key=lambda version: release_key(version) or ())
        if newest and (release_key(newest) or ()) > (release_key(current) or ()):
            return f"- {name} {current} -> {newest}"
        return None

    with concurrent.futures.ThreadPoolExecutor(WORKERS) as pool:
        lines = [line for line in pool.map(one, sorted(locked)) if line]
    print("\n".join(lines) if lines else "Every locked release is the newest of its line.")
    return 0


def parser() -> argparse.ArgumentParser:
    """Build the command line."""
    root = argparse.ArgumentParser(prog="runtime-lock", description=__doc__.splitlines()[0])
    commands = root.add_subparsers(dest="command", required=True)

    def sourced(command: argparse.ArgumentParser) -> None:
        command.add_argument("--pypi", default=PYPI, help="index root; a mirror serves tests")
        command.add_argument("--files", default=FILES, help="file root; a mirror serves tests")

    make = commands.add_parser("generate", help="resolve and write the lock (network)")
    sourced(make)
    make.add_argument("--ansible-core", required=True, help="X.Y or X.Y.Z")
    make.add_argument("--python", required=True, help="comma-separated 3.N ABIs")
    make.add_argument("--installer", required=True, help="pip==X.Y.Z")
    make.add_argument("--tested", action="append", default=[], help="id:v1,v2; repeatable")
    make.add_argument("--output", default=str(DEFAULT_LOCK))
    make.add_argument("--licenses", help="the license table; beside --output by default")
    make.add_argument("--report", help="JSON of ABI -> pip report, in place of running pip")
    make.set_defaults(run=generate)
    for name, run, summary in (
        ("verify", verify, "download every locked file and compare it (network)"),
        ("outdated", outdated, "print newer releases, advisory (network)"),
    ):
        command = commands.add_parser(name, help=summary)
        sourced(command)
        command.add_argument("--lock", default=str(DEFAULT_LOCK))
        if name == "verify":
            command.add_argument("--licenses", help="the license table; beside --lock by default")
        command.set_defaults(run=run)
    return root


def main(argv: list[str] | None = None) -> int:
    """Run one subcommand and report a failure as one line."""
    arguments = parser().parse_args(argv)
    try:
        return arguments.run(arguments)
    except ToolError as error:
        print(f"runtime-lock: {error}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
