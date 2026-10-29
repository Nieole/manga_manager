// Package scanner provides complete library/series ingestion and conservative cleanup.
// Discovery admits candidates, interpretation derives source facts, and a sole ingestion
// writer claims identities, commits books and refreshes derived series facts. bookidentity
// owns evidence and seriesmeta owns source-specific metadata rules. Covers have independent
// runs. Ingestion coordination consumes filesystem changes and actual scan outcomes;
// application orchestration owns run admission and lifecycle.
package scanner
