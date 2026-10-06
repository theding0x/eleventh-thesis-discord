// Package content turns Markdown files into Discord messages.
package content

import (
	"strings"
	"unicode/utf8"
)

// Limit is the target size of one message in Unicode code points. Discord's
// own limit is 2,000; the margin keeps edits and mentions safe.
const Limit = 1900

const fenceMarker = "```"

// Chunk splits md into messages of at most limit code points each.
//
// Fenced code blocks and tables are never split unless they alone exceed the
// limit; tables are wrapped in code fences because Discord does not render
// Markdown tables. A heading of any level never ends a chunk. Every chunk is
// trimmed and non-empty. The output depends only on the input, so an
// unchanged file yields identical chunks. An empty input yields an empty,
// non-nil slice.
func Chunk(md string, limit int) []string {
	if limit <= 0 {
		limit = Limit
	}
	out := []string{}
	var cur string
	flush := func() {
		if t := strings.TrimSpace(cur); t != "" {
			out = append(out, t)
		}
		cur = ""
	}
	for _, u := range buildUnits(parseBlocks(md), limit) {
		if cur == "" {
			cur = u
			continue
		}
		if runes(cur)+2+runes(u) <= limit {
			cur += "\n\n" + u
			continue
		}
		flush()
		cur = u
	}
	flush()
	return out
}

func runes(s string) int { return utf8.RuneCountInString(s) }

// fence is a fenced block: an opening line, body lines and, when present, a
// closing line. Tables are represented as fences with a plain "```" opener.
type fence struct {
	open   string
	body   []string
	closed bool
}

// block is a paragraph or a fence.
type block struct {
	text    string
	fence   *fence // nil for paragraphs
	heading bool   // paragraph whose last line is a heading
}

func wrap(open string, body []string, closed bool) string {
	parts := make([]string, 0, len(body)+2)
	parts = append(parts, open)
	parts = append(parts, body...)
	if closed {
		parts = append(parts, fenceMarker)
	}
	return strings.Join(parts, "\n")
}

func isFenceLine(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " \t"), fenceMarker)
}

// isTableLine reports whether line belongs to a table. Tables nested in list
// items are indented, and the chunker trims chunks, so indentation is ignored.
func isTableLine(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " \t"), "|")
}

func isBlank(line string) bool { return strings.TrimSpace(line) == "" }

// isHeading reports whether line matches ^#{1,6} .
func isHeading(line string) bool {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	return n >= 1 && n <= 6 && n < len(line) && line[n] == ' '
}

func parseBlocks(md string) []block {
	lines := strings.Split(md, "\n")
	var blocks []block
	for i := 0; i < len(lines); {
		line := lines[i]
		switch {
		case isBlank(line):
			i++
		case isFenceLine(line):
			f := &fence{open: strings.TrimRight(line, "\r")}
			i++
			for i < len(lines) {
				if isFenceLine(lines[i]) {
					f.closed = true
					i++
					break
				}
				f.body = append(f.body, lines[i])
				i++
			}
			blocks = append(blocks, block{text: wrap(f.open, f.body, f.closed), fence: f})
		case isTableLine(line):
			f := &fence{open: fenceMarker, closed: true}
			for i < len(lines) && isTableLine(lines[i]) {
				// Dedent: the fence is flush left, so the rows stay aligned.
				f.body = append(f.body, strings.TrimLeft(lines[i], " \t"))
				i++
			}
			blocks = append(blocks, block{text: wrap(f.open, f.body, true), fence: f})
		default:
			start := i
			for i < len(lines) && !isBlank(lines[i]) && !isFenceLine(lines[i]) && !isTableLine(lines[i]) {
				i++
			}
			para := lines[start:i]
			blocks = append(blocks, block{
				text:    strings.Join(para, "\n"),
				heading: isHeading(para[len(para)-1]),
			})
		}
	}
	return blocks
}

// buildUnits glues heading paragraphs to the block that follows them and
// splits any unit that is over the limit. Every returned unit fits.
func buildUnits(blocks []block, limit int) []string {
	var units []string
	var pending []block // heading paragraphs waiting for a body
	emit := func(prefix []block, body *block) {
		units = append(units, splitUnit(prefix, body, limit)...)
	}
	for i := range blocks {
		b := blocks[i]
		if b.heading {
			pending = append(pending, b)
			continue
		}
		emit(pending, &b)
		pending = nil
	}
	if len(pending) > 0 { // headings at the very end: nothing to glue to
		emit(pending[:len(pending)-1], &pending[len(pending)-1])
	}
	return units
}

func joinTexts(prefix []block) string {
	texts := make([]string, len(prefix))
	for i, p := range prefix {
		texts[i] = p.text
	}
	return strings.Join(texts, "\n\n")
}

// splitUnit returns the unit (prefix headings + body) as one string if it
// fits, and otherwise as pieces that each fit.
func splitUnit(prefix []block, body *block, limit int) []string {
	prefixText := joinTexts(prefix)
	full := body.text
	if prefixText != "" {
		full = prefixText + "\n\n" + body.text
	}
	if runes(full) <= limit {
		return []string{full}
	}
	// The prefix must leave room for at least one body character plus the
	// fence lines.
	if body.fence != nil && prefixText != "" && runes(prefixText)+2+runes(body.fence.open)+5+1 > limit {
		return splitFenceWithLongPrefix(prefixText, body, limit)
	}
	if body.fence != nil {
		return splitFence(prefixText, body.fence, limit)
	}
	return splitParagraph(full, limit)
}

