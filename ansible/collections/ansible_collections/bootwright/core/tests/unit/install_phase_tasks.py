"""Read installation phases in their actual playbook order for structural probes."""


def apply_tasks(loader, role):
    plays = loader.load_from_file(str(role.parents[1] / 'playbooks/containercluster/install_apply.yml'), trusted_as_template=True)
    result = []

    def append(items):
        for task in items:
            result.append(task)
            if 'block' in task:
                append(task['block'])
    for play in plays:
        for task in play['tasks']:
            entry = task['ansible.builtin.import_role']
            assert entry['name'] == 'bootwright.core.containercluster_install_agent'
            append(loader.load_from_file(str(role / 'tasks' / (entry['tasks_from'] + '.yml')), trusted_as_template=True))
    return result
