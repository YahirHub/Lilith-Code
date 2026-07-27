package providers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchModelsUsesOpenAICompatibleModelsEndpoint(t *testing.T) {
	var gotPath, gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "gpt-5.4", "object": "model"},
				{"id": "custom-model", "object": "model"},
			},
		})
	}))
	defer server.Close()

	models, err := FetchModels(server.URL+"/v1", "test-key")
	if err != nil {
		t.Fatalf("FetchModels() error = %v", err)
	}
	if gotPath != "/v1/models" {
		t.Fatalf("path = %q, want /v1/models", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if len(models) != 2 || models[0].ID != "gpt-5.4" || models[1].ID != "custom-model" {
		t.Fatalf("models = %+v", models)
	}
	if models[0].MaxContextTokens <= 0 || models[1].MaxContextTokens <= 0 {
		t.Fatalf("los modelos descubiertos deben quedar con contexto resoluble: %+v", models)
	}
}
