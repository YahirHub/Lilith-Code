package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	ligoal "github.com/lilith/li/internal/goal"
	planstate "github.com/lilith/li/internal/plan"
	"github.com/lilith/li/internal/providers/openai"
	litodo "github.com/lilith/li/internal/todo"
)

const (
	PortableFormat  = "lilith-chat-jsonl"
	PortableVersion = 1
)

// PortableStats summarizes the conversation material written to/read from a
// portable JSONL export. It intentionally contains no project path metadata.
type PortableStats struct {
	Messages    int
	Transcript  int
	Compactions int
}

type portableMeta struct {
	Type       string    `json:"type"`
	Format     string    `json:"format"`
	Version    int       `json:"version"`
	Title      string    `json:"title,omitempty"`
	CreatedAt  time.Time `json:"createdAt,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt,omitempty"`
	ExportedAt time.Time `json:"exportedAt"`
}

type portableState struct {
	Type string           `json:"type"`
	Todo *litodo.State    `json:"todo,omitempty"`
	Plan *planstate.State `json:"plan,omitempty"`
	Goal *ligoal.State    `json:"goal,omitempty"`
}

type portableRecord struct {
	Type       string            `json:"type"`
	Format     string            `json:"format,omitempty"`
	Version    int               `json:"version,omitempty"`
	Title      string            `json:"title,omitempty"`
	CreatedAt  time.Time         `json:"createdAt,omitempty"`
	UpdatedAt  time.Time         `json:"updatedAt,omitempty"`
	ExportedAt time.Time         `json:"exportedAt,omitempty"`
	Message    *openai.Message   `json:"message,omitempty"`
	Transcript *TranscriptEntry  `json:"transcript,omitempty"`
	Compaction *CompactionRecord `json:"compaction,omitempty"`
	Todo       *litodo.State     `json:"todo,omitempty"`
	Plan       *planstate.State  `json:"plan,omitempty"`
	Goal       *ligoal.State     `json:"goal,omitempty"`
}

// ExportJSONL writes one portable conversation. ProjectPath, session IDs,
// rewind/fork workspace provenance and live sidecars are deliberately omitted:
// the import destination becomes the project binding on the receiving PC.
func ExportJSONL(path string, source *Session) (PortableStats, error) {
	var stats PortableStats
	if source == nil {
		return stats, errors.New("sesión vacía")
	}
	if len(source.Messages) == 0 {
		return stats, errors.New("la conversación no contiene mensajes para exportar")
	}
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return stats, errors.New("ruta de exportación vacía")
	}
	if !strings.EqualFold(filepath.Ext(path), ".jsonl") {
		return stats, errors.New("el archivo de exportación debe terminar en .jsonl")
	}
	parent := filepath.Dir(path)
	if info, err := os.Stat(parent); err != nil {
		return stats, fmt.Errorf("directorio de destino no disponible: %w", err)
	} else if !info.IsDir() {
		return stats, errors.New("el destino padre no es un directorio")
	}

	tmp, err := os.CreateTemp(parent, ".lilith-chat-export-*.tmp")
	if err != nil {
		return stats, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(fileMode); err != nil {
		_ = tmp.Close()
		return stats, err
	}

	writer := bufio.NewWriterSize(tmp, 64*1024)
	enc := json.NewEncoder(writer)
	write := func(value any) error { return enc.Encode(value) }

	meta := portableMeta{
		Type:       "meta",
		Format:     PortableFormat,
		Version:    PortableVersion,
		Title:      source.Title,
		CreatedAt:  source.CreatedAt,
		UpdatedAt:  source.UpdatedAt,
		ExportedAt: time.Now(),
	}
	if err := write(meta); err != nil {
		_ = tmp.Close()
		return stats, err
	}
	if source.Todo != nil || source.Plan != nil || source.Goal != nil {
		if err := write(portableState{Type: "state", Todo: source.Todo, Plan: source.Plan, Goal: source.Goal}); err != nil {
			_ = tmp.Close()
			return stats, err
		}
	}
	messages := openai.SanitizeMessages(source.Messages)
	for i := range messages {
		record := portableRecord{Type: "message", Message: &messages[i]}
		if err := write(record); err != nil {
			_ = tmp.Close()
			return stats, err
		}
		stats.Messages++
	}
	for i := range source.Compactions {
		compaction := source.Compactions[i]
		compaction.ArchivedMessages = openai.SanitizeMessages(compaction.ArchivedMessages)
		record := portableRecord{Type: "compaction", Compaction: &compaction}
		if err := write(record); err != nil {
			_ = tmp.Close()
			return stats, err
		}
		stats.Compactions++
	}
	for i := range source.Transcript {
		record := portableRecord{Type: "transcript", Transcript: &source.Transcript[i]}
		if err := write(record); err != nil {
			_ = tmp.Close()
			return stats, err
		}
		stats.Transcript++
	}
	if err := writer.Flush(); err != nil {
		_ = tmp.Close()
		return stats, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return stats, err
	}
	if err := tmp.Close(); err != nil {
		return stats, err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		// Windows does not replace an existing destination with os.Rename.
		// /export is explicit, so replacing the named backup is intentional.
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return stats, err
		}
		if err = os.Rename(tmpPath, path); err != nil {
			return stats, err
		}
	}
	_ = os.Chmod(path, fileMode)
	return stats, nil
}

