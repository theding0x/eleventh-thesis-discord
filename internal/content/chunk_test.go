package content

import (
	"math/rand"
	"os"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

func lastLine(s string) string {
	i := strings.LastIndex(s, "\n")
	return s[i+1:]
}

func requireWithinLimit(t *testing.T, chunks []string, limit int) {
	t.Helper()
	for i, c := range chunks {
		if n := utf8.RuneCountInString(c); n > limit {
			t.Errorf("chunk %d has %d code points, limit %d", i, n, limit)
		}
		if c == "" || strings.TrimSpace(c) != c {
			t.Errorf("chunk %d is empty or not trimmed: %q", i, c)
		}
	}
}

func requireNoHeadingLast(t *testing.T, chunks []string) {
	t.Helper()
	for i, c := range chunks {
		if strings.HasPrefix(lastLine(c), "#") {
			t.Errorf("chunk %d ends with a heading line %q", i, lastLine(c))
		}
	}
}

func TestChunkShortReturnsOne(t *testing.T) {
	got := Chunk("# A\n\nbody", Limit)
	want := []string{"# A\n\nbody"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestChunkTrimsWhitespace(t *testing.T) {
	got := Chunk("\n\n  text  \n\n", Limit)
	want := []string{"text"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestChunkEmptyInput(t *testing.T) {
	for _, in := range []string{"", "\n\n  \n"} {
		got := Chunk(in, Limit)
		if got == nil || len(got) != 0 {
			t.Errorf("Chunk(%q) = %#v, want empty non-nil slice", in, got)
		}
	}
}

func TestChunkRespectsLimit(t *testing.T) {
	paras := make([]string, 50)
	for i := range paras {
		paras[i] = strings.Repeat("a", 100)
	}
	in := strings.Join(paras, "\n\n")
	chunks := Chunk(in, Limit)
	if len(chunks) < 2 {
		t.Fatalf("expected several chunks, got %d", len(chunks))
	}
	requireWithinLimit(t, chunks, Limit)
	if got := strings.Join(chunks, "\n\n"); got != in {
		t.Fatalf("joined chunks differ from input")
	}
}

// headingAtBoundary builds: a paragraph that leaves room for a heading but not
// for the heading plus the next paragraph, then the heading, then a paragraph.
func headingAtBoundary(heading string) string {
	return strings.Repeat("a", 1850) + "\n\n" + heading + "\n\n" + strings.Repeat("b", 100)
}

func TestChunkHeadingNotOrphaned(t *testing.T) {
	chunks := Chunk(headingAtBoundary("## H"), Limit)
	want := []string{strings.Repeat("a", 1850), "## H\n\n" + strings.Repeat("b", 100)}
	if !reflect.DeepEqual(chunks, want) {
		t.Fatalf("got %d chunks %q", len(chunks), chunks)
	}
	requireNoHeadingLast(t, chunks)
}

func TestChunkThirdLevelHeadingGlued(t *testing.T) {
	chunks := Chunk(headingAtBoundary("### H"), Limit)
	want := []string{strings.Repeat("a", 1850), "### H\n\n" + strings.Repeat("b", 100)}
	if !reflect.DeepEqual(chunks, want) {
		t.Fatalf("got %d chunks %q", len(chunks), chunks)
	}
	requireNoHeadingLast(t, chunks)
}

func TestChunkConsecutiveHeadingsGlued(t *testing.T) {
	in := strings.Repeat("a", 1850) + "\n\n## H1\n\n### H2\n\n" + strings.Repeat("b", 100)
	chunks := Chunk(in, Limit)
	want := []string{strings.Repeat("a", 1850), "## H1\n\n### H2\n\n" + strings.Repeat("b", 100)}
	if !reflect.DeepEqual(chunks, want) {
		t.Fatalf("got %q", chunks)
	}
}

func TestChunkHeadingAsLastBlockKept(t *testing.T) {
	got := Chunk("body\n\n## Trailing", Limit)
	want := []string{"body\n\n## Trailing"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	// Even when it does not fit with the previous block, it is kept.
	got = Chunk(strings.Repeat("a", 1895)+"\n\n## Trailing", Limit)
	if len(got) != 2 || got[1] != "## Trailing" {
		t.Fatalf("got %q", got)
	}
}

func TestChunkLongBlockHeadingNotLast(t *testing.T) {
	var lines []string
	line := strings.Repeat("x", 40)
	for i := 0; i < 46; i++ {
		lines = append(lines, line)
	}
	// 46 lines of 40 plus "### H" is 1,891 code points: the heading would be
	// the last line of the first piece, and the next line would not fit.
	lines = append(lines, "### H")
	for i := 0; i < 28; i++ {
		lines = append(lines, line)
	}
	in := strings.Join(lines, "\n")
	if n := utf8.RuneCountInString(in); n < 2900 || n > 3100 {
		t.Fatalf("test input should be about 3,000 characters, got %d", n)
	}
	chunks := Chunk(in, Limit)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks", len(chunks))
	}
	requireWithinLimit(t, chunks, Limit)
	requireNoHeadingLast(t, chunks)
	if !strings.HasPrefix(chunks[1], "### H\n") {
		t.Errorf("heading should move to the start of the next piece, got %q", chunks[1][:20])
	}
	if got := strings.Join(chunks, "\n"); got != in {
		t.Errorf("no line may be lost or reordered")
	}
}

func TestChunkWrapsTableInFence(t *testing.T) {
	got := Chunk("text\n\n| a | b |\n|---|---|\n| 1 | 2 |", Limit)
	want := []string{"text\n\n```\n| a | b |\n|---|---|\n| 1 | 2 |\n```"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func filler(total int) string {
	var paras []string
	for utf8.RuneCountInString(strings.Join(paras, "\n\n")) < total {
		paras = append(paras, strings.Repeat("p", 100))
	}
	return strings.Join(paras, "\n\n")
}

func TestChunkWrapsIndentedTableInFence(t *testing.T) {
	in := "2. Split:\n\n   | a | b |\n   |---|---|\n   | 1 | 2 |\n\n3. Next"
	got := Chunk(in, Limit)
	want := []string{"2. Split:\n\n```\n| a | b |\n|---|---|\n| 1 | 2 |\n```\n\n3. Next"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func countContaining(chunks []string, sub string) int {
	n := 0
	for _, c := range chunks {
		if strings.Contains(c, sub) {
			n++
		}
	}
	return n
}

func TestChunkKeepsTableWhole(t *testing.T) {
	rows := []string{"| n | v |", "|---|---|"}
	for i := 0; i < 10; i++ {
		rows = append(rows, "| "+strings.Repeat("r", 5)+" | "+strings.Repeat("c", 10)+" |")
	}
	table := strings.Join(rows, "\n")
	pre := filler(1800)
	if n := utf8.RuneCountInString(pre); n < 1750 || n > 1850 {
		t.Fatalf("filler should be about 1,800, got %d", n)
	}
	chunks := Chunk(pre+"\n\n"+table, Limit)
	requireWithinLimit(t, chunks, Limit)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks", len(chunks))
	}
	if chunks[1] != "```\n"+table+"\n```" {
		t.Errorf("second chunk should be exactly the fenced table, got %q", chunks[1])
	}
	if countContaining(chunks, "```\n"+table+"\n```") != 1 {
		t.Errorf("fenced table must appear intact in exactly one chunk")
	}
}

func TestChunkKeepsFenceWhole(t *testing.T) {
	var body []string
	for i := 0; i < 10; i++ {
		body = append(body, strings.Repeat("c", 20))
	}
	fence := "```go\n" + strings.Join(body, "\n") + "\n```"
	chunks := Chunk(filler(1800)+"\n\n"+fence, Limit)
	requireWithinLimit(t, chunks, Limit)
	if len(chunks) != 2 || chunks[1] != fence {
		t.Fatalf("got %d chunks %q", len(chunks), chunks)
	}
	if countContaining(chunks, fence) != 1 {
		t.Errorf("fence must appear intact in exactly one chunk")
	}
}

func TestChunkSplitsLongFenceAndRewraps(t *testing.T) {
	var body []string
	for i := 0; i < 100; i++ {
		body = append(body, strings.Repeat("c", 39))
	}
	in := "```go\n" + strings.Join(body, "\n") + "\n```"
	chunks := Chunk(in, Limit)
	if len(chunks) < 2 {
		t.Fatalf("expected a split, got %d chunk", len(chunks))
	}
	requireWithinLimit(t, chunks, Limit)
	total := 0
	for i, c := range chunks {
		if !strings.HasPrefix(c, "```go\n") || !strings.HasSuffix(c, "\n```") {
			t.Errorf("chunk %d is not wrapped in fences: %q", i, c[:10])
		}
		for _, l := range strings.Split(c, "\n") {
			if l == strings.Repeat("c", 39) {
				total++
			}
		}
	}
	if total != 100 {
		t.Errorf("expected 100 body lines across chunks, got %d", total)
	}
}

func TestChunkSplitsLongTableAndRewraps(t *testing.T) {
	rows := []string{"| a | b |", "|---|---|"}
	for i := 0; i < 80; i++ {
		rows = append(rows, "| "+strings.Repeat("x", 30)+" | "+strings.Repeat("y", 30)+" |")
	}
	chunks := Chunk(strings.Join(rows, "\n"), Limit)
	if len(chunks) < 2 {
		t.Fatalf("expected a split, got %d chunk", len(chunks))
	}
	requireWithinLimit(t, chunks, Limit)
	for i, c := range chunks {
		if !strings.HasPrefix(c, "```\n|") || !strings.HasSuffix(c, "|\n```") {
			t.Errorf("chunk %d is not a fenced table piece: %q", i, c)
		}
	}
}

func TestChunkUnclosedFenceAtEOF(t *testing.T) {
	in := "intro\n\n```\ncode line\nmore"
	got := Chunk(in, Limit)
	if !reflect.DeepEqual(got, []string{in}) {
		t.Fatalf("got %q", got)
	}
	long := "```\n" + strings.Repeat(strings.Repeat("z", 99)+"\n", 60)
	chunks := Chunk(long, Limit)
	requireWithinLimit(t, chunks, Limit)
	if len(chunks) < 2 {
		t.Fatalf("expected a split")
	}
}

func TestChunkHardSplitsLongLine(t *testing.T) {
	chunks := Chunk(strings.Repeat("a", 5000), Limit)
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	requireWithinLimit(t, chunks, Limit)
	if strings.Join(chunks, "") != strings.Repeat("a", 5000) {
		t.Errorf("hard split lost characters")
	}
}

func TestChunkHardSplitsLongHeadingLine(t *testing.T) {
	in := "### " + strings.Repeat("a", 5000)
	chunks := Chunk(in, Limit)
	requireWithinLimit(t, chunks, Limit)
	if strings.Join(chunks, "") != in {
		t.Errorf("hard split lost characters")
	}
}

func TestChunkCountsRunes(t *testing.T) {
	chunks := Chunk(strings.Repeat("é", 1900), Limit)
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1", len(chunks))
	}
	if got := Chunk(strings.Repeat("é", 1901), Limit); len(got) != 2 {
		t.Fatalf("1901 runes should split, got %d chunks", len(got))
	}
}

func TestChunkCRLFDoesNotPanic(t *testing.T) {
	in := "# A\r\n\r\nbody\r\n\r\n| a |\r\n|---|\r\n\r\n```\r\ncode\r\n```\r\n"
	got := Chunk(in, Limit)
	if len(got) != 1 {
		t.Fatalf("got %q", got)
	}
}

func TestChunkDeterministic(t *testing.T) {
	in := headingAtBoundary("### H") + "\n\n| a | b |\n|---|---|\n\n" + strings.Repeat("line\n", 800)
	a := Chunk(in, Limit)
	b := Chunk(in, Limit)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("two calls differ")
	}
}

func TestChunkSmallLimitTerminates(t *testing.T) {
	in := "## H\n\n### H2\n\n" + strings.Repeat("word ", 50) + "\n## T\n```\n" + strings.Repeat("c\n", 30) + "```\n\n| a |\n| b |"
	for _, limit := range []int{1, 2, 5, 10, 40} {
		chunks := Chunk(in, limit)
		if len(chunks) == 0 {
			t.Errorf("limit %d produced no chunks", limit)
		}
	}
}

// TestChunkInvariantsOnGeneratedInput throws pseudo-random Markdown (fixed
// seed) at several limits and checks the invariants that later tasks rely on.
func TestChunkInvariantsOnGeneratedInput(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	pieces := []func() string{
		func() string { return strings.Repeat("w", 1+rng.Intn(300)) },
		func() string { return "## " + strings.Repeat("h", 1+rng.Intn(40)) },
		func() string { return "###### " + strings.Repeat("h", 1+rng.Intn(40)) },
		func() string { return "text\n### H\n" + strings.Repeat("v", rng.Intn(120)) },
		func() string { return "| a | b |\n|---|---|\n" + strings.Repeat("| 1 | 2 |\n", 1+rng.Intn(30)) },
		func() string { return "```go\n" + strings.Repeat("code line\n", rng.Intn(40)) + "```" },
		func() string { return strings.Repeat("line of prose\n", 1+rng.Intn(60)) },
	}
	for run := 0; run < 200; run++ {
		var parts []string
		for i, n := 0, 1+rng.Intn(25); i < n; i++ {
			parts = append(parts, pieces[rng.Intn(len(pieces))]())
		}
		in := strings.Join(parts, "\n\n")
		for _, limit := range []int{200, 500, Limit} {
			chunks := Chunk(in, limit)
			requireWithinLimit(t, chunks, limit)
			// Only a heading that is the very last line of the input may end
			// a chunk.
			for i, c := range chunks {
				if strings.HasPrefix(lastLine(c), "#") && i != len(chunks)-1 {
					t.Fatalf("run %d limit %d: chunk %d ends with heading %q\ninput: %q\nchunks: %q", run, limit, i, lastLine(c), in, chunks)
				}
			}
			if !reflect.DeepEqual(chunks, Chunk(in, limit)) {
				t.Fatalf("run %d limit %d: not deterministic", run, limit)
			}
		}
	}
}

func TestChunkRealConstitution(t *testing.T) {
	raw, err := os.ReadFile("../../content/constitution.md")
	if err != nil {
		t.Fatal(err)
	}
	chunks := Chunk(string(raw), Limit)
	if len(chunks) < 2 {
		t.Fatalf("got %d chunks, want at least 2", len(chunks))
	}
	requireWithinLimit(t, chunks, Limit)
	requireNoHeadingLast(t, chunks)

	tableLines := 0
	for i, c := range chunks {
		inFence := false
		for _, line := range strings.Split(c, "\n") {
			if strings.HasPrefix(line, "```") {
				inFence = !inFence
				continue
			}
			if strings.HasPrefix(line, "|") {
				tableLines++
				if !inFence {
					t.Errorf("chunk %d has a table line outside a fence: %q", i, line)
				}
			}
		}
		if inFence {
			t.Errorf("chunk %d has an unbalanced fence", i)
		}
	}
	if tableLines == 0 {
		t.Fatal("no table lines found; Appendices A and B should have tables")
	}
	t.Logf("%d chunks, %d table lines", len(chunks), tableLines)
	for i, c := range chunks {
		t.Logf("chunk %d: %d code points", i, utf8.RuneCountInString(c))
	}
}
