// Package store maintains an in-memory thread-safe list of running Spark driver pods.
package store

import (
	"sync"
	"time"
)

// Driver holds the metadata we track for a single Spark driver pod.
type Driver struct {
	// PodName is the Kubernetes pod name, used as the map key.
	PodName string
	// CreatedAt is the pod creation timestamp.
	CreatedAt time.Time
	// AppSelector is the value of the spark-app-selector label.
	AppSelector string
	// AppName is the value of the spark-app-name label.
	AppName string
}

// Store is a thread-safe in-memory store of Spark driver pods.
type Store struct {
	mu      sync.RWMutex
	drivers map[string]Driver
}

// New creates an empty Store.
func New() *Store {
	return &Store{drivers: make(map[string]Driver)}
}

// Add inserts or updates a driver entry.
func (s *Store) Add(d Driver) {
	s.mu.Lock()
	s.drivers[d.PodName] = d
	s.mu.Unlock()
}

// Remove deletes the driver entry with the given pod name.
func (s *Store) Remove(podName string) {
	s.mu.Lock()
	delete(s.drivers, podName)
	s.mu.Unlock()
}

// List returns a snapshot of all current drivers in an unspecified order.
func (s *Store) List() []Driver {
	s.mu.RLock()
	out := make([]Driver, 0, len(s.drivers))
	for _, d := range s.drivers {
		out = append(out, d)
	}
	s.mu.RUnlock()
	return out
}
