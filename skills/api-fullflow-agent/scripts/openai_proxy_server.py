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
from fastapi.responses import JSONResponse, StreamingResponse
import uvicorn

from api_txt import load_api_config, load_api_config_from_env
from gateway_client import AgentGatewayClient, AgentGatewayError
from openai_compat import (
    build_chat_completion_response,
    build_responses_response,
    extract_gateway_text_delta,
    extract_gateway_text_from_nonstream,
    iter_chat_completion_sse,
    iter_responses_sse,
    parse_openai_chat_request,
    parse_openai_responses_request,
)


def create_app(client: AgentGatewayClient) -> FastAPI:
    app = FastAPI(title="Agent Gateway OpenAI Proxy", version="0.1.0")

    @app.get("/health")
    def health() -> Dict[str, Any]:
        return {"ok": True}

    @app.post("/v1/chat/completions")
    async def chat_completions(req: Request) -> Any:
        payload = await req.json()
        parsed = parse_openai_chat_request(payload)

        if not parsed.prompt:
            raise HTTPException(status_code=400, detail="messages 为空，无法生成 prompt")

        session_id = client.create_session()
        try:
            if not parsed.stream:
                run_resp = client.run(session_id=session_id, text=parsed.prompt, stream=False)
                text = extract_gateway_text_from_nonstream(run_resp)
                return JSONResponse(build_chat_completion_response(text=text, model=parsed.model))

            # stream=true
            events_iter, _meta = client.run(session_id=session_id, text=parsed.prompt, stream=True)

            def _deltas() -> Iterator[str]:
                for evt in events_iter:
                    d = extract_gateway_text_delta(evt)
                    if d:
                        yield d

            sse_iter = iter_chat_completion_sse(deltas=_deltas(), model=parsed.model)
            return StreamingResponse(
                sse_iter,
                media_type="text/event-stream",
                headers={"Cache-Control": "no-cache"},
            )
        finally:
            # Best-effort cleanup
            try:
                client.delete_session(session_id)
            except Exception:
                pass

    @app.post("/v1/responses")
    async def responses(req: Request) -> Any:
        payload = await req.json()
        parsed = parse_openai_responses_request(payload)

        if not parsed.prompt:
            raise HTTPException(status_code=400, detail="input/messages 为空，无法生成 prompt")

        session_id = client.create_session()
        try:
            if not parsed.stream:
                run_resp = client.run(session_id=session_id, text=parsed.prompt, stream=False)
                text = extract_gateway_text_from_nonstream(run_resp)
                return JSONResponse(build_responses_response(text=text, model=parsed.model))

            events_iter, _meta = client.run(session_id=session_id, text=parsed.prompt, stream=True)

            def _deltas() -> Iterator[str]:
                for evt in events_iter:
                    d = extract_gateway_text_delta(evt)
                    if d:
                        yield d

            sse_iter = iter_responses_sse(deltas=_deltas(), model=parsed.model)
            return StreamingResponse(
                sse_iter,
                media_type="text/event-stream",
                headers={"Cache-Control": "no-cache"},
            )
        finally:
            try:
                client.delete_session(session_id)
            except Exception:
                pass

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
    parser.add_argument("--host", default="0.0.0.0")
    parser.add_argument("--port", default=8000, type=int)
    parser.add_argument("--log-level", default="info", choices=["critical", "error", "warning", "info", "debug"])
    args = parser.parse_args()

    client = _load_client(args.api_txt, args.base_url)
    app = create_app(client)

    uvicorn.run(app, host=args.host, port=args.port, log_level=args.log_level)


if __name__ == "__main__":
    main()

