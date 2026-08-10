from __future__ import annotations

import re
from urllib.parse import urlsplit


_MAC_RE = re.compile(r"^[0-9a-f]{2}(:[0-9a-f]{2}){5}$")


def bootwright_redfish_url(ref, base_url):
    ref_text = _string(ref).strip()
    if not ref_text:
        return ""
    parsed = urlsplit(ref_text)
    if parsed.scheme in {"http", "https"} and parsed.netloc:
        return ref_text
    base_text = _string(base_url).rstrip("/")
    if not base_text:
        return ref_text
    if ref_text.startswith("/"):
        return base_text + ref_text
    return base_text + "/" + ref_text


def bootwright_redfish_system_id(configured, collection=None):
    configured_text = _string(configured).strip()
    if configured_text:
        return configured_text
    if not isinstance(collection, dict):
        return ""
    members = collection.get("Members")
    if not isinstance(members, list) or not members or not isinstance(members[0], dict):
        return ""
    ref = _string(members[0].get("@odata.id")).strip().rstrip("/")
    if not ref:
        return ""
    return ref.rsplit("/", 1)[-1]


def bootwright_vmedia_action_descriptors(resource, action_name):
    descriptors = []
    for path, source, vendor, action in _iter_action_objects(resource, action_name):
        target = action.get("target")
        if not isinstance(target, str) or not target:
            continue
        descriptor = {
            "target": target,
            "path": path,
            "source": source,
        }
        action_info = action.get("@Redfish.ActionInfo")
        if isinstance(action_info, str) and action_info:
            descriptor["actionInfo"] = action_info
        if vendor:
            descriptor["vendor"] = vendor
        descriptors.append(descriptor)
    return descriptors


def bootwright_redfish_vmm_control_actions(actions, action_info_results):
    if not isinstance(actions, list):
        return []
    if not isinstance(action_info_results, list):
        action_info_results = []

    by_target = {}
    for result in action_info_results:
        if not isinstance(result, dict):
            continue
        item = result.get("item")
        if not isinstance(item, dict):
            continue
        target = item.get("target")
        if isinstance(target, str) and target:
            by_target[target] = result

    supported = []
    for action in actions:
        if not isinstance(action, dict):
            continue
        target = action.get("target")
        action_info = action.get("actionInfo")
        result = by_target.get(target)
        if not isinstance(target, str) or not target:
            continue
        if not isinstance(action_info, str) or not action_info:
            continue
        if not isinstance(result, dict) or _status_code(result.get("status")) != 200:
            continue
        if _action_info_accepts_vmm_control(result.get("json")):
            supported.append(dict(action, bodyStyle="vmm-control"))
    return supported


def bootwright_redfish_power_on_reset_type(resource, action_info_results=None):
    values = _reset_type_allowable_values(resource, action_info_results)
    if not values:
        return "On"
    for candidate in ("ForceOn", "On", "PushPowerButton"):
        if candidate in values:
            return candidate
    return "On"


def bootwright_redfish_vmedia_attached(resource, expected_image, expected_protocol=""):
    if not isinstance(resource, dict):
        return False
    if not _truthy(resource.get("Inserted")):
        return False
    if not _protocol_matches(resource.get("TransferProtocolType"), expected_protocol):
        return False
    return _image_matches(resource.get("Image"), expected_image)


def bootwright_redfish_ethernet_macs(results):
    if isinstance(results, dict):
        results = results.get("results", [])
    if not isinstance(results, list):
        return []
    macs = set()
    for result in results:
        if not isinstance(result, dict) or _status_code(result.get("status")) != 200:
            continue
        resource = result.get("json")
        if not isinstance(resource, dict):
            continue
        for field in ("MACAddress", "PermanentMACAddress"):
            mac = _canonical_mac(resource.get(field))
            if mac:
                macs.add(mac)
    return sorted(macs)


