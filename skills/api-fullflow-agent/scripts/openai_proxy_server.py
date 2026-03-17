#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
OpenAI 兼容代理服务：
- POST /v1/chat/completions (旧)
- POST /v1/responses (新)

把 OpenAI 请求转换为 Agent 网关的 run 调用，内部流程：createSession -> run -> deleteSession。

注意：
- 这是“协议适配层”，不实现 OpenAI 的完整全部字段，只覆盖常用文本对话场景。
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any, Dict, Iterable, Iterator, Optional

from fastapi import FastAPI, HTTPException, Request
from fastapi.middleware.cors import CORSMiddleware
from fastapi.responses import JSONResponse, StreamingResponse
import uvicorn

from api_txt import load_api_config, load_api_config_from_env
from gateway_client import AgentGatewayClient, AgentGatewayError
from openai_compat import (
    build_chat_completion_response,
    build_responses_response,
    extract_gateway_text_delta,
    extract_gateway_text_from_nonstream,
    iter_smart_deltas,
    iter_chat_completion_sse,
    iter_responses_sse,
    parse_openai_chat_request,
    parse_openai_responses_request,
)

import threading
import time
from typing import Any, Dict, Iterator, Optional, Tuple


class SessionManager:
    """
    Reuse gateway sessionId per client key with idle TTL.

    Key priority:
    - Header: x-agent-session
    - Header: x-client-id
    - Fallback: client IP
    """

    def __init__(self, client: AgentGatewayClient, ttl_s: int = 600) -> None:
        self._client = client
        self._ttl_s = max(0, int(ttl_s))
        self._lock = threading.Lock()
        # key -> {"session_id": str, "last": float}
        self._sessions: Dict[str, Dict[str, Any]] = {}

    def _now(self) -> float:
        return time.monotonic()

    def _expired(self, last: float, now: float) -> bool:
        return self._ttl_s > 0 and (now - last) > self._ttl_s

    def _cleanup_locked(self, now: float) -> None:
        for key, entry in list(self._sessions.items()):
            if self._expired(entry["last"], now):
                self._drop_locked(key, entry["session_id"])

    def _drop_locked(self, key: str, session_id: str) -> None:
        self._sessions.pop(key, None)
        try:
            self._client.delete_session(session_id)
        except Exception:
            pass

    def get_or_create(self, key: str, *, force_new: bool = False) -> str:
        now = self._now()
        with self._lock:
            self._cleanup_locked(now)
            if not force_new and self._ttl_s > 0:
                entry = self._sessions.get(key)
                if entry:
                    entry["last"] = now
                    return entry["session_id"]

        # create outside lock (network call)
        session_id = self._client.create_session()
        with self._lock:
            self._sessions[key] = {"session_id": session_id, "last": now}
        return session_id

    def invalidate(self, key: str) -> None:
        with self._lock:
            entry = self._sessions.get(key)
            if not entry:
                return
            self._drop_locked(key, entry["session_id"])

    def touch(self, key: str) -> None:
        now = self._now()
        with self._lock:
            entry = self._sessions.get(key)
            if entry:
                entry["last"] = now


def _session_key(req: Request) -> str:
    hdr = req.headers.get("x-agent-session")
    if hdr:
        return f"hdr:{hdr}"
    cid = req.headers.get("x-client-id")
    if cid:
        return f"cid:{cid}"
    host = req.client.host if req.client else "unknown"
    return f"ip:{host}"


