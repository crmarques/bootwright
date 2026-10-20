"""Each insert owns a fresh hard link; only that file can be retired."""

import os

import pytest

from ansible_collections.bootwright.core.plugins.modules import containercluster_media_token as token


def published(tmp_path):
    root = tmp_path / 'private'
    source = root / 'abcdef'
    source.mkdir(parents=True)
    (source / 'agent.iso').write_bytes(b'image')
    base = 'https://192.0.2.1/private/cluster'
    return {'publicationRoot': {'path': str(root), 'url': base}}, base + '/abcdef/agent.iso'


def test_each_insert_is_unique_and_eject_removes_only_its_link(tmp_path):
    request, source = published(tmp_path)
    first = token.operate(request, source, '', 'publish')['url']
    second = token.operate(request, source, '', 'publish')['url']
    assert len(first.split("/")[-2].removeprefix("insert-")) == 64
    assert (tmp_path / "private" / first.split("/")[-2]).stat().st_mode & 0o777 == 0o711
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
    other = tmp_path / 'private' / 'insert-foreign'
    other.mkdir()
    (other / 'agent.iso').write_bytes(b'image')
    url = request['publicationRoot']['url'] + '/insert-foreign/agent.iso'
    assert not token.operate(request, source, url, 'inspect')['own']
    assert not token.operate(request, source, url, 'remove')['changed']
    assert (other / 'agent.iso').exists()


def test_symlinked_token_is_never_owned(tmp_path):
    request, source = published(tmp_path)
    os.symlink('abcdef', tmp_path / 'private' / 'insert-link')
    url = request['publicationRoot']['url'] + '/insert-link/agent.iso'
    assert not token.operate(request, source, url, 'inspect')['own']


def test_symlinked_publication_root_refuses(tmp_path):
    request, source = published(tmp_path)
    os.symlink('private', tmp_path / 'alias')
    request['publicationRoot']['path'] = str(tmp_path / 'alias')
    with pytest.raises(OSError):
        token.operate(request, source, '', 'publish')


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
        Handler(connection, ('127.0.0.1', 1), SimpleNamespace(), directory=str(tmp_path / 'private'))
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
