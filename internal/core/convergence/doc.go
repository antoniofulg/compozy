// Package convergence defines the pure, deterministic contracts for internal
// review convergence. It gives configuration, route resolution, semantic finding
// identity, and the phase/terminal state machine a single meaning that every
// later slice (durable store, phase runners, coordinator, and presentation)
// consumes unchanged.
//
// The package has no process, storage, daemon, API, or UI dependencies. All
// decisions are computed from explicit inputs so the same logic can be reused
// and tested without a running daemon, database, or model provider.
package convergence
