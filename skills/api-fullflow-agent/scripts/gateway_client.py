#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Agent 网关 API client: createSession -> run -> (feedback) -> deleteSession.

Design goals:
- Minimal deps (requests only)
- Streaming friendly (SSE-like `data:` lines)
- Safe cleanup (deleteSession in finally)
"""

from __future__ import annotations

from dataclasses import dataclass
import json
from typing import Any, Dict, Generator, Iterable, List, Optional, Tuple

import requests

from api_txt import ApiConfig


class AgentGatewayError(RuntimeError):
    def __init__(self, message: str, *, response: Optional[requests.Response] = None):
        super().__init__(message)
        self.response = response


@dataclass(frozen=True)
class RunMeta:
    request_id: Optional[str] = None
    task_id: Optional[str] = None
    unique_code: Optional[str] = None


def _normalize_bearer(app_key: str) -> str:
    v = (app_key or "").strip()
    if not v:
        return v
    if v.lower().startswith("bearer "):
        return v
    return f"Bearer {v}"


class AgentGatewayClient:
    def __init__(
        self,
        config: ApiConfig,
        *,
        timeout_s: float = 120.0,
        stream_read_timeout_s: Optional[float] = None,
        session: Optional[requests.Session] = None,
    ) -> None:
        self._cfg = config
        self._timeout_s = timeout_s
        self._stream_read_timeout_s = stream_read_timeout_s
        self._http = session or requests.Session()

    @property
    def base_url(self) -> str:
        return self._cfg.base_url

    def _url(self, path: str) -> str:
        return f"{self._cfg.base_url.rstrip('/')}/{path.lstrip('/')}"

    def _headers(self, *, accept_stream: bool = False) -> Dict[str, str]:
        h = {
            "Authorization": _normalize_bearer(self._cfg.app_key),
            "Content-Type": "application/json",
        }
        if accept_stream:
            h["Accept"] = "text/event-stream"
            h["Cache-Control"] = "no-cache"
        return h

    def create_session(self) -> str:
        url = self._url("/createSession")
        payload: Dict[str, Any] = {"agentCode": self._cfg.agent_code}
        if self._cfg.agent_version:
            payload["agentVersion"] = self._cfg.agent_version

        r = self._http.post(url, headers=self._headers(), json=payload, timeout=self._timeout_s)
        if r.status_code >= 400:
            raise AgentGatewayError(f"createSession HTTP {r.status_code}: {r.text}", response=r)

        data = _safe_json(r)
        if not data.get("success", False):
            raise AgentGatewayError(f"createSession failed: {data}", response=r)

        session_id = (data.get("data") or {}).get("uniqueCode")
        if not session_id:
            raise AgentGatewayError(f"createSession missing uniqueCode: {data}", response=r)
        return str(session_id)

    def delete_session(self, session_id: str) -> bool:
        url = self._url("/deleteSession")
        payload = {"sessionId": session_id}
        r = self._http.post(url, headers=self._headers(), json=payload, timeout=self._timeout_s)
        if r.status_code >= 400:
            raise AgentGatewayError(f"deleteSession HTTP {r.status_code}: {r.text}", response=r)
        data = _safe_json(r)
        if not data.get("success", False):
            raise AgentGatewayError(f"deleteSession failed: {data}", response=r)
        return bool(data.get("data"))

    def feedback(
        self,
        *,
        session_id: str,
        request_id: str,
        task_id: str,
        vote: str,
        subject: str = "TASK",
        provider_source: str = "USER",
        unique_code: Optional[str] = None,
        comment: Optional[str] = None,
        extend_info: Optional[Dict[str, Any]] = None,
    ) -> Dict[str, Any]:
        url = self._url("/feedback")
        payload: Dict[str, Any] = {
            "sessionId": session_id,
            "requestId": request_id,
            "taskId": task_id,
            "subject": subject,
            "provider": {"source": provider_source, "extendInfo": extend_info or {}},
            "vote": vote,
        }
        if unique_code:
            payload["uniqueCode"] = unique_code
        if comment is not None:
            payload["extCommentsInfo"] = {"comment": comment}

        r = self._http.post(url, headers=self._headers(), json=payload, timeout=self._timeout_s)
        if r.status_code >= 400:
            raise AgentGatewayError(f"feedback HTTP {r.status_code}: {r.text}", response=r)
        data = _safe_json(r)
        if not data.get("success", False):
            raise AgentGatewayError(f"feedback failed: {data}", response=r)
        return data

    def run(
        self,
        *,
        session_id: str,
        text: str,
        stream: bool,
        delta: bool = True,
        trace: bool = False,
        metadata: Optional[Dict[str, Any]] = None,
        attachments: Optional[List[Dict[str, Any]]] = None,
    ) -> Dict[str, Any] | Tuple[Iterable[Dict[str, Any]], RunMeta]:
        """
        When stream=False: returns full JSON response dict.
        When stream=True: returns (events_iter, meta) where events_iter yields parsed event dicts.
        """

        url = self._url("/run")
        payload: Dict[str, Any] = {
            "sessionId": session_id,
            "stream": bool(stream),
            "delta": bool(delta),
        }
        if trace:
            payload["trace"] = True
        payload["message"] = {
            "text": text,
            "metadata": metadata or {},
            "attachments": attachments or [],
        }

        if not stream:
            r = self._http.post(url, headers=self._headers(), json=payload, timeout=self._timeout_s)
            if r.status_code >= 400:
                raise AgentGatewayError(f"run HTTP {r.status_code}: {r.text}", response=r)
            return _safe_json(r)

        stream_timeout = (self._timeout_s, self._stream_read_timeout_s)
        r = self._http.post(
            url,
            headers=self._headers(accept_stream=True),
            json=payload,
            timeout=stream_timeout,
            stream=True,
        )
        if r.status_code >= 400:
            raise AgentGatewayError(f"run(stream) HTTP {r.status_code}: {r.text}", response=r)

        meta = RunMeta()
        # Force UTF-8 for event-stream if server doesn't set it.
        r.encoding = "utf-8"
        events = _iter_stream_events(r, meta)
        return events, meta


def _safe_json(r: requests.Response) -> Dict[str, Any]:
    try:
        return r.json()
    except Exception as e:  # noqa: BLE001 - provide debug context
        raise AgentGatewayError(f"Response is not JSON: {e}; body={r.text[:500]}", response=r)


def _iter_stream_events(
    r: requests.Response,
    meta: RunMeta,
) -> Generator[Dict[str, Any], None, None]:
    """
    Parse streaming response. Common format (SSE-like):
      data: {...json...}
      data: [DONE]
    """

    # NOTE: requests iter_lines handles chunk boundaries for us.
    for raw in r.iter_lines(decode_unicode=False):
        if raw is None:
            continue
        if isinstance(raw, bytes):
            line = raw.decode("utf-8", errors="replace").strip()
        else:
            line = str(raw).strip()
        if not line:
            continue
        if line.startswith("data:"):
            line = line[len("data:") :].strip()
        if not line:
            continue
        if line == "[DONE]":
            break

        evt: Optional[Dict[str, Any]] = None
        try:
            evt = json.loads(line)
        except json.JSONDecodeError:
            # Some gateways might interleave plain text lines; ignore.
            continue

        _try_update_meta(meta, evt)
        yield evt

        if _is_end_event(evt):
            break


def _is_end_event(evt: Dict[str, Any]) -> bool:
    # event itself or nested under "data"
    if bool(evt.get("end")):
        return True
    data = evt.get("data")
    if isinstance(data, dict) and bool(data.get("end")):
        return True
    return False


def _try_update_meta(meta: RunMeta, evt: Dict[str, Any]) -> None:
    """
    Best-effort extraction of requestId/taskId/uniqueCode from stream events.
    The actual keys may vary; this is intentionally defensive.
    """

    def _pick(d: Dict[str, Any], *keys: str) -> Optional[str]:
        for k in keys:
            v = d.get(k)
            if isinstance(v, str) and v.strip():
                return v.strip()
        return None

    # Shallow fields
    request_id = _pick(evt, "requestId", "request_id")
    task_id = _pick(evt, "taskId", "task_id", "uiTaskId", "ui_task_id")
    unique_code = _pick(evt, "uniqueCode", "unique_code")

    # Common nested: metadata.*
    content = evt.get("content")
    if isinstance(content, dict):
        md = content.get("metadata")
        if isinstance(md, dict):
            request_id = request_id or _pick(md, "requestId", "request_id")
            task_id = task_id or _pick(md, "taskId", "task_id", "uiTaskId", "ui_task_id")
            unique_code = unique_code or _pick(md, "uniqueCode", "unique_code")

    if request_id or task_id or unique_code:
        object.__setattr__(meta, "request_id", request_id or meta.request_id)
        object.__setattr__(meta, "task_id", task_id or meta.task_id)
        object.__setattr__(meta, "unique_code", unique_code or meta.unique_code)


def main() -> None:
    import argparse
    from api_txt import load_api_config

    parser = argparse.ArgumentParser(description="Quick smoke: createSession->deleteSession")
    parser.add_argument("--api-txt", default="api.txt")
    args = parser.parse_args()

    cfg = load_api_config(args.api_txt)
    cli = AgentGatewayClient(cfg)
    sid = cli.create_session()
    print("sessionId:", sid)
    ok = cli.delete_session(sid)
    print("deleted:", ok)


if __name__ == "__main__":
    main()
