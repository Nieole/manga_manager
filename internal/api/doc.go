// Package api adapts HTTP requests for /api (including Mihon), OPDS and KOReader.
// It owns protocol parsing, authorization, responses, SSE projections and transport cache leaves.
// Application capabilities own use cases, restart declarations, recovery and background scheduling;
// bootstrap assembles the production application and binds its HTTP Host before opening inputs.
// Host composes independently bound feature adapters under one shared authentication gate.
// Task state and admission belong to task; persistence belongs to taskstore and database.
package api
