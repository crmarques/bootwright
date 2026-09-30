"""The inspection reports the package tree complete and present apart.

The apply publishes the tree by one rename, so it appears whole with its
.treeinfo. A removal deletes it with the file module's state=absent, which
removes a directory through shutil.rmtree in the order the directory lists its
entries (ansible/modules/file.py, ensure_absent, in ansible-core 2.21), so a
removal stopped part way can leave the directory without its marker. That is
content the removal still takes back, and reading the marker alone would call
it withdrawn.
"""

from __future__ import annotations

from ansible_collections.bootwright.core.plugins.modules.managedos_install_inspect import observe


def request(root, tree=True):
    value = {"image": {"path": str(root / "os" / "rhel-01" / "install.iso")}}
    if tree:
        value["tree"] = {"path": str(root / "os" / "rhel-9-8" / "tree")}
    return value


def test_a_published_tree_is_complete_and_present(tmp_path):
    tree = tmp_path / "os" / "rhel-9-8" / "tree"
    (tree / "BaseOS").mkdir(parents=True)
    (tree / ".treeinfo").write_text("[general]\n")
    observed = observe(request(tmp_path))
    assert observed["tree"] is True
    assert observed["treeContent"] is True


def test_a_tree_a_removal_stopped_in_is_present_though_not_complete(tmp_path):
    (tmp_path / "os" / "rhel-9-8" / "tree" / "AppStream" / "Packages").mkdir(parents=True)
    observed = observe(request(tmp_path))
    assert observed["tree"] is False
    assert observed["treeContent"] is True


def test_an_empty_tree_directory_is_still_content(tmp_path):
    (tmp_path / "os" / "rhel-9-8" / "tree").mkdir(parents=True)
    assert observe(request(tmp_path))["treeContent"] is True


def test_a_tree_that_is_gone_is_neither(tmp_path):
    observed = observe(request(tmp_path))
    assert observed["tree"] is False
    assert observed["treeContent"] is False


def test_a_request_without_a_tree_reports_none(tmp_path):
    (tmp_path / "os" / "rhel-9-8" / "tree").mkdir(parents=True)
    observed = observe(request(tmp_path, tree=False))
    assert observed["tree"] is False
    assert observed["treeContent"] is False
