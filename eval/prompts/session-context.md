# Session context evidence

These probes cover two failures seen during the Ornith conversation: difficulty
searching saved messages, and claims about persona changes made from incomplete
prompt reads.

Probe 37 must search the saved conversation for a literal marker, quote it with
its archive ID, and inspect current request membership. The marker is in the
initial task; this probe checks tool discovery and evidence use, not recovery
from a long conversation.

Probe 38 must inspect system revisions and current identity text. A fresh session
has no recorded persona edit. The answer must not invent an older version,
duplication, string replacement, or restart behavior. Even a real text difference
would not establish how settings were applied. Review the answer manually: the
text checks only catch some unsupported claims.

Go regression tests separately check compacted history recovery, unchanged
archive records, prompt changes on resume, unknown legacy delivery, Unicode
paging and tool activation during the inspecting batch. These deterministic
checks do not establish model reliability. Run repeated live probes with the
same backend before comparing models.

`45-assistant-role` replays a saved download report with an assistant message phrased like a user request. The model must report one actual user request, keep its own message attributed to itself, and finish without starting new work. The report exists in the seeded workspace and is read through an eager tool. No media capability is required and this probe performs no download.
