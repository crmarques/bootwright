# OpenShift Agent ISO Squashfs Read Errors After Virtual Media Eject

## Symptom

`openshift-install agent wait-for install-complete` loops on:

```text
Agent Rest API never initialized. Bootstrap Kube API never initialized
```

The VM console shows repeated live ISO filesystem errors:

```text
SQUASHFS error: Unable to read page
SQUASHFS error: Unable to read fragment cache entry
```

The node can still have its expected IP and SSH port open, but the Assisted
Service / agent API never reaches readiness.

## Likely Cause

The Redfish boot role detached the virtual CD as soon as SSH opened on the
live ISO. Newer RHCOS agent ISOs can still read squashfs pages from virtual
media after SSH is available, so removing the CD corrupts the running live
environment before the agent API initializes.

## Fix

Do not eject virtual media during the live ISO phase. After the node reaches
SSH, change subsequent boots back to disk with a Redfish boot-device PATCH
and keep the ISO attached while `openshift-install agent wait-for
install-complete` runs. Pre-insert cleanup still removes stale media before
the next run.
