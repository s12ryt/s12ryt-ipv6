package app

import (
	"sort"
	"strings"
	"sync"

	"github.com/s12ryt/s12ryt-ipv6/internal/admin"
)

type HealthIssue struct {
	Component string
	State     admin.HealthState
	Error     string
}

type HealthTracker struct {
	mu     sync.RWMutex
	issues map[string]HealthIssue
}

func NewHealthTracker() *HealthTracker {
	return &HealthTracker{issues: make(map[string]HealthIssue)}
}

func (t *HealthTracker) ReportDegraded(component string, err error) {
	t.report(component, admin.HealthDegraded, err)
}

func (t *HealthTracker) ReportUnhealthy(component string, err error) {
	t.report(component, admin.HealthUnhealthy, err)
}

func (t *HealthTracker) report(component string, state admin.HealthState, err error) {
	component = strings.TrimSpace(component)
	if component == "" || err == nil {
		return
	}
	t.mu.Lock()
	t.issues[component] = HealthIssue{Component: component, State: state, Error: err.Error()}
	t.mu.Unlock()
}

func (t *HealthTracker) Recover(component string) {
	component = strings.TrimSpace(component)
	if component == "" {
		return
	}
	t.mu.Lock()
	delete(t.issues, component)
	t.mu.Unlock()
}

func (t *HealthTracker) State() admin.HealthState {
	t.mu.RLock()
	defer t.mu.RUnlock()
	state := admin.HealthHealthy
	for _, issue := range t.issues {
		if issue.State == admin.HealthUnhealthy {
			return admin.HealthUnhealthy
		}
		if issue.State == admin.HealthDegraded {
			state = admin.HealthDegraded
		}
	}
	return state
}

func (t *HealthTracker) Issues() []HealthIssue {
	t.mu.RLock()
	issues := make([]HealthIssue, 0, len(t.issues))
	for _, issue := range t.issues {
		issues = append(issues, issue)
	}
	t.mu.RUnlock()
	sort.Slice(issues, func(left, right int) bool {
		return issues[left].Component < issues[right].Component
	})
	return issues
}
