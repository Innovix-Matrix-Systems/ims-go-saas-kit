package scaffold

import "strings"

// mcpHint starts the commented-out x-mcp mark the generator puts on a list
// operation: uncomment it to make the operation an MCP tool. Regeneration
// keeps an uncommented mark (see keepMCPMarks).
const mcpHint = "# x-mcp: "

// mcpMarks returns the x-mcp lines inside the generated paths region, keyed
// by the operationId above them.
func mcpMarks(s string) map[string]string {
	marks := map[string]string{}
	start := strings.Index(s, markerPathsBegin)
	end := strings.Index(s, markerPathsEnd)
	if start < 0 || end < start {
		return marks
	}
	operation := ""
	for _, line := range strings.Split(s[start:end], "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "operationId:"):
			operation = strings.TrimSpace(strings.TrimPrefix(trimmed, "operationId:"))
		case strings.HasPrefix(trimmed, "x-mcp:") && operation != "":
			marks[operation] = line
		}
	}
	return marks
}

// keepMCPMarks puts kept x-mcp lines back after their operationId, in place
// of the generator's commented hint.
func keepMCPMarks(paths string, marks map[string]string) string {
	if len(marks) == 0 {
		return paths
	}
	lines := strings.Split(paths, "\n")
	out := make([]string, 0, len(lines)+len(marks))
	replacing := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if replacing && strings.HasPrefix(trimmed, mcpHint) {
			replacing = false
			continue
		}
		replacing = false
		out = append(out, line)
		if strings.HasPrefix(trimmed, "operationId:") {
			if mark, ok := marks[strings.TrimSpace(strings.TrimPrefix(trimmed, "operationId:"))]; ok {
				out = append(out, mark)
				replacing = true
			}
		}
	}
	return strings.Join(out, "\n")
}

// snakeCase turns a tag (Products, OrderItems) into a tool name part
// (products, order_items).
func snakeCase(tag string) string {
	var b strings.Builder
	for i, r := range tag {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				b.WriteByte('_')
			}
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return b.String()
}
