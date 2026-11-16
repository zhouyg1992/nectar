package oncall

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const storeVersion = 1

type storeFile struct {
	Version int     `json:"version"`
	Watches []Watch `json:"watches"`
	Jobs    []Job   `json:"jobs"`
}

// Store persists watches and jobs. The file contains operational metadata only;
// repository contents and diffs live under the job artifact directory.
type Store struct {
	mu      sync.RWMutex
	path    string
	watches map[string]Watch
	jobs    map[string]Job
}

func NewStore(path string) (*Store, error) {
	s := &Store{
		path:    path,
		watches: make(map[string]Watch),
		jobs:    make(map[string]Job),
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	if s.path == "" {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read on-call store: %w", err)
	}
	var file storeFile
	if err := json.Unmarshal(data, &file); err != nil {
		return fmt.Errorf("parse on-call store: %w", err)
	}
	if file.Version != 0 && file.Version != storeVersion {
		return fmt.Errorf("unsupported on-call store version %d", file.Version)
	}
	for _, watch := range file.Watches {
		if watch.ID == "" {
			continue
		}
		s.watches[watch.ID] = watch
	}
	for _, job := range file.Jobs {
		if job.ID == "" {
			continue
		}
		s.jobs[job.ID] = job
	}
	return nil
}

func (s *Store) AddWatch(watch Watch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.watches[watch.ID]; exists {
		return fmt.Errorf("on-call watch %s already exists", watch.ID)
	}
	s.watches[watch.ID] = watch
	return s.persistLocked()
}

func (s *Store) UpdateWatch(watch Watch) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.watches[watch.ID]; !exists {
		return fmt.Errorf("on-call watch %s not found", watch.ID)
	}
	s.watches[watch.ID] = watch
	return s.persistLocked()
}

func (s *Store) GetWatch(id string) (Watch, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	watch, ok := s.watches[id]
	return watch, ok
}

func (s *Store) ListWatches(includePaused bool) []Watch {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Watch, 0, len(s.watches))
	for _, watch := range s.watches {
		if !includePaused && watch.Status != WatchActive {
			continue
		}
		out = append(out, watch)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func (s *Store) DeleteWatch(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.watches[id]; !exists {
		return fmt.Errorf("on-call watch %s not found", id)
	}
	delete(s.watches, id)
	return s.persistLocked()
}

func (s *Store) AddJob(job Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.jobs[job.ID]; exists {
		return fmt.Errorf("on-call job %s already exists", job.ID)
	}
	s.jobs[job.ID] = job
	return s.persistLocked()
}

func (s *Store) UpdateJob(job Job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.jobs[job.ID]; !exists {
		return fmt.Errorf("on-call job %s not found", job.ID)
	}
	s.jobs[job.ID] = job
	return s.persistLocked()
}

func (s *Store) GetJob(id string) (Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[id]
	return job, ok
}

func (s *Store) FindJobByRun(watchID string, runID int64) (Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, job := range s.jobs {
		if job.WatchID == watchID && job.RunID == runID {
			return job, true
		}
	}
	return Job{}, false
}

func (s *Store) ListJobs(limit int) []Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Job, 0, len(s.jobs))
	for _, job := range s.jobs {
		out = append(out, job)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *Store) Report(since, until time.Time) Report {
	s.mu.RLock()
	defer s.mu.RUnlock()
	report := Report{Since: since, Until: until}
	for _, job := range s.jobs {
		if job.CreatedAt.Before(since) || job.CreatedAt.After(until) {
			continue
		}
		report.Total++
		switch job.Status {
		case JobCompleted:
			report.Completed++
		case JobFailed:
			report.Failed++
		case JobRunning, JobQueued:
			report.Running++
		case JobCanceled:
			report.Canceled++
		}
		report.Jobs = append(report.Jobs, job)
	}
	sort.Slice(report.Jobs, func(i, j int) bool {
		return report.Jobs[i].CreatedAt.After(report.Jobs[j].CreatedAt)
	})
	return report
}

// MarkInterruptedJobs fails jobs that were queued or running when Nectar
// stopped. They cannot be resumed safely because their engine process is gone.
func (s *Store) MarkInterruptedJobs(now time.Time) ([]Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var changed []Job
	for id, job := range s.jobs {
		if job.Status != JobQueued && job.Status != JobRunning {
			continue
		}
		job.Status = JobFailed
		job.Error = "nectar restarted while the on-call job was running"
		job.FinishedAt = timePtr(now)
		s.jobs[id] = job
		changed = append(changed, job)
	}
	if len(changed) == 0 {
		return nil, nil
	}
	return changed, s.persistLocked()
}

func (s *Store) persistLocked() error {
	if s.path == "" {
		return nil
	}
	file := storeFile{Version: storeVersion}
	for _, watch := range s.watches {
		file.Watches = append(file.Watches, watch)
	}
	for _, job := range s.jobs {
		file.Jobs = append(file.Jobs, job)
	}
	sort.Slice(file.Watches, func(i, j int) bool { return file.Watches[i].ID < file.Watches[j].ID })
	sort.Slice(file.Jobs, func(i, j int) bool { return file.Jobs[i].ID < file.Jobs[j].ID })
	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal on-call store: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create on-call store directory: %w", err)
	}
	temp, err := os.CreateTemp(dir, ".oncall-*.tmp")
	if err != nil {
		return fmt.Errorf("create on-call temp file: %w", err)
	}
	tempPath := temp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("chmod on-call temp file: %w", err)
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write on-call store: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close on-call store: %w", err)
	}
	if err := os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("replace on-call store: %w", err)
	}
	cleanup = false
	return nil
}

func timePtr(t time.Time) *time.Time { return &t }
