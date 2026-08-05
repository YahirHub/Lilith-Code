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

func TestFetchModelsTreatsMissingEndpointAsUnsupportedCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	models, err := FetchModels(server.URL+"/v1", "")
	if models != nil {
		t.Fatalf("models = %+v, want nil", models)
	}
	if !IsModelCatalogUnavailable(err) {
		t.Fatalf("error = %v, want unsupported catalog", err)
	}
}

func TestFetchModelsEnrichesCurrentCommandCodeCatalog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "claude-fable-5", "object": "model"},
				{"id": "Qwen/Qwen3.8-Max", "object": "model"},
				{"id": "thinkingmachines/inkling-small", "object": "model"},
				{"id": "meta/muse-spark-1.1", "object": "model"},
			},
		})
	}))
	defer server.Close()

	models, err := FetchModels(server.URL+"/v1", "")
	if err != nil {
		t.Fatalf("FetchModels() error = %v", err)
	}

	want := map[string]int{
		"claude-fable-5":                 1_000_000,
		"Qwen/Qwen3.8-Max":               1_000_000,
		"thinkingmachines/inkling-small": 1_000_000,
		"meta/muse-spark-1.1":            1_048_576,
	}
	if len(models) != len(want) {
		t.Fatalf("models = %d, se esperaban %d", len(models), len(want))
	}
	for _, model := range models {
		if model.MaxContextTokens != want[model.ID] {
			t.Errorf("contexto de %q = %d, se esperaba %d", model.ID, model.MaxContextTokens, want[model.ID])
		}
		if model.Name == "" {
			t.Errorf("%q no recibió nombre visible", model.ID)
		}
	}
}
