package openai

import (
	"strings"
	"sync"

	"github.com/pkoukk/tiktoken-go"
)

// encodingForModel 返回给定模型对应的 tokenizer 编码器。
//
// 选用规则：
//   - DeepSeek / Qwen 等基于 BPE 的中英文模型，统一回退到 cl100k_base（OpenAI 通用编码），
//     该编码对中英文混合文本估算误差可接受，且无需为每个上游模型单独维护词表。
//   - 若 tiktoken 加载失败（如离线环境缺少词表缓存），返回 nil，调用方需做 nil 兜底。
func encodingForModel(model string) *tiktoken.Tiktoken {
	tke, err := tiktoken.EncodingForModel(normalizeForTokenizer(model))
	if err == nil && tke != nil {
		return tke
	}
	// 回退到通用编码
	tke, err = tiktoken.GetEncoding(tiktoken.MODEL_CL100K_BASE)
	if err != nil {
		return nil
	}
	return tke
}

// normalizeForTokenizer 把内置模型别名归一化为 tiktoken-go 能识别的常见名称族，
// 例如 Qwen / DeepSeek 都按 gpt 系列处理，最终都会落到 cl100k_base 回退。
func normalizeForTokenizer(model string) string {
	m := strings.ToLower(strings.TrimSpace(model))
	switch {
	case strings.Contains(m, "deepseek"), strings.Contains(m, "qwen"):
		return "gpt-3.5-turbo"
	default:
		return m
	}
}

// tokenizerCache 缓存按 model 解析出的 encoder，避免重复加载词表。
var (
	tokenizerOnce sync.Map // map[string]*tiktoken.Tiktoken
)

func cachedEncoder(model string) *tiktoken.Tiktoken {
	key := normalizeForTokenizer(model)
	if v, ok := tokenizerOnce.Load(key); ok {
		return v.(*tiktoken.Tiktoken)
	}
	enc := encodingForModel(model)
	if enc != nil {
		tokenizerOnce.Store(key, enc)
	}
	return enc
}

// CountTokensText 估算单段文本的 token 数。
// model 仅用于选择编码器；为空或无法识别时回退到 cl100k_base。
// 当编码器不可用时（例如离线缺词表），退化为按 UTF-8 字符数的保守估算：
// 英文约 4 char/token，中文按 1.5 char/token 粗略折算。
func CountTokensText(text, model string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	if enc := cachedEncoder(model); enc != nil {
		return len(enc.Encode(text, nil, nil))
	}
	return estimateFallback(text)
}

// CountTokensPrompt 估算 prompt（拼好的 "role: content" 多行文本）的 token 数。
// 直接复用 CountTokensText，保持与非流式 prompt 文本一致。
func CountTokensPrompt(prompt, model string) int {
	return CountTokensText(prompt, model)
}

// Usage 是 OpenAI 风格的 usage 字段结构。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// BuildUsage 估算 prompt 与 completion 文本的 token 数并返回 usage map。
func BuildUsage(prompt, completion, model string) map[string]any {
	pt := CountTokensPrompt(prompt, model)
	ct := CountTokensText(completion, model)
	return map[string]any{
		"prompt_tokens":     pt,
		"completion_tokens": ct,
		"total_tokens":      pt + ct,
	}
}

// estimateFallback 在编码器不可用时给出保守估算，避免 usage 恒为 0。
func estimateFallback(text string) int {
	var ascii, non int
	for _, r := range text {
		if r < 0x80 {
			ascii++
		} else {
			non++
		}
	}
	// 英文 ~4 char/token，中文 ~1.5 char/token，向下取整并保证非空文本至少 1。
	n := ascii/4 + int(float64(non)/1.5)
	if n == 0 && text != "" {
		n = 1
	}
	return n
}