def create_app(
    client: AgentGatewayClient,
    *,
    default_model: str = "agent",
    session_ttl_s: int = 600,
) -> FastAPI:
    app = FastAPI(title="Agent Gateway OpenAI Proxy", version="0.1.0")
    # Allow browser direct calls by default (can be restricted later).
    app.add_middleware(
        CORSMiddleware,
        allow_origins=["*"],
        allow_credentials=True,
        allow_methods=["*"],
        allow_headers=["*"],
    )
    session_mgr = SessionManager(client, ttl_s=session_ttl_s) if session_ttl_s > 0 else None

    @app.get("/health")
    def health() -> Dict[str, Any]:
        return {"ok": True}

    @app.get("/model")
    def model_alias() -> Dict[str, Any]:
        return {"model": default_model}

    @app.get("/v1/models")
    def models() -> Dict[str, Any]:
        return {"object": "list", "data": [{"id": default_model, "object": "model"}]}

    @app.post("/v1/chat/completions")
    async def chat_completions(req: Request) -> Any:
        payload = await req.json()
        parsed = parse_openai_chat_request(payload)

        if not parsed.prompt:
            raise HTTPException(status_code=400, detail="messages 为空，无法生成 prompt")

        session_key = _session_key(req)
        force_new = req.headers.get("x-agent-session-reset", "").lower() in ("1", "true", "yes")
        close_after = req.headers.get("x-agent-session-close", "").lower() in ("1", "true", "yes")

        if session_mgr:
            session_id = session_mgr.get_or_create(session_key, force_new=force_new)
            delete_on_finish = False
        else:
            session_id = client.create_session()
            delete_on_finish = True
        try:
            if not parsed.stream:
                run_resp = client.run(session_id=session_id, text=parsed.prompt, stream=False)
                text = extract_gateway_text_from_nonstream(run_resp)
                return JSONResponse(build_chat_completion_response(text=text, model=parsed.model))

            # stream=true
            events_iter, _meta = client.run(session_id=session_id, text=parsed.prompt, stream=True)

            def _deltas() -> Iterator[str]:
                def _raw():
                    for evt in events_iter:
                        d = extract_gateway_text_delta(evt)
                        if d:
                            yield d

                try:
                    for d in iter_smart_deltas(_raw()):
                        yield d
                except Exception:
                    if session_mgr:
                        session_mgr.invalidate(session_key)
                    raise

            sse_iter = iter_chat_completion_sse(deltas=_deltas(), model=parsed.model)
            return StreamingResponse(
                sse_iter,
                media_type="text/event-stream",
                headers={"Cache-Control": "no-cache"},
            )
        finally:
            if delete_on_finish:
                try:
                    client.delete_session(session_id)
                except Exception:
                    pass
            elif close_after and session_mgr:
                session_mgr.invalidate(session_key)

    @app.post("/v1/responses")
    async def responses(req: Request) -> Any:
        payload = await req.json()
        parsed = parse_openai_responses_request(payload)

        if not parsed.prompt:
            raise HTTPException(status_code=400, detail="input/messages 为空，无法生成 prompt")

        session_key = _session_key(req)
        force_new = req.headers.get("x-agent-session-reset", "").lower() in ("1", "true", "yes")
        close_after = req.headers.get("x-agent-session-close", "").lower() in ("1", "true", "yes")

        if session_mgr:
            session_id = session_mgr.get_or_create(session_key, force_new=force_new)
            delete_on_finish = False
        else:
            session_id = client.create_session()
            delete_on_finish = True
        try:
            if not parsed.stream:
                run_resp = client.run(session_id=session_id, text=parsed.prompt, stream=False)
                text = extract_gateway_text_from_nonstream(run_resp)
                return JSONResponse(build_responses_response(text=text, model=parsed.model))

            events_iter, _meta = client.run(session_id=session_id, text=parsed.prompt, stream=True)

            def _deltas() -> Iterator[str]:
                def _raw():
                    for evt in events_iter:
                        d = extract_gateway_text_delta(evt)
                        if d:
                            yield d

                try:
                    for d in iter_smart_deltas(_raw()):
                        yield d
                except Exception:
                    if session_mgr:
                        session_mgr.invalidate(session_key)
                    raise

            sse_iter = iter_responses_sse(deltas=_deltas(), model=parsed.model)
            return StreamingResponse(
                sse_iter,
                media_type="text/event-stream",
                headers={"Cache-Control": "no-cache"},
            )
        finally:
            if delete_on_finish:
                try:
                    client.delete_session(session_id)
                except Exception:
                    pass
            elif close_after and session_mgr:
                session_mgr.invalidate(session_key)

    @app.exception_handler(AgentGatewayError)
    async def _handle_gateway_error(_req: Request, exc: AgentGatewayError) -> JSONResponse:
        # Keep a stable JSON envelope for debuggability.
        body = {"error": {"type": "agent_gateway_error", "message": str(exc)}}
        return JSONResponse(status_code=502, content=body)

    return app


def _load_client(api_txt: Optional[str], base_url: Optional[str]) -> AgentGatewayClient:
    if api_txt and Path(api_txt).exists():
        cfg = load_api_config(api_txt, base_url=base_url)
    else:
        # fallback to env
        cfg = load_api_config_from_env()
        if base_url:
            cfg = cfg.__class__(**{**cfg.__dict__, "base_url": base_url})
    return AgentGatewayClient(cfg)


def main() -> None:
    parser = argparse.ArgumentParser(description="OpenAI compatible proxy for Agent gateway API")
    parser.add_argument("--api-txt", default="api.txt", help="Path to api.txt (key/agentCode/agentVersion)")
    parser.add_argument("--base-url", default=None, help="Override gateway base url")
    parser.add_argument("--default-model", default="agent", help="Default model name for /model and /v1/models")
    parser.add_argument("--session-ttl", default=600, type=int, help="Session idle TTL seconds (0=disable reuse)")
    parser.add_argument("--host", default="0.0.0.0")
    parser.add_argument("--port", default=8000, type=int)
    parser.add_argument("--log-level", default="info", choices=["critical", "error", "warning", "info", "debug"])
    args = parser.parse_args()

    client = _load_client(args.api_txt, args.base_url)
    app = create_app(client, default_model=args.default_model, session_ttl_s=args.session_ttl)

    uvicorn.run(app, host=args.host, port=args.port, log_level=args.log_level)


if __name__ == "__main__":
    main()
