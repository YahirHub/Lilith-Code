package shell

import (
	"fmt"
	"path/filepath"
	"strings"
)

type shellToken struct {
	value string
}

// IsRepositorySearchCommand reports whether command starts with a repository
// inspection command. The terminal tool uses this to apply a short safety
// deadline without changing the unlimited default for builds, tests or installs.
func IsRepositorySearchCommand(command string) bool {
	segment := leadingCommandSegment(command)
	tokens, ok := tokenizeSimpleShellCommand(segment)
	if !ok || len(tokens) == 0 {
		return false
	}
	idx := commandTokenIndex(tokens)
	if idx < 0 {
		return false
	}
	name := commandBase(tokens[idx].value)
	switch name {
	case "grep":
		recursive, _, _ := analyzeGrepArgs(tokens[idx+1:])
		return recursive
	case "rg", "ripgrep", "find", "fd", "fdfind":
		return true
	case "git":
		return idx+1 < len(tokens) && strings.EqualFold(tokens[idx+1].value, "grep")
	default:
		return false
	}
}

// validateRepositorySearch rejects the common model-generated command
// `grep -rn "pattern"` when it has no explicit target. GNU grep treats that as
// the current directory and may walk metadata, dependencies and build output;
// other grep implementations differ. Lilith already has code_search for this
// job, so failing before process creation is safer and portable.
func validateRepositorySearch(command string) error {
	tokens, ok := tokenizeSimpleShellCommand(command)
	if !ok || len(tokens) == 0 {
		return nil
	}
	idx := commandTokenIndex(tokens)
	if idx < 0 || commandBase(tokens[idx].value) != "grep" {
		return nil
	}
	recursive, hasPattern, fileOperands := analyzeGrepArgs(tokens[idx+1:])
	if !recursive || !hasPattern || fileOperands > 0 {
		return nil
	}
	return fmt.Errorf("recursive grep without an explicit path was blocked before execution: use code_search for repository content, or pass a concrete file/directory and exclusions; no command was run")
}

func leadingCommandSegment(command string) string {
	quote := byte(0)
	escaped := false
	for i := 0; i < len(command); i++ {
		c := command[i]
		if escaped {
			escaped = false
			continue
		}
		if quote == 0 {
			switch c {
			case '\\':
				escaped = true
			case '\'', '"':
				quote = c
			case ';', '&', '|', '<', '>', '\r', '\n':
				return strings.TrimSpace(command[:i])
			}
			continue
		}
		if c == quote {
			quote = 0
			continue
		}
		if quote == '"' && c == '\\' {
			escaped = true
		}
	}
	return strings.TrimSpace(command)
}

func commandTokenIndex(tokens []shellToken) int {
	if len(tokens) == 0 {
		return -1
	}
	if strings.EqualFold(tokens[0].value, "command") {
		if len(tokens) < 2 {
			return -1
		}
		return 1
	}
	return 0
}

func commandBase(value string) string {
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(value, "\\", "/")))
	return strings.TrimSuffix(base, ".exe")
}

func analyzeGrepArgs(args []shellToken) (recursive, hasPattern bool, fileOperands int) {
	options := true
	consumeValue := ""

	for _, token := range args {
		value := token.value
		if consumeValue != "" {
			if consumeValue == "pattern" {
				hasPattern = true
			}
			consumeValue = ""
			continue
		}
		if options && value == "--" {
			options = false
			continue
		}
		if options && strings.HasPrefix(value, "--") {
			name, _, hasAttached := strings.Cut(value, "=")
			switch name {
			case "--recursive":
				recursive = true
			case "--regexp", "--file":
				if hasAttached {
					hasPattern = true
				} else {
					consumeValue = "pattern"
				}
			case "--after-context", "--before-context", "--context", "--binary-files", "--devices", "--directories", "--exclude", "--exclude-dir", "--include", "--label", "--max-count":
				if !hasAttached {
					consumeValue = "option"
				}
			}
			continue
		}
		if options && strings.HasPrefix(value, "-") && value != "-" {
			short := strings.TrimPrefix(value, "-")
			for i := 0; i < len(short); i++ {
				switch short[i] {
				case 'r', 'R':
					recursive = true
				case 'e', 'f':
					if i+1 < len(short) {
						hasPattern = true
					} else {
						consumeValue = "pattern"
					}
					i = len(short)
				case 'A', 'B', 'C', 'D', 'd', 'm':
					if i+1 == len(short) {
						consumeValue = "option"
					}
					i = len(short)
				}
			}
			continue
		}

		if !hasPattern {
			hasPattern = true
			continue
		}
		fileOperands++
	}
	return recursive, hasPattern, fileOperands
}

// tokenizeSimpleShellCommand tokenizes one command only. Operators, redirects,
// newlines and unmatched quotes deliberately make the command ineligible for
// the pre-execution grep guard; complex shell programs preserve their exact text
// and remain covered by the repository-search timeout.
func tokenizeSimpleShellCommand(command string) ([]shellToken, bool) {
	var tokens []shellToken
	for i := 0; i < len(command); {
		for i < len(command) && (command[i] == ' ' || command[i] == '\t') {
			i++
		}
		if i >= len(command) {
			break
		}
		if strings.ContainsRune(";&|<>\r\n", rune(command[i])) {
			return nil, false
		}
		var value strings.Builder
		quote := byte(0)
		for i < len(command) {
			c := command[i]
			if quote == 0 {
				switch c {
				case ' ', '\t':
					goto tokenDone
				case ';', '&', '|', '<', '>', '\r', '\n':
					return nil, false
				case '\'', '"':
					quote = c
					i++
					continue
				case '\\':
					if i+1 >= len(command) {
						return nil, false
					}
					i++
					value.WriteByte(command[i])
					i++
					continue
				default:
					value.WriteByte(c)
					i++
					continue
				}
			}
			if c == quote {
				quote = 0
				i++
				continue
			}
			if quote == '"' && c == '\\' && i+1 < len(command) {
				i++
				value.WriteByte(command[i])
				i++
				continue
			}
			value.WriteByte(c)
			i++
		}
		if quote != 0 {
			return nil, false
		}
	tokenDone:
		tokens = append(tokens, shellToken{value: value.String()})
	}
	return tokens, true
}
