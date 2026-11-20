package main

// An enabled gate requires usable measurements. Skips do not count as failures,
// but an all-skipped experiment cannot satisfy a release gate.
func evaluationExitCode(results []Result, maxFailures int) int {
	if maxFailures < 0 {
		return 0
	}

	stats := aggregateRuns(results)
	if stats.Errors > 0 || stats.Scored == 0 {
		return 2
	}
	if stats.Scored-stats.Passed > maxFailures {
		return 1
	}
	return 0
}
