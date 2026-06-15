package openai

import (
	"strings"
	"time"
)

func EmbeddingModelID(model string) string {
	normalized := NormalizeModelName(model)
	switch normalized {
	case "", "embedding", "embeddings":
		return BGEM3EmbeddingModelID
	case QwenRerankerModelID, BGEM3EmbeddingModelID:
		return normalized
	default:
		return BGEM3EmbeddingModelID
	}
}

func RerankModelID(model string) string {
	normalized := NormalizeModelName(model)
	switch normalized {
	case "", "rerank", "reranker":
		return QwenRerankerModelID
	case QwenRerankerModelID:
		return normalized
	default:
		return QwenRerankerModelID
	}
}

func BuildEmbeddingsRequest(payload map[string]any) map[string]any {
	out := map[string]any{
		"input": payload["input"],
	}
	if _, ok := payload["input"]; !ok {
		out["input"] = []any{}
	}
	return out
}

func BuildEmbeddingsResponse(body map[string]any, model string) map[string]any {
	data, _ := body["data"].([]any)
	if data == nil {
		data = []any{}
	}
	usage, _ := body["usage"].(map[string]any)
	if usage == nil {
		usage = map[string]any{"prompt_tokens": 0, "total_tokens": 0}
	}
	return map[string]any{
		"object": "list",
		"data":   data,
		"model":  model,
		"usage":  usage,
	}
}

func BuildRerankRequest(payload map[string]any) map[string]any {
	query := strOr(payload["query"], "")
	documents, _ := payload["documents"].([]any)
	parameters := map[string]any{
		"return_documents": true,
	}
	if topN, ok := payload["top_n"]; ok {
		parameters["top_n"] = topN
	}
	if returnDocs, ok := payload["return_documents"]; ok {
		parameters["return_documents"] = returnDocs
	}
	return map[string]any{
		"input": map[string]any{
			"query":     query,
			"documents": firstAnySlice(documents),
		},
		"parameters": parameters,
	}
}

func BuildRerankResponse(body map[string]any, model string) map[string]any {
	data, _ := body["data"].([]any)
	if data == nil {
		data = []any{}
	}
	results := make([]any, 0, len(data))
	for _, item := range data {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		result := map[string]any{
			"index":           row["index"],
			"relevance_score": row["relevance_score"],
		}
		if doc, ok := row["document"]; ok {
			result["document"] = doc
		}
		results = append(results, result)
	}
	return map[string]any{
		"id":      newSortableID("rerank"),
		"object":  "list",
		"model":   model,
		"results": results,
	}
}

func ValidateEmbeddingsPayload(payload map[string]any) string {
	if payload == nil {
		return "invalid json"
	}
	if _, ok := payload["input"]; !ok {
		return "missing input"
	}
	return ""
}

func ValidateRerankPayload(payload map[string]any) string {
	if payload == nil {
		return "invalid json"
	}
	if strings.TrimSpace(strOr(payload["query"], "")) == "" {
		return "missing query"
	}
	docs, ok := payload["documents"].([]any)
	if !ok || len(docs) == 0 {
		return "missing documents"
	}
	return ""
}

func firstAnySlice(v []any) []any {
	if v == nil {
		return []any{}
	}
	return v
}

func newSortableID(prefix string) string {
	return prefix + "_" + strings.ReplaceAll(time.Now().Format("20060102150405.000000000"), ".", "")
}
