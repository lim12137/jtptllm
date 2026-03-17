#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Parse `api.txt` (APP_KEY / agentCode / agentVersion) used by the Agent gateway API.

This file is intentionally dependency-light so it can be copied into other projects.
"""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
import os
import re
from typing import Dict, Optional


DEFAULT_BASE_URL = (
    "http://10.54.102.36:80/xlm-gateway--vinrl/sfm-api-gateway/gateway/agent/api"
)


@dataclass(frozen=True)
class ApiConfig:
    app_key: str
    agent_code: str
    agent_version: Optional[str] = None
    base_url: str = DEFAULT_BASE_URL


_LINE_RE = re.compile(r"^\s*([A-Za-z0-9_]+)\s*[:：]\s*(.*?)\s*$")


def parse_api_txt(text: str) -> Dict[str, str]:
    """
    Parse api.txt content into a dict.

    Supports both ':' and '：' separators and allows extra whitespace.
    Ignores empty lines.
    """

    out: Dict[str, str] = {}
    for raw in (text or "").splitlines():
        line = raw.strip()
        if not line:
            continue
        m = _LINE_RE.match(line)
        if not m:
            continue
        k, v = m.group(1).strip(), m.group(2).strip()
        if not k:
            continue
        out[k] = v
    return out


def load_api_config(
    api_txt_path: str | Path,
    *,
    base_url: Optional[str] = None,
    env_prefix: str = "AGENT_",
) -> ApiConfig:
    """
    Load ApiConfig from `api.txt` with optional env overrides.

    Env overrides:
    - {env_prefix}APP_KEY
    - {env_prefix}AGENT_CODE
    - {env_prefix}AGENT_VERSION
    - {env_prefix}BASE_URL
    """

    p = Path(api_txt_path)
    text = p.read_text(encoding="utf-8")
    data = parse_api_txt(text)

    # Normalize common key names.
    app_key = (
        os.getenv(f"{env_prefix}APP_KEY")
        or data.get("APP_KEY")
        or data.get("app_key")
        or data.get("key")
    )
    agent_code = (
        os.getenv(f"{env_prefix}AGENT_CODE")
        or data.get("agentCode")
        or data.get("agent_code")
    )
    agent_version = (
        os.getenv(f"{env_prefix}AGENT_VERSION")
        or data.get("agentVersion")
        or data.get("agent_version")
    )

    final_base_url = (
        base_url
        or os.getenv(f"{env_prefix}BASE_URL")
        or data.get("baseUrl")
        or data.get("base_url")
        or DEFAULT_BASE_URL
    )

    missing = []
    if not app_key:
        missing.append("key(APP_KEY)")
    if not agent_code:
        missing.append("agentCode")
    if missing:
        raise ValueError(
            "api.txt 缺少必要字段: "
            + ", ".join(missing)
            + "。请提供 api.txt，或设置环境变量 "
            + f"{env_prefix}APP_KEY/{env_prefix}AGENT_CODE/{env_prefix}AGENT_VERSION"
        )

    return ApiConfig(
        app_key=str(app_key).strip(),
        agent_code=str(agent_code).strip(),
        agent_version=str(agent_version).strip() if agent_version else None,
        base_url=str(final_base_url).strip(),
    )


def load_api_config_from_env(*, env_prefix: str = "AGENT_") -> ApiConfig:
    app_key = os.getenv(f"{env_prefix}APP_KEY")
    agent_code = os.getenv(f"{env_prefix}AGENT_CODE")
    agent_version = os.getenv(f"{env_prefix}AGENT_VERSION")
    base_url = os.getenv(f"{env_prefix}BASE_URL") or DEFAULT_BASE_URL

    missing = []
    if not app_key:
        missing.append(f"{env_prefix}APP_KEY")
    if not agent_code:
        missing.append(f"{env_prefix}AGENT_CODE")
    if missing:
        raise ValueError("缺少环境变量: " + ", ".join(missing))

    return ApiConfig(
        app_key=app_key.strip(),
        agent_code=agent_code.strip(),
        agent_version=agent_version.strip() if agent_version else None,
        base_url=base_url.strip(),
    )


def main() -> None:
    import argparse
    import json

    parser = argparse.ArgumentParser(description="Parse api.txt and print normalized config.")
    parser.add_argument("--api-txt", default="api.txt")
    args = parser.parse_args()

    cfg = load_api_config(args.api_txt)
    print(json.dumps(cfg.__dict__, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()

