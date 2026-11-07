// Package daemon holds the workspace worker lease, advances schedules and
// claims queued jobs. Jobs execute through service with operator-supplied
// policy. Cancellation stops the scheduler and worker before releasing the lease.
package daemon
