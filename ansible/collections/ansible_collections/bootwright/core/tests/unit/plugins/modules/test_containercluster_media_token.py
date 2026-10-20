"""Each insert owns a fresh hard link; only that file can be retired."""

import json
import os

import pytest
from ansible.module_utils.testing import patch_module_args

from ansible_collections.bootwright.core.plugins.modules import containercluster_media_token as token


RESERVED = ('public', 'private', 'clusters', 'cluster')


@pytest.fixture(autouse=True)
def trust_the_test_tree(tmp_path, monkeypatch):
    system = set()
    for parent in tmp_path.parents:
        info = os.stat(parent)
        system.add((info.st_dev, info.st_ino))
    original = token.trusted

    def trusted(fd):
        info = os.fstat(fd)
        if (info.st_dev, info.st_ino) not in system:
            original(fd)

    monkeypatch.setattr(token, 'OWNER', os.getuid())
    monkeypatch.setattr(token, 'trusted', trusted)


def content(tmp_path):
    return tmp_path / 'services' / 'ctx' / 'artifact-server' / 'server'


def reserved(tmp_path):
    return content(tmp_path).joinpath(*RESERVED)


def published(tmp_path):
    root = reserved(tmp_path)
    source = root / 'abcdef'
    source.mkdir(parents=True)
    (source / 'agent.iso').write_bytes(b'image')
    base = 'https://192.0.2.1/private/cluster'
    return {'publicationRoot': {'path': str(root), 'url': base}}, base + '/abcdef/agent.iso'


def moved(tmp_path, link):
    disk = tmp_path / 'disk'
    disk.mkdir()
    (tmp_path / 'services').rename(disk / 'services')
    os.symlink(link, tmp_path / 'services')
    return disk


def test_each_insert_is_unique_and_eject_removes_only_its_link(tmp_path):
    request, source = published(tmp_path)
    first = token.operate(request, source, '', 'publish')['url']
    second = token.operate(request, source, '', 'publish')['url']
    assert len(first.split("/")[-2].removeprefix("insert-")) == 64
    assert (reserved(tmp_path) / first.split("/")[-2]).stat().st_mode & 0o777 == 0o711
    assert first != second
    assert token.operate(request, source, first, 'inspect')['own']
    assert token.operate(request, source, second, 'inspect')['own']
    assert token.operate(request, source, first, 'remove')['changed']
    assert not token.operate(request, source, first, 'inspect')['own']
    assert token.operate(request, source, second, 'inspect')['own']
    assert token.operate(request, source, source, 'inspect')['own']
    assert not token.operate(request, source, source, 'remove')['changed']


def test_another_inode_under_reserved_root_is_foreign(tmp_path):
    request, source = published(tmp_path)
    other = reserved(tmp_path) / 'insert-foreign'
    other.mkdir()
    (other / 'agent.iso').write_bytes(b'image')
    url = request['publicationRoot']['url'] + '/insert-foreign/agent.iso'
    assert not token.operate(request, source, url, 'inspect')['own']
    assert not token.operate(request, source, url, 'remove')['changed']
    assert (other / 'agent.iso').exists()


def test_symlinked_token_is_never_owned(tmp_path):
    request, source = published(tmp_path)
    os.symlink('abcdef', reserved(tmp_path) / 'insert-link')
    url = request['publicationRoot']['url'] + '/insert-link/agent.iso'
    assert not token.operate(request, source, url, 'inspect')['own']


@pytest.mark.parametrize('depth', range(len(RESERVED) + 1))
def test_a_symlink_from_the_content_root_down_refuses(tmp_path, depth):
    request, source = published(tmp_path)
    component = content(tmp_path).joinpath(*RESERVED[:depth])
    component.rename(component.with_name('moved'))
    os.symlink('moved', component)
    for operation in ['inspect', 'publish', 'remove', 'sweep']:
        with pytest.raises(OSError):
            token.operate(request, source, source, operation)
    assert (reserved(tmp_path) / 'abcdef' / 'agent.iso').read_bytes() == b'image'


