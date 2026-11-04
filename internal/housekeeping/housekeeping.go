// Package housekeeping follows uploads to Paperless and removes scans
// once Paperless has filed every page of them and the retention period
// has passed. It runs whether or not anyone has the app open.
package housekeeping

import (
	"context"
	"log"
	"slices"
	"time"

	"github.com/andiapps-dev/doxie-scanner/internal/paperless"
	"github.com/andiapps-dev/doxie-scanner/internal/storage"
)

// DefaultRetentionDays applies until the user picks something else.
const DefaultRetentionDays = 7

// RetentionDays is how long imported scans are kept; 0 keeps them forever.
func RetentionDays(s storage.AppSettings) int {
	if s.RetentionDays == nil {
		return DefaultRetentionDays
	}
	return *s.RetentionDays
}

// Keeper does the housekeeping for one store.
type Keeper struct {
	Store     *storage.Store
	Paperless *paperless.Client // nil when sending isn't set up
	// Scanning reports whether a job is still being scanned; such jobs
	// are never touched.
	Scanning func(jobID string) bool
	Now      func() time.Time
}

func pending(s storage.SentDocument) bool {
	return s.TaskID != "" && (s.Status == paperless.TaskQueued || s.Status == paperless.TaskProcessing)
}

// Waiting reports whether any of a job's documents is still on its way:
// being handed to Paperless, or handed over and not yet filed.
func Waiting(m storage.JobMeta) bool {
	for _, s := range m.Sent {
		if s.Status == storage.SentSending || pending(s) {
			return true
		}
	}
	return false
}

// ErrInterrupted is recorded for documents the app stopped before it
// finished handing them to Paperless.
const ErrInterrupted = "the scanner app restarted before this reached Paperless: send it again"

// FailInterrupted marks documents left mid-send by an earlier run as
// failed, so they can be sent again. Call it before serving requests.
func (k *Keeper) FailInterrupted() {
	jobs, err := k.Store.ListJobs()
	if err != nil {
		log.Printf("housekeeping: %v", err)
		return
	}
	for _, job := range jobs {
		if !slices.ContainsFunc(job.Sent, func(s storage.SentDocument) bool { return s.Status == storage.SentSending }) {
			continue
		}
		_, err := k.Store.UpdateMeta(job.ID, func(m *storage.JobMeta) error {
			for i := range m.Sent {
				if m.Sent[i].Status == storage.SentSending {
					m.Sent[i].Status, m.Sent[i].Error = paperless.TaskFailed, ErrInterrupted
				}
			}
			return nil
		})
		if err != nil {
			log.Printf("housekeeping: %s: %v", job.ID, err)
		}
	}
}

// RefreshSent asks Paperless about a job's pending uploads and records
// the answers.
func (k *Keeper) RefreshSent(ctx context.Context, jobID string) (storage.JobMeta, error) {
	meta, err := k.Store.LoadMeta(jobID)
	if err != nil {
		return meta, err
	}
	updates := map[string]paperless.Task{}
	for _, s := range meta.Sent {
		if !pending(s) {
			continue
		}
		task, err := k.Paperless.Task(ctx, s.TaskID)
		if err != nil {
			return meta, err
		}
		updates[s.TaskID] = task
	}
	if len(updates) == 0 {
		return meta, nil
	}
	now := k.Now()
	return k.Store.UpdateMeta(jobID, func(m *storage.JobMeta) error {
		for i, s := range m.Sent {
			task, found := updates[s.TaskID]
			if !found {
				continue
			}
			m.Sent[i].Status, m.Sent[i].DocumentID = task.Status, task.DocumentID
			switch task.Status {
			case paperless.TaskFailed:
				m.Sent[i].Error = task.Message
			case paperless.TaskDone:
				if m.Sent[i].FiledAt == nil {
					m.Sent[i].FiledAt = &now
				}
			}
		}
		return nil
	})
}

// Imported reports whether every page kept in a job went into a document
// Paperless has filed, and when the last of them was filed. Pages the
// user hid don't need to be sent; an upload still in progress means the
// answer isn't in yet.
func Imported(m storage.JobMeta) (time.Time, bool) {
	if m.Status == storage.StatusRunning || len(m.Sent) == 0 {
		return time.Time{}, false
	}
	filedPages := map[string]bool{}
	var last time.Time
	if Waiting(m) {
		return time.Time{}, false
	}
	for _, s := range m.Sent {
		if s.Status != paperless.TaskDone {
			continue
		}
		for _, id := range s.Pages {
			filedPages[id] = true
		}
		filed := s.SentAt
		if s.FiledAt != nil {
			filed = *s.FiledAt
		}
		if filed.After(last) {
			last = filed
		}
	}
	for _, p := range m.VisiblePages() {
		if !filedPages[p.ID] {
			return time.Time{}, false
		}
	}
	return last, len(filedPages) > 0
}

// RemoveAt is when an imported job is due to be deleted, if it is.
func RemoveAt(m storage.JobMeta, days int) (time.Time, bool) {
	filed, ok := Imported(m)
	if !ok || days <= 0 {
		return time.Time{}, false
	}
	return filed.Add(time.Duration(days) * 24 * time.Hour), true
}

// Sweep follows pending uploads, then deletes imported scans whose
// retention period has passed.
func (k *Keeper) Sweep(ctx context.Context) {
	settings, err := k.Store.LoadSettings()
	if err != nil {
		log.Printf("housekeeping: %v", err)
		return
	}
	days := RetentionDays(settings)
	jobs, err := k.Store.ListJobs()
	if err != nil {
		log.Printf("housekeeping: %v", err)
		return
	}
	for _, job := range jobs {
		if k.Scanning(job.ID) {
			continue
		}
		if k.Paperless != nil {
			if refreshed, err := k.RefreshSent(ctx, job.ID); err == nil {
				job = refreshed
			}
		}
		if due, ok := RemoveAt(job, days); ok && !k.Now().Before(due) {
			if err := k.Store.DeleteJob(job.ID); err != nil {
				log.Printf("housekeeping: delete %s: %v", job.ID, err)
				continue
			}
			log.Printf("housekeeping: removed %s (%q), filed in Paperless more than %d days ago", job.ID, job.Name, days)
		}
	}
}

// Run sweeps every interval until ctx ends.
func (k *Keeper) Run(ctx context.Context, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			k.Sweep(ctx)
		}
	}
}
