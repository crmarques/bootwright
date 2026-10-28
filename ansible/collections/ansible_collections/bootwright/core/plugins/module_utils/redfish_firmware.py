"""Declared firmware settings, applied or proved through one controller.

A Machine may declare Secure Boot, the boot mode and BIOS attributes. Each is
compared with what the controller reports by value and JSON type both, because
a controller that reads 1 has not reported true, and with the value the linked
settings object holds pending for it, because the next reset would apply that
one instead. Proving only reads. Enforcing
writes only what differs, and only once reads alone show every write can land:
each resource is followed from the controller's own links and never built, a
value the controller advertises it does not take is never sent, and a reset
that would also apply a pending change this Machine does not declare is never
caused.

Settings a reset applies are applied by one power cycle that stops in firmware
setup: a one-time BiosSetup boot is written and proved first, so the cycle
never boots the operator's disk; the machine is powered on, the settings are
read until they report the declared values within a bound, and the machine
this step asked to power on is forced off and polled to Off whatever happened,
since a power-on may land whatever was read. Each poll stops once its share
of the bound has passed, each read's socket timeout cut to the share left, and
the machine is not powered on once the writes before it have spent their
share. A SIGTERM received while enforcing is only recorded: it ends the poll
under way as a spent share would, any machine the step powered on is still
forced off and polled to Off, and the step reports that it was asked to stop.
The outcome is a final read, never a status code.

A refusal names declared field paths, fixed reasons, the client's own failure
lines and the controller's message identifiers, never a value the controller
reported or the credential. A read that fails before anything was written is
the client's failure and names no refusal; once a write was sent, any failure
is the refusal that the settings were not applied.
"""

from __future__ import annotations

import re
import signal

from ansible_collections.bootwright.core.plugins.module_utils import redfish_control, redfish_discovery

MISMATCH = "firmware-setting-mismatch"
NOT_APPLIED = "firmware-setting-not-applied"
ENFORCE, PROVE = "enforce", "prove"
SECURE_BOOT = "firmware.secureBoot"
BOOT_MODE = "firmware.bootMode"
BIOS = "firmware.biosAttributes."
UEFI = "UEFI"
SETUP = "BiosSetup"
FIELDS = ("mode", "secureBoot", "bootMode", "biosAttributes")
VALUES = ("string", "integer", "boolean")
NAME = re.compile(r"[A-Za-z][A-Za-z0-9_]{1,127}")
MAX_ATTRIBUTES = 64
MAX_STRING = 256
INT64 = (-(1 << 63), (1 << 63) - 1)
MAX_LINES = 16
ON_RESET = "OnReset"
STOPPED = "the step was asked to stop"
LATE = "the writes took longer than their share of the bound; the machine was not powered on"


class Termination:
    """While the step enforces, a SIGTERM is only recorded in `received`.

    The handler never raises, so no request, write or power-off is cut short
    where it stands: the polls read `received` at each read and pause, and the
    step stops at the next one. The handler before it is restored on exit,
    however the step leaves.
    """

    def __init__(self, install=signal.signal):
        self.install, self.previous, self.received = install, None, False

    def __enter__(self):
        self.previous = self.install(signal.SIGTERM, self.handle)
        return self

    def handle(self, _signum, _frame):
        self.received = True

    def __exit__(self, _kind, _value, _trace):
        self.install(signal.SIGTERM, signal.SIG_DFL if self.previous is None else self.previous)
        return False


class Refusal(Exception):
    """Ends an enforcement with its reasons and the declared fields they concern."""

    def __init__(self, reasons, fields=()):
        super().__init__("; ".join(reasons))
        self.reasons, self.fields = list(reasons), list(fields)


