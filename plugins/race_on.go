//go:build race

package plugins

// raceEnabled reports whether this binary was built with the race detector.
// A plugin can only be loaded by a host built with the same race setting,
// so fetcher.build must mirror it.
const raceEnabled = true
