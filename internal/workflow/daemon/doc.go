// Package daemon holds the workspace worker lease, advances schedules and
// claims queued jobs. Jobs execute through service with operator-supplied
// policy. Cancellation joins the scheduler and all jobs before releasing the lease.
package daemon
