"""Execute the shared cluster object read entry point on the controller."""
from __future__ import annotations

from ansible.plugins.action import ActionBase
from ansible_collections.bootwright.core.plugins.module_utils.cluster_objects_engine import Engine, Refused


class ActionModule(ActionBase):
    TRANSFERS_FILES = False

    def run(self, tmp=None, task_vars=None):
        del tmp
        try:
            result = self._run_program(task_vars)
        except (ValueError, TypeError, KeyError, OSError):
            result = {"failed": True, "changed": False, "msg": "cluster object request or answer is invalid"}
        result["_ansible_no_log"] = True
        return result

    def _run_program(self, task_vars):
        request = dict(self._task.args["request"])
        if "secretSources" in request:
            materials = request.get("materials", {})
            request["materials"] = {secret: {part: materials[variable] for part, variable in parts.items()}
                                    for secret, parts in request["secretSources"].items()}
        base = {key: request[key] for key in ("oc", "kubeconfig", "deadline")}

        def read(obj, fields):
            transport = dict(base, object=obj, fields=fields)
            return self._execute_module(module_name="bootwright.core.cluster_objects_read",
                                        module_args={"request": transport}, task_vars=task_vars)

        def write(action, obj, extra):
            transport = dict(base, action=action, object=obj, **extra)
            return self._execute_module(module_name="bootwright.core.cluster_objects_write",
                                        module_args={"request": transport}, task_vars=task_vars)

        try:
            return Engine(request, read, None).execute(observe=True)
        except Refused as refused:
            return {"failed": True, "changed": False, "msg": str(refused)}
        except (ValueError, TypeError, KeyError, OSError):
            return {"failed": True, "changed": False, "msg": "cluster object request or answer is invalid"}
