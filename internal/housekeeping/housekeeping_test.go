package housekeeping

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andiapps-dev/doxie-scanner/internal/paperless"
	"github.com/andiapps-dev/doxie-scanner/internal/storage"
)

var now = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

func days(n int) *int { return &n }

func sent(status string, filedDaysAgo int, pages ...string) storage.SentDocument {
	filed := now.Add(-time.Duration(filedDaysAgo) * 24 * time.Hour)
	s := storage.SentDocument{Status: status, Pages: pages, SentAt: filed, TaskID: "t-" + status}
	if status == paperless.TaskDone {
		s.FiledAt = &filed
	}
	return s
}

func job(id string, pages []storage.PageMeta, docs ...storage.SentDocument) storage.JobMeta {
	return storage.JobMeta{ID: id, Status: storage.StatusCompleted, Pages: pages, Sent: docs}
}

func pages(ids ...string) []storage.PageMeta {
	out := make([]storage.PageMeta, len(ids))
	for i, id := range ids {
		out[i] = storage.PageMeta{ID: id}
	}
	return out
}

func TestImported(t *testing.T) {
	hidden := pages("001f", "001b")
	hidden[1].Hidden = true
	cases := map[string]struct {
		job  storage.JobMeta
		want bool
	}{
		"every page filed":         {job("a", pages("001f", "002f"), sent("done", 9, "001f"), sent("done", 8, "002f")), true},
		"hidden pages don't count": {job("b", hidden, sent("done", 9, "001f")), true},
		"a page never sent":        {job("c", pages("001f", "002f"), sent("done", 9, "001f")), false},
		"an upload still waiting":  {job("d", pages("001f"), sent("done", 9, "001f"), sent("queued", 1, "001f")), false},
		"failed but resent":        {job("e", pages("001f"), sent("failed", 9, "001f"), sent("done", 8, "001f")), true},
		"only a failure":           {job("f", pages("001f"), sent("failed", 9, "001f")), false},
		"never sent":               {job("g", pages("001f")), false},
		"still scanning":           {storage.JobMeta{Status: storage.StatusRunning, Sent: []storage.SentDocument{sent("done", 9, "001f")}}, false},
	}
	for name, c := range cases {
		if _, got := Imported(c.job); got != c.want {
			t.Errorf("%s: got %v, want %v", name, got, c.want)
		}
	}
	if filed, _ := Imported(cases["every page filed"].job); !filed.Equal(now.Add(-8 * 24 * time.Hour)) {
		t.Errorf("filed time should be the last filing, got %v", filed)
	}
	noFiledAt := job("h", pages("001f"), storage.SentDocument{Status: "done", Pages: []string{"001f"}, SentAt: now})
	if filed, ok := Imported(noFiledAt); !ok || !filed.Equal(now) {
		t.Errorf("without FiledAt the sent time stands in, got %v %v", filed, ok)
	}
}

func TestRemoveAt(t *testing.T) {
	j := job("a", pages("001f"), sent("done", 2, "001f"))
	if due, ok := RemoveAt(j, 7); !ok || !due.Equal(now.Add(5*24*time.Hour)) {
		t.Errorf("got %v %v", due, ok)
	}
	if _, ok := RemoveAt(j, 0); ok {
		t.Error("0 days keeps scans forever")
	}
}

func TestRetentionDays(t *testing.T) {
	if RetentionDays(storage.AppSettings{}) != DefaultRetentionDays || RetentionDays(storage.AppSettings{RetentionDays: days(0)}) != 0 {
		t.Error("unset means the default; an explicit 0 means forever")
	}
}

func keeper(t *testing.T, tasks map[string]string) (*Keeper, *storage.Store) {
	t.Helper()
	store := storage.New(t.TempDir())
	k := &Keeper{Store: store, Scanning: func(string) bool { return false }, Now: func() time.Time { return now }}
	if tasks != nil {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, tasks[r.URL.Query().Get("task_id")])
		}))
		t.Cleanup(srv.Close)
		k.Paperless = paperless.New(srv.URL, "t")
	}
	return k, store
}

func save(t *testing.T, store *storage.Store, j storage.JobMeta) {
	t.Helper()
	if err := store.CreateJob(j); err != nil {
		t.Fatal(err)
	}
}

func exists(store *storage.Store, id string) bool {
	_, err := store.LoadMeta(id)
	return err == nil
}

