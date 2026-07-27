package providers

import "testing"

func TestNormalizeBaseURL(t *testing.T) {
	cases := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"https://api.openai.com/v1/", "https://api.openai.com/v1", false},
		{"https://api.openai.com/v1/chat/completions", "https://api.openai.com/v1", false},
		{"http://localhost:11434/v1", "http://localhost:11434/v1", false},
		{"http://api.example.com/v1", "", true},
		{"", "", true},
		{"ftp://foo", "", true},
	}
	for _, c := range cases {
		got, err := NormalizeBaseURL(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("NormalizeBaseURL(%q): err=%v wantErr=%v", c.in, err, c.wantErr)
			continue
		}
		if got != c.want && !c.wantErr {
			t.Errorf("NormalizeBaseURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParseModelsInput(t *testing.T) {
	models, err := ParseModelsInput("gpt-4o=128000, gpt-4o-mini\n o1-preview=200_000")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(models) != 3 {
		t.Fatalf("want 3 models, got %d", len(models))
	}
	if models[0].ID != "gpt-4o" || models[0].MaxContextTokens != 128000 {
		t.Errorf("bad model[0]: %+v", models[0])
	}
	if models[2].MaxContextTokens != 200000 {
		t.Errorf("underscore parsing broke: %+v", models[2])
	}
	if _, err := ParseModelsInput(""); err == nil {
		t.Errorf("empty input should error")
	}
}

func TestSlugFromName(t *testing.T) {
	tests := map[string]string{
		"OpenAI":       "openai",
		"My Provider!": "my-provider",
		"  --Groq--":   "groq",
	}
	for in, want := range tests {
		got, err := SlugFromName(in)
		if err != nil {
			t.Errorf("SlugFromName(%q): %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("SlugFromName(%q) = %q, want %q", in, got, want)
		}
	}
	if _, err := SlugFromName("!!!"); err == nil {
		t.Errorf("empty slug should error")
	}
}
