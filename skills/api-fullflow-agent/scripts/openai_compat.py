#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
OpenAI 旧/新格式兼容层：
- /v1/chat/completions (legacy)
- /v1/responses (new)

这里只做“格式适配”，不直接做网络请求。
"""

from __future__ import annotations

from dataclasses import dataclass
import time
import uuid
from typing import Any, Dict, Iterable, List, Optional, Tuple


def _now_ts() -> int:
    return int(time.time())


def new_id(prefix: str) -> str:
    return f"{prefix}_{uuid.uuid4().hex}"


def coerce_content_to_str(content: Any) -> str:
    """
    Convert OpenAI-style message content into plain text.

    Supports:
    - string
    - list of parts: [{"type":"text","text":"..."}, {"type":"input_text","text":"..."}]
    """

    if content is None:
        return ""
    if isinstance(content, str):
        return content
    if isinstance(content, (int, float, bool)):
        return str(content)
    if isinstance(content, list):
        chunks: List[str] = []
        for part in content:
            if isinstance(part, str):
                chunks.append(part)
                continue
            if not isinstance(part, dict):
                continue
            ptype = part.get("type")
            if ptype in ("text", "input_text", "output_text"):
                # Most common: {"type":"text","text":"..."}
                t = part.get("text")
                if isinstance(t, str):
                    chunks.append(t)
                continue
            # Some SDK variants: {"type":"text","text":{"value":"..."}} (rare)
            t = part.get("text")
            if isinstance(t, dict) and isinstance(t.get("value"), str):
                chunks.append(t["value"])
        return "".join(chunks)
    if isinstance(content, dict):
        # Best-effort
        if isinstance(content.get("text"), str):
            return content["text"]
        if isinstance(content.get("value"), str):
            return content["value"]
    return ""


def openai_chat_messages_to_prompt(messages: Any) -> str:
    """
    Convert OpenAI legacy chat messages into a single prompt string.

    We keep it intentionally simple (role: content per line) because the upstream
    gateway only accepts one `message.text` per call.
    """

    if not isinstance(messages, list):
        return ""

    lines: List[str] = []
    for m in messages:
        if not isinstance(m, dict):
            continue
        role = str(m.get("role") or "user").strip()
        content = coerce_content_to_str(m.get("content"))
        if not content.strip():
            continue
        lines.append(f"{role}: {content}".rstrip())
    return "\n".join(lines).strip()


def openai_responses_input_to_prompt(payload: Dict[str, Any]) -> str:
    """
    Convert OpenAI Responses API payload into a prompt string.

    Supported inputs:
    - payload["input"] as string
    - payload["input"] as list[str|dict]
    - payload["messages"] (fallback)
    - payload["instructions"] (optional) prefixed as system
    """

    instructions = payload.get("instructions")

    if "messages" in payload and "input" not in payload:
        prompt = openai_chat_messages_to_prompt(payload.get("messages"))
        if instructions and instructions.strip():
            return f"system: {instructions.strip()}\n{prompt}".strip()
        return prompt

    inp = payload.get("input")
    prompt = ""
    if isinstance(inp, str):
        prompt = inp
    elif isinstance(inp, list):
        lines: List[str] = []
        for item in inp:
            if isinstance(item, str):
                if item.strip():
                    lines.append(f"user: {item}")
                continue
            if not isinstance(item, dict):
                continue
            role = str(item.get("role") or "user").strip()
            content = coerce_content_to_str(item.get("content"))
            if not content.strip():
                continue
            lines.append(f"{role}: {content}".rstrip())
        prompt = "\n".join(lines).strip()
    elif inp is None:
        prompt = ""
    else:
        prompt = str(inp)

    if instructions and isinstance(instructions, str) and instructions.strip():
        if prompt:
            return f"system: {instructions.strip()}\n{prompt}".strip()
        return f"system: {instructions.strip()}".strip()
    return prompt.strip()


def extract_gateway_text_from_nonstream(run_resp: Dict[str, Any]) -> str:
    """
    Extract assistant text from gateway non-stream response.
    The gateway formats may vary; this is best-effort.
    """

    if not isinstance(run_resp, dict):
        return ""

    # Common shape: {"success":true,"data":{"message":{...}}}
    data = run_resp.get("data")
    if isinstance(data, dict):
        msg = data.get("message")
        if isinstance(msg, dict):
            return _extract_text_from_message(msg)

        # Sometimes directly returns message fields
        return _extract_text_from_message(data)

    # Fallback: maybe already message-like
    return _extract_text_from_message(run_resp)


def _extract_text_from_message(msg: Dict[str, Any]) -> str:
    content = msg.get("content")

    # content could be list of parts
    if isinstance(content, list):
        parts: List[str] = []
        for c in content:
            if not isinstance(c, dict):
                continue
            parts.append(_extract_text_from_content_obj(c))
        return "".join([p for p in parts if p])

    if isinstance(content, dict):
        return _extract_text_from_content_obj(content)

    # Some variants: {"content":{"type":"text","text":{"value":"..."}}}
    if isinstance(msg.get("text"), str):
        return msg["text"]

    return ""


def extract_gateway_text_delta(evt: Dict[str, Any]) -> Optional[str]:
    """
    Extract assistant text delta from gateway stream event.
    """

    if not isinstance(evt, dict):
        return None

    # Sometimes nested under {"data": {...}}
    if isinstance(evt.get("data"), dict) and "content" in evt["data"]:
        evt = evt["data"]

    content = evt.get("content")
    if isinstance(content, list):
        # If gateway emits list in a stream event, treat it as full snapshot.
        parts: List[str] = []
        for c in content:
            if isinstance(c, dict):
                parts.append(_extract_text_from_content_obj(c))
        s = "".join([p for p in parts if p]).strip()
        return s or None

    if isinstance(content, dict):
        s = _extract_text_from_content_obj(content)
        s = s.strip()
        return s or None

    return None


def is_gateway_end_event(evt: Dict[str, Any]) -> bool:
    if not isinstance(evt, dict):
        return False
    if bool(evt.get("end")):
        return True
    data = evt.get("data")
    return isinstance(data, dict) and bool(data.get("end"))


def _extract_text_from_content_obj(content: Dict[str, Any]) -> str:
    ctype = content.get("type")
    if ctype != "text":
        return ""
    text = content.get("text")
    if isinstance(text, dict) and isinstance(text.get("value"), str):
        return text["value"]
    if isinstance(text, str):
        return text
    return ""


def build_chat_completion_response(*, text: str, model: str) -> Dict[str, Any]:
    """
    Minimal OpenAI-compatible response for /v1/chat/completions (non-stream).
    """

    created = _now_ts()
    cid = new_id("chatcmpl")
    return {
        "id": cid,
        "object": "chat.completion",
        "created": created,
        "model": model,
        "choices": [
            {
                "index": 0,
                "message": {"role": "assistant", "content": text},
                "finish_reason": "stop",
            }
        ],
        "usage": {"prompt_tokens": 0, "completion_tokens": 0, "total_tokens": 0},
    }


def iter_chat_completion_sse(
    *,
    deltas: Iterable[str],
    model: str,
    chatcmpl_id: Optional[str] = None,
) -> Iterable[str]:
    """
    Yield SSE lines for /v1/chat/completions stream=true.
    """

    created = _now_ts()
    cid = chatcmpl_id or new_id("chatcmpl")

    # First chunk: include role.
    first = {
        "id": cid,
        "object": "chat.completion.chunk",
        "created": created,
        "model": model,
        "choices": [{"index": 0, "delta": {"role": "assistant"}, "finish_reason": None}],
    }
    yield _sse_data(first)

    for d in deltas:
        if not d:
            continue
        chunk = {
            "id": cid,
            "object": "chat.completion.chunk",
            "created": created,
            "model": model,
            "choices": [{"index": 0, "delta": {"content": d}, "finish_reason": None}],
        }
        yield _sse_data(chunk)

    final = {
        "id": cid,
        "object": "chat.completion.chunk",
        "created": created,
        "model": model,
        "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}],
    }
    yield _sse_data(final)
    yield "data: [DONE]\n\n"


def build_responses_response(*, text: str, model: str) -> Dict[str, Any]:
    """
    Minimal OpenAI-compatible response for /v1/responses (non-stream).
    """

    rid = new_id("resp")
    created = _now_ts()
    return {
        "id": rid,
        "object": "response",
        "created_at": created,
        "model": model,
        "output": [
            {
                "type": "message",
                "role": "assistant",
                "content": [{"type": "output_text", "text": text}],
            }
        ],
        # Convenience (OpenAI often provides this).
        "output_text": text,
    }


def iter_responses_sse(*, deltas: Iterable[str], model: str, resp_id: Optional[str] = None) -> Iterable[str]:
    """
    Minimal streaming for /v1/responses stream=true.
    We stream output_text deltas + a completion marker.
    """

    rid = resp_id or new_id("resp")
    created = _now_ts()

    created_evt = {"type": "response.created", "response": {"id": rid, "model": model, "created_at": created}}
    yield _sse_data(created_evt)

    for d in deltas:
        if not d:
            continue
        yield _sse_data({"type": "response.output_text.delta", "delta": d, "response_id": rid})

    yield _sse_data({"type": "response.completed", "response_id": rid})
    yield "data: [DONE]\n\n"


def _sse_data(obj: Dict[str, Any]) -> str:
    import json

    return f"data: {json.dumps(obj, ensure_ascii=False)}\n\n"


def iter_smart_deltas(chunks: Iterable[str]) -> Iterable[str]:
    """
    Convert mixed stream chunks into clean deltas.

    If upstream sends full snapshots (prefix of previous output),
    emit only the suffix. If upstream sends true deltas, emit as-is.
    """

    full = ""
    for chunk in chunks:
        if not chunk:
            continue
        if full and chunk.startswith(full):
            delta = chunk[len(full) :]
            full = chunk
        else:
            delta = chunk
            full = full + chunk
        if delta:
            yield delta


@dataclass(frozen=True)
class ParsedOpenAIRequest:
    model: str
    prompt: str
    stream: bool


def parse_openai_chat_request(payload: Dict[str, Any]) -> ParsedOpenAIRequest:
    model = str(payload.get("model") or "agent")
    stream = bool(payload.get("stream", False))
    prompt = openai_chat_messages_to_prompt(payload.get("messages"))
    return ParsedOpenAIRequest(model=model, prompt=prompt, stream=stream)


def parse_openai_responses_request(payload: Dict[str, Any]) -> ParsedOpenAIRequest:
    model = str(payload.get("model") or "agent")
    stream = bool(payload.get("stream", False))
    prompt = openai_responses_input_to_prompt(payload)
    return ParsedOpenAIRequest(model=model, prompt=prompt, stream=stream)
