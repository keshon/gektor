// Package store owns workspace-scoped workflow records, queue claims, schedules
// in SQLite, with process leases for active runs. It freezes submitted recipes but never runs their
// operations. The caller owns the store handle and must close it.
package store
