# Repair a small Go CLI

The repository contains a decimal-money library and its CLI. The agent must
understand both files, fix decimal scaling and argument handling, reject malformed
input, and run checks against the actual project. This measures a complete repair,
not a prescribed tool sequence.

Trusted acceptance tests are outside the agent workspace and injected only after
the run. They check integer cents, fractions, signed refunds, invalid syntax,
bounded randomized sums and actual CLI execution. Adding or rewriting local tests
cannot turn an incorrect implementation into a passing result.

The frozen scope is intentionally small enough for 16 model turns. It measures
repair correctness and verification, not production finance suitability or design.
