package tools

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/lilith/li/internal/skills"
)

const (
	defaultSkillListLimit = 50
	maxSkillListLimit     = 200
	defaultSkillFileLimit = 200
	maxSkillFileLimit     = 2000
)

func init() {
	register(Definition{
		Name: "list_skills",
		Description: "List the user/project skills available to this Lilith session without reading their full SKILL.md files. " +
			"Optionally filter by name or description. Use this to discover a skill before searching or reading it.",
		PromptSnippet: "List available skills and their compact metadata",
		PromptGuidelines: []string{
			"For large skills, discover them with list_skills and use skill_search/skill_files before reading full resources.",
		},
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Optional words to match against skill name and description.",
				},
				"source": map[string]any{
					"type":        "string",
					"enum":        []string{"user", "project"},
					"description": "Optional source filter.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"maximum":     maxSkillListLimit,
					"default":     defaultSkillListLimit,
					"description": "Maximum skills to return.",
				},
			},
		},
		Run: runListSkills,
	})

	register(Definition{
		Name: "skill_search",
		Description: "Search recursively inside one skill and return only the highest-scoring matching snippets and resource paths. " +
			"This is the preferred way to navigate large references, examples, scripts, templates and assets without loading whole files. " +
			"Search is deterministic and local; binary assets can match by path/name.",
		PromptSnippet: "Search one skill and return the most relevant bounded snippets/resources",
		PromptGuidelines: []string{
			"A matching skill's SKILL.md must be loaded with skill_read before project work; skill_search is for its large secondary resources, not a substitute for the main instructions.",
			"Narrow skill_search with path, pattern, kinds or extensions when possible, then use bounded skill_read for the exact result/range you need.",
		},
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"skill": map[string]any{
					"type":        "string",
					"description": "Skill name, for example bootstrap-ui-design.",
				},
				"query": map[string]any{
					"type":        "string",
					"description": "Text or regex to search for. Results are ranked by local match relevance.",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Optional skill-relative directory/file scope, e.g. references or assets/snippets.",
				},
				"pattern": map[string]any{
					"type":        "string",
					"description": "Optional glob-like skill-relative path filter, e.g. assets/**/*.html.",
				},
				"kinds": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "string",
						"enum": []string{"instructions", "reference", "script", "asset", "example", "test", "other"},
					},
					"description": "Optional resource-kind filters.",
				},
				"extensions": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional extensions such as md, html, css or py.",
				},
				"regex": map[string]any{
					"type":        "boolean",
					"default":     false,
					"description": "Treat query as a Go regular expression instead of words.",
				},
				"case_sensitive": map[string]any{
					"type":        "boolean",
					"default":     false,
					"description": "Use case-sensitive matching.",
				},
				"include_maintenance": map[string]any{
					"type":        "boolean",
					"default":     false,
					"description": "Also search maintainer-only files such as AGENTS.md/README.md. Normally leave false.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"maximum":     24,
					"default":     8,
					"description": "Maximum ranked results to return.",
				},
				"context_lines": map[string]any{
					"type":        "integer",
					"minimum":     0,
					"maximum":     8,
					"default":     2,
					"description": "Context lines around each text match.",
				},
			},
			"required": []string{"skill", "query"},
		},
		Run: runSkillSearch,
	})

	register(Definition{
		Name: "skill_files",
		Description: "List files/resources inside one skill without reading their contents. " +
			"Filter by path, glob-like pattern, kind or extension to discover references, scripts, templates, snippets, examples and binary assets cheaply.",
		PromptSnippet: "List/filter a skill's resources without loading file contents",
		PromptGuidelines: []string{
			"Use skill_files to discover assets/scripts/references before choosing a specific file to search or read.",
		},
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"skill": map[string]any{
					"type":        "string",
					"description": "Skill name.",
				},
				"path": map[string]any{
					"type":        "string",
					"description": "Optional skill-relative directory/file scope.",
				},
				"pattern": map[string]any{
					"type":        "string",
					"description": "Optional glob-like path filter, e.g. assets/snippets/*.html.",
				},
				"kinds": map[string]any{
					"type": "array",
					"items": map[string]any{
						"type": "string",
						"enum": []string{"instructions", "reference", "script", "asset", "example", "test", "other"},
					},
					"description": "Optional resource-kind filters.",
				},
				"extensions": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Optional extension filters.",
				},
				"include_maintenance": map[string]any{
					"type":        "boolean",
					"default":     false,
					"description": "Also list maintainer-only files such as AGENTS.md/README.md.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"maximum":     maxSkillFileLimit,
					"default":     defaultSkillFileLimit,
					"description": "Maximum resources to return.",
				},
			},
			"required": []string{"skill"},
		},
		Run: runSkillFiles,
	})

	register(Definition{
		Name: "skill_read",
		Description: "Load a skill's instructions or read a bounded line range from one of its resources. Path defaults to SKILL.md. " +
			"If the current task matches an available skill description, call skill_read for that skill's SKILL.md before inspecting the project, editing files, running commands, or answering; if more SKILL.md lines are reported, keep reading until the main skill is complete. " +
			"For large secondary references/scripts/assets, use skill_search first and then bounded offset/limit reads. Binary assets return metadata only.",
		PromptSnippet: "Read a bounded line range from a selected skill resource",
		PromptGuidelines: []string{
			"When an available skill clearly matches the user's task, load its complete SKILL.md first with skill_read; if the result says more lines are available, continue until the main file is complete before substantive project actions.",
			"For secondary references and scripts, read only the range needed and use skill_search first when the resource is large.",
		},
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"skill": map[string]any{
					"type":        "string",
					"description": "Skill name.",
				},
				"path": map[string]any{
					"type":        "string",
					"default":     "SKILL.md",
					"description": "Skill-relative resource path.",
				},
				"offset": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"default":     1,
					"description": "1-based first line to return.",
				},
				"limit": map[string]any{
					"type":        "integer",
					"minimum":     1,
					"maximum":     500,
					"default":     120,
					"description": "Maximum lines to return.",
				},
			},
			"required": []string{"skill"},
		},
		Run: runSkillRead,
	})
}