@pytest.mark.parametrize('link', ['absolute', 'relative', 'chained', 'parent'])
def test_a_symlinked_ancestor_inspects_publishes_and_removes(tmp_path, link):
    request, source = published(tmp_path)
    disk = tmp_path / 'disk'
    target = {'absolute': str(disk / 'services'), 'relative': 'disk/services',
              'chained': 'hop', 'parent': 'disk/../disk/services'}[link]
    moved(tmp_path, target)
    if link == 'chained':
        os.symlink(disk / 'services', tmp_path / 'hop')
    inserted = token.operate(request, source, '', 'publish')['url']
    assert token.operate(request, source, inserted, 'inspect')['own']
    assert token.operate(request, source, source, 'inspect')['own']
    stale = token.operate(request, source, '', 'publish')['url']
    assert token.operate(request, source, '', 'sweep', [inserted])['removed'] == 1
    assert not (reserved(tmp_path) / stale.split('/')[-2]).exists()
    assert token.operate(request, source, inserted, 'remove')['changed']
    assert sorted(os.listdir(reserved(tmp_path))) == ['abcdef']


@pytest.mark.parametrize('mode', [0o775, 0o757, 0o1777])
def test_a_resolved_ancestor_others_can_write_refuses(tmp_path, mode):
    request, source = published(tmp_path)
    disk = moved(tmp_path, 'disk/services')
    disk.chmod(mode)
    with pytest.raises(ValueError):
        token.operate(request, source, source, 'inspect')
    disk.chmod(0o755)
    (disk / 'services').chmod(mode)
    with pytest.raises(ValueError):
        token.operate(request, source, source, 'inspect')


def test_a_writable_directory_holding_the_link_refuses(tmp_path):
    request, source = published(tmp_path)
    moved(tmp_path, 'disk/services')
    tmp_path.chmod(0o777)
    try:
        with pytest.raises(ValueError):
            token.operate(request, source, source, 'inspect')
    finally:
        tmp_path.chmod(0o700)


def test_a_resolved_ancestor_root_does_not_own_refuses(tmp_path, monkeypatch):
    request, source = published(tmp_path)
    moved(tmp_path, 'disk/services')
    monkeypatch.setattr(token, 'OWNER', os.getuid() + 1)
    with pytest.raises(ValueError):
        token.operate(request, source, source, 'inspect')


def test_a_root_outside_the_reserved_shape_refuses(tmp_path):
    request, source = published(tmp_path)
    for path in [str(tmp_path / 'other'), str(content(tmp_path) / 'public' / 'clusters' / 'cluster' / 'x'),
                 str(reserved(tmp_path)) + '/../cluster']:
        request['publicationRoot']['path'] = path
        with pytest.raises(ValueError):
            token.operate(request, source, source, 'inspect')


def test_the_module_inspects_through_a_symlinked_ancestor(tmp_path, capsys):
    request, source = published(tmp_path)
    moved(tmp_path, 'disk/services')
    capsys.readouterr()
    with patch_module_args({'request': request, 'source': source, 'image': source, 'operation': 'inspect'}), pytest.raises(SystemExit):
        token.main()
    result = json.loads(capsys.readouterr().out)
    assert not result.get('failed') and result['own']


def test_foreign_origin_and_traversal_are_never_owned(tmp_path):
    request, source = published(tmp_path)
    for image in [source.replace('192.0.2.1', '192.0.2.2'), source.replace('abcdef', '..'), source + '?x=1']:
        assert not token.operate(request, source, image, 'inspect')['own']


def test_ejected_token_answers_404_while_other_insert_still_serves(tmp_path):
    from http.server import SimpleHTTPRequestHandler
    from io import BytesIO
    from types import SimpleNamespace
    from urllib.parse import urlsplit

    class Handler(SimpleHTTPRequestHandler):
        def log_message(self, format, *args):
            pass

    class Connection:
        def __init__(self, url):
            self.request = BytesIO(('GET %s HTTP/1.0\r\n\r\n' % urlsplit(url).path).encode())
            self.response = bytearray()

        def makefile(self, *args):
            return self.request

        def sendall(self, data):
            self.response.extend(data)

    def fetch(url):
        connection = Connection(url)
        Handler(connection, ('127.0.0.1', 1), SimpleNamespace(), directory=str(reserved(tmp_path)))
        return bytes(connection.response)

    request, source = published(tmp_path)
    base = 'https://192.0.2.1'
    request['publicationRoot']['url'] = base
    source = base + '/abcdef/agent.iso'
    first = token.operate(request, source, '', 'publish')['url']
    second = token.operate(request, source, '', 'publish')['url']
    for url in [first, second]:
        response = fetch(url)
        assert response.startswith(b'HTTP/1.0 200 ') and response.endswith(b'image')
    token.operate(request, source, first, 'remove')
    assert fetch(first).startswith(b'HTTP/1.0 404 ')
    for url in [second, source]:
        response = fetch(url)
        assert response.startswith(b'HTTP/1.0 200 ') and response.endswith(b'image')


