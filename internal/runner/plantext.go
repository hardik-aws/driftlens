package runner

import "strings"

// parsePlanDetails splits human-readable `terraform show <planfile>` output into
// per-resource diff blocks keyed by resource address. A block starts at a
// "# <address> will be ..." / "# <address> must be ..." header and runs until
// the next header or a top-level (unindented, non-blank) line such as the
// trailing "Plan: ..." summary.
func parsePlanDetails(text string) map[string]string {
	res := map[string]string{}
	var addr string
	var buf []string

	flush := func() {
		if addr != "" {
			res[addr] = strings.TrimRight(strings.Join(buf, "\n"), "\n")
		}
		addr, buf = "", nil
	}

	for _, ln := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(ln)
		if isResourceHeader(trimmed) {
			flush()
			addr = strings.Fields(trimmed)[1]
			buf = append(buf, ln)
			continue
		}
		if addr == "" {
			continue
		}
		// a non-blank line with no leading whitespace ends the current block
		if ln != "" && !strings.HasPrefix(ln, " ") {
			flush()
			continue
		}
		buf = append(buf, ln)
	}
	flush()
	return res
}

// isResourceHeader reports whether a trimmed line begins a resource diff block.
// It must start with "# " and announce an action, distinguishing real headers
// from inline comments like "# (14 unchanged attributes hidden)".
func isResourceHeader(trimmed string) bool {
	if !strings.HasPrefix(trimmed, "# ") {
		return false
	}
	return strings.Contains(trimmed, " will be ") || strings.Contains(trimmed, " must be ")
}
