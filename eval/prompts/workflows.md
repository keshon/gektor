# Native image workflows

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
the exact saved recipe against its execution record, and decodes actual outputs.
Each case forbids executable/shell fallbacks. Structural, trace and limited answer
checks contribute to the verdict; free-form prose and visual quality are not fully
scored. Model explanations still need review. Three repeats measure a small sample
of consistency, not a general reliability guarantee.