def test_sweep_keeps_presented_links_and_retires_every_other_insert(tmp_path):
    request, source = published(tmp_path)
    kept = token.operate(request, source, '', 'publish')['url']
    stale = [token.operate(request, source, '', 'publish')['url'] for attempt in range(2)]
    other = reserved(tmp_path) / 'manual'
    other.mkdir()
    (other / 'agent.iso').write_bytes(b'other')
    result = token.operate(request, source, '', 'sweep', [kept, 'https://192.0.2.9/elsewhere.iso', ''])
    assert result == {'changed': True, 'removed': 2, 'kept': 1, 'refused': 0}
    assert token.operate(request, source, kept, 'inspect')['own']
    for url in stale:
        assert not (reserved(tmp_path) / url.split('/')[-2]).exists()
    assert (reserved(tmp_path) / 'abcdef' / 'agent.iso').read_bytes() == b'image'
    assert (other / 'agent.iso').read_bytes() == b'other'
    assert not any(url.split('/')[-2] in repr(result) for url in [kept] + stale)
    assert token.operate(request, source, '', 'sweep', [kept]) == {'changed': False, 'removed': 0, 'kept': 1, 'refused': 0}


@pytest.mark.parametrize('echo', [
    lambda url: url,
    lambda url: url.replace('https://192.0.2.1:8443/', 'https://192.0.2.1/'),
    lambda url: url.replace('https://192.0.2.1:8443/', 'https://artifacts.example.com/'),
    lambda url: url.replace('https://192.0.2.1:8443/', 'HTTP://ARTIFACTS:80/'),
])
def test_sweep_keeps_a_link_a_controller_echoes_under_another_port_or_host(tmp_path, echo):
    request, source = published(tmp_path)
    request['publicationRoot']['url'] = 'https://192.0.2.1:8443/private/cluster'
    source = source.replace('https://192.0.2.1/', 'https://192.0.2.1:8443/')
    presented = token.operate(request, source, '', 'publish')['url']
    stale = token.operate(request, source, '', 'publish')['url']
    result = token.operate(request, source, '', 'sweep', [echo(presented)])
    assert result == {'changed': True, 'removed': 1, 'kept': 1, 'refused': 0}
    assert token.operate(request, source, presented, 'inspect')['own']
    assert not (reserved(tmp_path) / stale.split('/')[-2]).exists()


def test_sweep_keeps_nothing_for_an_unrelated_url(tmp_path):
    request, source = published(tmp_path)
    inserted = token.operate(request, source, '', 'publish')['url']
    name = inserted.split('/')[-2]
    unrelated = ['https://192.0.2.1/private/cluster/insert-' + 'c' * 64 + '/agent.iso',
                 'https://192.0.2.9/' + name[:-1] + '/agent.iso', 'https://[::1/' + name + '/agent.iso',
                 'https://192.0.2.1/private/cluster/agent.iso?token=' + name, 'not a url']
    result = token.operate(request, source, '', 'sweep', unrelated)
    assert result == {'changed': True, 'removed': 1, 'kept': 0, 'refused': 0}
    assert not (reserved(tmp_path) / name).exists()


def test_sweep_with_nothing_presented_leaves_only_the_source(tmp_path):
    request, source = published(tmp_path)
    token.operate(request, source, '', 'publish')
    (reserved(tmp_path) / 'insert-interrupted').mkdir()
    assert token.operate(request, source, '', 'sweep', [])['removed'] == 2
    assert sorted(os.listdir(reserved(tmp_path))) == ['abcdef']


