"""The media block refuses a layout the install block's walk would refuse, naming the directory."""

import json
import os

import pytest
from ansible.module_utils.testing import patch_module_args

from ansible_collections.bootwright.core.plugins.module_utils import containercluster_publication as publication
from ansible_collections.bootwright.core.plugins.modules import containercluster_media_layout as layout


TOKEN = '0123456789abcdef' * 4
URL = 'https://192.0.2.1:8443/private/clusters/cluster'


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


def request(tmp_path):
    content = tmp_path / 'services' / 'ctx' / 'artifact-server' / 'server'
    directory = content / 'public' / 'private' / 'clusters' / 'cluster' / TOKEN
    directory.mkdir(parents=True)
    (directory / 'agent.iso').write_bytes(b'image')
    return {'image': {'path': str(directory.parent), 'url': URL}}


def run(arguments, capsys):
    capsys.readouterr()
    with patch_module_args(arguments), pytest.raises(SystemExit):
        layout.main()
    output = capsys.readouterr().out
    return json.loads(output), output


@pytest.mark.parametrize('operation', ['publish', 'remove'])
def test_an_accepted_layout_changes_nothing(tmp_path, capsys, operation):
    frozen = request(tmp_path)
    (tmp_path / 'services').rename(tmp_path / 'disk')
    os.symlink('disk', tmp_path / 'services')
    result = run({'request': frozen, 'operation': operation}, capsys)[0]
    assert not result.get('failed') and result['changed'] is False and result['present'] is True
    assert (tmp_path / 'disk' / 'ctx').is_dir()


@pytest.mark.parametrize('operation,effect', [('publish', 'published'), ('remove', 'removed')])
def test_a_refusal_names_the_directory_and_no_url_or_token(tmp_path, capsys, operation, effect):
    frozen = request(tmp_path)
    private = tmp_path / 'services' / 'ctx' / 'artifact-server' / 'server' / 'public' / 'private'
    private.rename(private.with_name('moved'))
    os.symlink('moved', private)
    result, output = run({'request': frozen, 'operation': operation}, capsys)
    assert result['failed']
    assert str(private) + ' is a symbolic link' in result['msg']
    assert 'Nothing was %s' % effect in result['msg']
    for secret in ['https://', '192.0.2.1', TOKEN, 'insert-']:
        assert secret not in output
    os.unlink(private)
    private.with_name('moved').rename(private)
    os.symlink(TOKEN, private / 'clusters' / 'cluster' / ('insert-' + 'b' * 64))
    result, output = run({'request': frozen, 'operation': operation}, capsys)
    assert result['failed'] and frozen['image']['path'] + ' holds a symbolic link' in result['msg']
    for secret in ['https://', TOKEN, 'insert-']:
        assert secret not in output
