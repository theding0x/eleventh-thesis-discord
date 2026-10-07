package reconcile

import (
	"reflect"
	"testing"

	"github.com/theding0x/eleventh-thesis-discord/internal/model"
)

// constitutionChunks returns the constitution chunks and fails unless there
// are exactly the three the tests below assume.
func constitutionChunks(t *testing.T, d Desired) []string {
	t.Helper()
	chunks := d.Content["constitution"]
	if len(chunks) != 3 {
		t.Fatalf("test spec has %d constitution chunks, want 3", len(chunks))
	}
	return chunks
}

func TestContentConverged(t *testing.T) {
	d := mustDesired(t)
	p := Reconcile(d, converged(d), false)
	if got := strs(p); len(got) != 0 {
		t.Errorf("actions = %q, want none", got)
	}
}

func TestContentChanged(t *testing.T) {
	d := mustDesired(t)
	chunks := constitutionChunks(t, d)
	g := converged(d)
	g.BotMessages["constitution"][1].Content = "stale text"

	p := Reconcile(d, g, false)
	want := []string{"~ edit message 2/3 in #constitution"}
	if got := strs(p); !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %q, want %q", got, want)
	}
	a := p.Actions[0]
	if a.Kind != EditMessage || a.Target != "constitution" || a.Index != 2 || a.Total != 3 {
		t.Errorf("action = %+v", a)
	}
	if a.Message != (model.Message{ID: g.BotMessages["constitution"][1].ID, Content: chunks[1]}) {
		t.Errorf("edit carries %+v, want observed ID and desired chunk", a.Message)
	}
}

func TestContentLonger(t *testing.T) {
	d := mustDesired(t)
	chunks := constitutionChunks(t, d)
	g := converged(d)
	g.BotMessages["constitution"] = g.BotMessages["constitution"][:2]

	p := Reconcile(d, g, false)
	want := []string{"+ post message 3/3 in #constitution"}
	if got := strs(p); !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %q, want %q", got, want)
	}
	a := p.Actions[0]
	if a.Kind != PostMessage || a.Target != "constitution" || a.Index != 3 || a.Total != 3 {
		t.Errorf("action = %+v", a)
	}
	if a.Message != (model.Message{Content: chunks[2]}) {
		t.Errorf("post carries %+v, want the desired chunk and no ID", a.Message)
	}
}

func TestContentShorter(t *testing.T) {
	d := mustDesired(t)
	g := converged(d)
	g.BotMessages["constitution"] = append(g.BotMessages["constitution"], model.Message{ID: "extra", Content: "old tail"})

	p := Reconcile(d, g, false)
	want := []string{"- delete message in #constitution"}
	if got := strs(p); !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %q, want %q", got, want)
	}
	a := p.Actions[0]
	if a.Kind != DeleteMessage || a.Target != "constitution" || a.Message.ID != "extra" || a.Index != 4 || a.Total != 3 {
		t.Errorf("action = %+v, want delete of message extra", a)
	}
}

func TestContentNewChannel(t *testing.T) {
	d := mustDesired(t)
	g := converged(d)
	var channels []model.Channel
	for _, c := range g.Channels {
		if c.Name != "welcome" {
			channels = append(channels, c)
		}
	}
	g.Channels = channels
	delete(g.BotMessages, "welcome")

	p := Reconcile(d, g, false)
	want := []string{
		"+ create text #welcome in Front Door",
		"+ post message 1/1 in #welcome",
	}
	if got := strs(p); !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %q, want %q", got, want)
	}
	if p.Actions[1].Message.Content != d.Content["welcome"][0] {
		t.Errorf("post content = %q, want the welcome chunk", p.Actions[1].Message.Content)
	}
}

func TestContentEmptyChunksDeletesAllBotMessages(t *testing.T) {
	d := mustDesired(t)
	g := converged(d)
	d.Content = map[string][]string{"constitution": {}}

	p := Reconcile(d, g, false)
	want := []string{
		"- delete message in #constitution",
		"- delete message in #constitution",
		"- delete message in #constitution",
	}
	if got := strs(p); !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %q, want %q", got, want)
	}
	for i, a := range p.Actions {
		if id := g.BotMessages["constitution"][i].ID; a.Message.ID != id {
			t.Errorf("action %d deletes %q, want %q", i, a.Message.ID, id)
		}
	}
}

func TestContentChangedFirstAndMissingLast(t *testing.T) {
	d := mustDesired(t)
	constitutionChunks(t, d)
	g := converged(d)
	g.BotMessages["constitution"][0].Content = "stale text"
	g.BotMessages["constitution"] = g.BotMessages["constitution"][:2]

	want := []string{
		"~ edit message 1/3 in #constitution",
		"+ post message 3/3 in #constitution",
	}
	if got := strs(Reconcile(d, g, false)); !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %q, want %q", got, want)
	}
}

func TestContentDeletesComeAfterEdits(t *testing.T) {
	d := mustDesired(t)
	constitutionChunks(t, d)
	g := converged(d)
	g.BotMessages["constitution"][2].Content = "stale text"
	g.BotMessages["constitution"] = append(g.BotMessages["constitution"],
		model.Message{ID: "x1", Content: "a"}, model.Message{ID: "x2", Content: "b"})

	want := []string{
		"~ edit message 3/3 in #constitution",
		"- delete message in #constitution",
		"- delete message in #constitution",
	}
	p := Reconcile(d, g, false)
	if got := strs(p); !reflect.DeepEqual(got, want) {
		t.Fatalf("actions = %q, want %q", got, want)
	}
	if p.Actions[1].Message.ID != "x1" || p.Actions[2].Message.ID != "x2" {
		t.Errorf("deletes = %q, %q, want x1, x2", p.Actions[1].Message.ID, p.Actions[2].Message.ID)
	}
}

func TestContentSkipsChannelsThatAreNotText(t *testing.T) {
	d := mustDesired(t)
	g := converged(d)
	d.Content = map[string][]string{"Comms": {"hello"}, "ghost": {"hello"}}
	if got := strs(Reconcile(d, g, false)); len(got) != 0 {
		t.Errorf("actions = %q, want none", got)
	}
}
