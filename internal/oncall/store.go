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

const storeVersion = 2

type storeFile struct {
	Version int     `json:"version"`
	Shifts  []Shift `json:"shifts"`
	Jobs    []Job   `json:"jobs"`
}

type Store struct {
	mu     sync.RWMutex
	path   string
	shifts map[string]Shift
	jobs   map[string]Job
}

func NewStore(path string) (*Store, error) {
	s := &Store{
		path:   path,
		shifts: make(map[string]Shift),
		jobs:   make(map[string]Job),
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
	for _, shift := range file.Shifts {
		if shift.ID != "" {
			s.shifts[shift.ID] = shift
		}
	}
	for _, job := range file.Jobs {
		if job.ID != "" {
			s.jobs[job.ID] = job
		}
	}
	return nil
}

func (s *Store) AddShift(shift Shift) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.shifts[shift.ID]; exists {
		return fmt.Errorf("on-call shift %s already exists", shift.ID)
	}
	s.shifts[shift.ID] = shift
	return s.persistLocked()
}

func (s *Store) UpdateShift(shift Shift) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.shifts[shift.ID]; !exists {
		return fmt.Errorf("on-call shift %s not found", shift.ID)
	}
	s.shifts[shift.ID] = shift
	return s.persistLocked()
}

func (s *Store) GetShift(id string) (Shift, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	shift, ok := s.shifts[id]
	return shift, ok
}

func (s *Store) ListShifts(includeInactive bool) []Shift {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Shift, 0, len(s.shifts))
	for _, shift := range s.shifts {
		if !includeInactive && shift.Status != ShiftActive {
			continue
		}
		out = append(out, shift)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out
}

func (s *Store) DeleteShift(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.shifts[id]; !exists {
		return fmt.Errorf("on-call shift %s not found", id)
	}
	delete(s.shifts, id)
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

func (s *Store) Report(shiftID string, since, until time.Time) Report {
	s.mu.RLock()
	defer s.mu.RUnlock()
	report := Report{Since: since, Until: until}
	if shiftID != "" {
		report.Shift = s.shifts[shiftID]
	}
	for _, job := range s.jobs {
		if shiftID != "" && job.ShiftID != shiftID {
			continue
		}
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
	if shift, ok := s.shifts[shiftID]; ok {
		report.Incidents = shift.Incidents
	}
	return report
}

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
	for _, shift := range s.shifts {
		file.Shifts = append(file.Shifts, shift)
	}
	for _, job := range s.jobs {
		file.Jobs = append(file.Jobs, job)
	}
	sort.Slice(file.Shifts, func(i, j int) bool { return file.Shifts[i].ID < file.Shifts[j].ID })
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