func runListSkills(_ context.Context, args map[string]any, env Env) (string, error) {
	query := normalizeSkillLookup(str(args, "query"))
	source := strings.ToLower(strings.TrimSpace(str(args, "source")))
	limit := intArg(args, "limit", defaultSkillListLimit)
	if limit > maxSkillListLimit {
		limit = maxSkillListLimit
	}
	if limit < 1 {
		limit = 1
	}

	type rankedSkill struct {
		skill skills.Skill
		score int
	}
	ranked := make([]rankedSkill, 0, len(env.Skills))
	for _, sk := range env.Skills {
		if source != "" && !strings.EqualFold(sk.Source, source) {
			continue
		}
		score := 1
		if query != "" {
			name := normalizeSkillLookup(sk.Name)
			desc := normalizeSkillLookup(sk.Description)
			score = 0
			if name == query {
				score += 200
			}
			if strings.Contains(name, query) {
				score += 100
			}
			if strings.Contains(desc, query) {
				score += 60
			}
			matched := 0
			for _, term := range strings.Fields(query) {
				if strings.Contains(name, term) {
					score += 25
					matched++
				} else if strings.Contains(desc, term) {
					score += 12
					matched++
				}
			}
			if matched == 0 {
				continue
			}
		}
		ranked = append(ranked, rankedSkill{skill: sk, score: score})
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].skill.Name < ranked[j].skill.Name
	})
	truncated := len(ranked) > limit
	if truncated {
		ranked = ranked[:limit]
	}

	var b strings.Builder
	fmt.Fprintf(&b, "skills: %d\n", len(ranked))
	for _, item := range ranked {
		fmt.Fprintf(&b, "- %s [%s]: %s\n", item.skill.Name, item.skill.Source, oneLine(item.skill.Description))
	}
	if truncated {
		b.WriteString("note: results truncated; narrow query/source or raise limit\n")
	}
	if len(ranked) == 0 {
		b.WriteString("no matching skills\n")
	}
	return b.String(), nil
}

func runSkillSearch(_ context.Context, args map[string]any, env Env) (string, error) {
	sk, err := requireSkill(env, str(args, "skill"))
	if err != nil {
		return "", err
	}
	query := strings.TrimSpace(str(args, "query"))
	if query == "" {
		return "", errors.New("query is required")
	}
	result, err := skills.Search(*sk, skills.SearchOptions{
		Query:              query,
		Path:               str(args, "path"),
		Pattern:            str(args, "pattern"),
		Kinds:              resourceKinds(strSlice(args, "kinds")),
		Extensions:         strSlice(args, "extensions"),
		Regex:              boolArg(args, "regex"),
		CaseSensitive:      boolArg(args, "case_sensitive"),
		Limit:              intArg(args, "limit", 8),
		ContextLines:       intArgAllowZero(args, "context_lines", 2),
		IncludeMaintenance: boolArg(args, "include_maintenance"),
	})
	if err != nil {
		return "", err
	}
	return skills.FormatSearchResult(*sk, query, result), nil
}

