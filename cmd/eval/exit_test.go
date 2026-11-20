package main

import "testing"

func TestEvaluationExitPolicy(t *testing.T) {
	tests := []struct {
		name          string
		results       []Result
		maximum, want int
	}{
		{name: "exploration", results: []Result{{}, {Errored: true}}, maximum: -1, want: 0},
		{name: "pass", results: []Result{{Passed: true}}, maximum: 0, want: 0},
		{name: "failure", results: []Result{{Passed: true}, {}}, maximum: 0, want: 1},
		{name: "allowed failure", results: []Result{{Passed: true}, {}}, maximum: 1, want: 0},
		{name: "over threshold", results: []Result{{}, {}}, maximum: 1, want: 1},
		{name: "error despite passes", results: []Result{{Passed: true}, {Errored: true}}, maximum: 100, want: 2},
		{name: "error before threshold", results: []Result{{}, {Errored: true}}, maximum: 0, want: 2},
		{name: "skips excluded", results: []Result{{Passed: true}, {Skipped: true}}, maximum: 0, want: 0},
		{name: "all skipped", results: []Result{{Skipped: true}}, maximum: 0, want: 2},
		{name: "empty", maximum: 0, want: 2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := evaluationExitCode(test.results, test.maximum)
			if got != test.want {
				t.Fatalf("exit = %d, want %d", got, test.want)
			}
		})
	}
}
