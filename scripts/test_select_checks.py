#!/usr/bin/env python3
"""Unit tests for the change-scope selector."""

from __future__ import annotations

import importlib.util
from pathlib import Path
import tempfile
import unittest


def load_selector():
    path = Path(__file__).resolve().parent / "select-checks.py"
    spec = importlib.util.spec_from_file_location("select_checks", path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


selector = load_selector()


class ClassifyTest(unittest.TestCase):
    def setUp(self):
        self.root = Path(tempfile.mkdtemp())

    def test_go_source_is_the_go_domain(self):
        self.assertEqual(selector.classify(self.root, {"internal/cli/apply_cmd.go"}), {"go"})

    def test_api_source_is_both_go_and_api(self):
        self.assertEqual(selector.classify(self.root, {"api/v1alpha1/machine.go"}), {"go", "api"})

    def test_docs_and_specs_are_distinct_domains(self):
        self.assertEqual(selector.classify(self.root, {"docs/concepts/storage.md"}), {"docs"})
        self.assertEqual(selector.classify(self.root, {"specs/state-model.md"}), {"specs"})
        self.assertEqual(selector.classify(self.root, {"mkdocs.yml"}), {"docs"})

    def test_agent_guidance_is_the_agents_domain(self):
        self.assertEqual(selector.classify(self.root, {"AGENTS.md"}), {"agents"})
        self.assertEqual(selector.classify(self.root, {".agents/knowledge/KNOWLEDGE.md"}), {"agents"})

    def test_examples_and_e2e_are_fixtures(self):
        self.assertEqual(selector.classify(self.root, {"examples/a/machine.yaml"}), {"fixtures"})
        self.assertEqual(selector.classify(self.root, {"test/e2e/001-sno-libvirt/x.yaml"}), {"fixtures"})

    def test_shell_shebang_adds_the_shell_domain(self):
        script = self.root / "ansible" / "run.sh"
        script.parent.mkdir(parents=True)
        script.write_text("#!/bin/bash\necho hi\n")
        self.assertEqual(selector.classify(self.root, {"ansible/run.sh"}), {"ansible", "shell"})

    def test_a_non_shell_file_never_adds_the_shell_domain(self):
        target = self.root / "ansible" / "play.yml"
        target.parent.mkdir(parents=True)
        target.write_text("- hosts: all\n")
        self.assertEqual(selector.classify(self.root, {"ansible/play.yml"}), {"ansible"})


class StagesTest(unittest.TestCase):
    def test_go_change_runs_the_go_guardrails_only(self):
        self.assertEqual(selector.stages_for({"go"}), set(selector.GO_STAGES))

    def test_docs_change_runs_the_docs_build_and_stale_terms(self):
        stages = selector.stages_for({"docs"})
        self.assertIn("docs-check", stages)
        self.assertIn("stale-term-check", stages)
        self.assertNotIn("check-gofmt", stages)

    def test_ansible_change_runs_syntax_check_and_bundle_sync(self):
        stages = selector.stages_for({"ansible"})
        self.assertIn("ansible-syntax-check", stages)
        self.assertIn("sync-bundle", stages)

    def test_workflow_yaml_change_runs_only_the_workflow_lint(self):
        self.assertEqual(selector.stages_for({"workflows"}), {"workflow-yaml-check"})

    def test_no_domain_runs_no_stage(self):
        self.assertEqual(selector.stages_for(set()), set())


class PackagesTest(unittest.TestCase):
    def setUp(self):
        self.root = Path(tempfile.mkdtemp())

    def test_prose_change_selects_the_prose_readers(self):
        packages = selector.packages_for(self.root, {"docs/x.md"}, {"docs"}, "scoped")
        self.assertEqual(set(packages), set(selector.PROSE_READER_PACKAGES))

    def test_agent_guidance_alone_selects_only_the_repo_guards(self):
        packages = selector.packages_for(self.root, {"AGENTS.md"}, {"agents"}, "scoped")
        self.assertEqual(set(packages), {"./internal/repo/checks/..."})

    def test_agent_guidance_with_docs_still_selects_every_prose_reader(self):
        packages = selector.packages_for(self.root, {"AGENTS.md", "docs/x.md"}, {"agents", "docs"}, "scoped")
        self.assertEqual(set(packages), set(selector.PROSE_READER_PACKAGES))

    def test_fixture_change_selects_the_fixture_readers(self):
        packages = selector.packages_for(self.root, {"examples/a.yaml"}, {"fixtures"}, "scoped")
        self.assertEqual(set(packages), set(selector.FIXTURE_READER_PACKAGES))

    def test_ansible_change_selects_the_bundle_and_guard_packages(self):
        packages = selector.packages_for(self.root, {"ansible/x.yml"}, {"ansible"}, "scoped")
        self.assertEqual(set(packages), set(selector.ANSIBLE_READER_PACKAGES))

    def test_feature_tier_raises_the_floor_above_a_prose_only_change(self):
        scoped = set(selector.packages_for(self.root, {"docs/x.md"}, {"docs"}, "scoped"))
        feature = set(selector.packages_for(self.root, {"docs/x.md"}, {"docs"}, "feature"))
        self.assertTrue(scoped < feature)
        self.assertTrue(set(selector.FEATURE_FLOOR_PACKAGES) <= feature)

    def test_feature_tier_stays_empty_when_nothing_was_selected(self):
        self.assertEqual(selector.packages_for(self.root, set(), set(), "feature"), [])


class PackagePatternTest(unittest.TestCase):
    def test_module_root_becomes_dot(self):
        selector._MODULE_CACHE.clear()
        selector._MODULE_CACHE.append("example.com/mod")
        self.assertEqual(selector.package_pattern("example.com/mod"), ".")

    def test_subpackage_becomes_a_relative_pattern(self):
        selector._MODULE_CACHE.clear()
        selector._MODULE_CACHE.append("example.com/mod")
        self.assertEqual(selector.package_pattern("example.com/mod/internal/cli"), "./internal/cli")

    def test_foreign_import_path_is_left_alone(self):
        selector._MODULE_CACHE.clear()
        selector._MODULE_CACHE.append("example.com/mod")
        self.assertEqual(selector.package_pattern("other.example/pkg"), "other.example/pkg")


class ShellQuoteTest(unittest.TestCase):
    def test_embedded_single_quote_is_escaped(self):
        self.assertEqual(selector.shell_quote("a'b"), "'a'\\''b'")

    def test_empty_value_is_still_quoted(self):
        self.assertEqual(selector.shell_quote(""), "''")


class CollectChangedTest(unittest.TestCase):
    def test_a_non_git_root_widens_to_the_full_gate(self):
        root = Path(tempfile.mkdtemp())
        changed, reason = selector.collect_changed(root, "main", 250)
        self.assertEqual(changed, set())
        self.assertIn("not a git worktree", reason)


if __name__ == "__main__":
    unittest.main()
