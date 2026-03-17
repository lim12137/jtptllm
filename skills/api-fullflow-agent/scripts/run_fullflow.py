#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
One-shot CLI: createSession -> run -> (optional feedback) -> deleteSession.

Usage:
  python run_fullflow.py --api-txt api.txt --text "你好" --stream
"""

from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any, Dict, Iterator, Optional

from api_txt import load_api_config
from gateway_client import AgentGatewayClient, AgentGatewayError
from openai_compat import extract_gateway_text_delta, extract_gateway_text_from_nonstream


def main() -> None:
    parser = argparse.ArgumentParser(description="Agent 网关全流程调用（createSession/run/deleteSession）")
    parser.add_argument("--api-txt", default="api.txt")
    parser.add_argument("--base-url", default=None)
    parser.add_argument("--text", required=True, help="用户输入")
    parser.add_argument("--stream", action="store_true", help="使用流式输出")
    parser.add_argument("--trace", action="store_true")
    parser.add_argument(
        "--delta",
        action=argparse.BooleanOptionalAction,
        default=True,
        help="stream=true 时是否返回增量文本（默认 true）",
    )

    # optional feedback
    parser.add_argument("--feedback", action="store_true", help="尝试发送 feedback（需要 requestId/taskId）")
    parser.add_argument("--vote", default="LIKE", choices=["LIKE", "DISLIKE"])
    parser.add_argument("--comment", default=None)
    args = parser.parse_args()

    cfg = load_api_config(args.api_txt, base_url=args.base_url)
    cli = AgentGatewayClient(cfg)

    session_id = cli.create_session()
    try:
        if not args.stream:
            run_resp = cli.run(session_id=session_id, text=args.text, stream=False, delta=args.delta, trace=args.trace)
            text = extract_gateway_text_from_nonstream(run_resp)
            print(text)

            if args.feedback:
                _try_feedback(cli, session_id=session_id, run_resp=run_resp, vote=args.vote, comment=args.comment)
            return

        events_iter, meta = cli.run(
            session_id=session_id,
            text=args.text,
            stream=True,
            delta=args.delta,
            trace=args.trace,
        )
        for evt in events_iter:
            d = extract_gateway_text_delta(evt)
            if d:
                print(d, end="", flush=True)
        print("")

        if args.feedback and meta.request_id and meta.task_id:
            cli.feedback(
                session_id=session_id,
                request_id=meta.request_id,
                task_id=meta.task_id,
                vote=args.vote,
                comment=args.comment,
                unique_code=meta.unique_code,
            )
        elif args.feedback:
            print("[WARN] 未能从流式事件提取 requestId/taskId，跳过 feedback。")
    finally:
        try:
            cli.delete_session(session_id)
        except Exception as e:  # noqa: BLE001
            print(f"[WARN] deleteSession 失败: {e}")


def _try_feedback(
    cli: AgentGatewayClient,
    *,
    session_id: str,
    run_resp: Dict[str, Any],
    vote: str,
    comment: Optional[str],
) -> None:
    """
    Non-stream feedback: try to pick requestId/taskId from response.
    """

    data = run_resp.get("data")
    if not isinstance(data, dict):
        print("[WARN] run 非流式响应不包含 data，无法 feedback。")
        return

    # Best-effort: metadata might exist under message.metadata
    msg = data.get("message")
    md: Dict[str, Any] = {}
    if isinstance(msg, dict) and isinstance(msg.get("metadata"), dict):
        md = msg["metadata"]

    request_id = md.get("requestId") or md.get("request_id")
    task_id = md.get("taskId") or md.get("task_id") or md.get("uiTaskId") or md.get("ui_task_id")
    unique_code = md.get("uniqueCode") or md.get("unique_code")

    if not (isinstance(request_id, str) and isinstance(task_id, str)):
        print("[WARN] 未能从非流式响应提取 requestId/taskId，跳过 feedback。")
        return

    cli.feedback(
        session_id=session_id,
        request_id=request_id,
        task_id=task_id,
        vote=vote,
        comment=comment,
        unique_code=unique_code if isinstance(unique_code, str) else None,
    )


if __name__ == "__main__":
    main()
