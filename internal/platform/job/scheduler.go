package job

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type JobFunc func(context.Context) error

type Scheduler struct {
	mu      sync.Mutex
	jobs    map[string]*scheduledJob
	started bool
	ctx     context.Context
	cancel  context.CancelFunc
}

type scheduledJob struct {
	mu            sync.Mutex
	name          string
	interval      time.Duration
	fn            JobFunc
	stop          context.CancelFunc
	running       bool
	lastStart     time.Time
	lastFinish    time.Time
	lastError     string
	retryDelay    time.Duration
	maxRetryCount int
}

func NewScheduler() *Scheduler {
	return &Scheduler{jobs: make(map[string]*scheduledJob)}
}

func (s *Scheduler) Every(name string, interval time.Duration, fn JobFunc) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.jobs[name]; exists {
		return fmt.Errorf("job %q already registered", name)
	}
	s.jobs[name] = &scheduledJob{name: name, interval: interval, fn: fn, retryDelay: 2 * time.Second, maxRetryCount: 2}
	if s.started {
		s.startJobLocked(s.jobs[name])
	}
	return nil
}

func (s *Scheduler) Start(parent context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started {
		return
	}
	s.ctx, s.cancel = context.WithCancel(parent)
	s.started = true
	for _, job := range s.jobs {
		s.startJobLocked(job)
	}
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.started {
		return
	}
	s.cancel()
	for _, job := range s.jobs {
		if job.stop != nil {
			job.stop()
			job.stop = nil
		}
	}
	s.started = false
}

func (s *Scheduler) startJobLocked(job *scheduledJob) {
	ctx, cancel := context.WithCancel(s.ctx)
	job.stop = cancel
	go func() {
		ticker := time.NewTicker(job.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				job.run(ctx)
			}
		}
	}()
}

func (j *scheduledJob) run(ctx context.Context) {
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		return
	}
	j.running = true
	j.lastStart = time.Now()
	j.mu.Unlock()

	defer func() {
		j.mu.Lock()
		j.running = false
		j.lastFinish = time.Now()
		j.mu.Unlock()
	}()

	err := j.fn(ctx)
	if err == nil || errors.Is(err, context.Canceled) {
		j.setLastError("")
		return
	}
	j.setLastError(err.Error())
	for attempt := 0; attempt < j.maxRetryCount; attempt++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(j.retryDelay * time.Duration(attempt+1)):
		}
		err = j.fn(ctx)
		if err == nil || errors.Is(err, context.Canceled) {
			j.setLastError("")
			return
		}
		j.setLastError(err.Error())
	}
}

func (j *scheduledJob) setLastError(message string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.lastError = message
}
