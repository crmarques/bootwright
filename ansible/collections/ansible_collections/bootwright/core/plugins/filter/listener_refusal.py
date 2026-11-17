"""Name why a fetch through an artifact server listener failed.

A listener proof fetches one byte under no_log, because its URL or its native
answer may carry a capability token, and a separate step outside no_log fails
with this message. The classes are the ones an operator acts on differently: a
listener that answered 403 serves as a worker that cannot read the file, a 404
finds nothing at the published path, a certificate that did not verify names
the Secret the serving certificate is bound from, a request that ended before
any status line carries its native cause, and any other status is named as it
is. The native cause loses the token and every character that is not
printable, and is bounded to 512 characters.
"""

from __future__ import annotations

DOCUMENTATION = r"""
name: listener_refusal
short_description: Name why a fetch through an artifact server listener failed
version_added: "0.1.0"
description:
  - Classifies the result ansible.builtin.uri registered for a failed listener
    fetch as 403, 404, a certificate that did not verify, a request that ended
    before any status line, or another status.
  - The native cause is cleaned by C(bootwright.core.printable), bounded to
    512 characters, and every occurrence of the token is replaced by
    C(<token>), in the cause and the subject alike.
options:
  _input:
    description: The result the fetch registered.
    type: dict
    required: true
  token:
    description: The capability token the private URL carries, or an empty text when it carries none.
    type: str
    required: true
  secret:
    description: The Secret the serving certificate is bound from, or an empty text when the request names none.
    type: str
    required: true
  subject:
    description: What could not be fetched, as the operator knows it.
    type: str
    required: true
  consequence:
    description: What the refusal leaves undone, or an empty text.
    type: str
    default: ""
author:
  - Bootwright contributors (@crmarques)
"""

EXAMPLES = r"""
- name: Refuse a private file the listener does not serve
  ansible.builtin.fail:
    msg: >-
      {{ fetched | bootwright.core.listener_refusal(token, 'artifact-server-tls',
         'the private kickstart.cfg', 'so no machine was given media') }}
  when: fetched.status | default(-1) | int not in [200, 206]
"""

RETURN = r"""
_value:
  description: The operator message, with no token in it.
  type: str
"""

from ansible.errors import AnsibleFilterError
from ansible_collections.bootwright.core.plugins.filter.printable import printable

BOUND = 512
UNVERIFIED = ("CERTIFICATE_VERIFY_FAILED", "certificate verify failed")


def status_of(result):
    """The status line's code, or -1 when the request ended before one."""
    status = result.get("status", -1)
    if isinstance(status, bool):
        return -1
    try:
        return int(status)
    except (TypeError, ValueError):
        return -1


def hidden(text, token):
    return text.replace(token, "<token>") if token else text


def cause_of(result, token):
    native = result.get("msg", "the request recorded no result")
    return printable(hidden(str(native), token))[:BOUND]


def classified(status, cause, secret):
    if status == 403:
        return "the listener answered 403, so the worker it serves as cannot read it"
    if status == 404:
        return "the listener answered 404, so the worker it serves as finds nothing at the path this attempt published"
    if any(text in cause for text in UNVERIFIED):
        anchor = ("the serving certificate Secret %s holds" % secret if secret
                  else "the serving certificate bound to this operation")
        return "its certificate did not verify against %s: %s" % (anchor, cause)
    if status == -1:
        return "the request failed before any status line: " + cause
    return "the listener answered %d where 200 or 206 was expected" % status


def listener_refusal(result, token, secret, subject, consequence=""):
    """The operator message for a failed listener fetch, without the token."""
    if not isinstance(result, dict):
        raise AnsibleFilterError("listener_refusal: the input is not a registered result")
    if not all(isinstance(value, str) for value in (token, secret, subject, consequence)) or not subject:
        raise AnsibleFilterError("listener_refusal: the token, secret, subject and consequence must be texts, the subject not empty")
    lead = subject + " could not be fetched through its artifact server listener"
    if consequence:
        lead += ", " + consequence
    return hidden("%s: %s" % (lead, classified(status_of(result), cause_of(result, token), secret)), token)


class FilterModule:
    """The filters this collection offers its roles."""

    def filters(self):
        return {"listener_refusal": listener_refusal}
