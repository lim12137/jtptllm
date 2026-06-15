package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	Qwen36ModelID   = "rsv-q23123sde"
	Qwen36ModelName = "Qwen3.6-27B"
)

type ToolPlan struct {
	NeedTool    bool           `json:"need_tool"`
	ToolName    string         `json:"tool_name"`
	Arguments   map[string]any `json:"arguments"`
	FinalAnswer string         `json:"final_answer"`
}

func ShouldUseLocalToolShim(model string, payload map[string]any) bool {
	if !HasTools(payload) {
		return false
	}
	m := NormalizeModelName(model)
	return m == Qwen36ModelID || m == Qwen36ModelName
}

func HasTools(payload map[string]any) bool {
	tools, ok := payload["tools"].([]any)
	return ok && len(tools) > 0
}

func BuildToolPlanningChatRequest(payload map[string]any) map[string]any {
	model := NormalizeModelName(strOr(payload["model"], "agent"))
	rawMsgs := asRawMessages(payload["messages"])
	loopDecision := AnalyzeToolLoop(rawMsgs)
	prompt := buildToolPlanningPrompt(rawMsgs, payload["tools"], loopDecision)

	out := map[string]any{
		"model":  model,
		"stream": false,
		"messages": []any{
			map[string]any{
				"role":    "system",
				"content": toolPlanningSystemPrompt(),
			},
			map[string]any{
				"role":    "user",
				"content": prompt,
			},
		},
		"response_format": map[string]any{"type": "json_object"},
	}
	copyIfPresent(payload, out, "temperature", "top_p", "presence_penalty", "max_tokens")
	return out
}

func IsToolPlanningRequest(payload map[string]any) bool {
	msgs := asMessages(payload["messages"])
	if len(msgs) != 2 {
		return false
	}
	if strings.TrimSpace(msgs[0].Role) != "system" {
		return false
	}
	if strings.TrimSpace(msgs[1].Role) != "user" {
		return false
	}
	return strings.Contains(coerceContentToStr(msgs[0].Content), "你是一个工具规划器")
}

// DebugPlanningMetrics 在 SHIM_DEBUG=1 时打印 planning prompt 的规模指标。
// SHIM_DUMP=1 时额外把 prompt 写到 shim_prompt_dump.txt（用于分析构成）。
// 返回值始终为 true（便于在 handler 里 if 包裹，不额外占行）。
func DebugPlanningMetrics(planningReq, originalPayload map[string]any) bool {
	msgs, _ := originalPayload["messages"].([]any)
	tools, _ := originalPayload["tools"].([]any)
	promptLen := 0
	var promptContent string
	if list, _ := planningReq["messages"].([]any); len(list) >= 2 {
		if um, ok := list[1].(map[string]any); ok {
			if c, ok := um["content"].(string); ok {
				promptLen = len(c)
				promptContent = c
			}
		}
	}
	if os.Getenv("SHIM_DEBUG") == "1" {
		log.Printf("[shim-debug] messages=%d tools=%d planning_prompt_chars=%d planning_prompt_approx_tokens=%d",
			len(msgs), len(tools), promptLen, promptLen/4)
	}
	if os.Getenv("SHIM_DUMP") == "1" && promptContent != "" {
		_ = os.WriteFile("shim_prompt_dump.txt", []byte(promptContent), 0o644)
		if data, err := json.MarshalIndent(originalPayload, "", "  "); err == nil {
			_ = os.WriteFile("shim_payload_dump.json", data, 0o644)
		}
	}
	return true
}

func ParseToolPlan(text string) (ToolPlan, error) {
	clean := strings.TrimSpace(text)
	clean = strings.TrimPrefix(clean, "```json")
	clean = strings.TrimPrefix(clean, "```")
	clean = strings.TrimSuffix(clean, "```")
	clean = strings.TrimSpace(clean)

	var plan ToolPlan
	if err := json.Unmarshal([]byte(clean), &plan); err != nil {
		return ToolPlan{}, err
	}
	if plan.Arguments == nil {
		plan.Arguments = map[string]any{}
	}
	if plan.NeedTool && strings.TrimSpace(plan.ToolName) == "" {
		return ToolPlan{}, errors.New("tool_name 为空")
	}
	if !plan.NeedTool && strings.TrimSpace(plan.FinalAnswer) == "" {
		return ToolPlan{}, errors.New("final_answer 为空")
	}
	return plan, nil
}

