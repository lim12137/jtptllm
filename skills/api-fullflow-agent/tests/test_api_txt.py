from pathlib import Path

import pytest

from api_txt import DEFAULT_BASE_URL, load_api_config, parse_api_txt


def test_parse_api_txt_supports_fullwidth_colon_and_spaces():
    text = """
    key  ：  abc
    agentCode  ：  code-123
    agentVersion：  1773710606282
    """
    d = parse_api_txt(text)
    assert d["key"] == "abc"
    assert d["agentCode"] == "code-123"
    assert d["agentVersion"] == "1773710606282"


def test_load_api_config_reads_required_fields(tmp_path: Path):
    p = tmp_path / "api.txt"
    p.write_text("key: k\nagentCode: c\nagentVersion: v\n", encoding="utf-8")
    cfg = load_api_config(p)
    assert cfg.app_key == "k"
    assert cfg.agent_code == "c"
    assert cfg.agent_version == "v"
    assert cfg.base_url == DEFAULT_BASE_URL


def test_load_api_config_missing_fields_raises(tmp_path: Path):
    p = tmp_path / "api.txt"
    p.write_text("agentCode: c\n", encoding="utf-8")
    with pytest.raises(ValueError):
        load_api_config(p)

