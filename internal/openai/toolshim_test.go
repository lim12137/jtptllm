package openai

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestBuildToolPlanningChatRequest(t *testing.T) {
	req := BuildToolPlanningChatRequest(map[string]any{
		"model":       Qwen36ModelID,
		"temperature": 0.2,
		"tool_choice": "auto",
		"messages": []any{
			map[string]any{"role": "user", "content": "查天气"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name": "get_weather",
				},
			},
		},
	})

	if req["model"] != Qwen36ModelID {
		t.Fatalf("model: %#v", req["model"])
	}
	if _, ok := req["tools"]; ok {
		t.Fatal("planning request should not include tools")
	}
	if _, ok := req["tool_choice"]; ok {
		t.Fatal("planning request should not include tool_choice")
	}
	if !IsToolPlanningRequest(req) {
		t.Fatal("expected tool planning request marker")
	}
}

func TestParseToolPlanWrappedJSON(t *testing.T) {
	plan, err := ParseToolPlan("```json\n{\"need_tool\":false,\"tool_name\":\"\",\"arguments\":{},\"final_answer\":\"ok\"}\n```")
	if err != nil {
		t.Fatalf("parse tool plan: %v", err)
	}
	if plan.NeedTool {
		t.Fatal("expected no tool")
	}
	if plan.FinalAnswer != "ok" {
		t.Fatalf("final answer: %#v", plan.FinalAnswer)
	}
}

func TestShouldUseLocalToolShimOnlyForQwen36WithTools(t *testing.T) {
	payload := map[string]any{
		"tools": []any{map[string]any{"type": "function"}},
	}
	if !ShouldUseLocalToolShim(Qwen36ModelID, payload) {
		t.Fatal("expected shim for qwen model id")
	}
	if ShouldUseLocalToolShim("gpt-4.1", payload) {
		t.Fatal("did not expect shim for other model")
	}
	if ShouldUseLocalToolShim(Qwen36ModelID, map[string]any{}) {
		t.Fatal("did not expect shim without tools")
	}
}

// TestSummarizeToolsSmallList 小列表（<=阈值）输出完整 JSON。
func TestSummarizeToolsSmallList(t *testing.T) {
	tools := []any{
		map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":       "get_weather",
				"parameters": map[string]any{"type": "object"},
			},
		},
	}
	out := summarizeTools(tools, 5)
	if !strings.Contains(out, "parameters") {
		t.Fatalf("small tool list should keep full schema, got: %s", out)
	}
}

// TestSummarizeToolsLargeList 大列表只输出 name+description。
func TestSummarizeToolsLargeList(t *testing.T) {
	tools := make([]any, 0, 7)
	for i := 0; i < 7; i++ {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "tool_" + string(rune('a'+i)),
				"description": "does thing " + string(rune('a'+i)),
				"parameters":  map[string]any{"type": "object", "properties": map[string]any{"x": map[string]any{"type": "string"}}},
			},
		})
	}
	out := summarizeTools(tools, 5)
	if strings.Contains(out, "parameters") {
		t.Fatalf("large tool list should drop parameters schema, got: %s", out)
	}
	if !strings.Contains(out, "tool_a") || !strings.Contains(out, "does thing a") {
		t.Fatalf("large tool list should keep name+description, got: %s", out)
	}
}

// TestMessageToPlanningLineAssistantToolCalls assistant 的 tool_calls 被渲染。
func TestMessageToPlanningLineAssistantToolCalls(t *testing.T) {
	msg := map[string]any{
		"role":    "assistant",
		"content": "",
		"tool_calls": []any{
			map[string]any{
				"id": "call_1",
				"function": map[string]any{
					"name":      "get_weather",
					"arguments": `{"city":"北京"}`,
				},
			},
		},
	}
	line := messageToPlanningLine(msg, "")
	if !strings.Contains(line, "get_weather") {
		t.Fatalf("expected tool call rendered, got: %s", line)
	}
	if !strings.Contains(line, "北京") {
		t.Fatalf("expected arguments rendered, got: %s", line)
	}
	if !strings.HasPrefix(line, "assistant:") {
		t.Fatalf("expected assistant prefix, got: %s", line)
	}
}