func runSkillFiles(_ context.Context, args map[string]any, env Env) (string, error) {
	sk, err := requireSkill(env, str(args, "skill"))
	if err != nil {
		return "", err
	}
	files, truncated, err := skills.ListFiles(*sk, skills.ListFilesOptions{
		Path:               str(args, "path"),
		Pattern:            str(args, "pattern"),
		Kinds:              resourceKinds(strSlice(args, "kinds")),
		Extensions:         strSlice(args, "extensions"),
		Limit:              intArg(args, "limit", defaultSkillFileLimit),
		IncludeMaintenance: boolArg(args, "include_maintenance"),
	})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "skill: %s\nfiles: %d\n", sk.Name, len(files))
	for _, f := range files {
		mode := "text"
		if !f.Text {
			mode = "binary"
		}
		line := fmt.Sprintf("- %s [%s] %s %s\n", f.Path, f.Kind, humanBytes(f.Size), mode)
		if b.Len()+len(line) > 64*1024 {
			truncated = true
			break
		}
		b.WriteString(line)
	}
	if truncated {
		b.WriteString("note: results truncated; narrow path/pattern/kinds/extensions or raise limit\n")
	}
	if len(files) == 0 {
		b.WriteString("no matching resources\n")
	}
	return b.String(), nil
}

func runSkillRead(_ context.Context, args map[string]any, env Env) (string, error) {
	sk, err := requireSkill(env, str(args, "skill"))
	if err != nil {
		return "", err
	}
	path := str(args, "path")
	limit := intArg(args, "limit", 120)
	if _, explicitlyLimited := args["limit"]; !explicitlyLimited && (strings.TrimSpace(path) == "" || strings.EqualFold(filepath.ToSlash(strings.TrimSpace(path)), "SKILL.md")) {
		// SKILL.md is the mandatory entry point. Read as much of the main
		// instructions as the safety cap permits in one call; secondary resources
		// keep the smaller default to preserve progressive disclosure.
		limit = 500
	}
	res, err := skills.ReadFile(*sk, path, intArg(args, "offset", 1), limit)
	if err != nil {
		return "", err
	}
	if res.Binary {
		return fmt.Sprintf("== skill:%s/%s ==\n[%s binary %s]\nBinary contents are not inlined. Use skill_files/skill_search to locate assets and reference the exact path when needed.\n",
			sk.Name, res.Path, res.Kind, humanBytes(res.Size)), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "== skill:%s/%s ==\n", sk.Name, res.Path)
	if res.Total == 0 {
		fmt.Fprintf(&b, "[%s empty file]\n", res.Kind)
		return b.String(), nil
	}
	if res.EndLine == 0 {
		fmt.Fprintf(&b, "[%s offset %d is beyond end of file (%d lines)]\n", res.Kind, res.Offset, res.Total)
		return b.String(), nil
	}
	fmt.Fprintf(&b, "[%s lines %d-%d of %d]\n", res.Kind, res.Offset, res.EndLine, res.Total)
	b.WriteString(res.Content)
	if res.Content != "" && !strings.HasSuffix(res.Content, "\n") {
		b.WriteByte('\n')
	}
	if res.ByteTruncated {
		b.WriteString("[output truncated by safety cap; use skill_search or a narrower line range]\n")
	}
	if res.Truncated && !res.ByteTruncated {
		fmt.Fprintf(&b, "[more available: call skill_read with offset=%d]\n", res.EndLine+1)
	}
	return b.String(), nil
}

func requireSkill(env Env, name string) (*skills.Skill, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("skill is required")
	}
	if sk := skills.Find(env.Skills, name); sk != nil {
		return sk, nil
	}
	if len(env.Skills) == 0 {
		return nil, fmt.Errorf("skill %q is not available in this session; skills may be disabled or no skill paths were discovered", name)
	}
	available := make([]string, 0, minInt(len(env.Skills), 12))
	for _, sk := range env.Skills {
		available = append(available, sk.Name)
		if len(available) == 12 {
			break
		}
	}
	return nil, fmt.Errorf("skill %q not found; available: %s", name, strings.Join(available, ", "))
}

func resourceKinds(raw []string) []skills.ResourceKind {
	out := make([]skills.ResourceKind, 0, len(raw))
	seen := map[skills.ResourceKind]bool{}
	for _, item := range raw {
		kind := skills.ResourceKind(strings.ToLower(strings.TrimSpace(item)))
		switch kind {
		case skills.KindInstructions, skills.KindReference, skills.KindScript, skills.KindAsset, skills.KindExample, skills.KindTest, skills.KindOther:
			if !seen[kind] {
				seen[kind] = true
				out = append(out, kind)
			}
		}
	}
	return out
}

func intArgAllowZero(args map[string]any, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case float64:
		if t >= 0 {
			return int(t)
		}
	case int:
		if t >= 0 {
			return t
		}
	}
	return def
}

func normalizeSkillLookup(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) <= 180 {
		return s
	}
	r := []rune(s)
	return string(r[:177]) + "..."
}

func humanBytes(n int64) string {
	if n < 1024 {
		return strconv.FormatInt(n, 10) + "B"
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1fKiB", float64(n)/1024)
	}
	if n < 1024*1024*1024 {
		return fmt.Sprintf("%.1fMiB", float64(n)/(1024*1024))
	}
	return fmt.Sprintf("%.1fGiB", float64(n)/(1024*1024*1024))
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
