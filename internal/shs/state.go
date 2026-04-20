package shs

import "sync/atomic"

// State implements Handler and provides a thread-safe IsUp method that reports
// the current SHS availability. It is meant to be shared between the SHS
// EndpointSlice watcher and the HTTP server so both can observe the same state
// without additional synchronisation.
type State struct {
	up atomic.Bool
}

// OnUp marks the SHS as available. Called by the EndpointSlice watcher.
func (s *State) OnUp() { s.up.Store(true) }

// OnDown marks the SHS as unavailable. Called by the EndpointSlice watcher.
func (s *State) OnDown() { s.up.Store(false) }

// IsUp reports whether the SHS currently has at least one ready endpoint.
func (s *State) IsUp() bool { return s.up.Load() }
