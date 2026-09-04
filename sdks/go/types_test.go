package elydora

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestRegisterAgentRequestIncludesIntegrationType(t *testing.T) {
	payload, err := json.Marshal(RegisterAgentRequest{
		AgentID:         "agent-1",
		IntegrationType: IntegrationTypeSDK,
		Keys:            []RegisterAgentKeyInput{},
	})
	if err != nil {
		t.Fatalf("marshal register agent request: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unmarshal register agent request: %v", err)
	}
	if got := decoded["integration_type"]; got != "sdk" {
		t.Fatalf("integration_type = %v, want sdk", got)
	}
}

func TestIntegrationTypesMatchPublicAPIContract(t *testing.T) {
	got := []IntegrationType{
		IntegrationTypeAugment,
		IntegrationTypeClaudecode,
		IntegrationTypeCline,
		IntegrationTypeCodex,
		IntegrationTypeCopilot,
		IntegrationTypeCursor,
		IntegrationTypeDroid,
		IntegrationTypeGemini,
		IntegrationTypeGrok,
		IntegrationTypeKimi,
		IntegrationTypeKiroCLI,
		IntegrationTypeKiroIDE,
		IntegrationTypeLetta,
		IntegrationTypeOpenCode,
		IntegrationTypeQwen,
		IntegrationTypeEnterprise,
		IntegrationTypeGUI,
		IntegrationTypeSDK,
		IntegrationTypeOther,
	}
	want := []IntegrationType{
		"augment", "claudecode", "cline", "codex", "copilot", "cursor", "droid",
		"gemini", "grok", "kimi", "kirocli", "kiroide", "letta", "opencode", "qwen",
		"enterprise", "gui", "sdk", "other",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("integration types = %v, want %v", got, want)
	}
}