// ForceStopPlan 在判定为工具循环时，把 planner 的 plan 强制改写为"直接总结"。
// 若 plan 自身已 NeedTool=false 则原样返回；否则基于工具结果合成一个兜底 final_answer。
// 合成时只参考「最近一条真实 user 消息之后」的工具结果（当前回合），避免混入历史回合。
func ForceStopPlan(plan ToolPlan, messages []map[string]any, reason string) ToolPlan {
	if !plan.NeedTool {
		return plan
	}
	answer := strings.TrimSpace(plan.FinalAnswer)
	if answer == "" {
		answer = synthesizeFinalAnswerFromToolResults(truncateToCurrentTurn(messages))
	}
	if answer == "" {
		// 兜底仍为空时给一个占位，避免 ParseToolPlan 的 final_answer 校验在下游失败
		answer = "（已达到工具调用上限，基于已收集的工具结果无法生成更详细的总结，请基于上方工具结果继续。）"
	}
	if os.Getenv("SHIM_DEBUG") == "1" {
		log.Printf("[shim-debug] tool-loop-force-stop applied reason=%s synthesized=%v", reason, plan.FinalAnswer == "")
	}
	return ToolPlan{
		NeedTool:    false,
		ToolName:    "",
		Arguments:   map[string]any{},
		FinalAnswer: answer,
	}
}

func BuildToolCallChatCompletionResponse(plan ToolPlan, model string) map[string]any {
	created := time.Now().Unix()
	cid := newID("chatcmpl")
	if !plan.NeedTool {
		return map[string]any{
			"id":      cid,
			"object":  "chat.completion",
			"created": created,
			"model":   model,
			"choices": []any{
				map[string]any{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": plan.FinalAnswer,
					},
					"finish_reason": "stop",
				},
			},
		}
	}

	args, _ := json.Marshal(plan.Arguments)
	return map[string]any{
		"id":      cid,
		"object":  "chat.completion",
		"created": created,
		"model":   model,
		"choices": []any{
			map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": "",
					"tool_calls": []any{
						map[string]any{
							"id":   newID("call"),
							"type": "function",
							"function": map[string]any{
								"name":      plan.ToolName,
								"arguments": string(args),
							},
						},
					},
				},
				"finish_reason": "tool_calls",
			},
		},
	}
}

func IterToolCallChatCompletionSSE(plan ToolPlan, model string) []string {
	created := time.Now().Unix()
	cid := newID("chatcmpl")
	var out []string
	out = append(out, sseData(map[string]any{
		"id":      cid,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"role": "assistant"}, "finish_reason": nil}},
	}))

	if !plan.NeedTool {
		if strings.TrimSpace(plan.FinalAnswer) != "" {
			out = append(out, sseData(map[string]any{
				"id":      cid,
				"object":  "chat.completion.chunk",
				"created": created,
				"model":   model,
				"choices": []any{map[string]any{"index": 0, "delta": map[string]any{"content": plan.FinalAnswer}, "finish_reason": nil}},
			}))
		}
		out = append(out, sseData(map[string]any{
			"id":      cid,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}},
		}))
		out = append(out, "data: [DONE]\n\n")
		return out
	}

	args, _ := json.Marshal(plan.Arguments)
	out = append(out, sseData(map[string]any{
		"id":      cid,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []any{
			map[string]any{
				"index": 0,
				"delta": map[string]any{
					"tool_calls": []any{
						map[string]any{
							"index": 0,
							"id":    newID("call"),
							"type":  "function",
							"function": map[string]any{
								"name":      plan.ToolName,
								"arguments": string(args),
							},
						},
					},
				},
				"finish_reason": nil,
			},
		},
	}))
	out = append(out, sseData(map[string]any{
		"id":      cid,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []any{map[string]any{"index": 0, "delta": map[string]any{}, "finish_reason": "tool_calls"}},
	}))
	out = append(out, "data: [DONE]\n\n")
	return out
}