def bootwright_redfish_mac_validation(declared_interfaces, redfish_results):
    declared = []
    if isinstance(declared_interfaces, list):
        for iface in declared_interfaces:
            if not isinstance(iface, dict):
                continue
            mac = _canonical_mac(iface.get("macAddress"))
            if not mac:
                continue
            name = _string(iface.get("name"))
            display = f"{name}={mac}" if name else mac
            declared.append({"name": name, "macAddress": mac, "display": display})

    observed = bootwright_redfish_ethernet_macs(redfish_results)
    observed_set = set(observed)
    missing = [iface for iface in declared if iface["macAddress"] not in observed_set]
    return {
        "supported": len(observed) > 0,
        "declared": declared,
        "observed": observed,
        "missing": missing,
    }


def bootwright_redfish_tpm2_evidence(resource):
    if not isinstance(resource, dict) or "TrustedModules" not in resource:
        return _tpm2_evidence(False, "TrustedModules is not reported", [], 0, 0)
    modules = resource.get("TrustedModules")
    if not isinstance(modules, list):
        return _tpm2_evidence(False, "TrustedModules is not an array", [], 0, 0)

    observed = []
    tpm20_count = 0
    ready_count = 0
    for index, module in enumerate(modules):
        if not isinstance(module, dict):
            observed.append(f"[{index}] malformed module entry")
            continue
        interface_type = _evidence_value(module.get("InterfaceType"))
        status = module.get("Status")
        if not isinstance(status, dict):
            status = {}
        state = _evidence_value(status.get("State"))
        health = _evidence_value(status.get("Health"))
        observed.append(
            f"[{index}] InterfaceType={interface_type}, State={state}, Health={health}"
        )
        if interface_type != "TPM2_0":
            continue
        tpm20_count += 1
        if state == "Enabled" and health == "OK":
            ready_count += 1

    if ready_count > 0:
        reason = "a TPM2_0 module is enabled and healthy"
    elif not modules:
        reason = "TrustedModules is empty"
    elif tpm20_count == 0:
        reason = "no module reports InterfaceType=TPM2_0"
    else:
        reason = "no TPM2_0 module reports State=Enabled and Health=OK"
    return _tpm2_evidence(ready_count > 0, reason, observed, tpm20_count, ready_count)


def _tpm2_evidence(proven, reason, observed, tpm20_count, ready_count):
    return {
        "proven": proven,
        "reason": reason,
        "observed": observed[:16],
        "trustedModuleCount": len(observed),
        "tpm20Count": tpm20_count,
        "readyCount": ready_count,
    }


def _evidence_value(value):
    text = _string(value).strip()
    if not text:
        return "<not reported>"
    return re.sub(r"[^A-Za-z0-9_.:-]", "?", text)[:64]


def _action_info_accepts_vmm_control(action_info):
    if not isinstance(action_info, dict):
        return False
    parameters = action_info.get("Parameters")
    if not isinstance(parameters, list):
        return False

    by_name = {}
    for parameter in parameters:
        if not isinstance(parameter, dict):
            continue
        name = parameter.get("Name")
        if isinstance(name, str):
            by_name[name] = parameter

    if "Image" not in by_name or "VmmControlType" not in by_name:
        return False

    values = by_name["VmmControlType"].get("AllowableValues")
    if values is None:
        return True
    if not isinstance(values, list):
        return False
    return {"Connect", "Disconnect"}.issubset(
        {value for value in values if isinstance(value, str)}
    )


def _reset_type_allowable_values(resource, action_info_results):
    values = []
    for _path, _source, _vendor, action in _iter_action_objects(resource, "#ComputerSystem.Reset"):
        values.extend(_string_list(action.get("ResetType@Redfish.AllowableValues")))

    if isinstance(action_info_results, dict):
        action_info_results = action_info_results.get("results", [])
    if isinstance(action_info_results, list):
        for result in action_info_results:
            if not isinstance(result, dict) or _status_code(result.get("status")) != 200:
                continue
            values.extend(_action_info_parameter_values(result.get("json"), "ResetType"))

    seen = set()
    ordered = []
    for value in values:
        if value not in seen:
            seen.add(value)
            ordered.append(value)
    return ordered


