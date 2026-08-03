package auth

import (
	"net/netip"
	"sync"
	"time"
)

type LoginLimiter struct {
	mu          sync.Mutex
	now         func() time.Time
	perLimit    int
	globalLimit int
	window      time.Duration
	perSource   map[string][]time.Time
	global      []time.Time
}

func NewLoginLimiter(now func() time.Time, perLimit, globalLimit int, window time.Duration) *LoginLimiter {
	if now == nil {
		now = time.Now
	}
	return &LoginLimiter{
		now: now, perLimit: perLimit, globalLimit: globalLimit, window: window, perSource: make(map[string][]time.Time),
	}
}

func (l *LoginLimiter) Allow(address netip.Addr) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked()
	return len(l.global) < l.globalLimit && len(l.perSource[sourceKey(address)]) < l.perLimit
}

func (l *LoginLimiter) RecordFailure(address netip.Addr) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked()
	now := l.now()
	key := sourceKey(address)
	l.perSource[key] = append(l.perSource[key], now)
	l.global = append(l.global, now)
}

func (l *LoginLimiter) Reset(address netip.Addr) {
	l.mu.Lock()
	delete(l.perSource, sourceKey(address))
	l.mu.Unlock()
}

func (l *LoginLimiter) pruneLocked() {
	cutoff := l.now().Add(-l.window)
	l.global = pruneBefore(l.global, cutoff)
	for key, attempts := range l.perSource {
		attempts = pruneBefore(attempts, cutoff)
		if len(attempts) == 0 {
			delete(l.perSource, key)
		} else {
			l.perSource[key] = attempts
		}
	}
}

func sourceKey(address netip.Addr) string {
	address = address.Unmap()
	if address.Is6() {
		return netip.PrefixFrom(address, 64).Masked().String()
	}
	return address.String()
}

func pruneBefore(values []time.Time, cutoff time.Time) []time.Time {
	index := 0
	for index < len(values) && !values[index].After(cutoff) {
		index++
	}
	return values[index:]
}
