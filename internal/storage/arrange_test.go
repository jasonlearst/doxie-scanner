package storage

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/andiapps-dev/doxie-scanner/internal/pageimage"
)

// layout builds a job from a spec such as "1f 1b- | 2f": a page is its
// sheet and side, a trailing - hides it, and a leading | splits there.
func layout(spec string) JobMeta {
	var j JobMeta
	for _, tok := range strings.Fields(spec) {
		split := strings.HasPrefix(tok, "|")
		tok = strings.TrimPrefix(tok, "|")
		if tok == "" {
			continue
		}
		hidden := strings.HasSuffix(tok, "-")
		tok = strings.TrimSuffix(tok, "-")
		var sheet int
		var side byte
		fmt.Sscanf(tok, "%d%c", &sheet, &side)
		p := PageMeta{Sheet: sheet, Side: map[byte]string{'f': "front", 'b': "back"}[side], Hidden: hidden}
		p.ID = PageID(sheet, p.Side)
		p.File = PageFilename(p.ID)
		j.Pages = append(j.Pages, p)
		if split {
			j.Splits = append(j.Splits, p.ID)
		}
	}
	return j
}

// describe is the inverse of layout, splits in page order.
func describe(j JobMeta) string {
	var out []string
	for _, p := range j.Pages {
		tok := strings.TrimLeft(p.ID[:3], "0") + p.ID[3:]
		if p.Hidden {
			tok += "-"
		}
		for _, s := range j.Splits {
			if s == p.ID {
				tok = "|" + tok
			}
		}
		out = append(out, tok)
	}
	return strings.Join(out, " ")
}

func id(short string) string { return layout(short).Pages[0].ID }

func TestMovePage(t *testing.T) {
	cases := []struct {
		name, from, page, rel, target, want string
	}{
		{"after, in one document", "1f 2f 3f", "1f", "after", "3f", "2f 3f 1f"},
		{"before, in one document", "1f 2f 3f", "3f", "before", "1f", "3f 1f 2f"},
		{"to the end, crossing a divider", "1f 2f |3f 4f", "3f", "after", "4f", "1f 2f |4f 3f"},
		{"to the start, crossing a divider", "1f 2f |3f 4f", "4f", "before", "1f", "4f 1f 2f |3f"},
		{"just above a divider ends that document", "1f 2f |3f 4f", "4f", "after", "2f", "1f 2f 4f |3f"},
		{"just below a divider starts the next", "1f 2f |3f 4f", "1f", "before", "3f", "2f |1f 3f 4f"},
		{"a document's first page leaves its divider behind", "1f 2f |3f 4f", "3f", "after", "1f", "1f 3f 2f |4f"},
		{"an emptied document goes", "1f |2f |3f", "2f", "after", "3f", "1f |3f 2f"},
		{"an emptied first document goes", "1f |2f 3f", "1f", "after", "3f", "2f 3f 1f"},
		{"a hidden back travels with its front", "1f 1b- 2f 2b-", "2f", "before", "1f", "2f 2b- 1f 1b-"},
		{"after a page lands after its hidden back", "1f 1b- 2f 2b-", "1f", "after", "2f", "2f 2b- 1f 1b-"},
		{"before a page lands before its hidden front", "1f 2f- 2b 3f", "3f", "before", "2b", "1f 3f 2f- 2b"},
		{"a hidden front travels with its back", "1f 2f- 2b 3f", "2b", "after", "3f", "1f 3f 2f- 2b"},
		{"other hidden pages stay put", "1f 9f- 2f 3f", "1f", "after", "3f", "9f- 2f 3f 1f"},
		{"splits on hidden pages stay", "1f |1b- 2f |3f", "3f", "before", "1f", "3f 1f |1b- 2f"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			j := layout(c.from)
			if err := j.MovePage(id(c.page), id(c.target), c.rel == "after"); err != nil {
				t.Fatal(err)
			}
			if got := describe(j); got != c.want {
				t.Errorf("%s, move %s %s %s: got %q, want %q", c.from, c.page, c.rel, c.target, got, c.want)
			}
		})
	}
}

func TestMovePage_Refusals(t *testing.T) {
	j := layout("1f 1b- 2f")
	if err := j.MovePage("009f", "001f", true); !errors.Is(err, ErrPageNotFound) {
		t.Errorf("unknown page: %v", err)
	}
	for _, c := range [][2]string{{"001b", "002f"}, {"001f", "009f"}, {"001f", "001b"}, {"001f", "001f"}, {"001f", ""}} {
		if err := j.MovePage(c[0], c[1], true); err == nil || errors.Is(err, ErrPageNotFound) {
			t.Errorf("move %s next to %s: got %v, want a refusal", c[0], c[1], err)
		}
	}
	if got := describe(j); got != "1f 1b- 2f" {
		t.Errorf("a refused move changed the job: %q", got)
	}
}

