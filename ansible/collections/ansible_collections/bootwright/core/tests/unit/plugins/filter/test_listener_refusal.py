"""The listener_refusal filter names why a listener fetch failed, bounded, printable and without its token.

The results are the forms ansible-core 2.21.4's ansible.builtin.uri registers:
a refused status prefixed "Status code was %s and not %s: ", a URLError as
"Request failed: %s" with status -1, and a body it could not read as "HTTP
Error while fetching {url}: ..." with no status at all.
"""

from __future__ import annotations

import pytest
from ansible.errors import AnsibleFilterError

from ansible_collections.bootwright.core.plugins.filter.listener_refusal import listener_refusal

TOKEN = "0123456789abcdef" * 4
URL = "https://192.0.2.1:8443/private/hosts/m1/" + TOKEN + "/kickstart.cfg"
SECRET = "artifact-server-tls"
SUBJECT = "the private kickstart.cfg"
CONSEQUENCE = "so no machine was given media"
LEAD = SUBJECT + " could not be fetched through its artifact server listener, " + CONSEQUENCE + ": "
REFUSED = "Status code was %s and not [200, 206]: HTTP Error %s: %s"
UNVERIFIED = "Request failed: <urlopen error [SSL: CERTIFICATE_VERIFY_FAILED] certificate verify failed> " + URL
UNREACHED = "Request failed: <urlopen error [Errno 111] Connection refused>"
UNREAD = "HTTP Error while fetching %s: IncompleteRead(0 bytes read, 1 more expected)" % URL

CLASSES = [
    ({"status": 403, "msg": REFUSED % (403, 403, "Forbidden")},
     "the listener answered 403, so the worker it serves as cannot read it"),
    ({"status": 404, "msg": REFUSED % (404, 404, "Not Found")},
     "the listener answered 404, so the worker it serves as finds nothing at the path this attempt published"),
    ({"status": -1, "msg": UNVERIFIED},
     "its certificate did not verify against the serving certificate Secret %s holds: %s"
     % (SECRET, UNVERIFIED.replace(TOKEN, "<token>"))),
    ({"status": -1, "msg": "certificate verify failed: self-signed certificate"},
     "its certificate did not verify against the serving certificate Secret %s holds: "
     "certificate verify failed: self-signed certificate" % SECRET),
    ({"status": -1, "msg": UNREACHED}, "the request failed before any status line: " + UNREACHED),
    ({"failed": True, "msg": UNREAD}, "the request failed before any status line: " + UNREAD.replace(TOKEN, "<token>")),
    ({}, "the request failed before any status line: the request recorded no result"),
    ({"status": 500, "msg": REFUSED % (500, 500, "Internal Server Error")},
     "the listener answered 500 where 200 or 206 was expected"),
    ({"status": "502", "msg": REFUSED % (502, 502, "Bad Gateway")},
     "the listener answered 502 where 200 or 206 was expected"),
    ({"status": "not a status", "msg": UNREACHED}, "the request failed before any status line: " + UNREACHED),
]


@pytest.mark.parametrize("result, cause", CLASSES, ids=[str(cause)[:48] for _result, cause in CLASSES])
def test_each_class_of_failed_fetch_is_named_with_its_cause(result, cause):
    assert listener_refusal(result, TOKEN, SECRET, SUBJECT, CONSEQUENCE) == LEAD + cause


def test_the_token_never_reaches_the_message():
    for result, _cause in CLASSES:
        message = listener_refusal(dict(result, msg=str(result.get("msg", "")) + " " + TOKEN), TOKEN, SECRET, SUBJECT,
                                   CONSEQUENCE)
        assert TOKEN not in message, message
    assert TOKEN not in listener_refusal({"status": -1, "msg": UNREACHED}, TOKEN, SECRET, "the " + TOKEN, CONSEQUENCE)


def test_no_token_leaves_the_cause_unchanged():
    assert listener_refusal({"status": -1, "msg": UNREACHED}, "", SECRET, SUBJECT, CONSEQUENCE) == (
        LEAD + "the request failed before any status line: " + UNREACHED)


def test_the_native_cause_is_printable_and_bounded():
    native = "Request failed: \x1b[31m" + "x" * 2000 + "\u202e\n" + TOKEN
    message = listener_refusal({"status": -1, "msg": native}, TOKEN, SECRET, SUBJECT, CONSEQUENCE)
    cause = message[len(LEAD + "the request failed before any status line: "):]
    assert cause == ("Request failed: [31m" + "x" * 2000)[:512]
    assert all(character.isprintable() for character in message)


@pytest.mark.parametrize("breaks", ["\n", "\r\n", "\n\n\r", "\r"])
def test_the_lines_of_a_native_cause_stay_apart(breaks):
    native = "Connection refused" + breaks + "second" + breaks + TOKEN
    message = listener_refusal({"status": -1, "msg": native}, TOKEN, SECRET, SUBJECT, CONSEQUENCE)
    assert message == LEAD + "the request failed before any status line: Connection refused second <token>"


def test_a_certificate_failure_with_no_bound_secret_names_the_bound_certificate():
    assert listener_refusal({"status": -1, "msg": UNVERIFIED}, TOKEN, "", SUBJECT, CONSEQUENCE) == (
        LEAD + "its certificate did not verify against the serving certificate bound to this operation: "
        + UNVERIFIED.replace(TOKEN, "<token>"))


def test_no_consequence_ends_the_lead_at_the_listener():
    assert listener_refusal({"status": 403}, "", "", SUBJECT) == (
        SUBJECT + " could not be fetched through its artifact server listener: "
        "the listener answered 403, so the worker it serves as cannot read it")


@pytest.mark.parametrize("arguments", [
    (None, TOKEN, SECRET, SUBJECT, CONSEQUENCE),
    ({"status": 403}, None, SECRET, SUBJECT, CONSEQUENCE),
    ({"status": 403}, TOKEN, None, SUBJECT, CONSEQUENCE),
    ({"status": 403}, TOKEN, SECRET, "", CONSEQUENCE),
    ({"status": 403}, TOKEN, SECRET, SUBJECT, None),
])
def test_only_a_result_and_texts_are_judged(arguments):
    with pytest.raises(AnsibleFilterError):
        listener_refusal(*arguments)
