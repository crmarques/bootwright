"""The artifact server records each completed fetch without what was fetched.

A BMC that cannot fetch an image leaves its trace only on the server it tried,
so the server logs each completed request to standard output, which its unit's
journal keeps. The path of a privately delivered image carries its token, so
the line names who asked, when, over which TLS, the method, the status and the
bytes sent, and nothing that holds the request's URI.
"""

from __future__ import annotations

import pathlib
import re

GOLDEN = (pathlib.Path(__file__).resolve().parent / "goldens" / "templates" / "infra_artifact_server_nginx"
          / "nginx.conf")
FIELDS = {"$remote_addr", "$time_iso8601", "$ssl_protocol", "$ssl_cipher", "$request_method", "$status",
          "$body_bytes_sent"}
URI_VARIABLES = ("$request", "$request_uri", "$uri", "$args", "$query_string", "$http_referer")


def test_the_artifact_server_logs_each_request_without_its_uri():
    rendered = GOLDEN.read_text(encoding="utf-8")
    lines = [line.strip() for line in rendered.splitlines()]
    assert lines.count("access_log /dev/stdout fetch;") == 1
    assert not [line for line in lines if line.startswith("access_log") and line not in ("access_log /dev/stdout fetch;", "access_log off;")]
    formats = re.findall(r"^\s*log_format\s+fetch\s+'([^']*)';\s*$", rendered, re.M)
    assert len(formats) == 1
    assert set(re.findall(r"\$[a-z_0-9]+", formats[0])) == FIELDS
    logging = set(re.findall(r"\$[a-z_0-9]+", " ".join(
        line for line in lines if line.startswith(("log_format", "access_log", "error_log")))))
    assert not logging.intersection(URI_VARIABLES)
    # $uri is what try_files serves from, and that is its one use.
    elsewhere = set(re.findall(r"\$[a-z_0-9]+", rendered)).intersection(URI_VARIABLES)
    assert elsewhere <= {"$uri", "$http_content_length", "$http_authorization", "$http_transfer_encoding"}
    assert [line for line in lines if "$uri" in line] == ["try_files $uri =404;"] * 2
    assert "error_log /dev/stderr warn;" in lines
    assert lines.count("access_log off;") == 1
    assert lines.count("error_log /dev/null crit;") == 2
    callback = rendered.split('location ~ "^/_bootwright/inventory/', 1)[1].split("location /_bootwright/", 1)[0]
    assert "access_log off;" in callback
    assert "client_max_body_size 48k;" in callback
    assert "client_body_buffer_size 64k;" in callback
    assert "proxy_pass http://unix:" in callback