func toolPlanningSystemPrompt() string {
	return strings.TrimSpace(`
你是一个工具规划器，不直接回答用户问题。
你必须在给定工具列表中选择是否需要调用工具。
你必须返回严格 JSON，不要输出 markdown，不要输出解释。

返回格式：
{
  "need_tool": true,
  "tool_name": "工具名",
  "arguments": {},
  "final_answer": ""
}

如果不需要工具，返回：
{
  "need_tool": false,
  "tool_name": "",
  "arguments": {},
  "final_answer": "直接给用户的最终回答"
}

重要规则：
- 若对话历史中已有工具返回结果（role 为 tool/function，或 user 内含 tool_result），且足以回答问题，则必须 need_tool:false 并总结，禁止重复调用；只有用户提出结果无法覆盖的新需求时才再次调用。
- 优先根据“最后一个用户请求”和“最近的工具结果”判断，而不是重复读取整个历史。`)
}

const (
	// toolSummarizeThreshold 工具数超过此阈值时，只输出 name+description 而非完整 schema。
	toolSummarizeThreshold = 5
	// skillSummaryModeEnv 控制 skills metadata 的摘要方式。
	skillSummaryModeEnv = "SHIM_SKILL_SUMMARY_MODE"
)

func buildToolPlanningPrompt(messages []map[string]any, tools any, loopDecision ToolLoopDecision) string {
	skillSummaryDecision := resolveSkillSummaryDecision(messages)
	if os.Getenv("SHIM_DEBUG") == "1" {
		if skillSummaryDecision.reason != "" {
			log.Printf("[shim-debug] skill_summary_mode=%s reason=%s", skillSummaryDecision.mode, skillSummaryDecision.reason)
		}
		if loopDecision.ForceStop {
			log.Printf("[shim-debug] tool-loop-force-stop reason=%s", loopDecision.Reason)
		}
	}
	var b strings.Builder
	b.WriteString("可用工具列表:\n")
	if os.Getenv("SHIM_SUMMARIZE") == "0" {
		// baseline：禁用摘要化，全量输出 JSON（用于 A/B 对比）
		if data, err := json.MarshalIndent(tools, "", "  "); err == nil {
			b.Write(data)
		} else {
			b.WriteString(fmt.Sprintf("%v", tools))
		}
	} else {
		b.WriteString(summarizeTools(tools, toolSummarizeThreshold))
	}
	b.WriteString("\n\n用户对话:\n")
	for _, msg := range messages {
		line := messageToPlanningLine(msg, skillSummaryDecision.mode)
		if line == "" {
			continue
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n工具参数提示:\n")
	b.WriteString(planningToolAnchors())
	if loopDecision.ForceStop && loopDecision.Hint != "" {
		b.WriteString(loopDecision.Hint)
	}
	return strings.TrimSpace(b.String())
}

// summarizeTools 将工具列表序列化为紧凑文本。
// 工具数 > threshold 时只输出 name + description（去掉 parameters schema），大幅减少 token。
// 工具数 <= threshold 时输出完整 JSON（小列表不值得摘要）。
func summarizeTools(tools any, threshold int) string {
	list, ok := tools.([]any)
	if !ok || len(list) == 0 {
		if data, err := json.MarshalIndent(tools, "", "  "); err == nil {
			return string(data)
		}
		return fmt.Sprintf("%v", tools)
	}

	// 抽取每个工具的 name + description。
	type toolBrief struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}
	briefs := make([]toolBrief, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		// 兼容 {function:{name,description}} 和直接 {name,description} 两种结构。
		fn, _ := m["function"].(map[string]any)
		if fn == nil {
			fn = m
		}
		name, _ := fn["name"].(string)
		desc, _ := fn["description"].(string)
		briefs = append(briefs, toolBrief{Name: name, Description: desc})
	}

	// 小列表：完整输出。
	if len(briefs) <= threshold {
		if data, err := json.MarshalIndent(list, "", "  "); err == nil {
			return string(data)
		}
	}

	// 大列表：只输出 name + description（超长的 description 截断，避免 skill 类工具带整段说明撑爆 prompt）。
	const descMaxRunes = 120
	var sb strings.Builder
	for i, br := range briefs {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("- ")
		sb.WriteString(br.Name)
		if br.Description != "" {
			sb.WriteString(": ")
			sb.WriteString(truncateRunes(br.Description, descMaxRunes))
		}
	}
	sb.WriteString("\n（如需调用工具，arguments 留空或填合理值，由调用方补全参数）")
	return sb.String()
}

// truncateRunes 按 rune 截断字符串，超过 max 的部分用 "…" 替代。
// 用于 summary 中超长 description 的保护性截断（避免中文按字节截断产生乱码）。
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// messageToPlanningLine 把单条消息渲染为 planning prompt 的一行 "role: content"。
// 对 assistant 带 tool_calls 的消息，渲染工具调用信息（不再丢失）。
func messageToPlanningLine(msg map[string]any, skillSummaryMode string) string {
	role := strings.ToLower(strings.TrimSpace(strOr(msg["role"], "")))
	if role == "" {
		role = "user"
	}

	// assistant 且带 tool_calls：渲染工具调用。
	if role == "assistant" {
		if calls, ok := msg["tool_calls"].([]any); ok && len(calls) > 0 {
			var parts []string
			for _, c := range calls {
				cm, ok := c.(map[string]any)
				if !ok {
					continue
				}
				fn, _ := cm["function"].(map[string]any)
				if fn == nil {
					continue
				}
				name, _ := fn["name"].(string)
				args, _ := fn["arguments"].(string)
				if name == "" {
					continue
				}
				if args != "" {
					parts = append(parts, name+"("+args+")")
				} else {
					parts = append(parts, name+"()")
				}
			}
			if len(parts) > 0 {
				return role + ": [调用工具 " + strings.Join(parts, ", ") + "]"
			}
		}
	}

	content := strings.TrimSpace(coerceContentToStr(msg["content"]))
	if content == "" {
		return ""
	}
	// 达尔文轮2变异：skills metadata system 消息体积巨大（40 skill × ~800字 ≈ 3万字），
	// 但 planning 阶段只需选"工具"（read/write/get_skill），不需要 617 个 skill 的详情。
	// 注意：code-agent 的 chat-completions.ts 会把多个 system 消息合并成一个，
	// 所以 [skills-summary-metadata] 可能在合并消息中间，用 Contains 而非 HasPrefix 匹配。
	const skillsMetadataMarker = "[skills-summary-metadata]"
	if strings.Contains(content, skillsMetadataMarker) {
		idx := strings.Index(content, skillsMetadataMarker)
		head := strings.TrimSpace(content[:idx])
		tail := strings.TrimSpace(content[idx:])
		skillsSummary := summarizeSkillsMetadataForPlanning(tail, skillSummaryMode)
		if head != "" {
			return role + ": " + head + "\n" + skillsSummary
		}
		return role + ": " + skillsSummary
	}
	// 达尔文轮1兜底：其他超长单条消息仍做上限保护（防 code-agent 注入其他大 system）。
	const maxPlanningMessageRunes = 3000
	rendered := role + ": " + content
	if r := []rune(rendered); len(r) > maxPlanningMessageRunes {
		rendered = string(r[:maxPlanningMessageRunes]) + "…[截断，原长" + strconv.Itoa(len(r)) + "字符]"
	}
	return rendered
}

func summarizeSkillsMetadataForPlanning(content, mode string) string {
	if mode == "" {
		mode = strings.ToLower(strings.TrimSpace(os.Getenv(skillSummaryModeEnv)))
	}
	if mode == "compact" {
		if summary := summarizeSkillsMetadataCompact(content, 8); summary != "" {
			return summary
		}
	}
	_ = content
	return "[skills-summary-metadata]（已省略 skill 摘要详情以节省上下文；如需技能详情，请调用 list_skills / get_skill；如需资源，再调用 read_skill_resource）"
}

type skillSummaryDecision struct {
	mode   string
	reason string
}

func resolveSkillSummaryDecision(messages []map[string]any) skillSummaryDecision {
	envMode := strings.ToLower(strings.TrimSpace(os.Getenv(skillSummaryModeEnv)))
	switch envMode {
	case "compact":
		return skillSummaryDecision{mode: "compact", reason: "env-compact"}
	case "conditional":
		text := latestUserText(messages)
		if text == "" {
			return skillSummaryDecision{mode: "", reason: "conditional-no-user-text"}
		}
		if looksLikeMixedSkillAndCodeTask(text) {
			return skillSummaryDecision{mode: "", reason: "conditional-mixed-skill-code"}
		}
		if looksLikePureSkillTask(text) {
			return skillSummaryDecision{mode: "compact", reason: "conditional-pure-skill"}
		}
		return skillSummaryDecision{mode: "", reason: "conditional-non-skill"}
	default:
		return skillSummaryDecision{mode: envMode, reason: ""}
	}
}

func latestUserText(messages []map[string]any) string {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		role := strings.ToLower(strings.TrimSpace(strOr(msg["role"], "")))
		if role != "user" {
			continue
		}
		text := strings.TrimSpace(coerceContentToStr(msg["content"]))
		if text != "" {
			return text
		}
	}
	return ""
}

