// Package metrics provides Prometheus-style metrics collection for OpenPool.
package metrics

import (
	"fmt"
	"sync"
	"time"
)

// MetricType defines the type of a metric.
type MetricType string

const (
	CounterType   MetricType = "counter"
	GaugeType     MetricType = "gauge"
	HistogramType MetricType = "histogram"
)

// Metric represents a single metric.
type Metric struct {
	Name        string
	Description string
	Type        MetricType
	Value       float64
	Labels      map[string]string
}

// Collector collects and exposes metrics.
type Collector struct {
	mu        sync.RWMutex
	counters  map[string]*Metric
	gauges    map[string]*Metric
	startTime time.Time
}

// NewCollector creates a new metrics collector.
func NewCollector() *Collector {
	return &Collector{
		counters:  make(map[string]*Metric),
		gauges:    make(map[string]*Metric),
		startTime: time.Now(),
	}
}

// Inc increments a counter by 1.
func (c *Collector) Inc(name, desc string, labels map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := metricKey(name, labels)
	if m, ok := c.counters[key]; ok {
		m.Value++
	} else {
		c.counters[key] = &Metric{
			Name:        name,
			Description: desc,
			Type:        CounterType,
			Value:       1,
			Labels:      labels,
		}
	}
}

// Add adds a value to a counter.
func (c *Collector) Add(name, desc string, value float64, labels map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := metricKey(name, labels)
	if m, ok := c.counters[key]; ok {
		m.Value += value
	} else {
		c.counters[key] = &Metric{
			Name:        name,
			Description: desc,
			Type:        CounterType,
			Value:       value,
			Labels:      labels,
		}
	}
}

// Set sets a gauge value.
func (c *Collector) Set(name, desc string, value float64, labels map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := metricKey(name, labels)
	c.gauges[key] = &Metric{
		Name:        name,
		Description: desc,
		Type:        GaugeType,
		Value:       value,
		Labels:      labels,
	}
}

// Observe records an observation for a histogram-like metric.
func (c *Collector) Observe(name, desc string, value float64, labels map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	key := metricKey(name, labels)
	if m, ok := c.counters[key+"_count"]; ok {
		m.Value++
	} else {
		c.counters[key+"_count"] = &Metric{
			Name:        name + "_count",
			Description: desc + " (count)",
			Type:        CounterType,
			Value:       1,
			Labels:      labels,
		}
	}

	if m, ok := c.counters[key+"_sum"]; ok {
		m.Value += value
	} else {
		c.counters[key+"_sum"] = &Metric{
			Name:        name + "_sum",
			Description: desc + " (sum)",
			Type:        CounterType,
			Value:       value,
			Labels:      labels,
		}
	}
}

// UptimeSeconds returns the collector uptime.
func (c *Collector) UptimeSeconds() float64 {
	return time.Since(c.startTime).Seconds()
}

// FormatPrometheus returns metrics in Prometheus text format.
func (c *Collector) FormatPrometheus() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var out string

	// Uptime metric
	out += fmt.Sprintf("# HELP openpool_uptime_seconds Time since node started\n")
	out += fmt.Sprintf("# TYPE openpool_uptime_seconds gauge\n")
	out += fmt.Sprintf("openpool_uptime_seconds %.3f\n", c.UptimeSeconds())

	// Counters
	for _, m := range c.counters {
		labelStr := formatLabels(m.Labels)
		out += fmt.Sprintf("# HELP %s %s\n", m.Name, m.Description)
		out += fmt.Sprintf("# TYPE %s counter\n", m.Name)
		out += fmt.Sprintf("%s%s %.0f\n", m.Name, labelStr, m.Value)
	}

	// Gauges
	for _, m := range c.gauges {
		labelStr := formatLabels(m.Labels)
		out += fmt.Sprintf("# HELP %s %s\n", m.Name, m.Description)
		out += fmt.Sprintf("# TYPE %s gauge\n", m.Name)
		out += fmt.Sprintf("%s%s %.3f\n", m.Name, labelStr, m.Value)
	}

	return out
}

// Snapshot returns a copy of all metrics.
func (c *Collector) Snapshot() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make(map[string]interface{})
	result["uptime_seconds"] = c.UptimeSeconds()

	counters := make(map[string]float64)
	for key, m := range c.counters {
		counters[key] = m.Value
	}
	result["counters"] = counters

	gauges := make(map[string]float64)
	for key, m := range c.gauges {
		gauges[key] = m.Value
	}
	result["gauges"] = gauges

	return result
}

func metricKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	key := name + "{"
	first := true
	for k, v := range labels {
		if !first {
			key += ","
		}
		key += fmt.Sprintf("%s=%s", k, v)
		first = false
	}
	key += "}"
	return key
}

func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	out := "{"
	first := true
	for k, v := range labels {
		if !first {
			out += ","
		}
		out += fmt.Sprintf("%s=\"%s\"", k, v)
		first = false
	}
	out += "}"
	return out
}