// TestMessageToPlanningLineToolResult tool 结果正常提取。
func TestMessageToPlanningLineToolResult(t *testing.T) {
	msg := map[string]any{
		"role":    "tool",
		"content": "晴，26C",
	}
	line := messageToPlanningLine(msg, "")
	if line != "tool: 晴，26C" {
		t.Fatalf("expected tool result line, got: %s", line)
	}
}

// TestMessageToPlanningLineAnthropicToolResult Anthropic 风格 tool_result 提取。
func TestMessageToPlanningLineAnthropicToolResult(t *testing.T) {
	msg := map[string]any{
		"role": "user",
		"content": []any{
			map[string]any{
				"type":    "tool_result",
				"content": "2026-06-15",
			},
		},
	}
	line := messageToPlanningLine(msg, "")
	if !strings.Contains(line, "2026-06-15") {
		t.Fatalf("expected anthropic tool_result extracted, got: %s", line)
	}
}

func TestMessageToPlanningLineSkillsMetadataSummary(t *testing.T) {
	msg := map[string]any{
		"role": "system",
		"content": "普通 system 规则\n[skills-summary-metadata] 以下是可用 skills 的摘要元数据（仅摘要，不含 SKILL 正文）。\n" +
			"{\n" +
			`  "skills": [` + "\n" +
			`    {"id":"doc-to-quiz","name":"Doc To Quiz","description":"把文档转成题库并生成配套内容","allowedTools":["get_skill","read_skill_resource"],"skillFile":"skills/doc-to-quiz/SKILL.md"},` + "\n" +
			`    {"id":"skill-creator","name":"Skill Creator","description":"创建和修改技能定义","allowedTools":["get_skill"],"skillFile":"skills/skill-creator/SKILL.md"}` + "\n" +
			"  ]\n" +
			"}",
	}

	line := messageToPlanningLine(msg, "")
	if !strings.Contains(line, "普通 system 规则") {
		t.Fatalf("expected to keep normal system head, got: %s", line)
	}
	if !strings.Contains(line, "已省略 skill 摘要详情以节省上下文") {
		t.Fatalf("expected summarized metadata placeholder, got: %s", line)
	}
	if !strings.Contains(line, "list_skills / get_skill") {
		t.Fatalf("expected get/list guidance kept, got: %s", line)
	}
}

func TestMessageToPlanningLineSkillsMetadataFallback(t *testing.T) {
	msg := map[string]any{
		"role":    "system",
		"content": "普通 system 规则\n[skills-summary-metadata] not-json",
	}

	line := messageToPlanningLine(msg, "")
	if !strings.Contains(line, "已省略 skill 摘要详情以节省上下文") {
		t.Fatalf("expected fallback placeholder, got: %s", line)
	}
}

func TestMessageToPlanningLineSkillsMetadataCompactMode(t *testing.T) {
	t.Setenv(skillSummaryModeEnv, "compact")
	msg := map[string]any{
		"role": "system",
		"content": "普通 system 规则\n[skills-summary-metadata] 以下是可用 skills 的摘要元数据（仅摘要，不含 SKILL 正文）。\n" +
			"{\n" +
			`  "skills": [` + "\n" +
			`    {"id":"doc-to-quiz","name":"Doc To Quiz","description":"把文档转成题库并生成配套内容","allowedTools":["get_skill","read_skill_resource"]},` + "\n" +
			`    {"id":"skill-creator","name":"Skill Creator","description":"创建和修改技能定义","allowedTools":["get_skill"]}` + "\n" +
			"  ]\n" +
			"}",
	}

	line := messageToPlanningLine(msg, "")
	if !strings.Contains(line, "compact：保留技能索引") {
		t.Fatalf("expected compact summary marker, got: %s", line)
	}
	if !strings.Contains(line, "doc-to-quiz") || !strings.Contains(line, "skill-creator") {
		t.Fatalf("expected compact summary to keep skill ids, got: %s", line)
	}
	if !strings.Contains(line, "tools=get_skill,read_skill_resource") {
		t.Fatalf("expected compact summary to keep allowed tools, got: %s", line)
	}
}

