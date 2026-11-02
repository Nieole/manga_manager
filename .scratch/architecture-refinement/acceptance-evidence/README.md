# Ticket 48 retained evidence

See [acceptance.md](../acceptance.md) for scope, target and limits. This directory is repository-owned; `.tmp` originals remain untouched.

- `*.command` / `*.log` / `*.exit`: captured invocation, canonically retained output (see below), actual exit. Completed prior-48 gates were inspected and reused only because initial retry had no tracked source changes at the same HEAD. Empty logs are valid only paired with actual completed exit (format/drift/lint commands differ).
- `run-*.sh`, `run-story.py`, `story-command.json`: original working directories/environment and exact commands; machine-local paths intentionally retained as provenance, not portable CI scripts.
- `story-tests.log`: Go JSON, exact `Package`, `Test`, `Action`, `Elapsed`. Referenced top-level pass is validated, not inferred from test presence. Nested platform skips still do not establish other-platform behavior.
- `web-results.json`, `heatmap-results.json`: fresh retry complete split Vitest results, concrete case status. Original summary-only runs also retained. Three suite/template references were corrected in `stories.json`; `stories-inherited.json` is the partial input with only line endings canonicalized.
- `catalogue.json` / `catalogue.txt`: implementation/consumer/manual boundary catalogue. `structure-audit.json`: tracked production declarations/imports and source digest, separate from behavioral proof.
- `inventory-input.md`, `prepare-inventory-input.py`: older external read-only inventory and interrupted inventory helper, provenance only. Stale pending-46/47 descriptions are **not** final acceptance findings.
- `doc-style.log`: timeout retained. `doc-style-retry.log`: bounded 600s traced attempt, completed exit 0; slow MSYS subprocess progress and non-blocking history advisories visible. No checker/source bypass.
- Linux/Darwin CGO build logs are **failures**, not runtime or portable release success.
- `manifest.json`: SHA256 of retained evidence (excluding manifest and validator output to avoid circular hashes).

## Text canonicalization and snapshot identity

The first staging attempt failed `git diff --cached --check`: CRLF in newly added text was treated as trailing whitespace. No commit was created. Effective local Git settings are `core.autocrlf=false`, `core.eol=lf`; `.gitattributes` only forces SQL to LF. The repair uses the existing `dos2unix` formatter on CRLF files, not a Git whitespace exception. All retained text must be LF with no horizontal trailing whitespace. No nonempty output lines, timestamps, test outcomes or invocation arguments are removed or rewritten. The original `.tmp` logs remain untouched; repository `.log` files are canonical text, **not byte-identical raw captures**.

Canonicalization replaces `CR LF -> LF` and removes surplus empty LF lines at EOF (retaining one final LF). The latter was required by the full-new-file Git whitespace check for `environment.log`, `web-tests.log` and `web-heatmap.log`; internal blank lines remain intact. Affected captured gate logs have original SHA256 identities:

- `environment.log`: `420665f0b530e51853b526ab89ffb43c1b186f1ea95e977e67413d5440b51002`.
- `linux-build.log`: `8fb210bad6a450b8126f9c44325aab91d182b057cda0074cccfcca18faa34015`.
- `web-tests.log`: `be19698fa4afbf677baf1856d605a2f830882abf453d9a80cebd615dff6c4cb9`.
- `web-heatmap.log`: `9861e289f37fed18c50384ea41a034651ea6b9533b2885d374195f665d9a1f8a`.

The same line-ending-only transform applies to `catalogue.json`, `catalogue.txt`, `inventory-input.md`, `stories-inherited.json` and `stories.json`. `structure-audit.json` also clarifies its digest algorithm (the digest/count are unchanged); the manifest also updates hashes. `verification.log` is regenerated validator output, not a product-test log. Manifest hashes identify final canonical bytes, including this explanation and the validator. The original `diff-check` gate only checked tracked worktree differences and missed then-untracked evidence; it is historical evidence, **not** proof of artifact committability. Full-file whitespace and staged-snapshot checks are required at handoff/restaging.

Source identity includes sorted Git-tracked `.go/.ts/.tsx/.sql/.yaml/.json` paths, excluding this entire evidence directory regardless of whether it is untracked, staged or committed. The validator requires HEAD, index and worktree production-source identities to agree; the historical target HEAD/tree remains provenance, not a requirement that future docs-only acceptance commits retain the old HEAD. This avoids a self-referential digest after adding JSON evidence.

Run `python .scratch/architecture-refinement/acceptance-evidence/verify.py` from repository root to validate mapping, actual witnesses, source identity and hashes. It does **not** execute application tests or constitute independent review.
