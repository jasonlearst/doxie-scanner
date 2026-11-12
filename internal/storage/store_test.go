package storage

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andiapps-dev/doxie-scanner/internal/pageimage"
)

func TestStore_CheckWritable_CreatesAndVerifies(t *testing.T) {
	root := filepath.Join(t.TempDir(), "does", "not", "exist", "yet")
	s := New(root)
	if err := s.CheckWritable(); err != nil {
		t.Fatalf("CheckWritable: %v", err)
	}
	if _, err := os.Stat(root); err != nil {
		t.Errorf("expected root to be created: %v", err)
	}
}

func TestStore_CheckWritable_FailsOnUnwritablePath(t *testing.T) {
	// A root path underneath a regular file can never be created.
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(filepath.Join(blocker, "data"))
	if err := s.CheckWritable(); err == nil {
		t.Fatal("expected an error")
	}
}

func TestStore_CheckWritable_FailsWhenDirNotWritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission-based test is unreliable")
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(root, 0o755) // allow t.TempDir()'s own cleanup to succeed

	s := New(root)
	if err := s.CheckWritable(); err == nil {
		t.Fatal("expected an error writing into a read-only directory")
	}
}

func TestStore_SaveMeta_WriteError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission-based test is unreliable")
	}
	s := New(t.TempDir())
	if err := s.CreateJob(JobMeta{ID: "job1", Name: "before"}); err != nil {
		t.Fatal(err)
	}
	// The new meta.json is written beside the old one and renamed over
	// it, so a read-only job directory blocks the save.
	if err := os.Chmod(s.jobDir("job1"), 0o555); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(s.jobDir("job1"), 0o755)

	if err := s.SaveMeta(JobMeta{ID: "job1", Name: "changed"}); err == nil {
		t.Fatal("expected a write error")
	}
	if meta, err := s.LoadMeta("job1"); err != nil || meta.Name != "before" {
		t.Errorf("a failed save must leave the old metadata whole: %+v, %v", meta, err)
	}
}