func looksLikePureSkillTask(text string) bool {
	lower := strings.ToLower(text)
	skillMarkers := []string{
		"skill", "技能", "list_skills", "get_skill", "read_skill_resource",
		"doc-to-quiz", "regulation-summary", "skill-creator",
	}
	for _, marker := range skillMarkers {
		if strings.Contains(lower, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

func looksLikeMixedSkillAndCodeTask(text string) bool {
	lower := strings.ToLower(text)
	codeMarkers := []string{
		"apps/code-agent/", "code-agent.ts", "tool-registry",
		"code_agent_tool_schemas", "tool_handlers", "getcodeagenttoolschemas",
		".ts", ".go", "入口函数", "源码", "仓库代码",
	}
	for _, marker := range codeMarkers {
		if strings.Contains(lower, strings.ToLower(marker)) {
			return true
		}
	}
	return false
}

type skillSummaryEntry struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	AllowedTools []string `json:"allowedTools"`
}

type skillsSummaryPayload struct {
	Skills []skillSummaryEntry `json:"skills"`
}

func summarizeSkillsMetadataCompact(content string, limit int) string {
	raw, ok := extractJSONObject(content)
	if !ok {
		return ""
	}
	var payload skillsSummaryPayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	if len(payload.Skills) == 0 {
		return ""
	}
	if limit <= 0 || limit > len(payload.Skills) {
		limit = len(payload.Skills)
	}

	var sb strings.Builder
	sb.WriteString("[skills-summary-metadata]（compact：保留技能索引，仍优先按需调用 list_skills / get_skill / read_skill_resource）")
	for i := 0; i < limit; i++ {
		skill := payload.Skills[i]
		sb.WriteString("\n- ")
		if skill.ID != "" {
			sb.WriteString(skill.ID)
		} else {
			sb.WriteString("unknown")
		}
		if skill.Name != "" && skill.Name != skill.ID {
			sb.WriteString(" | ")
			sb.WriteString(skill.Name)
		}
		if skill.Description != "" {
			sb.WriteString(" | ")
			sb.WriteString(truncateRunes(skill.Description, 100))
		}
		if len(skill.AllowedTools) > 0 {
			sb.WriteString(" | tools=")
			sb.WriteString(strings.Join(skill.AllowedTools, ","))
		}
	}
	if len(payload.Skills) > limit {
		sb.WriteString("\n…")
	}
	return sb.String()
}

func extractJSONObject(content string) (string, bool) {
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return "", false
	}
	return content[start : end+1], true
}

func planningToolAnchors() string {
	return strings.TrimSpace(`
规则：get_skill 只接受 arguments: {"id":"<skill-id>"}，不要使用 skill_id / skill_name / name。
规则：list_skills 查询关键词优先使用 arguments: {"query":"<keyword>"}，不要优先使用 keywords。
规则：read 只接受 arguments: {"path":"<path>"}，不要使用 file / file_path / filename。
规则：read_skill_resource 规范参数是 {"id":"<skill-id>","path":"<resource-path>"}；不要优先生成 skill_id / resource，尽管执行侧会做兼容。
规则：如果用户要读 apps/code-agent/code-agent.ts 的入口函数，优先搜索 runCodeAgentCli / main / runOneUserTurn，不要先搜 export default。
规则：如果用户要理解 code-agent 的工具注册机制，不要硬搜不存在的 "tool-registry"；优先查看 code-agent-tools.ts 中的 CODE_AGENT_TOOL_SCHEMAS / TOOL_HANDLERS / getCodeAgentToolSchemas。`)
}

// asRawMessages 从 payload 的 messages 字段提取原始 map 切片（保留 tool_calls 等字段）。
func asRawMessages(v any) []map[string]any {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, m)
	}
	return out
}

// AsRawMessages 是 asRawMessages 的导出版本，供 http 层调用。
func AsRawMessages(v any) []map[string]any {
	return asRawMessages(v)
}
