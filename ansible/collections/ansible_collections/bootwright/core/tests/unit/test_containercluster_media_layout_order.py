"""The media block proves its publication layout before any publication effect.

build.yml publishes through file, find and mv tasks that follow links, so the
layout the installation block's walk accepts is proved before build.yml is
included, unconditionally, and destroy.yml proves it before it removes the
reserved subtree.
"""

import pathlib

from ansible.parsing.dataloader import DataLoader

MEDIA = pathlib.Path(__file__).resolve().parents[2] / 'roles' / 'containercluster_media_agent'
LOADER = DataLoader()
LAYOUT = 'bootwright.core.containercluster_media_layout'
REQUEST = '{{ bootwright_cluster_media_request }}'
# What the apply runs before the layout is proved: checks, the protocol's
# progress and the installer's version read, none of which touches the
# served root.
BEFORE = {'ansible.builtin.assert', 'bootwright.core.containercluster_media_protocol', 'ansible.builtin.command'}


def tasks(name):
    return LOADER.load_from_file(str(MEDIA / 'tasks' / name))


def flat(items):
    for task in items:
        yield task
        for key in ('block', 'rescue', 'always'):
            yield from flat(task.get(key) or [])


def modules(task):
    return {key for key in task if '.' in key}


def proof(items, operation):
    found = [index for index, task in enumerate(items) if LAYOUT in task]
    assert len(found) == 1, found
    task = items[found[0]]
    assert task[LAYOUT] == {'request': REQUEST, 'operation': operation}
    assert not {'when', 'failed_when', 'ignore_errors', 'rescue'} & set(task)
    return found[0]


def test_the_apply_proves_the_layout_before_it_includes_the_build():
    apply = tasks('apply.yml')
    proved = proof(apply, 'publish')
    included = [index for index, task in enumerate(apply)
                if task.get('ansible.builtin.include_tasks') == 'build.yml']
    assert len(included) == 1 and proved < included[0]
    for task in apply[:proved]:
        assert modules(task) <= BEFORE, task['name']
        command = task.get('ansible.builtin.command')
        assert command is None or command['argv'][1:] == ['version'], task['name']
    assert not any(LAYOUT in task for task in flat(tasks('build.yml')))
    assert not any('ansible.builtin.include_tasks' in task or 'ansible.builtin.import_tasks' in task
                   for task in flat(tasks('build.yml')))


def test_the_destroy_proves_the_layout_before_it_removes_the_reserved_subtree():
    destroy = tasks('destroy.yml')
    proved = proof(destroy, 'remove')
    removals = [index for index, task in enumerate(destroy)
                if (task.get('ansible.builtin.file') or {}).get('state') == 'absent']
    assert removals and proved < min(removals)
    assert all(modules(task) <= BEFORE - {'ansible.builtin.command'} for task in destroy[:proved])