func TestStore_SaveMeta_ReplacesInOneStep(t *testing.T) {
	s := New(t.TempDir())
	if err := s.CreateJob(JobMeta{ID: "job1", Name: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveMeta(JobMeta{ID: "job1", Name: "b"}); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(s.jobDir("job1"))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 2 || names[0] != "meta.json" || names[1] != "pages" {
		t.Errorf("job directory holds %v: no temporary file should be left", names)
	}
	if info, err := os.Stat(s.metaPath("job1")); err != nil || info.Mode().Perm() != 0o644 {
		t.Errorf("meta.json mode %v, %v: want 0644", info.Mode().Perm(), err)
	}
}

func TestReplaceFile_Errors(t *testing.T) {
	dir := t.TempDir()
	if err := replaceFile(filepath.Join(dir, "missing", "f"), []byte("x")); err == nil {
		t.Error("a missing directory should fail")
	}
	// Renaming a file over a directory fails, after the temporary file
	// was written; it must not be left behind.
	if err := os.Mkdir(filepath.Join(dir, "taken"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "taken", "x"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := replaceFile(filepath.Join(dir, "taken"), []byte("x")); err == nil {
		t.Error("renaming over a non-empty directory should fail")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("left behind: %v", entries)
	}
}

func TestStore_DeleteJob_Error(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission-based test is unreliable")
	}
	s := New(t.TempDir())
	if err := s.CreateJob(JobMeta{ID: "job1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePageFile("job1", "001f.jpg", []byte("x")); err != nil {
		t.Fatal(err)
	}
	// Removing read permission on the job dir itself prevents RemoveAll
	// from listing/removing its contents.
	if err := os.Chmod(s.jobDir("job1"), 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(s.jobDir("job1"), 0o755)

	if err := s.DeleteJob("job1"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestStore_CreateAndLoadMeta(t *testing.T) {
	s := New(t.TempDir())
	meta := JobMeta{
		ID:     "20260714-153045-abcd",
		Name:   "Scan 2026-07-14 15:30",
		Driver: "doxie-dx400",
		Status: StatusRunning,
	}
	if err := s.CreateJob(meta); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	got, err := s.LoadMeta(meta.ID)
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if got.ID != meta.ID || got.Name != meta.Name || got.Status != meta.Status {
		t.Errorf("got %+v, want %+v", got, meta)
	}
}

func TestStore_LoadMeta_NotFound(t *testing.T) {
	s := New(t.TempDir())
	if _, err := s.LoadMeta("does-not-exist"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestStore_LoadMeta_CorruptJSON(t *testing.T) {
	s := New(t.TempDir())
	meta := JobMeta{ID: "job1"}
	if err := s.CreateJob(meta); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.metaPath("job1"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadMeta("job1"); err == nil {
		t.Fatal("expected an error for corrupt JSON")
	}
}

func TestStore_LoadMeta_PagesWithAndWithoutInk(t *testing.T) {
	s := New(t.TempDir())
	if err := s.CreateJob(JobMeta{ID: "job1"}); err != nil {
		t.Fatal(err)
	}
	before := `{"id":"job1","status":"completed","pages":[{"id":"001b","file":"001b.jpg","sheet":1,"side":"back","blank":true,"hidden":true,"edits":{}}]}`
	if err := os.WriteFile(s.metaPath("job1"), []byte(before), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := s.LoadMeta("job1")
	if err != nil || len(got.Pages) != 1 || got.Pages[0].Ink != nil || !got.Pages[0].Hidden {
		t.Fatalf("a scan saved before ink was recorded: %+v, %v", got, err)
	}

	got.Pages[0].Ink = &pageimage.Ink{Sheet: 0.0179, Densest: 0.193}
	if err := s.SaveMeta(got); err != nil {
		t.Fatal(err)
	}
	if again, err := s.LoadMeta("job1"); err != nil || again.Pages[0].Ink == nil || *again.Pages[0].Ink != *got.Pages[0].Ink {
		t.Errorf("ink didn't round-trip: %+v, %v", again.Pages[0].Ink, err)
	}
}

func TestStore_SaveMeta_Overwrites(t *testing.T) {
	s := New(t.TempDir())
	meta := JobMeta{ID: "job1", Name: "original"}
	if err := s.CreateJob(meta); err != nil {
		t.Fatal(err)
	}
	meta.Name = "renamed"
	if err := s.SaveMeta(meta); err != nil {
		t.Fatalf("SaveMeta: %v", err)
	}
	got, err := s.LoadMeta("job1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "renamed" {
		t.Errorf("got %q, want %q", got.Name, "renamed")
	}
}

func TestStore_ListJobs_SortedNewestFirst(t *testing.T) {
	s := New(t.TempDir())
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	ids := []string{"job-a", "job-b", "job-c"}
	times := []time.Time{base, base.Add(2 * time.Hour), base.Add(1 * time.Hour)}
	for i, id := range ids {
		if err := s.CreateJob(JobMeta{ID: id, CreatedAt: times[i]}); err != nil {
			t.Fatal(err)
		}
	}

	jobs, err := s.ListJobs()
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 3 {
		t.Fatalf("got %d jobs, want 3", len(jobs))
	}
	// job-b (base+2h) should be newest, then job-c (base+1h), then job-a (base).
	want := []string{"job-b", "job-c", "job-a"}
	for i, id := range want {
		if jobs[i].ID != id {
			t.Errorf("position %d: got %q, want %q", i, jobs[i].ID, id)
		}
	}
}

func TestStore_ListJobs_EmptyWhenNoJobsDir(t *testing.T) {
	s := New(t.TempDir())
	jobs, err := s.ListJobs()
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs, got %d", len(jobs))
	}
}

func TestStore_ListJobs_SkipsCorruptJobDirs(t *testing.T) {
	s := New(t.TempDir())
	if err := s.CreateJob(JobMeta{ID: "good-job"}); err != nil {
		t.Fatal(err)
	}
	// A directory with no meta.json at all.
	if err := os.MkdirAll(filepath.Join(s.jobsDir(), "broken-job"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A non-directory entry directly inside jobs/.
	if err := os.WriteFile(filepath.Join(s.jobsDir(), "stray-file"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	jobs, err := s.ListJobs()
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].ID != "good-job" {
		t.Errorf("expected only good-job, got %+v", jobs)
	}
}

func TestStore_ListJobs_Error(t *testing.T) {
	// Root itself is a file, not a directory: os.ReadDir on jobs/ fails
	// with something other than "not exist".
	root := filepath.Join(t.TempDir(), "root-as-file")
	if err := os.WriteFile(root, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(root)
	if _, err := s.ListJobs(); err == nil {
		t.Fatal("expected an error")
	}
}

func TestStore_PageFileRoundTrip(t *testing.T) {
	s := New(t.TempDir())
	if err := s.CreateJob(JobMeta{ID: "job1"}); err != nil {
		t.Fatal(err)
	}
	data := []byte("fake jpeg bytes")
	if err := s.SavePageFile("job1", "001f.jpg", data); err != nil {
		t.Fatalf("SavePageFile: %v", err)
	}
	got, err := s.LoadPageFile("job1", "001f.jpg")
	if err != nil {
		t.Fatalf("LoadPageFile: %v", err)
	}
	if string(got) != string(data) {
		t.Errorf("got %q, want %q", got, data)
	}

	if err := s.DeletePageFile("job1", "001f.jpg"); err != nil {
		t.Fatalf("DeletePageFile: %v", err)
	}
	if _, err := s.LoadPageFile("job1", "001f.jpg"); err == nil {
		t.Fatal("expected an error after deletion")
	}
}

func TestStore_SavePageFile_Error(t *testing.T) {
	s := New(t.TempDir())
	// Job directory was never created, so pages/ doesn't exist.
	if err := s.SavePageFile("no-such-job", "001f.jpg", []byte("x")); err == nil {
		t.Fatal("expected an error")
	}
}

func TestStore_DeletePageFile_Error(t *testing.T) {
	s := New(t.TempDir())
	if err := s.DeletePageFile("no-such-job", "001f.jpg"); err == nil {
		t.Fatal("expected an error")
	}
}

func TestStore_DeleteJob(t *testing.T) {
	s := New(t.TempDir())
	if err := s.CreateJob(JobMeta{ID: "job1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteJob("job1"); err != nil {
		t.Fatalf("DeleteJob: %v", err)
	}
	if _, err := s.LoadMeta("job1"); err == nil {
		t.Fatal("expected job to be gone")
	}
}

func TestStore_CreateJob_Error(t *testing.T) {
	// Root itself is a file: MkdirAll for the pages dir must fail.
	root := filepath.Join(t.TempDir(), "root-as-file")
	if err := os.WriteFile(root, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := New(root)
	if err := s.CreateJob(JobMeta{ID: "job1"}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestStore_Root(t *testing.T) {
	s := New("/some/path")
	if s.Root() != "/some/path" {
		t.Errorf("got %q", s.Root())
	}
}

func TestStore_RenderCache(t *testing.T) {
	s := New(t.TempDir())
	if err := s.CreateJob(JobMeta{ID: "job1"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.LoadRender("job1", "001f", "k1"); ok {
		t.Fatal("nothing cached yet")
	}
	for _, key := range []string{"k1", "k2"} {
		if err := s.SaveRender("job1", "001f", key, []byte(key)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveRender("job1", "002f", "k1", []byte("other page")); err != nil {
		t.Fatal(err)
	}
	if got, ok := s.LoadRender("job1", "001f", "k2"); !ok || string(got) != "k2" {
		t.Errorf("LoadRender: got %q, %v", got, ok)
	}

	if err := s.DeleteRenders("job1", "001f"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.LoadRender("job1", "001f", "k1"); ok {
		t.Error("renders of 001f should be gone")
	}
	if _, ok := s.LoadRender("job1", "002f", "k1"); !ok {
		t.Error("other pages' renders must survive")
	}
}

func TestStore_DeletePageFileDropsItsRenders(t *testing.T) {
	s := New(t.TempDir())
	if err := s.CreateJob(JobMeta{ID: "job1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SavePageFile("job1", "001f.jpg", []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRender("job1", "001f", "thumb", []byte("t")); err != nil {
		t.Fatal(err)
	}
	if err := s.DeletePageFile("job1", "001f.jpg"); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.LoadRender("job1", "001f", "thumb"); ok {
		t.Error("deleting a page should drop its cached renders")
	}
}

func TestStore_SaveRender_Error(t *testing.T) {
	s := New(t.TempDir())
	if err := os.WriteFile(filepath.Join(s.Root(), "jobs"), []byte("not a dir"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRender("job1", "001f", "k", []byte("x")); err == nil {
		t.Fatal("expected an error when the cache directory can't be created")
	}
}

func TestStore_DeleteRenders_Error(t *testing.T) {
	s := New(t.TempDir())
	if err := s.CreateJob(JobMeta{ID: "job1"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveRender("job1", "001f", "k", []byte("x")); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(s.Root(), "jobs", "job1", "pages", ".render")
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	if err := s.DeleteRenders("job1", "001f"); err == nil {
		t.Fatal("expected an error when a cached file can't be removed")
	}
}

func TestStore_Settings(t *testing.T) {
	s := New(t.TempDir())
	if got, err := s.LoadSettings(); err != nil || got != (AppSettings{}) {
		t.Fatalf("no file yet should give zero settings, got %+v %v", got, err)
	}
	want := AppSettings{ButtonScan: ScanSettings{Resolution: 600, Sides: "double", Color: "gray"}}
	if err := s.SaveSettings(want); err != nil {
		t.Fatal(err)
	}
	if got, err := s.LoadSettings(); err != nil || got != want {
		t.Errorf("round trip: got %+v %v", got, err)
	}

	if err := os.WriteFile(filepath.Join(s.Root(), "settings.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadSettings(); err == nil {
		t.Error("corrupt settings should fail to load")
	}
	if err := os.Chmod(filepath.Join(s.Root(), "settings.json"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(filepath.Join(s.Root(), "settings.json"), 0o644) })
	if _, err := s.LoadSettings(); err == nil {
		t.Error("unreadable settings should fail to load")
	}
	if err := s.SaveSettings(want); err == nil {
		t.Error("unwritable settings should fail to save")
	}
}

func TestStore_UpdateMeta(t *testing.T) {
	s := New(t.TempDir())
	if err := s.CreateJob(JobMeta{ID: "job1", Name: "a"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.UpdateMeta("job1", func(m *JobMeta) error { m.Name = "b"; return nil })
	if err != nil || got.Name != "b" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if saved, _ := s.LoadMeta("job1"); saved.Name != "b" {
		t.Error("the change should be saved")
	}

	stop := errors.New("rejected")
	if _, err := s.UpdateMeta("job1", func(m *JobMeta) error { m.Name = "c"; return stop }); !errors.Is(err, stop) {
		t.Errorf("a rejected change returns its error, got %v", err)
	}
	if saved, _ := s.LoadMeta("job1"); saved.Name != "b" {
		t.Error("a rejected change must not be saved")
	}
	if _, err := s.UpdateMeta("nope", func(*JobMeta) error { return nil }); !errors.Is(err, ErrJobNotFound) {
		t.Errorf("unknown job: got %v", err)
	}

	if err := os.Chmod(s.jobDir("job1"), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(s.jobDir("job1"), 0o755) })
	if _, err := s.UpdateMeta("job1", func(*JobMeta) error { return nil }); err == nil {
		t.Error("a failed save should surface")
	}
}

// combineFixture saves two jobs with one original each and a cached
// render, and returns the store.
func combineFixture(t *testing.T) *Store {
	t.Helper()
	s := New(t.TempDir())
	t0 := time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)
	into := layout("1f 1b-")
	into.ID, into.CreatedAt = "into", t0
	from := layout("1f")
	from.ID, from.CreatedAt = "from", t0.Add(time.Minute)
	for _, j := range []JobMeta{into, from} {
		if err := s.CreateJob(j); err != nil {
			t.Fatal(err)
		}
		for _, p := range j.Pages {
			if err := s.SavePageFile(j.ID, p.File, []byte(j.ID+"/"+p.ID)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := s.SaveRender("into", "002f", "k", []byte("stale")); err != nil {
		t.Fatal(err)
	}
	return s
}

func noCheck(JobMeta, JobMeta) error { return nil }

func TestStore_CombineJobs(t *testing.T) {
	s := combineFixture(t)
	got, err := s.CombineJobs("into", "from", noCheck)
	if err != nil || describe(got) != "1f 1b- 2f" {
		t.Fatalf("got %q, %v", describe(got), err)
	}
	if saved, _ := s.LoadMeta("into"); describe(saved) != "1f 1b- 2f" {
		t.Errorf("saved %q", describe(saved))
	}
	if data, err := s.LoadPageFile("into", "002f.jpg"); err != nil || string(data) != "from/001f" {
		t.Errorf("the incoming original: %q, %v", data, err)
	}
	if _, ok := s.LoadRender("into", "002f", "k"); ok {
		t.Error("a render left under the incoming page's name must be dropped")
	}
	if _, err := s.LoadMeta("from"); err == nil {
		t.Error("the combined-away job should be deleted")
	}
}

func TestStore_CombineJobs_CopiesWhereItCantLink(t *testing.T) {
	s := combineFixture(t)
	linkFile = func(string, string) error { return errors.New("no links here") }
	t.Cleanup(func() { linkFile = os.Link })
	if _, err := s.CombineJobs("into", "from", noCheck); err != nil {
		t.Fatal(err)
	}
	if data, _ := s.LoadPageFile("into", "002f.jpg"); string(data) != "from/001f" {
		t.Errorf("copied original: %q", data)
	}
}

// unchanged checks a refused combine left both jobs as they were.
func unchanged(t *testing.T, s *Store) {
	t.Helper()
	if into, _ := s.LoadMeta("into"); describe(into) != "1f 1b-" {
		t.Errorf("into changed: %q", describe(into))
	}
	if from, err := s.LoadMeta("from"); err != nil || describe(from) != "1f" {
		t.Errorf("from changed: %q, %v", describe(from), err)
	}
	if _, err := s.LoadPageFile("into", "002f.jpg"); err == nil {
		t.Error("an incoming original was left behind")
	}
}

func TestStore_CombineJobs_Refusals(t *testing.T) {
	s := combineFixture(t)
	if _, err := s.CombineJobs("nope", "from", noCheck); !errors.Is(err, ErrJobNotFound) {
		t.Errorf("unknown into: %v", err)
	}
	if _, err := s.CombineJobs("into", "nope", noCheck); !errors.Is(err, ErrJobNotFound) {
		t.Errorf("unknown from: %v", err)
	}
	stop := errors.New("refused")
	if _, err := s.CombineJobs("into", "from", func(JobMeta, JobMeta) error { return stop }); !errors.Is(err, stop) {
		t.Errorf("check: %v", err)
	}
	unchanged(t, s)
}

func TestStore_CombineJobs_MissingOriginal(t *testing.T) {
	s := combineFixture(t)
	from, _ := s.LoadMeta("from")
	from.Pages = append(from.Pages, layout("2f").Pages...)
	if err := s.SaveMeta(from); err != nil {
		t.Fatal(err)
	}
	linkFile = func(string, string) error { return errors.New("no links here") }
	t.Cleanup(func() { linkFile = os.Link })
	if _, err := s.CombineJobs("into", "from", noCheck); err == nil {
		t.Fatal("an original that can't be read must stop the combine")
	}
	if into, _ := s.LoadMeta("into"); describe(into) != "1f 1b-" {
		t.Errorf("into changed: %q", describe(into))
	}
	for _, f := range []string{"002f.jpg", "003f.jpg"} {
		if _, err := s.LoadPageFile("into", f); err == nil {
			t.Errorf("%s was left behind", f)
		}
	}
}

func TestStore_CombineJobs_SaveFailureChangesNothing(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission-based test is unreliable")
	}
	s := combineFixture(t)
	if err := os.Chmod(s.jobDir("into"), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(s.jobDir("into"), 0o755) })
	if _, err := s.CombineJobs("into", "from", noCheck); err == nil || errors.Is(err, ErrLeftBehind) {
		t.Fatalf("got %v, want the save's error", err)
	}
	unchanged(t, s)
}

func TestStore_CombineJobs_LeftBehind(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: permission-based test is unreliable")
	}
	s := combineFixture(t)
	if err := os.Chmod(s.jobDir("from"), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(s.jobDir("from"), 0o755) })
	got, err := s.CombineJobs("into", "from", noCheck)
	if !errors.Is(err, ErrLeftBehind) || describe(got) != "1f 1b- 2f" {
		t.Fatalf("got %q, %v: want the combined job and ErrLeftBehind", describe(got), err)
	}
	if _, err := s.LoadMeta("from"); err != nil {
		t.Error("a job that couldn't be deleted is still there, so no page is lost")
	}
}