func TestSweep_DeletesOnlyExpiredImports(t *testing.T) {
	k, store := keeper(t, nil)
	save(t, store, job("old", pages("001f"), sent("done", 8, "001f")))
	save(t, store, job("recent", pages("001f"), sent("done", 3, "001f")))
	save(t, store, job("partial", pages("001f", "002f"), sent("done", 30, "001f")))
	save(t, store, job("review", pages("001f")))
	save(t, store, job("scanning", pages("001f"), sent("done", 30, "001f")))
	k.Scanning = func(id string) bool { return id == "scanning" }

	k.Sweep(context.Background())

	for id, want := range map[string]bool{"old": false, "recent": true, "partial": true, "review": true, "scanning": true} {
		if got := exists(store, id); got != want {
			t.Errorf("%s: kept=%v, want %v", id, got, want)
		}
	}
}

func TestSweep_HonorsRetentionSetting(t *testing.T) {
	k, store := keeper(t, nil)
	save(t, store, job("old", pages("001f"), sent("done", 400, "001f")))
	if err := store.SaveSettings(storage.AppSettings{RetentionDays: days(0)}); err != nil {
		t.Fatal(err)
	}
	k.Sweep(context.Background())
	if !exists(store, "old") {
		t.Error("retention 0 must keep everything")
	}
	if err := store.SaveSettings(storage.AppSettings{RetentionDays: days(500)}); err != nil {
		t.Fatal(err)
	}
	k.Sweep(context.Background())
	if !exists(store, "old") {
		t.Error("400 days old is inside a 500-day window")
	}
}

func TestSweep_FollowsPendingUploadsFirst(t *testing.T) {
	k, store := keeper(t, map[string]string{"t-queued": `[{"status":"SUCCESS","related_document":"7"}]`})
	j := job("waiting", pages("001f"), sent("queued", 10, "001f"))
	save(t, store, j)

	k.Sweep(context.Background())

	meta, err := store.LoadMeta("waiting")
	if err != nil {
		t.Fatal("filed just now: it should be kept for the retention period")
	}
	if s := meta.Sent[0]; s.Status != paperless.TaskDone || s.DocumentID != 7 || s.FiledAt == nil || !s.FiledAt.Equal(now) {
		t.Errorf("the pending upload should be recorded as filed now: %+v", s)
	}
}

func TestRefreshSent(t *testing.T) {
	k, store := keeper(t, map[string]string{"t-queued": `[{"status":"FAILURE","result":"duplicate"}]`})
	save(t, store, job("a", pages("001f"), sent("queued", 1, "001f"), sent("done", 2, "001f")))
	meta, err := k.RefreshSent(context.Background(), "a")
	if err != nil || meta.Sent[0].Status != paperless.TaskFailed || meta.Sent[0].Error != "duplicate" {
		t.Errorf("got %+v, %v", meta.Sent, err)
	}
	if _, err := k.RefreshSent(context.Background(), "nope"); err == nil {
		t.Error("unknown job should fail")
	}

	down, store2 := keeper(t, nil)
	down.Paperless = paperless.New("http://127.0.0.1:1", "t")
	save(t, store2, job("b", pages("001f"), sent("queued", 1, "001f")))
	if _, err := down.RefreshSent(context.Background(), "b"); err == nil {
		t.Error("an unreachable Paperless should fail")
	}
}

func TestSweep_StorageProblemsAreLoggedNotFatal(t *testing.T) {
	k, store := keeper(t, nil)
	if err := os.WriteFile(filepath.Join(store.Root(), "settings.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	k.Sweep(context.Background())

	k2, store2 := keeper(t, nil)
	if err := os.WriteFile(filepath.Join(store2.Root(), "jobs"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	k2.Sweep(context.Background())

	k3, store3 := keeper(t, nil)
	save(t, store3, job("old", pages("001f"), sent("done", 30, "001f")))
	jobs := filepath.Join(store3.Root(), "jobs")
	if err := os.Chmod(jobs, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(jobs, 0o755) })
	k3.Sweep(context.Background())
	if _, err := os.Stat(filepath.Join(jobs, "old")); err != nil {
		t.Error("a delete the filesystem refuses should leave the job's folder behind, not crash the sweep")
	}
}

func TestRun_StopsWithContext(t *testing.T) {
	k, store := keeper(t, nil)
	save(t, store, job("old", pages("001f"), sent("done", 30, "001f")))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { k.Run(ctx, 5*time.Millisecond); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for exists(store, "old") && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done
	if exists(store, "old") {
		t.Error("Run should have swept")
	}
}
