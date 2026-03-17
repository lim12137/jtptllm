from openai_compat import (
    build_chat_completion_response,
    build_responses_response,
    extract_gateway_text_delta,
    extract_gateway_text_from_nonstream,
    iter_chat_completion_sse,
    iter_responses_sse,
    iter_smart_deltas,
    openai_chat_messages_to_prompt,
    openai_responses_input_to_prompt,
)


def test_openai_chat_messages_to_prompt_string_and_parts():
    messages = [
        {"role": "system", "content": "你是助手"},
        {"role": "user", "content": [{"type": "text", "text": "你好"}, {"type": "text", "text": "！"}]},
    ]
    prompt = openai_chat_messages_to_prompt(messages)
    assert "system: 你是助手" in prompt
    assert "user: 你好！" in prompt


def test_openai_responses_input_to_prompt_with_instructions():
    payload = {
        "model": "agent",
        "instructions": "用中文回答",
        "input": [
            {"role": "user", "content": [{"type": "input_text", "text": "你好"}]},
        ],
    }
    prompt = openai_responses_input_to_prompt(payload)
    assert prompt.startswith("system: 用中文回答")
    assert "user: 你好" in prompt


def test_extract_gateway_text_from_nonstream_response():
    run_resp = {
        "success": True,
        "data": {
            "message": {
                "role": "assistant",
                "content": [{"type": "text", "text": {"value": "智能体输出文本"}}],
            }
        },
    }
    assert extract_gateway_text_from_nonstream(run_resp) == "智能体输出文本"


def test_extract_gateway_text_delta_from_stream_event():
    evt = {"object": "message.delta", "content": {"type": "text", "text": {"value": "增量"}}}
    assert extract_gateway_text_delta(evt) == "增量"


def test_iter_chat_completion_sse_ends_with_done():
    lines = list(iter_chat_completion_sse(deltas=["a", "b"], model="agent"))
    assert any("chat.completion.chunk" in ln for ln in lines)
    assert lines[-1].strip() == "data: [DONE]"


def test_iter_responses_sse_ends_with_done():
    lines = list(iter_responses_sse(deltas=["a", "b"], model="agent"))
    assert any("response.output_text.delta" in ln for ln in lines)
    assert lines[-1].strip() == "data: [DONE]"


def test_build_openai_nonstream_responses_have_text():
    a = build_chat_completion_response(text="x", model="agent")
    assert a["choices"][0]["message"]["content"] == "x"
    b = build_responses_response(text="y", model="agent")
    assert b["output_text"] == "y"


def test_iter_smart_deltas_handles_full_snapshots():
    chunks = ["你", "你好", "你好！"]
    assert list(iter_smart_deltas(chunks)) == ["你", "好", "！"]


def test_iter_smart_deltas_passes_true_deltas():
    chunks = ["你", "好", "！"]
    assert list(iter_smart_deltas(chunks)) == ["你", "好", "！"]
