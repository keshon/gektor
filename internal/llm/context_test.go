package llm

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestContextReportAccountsWithoutExposingContent(t *testing.T) {
	for _, messages := range [][]Message{
		{},
		{{Role: RoleSystem, Content: "CORE\n\nprivate project instructions"}, {Role: RoleUser, Content: "你好", Reasoning: "not replayed"}},
		{{Role: RoleSystem, Content: "CORE"}, {Role: RoleSystem, Content: "private project instructions"}, {Role: RoleUser, Content: "question", Images: []string{"image.png"}}},
	} {
		req := ChatRequest{Messages: messages, Tools: []ToolDef{{Name: "small", Parameters: json.RawMessage(`{"type":"object"}`)}, {Name: "large", Description: strings.Repeat("words ", 100), Parameters: json.RawMessage(`{"type":"object"}`)}}}
		report := AnalyzeContext(req, []PromptSection{{Name: "core", Text: "CORE"}, {Name: "project", Text: "private project instructions"}, {Name: "skills"}})
		total := 0
		for _, section := range report.Sections {
			if section.EstimatedTokens < 0 {
				t.Fatal(section)
			}
			total += section.EstimatedTokens
		}
		if total != EstimatePromptTokens(req) || report.EstimatedTokens != total || report.Tools[0].Name != "large" {
			t.Fatal(report)
		}
		data, _ := json.Marshal(report)
		if strings.Contains(string(data), "private project instructions") || report.BackendTokens != 0 {
			t.Fatal("report leaked content or invented a measured count")
		}
	}
}
