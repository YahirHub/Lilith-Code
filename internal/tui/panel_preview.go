package tui

// cappedTailPreview keeps a compact panel as short as its content while
// enforcing its historical preview limit. When content exceeds the limit,
// one row is reserved for the "hidden lines" hint and the remaining rows show
// the newest content.
func cappedTailPreview(lines []string, maxLines int) (visible []string, hidden int) {
	if maxLines <= 0 || len(lines) <= maxLines {
		return lines, 0
	}
	if maxLines == 1 {
		return nil, len(lines)
	}
	visibleCount := maxLines - 1
	hidden = len(lines) - visibleCount
	return lines[len(lines)-visibleCount:], hidden
}
