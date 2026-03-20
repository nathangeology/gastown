package web

import (
	"sync"
	"time"
)

// CachingFetcher wraps a ConvoyFetcher and caches results for a configurable TTL.
// This prevents the dashboard from re-fetching all data on every auto-refresh
// (default 30s), which was causing Dolt server overload from subprocess storms.
type CachingFetcher struct {
	inner ConvoyFetcher
	ttl   time.Duration

	mu      sync.RWMutex
	entries map[string]*cacheEntry
}

type cacheEntry struct {
	data      interface{}
	err       error
	fetchedAt time.Time
}

// NewCachingFetcher wraps a fetcher with a cache. TTL of 0 disables caching.
func NewCachingFetcher(inner ConvoyFetcher, ttl time.Duration) ConvoyFetcher {
	if ttl <= 0 {
		return inner
	}
	return &CachingFetcher{
		inner:   inner,
		ttl:     ttl,
		entries: make(map[string]*cacheEntry),
	}
}

func (c *CachingFetcher) get(key string) (*cacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[key]
	if !ok || time.Since(e.fetchedAt) > c.ttl {
		return nil, false
	}
	return e, true
}

func (c *CachingFetcher) set(key string, data interface{}, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = &cacheEntry{data: data, err: err, fetchedAt: time.Now()}
}

func (c *CachingFetcher) FetchConvoys() ([]ConvoyRow, error) {
	if e, ok := c.get("convoys"); ok {
		return e.data.([]ConvoyRow), e.err
	}
	data, err := c.inner.FetchConvoys()
	c.set("convoys", data, err)
	return data, err
}

func (c *CachingFetcher) FetchMergeQueue() ([]MergeQueueRow, error) {
	if e, ok := c.get("mergequeue"); ok {
		return e.data.([]MergeQueueRow), e.err
	}
	data, err := c.inner.FetchMergeQueue()
	c.set("mergequeue", data, err)
	return data, err
}

func (c *CachingFetcher) FetchWorkers() ([]WorkerRow, error) {
	if e, ok := c.get("workers"); ok {
		return e.data.([]WorkerRow), e.err
	}
	data, err := c.inner.FetchWorkers()
	c.set("workers", data, err)
	return data, err
}

func (c *CachingFetcher) FetchMail() ([]MailRow, error) {
	if e, ok := c.get("mail"); ok {
		return e.data.([]MailRow), e.err
	}
	data, err := c.inner.FetchMail()
	c.set("mail", data, err)
	return data, err
}

func (c *CachingFetcher) FetchRigs() ([]RigRow, error) {
	if e, ok := c.get("rigs"); ok {
		return e.data.([]RigRow), e.err
	}
	data, err := c.inner.FetchRigs()
	c.set("rigs", data, err)
	return data, err
}

func (c *CachingFetcher) FetchDogs() ([]DogRow, error) {
	if e, ok := c.get("dogs"); ok {
		return e.data.([]DogRow), e.err
	}
	data, err := c.inner.FetchDogs()
	c.set("dogs", data, err)
	return data, err
}

func (c *CachingFetcher) FetchEscalations() ([]EscalationRow, error) {
	if e, ok := c.get("escalations"); ok {
		return e.data.([]EscalationRow), e.err
	}
	data, err := c.inner.FetchEscalations()
	c.set("escalations", data, err)
	return data, err
}

func (c *CachingFetcher) FetchHealth() (*HealthRow, error) {
	if e, ok := c.get("health"); ok {
		return e.data.(*HealthRow), e.err
	}
	data, err := c.inner.FetchHealth()
	c.set("health", data, err)
	return data, err
}

func (c *CachingFetcher) FetchQueues() ([]QueueRow, error) {
	if e, ok := c.get("queues"); ok {
		return e.data.([]QueueRow), e.err
	}
	data, err := c.inner.FetchQueues()
	c.set("queues", data, err)
	return data, err
}

func (c *CachingFetcher) FetchSessions() ([]SessionRow, error) {
	if e, ok := c.get("sessions"); ok {
		return e.data.([]SessionRow), e.err
	}
	data, err := c.inner.FetchSessions()
	c.set("sessions", data, err)
	return data, err
}

func (c *CachingFetcher) FetchHooks() ([]HookRow, error) {
	if e, ok := c.get("hooks"); ok {
		return e.data.([]HookRow), e.err
	}
	data, err := c.inner.FetchHooks()
	c.set("hooks", data, err)
	return data, err
}

func (c *CachingFetcher) FetchMayor() (*MayorStatus, error) {
	if e, ok := c.get("mayor"); ok {
		return e.data.(*MayorStatus), e.err
	}
	data, err := c.inner.FetchMayor()
	c.set("mayor", data, err)
	return data, err
}

func (c *CachingFetcher) FetchIssues() ([]IssueRow, error) {
	if e, ok := c.get("issues"); ok {
		return e.data.([]IssueRow), e.err
	}
	data, err := c.inner.FetchIssues()
	c.set("issues", data, err)
	return data, err
}

func (c *CachingFetcher) FetchActivity() ([]ActivityRow, error) {
	if e, ok := c.get("activity"); ok {
		return e.data.([]ActivityRow), e.err
	}
	data, err := c.inner.FetchActivity()
	c.set("activity", data, err)
	return data, err
}
