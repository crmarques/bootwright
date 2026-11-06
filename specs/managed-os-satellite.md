# Satellite content selection

A managed RHEL registration may explicitly select one ordinary or composite
content view and its lifecycle environment through the
[RHSM fields](api/secrets.md#rhsm). They name labels of an existing published
view/environment pair; Bootwright creates, publishes and promotes no views.

The private D-Bus `RegisterWithActivationKeys` options contain
`environment_names`, a string `<lifecycleEnvironment>/<contentView>`, while
connection options stay empty. Omitting both fields leaves the registration
options empty and uses the activation key's selection. The choice is frozen in
the installation request and digest; continuation never drops it or retries
without it. This uses the native service's named-environment option rather than
the CLI registration interface, which refuses activation keys with environments.

Before committing a fresh registration's consumer to the ownership record,
adopting a pending consumer, or using an owned consumer, the worker runs
`subscription-manager identity` with its fixed C locale, capturing the bounded
answer for at most 60 seconds. The answer must name exactly this consumer UUID
and one `environment name:` or `environment names:` line equal to the requested
pair. The five native fields must appear exactly once in their native order:
identity, name, organization name, organization ID, environment name(s). Extra
lines, including newlines within remote identity metadata, are refused.
Missing, duplicated, multiple or mismatching identities or environments
refuse `registration-refused` before release or repository effects. This also
prevents a server that ignores named environments from silently falling back
to the activation key's selection. A failed, timed-out or oversized identity
read refuses `registration-unavailable`; no captured identity or native error
text enters the result. A refused new registration retains its pending
ownership record, allowing the ordinary owned
[unregistration](managed-os.md#registration) path to remove its consumer.

An RHSM client lacking the named-environment option refuses
`registration-refused`. Its diagnostic names the declaration fields and the
required native option, alongside the organization and activation key Secret
references. Omission retains the existing registration and adoption behavior.
No secret enters argv, and endpoint verification and secret delivery follow
[registration](managed-os.md#registration).

Native contracts: [registration service](https://github.com/candlepin/subscription-manager/blob/subscription-manager-1.29.45-1/src/rhsmlib/services/register.py),
[connection option encoding](https://github.com/candlepin/subscription-manager/blob/subscription-manager-1.29.45-1/src/rhsm/connection.py)
and [identity output](https://github.com/candlepin/subscription-manager/blob/subscription-manager-1.29.45-1/src/subscription_manager/cli_command/identity.py).
