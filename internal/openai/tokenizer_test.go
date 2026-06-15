package openai

import "testing"

// TestCountTokensEmpty 验证空文本返回 0。
func TestCountTokensEmpty(t *testing.T) {
	if got := CountTokensText("", "qwen3.6"); got != 0 {
		t.Fatalf("expected 0 tokens for empty text, got %d", got)
	}
}

// TestCountTokensEnglish 验证英文短句能得到合理 token 数。
func TestCountTokensEnglish(t *testing.T) {
	got := CountTokensText("hello world from the proxy", "qwen3.6")
	if got <= 0 {
		t.Fatalf("expected positive token count for english, got %d", got)
	}
}

// TestCountTokensChinese 验证中文文本也能估算出 token。
func TestCountTokensChinese(t *testing.T) {
	got := CountTokensText("你好，这是一个用于测试的中文句子。", "deepseek")
	if got <= 0 {
		t.Fatalf("expected positive token count for chinese, got %d", got)
	}
}

// TestCountTokensMixed 验证中英文混合文本能估算。
func TestCountTokensMixed(t *testing.T) {
	got := CountTokensText("Hello 你好，this is a mixed 中英文 sentence.", "qwen3.6")
	if got <= 0 {
		t.Fatalf("expected positive token count for mixed text, got %d", got)
	}
}

// TestCountTokensPromptEqualsText 验证 prompt 统计与等价文本一致。
func TestCountTokensPromptEqualsText(t *testing.T) {
	s := "user: 你好\nassistant: 你好，有什么可以帮你？"
	if CountTokensPrompt(s, "qwen3.6") != CountTokensText(s, "qwen3.6") {
		t.Fatal("CountTokensPrompt should equal CountTokensText for same input")
	}
}

// TestBuildUsageFields 验证 usage 各字段合理且 total = prompt + completion。
func TestBuildUsageFields(t *testing.T) {
	usage := BuildUsage("user: 你好", "你好，我是助手。", "qwen3.6")
	pt, _ := usage["prompt_tokens"].(int)
	ct, _ := usage["completion_tokens"].(int)
	total, _ := usage["total_tokens"].(int)
	if pt <= 0 || ct <= 0 {
		t.Fatalf("expected positive prompt/completion tokens, got prompt=%d completion=%d", pt, ct)
	}
	if total != pt+ct {
		t.Fatalf("expected total = prompt + completion, got %d != %d + %d", total, pt, ct)
	}
}

// TestBuildChatCompletionResponseWithUsage 验证构造的响应包含 usage。
func TestBuildChatCompletionResponseWithUsage(t *testing.T) {
	usage := BuildUsage("user: hi", "hello there", "qwen3.6")
	resp := BuildChatCompletionResponseWithUsage("hello there", "qwen3.6", usage)
	u, ok := resp["usage"].(map[string]any)
	if !ok {
		t.Fatal("response missing usage")
	}
	total, _ := u["total_tokens"].(int)
	if total <= 0 {
		t.Fatalf("expected positive total_tokens in response, got %d", total)
	}
}

// TestBuildChatCompletionResponseFallbackUsage 验证 usage 为 nil 时仍带默认 usage 字段。
func TestBuildChatCompletionResponseFallbackUsage(t *testing.T) {
	resp := BuildChatCompletionResponse("hi", "qwen3.6")
	u, ok := resp["usage"].(map[string]any)
	if !ok {
		t.Fatal("response missing usage")
	}
	for _, k := range []string{"prompt_tokens", "completion_tokens", "total_tokens"} {
		if _, ok := u[k].(int); !ok {
			t.Fatalf("usage missing %s", k)
		}
	}
}
