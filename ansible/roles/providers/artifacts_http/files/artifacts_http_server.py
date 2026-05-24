#!/usr/bin/env python3

import argparse
import functools
import http.server
import os
import re
import ssl


_RANGE_ABSENT = object()
_RANGE_INVALID = object()
_RANGE_RE = re.compile(r"^bytes=(\d*)-(\d*)$")
_COPY_CHUNK_SIZE = 64 * 1024


class RangeHTTPRequestHandler(http.server.SimpleHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def send_head(self):
        self._range = None
        path = self.translate_path(self.path)
        if os.path.isdir(path):
            self.send_error(404, "File not found")
            return None

        try:
            source = open(path, "rb")
        except OSError:
            self.send_error(404, "File not found")
            return None

        try:
            stat = os.fstat(source.fileno())
            size = stat.st_size
            parsed_range = self._parse_range(self.headers.get("Range"), size)
            if parsed_range is _RANGE_INVALID:
                self.send_response(416)
                self.send_header("Content-Range", f"bytes */{size}")
                self.send_header("Content-Length", "0")
                self.end_headers()
                source.close()
                return None

            content_type = self.guess_type(path)
            if parsed_range is _RANGE_ABSENT:
                self.send_response(200)
                self.send_header("Content-type", content_type)
                self.send_header("Content-Length", str(size))
                self.send_header("Last-Modified", self.date_time_string(stat.st_mtime))
                self.end_headers()
                return source

            start, end = parsed_range
            length = end - start + 1
            self._range = (start, length)
            self.send_response(206)
            self.send_header("Content-type", content_type)
            self.send_header("Content-Range", f"bytes {start}-{end}/{size}")
            self.send_header("Content-Length", str(length))
            self.send_header("Last-Modified", self.date_time_string(stat.st_mtime))
            self.end_headers()
            return source
        except Exception:
            source.close()
            raise

    def end_headers(self):
        self.send_header("Accept-Ranges", "bytes")
        self.send_header("Connection", "close")
        self.close_connection = True
        super().end_headers()

    def copyfile(self, source, outputfile):
        selected_range = getattr(self, "_range", None)
        if selected_range is None:
            remaining = None
        else:
            start, remaining = selected_range
            source.seek(start)

        try:
            self._copy_bytes(source, outputfile, remaining)
        except (ConnectionError, ssl.SSLError) as exc:
            self.close_connection = True
            self.log_message(
                "client closed connection while streaming %s: %s",
                self.path,
                exc.__class__.__name__,
            )
        return None

    @staticmethod
    def _copy_bytes(source, outputfile, remaining):
        while remaining is None or remaining > 0:
            read_size = _COPY_CHUNK_SIZE
            if remaining is not None:
                read_size = min(read_size, remaining)
            chunk = source.read(read_size)
            if not chunk:
                break
            outputfile.write(chunk)
            if remaining is not None:
                remaining -= len(chunk)

    @staticmethod
    def _parse_range(header, size):
        if not header:
            return _RANGE_ABSENT

        match = _RANGE_RE.match(header.strip())
        if not match:
            return _RANGE_INVALID

        first, last = match.groups()
        if first == "" and last == "":
            return _RANGE_INVALID

        if first == "":
            suffix = int(last)
            if suffix <= 0 or size == 0:
                return _RANGE_INVALID
            return max(size - suffix, 0), size - 1

        start = int(first)
        end = int(last) if last != "" else size - 1
        if start > end or start >= size:
            return _RANGE_INVALID
        return start, min(end, size - 1)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("port", type=int)
    parser.add_argument("--bind", default="")
    parser.add_argument("--directory", required=True)
    parser.add_argument("--certfile", required=True)
    parser.add_argument("--keyfile", required=True)
    args = parser.parse_args()

    handler = functools.partial(RangeHTTPRequestHandler, directory=args.directory)
    with http.server.ThreadingHTTPServer((args.bind, args.port), handler) as server:
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
        context.load_cert_chain(args.certfile, args.keyfile)
        server.socket = context.wrap_socket(server.socket, server_side=True)
        server.serve_forever()


if __name__ == "__main__":
    main()
