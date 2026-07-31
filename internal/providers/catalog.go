package providers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	modelcat "github.com/lilith/li/internal/models"
)

// Enrich completa nombre y ventana de contexto de cada modelo a partir del
// catálogo conocido. La coincidencia es por nombre normalizado, así que
// `omnirouter/deepseek-v4-flash` u `opencode/deepseek_v4_pro` heredan la misma
// configuración que `deepseek-v4-flash`/`deepseek-v4-pro`.
func Enrich(in []Model) []Model {
	out := make([]Model, len(in))
	copy(out, in)
	for i := range out {
		spec, ok := modelcat.Lookup(out[i].ID)
		if out[i].MaxContextTokens <= 0 {
			if ok && spec.MaxContext > 0 {
				out[i].MaxContextTokens = spec.MaxContext
			} else {
				out[i].MaxContextTokens = modelcat.DefaultMaxContext
			}
		}
		if out[i].MaxOutputTokens <= 0 && ok && spec.MaxOutput > 0 {
			out[i].MaxOutputTokens = spec.MaxOutput
		}
		if out[i].Name == "" && ok {
			out[i].Name = spec.Name
		}
	}
	return out
}

// ContextWindow devuelve la ventana de contexto del modelo activo del
// proveedor, usando el catálogo cuando el proveedor no la declara.
func (p *Provider) ContextWindow(modelID string) int {
	if p != nil {
		if m := findModel(p.Models, modelID); m != nil && m.MaxContextTokens > 0 {
			return m.MaxContextTokens
		}
	}
	return modelcat.MaxContext(modelID)
}

// ResolveKeyInput convierte la entrada del formulario ("sk-…", "env:VAR",
// "none", "") en la credencial efectiva para una consulta inmediata.
func ResolveKeyInput(input string) string {
	v := strings.TrimSpace(input)
	if v == "" || strings.EqualFold(v, "none") {
		return ""
	}
	if len(v) > 4 && strings.EqualFold(v[:4], "env:") {
		return strings.TrimSpace(os.Getenv(strings.TrimSpace(v[4:])))
	}
	return v
}

// FetchModels consulta {baseURL}/models en un endpoint OpenAI-compatible y
// devuelve el catálogo enriquecido. La credencial es opcional.
func FetchModels(baseURL, apiKeyInput string) ([]Model, error) {
	base, err := NormalizeBaseURL(baseURL)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, base+"/models", nil)
	if err != nil {
		return nil, err
	}
	if key := ResolveKeyInput(apiKeyInput); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("No se pudo consultar %s/models: %v", base, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		if modelCatalogUnavailableStatus(resp.StatusCode) {
			return nil, newModelCatalogUnavailableError(base+"/models", resp.StatusCode)
		}
		return nil, fmt.Errorf("El endpoint respondió HTTP %d al listar modelos.", resp.StatusCode)
	}

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, errors.New("La respuesta de /models no es un JSON válido.")
	}

	seen := map[string]bool{}
	out := []Model{}
	for _, item := range payload.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, Model{ID: id})
	}
	if len(out) == 0 {
		return nil, errors.New("El endpoint no devolvió ningún modelo.")
	}
	return Enrich(out), nil
}