def validated(firmware):
    """The mode and the declared settings by field path, or ValueError.

    The keys are closed and every value has its exact type, so a string never
    stands for a Boolean and a Boolean never for an integer. Attributes are
    passed on verbatim and in name order; nothing interprets them.
    """
    if not isinstance(firmware, dict) or set(firmware) - set(FIELDS):
        raise ValueError("the firmware declaration names a field this step does not know")
    mode = firmware.get("mode") or ENFORCE
    if mode not in (ENFORCE, PROVE):
        raise ValueError("firmware.mode is neither enforce nor prove")
    wanted = {}
    if firmware.get("secureBoot") is not None:
        if not isinstance(firmware["secureBoot"], bool):
            raise ValueError("firmware.secureBoot is not a Boolean")
        wanted[SECURE_BOOT] = firmware["secureBoot"]
    if firmware.get("bootMode") is not None:
        if firmware["bootMode"] != UEFI:
            raise ValueError("firmware.bootMode is not UEFI")
        wanted[BOOT_MODE] = UEFI
    attributes = firmware.get("biosAttributes") or []
    if not isinstance(attributes, list) or len(attributes) > MAX_ATTRIBUTES:
        raise ValueError("firmware.biosAttributes is not a list of at most 64 entries")
    named = dict(_attribute(entry, position) for position, entry in enumerate(attributes))
    if len(named) != len(attributes):
        raise ValueError("firmware.biosAttributes names one attribute twice")
    wanted.update((BIOS + name, named[name]) for name in sorted(named))
    return mode, wanted


def _attribute(entry, position):
    where = "firmware.biosAttributes[%d]" % position
    if not isinstance(entry, dict) or set(entry) - {"name"} - set(VALUES):
        raise ValueError(where + " is not a name with one value")
    name = entry.get("name")
    if not isinstance(name, str) or not NAME.fullmatch(name):
        raise ValueError(where + " has no valid name")
    given = [kind for kind in VALUES if entry.get(kind) is not None]
    if len(given) != 1:
        raise ValueError(where + " carries not exactly one value")
    value = entry[given[0]]
    if given[0] == "string" and isinstance(value, str) and len(value) <= MAX_STRING and all(
            " " <= char <= "~" for char in value):
        return name, str(value)
    if given[0] == "integer" and isinstance(value, int) and not isinstance(value, bool) and INT64[0] <= value <= INT64[1]:
        return name, value
    if given[0] == "boolean" and isinstance(value, bool):
        return name, value
    raise ValueError(where + " carries a value of another type or out of bounds")


def same(declared, observed):
    """Typed equality: True is not 1 and "1" is not 1."""
    return type(declared) is type(observed) and declared == observed


def link(resource, name):
    """The reference a resource's own property names, or ""."""
    value = resource.get(name) if isinstance(resource, dict) else None
    reference = value.get("@odata.id") if isinstance(value, dict) else None
    return reference if isinstance(reference, str) and reference.strip() else ""


def settings_target(resource):
    """The pending-settings resource the controller links, or "" when it links none.

    Only the resource's own `@Redfish.Settings.SettingsObject` names it; no
    conventional path is ever built.
    """
    settings = resource.get("@Redfish.Settings") if isinstance(resource, dict) else None
    return link(settings, "SettingsObject")


def applies_on_reset(resource, settings):
    """Whether the controller advertises the OnReset apply time for these settings."""
    block = resource.get("@Redfish.Settings") if isinstance(resource, dict) else None
    supported = block.get("SupportedApplyTimes") if isinstance(block, dict) else None
    apply_time = settings.get("@Redfish.SettingsApplyTime") if isinstance(settings, dict) else None
    allowed = apply_time.get("ApplyTime@Redfish.AllowableValues") if isinstance(apply_time, dict) else None
    return any(isinstance(listed, list) and ON_RESET in listed for listed in (supported, allowed))


def excludes(holder, name, value):
    """Whether a resource advertises the values a property takes, without this one."""
    listed = holder.get(name + "@Redfish.AllowableValues") if isinstance(holder, dict) else None
    return isinstance(listed, list) and value not in listed


