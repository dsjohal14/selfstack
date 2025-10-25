package jobs

import "testing"

func TestNewQueue(t *testing.T) {
	q := NewQueue()
	if q == nil {
		t.Fatal("NewQueue() returned nil")
	}

	if q.Count() != 0 {
		t.Errorf("new queue should be empty, got %d jobs", q.Count())
	}
}

func TestEnqueue(t *testing.T) {
	q := NewQueue()

	job := q.Enqueue("test-job-1")
	if job == nil {
		t.Fatal("Enqueue() returned nil")
	}

	if job.ID != "test-job-1" {
		t.Errorf("expected job ID test-job-1, got %s", job.ID)
	}

	if job.Status != "pending" {
		t.Errorf("expected status pending, got %s", job.Status)
	}

	if q.Count() != 1 {
		t.Errorf("expected 1 job in queue, got %d", q.Count())
	}
}

func TestMultipleJobs(t *testing.T) {
	q := NewQueue()

	q.Enqueue("job-1")
	q.Enqueue("job-2")
	q.Enqueue("job-3")

	if q.Count() != 3 {
		t.Errorf("expected 3 jobs in queue, got %d", q.Count())
	}
}
