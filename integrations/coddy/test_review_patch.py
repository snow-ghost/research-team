"""Verify the pinned Coddy patch with mocked agents, Git and GitHub."""

import ast
import logging
import os
from pathlib import Path
import subprocess
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock


REVISION = "fc3b9491d76d5911aa78e51fc6494ddf3c21bb77"
PATCH = Path(__file__).parent / "patches" / "review-loop-v1.patch"


class ReviewPatchTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        source = os.environ.get("CODDY_SOURCE")
        if not source:
            raise unittest.SkipTest("Set CODDY_SOURCE to the reviewed Coddy checkout")
        head = subprocess.check_output(
            ["git", "-C", source, "rev-parse", "HEAD"], text=True
        ).strip()
        if head != REVISION:
            raise RuntimeError("Coddy revision mismatch")
        # Read committed source, never execute a potentially modified worktree.
        original = subprocess.check_output(
            ["git", "-C", source, "show", f"{REVISION}:coddy/worker/review_loop.py"],
            text=True,
        )
        cls.temp = tempfile.TemporaryDirectory()
        cls.addClassCleanup(cls.temp.cleanup)
        root = Path(cls.temp.name)
        module = root / "coddy" / "worker" / "review_loop.py"
        module.parent.mkdir(parents=True)
        module.write_text(original)
        subprocess.run(["git", "apply", str(PATCH.resolve())], cwd=root, check=True)
        tree = ast.parse(module.read_text())
        function = next(
            node for node in tree.body
            if isinstance(node, ast.FunctionDef) and node.name == "run_review_loop_for_pr"
        )
        subset = ast.Module(
            body=[ast.ImportFrom(module="__future__", names=[ast.alias(name="annotations")], level=0), function],
            type_ignores=[],
        )
        cls.code = compile(ast.fix_missing_locations(subset), str(module), "exec")

    def setUp(self):
        self.publish = Mock()
        self.namespace = {
            "LOG": logging.getLogger("coddy-patch-test"),
            "ReviewComment": SimpleNamespace,
            "GitPlatformError": RuntimeError,
            "fetch_and_checkout_branch": Mock(),
            "checkout_branch": Mock(),
            "set_commit_author": Mock(),
            "read_review_reply": Mock(return_value=None),
            "commit_all_and_push": self.publish,
        }
        exec(self.code, self.namespace)
        self.adapter = Mock()
        self.adapter.get_pr.return_value = SimpleNamespace(head_branch="7-research-example")
        self.agent = Mock()
        self.agent.process_review_item.return_value = "addressed"
        comment = SimpleNamespace(
            in_reply_to_id=None, comment_id=91, content="Check n=1", name="alice",
            path="generator.go", line=1, created_at=1, updated_at=1,
        )
        self.pr = SimpleNamespace(
            pr_id=8, issue_id=7, reviews=[SimpleNamespace(comments=[comment])]
        )

    def run_review(self, name="Coddy Bot"):
        return self.namespace["run_review_loop_for_pr"](
            self.adapter, self.agent, self.pr, "org/lab", Path(self.temp.name),
            bot_name=name, bot_email="bot@example.test", default_branch="main",
        )

    def test_branch_message_and_identity_are_forwarded(self):
        self.assertEqual(self.run_review(), "success")
        self.assertEqual(
            self.publish.call_args.args,
            ("7-research-example", "#7 Address review comments on PR #8",
             "Coddy Bot", "bot@example.test"),
        )

    def test_publish_failure_is_not_success(self):
        self.publish.side_effect = RuntimeError("simulated failure")
        self.assertEqual(self.run_review(), "failed")

    def test_missing_identity_prevents_publish(self):
        self.assertEqual(self.run_review(name=None), "failed")
        self.publish.assert_not_called()


if __name__ == "__main__":
    unittest.main()
