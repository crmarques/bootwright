"""The media block proves the layout the installation block's walk accepts."""

import os

import pytest

from ansible_collections.bootwright.core.plugins.module_utils import containercluster_publication as publication


RESERVED = ('public', 'private', 'clusters', 'cluster')
TOKEN = '0123456789abcdef' * 4


@pytest.fixture(autouse=True)
def trust_the_test_tree(tmp_path, monkeypatch):
    system = set()
    for parent in tmp_path.parents:
        info = os.stat(parent)
        system.add((info.st_dev, info.st_ino))
    original = publication.trusted

    def trusted(fd, path):
        info = os.fstat(fd)
        if (info.st_dev, info.st_ino) not in system:
            original(fd, path)

    monkeypatch.setattr(publication, 'OWNER', os.getuid())
    monkeypatch.setattr(publication, 'trusted', trusted)


def content(tmp_path):
    return tmp_path / 'services' / 'ctx' / 'artifact-server' / 'server'


def reserved(tmp_path):
    return content(tmp_path).joinpath(*RESERVED)


def published(tmp_path):
    directory = reserved(tmp_path) / TOKEN
    directory.mkdir(parents=True)
    (directory / 'agent.iso').write_bytes(b'image')
    return str(reserved(tmp_path))


def moved(tmp_path, link):
    disk = tmp_path / 'disk'
    disk.mkdir()
    (tmp_path / 'services').rename(disk / 'services')
    os.symlink(link, tmp_path / 'services')
    return disk


def refused(path, removal=False):
    with pytest.raises(publication.LayoutError) as caught:
        publication.prove(path, removal)
    assert TOKEN not in str(caught.value) and 'insert-' not in str(caught.value)
    return caught.value


@pytest.mark.parametrize('depth', range(len(RESERVED) + 1))
def test_a_symlink_from_the_content_root_down_refuses_naming_it(tmp_path, depth):
    path = published(tmp_path)
    component = content(tmp_path).joinpath(*RESERVED[:depth])
    component.rename(component.with_name('moved'))
    os.symlink('moved', component)
    for removal in (False, True):
        error = refused(path, removal)
        assert error.directory == str(component)
        assert 'is a symbolic link' in str(error)


@pytest.mark.parametrize('level', ['entry', 'image'])
def test_a_symlink_beneath_the_reserved_root_refuses_naming_the_reserved_root(tmp_path, level):
    path = published(tmp_path)
    if level == 'entry':
        os.symlink(TOKEN, reserved(tmp_path) / ('insert-' + 'a' * 64))
    else:
        image = reserved(tmp_path) / TOKEN / 'agent.iso'
        image.rename(tmp_path / 'image')
        os.symlink(tmp_path / 'image', image)
    for removal in (False, True):
        assert refused(path, removal).directory == path


@pytest.mark.parametrize('mode', [0o775, 0o757, 0o1777])
def test_a_resolved_ancestor_others_can_write_refuses_naming_it(tmp_path, mode):
    path = published(tmp_path)
    disk = moved(tmp_path, 'disk/services')
    disk.chmod(mode)
    error = refused(path)
    assert error.directory == str(disk) and 'writable by its group or by others' in str(error)
    disk.chmod(0o755)
    (disk / 'services' / 'ctx').chmod(mode)
    assert refused(path, True).directory == str(disk / 'services' / 'ctx')


def test_a_resolved_ancestor_root_does_not_own_refuses_naming_it(tmp_path, monkeypatch):
    path = published(tmp_path)
    moved(tmp_path, 'disk/services')
    monkeypatch.setattr(publication, 'OWNER', os.getuid() + 1)
    error = refused(path)
    assert error.directory == str(tmp_path) and 'not owned by root' in str(error)


@pytest.mark.parametrize('depth', range(len(RESERVED) + 1))
def test_a_writable_directory_from_the_content_root_down_refuses_naming_it(tmp_path, depth):
    path = published(tmp_path)
    component = content(tmp_path).joinpath(*RESERVED[:depth])
    component.chmod(0o777)
    for removal in (False, True):
        error = refused(path, removal)
        assert error.directory == str(component) and 'writable by its group or by others' in str(error)


@pytest.mark.parametrize('link', ['absolute', 'relative', 'chained', 'parent'])
def test_a_symlinked_ancestor_is_accepted(tmp_path, link):
    path = published(tmp_path)
    disk = tmp_path / 'disk'
    target = {'absolute': str(disk / 'services'), 'relative': 'disk/services',
              'chained': 'hop', 'parent': 'disk/../disk/services'}[link]
    moved(tmp_path, target)
    if link == 'chained':
        os.symlink(disk / 'services', tmp_path / 'hop')
    assert publication.prove(path) is True
    assert publication.prove(path, True) is True


def test_a_directory_missing_beneath_the_content_root_is_accepted(tmp_path):
    path = str(reserved(tmp_path))
    content(tmp_path).mkdir(parents=True)
    assert publication.prove(path) is False
    (content(tmp_path) / 'public' / 'private').mkdir(parents=True)
    assert publication.prove(path) is False
    assert publication.prove(path, True) is False


@pytest.mark.parametrize('missing', ['content', 'ancestor'])
def test_a_missing_content_root_refuses_a_publication_and_is_nothing_to_remove(tmp_path, missing):
    path = str(reserved(tmp_path))
    (content(tmp_path).parent if missing == 'content' else tmp_path).mkdir(parents=True, exist_ok=True)
    error = refused(path)
    assert error.directory == str(content(tmp_path)) and 'does not exist' in str(error)
    assert publication.prove(path, True) is False


def test_a_root_outside_the_reserved_shape_refuses(tmp_path):
    for path in [str(tmp_path / 'other'), str(content(tmp_path) / 'public' / 'clusters' / 'cluster' / 'x'),
                 str(reserved(tmp_path)) + '/../cluster', 'relative/public/private/clusters/cluster']:
        with pytest.raises(ValueError):
            publication.prove(path)
