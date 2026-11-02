// Package runtimecfg owns publication and retirement of configuration-derived process resources.
//
// Runtime owns one Resources instance: startup, settings and file reload share Apply;
// Close follows input shutdown and user drain. Pools retire borrowed handles on final
// return, while image processors keep the permit generation they acquired.
//
// Config.Manager owns configuration facts and compensation; parser and images own
// resource mechanics. The executable owns the log file; Resources updates its level.
package runtimecfg
