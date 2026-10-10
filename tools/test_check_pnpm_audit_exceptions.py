#!/usr/bin/env python3
"""Exercise the audit checker CLI with valid and failed audit responses."""

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path


PROJECT_ROOT = Path(__file__).resolve().parents[1]
CHECKER = PROJECT_ROOT / "tools" / "check_pnpm_audit_exceptions.py"
TEST_CACHE = PROJECT_ROOT / ".cache" / "publish-v0.2.15-r1"


class AuditReportCLITest(unittest.TestCase):
    def setUp(self):
        TEST_CACHE.mkdir(parents=True, exist_ok=True)
        self.temp = tempfile.TemporaryDirectory(prefix="audit-report-tests-", dir=TEST_CACHE)
        self.root = Path(self.temp.name).resolve()
        self.assertTrue(self.root.is_relative_to(TEST_CACHE.resolve()))
        self.addCleanup(self.temp.cleanup)

    def run_checker(self, report=None, *, raw=None, exceptions="version: 1\nexceptions:\n"):
        audit = self.root / "audit.json"
        audit.write_text(json.dumps(report) if raw is None else raw, encoding="utf-8")
        exception_file = self.root / "exceptions.yml"
        exception_file.write_text(exceptions, encoding="utf-8")
        return subprocess.run(
            [sys.executable, "-B", str(CHECKER), "--audit", str(audit),
             "--exceptions", str(exception_file)],
            capture_output=True, text=True, encoding="utf-8", check=False,
            cwd=PROJECT_ROOT,
        )

    def assert_rejected(self, result):
        self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
        self.assertIn("Invalid pnpm audit report", result.stderr)
        self.assertNotIn("Audit exceptions validated.", result.stdout)
        self.assertNotIn("Traceback", result.stderr)

    def test_rejects_registry_error_response(self):
        self.assert_rejected(self.run_checker({"error": {"code": "EAI_AGAIN"}}))

    def test_rejects_error_even_when_empty_advisories_are_present(self):
        self.assert_rejected(self.run_checker({"error": {"code": "EAI_AGAIN"}, "advisories": {}}))

    def test_rejects_empty_report(self):
        self.assert_rejected(self.run_checker({}))

    def test_rejects_non_object_reports(self):
        for report in (None, [], "unavailable", 0, True):
            with self.subTest(report=report):
                self.assert_rejected(self.run_checker(report))

    def test_rejects_missing_supported_report_sections(self):
        self.assert_rejected(self.run_checker({"metadata": {"vulnerabilities": {"high": 0}}}))

    def test_rejects_wrong_report_section_types(self):
        for key in ("advisories", "vulnerabilities"):
            for value in (None, [], "unavailable", 0):
                with self.subTest(key=key, value=value):
                    self.assert_rejected(self.run_checker({key: value}))

    def test_rejects_wrong_section_type_alongside_a_valid_section(self):
        self.assert_rejected(self.run_checker({"advisories": {}, "vulnerabilities": []}))

    def test_rejects_non_object_advisory_and_vulnerability_entries(self):
        for key in ("advisories", "vulnerabilities"):
            for value in (None, [], "unavailable", 0):
                with self.subTest(key=key, value=value):
                    self.assert_rejected(self.run_checker({key: {"entry": value}}))

    def test_rejects_wrong_severity_type(self):
        for report in (
            {"advisories": {"1": {"module_name": "package", "severity": 3}}},
            {"vulnerabilities": {"package": {"severity": 3, "via": []}}},
        ):
            with self.subTest(report=report):
                self.assert_rejected(self.run_checker(report))

    def test_rejects_wrong_advisory_name_type(self):
        self.assert_rejected(self.run_checker({
            "advisories": {"1": {"module_name": ["package"], "severity": "high"}},
        }))

    def test_rejects_wrong_vulnerability_via_type(self):
        for via in (None, {}, 3, [None], [3]):
            with self.subTest(via=via):
                self.assert_rejected(self.run_checker({
                    "vulnerabilities": {"package": {"severity": "high", "via": via}},
                }))

    def test_rejects_malformed_json_and_empty_file(self):
        for raw in ("", "{", "not JSON"):
            with self.subTest(raw=raw):
                self.assert_rejected(self.run_checker(raw=raw))

    def test_accepts_pnpm9_empty_advisories(self):
        result = self.run_checker({"advisories": {}, "metadata": {}})
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Audit exceptions validated.", result.stdout)

    def test_accepts_new_empty_vulnerabilities(self):
        result = self.run_checker({"vulnerabilities": {}})
        self.assertEqual(result.returncode, 0, result.stderr)

    def high_report(self, *, modern=False):
        advisory = {"module_name": "package", "severity": "high",
                    "github_advisory_id": "GHSA-test-advisory", "title": "Example advisory"}
        if modern:
            return {"vulnerabilities": {"package": {"severity": "high", "via": [advisory]}}}
        return {"advisories": {"1": advisory}}

    def exception(self, *, expires_on="2999-01-01", severity="high"):
        return (
            "version: 1\nexceptions:\n"
            "  - package: package\n"
            "    advisory: GHSA-test-advisory\n"
            f"    severity: {severity}\n"
            "    mitigation: Bounded use while migrating\n"
            f"    expires_on: {expires_on}\n"
        )

    def test_rejects_high_vulnerability_without_exception(self):
        for modern in (False, True):
            with self.subTest(modern=modern):
                result = self.run_checker(self.high_report(modern=modern))
                self.assertEqual(result.returncode, 1, result.stdout)
                self.assertIn("High/Critical vulnerabilities missing exceptions", result.stderr)

    def test_accepts_matching_valid_high_exception(self):
        for modern in (False, True):
            with self.subTest(modern=modern):
                result = self.run_checker(self.high_report(modern=modern), exceptions=self.exception())
                self.assertEqual(result.returncode, 0, result.stderr)

    def test_rejects_expired_high_exception(self):
        result = self.run_checker(self.high_report(), exceptions=self.exception(expires_on="2000-01-01"))
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn("Exceptions expired", result.stderr)

    def test_rejects_high_exception_severity_mismatch(self):
        result = self.run_checker(self.high_report(), exceptions=self.exception(severity="critical"))
        self.assertEqual(result.returncode, 1, result.stdout)
        self.assertIn("Exception severity mismatch", result.stderr)

    def test_preserves_non_high_vulnerability_behavior(self):
        result = self.run_checker({"advisories": {"1": {
            "module_name": "package", "severity": "moderate", "title": "Example advisory",
        }}})
        self.assertEqual(result.returncode, 0, result.stderr)


if __name__ == "__main__":
    unittest.main()
