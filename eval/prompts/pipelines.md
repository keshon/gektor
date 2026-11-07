# Agent use of pipelines

These probes test whether the Agent can use the engine, rather than calculate
an answer itself. Both start with a small, fixed child recipe. No model is
needed inside a workflow and no external service is contacted.

30: create a parent with two calls to the child, validate and execute it once.
Pass requires a completed stored run with declared outputs six=6 and ten=10,
two actual child calls, an unchanged child file and matching captured run evidence.
Merely returning those numbers in chat does not pass. Budget: 14 steps, 240 seconds.

31: create an hourly, paused trigger with frozen input n=7. Check schedules and
worker status. Pass requires the actual saved trigger and no runs or queued jobs.
The answer must mention both the paused state and the separate worker/service.
OS scheduling, shell commands and starting a service are forbidden.
Budget: 12 steps, 180 seconds.

These budgets allow discovery, schema reading, file preparation and verification.
They limit unproductive retries; they are not latency targets.
Revision conflicts, review recovery, overlap, service restarts and queue execution
remain deterministic integration tests in internal/automation. These probes do
not establish scheduling reliability or general recipe correctness.
