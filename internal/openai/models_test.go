package openai

import "testing"

func TestNormalizeModelName(t *testing.T) {
	cases := map[string]string{
		"deepseek":              DeepSeekV32ModelID,
		"DeepSeek-V3.2":         DeepSeekV32ModelID,
		"qwen3.6":               Qwen36ModelID,
		"Qwen3.6-27B":           Qwen36ModelID,
		"qwen3-reranker":        QwenRerankerModelID,
		"Qwen3-Reranker-0.6B":   QwenRerankerModelID,
		"bge-m3":                BGEM3EmbeddingModelID,
		"bge-m3-1024维":            BGEM3EmbeddingModelID,
	}
	for in, want := range cases {
		if got := NormalizeModelName(in); got != want {
			t.Fatalf("%s => %s, want %s", in, got, want)
		}
	}
}

func TestDisplayModelName(t *testing.T) {
	if got := DisplayModelName(Qwen36ModelID); got != Qwen36ModelName {
		t.Fatalf("display model: %s", got)
	}
}

func TestBuiltinModelMetadata(t *testing.T) {
	models := BuiltinModels()
	var deepseek ModelSpec
	var qwen ModelSpec
	var reranker ModelSpec
	var bge ModelSpec
	for _, spec := range models {
		switch spec.ID {
		case DeepSeekV32ModelID:
			deepseek = spec
		case Qwen36ModelID:
			qwen = spec
		case QwenRerankerModelID:
			reranker = spec
		case BGEM3EmbeddingModelID:
			bge = spec
		}
	}
	if deepseek.ContextWindow != 65536 {
		t.Fatalf("deepseek context: %d", deepseek.ContextWindow)
	}
	if qwen.ContextWindow != 262144 {
		t.Fatalf("qwen context: %d", qwen.ContextWindow)
	}
	foundVision := false
	for _, cap := range qwen.Capabilities {
		if cap == "vision" {
			foundVision = true
			break
		}
	}
	if !foundVision {
		t.Fatal("qwen should support vision")
	}
	if reranker.VectorDim != 1024 {
		t.Fatalf("reranker vector dim: %d", reranker.VectorDim)
	}
	if bge.VectorDim != 1024 {
		t.Fatalf("bge vector dim: %d", bge.VectorDim)
	}
	if len(reranker.Capabilities) != 2 || reranker.Capabilities[0] != "rerank" || reranker.Capabilities[1] != "embedding" {
		t.Fatalf("%s capabilities: %#v", reranker.Name, reranker.Capabilities)
	}
	if len(bge.Capabilities) != 1 || bge.Capabilities[0] != "embedding" {
		t.Fatalf("%s capabilities: %#v", bge.Name, bge.Capabilities)
	}
}
