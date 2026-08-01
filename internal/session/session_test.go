package session

import (
	"testing"
	"time"

	"github.com/lilith/li/internal/providers/openai"
	litodo "github.com/lilith/li/internal/todo"
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
	s.Todo = &litodo.State{SchemaVersion: litodo.SchemaVersion, Revision: 1, Tasks: []litodo.Task{{Key: "verify", Subject: "Verify result", Status: litodo.Pending}}}
	s.Compactions = []CompactionRecord{{ID: "compact-1", Summary: "estado anterior", TokensBefore: 90000, TokensAfter: 18000, ArchivedMessages: []openai.Message{{Role: "user", Content: "viejo"}}}}
	if err := st.Save(s); err != nil {
		t.Fatalf("save: %v", err)
	}
	if s.Title != "Crea un html de ejemplo" {
		t.Fatalf("título inesperado: %q", s.Title)
	}

	metas, err := st.List(project)
	if err != nil || len(metas) != 1 || metas[0].Turns != 2 {
		t.Fatalf("list inesperado: %+v err=%v", metas, err)
	}

	loaded, err := st.Load(project, s.ID)
	if err != nil || len(loaded.Messages) != 2 {
		t.Fatalf("load inesperado: %+v err=%v", loaded, err)
	}
	if loaded.Todo == nil || loaded.Todo.Revision != 1 || len(loaded.Todo.Tasks) != 1 || loaded.Todo.Tasks[0].Key != "verify" {
		t.Fatalf("todo no persistido: %+v", loaded.Todo)
	}
	if len(loaded.Compactions) != 1 || loaded.Compactions[0].Summary != "estado anterior" || len(loaded.Compactions[0].ArchivedMessages) != 1 {
		t.Fatalf("compactación no persistida: %+v", loaded.Compactions)
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

func TestLiveCheckpointRoundTripAndRevisionGuard(t *testing.T) {
	cfg := t.TempDir()
	project := t.TempDir()
	st := NewStore(cfg)

	s := New(project)
	s.Messages = []openai.Message{{Role: "user", Content: "haz una tarea"}}
	s.Transcript = []TranscriptEntry{{Kind: "user", Content: "haz una tarea"}}
	s.Revision = 1
	if err := st.Save(s); err != nil {
		t.Fatalf("save estable: %v", err)
	}

	live := &LiveCheckpoint{
		Revision:            2,
		BaseTranscriptCount: 1,
		BaseHistoryCount:    1,
		Entries: []TranscriptEntry{{
			Kind:     "thinking",
			Thinking: &ThinkingProgress{Content: "analizando..."},
		}},
		History: []openai.Message{{Role: "assistant", Content: "avance parcial"}},
		Todo:    &litodo.State{SchemaVersion: litodo.SchemaVersion, Revision: 2, Tasks: []litodo.Task{{Key: "step", Subject: "Continue", Status: litodo.InProgress}}},
	}
	if err := st.SaveLive(project, s.ID, live); err != nil {
		t.Fatalf("save live: %v", err)
	}

	loaded, err := st.Load(project, s.ID)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.Live == nil || loaded.Live.Revision != 2 || len(loaded.Live.Entries) != 1 {
		t.Fatalf("checkpoint live no recuperado: %+v", loaded.Live)
	}
	if loaded.Live.Todo == nil || loaded.Live.Todo.Revision != 2 || loaded.Live.Todo.Tasks[0].Key != "step" {
		t.Fatalf("todo live no recuperado: %+v", loaded.Live.Todo)
	}

	// Un snapshot estable más nuevo invalida cualquier sidecar atrasado, incluso
	// si una escritura asíncrona vieja intenta llegar después del clear.
	s.Revision = 3
	if err := st.Save(s); err != nil {
		t.Fatalf("save rev3: %v", err)
	}
	if err := st.ClearLive(project, s.ID, 3); err != nil {
		t.Fatalf("clear live: %v", err)
	}
	if err := st.SaveLive(project, s.ID, live); err != nil {
		t.Fatalf("stale live debería ignorarse, no fallar: %v", err)
	}
	loaded, err = st.Load(project, s.ID)
	if err != nil {
		t.Fatalf("load rev3: %v", err)
	}
	if loaded.Live != nil {
		t.Fatalf("un checkpoint rev2 no puede revivir sobre snapshot rev3: %+v", loaded.Live)
	}
}

func TestTouchIgnoresCompactionSummaryForTitle(t *testing.T) {
	s := New(t.TempDir())
	s.Messages = []openai.Message{
		{Role: "user", Content: "<conversation_summary>\nold handoff\n</conversation_summary>"},
		{Role: "user", Content: "Implementa la siguiente etapa"},
	}
	s.Touch()
	if s.Title != "Implementa la siguiente etapa" {
		t.Fatalf("summary became title: %q", s.Title)
	}
}

func TestListCountsArchivedAndLiveTurnsWithoutSummaryMarkers(t *testing.T) {
	cfg := t.TempDir()
	project := t.TempDir()
	st := NewStore(cfg)
	s := New(project)
	s.Messages = []openai.Message{
		{Role: "user", Content: "<conversation_summary>\nsummary\n</conversation_summary>"},
		{Role: "user", Content: "turno actual"},
	}
	s.Compactions = []CompactionRecord{{
		ID: "compact-1", Summary: "summary", ArchivedMessages: []openai.Message{
			{Role: "user", Content: "turno archivado 1"},
			{Role: "assistant", Content: "respuesta"},
			{Role: "user", Content: "turno archivado 2"},
		},
	}}
	s.Revision = 1
	if err := st.Save(s); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := st.SaveLive(project, s.ID, &LiveCheckpoint{
		Revision: 2, BaseHistoryCount: len(s.Messages), UpdatedAt: time.Now(),
		History: []openai.Message{{Role: "user", Content: "turno live"}},
	}); err != nil {
		t.Fatalf("save live: %v", err)
	}
	metas, err := st.List(project)
	if err != nil || len(metas) != 1 {
		t.Fatalf("list: %+v err=%v", metas, err)
	}
	if metas[0].Turns != 4 {
		t.Fatalf("turn count must include archive/current/live, got %d", metas[0].Turns)
	}
}

func TestTouchFallsBackToArchivedUserAfterFullCompaction(t *testing.T) {
	s := New(t.TempDir())
	s.Messages = []openai.Message{{Role: "user", Content: "<conversation_summary>\nhandoff\n</conversation_summary>"}}
	s.Compactions = []CompactionRecord{{ArchivedMessages: []openai.Message{{Role: "user", Content: "Corrige el compilador"}}}}
	s.Touch()
	if s.Title != "Corrige el compilador" {
		t.Fatalf("archived user title not restored: %q", s.Title)
	}
}