func TestSummarizeSkillsMetadataCompactFallbacksOnInvalidJSON(t *testing.T) {
	t.Setenv(skillSummaryModeEnv, "compact")
	if got := summarizeSkillsMetadataForPlanning("[skills-summary-metadata] invalid", ""); !strings.Contains(got, "已省略 skill 摘要详情以节省上下文") {
		t.Fatalf("expected fallback placeholder, got: %s", got)
	}
}

func TestResolveSkillSummaryDecisionConditionalPureSkill(t *testing.T) {
	t.Setenv(skillSummaryModeEnv, "conditional")
	msgs := []map[string]any{
		{"role": "user", "content": "请用 skill 做文档转题库，先 list_skills 再 get_skill。"},
	}
	decision := resolveSkillSummaryDecision(msgs)
	if decision.mode != "compact" {
		t.Fatalf("expected compact mode, got %#v", decision)
	}
}

func TestResolveSkillSummaryDecisionConditionalMixedTask(t *testing.T) {
	t.Setenv(skillSummaryModeEnv, "conditional")
	msgs := []map[string]any{
		{"role": "user", "content": "读取 skill-creator 技能的 SKILL.md，再看 apps/code-agent/code-agent.ts 的 tool-registry。"},
	}
	decision := resolveSkillSummaryDecision(msgs)
	if decision.mode != "" {
		t.Fatalf("expected placeholder mode, got %#v", decision)
	}
}

func TestPlanningToolAnchors(t *testing.T) {
	anchors := planningToolAnchors()
	if !strings.Contains(anchors, `get_skill 只接受 arguments: {"id":"<skill-id>"}`) {
		t.Fatalf("missing get_skill anchor: %s", anchors)
	}
	if !strings.Contains(anchors, `list_skills 查询关键词优先使用 arguments: {"query":"<keyword>"}`) {
		t.Fatalf("missing list_skills query anchor: %s", anchors)
	}
	if !strings.Contains(anchors, `read 只接受 arguments: {"path":"<path>"}`) {
		t.Fatalf("missing read anchor: %s", anchors)
	}
	if !strings.Contains(anchors, `read_skill_resource 规范参数是 {"id":"<skill-id>","path":"<resource-path>"}`) {
		t.Fatalf("missing read_skill_resource anchor: %s", anchors)
	}
	if !strings.Contains(anchors, `runCodeAgentCli / main / runOneUserTurn`) {
		t.Fatalf("missing entry search anchor: %s", anchors)
	}
	if !strings.Contains(anchors, `CODE_AGENT_TOOL_SCHEMAS / TOOL_HANDLERS / getCodeAgentToolSchemas`) {
		t.Fatalf("missing tool registry anchor: %s", anchors)
	}
}

// TestTruncateRunes 按 rune 截断，中文字符不产生乱码。
func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("abcdef", 10); got != "abcdef" {
		t.Fatalf("short string should be unchanged, got %q", got)
	}
	if got := truncateRunes("abcdef", 3); got != "abc…" {
		t.Fatalf("ascii truncate, got %q", got)
	}
	if got := truncateRunes("你好世界测试", 4); got != "你好世界…" {
		t.Fatalf("chinese truncate by rune, got %q", got)
	}
}

// TestSummarizeToolsTruncatesLongDescription 超长 description 被截断。
func TestSummarizeToolsTruncatesLongDescription(t *testing.T) {
	longDesc := strings.Repeat("详情", 100) // 200 runes
	tools := make([]any, 0, 7)
	for i := 0; i < 7; i++ {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        "t" + string(rune('a'+i)),
				"description": longDesc,
			},
		})
	}
	out := summarizeTools(tools, 5)
	// 每个工具 description 应被截断到 120 runes + …，不会出现完整 200 runes。
	for _, r := range []string{"a", "b", "c"} {
		idx := strings.Index(out, "t"+r+": ")
		if idx < 0 {
			t.Fatalf("tool t%s not found in summary", r)
		}
		// 截断后该工具描述的 rune 数不应超过 120+前缀
		lineEnd := strings.IndexByte(out[idx:], '\n')
		if lineEnd < 0 {
			lineEnd = len(out) - idx
		}
		line := out[idx : idx+lineEnd]
		if utf8.RuneCountInString(line) > 140 {
			t.Fatalf("description for t%s not truncated, line runes=%d: %s", r, utf8.RuneCountInString(line), line[:50])
		}
	}
}
