"""The managedos_install_inspect module's main() reads a temporary served root, as a task would run it."""

from __future__ import annotations

from ansible_collections.bootwright.core.plugins.modules import managedos_install_inspect as module

IDENTITY = "c" * 64


def request(root, frozen=IDENTITY):
    return {"image": {"path": str(root / "install.iso")}, "tree": {"path": str(root / "tree")}, "treeMedia": {"sha256": frozen},
            "private": {"path": str(root / "private")}}


def test_nothing_published_observes_as_all_absent_and_changes_nothing(tmp_path, run_module):
    result = run_module(module, {"request": request(tmp_path), "staging": str(tmp_path / "tree.staging"), "work": str(tmp_path / "work")})
    assert result["changed"] is False and not result.get("failed")
    assert result["observation"] == {"image": False, "private": False, "tree": False, "treeIdentity": "", "treeContent": False,
                                     "treeStaging": False, "work": False}


def test_a_published_image_a_complete_tree_of_the_frozen_identity_and_remnants_are_each_reported(tmp_path, run_module):
    (tmp_path / "install.iso").write_bytes(b"")
    tree = tmp_path / "tree"
    tree.mkdir()
    (tree / ".treeinfo").write_text("", encoding="ascii")
    (tree / ".bootwright-tree-identity").write_text(IDENTITY, encoding="ascii")
    (tmp_path / "tree.staging").mkdir()
    (tmp_path / "work").mkdir()
    observation = run_module(module, {"request": request(tmp_path), "staging": str(tmp_path / "tree.staging"), "work": str(tmp_path / "work")})["observation"]
    assert observation["image"] is True and observation["tree"] is True and observation["treeIdentity"] == IDENTITY
    assert observation["treeContent"] is True and observation["treeStaging"] is True and observation["work"] is True and observation["private"] is False


def test_a_tree_extracted_from_another_image_is_present_but_never_complete(tmp_path, run_module):
    tree = tmp_path / "tree"
    tree.mkdir()
    (tree / ".treeinfo").write_text("", encoding="ascii")
    (tree / ".bootwright-tree-identity").write_text("d" * 64, encoding="ascii")
    observation = run_module(module, {"request": request(tmp_path), "work": str(tmp_path / "work")})["observation"]
    assert observation["treeContent"] is True and observation["tree"] is False and observation["treeIdentity"] == "d" * 64


def test_a_request_without_its_work_area_fails_on_the_argument(tmp_path, run_module):
    result = run_module(module, {"request": request(tmp_path)})
    assert result["failed"] is True and "work" in result["msg"]