// splitFenceWithLongPrefix handles a fence that follows so much heading text
// that the prefix cannot share a piece with the fence. Only the trailing
// heading lines need to stay with the fence; everything before them is
// ordinary text and is emitted on its own.
func splitFenceWithLongPrefix(prefixText string, body *block, limit int) []string {
	lines := strings.Split(prefixText, "\n")
	k := len(lines)
	for k > 0 && (isHeading(lines[k-1]) || isBlank(lines[k-1])) {
		k--
	}
	head := strings.TrimSpace(strings.Join(lines[:k], "\n"))
	glue := strings.TrimSpace(strings.Join(lines[k:], "\n"))
	if head == "" || runes(glue)+2+runes(body.fence.open)+5+1 > limit {
		// Nothing sensible to glue: emit the prefix, then the fence.
		out := splitUnit(nil, &block{text: prefixText}, limit)
		return append(out, splitUnit(nil, body, limit)...)
	}
	out := splitUnit(nil, &block{text: head}, limit)
	return append(out, splitUnit([]block{{text: glue, heading: true}}, body, limit)...)
}

// cut returns the first n code points of s and the rest.
func cut(s string, n int) (string, string) {
	if n >= runes(s) {
		return s, ""
	}
	i, count := 0, 0
	for i < len(s) && count < n {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		count++
	}
	return s[:i], s[i:]
}

// splitFence splits a fence's body at line breaks and re-wraps every piece
// in the fence. A prefix (headings) is put before the first piece.
func splitFence(prefix string, f *fence, limit int) []string {
	overhead := runes(f.open) + 1 + 4 // "open\n" + "\n```"
	capacity := max(1, limit-overhead)
	firstCap := capacity
	if prefix != "" {
		firstCap = max(1, capacity-runes(prefix)-2)
	}

	var out []string
	var cur []string
	curLen := 0
	capNow := firstCap
	emit := func(lines []string) {
		piece := wrap(f.open, lines, true)
		if len(out) == 0 && prefix != "" {
			piece = prefix + "\n\n" + piece
		}
		out = append(out, piece)
		capNow = capacity
	}
	flush := func() {
		if len(cur) > 0 {
			emit(cur)
		}
		cur, curLen = nil, 0
	}
	for _, line := range f.body {
		for runes(line) > capNow {
			flush()
			var head string
			head, line = cut(line, capNow)
			emit([]string{head})
		}
		n := runes(line)
		if len(cur) > 0 && curLen+1+n > capNow {
			flush()
		}
		if len(cur) > 0 {
			curLen++
		}
		cur = append(cur, line)
		curLen += n
	}
	flush()
	return out
}

type textLine struct {
	s       string
	heading bool
}

// splitParagraph splits text at line breaks. A piece never ends on a heading
// line: trailing headings move to the start of the next piece. A line over
// the limit is split hard; its segments are not treated as headings.
func splitParagraph(text string, limit int) []string {
	var out []string
	var cur []textLine
	curLen := 0

	join := func(lines []textLine) string {
		parts := make([]string, len(lines))
		for i, l := range lines {
			parts[i] = l.s
		}
		return strings.Join(parts, "\n")
	}
	allHeadings := func(lines []textLine) bool {
		for _, l := range lines {
			if !l.heading && !isBlank(l.s) {
				return false
			}
		}
		return true
	}
	reset := func(lines []textLine) {
		cur = lines
		curLen = 0
		for i, l := range lines {
			if i > 0 {
				curLen++
			}
			curLen += runes(l.s)
		}
	}
	// emit writes cur out as a piece. When moveHeadings is set, trailing
	// heading lines are carried over to the next piece, provided something
	// else precedes them.
	emit := func(moveHeadings bool) {
		lines := cur
		var carry []textLine
		if moveHeadings {
			end := len(lines)
			for end > 0 && isBlank(lines[end-1].s) {
				end--
			}
			if end > 0 && lines[end-1].heading {
				k := end
				for k > 0 && (lines[k-1].heading || isBlank(lines[k-1].s)) {
					k--
				}
				if k > 0 {
					carry = lines[k:]
					for len(carry) > 0 && isBlank(carry[0].s) {
						carry = carry[1:]
					}
					lines = lines[:k]
				}
			}
		}
		out = append(out, join(lines))
		reset(carry)
	}

	for _, raw := range strings.Split(text, "\n") {
		line := textLine{s: raw, heading: isHeading(raw)}
		for {
			n := runes(line.s)
			fits := n <= limit
			if len(cur) > 0 {
				fits = curLen+1+n <= limit
			}
			if fits {
				cur = append(cur, line)
				if len(cur) > 1 {
					curLen++
				}
				curLen += n
				break
			}
			switch {
			case len(cur) == 0:
				// Over the limit on its own: hard split.
				var head string
				head, line.s = cut(line.s, limit)
				line.heading = false
				out = append(out, head)
			case allHeadings(cur):
				// Only headings so far; they must not end a piece, so
				// fill the piece with the start of this line.
				room := limit - curLen - 1
				if room < 1 {
					emit(false)
					break
				}
				var head string
				head, line.s = cut(line.s, room)
				line.heading = false
				cur = append(cur, textLine{s: head})
				curLen += 1 + runes(head)
				emit(false)
			default:
				emit(true)
			}
			if line.s == "" && !line.heading && len(cur) == 0 {
				// The hard split consumed the whole line.
				break
			}
		}
	}
	if len(cur) > 0 {
		out = append(out, join(cur))
	}
	return out
}
