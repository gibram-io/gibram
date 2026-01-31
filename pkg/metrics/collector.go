// Package metrics provides metrics collection for GibRAM
package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// Collector collects and aggregates metrics
type Collector struct {
	counters sync.Map // map[string]*atomic.Int64
	gauges   sync.Map // map[string]*atomic.Int64
	histos   sync.Map // map[string]*Histogram

	startTime time.Time
}

// NewCollector creates a new metrics collector
func NewCollector() *Collector {
	return &Collector{
		startTime: time.Now(),
	}
}

// Counter increments a counter metric
func (c *Collector) Counter(name string, delta int64) {
	val, _ := c.counters.LoadOrStore(name, &atomic.Int64{})
	val.(*atomic.Int64).Add(delta)
}

// Gauge sets a gauge metric
func (c *Collector) Gauge(name string, value int64) {
	val, _ := c.gauges.LoadOrStore(name, &atomic.Int64{})
	val.(*atomic.Int64).Store(value)
}

// Histogram records a value in a histogram
func (c *Collector) Histogram(name string, value float64) {
	h, _ := c.histos.LoadOrStore(name, NewHistogram())
	h.(*Histogram).Record(value)
}

// GetCounter returns current counter value
func (c *Collector) GetCounter(name string) int64 {
	val, ok := c.counters.Load(name)
	if !ok {
		return 0
	}
	return val.(*atomic.Int64).Load()
}

// GetGauge returns current gauge value
func (c *Collector) GetGauge(name string) int64 {
	val, ok := c.gauges.Load(name)
	if !ok {
		return 0
	}
	return val.(*atomic.Int64).Load()
}

// GetHistogram returns histogram stats
func (c *Collector) GetHistogram(name string) *HistogramStats {
	h, ok := c.histos.Load(name)
	if !ok {
		return nil
	}
	return h.(*Histogram).Stats()
}

// Snapshot returns all metrics as a snapshot
func (c *Collector) Snapshot() *Snapshot {
	snap := &Snapshot{
		Timestamp:  time.Now(),
		Uptime:     time.Since(c.startTime),
		Counters:   make(map[string]int64),
		Gauges:     make(map[string]int64),
		Histograms: make(map[string]*HistogramStats),
	}

	c.counters.Range(func(key, value any) bool {
		snap.Counters[key.(string)] = value.(*atomic.Int64).Load()
		return true
	})

	c.gauges.Range(func(key, value any) bool {
		snap.Gauges[key.(string)] = value.(*atomic.Int64).Load()
		return true
	})

	c.histos.Range(func(key, value any) bool {
		snap.Histograms[key.(string)] = value.(*Histogram).Stats()
		return true
	})

	return snap
}

// Reset resets all metrics
func (c *Collector) Reset() {
	c.counters = sync.Map{}
	c.gauges = sync.Map{}
	c.histos = sync.Map{}
	c.startTime = time.Now()
}

// Snapshot holds a point-in-time snapshot of all metrics
type Snapshot struct {
	Timestamp  time.Time
	Uptime     time.Duration
	Counters   map[string]int64
	Gauges     map[string]int64
	Histograms map[string]*HistogramStats
}
