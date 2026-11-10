import contextlib
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import tempfile
import unittest


class InitializationTests(unittest.TestCase):
    def setUp(self):
        spec = importlib.util.spec_from_file_location("napcat_init", Path(__file__).with_name("init.py"))
        self.module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(self.module)
        # Never fall back to a new C: test directory on this Windows workspace.
        parent = os.environ.get("NAPCAT_TEST_TMP", os.environ.get("TMPDIR", os.environ.get("TEMP")))
        self.tmp = tempfile.TemporaryDirectory(prefix="napcat-init-", dir=parent)
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name) / "napcat"

    def test_generates_matching_private_configuration_without_printing_secrets(self):
        output = io.StringIO()
        with contextlib.redirect_stdout(output):
            self.module.initialize(self.root, "123456789", "https://api.example.test")
        env = dict(line.split("=", 1) for line in (self.root / "sub2api.env").read_text().splitlines())
        keys = ["QQ_INVITE_ENCRYPTION_KEY", "QQ_INVITE_ONEBOT_TOKEN", "QQ_INVITE_WEBUI_TOKEN", "QQ_INVITE_EVENT_SECRET"]
        self.assertEqual(len({env[key] for key in keys}), 4)
        for key in keys:
            self.assertRegex(env[key], r"^[0-9a-f]{64}$")
            self.assertNotIn(env[key], output.getvalue())
        self.assertEqual(env["QQ_INVITE_BOT_QQ"], "123456789")
        self.assertEqual(env["QQ_INVITE_PUBLIC_BASE_URL"], "https://api.example.test")
        self.assertNotIn("QQ_IMAGE_ENCRYPTION_KEY", env)
        web = json.loads((self.root / "config/webui.json").read_text())
        self.assertEqual((web["token"], web["host"], web["port"]), (env["QQ_INVITE_WEBUI_TOKEN"], "0.0.0.0", 6099))
        onebot = json.loads((self.root / "config/onebot11_123456789.json").read_text())
        self.assertEqual(onebot, json.loads((self.root / "config/onebot11.json").read_text()))
        server = onebot["network"]["httpServers"][0]
        client = onebot["network"]["httpClients"][0]
        self.assertEqual((server["host"], server["port"], server["token"]), ("0.0.0.0", 3000, env["QQ_INVITE_ONEBOT_TOKEN"]))
        self.assertEqual(client["url"], "http://sub2api:8080/api/v1/qq-invite/events")
        self.assertEqual(client["token"], env["QQ_INVITE_EVENT_SECRET"])
        self.assertFalse(client["reportSelfMessage"])
        self.assertEqual(onebot["network"]["websocketServers"], [])
        for path in ["napcat.json", "napcat_123456789.json"]:
            log = json.loads((self.root / "config" / path).read_text())
            self.assertFalse(log["consoleLog"])
            self.assertFalse(log["fileLog"])
        if os.name != "nt":
            for file in [self.root / "sub2api.env", *self.root.glob("config/*.json")]:
                self.assertEqual(stat.S_IMODE(file.stat().st_mode), 0o600)

    def test_second_run_never_rotates_existing_keys(self):
        with contextlib.redirect_stdout(io.StringIO()):
            self.module.initialize(self.root, "123456789", "https://api.example.test")
        before = (self.root / "sub2api.env").read_bytes()
        with self.assertRaises(ValueError):
            self.module.initialize(self.root, "987654321", "https://api.example.test")
        self.assertEqual((self.root / "sub2api.env").read_bytes(), before)
        self.assertFalse((self.root / "config/onebot11_987654321.json").exists())

    def test_existing_session_configuration_is_preserved(self):
        self.root.mkdir()
        (self.root / "config").mkdir()
        original = self.root / "config/onebot11_999999.json"
        original.write_text("existing")
        with self.assertRaises(ValueError):
            self.module.initialize(self.root, "123456789", "https://api.example.test")
        self.assertEqual(original.read_text(), "existing")
        self.assertFalse((self.root / "sub2api.env").exists())

    def test_invalid_inputs_make_no_files(self):
        for qq, url in [("0", "https://api.example.test"), ("123\nSECRET=x", "https://api.example.test"), ("00123", "https://api.example.test"), ("123456789", "https://user:secret@api.example.test"), ("123456789", "https://api.example.test/path"), ("123456789", "https://api.example.test?key=secret"), ("123456789", "http://api.example.test"), ("123456789", "https://api.example.test\nSECRET=x")]:
            with self.subTest(qq=qq, url=url):
                with self.assertRaises(ValueError):
                    self.module.initialize(self.root, qq, url)
                self.assertFalse(self.root.exists())


if __name__ == "__main__":
    unittest.main()
