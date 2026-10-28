// Package koreader owns kosync accounts, authentication, device document matching
// and progress receipts, reads, resets and reconciliation. Matched device facts
// pass through reading; unmatched receipts retain their original occurrence time.
// Content/path evidence and rebuilding belong to bookidentity. HTTP is an adapter;
// application orchestration owns runs and supplies reconciliation checkpoints.
package koreader