def foreign_pending(current, pending, declared):
    """The pending settings a reset would apply that this Machine does not declare.

    A pending value equal to the current one changes nothing; any other
    pending name outside the declared ones is someone else's change.
    """
    current = current if isinstance(current, dict) else {}
    pending = pending if isinstance(pending, dict) else {}
    return sorted(name for name, value in pending.items()
                  if name not in declared and (name not in current or not same(value, current[name])))


def observed(wanted, system, secure, bios):
    """What the controller reports for each declared field path it reports at all."""
    seen = {}
    boot = system.get("Boot") if isinstance(system, dict) else None
    if BOOT_MODE in wanted and isinstance(boot, dict) and "BootSourceOverrideMode" in boot:
        seen[BOOT_MODE] = boot["BootSourceOverrideMode"]
    if SECURE_BOOT in wanted and isinstance(secure, dict) and "SecureBootEnable" in secure:
        seen[SECURE_BOOT] = secure["SecureBootEnable"]
    attributes = bios.get("Attributes") if isinstance(bios, dict) else None
    for path in wanted:
        if path.startswith(BIOS) and isinstance(attributes, dict) and path[len(BIOS):] in attributes:
            seen[path] = attributes[path[len(BIOS):]]
    return seen


def differences(wanted, seen, staged=None):
    """The declared field paths the controller does not report as declared, or
    whose pending value a reset would change from the declared one, in order."""
    staged = staged or {}
    return [path for path, value in wanted.items() if path not in seen or not same(value, seen[path])
            or (path in staged and not same(value, staged[path]))]


def _line(failure):
    """A failure's line: the client's own, or only the type of any other exception."""
    if isinstance(failure, redfish_control.ControllerError):
        return str(failure)
    return "an unexpected %s" % type(failure).__name__


def message_ids(body):
    """The controller's own message identifiers in an answer or a settings block."""
    entries = []
    if isinstance(body, dict):
        settings = body.get("@Redfish.Settings")
        for holder in (body, settings if isinstance(settings, dict) else {}):
            listed = holder.get("Messages")
            entries.extend(listed if isinstance(listed, list) else [listed] if isinstance(listed, dict) else [])
        error = body.get("error")
        extended = error.get("@Message.ExtendedInfo") if isinstance(error, dict) else None
        entries.extend(extended if isinstance(extended, list) else [])
    found = []
    for entry in entries:
        value = entry.get("MessageId") if isinstance(entry, dict) else None
        if isinstance(value, str) and redfish_discovery.MESSAGE_ID.fullmatch(value) and value[:64] not in found:
            found.append(value[:64])
    return found


def converge(client, firmware):
    """Apply or prove one Machine's declared firmware settings: the outcome as data.

    The result names whether any write was sent, the refusal when the
    settings do not read as declared ("" when they do), the declared field
    paths concerned, the reasons, the controller's message identifiers and the
    power state it last reported. A client failure before any write raises.
    """
    mode, wanted = validated(firmware)
    return Convergence(client, mode, wanted).run()


