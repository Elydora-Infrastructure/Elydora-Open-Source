package elydora

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	for _, integrationType := range got {
		if !integrationType.IsValid() {
			t.Fatalf("integration type %q must be valid", integrationType)
		}
	}
	if IntegrationType("future-cli").IsValid() {
		t.Fatal("unknown integration type must be invalid")
	}
}

func TestRegisterAgentRejectsInvalidIntegrationBeforeNetworkAccess(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requestCount++
	}))
	t.Cleanup(server.Close)

	client := &Client{baseURL: server.URL, httpClient: server.Client()}
	requests := []*RegisterAgentRequest{
		nil,
		{AgentID: "agent-1"},
		{AgentID: "agent-1", IntegrationType: IntegrationType("future-cli")},
	}
	for _, request := range requests {
		if _, err := client.RegisterAgent(request); err == nil {
			t.Fatalf("RegisterAgent(%+v) succeeded, want validation error", request)
		}
	}
	if requestCount != 0 {
		t.Fatalf("network request count = %d, want 0", requestCount)
	}
}
