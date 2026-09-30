"""The inspection reports the package tree complete and present apart.

The apply publishes the tree by one rename, so it appears whole with its
.treeinfo. A removal deletes it with the file module's state=absent, which
removes a directory through shutil.rmtree in the order the directory lists its
entries (ansible/modules/file.py, ensure_absent, in ansible-core 2.21), so the
removal withdraws the marker first and one stopped part way leaves the rest of
the directory without it. That is content the removal still takes back, and
reading the marker alone would call it withdrawn.

An apply killed part way leaves the staging copy it was extracting beside the
tree and the work area it builds the image in, and both are reported, so
neither outlives a removal unseen.
"""

from __future__ import annotations

from ansible_collections.bootwright.core.plugins.modules.managedos_install_inspect import observe


def request(root, tree=True):
    value = {"image": {"path": str(root / "os" / "rhel-01" / "install.iso")}}
    if tree:
        value["tree"] = {"path": str(root / "os" / "rhel-9-8" / "tree")}
    return value


def inspect(root, tree=True):
    """The observation over root, given the staging and work paths the role
    derives: none for the staging tree when the request hosts no tree."""
    staging = str(root / "os" / "rhel-9-8" / "tree.staging") if tree else ""
    return observe(request(root, tree), staging, str(root / "work"))


def test_a_published_tree_is_complete_and_present(tmp_path):
    tree = tmp_path / "os" / "rhel-9-8" / "tree"
    (tree / "BaseOS").mkdir(parents=True)
    (tree / ".treeinfo").write_text("[general]\n")
    observed = inspect(tmp_path)
    assert observed["tree"] is True
    assert observed["treeContent"] is True


def test_a_tree_a_removal_stopped_in_is_present_though_not_complete(tmp_path):
    (tmp_path / "os" / "rhel-9-8" / "tree" / "AppStream" / "Packages").mkdir(parents=True)
    observed = inspect(tmp_path)
    assert observed["tree"] is False
    assert observed["treeContent"] is True


def test_an_empty_tree_directory_is_still_content(tmp_path):
    (tmp_path / "os" / "rhel-9-8" / "tree").mkdir(parents=True)
    assert inspect(tmp_path)["treeContent"] is True


def test_a_tree_that_is_gone_is_neither(tmp_path):
    observed = inspect(tmp_path)
    assert observed["tree"] is False
    assert observed["treeContent"] is False


def test_a_request_without_a_tree_reports_none(tmp_path):
    (tmp_path / "os" / "rhel-9-8" / "tree").mkdir(parents=True)
    observed = inspect(tmp_path, tree=False)
    assert observed["tree"] is False
    assert observed["treeContent"] is False
    assert observed["treeStaging"] is False


# An apply killed while xorriso extracted leaves a partial copy beneath the
# served root, and one killed after its first write the work area with the
# rendered Kickstart in it.
def test_what_a_killed_apply_leaves_is_reported(tmp_path):
    (tmp_path / "os" / "rhel-9-8" / "tree.staging" / "BaseOS").mkdir(parents=True)
    (tmp_path / "work").mkdir()
    (tmp_path / "work" / "ks.cfg").write_text("text\n")
    observed = inspect(tmp_path)
    assert observed["treeStaging"] is True
    assert observed["work"] is True
    assert observed["tree"] is False
    assert observed["treeContent"] is False


def test_nothing_left_reports_no_staging_tree_and_no_work_area(tmp_path):
    observed = inspect(tmp_path)
    assert observed["treeStaging"] is False
    assert observed["work"] is False
