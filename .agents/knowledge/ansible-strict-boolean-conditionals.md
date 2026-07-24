# Ansible conditionals require boolean expressions

Ansible rejects a `when` expression whose result is a mapping, list, string, or
other value even when that value would traditionally be truthy. Hook references
arrive through the JSON extra-vars object and retain their mapping type, so a
guard such as `when: bootwright_gateway` fails when the gateway exists.

Test presence explicitly with a boolean expression such as
`when: bootwright_gateway is not none`. Use comparisons or boolean filters for
other conditionals instead of relying on container or scalar truthiness.
