package store

import (
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAddListRemove(t *testing.T) {
	s := New()

	if got := s.List(); len(got) != 0 {
		t.Fatalf("expected empty list, got %d entries", len(got))
	}

	d1 := Driver{PodName: "pod-1", CreatedAt: time.Now(), AppSelector: "sel-1", AppName: "app-1"}
	d2 := Driver{PodName: "pod-2", CreatedAt: time.Now(), AppSelector: "sel-2", AppName: "app-2"}

	s.Add(d1)
	s.Add(d2)

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(list))
	}

	s.Remove("pod-1")
	list = s.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 entry after remove, got %d", len(list))
	}
	if list[0].PodName != "pod-2" {
		t.Fatalf("expected pod-2, got %s", list[0].PodName)
	}

	// Remove non-existent key should be a no-op.
	s.Remove("does-not-exist")
	if len(s.List()) != 1 {
		t.Fatal("remove of non-existent key changed list")
	}
}

func TestUpdate(t *testing.T) {
	s := New()
	s.Add(Driver{PodName: "pod-1", AppName: "old"})
	s.Add(Driver{PodName: "pod-1", AppName: "new"})

	list := s.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 entry after update, got %d", len(list))
	}
	if list[0].AppName != "new" {
		t.Fatalf("expected updated AppName 'new', got '%s'", list[0].AppName)
	}
}

func TestConcurrency(t *testing.T) {
	s := New()
	var wg sync.WaitGroup
	const workers = 50

	for i := range workers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			d := Driver{PodName: "pod", AppSelector: "sel", AppName: "app", CreatedAt: time.Now()}
			_ = i
			s.Add(d)
			_ = s.List()
			s.Remove("pod")
		}(i)
	}
	wg.Wait()
}

// ---- Driver.RouteName -------------------------------------------------------

func TestRouteNameSimple(t *testing.T) {
	d := Driver{AppSelector: "spark-abc123"}
	if got := d.RouteName(); got != "spark-abc123-ui-route" {
		t.Errorf("got %q, want spark-abc123-ui-route", got)
	}
}

func TestRouteNameSanitization(t *testing.T) {
	cases := []struct {
		appSelector string
		want        string
	}{
		{"Spark_App.123", "spark-app-123-ui-route"},
		{"UPPER_CASE", "upper-case-ui-route"},
		{"with.dots", "with-dots-ui-route"},
		{"already-valid", "already-valid-ui-route"},
	}
	for _, tc := range cases {
		t.Run(tc.appSelector, func(t *testing.T) {
			d := Driver{AppSelector: tc.appSelector}
			if got := d.RouteName(); got != tc.want {
				t.Errorf("RouteName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRouteNameTruncation(t *testing.T) {
	// Build an AppSelector whose sanitised form + "-ui-route" exceeds 253 chars.
	long := strings.Repeat("abcdefghij", 26) // 260 chars
	d := Driver{AppSelector: long}
	name := d.RouteName()
	if len(name) > 253 {
		t.Errorf("route name length %d exceeds 253: %q", len(name), name)
	}
	const suffix = "-ui-route"
	if !strings.HasSuffix(name, suffix) {
		t.Errorf("route name does not end with %q: %q", suffix, name)
	}
}
