# Native workflows

Four scenarios use generated 1024×768 PNG images. The ICC case inserts an ICC
metadata marker to exercise the refusal gate; it does not test colour conversion.
No external images, executable paths or user skills are required.

- Success: save and validate a recipe, execute once, decode both width variants,
  compare the 800px image, and retain matching source hashes.
- ICC restriction: save and run a recipe; stop at preview with unsupported_metadata.
  A direct image_variants attempt is a trace failure even if safely blocked.
- Invalid file: reject the saved recipe's extra input field without editing or running.
- Existing directory: reject publication, preserve the sentinel and recipe exactly.

The verifier independently regenerates original bytes, compares hashes, checks
the exact saved source and its compiled definition against the execution record,
and decodes actual outputs. Parameterized recipes and declared outputs are
supported. Compiler-owned output collectors must match compilation; the four
public image steps and all success, preservation and output checks still apply.
Each case forbids executable/shell fallbacks. Structural, trace and limited answer
checks contribute to the verdict; free-form prose and visual quality are not fully
scored. Model explanations still need review. Three repeats measure a small sample
of consistency, not a general reliability guarantee.


`43-social-workflow` checks discovery and authoring of the shared public-post
node. Save post.json with the requested URL and a declared recipe output, then
validate it without execution. The recipe must use social_read_post. Network
reads, process commands and delegation are forbidden. The post URL is only an
authoring fixture; no provider or installed application is required. This checks
that the model discovers the new node, not whether FxTwitter is reachable.
