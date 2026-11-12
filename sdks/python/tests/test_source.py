"""Searches of the SDK's own source that the contract and the checklist ask for, run on every `bin/test`."""

from __future__ import annotations

import ast
import re
import sys
import unittest
from pathlib import Path

SOURCE = Path(__file__).resolve().parent.parent / "src" / "toggleslash"
FILES = sorted(SOURCE.glob("*.py"))

EVALUATOR = {
    "_bucket.py",
    "_clause.py",
    "_context.py",
    "_dates.py",
    "_evaluate.py",
    "_pattern.py",
    "_reference.py",
    "_ruleset.py",
    "_semver.py",
    "_text.py",
}


def imports(path: Path) -> set[str]:
    names: set[str] = set()
    for node in ast.walk(ast.parse(path.read_text(encoding="utf-8"))):
        if isinstance(node, ast.Import):
            names.update(alias.name.split(".")[0] for alias in node.names)
        elif isinstance(node, ast.ImportFrom) and node.level == 0 and node.module:
            names.add(node.module.split(".")[0])
    return names


class SourceSearches(unittest.TestCase):
    def test_there_are_sources_to_search(self) -> None:
        self.assertGreaterEqual(len(FILES), 15)
        self.assertTrue({path.name for path in FILES} >= EVALUATOR)

    def test_the_evaluator_imports_no_regular_expression_module(self) -> None:
        for path in FILES:
            if path.name in EVALUATOR:
                with self.subTest(file=path.name):
                    self.assertFalse({"re", "regex", "fnmatch", "glob"} & imports(path))

    def test_no_dependency_beyond_the_standard_library(self) -> None:
        allowed = set(sys.stdlib_module_names) | {"toggleslash"}
        for path in FILES:
            with self.subTest(file=path.name):
                self.assertLessEqual(imports(path), allowed)

    def test_the_sdk_writes_no_log_line_and_prints_nothing(self) -> None:
        for path in FILES:
            tree = ast.parse(path.read_text(encoding="utf-8"))
            with self.subTest(file=path.name):
                self.assertFalse({"logging", "warnings", "traceback", "pprint"} & imports(path))
                for node in ast.walk(tree):
                    if isinstance(node, ast.Call) and isinstance(node.func, ast.Name):
                        self.assertNotIn(node.func.id, {"print", "breakpoint", "input"})
                    if isinstance(node, ast.Attribute) and isinstance(node.value, ast.Name):
                        self.assertFalse(
                            node.value.id == "sys" and node.attr in {"stdout", "stderr"},
                            f"{path.name}:{node.lineno} writes to a standard stream",
                        )

    def test_the_only_request_headers_the_sdk_names_are_the_allowed_ones(self) -> None:
        allowed = {
            "authorization",
            "accept",
            "if-none-match",
            "content-type",
            # Added by the SDK's own HTTP layer, listed in RUN.md with why none identifies a user or a machine.
            "host",
            "connection",
            "content-length",
        }
        for path in FILES:
            text = path.read_text(encoding="utf-8")
            for line_number, line in enumerate(text.splitlines(), 1):
                for match in re.finditer(r'"((?:[A-Z][a-z]+)(?:-[A-Z][a-z]+)*)"', line):
                    name = match.group(1)
                    if "-" in name or name in {"Authorization", "Accept"}:
                        with self.subTest(file=path.name, line=line_number, header=name):
                            self.assertIn(name.lower(), allowed)

    def test_nothing_names_the_user_or_the_machine(self) -> None:
        everything = "\n".join(path.read_text(encoding="utf-8") for path in FILES).lower()
        for needle in (
            "user-agent",
            "gethostname",
            "getpass",
            "platform.",
            "os.environ",
            "getlogin",
        ):
            with self.subTest(needle=needle):
                self.assertNotIn(needle, everything)


if __name__ == "__main__":
    unittest.main()
