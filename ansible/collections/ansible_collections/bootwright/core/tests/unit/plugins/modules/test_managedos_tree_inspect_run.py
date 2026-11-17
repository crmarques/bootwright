"""The managedos_tree_inspect module's main() reads a temporary package tree, as a task would run it."""

from __future__ import annotations

from ansible_collections.bootwright.core.plugins.modules import managedos_tree_inspect as module

IDENTITY = "a" * 64


def request(path, frozen=IDENTITY):
    return {"request": {"publication": {"path": str(path)}, "media": {"sha256": frozen}}}


def test_a_complete_tree_of_the_frozen_identity_is_observed_without_a_change(tmp_path, run_module):
    tree = tmp_path / "tree"
    tree.mkdir()
    (tree / ".bootwright-tree-identity").write_text(IDENTITY + "\n", encoding="ascii")
    (tree / ".treeinfo").write_text("", encoding="ascii")
    result = run_module(module, request(tree))
    assert result["changed"] is False
    assert result["observation"] == {"content": True, "identity": IDENTITY, "staging": False, "tree": True}


def test_an_absent_tree_and_a_tree_of_another_identity_are_observed_as_not_ready(tmp_path, run_module):
    absent = run_module(module, request(tmp_path / "absent"))
    assert absent["observation"] == {"content": False, "identity": "", "staging": False, "tree": False}
    other = tmp_path / "other"
    other.mkdir()
    (other / ".bootwright-tree-identity").write_text("b" * 64, encoding="ascii")
    (other / ".treeinfo").write_text("", encoding="ascii")
    observed = run_module(module, request(other))["observation"]
    assert observed["identity"] == "b" * 64 and observed["tree"] is False


def test_a_tree_path_that_is_a_symbolic_link_fails_the_task(tmp_path, run_module):
    real = tmp_path / "real"
    real.mkdir()
    link = tmp_path / "link"
    link.symlink_to(real)
    result = run_module(module, request(link))
    assert result["failed"] is True and result["msg"] == "the package tree could not be inspected"
