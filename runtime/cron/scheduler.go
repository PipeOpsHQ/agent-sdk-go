package cron

import (
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	robcron "github.com/robfig/cron/v3"
)

// Scheduler manages recurring agent jobs using cron expressions.
type Scheduler struct {
	mu      sync.RWMutex
	cron    *robcron.Cron
	jobs    map[string]*managedJob
	runFunc RunFunc
	started bool
	maxRuns int
}

type managedJob struct {
	Job
	entryID robcron.EntryID
	runs    []JobRun
}

// New creates a new Scheduler. The runFunc is invoked for each triggered job.
func New(runFunc RunFunc) *Scheduler {
	return &Scheduler{
		cron:    robcron.New(),
		jobs:    make(map[string]*managedJob),
		runFunc: runFunc,
		maxRuns: 100,
	}
}

// Add registers a new scheduled job. Returns error if name is duplicate or
// cron expression is invalid.
func (s *Scheduler) Add(name, cronExpr string, cfg JobConfig) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if name == "" {
		return fmt.Errorf("job name is required")
	}
	if _, exists := s.jobs[name]; exists {
		return fmt.Errorf("job %q already exists", name)
	}

	entryID, err := s.cron.AddFunc(cronExpr, func() {
		s.executeJob(name)
	})
	if err != nil {
		return fmt.Errorf("invalid cron expression %q: %w", cronExpr, err)
	}

	mj := &managedJob{
		Job: Job{
			Name:     name,
			CronExpr: cronExpr,
			Config:   cfg,
			Enabled:  true,
		},
		entryID: entryID,
	}

	entry := s.cron.Entry(entryID)
	if !entry.Next.IsZero() {
		mj.NextRun = entry.Next
	}

	s.jobs[name] = mj
	return nil
}

func (s *Scheduler) executeJob(name string) {
	_, _ = s.runAndRecord(name, "schedule", true)
}

// Remove deletes a scheduled job by name.
func (s *Scheduler) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	mj, ok := s.jobs[name]
	if !ok {
		return fmt.Errorf("job %q not found", name)
	}
	s.cron.Remove(mj.entryID)
	delete(s.jobs, name)
	return nil
}

// List returns all registered jobs sorted by name.
func (s *Scheduler) List() []Job {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Job, 0, len(s.jobs))
	for _, mj := range s.jobs {
		j := mj.Job
		entry := s.cron.Entry(mj.entryID)
		if !entry.Next.IsZero() {
			j.NextRun = entry.Next
		}
		out = append(out, j)
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Name < out[k].Name })
	return out
}

// Get returns a single job by name.
func (s *Scheduler) Get(name string) (Job, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	mj, ok := s.jobs[name]
	if !ok {
		return Job{}, false
	}
	j := mj.Job
	entry := s.cron.Entry(mj.entryID)
	if !entry.Next.IsZero() {
		j.NextRun = entry.Next
	}
	return j, true
}

// SetEnabled enables or disables a job without removing it.
func (s *Scheduler) SetEnabled(name string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	mj, ok := s.jobs[name]
	if !ok {
		return fmt.Errorf("job %q not found", name)
	}
	mj.Enabled = enabled
	return nil
}

// Trigger manually executes a job immediately, regardless of its schedule.
func (s *Scheduler) Trigger(name string) (string, error) {
	return s.runAndRecord(name, "manual", false)
}

// History returns recent run history for a job.
func (s *Scheduler) History(name string, limit int) ([]JobRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	mj, ok := s.jobs[name]
	if !ok {
		return nil, fmt.Errorf("job %q not found", name)
	}
	if limit <= 0 || limit > len(mj.runs) {
		limit = len(mj.runs)
	}
	out := make([]JobRun, 0, limit)
	for i := len(mj.runs) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, mj.runs[i])
	}
	return out, nil
}

func (s *Scheduler) runAndRecord(name, trigger string, skipIfDisabled bool) (string, error) {
	s.mu.RLock()
	mj, ok := s.jobs[name]
	if !ok {
		s.mu.RUnlock()
		return "", fmt.Errorf("job %q not found", name)
	}
	if skipIfDisabled && !mj.Enabled {
		s.mu.RUnlock()
		return "", nil
	}
	cfg := mj.Config
	s.mu.RUnlock()

	started := time.Now()
	output, err := s.runFunc(cfg)
	finished := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
	mj2, ok := s.jobs[name]
	if !ok {
		return output, err
	}
	mj2.LastRun = finished
	mj2.RunCount++
	run := JobRun{
		At:         finished,
		DurationMS: finished.Sub(started).Milliseconds(),
		Trigger:    trigger,
	}
	if err != nil {
		mj2.LastErr = err.Error()
		run.Status = "failed"
		run.Error = err.Error()
		log.Printf("[cron] job %q failed (%s): %v", name, trigger, err)
	} else {
		mj2.LastErr = ""
		run.Status = "completed"
		run.Output = truncate(output, 2000)
		log.Printf("[cron] job %q completed (%s): %s", name, trigger, truncate(output, 100))
	}
	mj2.runs = append(mj2.runs, run)
	if s.maxRuns > 0 && len(mj2.runs) > s.maxRuns {
		mj2.runs = mj2.runs[len(mj2.runs)-s.maxRuns:]
	}
	entry := s.cron.Entry(mj2.entryID)
	if !entry.Next.IsZero() {
		mj2.NextRun = entry.Next
	}
	return output, err
}

// Start begins the cron scheduler. Non-blocking.
func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		s.cron.Start()
		s.started = true
	}
}

// Stop gracefully stops the scheduler.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		s.cron.Stop()
		s.started = false
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
