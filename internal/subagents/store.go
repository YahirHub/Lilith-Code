package subagents

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lilith/li/internal/providers/openai"
)

const (
	storeDirMode  = 0o700
	storeFileMode = 0o600
)

type childSession struct {
	ID         string           `json:"id"`
	AgentName  string           `json:"agentName"`
	Project    string           `json:"project"`
	ProviderID string           `json:"providerId"`
	ModelID    string           `json:"modelId"`
	CreatedAt  time.Time        `json:"createdAt"`
	UpdatedAt  time.Time        `json:"updatedAt"`
	Messages   []openai.Message `json:"messages"`
	Tools      []string         `json:"tools,omitempty"`
}

type childStore struct{ root string }

func newChildStore(configDir, project string) *childStore {
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.ToSlash(filepath.Clean(project)))))
	base := filepath.Base(filepath.Clean(project))
	if base == "." || base == "" {
		base = "root"
	}
	return &childStore{root: filepath.Join(configDir, "projects", base+"-"+hex.EncodeToString(sum[:4]), "subagents")}
}

func (s *childStore) save(c *childSession) error {
	if c == nil || strings.TrimSpace(c.ID) == "" {
		return nil
	}
	if err := os.MkdirAll(s.root, storeDirMode); err != nil {
		return err
	}
	_ = os.Chmod(s.root, storeDirMode)
	c.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	final := filepath.Join(s.root, c.ID+".json")
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, storeFileMode); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

func (s *childStore) load(id string) (*childSession, error) {
	id = strings.TrimSpace(id)
	if id == "" || strings.ContainsAny(id, `/\\`) {
		return nil, errors.New("invalid subagent task id")
	}
	data, err := os.ReadFile(filepath.Join(s.root, id+".json"))
	if err != nil {
		return nil, err
	}
	var c childSession
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func newTaskID() string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		n := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(n >> (8 * i))
		}
	}
	return "agent-" + time.Now().Format("20060102-150405") + "-" + hex.EncodeToString(b[:])
}
