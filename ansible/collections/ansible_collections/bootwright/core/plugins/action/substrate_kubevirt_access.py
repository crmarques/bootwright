"""Lend one KubeVirt provider hosted by a cluster of the context its access, on the controller.

This alone reads the host installation's administrator kubeconfig. It accepts
its form as every KubeVirt connection does, mints the token the frozen lend
request names and writes the kubeconfig carrying only that token to the run's
output file. Nothing this returns carries a kubeconfig, a token or a key; every
result is censored.
"""

from __future__ import annotations

from ansible.module_utils.common.yaml import yaml_load
from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils import kubevirt_access
from ansible_collections.bootwright.core.plugins.module_utils.kubevirt_api import Answered, Client, Refused, Unanswered, load


def lend(arguments, transport=None):
    client = Client(load(arguments["kubeconfig"], yaml_load), scratch=arguments["scratch"], transport=transport)
    data, evidence = kubevirt_access.mint(client, arguments["request"])
    kubevirt_access.write(arguments["output"], data)
    return evidence


class ActionModule(ActionBase):
    TRANSFERS_FILES = False
    _requires_connection = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        try:
            result = {"changed": False, "observation": lend(dict(self._task.args))}
        except Refused as error:
            result = {"failed": True, "msg": str(error)}
        except (Answered, Unanswered):
            result = {"failed": True, "msg": "the host cluster did not answer the token request"}
        except (KeyError, TypeError, ValueError, OSError):
            result = {"failed": True, "msg": "the KubeVirt host access request or the host's answer is invalid"}
        result["_ansible_no_log"] = True
        return result