def test_sweep_refuses_and_keeps_an_insert_entry_it_cannot_prove(tmp_path):
    request, source = published(tmp_path)
    private = reserved(tmp_path)
    stale = token.operate(request, source, '', 'publish')['url']
    foreign = private / 'insert-foreign'
    foreign.mkdir()
    (foreign / 'agent.iso').write_bytes(b'image')
    extra = private / 'insert-extra'
    extra.mkdir()
    os.link(private / 'abcdef' / 'agent.iso', extra / 'agent.iso')
    (extra / 'other').write_bytes(b'')
    (private / 'insert-file').write_bytes(b'')
    os.symlink('abcdef', private / 'insert-link')
    linked = private / 'insert-symlinked-image'
    linked.mkdir()
    os.symlink('../abcdef/agent.iso', linked / 'agent.iso')
    result = token.operate(request, source, '', 'sweep', [])
    assert result == {'changed': True, 'removed': 1, 'kept': 0, 'refused': 5}
    assert not (private / stale.split('/')[-2]).exists()
    for entry in ['insert-foreign', 'insert-extra', 'insert-file', 'insert-link', 'insert-symlinked-image']:
        assert os.path.lexists(private / entry)
    assert (private / 'abcdef' / 'agent.iso').read_bytes() == b'image'


def test_sweep_refuses_when_the_source_is_not_provable(tmp_path):
    request, source = published(tmp_path)
    stale = token.operate(request, source, '', 'publish')['url']
    for bad in ['', source.replace('abcdef', 'insert-' + 'a' * 64), source.replace('192.0.2.1', '192.0.2.2')]:
        with pytest.raises(ValueError):
            token.operate(request, bad, '', 'sweep', [])
    assert (reserved(tmp_path) / stale.split('/')[-2] / 'agent.iso').exists()


def test_swept_token_answers_404_while_the_presented_one_serves(tmp_path):
    from http.server import SimpleHTTPRequestHandler
    from io import BytesIO
    from types import SimpleNamespace
    from urllib.parse import urlsplit

    class Handler(SimpleHTTPRequestHandler):
        def log_message(self, format, *args):
            pass

    class Connection:
        def __init__(self, url):
            self.request = BytesIO(('GET %s HTTP/1.0\r\n\r\n' % urlsplit(url).path).encode())
            self.response = bytearray()

        def makefile(self, *args):
            return self.request

        def sendall(self, data):
            self.response.extend(data)

    def fetch(url):
        connection = Connection(url)
        Handler(connection, ('127.0.0.1', 1), SimpleNamespace(), directory=str(reserved(tmp_path)))
        return bytes(connection.response)

    request, source = published(tmp_path)
    base = 'https://192.0.2.1'
    request['publicationRoot']['url'] = base
    source = base + '/abcdef/agent.iso'
    failed = token.operate(request, source, '', 'publish')['url']
    presented = token.operate(request, source, '', 'publish')['url']
    assert fetch(failed).startswith(b'HTTP/1.0 200 ')
    token.operate(request, source, '', 'sweep', [presented.replace('https://192.0.2.1', 'HTTPS://192.0.2.1:443')])
    assert fetch(failed).startswith(b'HTTP/1.0 404 ')
    for url in [presented, source]:
        response = fetch(url)
        assert response.startswith(b'HTTP/1.0 200 ') and response.endswith(b'image')


def test_the_sweep_task_fails_on_an_unprovable_entry_and_reports_only_counts(tmp_path, capsys):
    request, source = published(tmp_path)
    kept = token.operate(request, source, '', 'publish')['url']
    stale = token.operate(request, source, '', 'publish')['url']
    (reserved(tmp_path) / 'insert-foreign').mkdir()
    (reserved(tmp_path) / 'insert-foreign' / 'agent.iso').write_bytes(b'image')
    capsys.readouterr()
    with patch_module_args({'request': request, 'source': source, 'presented': [kept], 'operation': 'sweep'}), pytest.raises(SystemExit):
        token.main()
    output = capsys.readouterr().out
    result = json.loads(output)
    assert result['failed'] and (result['removed'], result['kept'], result['refused']) == (1, 1, 1)
    assert all(url.split('/')[-2] not in output for url in [kept, stale, source])
    assert not (reserved(tmp_path) / stale.split('/')[-2]).exists()
