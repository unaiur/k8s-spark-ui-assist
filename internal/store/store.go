// Package store maintains an in-memory thread-safe list of running Spark driver pods.
package store

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
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

// invalidDNSChars matches any character that is not allowed in a DNS-1123 label.
var invalidDNSChars = regexp.MustCompile(`[^a-z0-9-]`)

// RouteName converts the driver's AppSelector into a valid DNS-1123 subdomain
// name and appends "-ui-route". Kubernetes label values allow uppercase letters,
// dots, underscores, and up to 63 characters — none of which are universally
// valid in resource names. The sanitisation steps are:
//  1. Lowercase the selector.
//  2. Replace any character that is not [a-z0-9-] with "-".
//  3. If the result (plus the "-ui-route" suffix) exceeds 253 characters,
//     truncate and append an 8-hex-character hash of the original selector so
//     the name remains unique and deterministic.
func (d Driver) RouteName() string {
	const suffix = "-ui-route"
	const maxLen = 253

	sanitized := invalidDNSChars.ReplaceAllString(strings.ToLower(d.AppSelector), "-")

	candidate := sanitized + suffix
	if len(candidate) <= maxLen {
		return candidate
	}

	// Hash the original selector to preserve uniqueness.
	h := sha256.Sum256([]byte(d.AppSelector))
	hash := fmt.Sprintf("%x", h[:4]) // 8 hex chars
	// Truncate sanitized so that sanitized + "-" + hash + suffix fits in maxLen.
	maxSanitized := maxLen - len(suffix) - 1 - len(hash)
	return sanitized[:maxSanitized] + "-" + hash + suffix
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
