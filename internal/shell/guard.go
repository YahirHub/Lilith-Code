package shell

import (
	"fmt"
	"regexp"
	"strings"
)

// MaxInlineFileWriteCommandBytes is deliberately lower than Windows/process
// command-line limits. Provider/tool argument streaming can truncate before the
// shell sees a syntactically complete heredoc, so large inline payloads belong
// in Lilith's native write_file/append_file tools instead.
const MaxInlineFileWriteCommandBytes = 6 << 10

var (
	heredocStartPattern       = regexp.MustCompile(`<<-?\s*['"]?([A-Za-z_][A-Za-z0-9_]*)['"]?`)
	powerShellHereStringStart = regexp.MustCompile(`(?m)@(['"])\r?$`)
	inlineWriterPattern       = regexp.MustCompile(`(?i)\b(cat|printf|echo|tee|powershell|pwsh|set-content|add-content|out-file|writealltext|appendalltext|frombase64string)\b`)
)

func validateCommandSafety(command string) error {
	if err := validateRepositorySearch(command); err != nil {
		return err
	}
	starts := heredocStartPattern.FindAllStringSubmatchIndex(command, -1)
	for _, match := range starts {
		if len(match) < 4 {
			continue
		}
		delimiter := command[match[2]:match[3]]
		allowTabs := strings.HasPrefix(command[match[0]:match[1]], "<<-")
		if !hasHeredocTerminator(command[match[1]:], delimiter, allowTabs) {
			return fmt.Errorf("incomplete shell heredoc blocked before execution: delimiter %q was not found. No command was run and no file was partially written; use write_file or append_file", delimiter)
		}
	}
	for _, match := range powerShellHereStringStart.FindAllStringSubmatchIndex(command, -1) {
		if len(match) < 4 {
			continue
		}
		quote := command[match[2]:match[3]]
		if !hasPowerShellHereStringTerminator(command[match[1]:], quote) {
			return fmt.Errorf("incomplete PowerShell here-string blocked before execution: closing %s@ was not found. No command was run and no file was partially written; use write_file or append_file", quote)
		}
	}
	if len([]byte(command)) <= MaxInlineFileWriteCommandBytes {
		return nil
	}
	lower := strings.ToLower(command)
	largeHeredoc := len(starts) > 0 || powerShellHereStringStart.MatchString(command)
	largeInlineWrite := inlineWriterPattern.MatchString(command) && (strings.Contains(command, ">") || strings.Contains(lower, "set-content") || strings.Contains(lower, "add-content") || strings.Contains(lower, "out-file") || strings.Contains(lower, "writealltext") || strings.Contains(lower, "appendalltext"))
	if largeHeredoc || largeInlineWrite {
		return fmt.Errorf("inline file-writing command blocked before execution: %d bytes exceeds the safe %d-byte limit. Use write_file for complete content or append_file for bounded sections; no command was run", len([]byte(command)), MaxInlineFileWriteCommandBytes)
	}
	return nil
}

func hasHeredocTerminator(suffix, delimiter string, allowTabs bool) bool {
	// The first fragment is the remainder of the line containing `<<EOF`; a
	// valid terminator must begin on a later line by itself.
	lines := strings.Split(strings.ReplaceAll(suffix, "\r\n", "\n"), "\n")
	for _, line := range lines[1:] {
		if allowTabs {
			line = strings.TrimLeft(line, "\t")
		}
		if line == delimiter {
			return true
		}
	}
	return false
}

func hasPowerShellHereStringTerminator(suffix, quote string) bool {
	lines := strings.Split(strings.ReplaceAll(suffix, "\r\n", "\n"), "\n")
	want := quote + "@"
	for _, line := range lines[1:] {
		if line == want {
			return true
		}
	}
	return false
}
