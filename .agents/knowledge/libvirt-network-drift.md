# Libvirt network drift and stale bridge conflicts

**Symptom:** Libvirt refuses to define a network, complains about a UUID mismatch, or reports that a bridge is already in use by another network. Re-applying after a partial run leaves the old network XML active.

**Root cause:** `net-define` rejects XML whose UUID differs from the persistent network definition. A running libvirt network also keeps its old XML until it is stopped, and stale networks from prior state can still claim the same bridge name.

**Fix:** Probe the target and every bridge claimant conclusively. Reuse the
existing UUID and reconcile fingerprint drift only when the live XML description
names Bootwright, the exact current context, and the expected cluster. A
same-name or same-bridge network with missing, malformed, or foreign identity is
a refusal, never an object to stop or undefine. Destroy applies the same live
classifier; a stale controller record cannot override contradictory XML.
