package translate

import "strings"

// segment is a piece of a Markdown body. Code is left exactly as written:
// `#42` in a code span is a string in someone's shell, not an issue.
type segment struct {
	text string
	code bool
}

// split cuts a Markdown body into prose and code: fenced code blocks and
// inline code spans. It is not a Markdown parser and does not need to be; the
// cost of a miss is a reference rewritten inside something that looked like
// code to a person, never a lost character.
func split(body string) []segment {
	var out []segment
	var prose strings.Builder
	flush := func() {
		if prose.Len() > 0 {
			out = append(out, segment{text: prose.String()})
			prose.Reset()
		}
	}

	lines := strings.SplitAfter(body, "\n")
	for i := 0; i < len(lines); i++ {
		fence, ok := openingFence(lines[i])
		if !ok {
			prose.WriteString(lines[i])
			continue
		}
		flush()
		var block strings.Builder
		block.WriteString(lines[i])
		for i++; i < len(lines); i++ {
			block.WriteString(lines[i])
			if closesFence(lines[i], fence) {
				break
			}
		}
		out = append(out, segment{text: block.String(), code: true})
	}
	flush()

	// Inline code spans, within the prose that is left.
	var final []segment
	for _, s := range out {
		if s.code {
			final = append(final, s)
			continue
		}
		final = append(final, splitSpans(s.text)...)
	}
	return final
}

// openingFence reports whether a line opens a fenced code block, and returns
// the fence: its character and length.
func openingFence(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 {
		return "", false
	}
	for _, ch := range []string{"`", "~"} {
		n := 0
		for n < len(trimmed) && trimmed[n] == ch[0] {
			n++
		}
		if n >= 3 {
			// A backtick fence's info string may not contain a backtick;
			// ``` foo ` bar is an inline span, not a fence.
			if ch == "`" && strings.Contains(trimmed[n:], "`") {
				return "", false
			}
			return trimmed[:n], true
		}
	}
	return "", false
}

func closesFence(line, fence string) bool {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 {
		return false
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == fence[0] {
		n++
	}
	return n >= len(fence) && strings.TrimSpace(trimmed[n:]) == ""
}

// splitSpans separates inline code spans: a run of n backticks up to the next
// run of exactly n. An unmatched run is literal text.
func splitSpans(text string) []segment {
	var out []segment
	start := 0
	for i := 0; i < len(text); {
		if text[i] != '`' {
			i++
			continue
		}
		n := run(text, i)
		end := findRun(text, i+n, n)
		if end < 0 {
			i += n
			continue
		}
		if i > start {
			out = append(out, segment{text: text[start:i]})
		}
		out = append(out, segment{text: text[i : end+n], code: true})
		i = end + n
		start = i
	}
	if start < len(text) {
		out = append(out, segment{text: text[start:]})
	}
	return out
}

func run(s string, i int) int {
	n := 0
	for i+n < len(s) && s[i+n] == '`' {
		n++
	}
	return n
}

func findRun(s string, from, n int) int {
	for i := from; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		m := run(s, i)
		if m == n {
			return i
		}
		i += m
	}
	return -1
}

// mapProse applies f to every prose segment and reassembles the body.
func mapProse(body string, f func(string) string) string {
	var b strings.Builder
	for _, s := range split(body) {
		if s.code {
			b.WriteString(s.text)
		} else {
			b.WriteString(f(s.text))
		}
	}
	return b.String()
}
