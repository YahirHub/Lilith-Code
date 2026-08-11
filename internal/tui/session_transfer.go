package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/lilith/li/internal/codeintel"
	"github.com/lilith/li/internal/session"
)

func portableChatPath(base, raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if len(name) >= 2 {
		if (name[0] == '"' && name[len(name)-1] == '"') || (name[0] == '\'' && name[len(name)-1] == '\'') {
			name = strings.TrimSpace(name[1 : len(name)-1])
		}
	}
	if name == "" {
		return "", errors.New("indica un archivo, por ejemplo /export nombredechat.jsonl")
	}
	if filepath.Ext(name) == "" {
		name += ".jsonl"
	}
	if !strings.EqualFold(filepath.Ext(name), ".jsonl") {
		return "", errors.New("el archivo debe terminar en .jsonl")
	}
	if !filepath.IsAbs(name) {
		name = filepath.Join(base, name)
	}
	return filepath.Clean(name), nil
}

func (m *ChatModel) sessionTransferBusy() bool {
	return m == nil || m.activeTurnID != 0 || m.streaming || m.compacting
}

func (m *ChatModel) snapshotPortableSession() *session.Session {
	if m == nil || m.sess == nil {
		return nil
	}
	out := session.Clone(m.sess)
	if out == nil {
		return nil
	}
	out.Messages = cloneHistoryMessages(m.history)
	out.Transcript = m.snapshotTranscriptRange(0, len(m.messages))
	out.Todo = m.todoStatePointer()
	out.Plan = m.planStatePointer()
	out.Goal = m.goalStatePointer()
	out.Live = nil
	out.Touch()
	return out
}

func (m *ChatModel) exportConversation(rawPath string) (string, error) {
	if m == nil || m.sess == nil {
		return "", errors.New("no hay una conversación activa para exportar")
	}
	if m.sessionTransferBusy() {
		return "", errors.New("no se puede exportar durante un turno activo; termina o cancela el turno actual primero")
	}
	cwd, err := getCwd()
	if err != nil {
		return "", fmt.Errorf("obtener directorio actual: %w", err)
	}
	path, err := portableChatPath(cwd, rawPath)
	if err != nil {
		return "", err
	}
	snapshot := m.snapshotPortableSession()
	stats, err := session.ExportJSONL(path, snapshot)
	if err != nil {
		return "", fmt.Errorf("exportar conversación: %w", err)
	}
	return fmt.Sprintf("Conversación exportada: %s · %d mensajes · %d entradas de transcript · %d compactaciones.", filepath.Base(path), stats.Messages, stats.Transcript, stats.Compactions), nil
}

func (m *ChatModel) importConversation(rawPath string) (string, error) {
	if m == nil || m.store == nil {
		return "", errors.New("no hay un almacén de sesiones disponible")
	}
	if m.sessionTransferBusy() {
		return "", errors.New("no se puede importar durante un turno activo; termina o cancela el turno actual primero")
	}
	cwd, err := getCwd()
	if err != nil {
		return "", fmt.Errorf("obtener directorio actual: %w", err)
	}
	cwd = filepath.Clean(cwd)
	path, err := portableChatPath(cwd, rawPath)
	if err != nil {
		return "", err
	}
	imported, stats, err := session.ImportJSONL(path, cwd)
	if err != nil {
		return "", fmt.Errorf("importar conversación: %w", err)
	}
	if err := m.store.Save(imported); err != nil {
		return "", fmt.Errorf("guardar conversación importada: %w", err)
	}

	// La conversación portable nunca impone el proyecto del equipo de origen.
	// Reanclar runtimes dependientes del workspace garantiza que las siguientes
	// herramientas trabajen desde el cwd en el que se ejecutó /import.
	if m.mcpRuntime != nil {
		_ = m.mcpRuntime.Close()
		m.mcpRuntime = nil
		m.mcpSignature = ""
	}
	m.LoadSession(imported)
	m.project = cwd
	m.codeIntel = codeintel.New(cwd, m.ctx.ConfigDir)
	m.agentCatalog = nil
	m.clearActiveRewindPoint()

	return fmt.Sprintf("Conversación importada como una sesión nueva y vinculada al directorio actual · %d mensajes · %d entradas de transcript · %d compactaciones.", stats.Messages, stats.Transcript, stats.Compactions), nil
}
