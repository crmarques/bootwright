"""The media block refuses a layout the install block's walk would refuse, naming the directory."""

import json
import os

import pytest
from ansible.module_utils.testing import patch_module_args

from ansible_collections.bootwright.core.plugins.module_utils import containercluster_publication as publication
from ansible_collections.bootwright.core.plugins.modules import containercluster_media_layout as layout
from ansible_collections.bootwright.core.plugins.modules import containercluster_media_inspect as inspect


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


def pushed(tmp_path):
    work = tmp_path / 'work'
    work.mkdir(mode=0o700)
    image = work / 'agent.iso'
    image.write_bytes(b'agent image')
    image.chmod(0o600)
    return {'deliveryKind': 'push', 'workRoot': str(work), 'image': {'path': str(image), 'url': ''}}


@pytest.mark.parametrize('operation', ['publish', 'remove'])
def test_pushed_media_uses_no_publication_layout(tmp_path, capsys, operation):
    frozen = pushed(tmp_path)
    result = run({'request': frozen, 'operation': operation}, capsys)[0]
    assert result['present'] and result['changed'] is False
    assert not (tmp_path / 'services').exists()


def test_pushed_media_replay_reads_only_the_private_image_and_bounded_receipt(tmp_path):
    frozen = pushed(tmp_path)
    receipt = tmp_path / 'work' / inspect.RECEIPT
    receipt.write_text(json.dumps({'inputs': 'a' * 64, 'installer': '4.21.10'}))
    receipt.chmod(0o600)
    assert inspect.observe(frozen) == {
        'booted': False, 'image': True, 'inputs': 'a' * 64, 'installer': '4.21.10', 'work': True,
    }
    receipt.write_bytes(b'x' * (inspect.MAX_RECEIPT + 1))
    assert inspect.observe(frozen)['inputs'] == ''
    (tmp_path / 'work' / 'agent.iso').write_bytes(b'')
    assert inspect.observe(frozen)['image'] is False


@pytest.mark.parametrize('problem', [
    'image-link', 'receipt-link', 'writable-image', 'readable-image',
    'readable-receipt', 'open-work', 'work-link', 'foreign-path',
])
def test_pushed_media_refuses_unsafe_private_paths(tmp_path, capsys, problem):
    frozen = pushed(tmp_path)
    image = tmp_path / 'work' / 'agent.iso'
    if problem == 'image-link':
        image.unlink()
        image.symlink_to(tmp_path / 'foreign')
    elif problem == 'receipt-link':
        (image.parent / 'bootwright-media.json').symlink_to(tmp_path / 'foreign')
    elif problem == 'writable-image':
        image.chmod(0o666)
    elif problem == 'readable-image':
        image.chmod(0o644)
    elif problem == 'readable-receipt':
        recorded = image.parent / inspect.RECEIPT
        recorded.write_text('{}')
        recorded.chmod(0o644)
    elif problem == 'open-work':
        image.parent.chmod(0o755)
    elif problem == 'work-link':
        image.parent.rename(tmp_path / 'moved')
        image.parent.symlink_to(tmp_path / 'moved')
    else:
        frozen['image']['path'] = str(tmp_path / 'foreign')
    result = run({'request': frozen, 'operation': 'publish'}, capsys)[0]
    assert result['failed']
    assert not (tmp_path / 'foreign').exists()
