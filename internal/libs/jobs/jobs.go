// Package jobs provides background job queue management and async task processing.
package jobs

import "time"

// Job represents a background job
type Job struct {
	ID        string
	Status    string
	CreatedAt time.Time
}

// Queue manages background jobs
type Queue struct {
	jobs []*Job
}

// NewQueue creates a new job queue
func NewQueue() *Queue {
	return &Queue{
		jobs: make([]*Job, 0),
	}
}

// Enqueue adds a job to the queue
func (q *Queue) Enqueue(id string) *Job {
	job := &Job{
		ID:        id,
		Status:    "pending",
		CreatedAt: time.Now(),
	}
	q.jobs = append(q.jobs, job)
	return job
}

// Count returns the number of jobs in the queue
func (q *Queue) Count() int {
	return len(q.jobs)
}
