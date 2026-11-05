package chatcompletions

import "testing"

func TestServer_UsageTotalsAccumulate(t *testing.T) {
	s := &Server{}
	if p, g, n := s.UsageTotals(); p != 0 || g != 0 || n != 0 {
		t.Fatalf("fresh totals = %d/%d/%d, want 0/0/0", p, g, n)
	}
	s.recordUsage(100, 20, 30)
	s.recordUsage(50, 5, 0)
	if p, g, n := s.UsageTotals(); p != 150 || g != 25 || n != 2 {
		t.Fatalf("totals = %d/%d/%d, want 150/25/2", p, g, n)
	}
	if c := s.CachedTotal(); c != 30 {
		t.Fatalf("cached = %d, want 30", c)
	}
}