class Convergence:
    """One step's reads and writes, holding what it sent and what it was told."""

    def __init__(self, client, mode, wanted):
        self.client, self.mode, self.wanted = client, mode, wanted
        self.changed, self.messages, self.reasons, self.links = False, [], [], {}
        self.deadline, self.termination = None, None

    def run(self):
        if not self.wanted:
            return self.result([])
        self.deadline = self.client.clock() + redfish_control.FIRMWARE_WRITES_BUDGET
        state = self.read()
        missing = differences(self.wanted, state["seen"], state["staged"])
        if not missing and not state["unpublished"]:
            return self.result([])
        if self.mode == PROVE:
            self.reasons.extend(state["unpublished"])
            return self.result(missing, MISMATCH)
        try:
            with Termination() as self.termination:
                self.enforce(state, missing)
                state = self.read()
        except Refusal as refusal:
            self.reasons.extend(refusal.reasons)
            return self.result(refusal.fields or missing, NOT_APPLIED)
        except Exception as failure:
            if not self.changed:
                raise
            self.reasons.append(_line(failure))
            return self.result(missing, NOT_APPLIED)
        left = differences(self.wanted, state["seen"], state["staged"])
        if not left and not state["unpublished"]:
            return self.result([])
        for body in (state["secure"], state["bios"]):
            self.note(body)
        self.reasons.extend(state["unpublished"])
        return self.result(left, NOT_APPLIED)

    def stopped(self):
        """Whether the step was asked to stop while enforcing."""
        return self.termination is not None and self.termination.received

    def halt(self, fields):
        """Refuse before any further write or the power-on once the step was asked to stop."""
        if self.stopped():
            raise Refusal([STOPPED], fields)

    def result(self, fields, refusal=""):
        if not refusal:
            self.reasons, self.messages = [], []
        return {"changed": self.changed, "refusal": refusal, "fields": list(fields),
                "reasons": self.reasons[:MAX_LINES], "messages": self.messages[:MAX_LINES],
                "power": self.client.last_power}

    def note(self, body):
        for found in message_ids(body):
            if found not in self.messages:
                self.messages.append(found)

    def read(self):
        """The system, each resource a declared field lives in and the pending
        settings each links, read fresh."""
        system = self.client.system()
        state = {"system": system, "secure": None, "bios": None, "settings": {}, "unpublished": []}
        if BOOT_MODE in self.wanted and not isinstance(system.get("Boot"), dict):
            state["unpublished"].append("the system reports no Boot settings")
        if SECURE_BOOT in self.wanted:
            state["secure"] = self.linked(system, "SecureBoot", state)
        if any(path.startswith(BIOS) for path in self.wanted):
            state["bios"] = self.linked(system, "Bios", state)
        for name, key in (("SecureBoot", "secure"), ("Bios", "bios")):
            target = settings_target(state[key])
            if target:
                state["settings"][name] = self.client._read(target, "reading the %s settings" % name)[0]
        state["seen"] = observed(self.wanted, system, state["secure"], state["bios"])
        state["staged"] = observed(self.wanted, None, state["settings"].get("SecureBoot"), state["settings"].get("Bios"))
        return state

    def linked(self, system, name, state):
        reference = link(system, name)
        if not reference:
            state["unpublished"].append("the system links no %s resource" % name)
            return None
        self.links[name] = reference
        return self.client._read(reference, "reading the %s resource" % name)[0]

    def enforce(self, state, missing):
        """Every write this declaration needs, refused before the first when one cannot land."""
        reset = [path for path in missing if path != BOOT_MODE]
        writes = self.preconditions(state, missing, reset)
        self.halt(missing)
        if reset:
            self.changed = True
            try:
                self.client.boot_once(SETUP, redfish_control.BOOT_BUDGET, self.stopped)
            except redfish_control.ControllerError as failure:
                self.halt(reset)
                raise Refusal(["the one-time BiosSetup boot was not proved: %s" % failure], reset) from None
        if BOOT_MODE in missing:
            self.write_mode(bool(reset))
        for doing, target, payload, pending, fields in writes:
            self.patch(target, payload, doing, fields)
            if pending:
                self.pending(target, pending, doing, fields)
        if reset:
            self.cycle(reset)

    def preconditions(self, state, missing, reset):
        """The writes to send, after every check reads alone can make."""
        reasons, writes = list(state["unpublished"]), []
        if state["system"].get("PowerState") != "Off":
            reasons.append("the system does not report PowerState Off")
        boot = state["system"].get("Boot") if isinstance(state["system"].get("Boot"), dict) else {}
        if BOOT_MODE in missing and excludes(boot, "BootSourceOverrideMode", UEFI):
            reasons.append("the system does not offer BootSourceOverrideMode UEFI")
        if reset and excludes(boot, "BootSourceOverrideTarget", SETUP):
            reasons.append("the system does not offer a one-time BiosSetup boot")
        if reset:
            reasons.extend(self.reset_types())
        if SECURE_BOOT in missing and state["secure"] is not None:
            writes.append(self.secure_write(state))
        bios = [path for path in missing if path.startswith(BIOS)]
        if bios and state["bios"] is not None:
            writes.append(self.bios_write(state, bios, reasons))
        if reset:
            reasons.extend(self.undeclared_pending(state))
        if reasons:
            raise Refusal(reasons, missing)
        return [write for write in writes if write]

    def reset_types(self):
        """Why the power cycle could not be asked for, or nothing, in fixed text."""
        return [reason for _request, reason in (self.reset_request("On"), self.reset_request("ForceOff")) if reason]

    def reset_request(self, kind):
        """Where one reset of the power cycle goes and its type, or why it cannot be
        asked for in fixed text, never the reset types the controller advertises
        nor the target it names. A target on another endpoint is never asked for."""
        try:
            request = self.client._reset_request(kind)
        except redfish_control.UnverifiedCertificate:
            raise
        except redfish_control.ControllerError:
            return None, "the controller offers no %s reset the power cycle can ask for" % kind
        try:
            self.client._own(request[0], "resetting")
        except redfish_control.ControllerError:
            return None, "the controller's %s reset names a target that is not this controller's endpoint" % kind
        return request, ""

    def secure_write(self, state):
        secure = state["secure"]
        target = settings_target(secure)
        payload, pending = {"SecureBootEnable": self.wanted[SECURE_BOOT]}, None
        if target:
            if applies_on_reset(secure, state["settings"]["SecureBoot"]):
                payload["@Redfish.SettingsApplyTime"] = {"ApplyTime": ON_RESET}
            pending = {"SecureBootEnable": self.wanted[SECURE_BOOT]}
        return "writing Secure Boot", target or self.links["SecureBoot"], payload, pending, [SECURE_BOOT]

    def bios_write(self, state, paths, reasons):
        bios = state["bios"]
        target = settings_target(bios)
        if not target:
            reasons.append("the Bios resource publishes no SettingsObject")
            return None
        attributes = {path[len(BIOS):]: self.wanted[path] for path in paths}
        payload = {"Attributes": attributes}
        if applies_on_reset(bios, state["settings"]["Bios"]):
            payload["@Redfish.SettingsApplyTime"] = {"ApplyTime": ON_RESET}
        return "writing the BIOS attributes", target, payload, {"Attributes": attributes}, paths

    def undeclared_pending(self, state):
        """Why the reset would also apply a pending change this Machine does not
        declare, in the Bios or the SecureBoot settings the system links."""
        reasons = []
        for name, key in (("Bios", "bios"), ("SecureBoot", "secure")):
            resource, reference = state[key], link(state["system"], name)
            if resource is None and reference:
                resource = self.client._read(reference, "reading the %s resource" % name)[0]
            target = settings_target(resource)
            if not target:
                continue
            settings = state["settings"].get(name)
            if settings is None:
                settings = self.client._read(target, "reading the %s settings" % name)[0]
            if key == "bios":
                current, pending = resource.get("Attributes"), settings.get("Attributes")
                declared = {path[len(BIOS):] for path in self.wanted if path.startswith(BIOS)}
            else:
                current, pending = _secure_boot(resource), _secure_boot(settings)
                declared = {"SecureBootEnable"} if SECURE_BOOT in self.wanted else set()
            foreign = foreign_pending(current, pending, declared)
            if foreign:
                reasons.append("the %s settings hold %d pending changes this Machine does not declare" % (
                    name, len(foreign)))
        return reasons

    def write_mode(self, reset):
        """Set the boot mode on the system and read it back, keeping the setup boot."""
        doing = "writing the boot mode"
        self.patch("", {"Boot": {"BootSourceOverrideMode": UEFI}}, doing, [BOOT_MODE])
        system = self.client.system()
        boot = system.get("Boot")
        if not isinstance(boot, dict) or not same(UEFI, boot.get("BootSourceOverrideMode")):
            raise Refusal([doing + ": the system does not report BootSourceOverrideMode UEFI"], [BOOT_MODE])
        if reset and not redfish_discovery.boot_selected(system, SETUP):
            raise Refusal([doing + ": the system no longer reports the one-time BiosSetup boot"], [BOOT_MODE])

    def patch(self, reference, payload, doing, fields):
        """One write carrying the resource's entity tag from a fresh read, the one
        retry after a 412 carrying the tag read again, and its task awaited."""
        status, answer, headers = self.tagged(reference, payload, doing, fields)
        if status == 412:
            status, answer, headers = self.tagged(reference, payload, doing, fields)
        self.note(answer)
        if status in redfish_control.PRIVILEGE or (status != 0 and not 200 <= status < 300):
            found = redfish_discovery.message_id(answer)
            raise Refusal(["%s: HTTP %d%s" % (doing, status, " (%s)" % found if found else "")], fields)
        if status == 202:
            reason = self.client._await_task(answer, headers, doing, redfish_control.TASK_BUDGET, self.stopped)
            self.halt(fields)
            if reason:
                raise Refusal([reason], fields)

    def tagged(self, reference, payload, doing, fields):
        self.halt(fields)
        status, body, headers = self.client.fetch(reference, doing=doing)
        if status != 200 or not isinstance(body, dict):
            raise Refusal(["%s: the resource did not answer before the write (HTTP %d)" % (doing, status)], fields)
        tag = headers.get("etag") or body.get("@odata.etag")
        precondition = {"If-Match": tag} if isinstance(tag, str) and tag else {}
        self.changed = True
        return self.client.fetch(reference, "PATCH", payload, headers=precondition, doing=doing)

    def pending(self, target, expected, doing, fields):
        """Read the pending settings back: they must hold the declared values."""
        status, body, _headers = self.client.fetch(target, doing=doing)
        if status != 200 or not isinstance(body, dict):
            raise Refusal(["%s: the pending settings could not be read back (HTTP %d)" % (doing, status)], fields)
        for key, value in expected.items():
            held = body.get(key)
            if isinstance(value, dict):
                held = held if isinstance(held, dict) else {}
                if not all(name in held and same(item, held[name]) for name, item in value.items()):
                    raise Refusal([doing + ": the pending settings do not hold the declared values"], fields)
            elif not same(value, held):
                raise Refusal([doing + ": the pending settings do not hold the declared values"], fields)

    def cycle(self, reset):
        """Power on into setup, read until the settings apply, and power off what
        this step powered on whatever happened.

        A fresh read just before the power-on must still report the one-time
        setup boot and power Off, because the writes before it may have changed
        either; a machine found running is not this step's to power off. Both
        resets are resolved before the power-on, so no failed read after it can
        keep the ForceOff from being sent. No power-on is sent once the step
        was asked to stop or its share before the power-on is spent, and a stop
        after it is reported once the machine was powered off.
        """
        system = self.client._read(doing="reading the system before the power cycle")[0]
        if not redfish_discovery.boot_selected(system, SETUP):
            raise Refusal(["the system no longer reports the one-time BiosSetup boot before the power cycle"], reset)
        if self.client.power_state() != "Off":
            raise Refusal(["the system reports another power state than Off before the power cycle"], reset)
        (start, on), (stop, off) = self.reset_request("On"), self.reset_request("ForceOff")
        if on or off:
            raise Refusal([reason for reason in (on, off) if reason], reset)
        self.halt(reset)
        if self.client.clock() >= self.deadline:
            raise Refusal([LATE], reset)
        late = []
        try:
            self.reset(start, "On")
            if not self.settled(reset):
                late.append("the declared settings did not read back within %d reads %d s apart and %d s" % (
                    redfish_control.FIRMWARE_POLLS, redfish_control.FIRMWARE_POLL_DELAY,
                    redfish_control.FIRMWARE_POLL_BUDGET))
        except Exception as failure:
            late.append("powering the machine on into setup: %s" % _line(failure))
        finally:
            off = self.power_off(stop)
        if self.stopped():
            raise Refusal([STOPPED] + ([off] if off else []), reset)
        if off:
            raise Refusal(late + [off], reset)
        self.reasons.extend(late)

    def reset(self, request, expected):
        """Send one resolved reset and poll the system, within the poll's share of
        the bound and until the step is asked to stop, until it reports `expected`."""
        target, chosen = request
        status, body, _headers = self.client.fetch(target, "POST", {"ResetType": chosen}, doing="resetting")
        if status != 0 and not 200 <= status < 300:
            raise redfish_control.ControllerError(_refused_reset(status, body))
        if self.client._await(redfish_control.POLL_ATTEMPTS, redfish_control.POLL_DELAY,
                              lambda seen: seen.get("PowerState") == expected,
                              redfish_control.POWER_ON_BUDGET, self.stopped) is not None:
            raise redfish_control.ControllerError("resetting: the system does not report %s" % expected)

    def power_off(self, request):
        """Force off the machine this step asked to power on: "" once a fresh read proves it Off.

        The ForceOff is always sent, because any power-on asked for may have
        landed or still land whatever was read, and the system is then polled to
        Off within the power-on poll's share of the bound, which a request to
        stop does not end. A ForceOff refused with 400 or 409 passes only when
        a fresh read then shows Off; any other answer is polled like an
        accepted one. A refused certificate ends the poll at once.
        """
        target, chosen = request
        try:
            status, body, _headers = self.client.fetch(target, "POST", {"ResetType": chosen}, doing="resetting")
            refused = "" if status == 0 or 200 <= status < 300 else _refused_reset(status, body) + "; "
            if status in (400, 409):
                stopped = self.client._read(doing="reading the power state")[0].get("PowerState") == "Off"
            else:
                stopped = self.client._await(redfish_control.POLL_ATTEMPTS, redfish_control.POLL_DELAY,
                                             lambda seen: seen.get("PowerState") == "Off",
                                             redfish_control.POWER_ON_BUDGET) is None
        except Exception as failure:
            return "the machine this step powered on was not proved off: %s" % _line(failure)
        if stopped:
            return ""
        return "the machine this step powered on was not proved off: %sthe system does not report Off" % refused

    def settled(self, reset):
        """Read the settings a reset applies until they report the declared values,
        within the settings poll's share of the bound and until the step is
        asked to stop: no pause outlasts either, and no read starts after them."""
        wanted = {path: self.wanted[path] for path in reset}
        deadline = self.client.clock() + redfish_control.FIRMWARE_POLL_BUDGET
        for _poll in range(redfish_control.FIRMWARE_POLLS):
            pause = min(redfish_control.FIRMWARE_POLL_DELAY, deadline - self.client.clock())
            if pause <= 0 or self.stopped():
                return False
            self.client.sleep(pause)
            secure = self.fresh("SecureBoot", deadline) if SECURE_BOOT in wanted else None
            bios = self.fresh("Bios", deadline) if any(path.startswith(BIOS) for path in wanted) else None
            if not differences(wanted, observed(wanted, None, secure, bios)):
                return True
        return False

    def fresh(self, name, deadline):
        """One poll's read of a linked resource; one that does not answer, or that
        the poll has no time left for or was asked to stop before, is not yet applied."""
        left = min(redfish_control.REQUEST_TIMEOUT, deadline - self.client.clock())
        if left <= 0 or self.stopped():
            return None
        status, body, _headers = self.client.fetch(self.links[name], timeout=left,
                                                   doing="reading the %s resource" % name)
        return body if status == 200 and isinstance(body, dict) else None


def _refused_reset(status, body):
    """A refused reset's line: its status and the controller's message identifier."""
    found = redfish_discovery.message_id(body)
    return "resetting: HTTP %d%s" % (status, " (%s)" % found if found else "")


def _secure_boot(resource):
    """The one Secure Boot property a step writes, as a settings map."""
    return {"SecureBootEnable": resource["SecureBootEnable"]} if "SecureBootEnable" in resource else {}