def _iter_action_objects(resource, action_name):
    """Yield (path, source, vendor, action) for each occurrence of action_name in
    the resource's standard Actions and each Oem vendor's Actions."""
    if not isinstance(resource, dict) or not isinstance(action_name, str) or not action_name:
        return
    container = resource.get("Actions")
    if isinstance(container, dict):
        action = container.get(action_name)
        if isinstance(action, dict):
            yield f"Actions.{action_name}", "standard", "", action
    oem = resource.get("Oem")
    if isinstance(oem, dict):
        for vendor, value in oem.items():
            if not isinstance(vendor, str) or not isinstance(value, dict):
                continue
            container = value.get("Actions")
            if isinstance(container, dict):
                action = container.get(action_name)
                if isinstance(action, dict):
                    yield f"Oem.{vendor}.Actions.{action_name}", "oem", vendor, action


def _action_info_parameter_values(action_info, parameter_name):
    if not isinstance(action_info, dict):
        return []
    parameters = action_info.get("Parameters")
    if not isinstance(parameters, list):
        return []
    for parameter in parameters:
        if not isinstance(parameter, dict):
            continue
        if parameter.get("Name") == parameter_name:
            return _string_list(parameter.get("AllowableValues"))
    return []


def _string_list(value):
    if not isinstance(value, list):
        return []
    return [item for item in value if isinstance(item, str) and item]


def _status_code(value):
    try:
        return int(value or 0)
    except (TypeError, ValueError):
        return 0


def _truthy(value):
    if isinstance(value, bool):
        return value
    if isinstance(value, str):
        return value.strip().lower() in {"1", "true", "yes", "on"}
    return bool(value)


def _protocol_matches(observed, expected):
    observed_text = _string(observed).upper()
    expected_text = _string(expected).upper()
    return not observed_text or not expected_text or observed_text == expected_text


def _image_matches(observed, expected):
    observed_text = _string(observed)
    expected_text = _string(expected)
    if not observed_text:
        return True
    if observed_text == expected_text:
        return True
    return _url_matches_with_optional_observed_port(observed_text, expected_text)


def _url_matches_with_optional_observed_port(observed, expected):
    try:
        observed_url = urlsplit(observed)
        expected_url = urlsplit(expected)
        observed_port = observed_url.port
        expected_port = expected_url.port
    except ValueError:
        return False

    if not observed_url.scheme or not expected_url.scheme:
        return False
    if observed_url.scheme.lower() != expected_url.scheme.lower():
        return False
    if (observed_url.hostname or "").lower() != (expected_url.hostname or "").lower():
        return False
    if observed_url.path != expected_url.path:
        return False
    if observed_url.query != expected_url.query:
        return False
    if observed_url.fragment != expected_url.fragment:
        return False
    return observed_port == expected_port or observed_port is None


def _string(value):
    return value if isinstance(value, str) else ""


def _canonical_mac(value):
    text = _string(value).strip().lower()
    compact = re.sub(r"[^0-9a-f]", "", text)
    if len(compact) == 12:
        text = ":".join(compact[i : i + 2] for i in range(0, 12, 2))
    else:
        text = text.replace("-", ":")
    if _MAC_RE.match(text):
        return text
    return ""


class FilterModule:
    def filters(self):
        return {
            "bootwright_vmedia_action_descriptors": bootwright_vmedia_action_descriptors,
            "bootwright_redfish_mac_validation": bootwright_redfish_mac_validation,
            "bootwright_redfish_system_id": bootwright_redfish_system_id,
            "bootwright_redfish_url": bootwright_redfish_url,
            "bootwright_redfish_vmedia_attached": bootwright_redfish_vmedia_attached,
            "bootwright_redfish_power_on_reset_type": bootwright_redfish_power_on_reset_type,
            "bootwright_redfish_tpm2_evidence": bootwright_redfish_tpm2_evidence,
            "bootwright_redfish_vmm_control_actions": bootwright_redfish_vmm_control_actions,
        }
