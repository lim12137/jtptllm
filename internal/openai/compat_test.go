package openai

import "testing"

func TestChatToPrompt(t *testing.T) {
	in := ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}}
	if PromptFromChat(in) != "user: hi" {
		t.Fatal("prompt")
	}
}