// ImportJSONL reads a portable conversation and binds it to projectPath. A new
// local session ID is always generated so importing never overwrites an existing
// chat, even when the same export is imported twice on the same machine.
func ImportJSONL(path, projectPath string) (*Session, PortableStats, error) {
	var stats PortableStats
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return nil, stats, errors.New("ruta de importación vacía")
	}
	if !strings.EqualFold(filepath.Ext(path), ".jsonl") {
		return nil, stats, errors.New("el archivo de importación debe terminar en .jsonl")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, stats, err
	}
	if !info.Mode().IsRegular() {
		return nil, stats, errors.New("el archivo de importación debe ser un archivo regular")
	}

	f, err := os.Open(path)
	if err != nil {
		return nil, stats, err
	}
	defer f.Close()

	decoder := json.NewDecoder(bufio.NewReaderSize(f, 64*1024))
	var meta *portableMeta
	var messages []openai.Message
	var transcript []TranscriptEntry
	var compactions []CompactionRecord
	var todo *litodo.State
	var plan *planstate.State
	var goal *ligoal.State
	stateSeen := false
	recordIndex := 0

	for {
		var record portableRecord
		err := decoder.Decode(&record)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, stats, fmt.Errorf("JSONL inválido en registro %d: %w", recordIndex+1, err)
		}
		recordIndex++
		typ := strings.ToLower(strings.TrimSpace(record.Type))
		if recordIndex == 1 && typ != "meta" {
			return nil, stats, errors.New("el primer registro del JSONL debe ser meta")
		}
		switch typ {
		case "meta":
			if meta != nil {
				return nil, stats, errors.New("el JSONL contiene más de un registro meta")
			}
			if record.Format != PortableFormat {
				return nil, stats, fmt.Errorf("formato de chat no compatible: %q", record.Format)
			}
			if record.Version != PortableVersion {
				return nil, stats, fmt.Errorf("versión de chat no compatible: %d", record.Version)
			}
			meta = &portableMeta{
				Type: record.Type, Format: record.Format, Version: record.Version,
				Title: record.Title, CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt, ExportedAt: record.ExportedAt,
			}
		case "state":
			if stateSeen {
				return nil, stats, errors.New("el JSONL contiene más de un registro state")
			}
			stateSeen = true
			todo, plan, goal = record.Todo, record.Plan, record.Goal
		case "message":
			if record.Message == nil {
				return nil, stats, fmt.Errorf("registro message %d vacío", recordIndex)
			}
			messages = append(messages, openai.SanitizeMessages([]openai.Message{*record.Message})...)
			stats.Messages++
		case "compaction":
			if record.Compaction == nil {
				return nil, stats, fmt.Errorf("registro compaction %d vacío", recordIndex)
			}
			compaction := *record.Compaction
			compaction.ArchivedMessages = openai.SanitizeMessages(compaction.ArchivedMessages)
			compactions = append(compactions, compaction)
			stats.Compactions++
		case "transcript":
			if record.Transcript == nil {
				return nil, stats, fmt.Errorf("registro transcript %d vacío", recordIndex)
			}
			transcript = append(transcript, *record.Transcript)
			stats.Transcript++
		default:
			return nil, stats, fmt.Errorf("tipo de registro JSONL no compatible: %q", record.Type)
		}
	}
	if meta == nil {
		return nil, stats, errors.New("el archivo no contiene metadatos de Lilith")
	}
	if len(messages) == 0 {
		return nil, stats, errors.New("el archivo no contiene mensajes de conversación")
	}
	projectPath = filepath.Clean(strings.TrimSpace(projectPath))
	if projectPath == "" || projectPath == "." {
		return nil, stats, errors.New("directorio actual de importación inválido")
	}

	out := New(projectPath)
	out.Title = strings.TrimSpace(meta.Title)
	if out.Title == "" {
		out.Title = "Sin título"
	}
	if !meta.CreatedAt.IsZero() {
		out.CreatedAt = meta.CreatedAt
	}
	out.UpdatedAt = time.Now()
	out.Messages = messages
	out.Transcript = transcript
	out.Compactions = compactions
	out.Todo = todo
	out.Plan = plan
	out.Goal = goal
	out.Revision = 0
	out.Live = nil
	out.ForkedFrom = nil
	return out, stats, nil
}
