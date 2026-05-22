# Libvirt network drift and stale bridge conflicts

**Symptom:** Libvirt refuses to define a network, complains about a UUID mismatch, or reports that a bridge is already in use by another network. Re-applying after a partial run leaves the old network XML active.

**Root cause:** `net-define` rejects XML whose UUID differs from the persistent network definition. A running libvirt network also keeps its old XML until it is stopped, and stale networks from prior state can still claim the same bridge name.

**Fix:** Reuse the existing UUID when the target network exists, fingerprint rendered network inputs in the XML description, stop the live network when the fingerprint drifts, and remove other networks that claim the desired bridge before defining the new one.

