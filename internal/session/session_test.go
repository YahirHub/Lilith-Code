package session

import (
	"testing"

	"github.com/lilith/li/internal/providers/openai"
)

func TestSaveListLoadAndDelete(t *testing.T) {
	cfg := t.TempDir()
	project := t.TempDir()
	st := NewStore(cfg)

	s := New(project)
	s.Messages = []openai.Message{
		{Role: "user", Content: "Crea un html de ejemplo"},
		{Role: "assistant", Content: "Listo."},
	}
	if err := st.Save(s); err != nil {
		t.Fatalf("save: %v", err)
	}
	if s.Title != "Crea un html de ejemplo" {
		t.Fatalf("título inesperado: %q", s.Title)
	}

	metas, err := st.List(project)
	if err != nil || len(metas) != 1 || metas[0].Turns != 1 {
		t.Fatalf("list inesperado: %+v err=%v", metas, err)
	}

	loaded, err := st.Load(project, s.ID)
	if err != nil || len(loaded.Messages) != 2 {
		t.Fatalf("load inesperado: %+v err=%v", loaded, err)
	}

	latest, err := st.Latest(project)
	if err != nil || latest == nil || latest.ID != s.ID {
		t.Fatalf("latest inesperado: %+v err=%v", latest, err)
	}

	if err := st.Delete(project, s.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	metas, _ = st.List(project)
	if len(metas) != 0 {
		t.Fatalf("la sesión debería haberse borrado: %+v", metas)
	}
}

func TestEmptySessionIsNotPersisted(t *testing.T) {
	cfg := t.TempDir()
	project := t.TempDir()
	st := NewStore(cfg)
	if err := st.Save(New(project)); err != nil {
		t.Fatalf("save vacío: %v", err)
	}
	metas, _ := st.List(project)
	if len(metas) != 0 {
		t.Fatalf("no debería guardar conversaciones vacías: %+v", metas)
	}
}

func TestSlugSeparatesProjectsWithSameBaseName(t *testing.T) {
	if slug("/a/proyecto") == slug("/b/proyecto") {
		t.Fatal("dos rutas distintas no deben compartir historial")
	}
}
