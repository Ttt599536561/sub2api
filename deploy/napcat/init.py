#!/usr/bin/env python3
"""Initialize a new private NapCat deployment without printing credentials."""

import argparse
import json
import os
from pathlib import Path
import re
import secrets
import sys
from urllib.parse import urlsplit


def initialize(root: Path, qq: str, public_url: str) -> None:
    if not re.fullmatch(r"[1-9][0-9]{4,19}", qq) or int(qq) > 2**63 - 1:
        raise ValueError("QQ number must be a canonical positive numeric account")
    if any(ord(char) < 33 for char in public_url):
        raise ValueError("Public URL must be an HTTPS origin")
    try:
        origin = urlsplit(public_url)
        port = origin.port
    except ValueError:
        raise ValueError("Public URL must be an HTTPS origin") from None
    if (origin.scheme != "https" or not origin.hostname or origin.username is not None
            or origin.password is not None or origin.path not in ("", "/")
            or origin.query or origin.fragment or "?" in public_url or "#" in public_url
            or any(char in public_url for char in "\\$'\"`=") or (port is not None and not 1 <= port <= 65535)):
        raise ValueError("Public URL must be an HTTPS origin")
    public_url = public_url.rstrip("/")
    root = Path(root).absolute()
    if root.is_symlink():
        raise ValueError("Output directory must not be a symbolic link")
    config_dir = root / "config"
    env_path = root / "sub2api.env"
    for directory in (config_dir, root / "qq", root / "plugins"):
        if directory.is_symlink() or (directory.exists() and (not directory.is_dir() or any(directory.iterdir()))):
            raise ValueError("Existing NapCat data/configuration found; no files changed")
    if env_path.exists() or env_path.is_symlink():
        raise ValueError("Existing backend credentials found; no files changed")

    values = {
        "QQ_INVITE_ENCRYPTION_KEY": secrets.token_hex(32),
        "QQ_INVITE_ALLOWED_HOSTS": "napcat",
        "QQ_INVITE_ONEBOT_URL": "http://napcat:3000",
        "QQ_INVITE_WEBUI_URL": "http://napcat:6099",
        "QQ_INVITE_ONEBOT_TOKEN": secrets.token_hex(32),
        "QQ_INVITE_WEBUI_TOKEN": secrets.token_hex(32),
        "QQ_INVITE_EVENT_SECRET": secrets.token_hex(32),
        "QQ_INVITE_BOT_QQ": qq,
        "QQ_INVITE_PUBLIC_BASE_URL": public_url,
    }
    network = {
        "httpServers": [{"name": "sub2api-api", "enable": True, "host": "0.0.0.0", "port": 3000,
                         "enableCors": False, "enableWebsocket": False, "messagePostFormat": "array",
                         "token": values["QQ_INVITE_ONEBOT_TOKEN"], "debug": False}],
        "httpSseServers": [],
        "httpClients": [{"name": "sub2api-events", "enable": True,
                         "url": "http://sub2api:8080/api/v1/qq-invite/events", "messagePostFormat": "array",
                         "reportSelfMessage": False, "token": values["QQ_INVITE_EVENT_SECRET"], "debug": False}],
        "websocketServers": [], "websocketClients": [], "plugins": [],
    }
    onebot = {"network": network, "musicSignUrl": "", "enableLocalFile2Url": False,
              "parseMultMsg": False, "imageDownloadProxy": "",
              "timeout": {"baseTimeout": 10000, "uploadSpeedKBps": 256, "downloadSpeedKBps": 256, "maxTimeout": 1800000}}
    webui = {"host": "0.0.0.0", "port": 6099, "token": values["QQ_INVITE_WEBUI_TOKEN"],
             "loginRate": 20, "enable2FA": False}
    quiet = {"fileLog": False, "consoleLog": False, "fileLogLevel": "error", "consoleLogLevel": "error",
             "packetBackend": "auto", "packetServer": "", "o3HookMode": 0, "autoTimeSync": True}
    files = {
        env_path: "".join(f"{key}={value}\n" for key, value in values.items()),
        config_dir / "webui.json": json.dumps(webui, indent=2) + "\n",
        config_dir / "onebot11.json": json.dumps(onebot, indent=2) + "\n",
        config_dir / f"onebot11_{qq}.json": json.dumps(onebot, indent=2) + "\n",
        config_dir / "napcat.json": json.dumps(quiet, indent=2) + "\n",
        config_dir / f"napcat_{qq}.json": json.dumps(quiet, indent=2) + "\n",
    }
    # Restrictive umask also protects intermediate parent directories. O_EXCL
    # refuses concurrent initializers instead of rotating a partially used key.
    previous_umask = os.umask(0o077)
    try:
        for directory in (root, config_dir, root / "qq", root / "plugins"):
            directory.mkdir(mode=0o700, parents=True, exist_ok=True)
            directory.chmod(0o700)
        for path, content in files.items():
            fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
            with os.fdopen(fd, "w", encoding="utf-8", newline="\n") as stream:
                stream.write(content)
                stream.flush()
                os.fsync(stream.fileno())
    finally:
        os.umask(previous_umask)
    print(f"Created private backend environment: {env_path}")
    print(f"Created NapCat configuration directory: {config_dir}")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--qq", required=True, help="Expected QQ account, not a group number")
    parser.add_argument("--public-url", required=True, help="Actual HTTPS Sub2 origin")
    parser.add_argument("--output-dir", type=Path, default=Path(__file__).resolve().parent)
    args = parser.parse_args()
    try:
        initialize(args.output_dir, args.qq, args.public_url)
    except (ValueError, OSError):
        # Avoid reflecting sensitive command input or OS file contents.
        print("Initialization refused: invalid input, existing data, or insufficient file permissions. No existing file was overwritten.", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
