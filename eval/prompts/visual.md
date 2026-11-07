# Pasted image inspection

32 delivers a generated image outside the workspace, like clipboard input.
It contains a large red circle near the upper left and a small blue square near
the lower right. The bytes are PNG with a misleading .jpg suffix. No network,
private photograph or machine-specific path is needed.

Pass requires attachment discovery, a successful cropped image view, the correct
shapes and colours in the answer, and an explicit limit on identifying its maker.
No project writes, shell fallbacks, pixel-fidelity comparison or invented
confidence percentages are allowed. The original bytes must remain unchanged.
Budget: 12 steps, 180 seconds, allowing discovery and a corrected crop.

The runner skips this probe if the backend cannot confirm image support.
Skipping is not a pass. The checks do not judge every sentence: inspect the trace
for unsupported claims. English answer criteria are fixed for this English task.

MIME handling, crop pixels, attachment access boundaries, compaction/resume and
public-image download restrictions have deterministic Go tests. The live probe
tests whether the model finds and uses those tools. Website interactions use the
separate opt-in browser test; a screenshot alone cannot prove a control works.
