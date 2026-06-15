package openai

import "strings"

const (
	DeepSeekV32ModelID    = "rsv-lixirkqxzkpslnqfgmxizjjil-aq"
	QwenRerankerModelID   = "rsv-11m4dmp2"
	BGEM3EmbeddingModelID = "rsv-tchvlgrj"
)

type ModelSpec struct {
	ID            string
	Name          string
	Aliases       []string
	ContextWindow int
	Capabilities  []string
	VectorDim     int
}

var builtinModels = []ModelSpec{
	{
		ID:            DeepSeekV32ModelID,
		Name:          "DeepSeek-V3.2",
		Aliases:       []string{"deepseek", "deepseek-v3.2", "DeepSeek-V3.2"},
		ContextWindow: 65536,
		Capabilities:  []string{"chat"},
	},
	{
		ID:            Qwen36ModelID,
		Name:          Qwen36ModelName,
		Aliases:       []string{"qwen3.6", "qwen3.6-27b", "Qwen3.6-27B"},
		ContextWindow: 262144,
		Capabilities:  []string{"chat", "vision", "tool-shim"},
	},
	{
		ID:            QwenRerankerModelID,
		Name:          "Qwen3-Reranker-0.6B",
		Aliases:       []string{"qwen3-reranker", "qwen3-reranker-0.6b", "Qwen3-Reranker-0.6B"},
		ContextWindow: 0,
		Capabilities:  []string{"rerank", "embedding"},
		VectorDim:     1024,
	},
	{
		ID:            BGEM3EmbeddingModelID,
		Name:          "bge-m3-1024\u7ef4",
		Aliases:       []string{"bge-m3", "bge-m3-1024", "bge-m3-1024\u7ef4"},
		ContextWindow: 0,
		Capabilities:  []string{"embedding"},
		VectorDim:     1024,
	},
}

func BuiltinModels() []ModelSpec {
	out := make([]ModelSpec, len(builtinModels))
	copy(out, builtinModels)
	return out
}

func NormalizeModelName(model string) string {
	m := strings.TrimSpace(model)
	if m == "" {
		return m
	}
	lower := strings.ToLower(m)
	for _, spec := range builtinModels {
		if m == spec.ID || m == spec.Name {
			return spec.ID
		}
		for _, alias := range spec.Aliases {
			if lower == strings.ToLower(strings.TrimSpace(alias)) {
				return spec.ID
			}
		}
	}
	return m
}

func DisplayModelName(model string) string {
	normalized := NormalizeModelName(model)
	for _, spec := range builtinModels {
		if normalized == spec.ID {
			return spec.Name
		}
	}
	return strings.TrimSpace(model)
}