func TestDocuments(t *testing.T) {
	j := layout("1f 1b- |2f |2b- 3f |4f")
	if got := fmt.Sprint(j.Documents()); got != "[[001f] [002f 003f] [004f]]" {
		t.Errorf("got %s", got)
	}
	if got := (JobMeta{}).Documents(); got != nil {
		t.Errorf("no pages: %v", got)
	}
}

func TestDeletePage_RemembersItsSheet(t *testing.T) {
	j := layout("1f |2f 3f 3b-")
	if _, err := j.DeletePage("009f"); !errors.Is(err, ErrPageNotFound) {
		t.Errorf("unknown page: %v", err)
	}
	page, err := j.DeletePage("002f")
	if err != nil || page.ID != "002f" || describe(j) != "1f 3f 3b-" {
		t.Fatalf("deleted %+v, %v: left %q", page, err, describe(j))
	}
	j.DeletePage("003b")
	j.DeletePage("003f")
	if j.LastSheet != 3 || j.highestSheet() != 3 {
		t.Errorf("LastSheet %d: sheet 3 was used and must not be handed out again", j.LastSheet)
	}
}

func TestCombine_OlderPagesFirstAsOneDocument(t *testing.T) {
	t0 := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	older := layout("1f 1b- |2f")
	older.CreatedAt, older.Status, older.Error = t0, StatusFailed, "paper jam: open the scanner"
	newer := layout("1f |1b 2f")
	newer.CreatedAt = t0.Add(time.Minute)
	done := t0.Add(2 * time.Minute)
	newer.CompletedAt = &done
	newer.Pages[0].Edits = pageimage.Edits{Rotation: 90}

	// The open scan is the older one: the newer one's pages follow.
	got, files := older.Combine(newer)
	if d := describe(got); d != "1f 1b- |2f 3f |3b 4f" {
		t.Errorf("open older: got %q", d)
	}
	if got.Pages[3].Edits.Rotation != 90 || got.Pages[3].File != "003f.jpg" || got.Pages[3].Sheet != 3 {
		t.Errorf("an incoming page keeps its edits under its new ID: %+v", got.Pages[3])
	}
	if fmt.Sprint(files) != "map[001b.jpg:003b.jpg 001f.jpg:003f.jpg 002f.jpg:004f.jpg]" {
		t.Errorf("files %v", files)
	}
	if got.Status != StatusCompleted || got.Error != "" || got.CompletedAt == nil || !got.CompletedAt.Equal(done) {
		t.Errorf("the combined scan counts as finished: %s %q %v", got.Status, got.Error, got.CompletedAt)
	}
	if got.LastSheet != 4 {
		t.Errorf("LastSheet %d", got.LastSheet)
	}

	// The open scan is the newer one: the older one's pages go first,
	// and a split on the newer part's first page would cut the two apart.
	newer.Splits = append(newer.Splits, "001f")
	got, _ = newer.Combine(older)
	if d := describe(got); d != "3f 3b- |4f 1f |1b 2f" {
		t.Errorf("open newer: got %q", d)
	}
	if got.ID != newer.ID || !got.CreatedAt.Equal(newer.CreatedAt) {
		t.Error("the combined scan keeps the open scan's identity")
	}
}

func TestCombine_NeverReusesASheet(t *testing.T) {
	into := layout("1f 2f")
	into.DeletePage("002f")
	from := layout("1f")
	got, _ := into.Combine(from)
	if d := describe(got); d != "1f 3f" {
		t.Errorf("got %q: sheet 2 was deleted, so the incoming page must not become 002f", d)
	}
}

func TestCombine_PagesWithoutSheetsGetIDsOfTheirOwn(t *testing.T) {
	into := JobMeta{Pages: []PageMeta{{ID: "002f", File: "002f.jpg"}}}
	from := JobMeta{CreatedAt: time.Unix(1, 0), Pages: []PageMeta{{ID: "a", File: "a.jpg"}, {ID: "b", File: "b.jpg", Side: "back"}, {ID: "c", File: "c.jpg"}}}
	got, _ := into.Combine(from)
	var ids []string
	for _, p := range got.Pages {
		ids = append(ids, p.ID)
	}
	// a and c share sheet 0 and side, and 002f is taken: no ID repeats.
	if fmt.Sprint(ids) != "[002f 001f 001b 003f]" {
		t.Errorf("ids %v", ids)
	}
}
