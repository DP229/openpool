package metrics

import (
	"strings"
	"testing"
)

func TestNewCollector(t *testing.T) {
	c := NewCollector()
	if c == nil {
		t.Fatal("NewCollector returned nil")
	}
	if c.counters == nil {
		t.Error("counters map not initialized")
	}
	if c.gauges == nil {
		t.Error("gauges map not initialized")
	}
	if c.startTime.IsZero() {
		t.Error("startTime not set")
	}
}

func TestInc(t *testing.T) {
	c := NewCollector()

	c.Inc("test_counter", "A test counter", nil)
	c.Inc("test_counter", "A test counter", nil)
	c.Inc("test_counter", "A test counter", nil)

	if len(c.counters) != 1 {
		t.Errorf("Expected 1 counter, got %d", len(c.counters))
	}

	for _, m := range c.counters {
		if m.Value != 3 {
			t.Errorf("Counter value = %v, want 3", m.Value)
		}
	}
}

func TestIncWithLabels(t *testing.T) {
	c := NewCollector()

	c.Inc("test_counter", "A test counter", map[string]string{"op": "fib"})
	c.Inc("test_counter", "A test counter", map[string]string{"op": "sum"})
	c.Inc("test_counter", "A test counter", map[string]string{"op": "fib"})

	if len(c.counters) != 2 {
		t.Errorf("Expected 2 counters (fib + sum), got %d", len(c.counters))
	}
}

func TestAdd(t *testing.T) {
	c := NewCollector()

	c.Add("test_counter", "A test counter", 5.5, nil)
	c.Add("test_counter", "A test counter", 2.5, nil)

	for _, m := range c.counters {
		if m.Value != 8.0 {
			t.Errorf("Counter value = %v, want 8.0", m.Value)
		}
	}
}

func TestSet(t *testing.T) {
	c := NewCollector()

	c.Set("test_gauge", "A test gauge", 42.0, nil)
	c.Set("test_gauge", "A test gauge", 99.0, nil)

	if len(c.gauges) != 1 {
		t.Errorf("Expected 1 gauge, got %d", len(c.gauges))
	}

	for _, m := range c.gauges {
		if m.Value != 99.0 {
			t.Errorf("Gauge value = %v, want 99.0", m.Value)
		}
	}
}

func TestObserve(t *testing.T) {
	c := NewCollector()

	c.Observe("request_duration", "Request duration", 100.0, nil)
	c.Observe("request_duration", "Request duration", 200.0, nil)
	c.Observe("request_duration", "Request duration", 300.0, nil)

	if len(c.counters) != 2 {
		t.Errorf("Expected 2 counters (_count + _sum), got %d", len(c.counters))
	}

	countKey := metricKey("request_duration", nil) + "_count"
	sumKey := metricKey("request_duration", nil) + "_sum"

	if c.counters[countKey].Value != 3 {
		t.Errorf("Count = %v, want 3", c.counters[countKey].Value)
	}
	if c.counters[sumKey].Value != 600.0 {
		t.Errorf("Sum = %v, want 600.0", c.counters[sumKey].Value)
	}
}

func TestUptimeSeconds(t *testing.T) {
	c := NewCollector()

	uptime := c.UptimeSeconds()
	if uptime < 0 {
		t.Errorf("Uptime = %v, want >= 0", uptime)
	}
}

func TestFormatPrometheus(t *testing.T) {
	c := NewCollector()

	c.Inc("test_counter", "A test counter", nil)
	c.Set("test_gauge", "A test gauge", 42.5, nil)

	output := c.FormatPrometheus()

	if !strings.Contains(output, "# HELP test_counter A test counter") {
		t.Error("Missing HELP for test_counter")
	}
	if !strings.Contains(output, "# TYPE test_counter counter") {
		t.Error("Missing TYPE for test_counter")
	}
	if !strings.Contains(output, "test_counter 1") {
		t.Error("Missing test_counter value")
	}
	if !strings.Contains(output, "# HELP test_gauge A test gauge") {
		t.Error("Missing HELP for test_gauge")
	}
	if !strings.Contains(output, "# TYPE test_gauge gauge") {
		t.Error("Missing TYPE for test_gauge")
	}
	if !strings.Contains(output, "test_gauge 42.500") {
		t.Error("Missing test_gauge value")
	}
	if !strings.Contains(output, "openpool_uptime_seconds") {
		t.Error("Missing uptime metric")
	}
}

func TestFormatPrometheusWithLabels(t *testing.T) {
	c := NewCollector()

	c.Inc("http_requests", "Total HTTP requests", map[string]string{"method": "GET", "status": "200"})
	c.Inc("http_requests", "Total HTTP requests", map[string]string{"method": "POST", "status": "200"})

	output := c.FormatPrometheus()

	if !strings.Contains(output, `method="GET"`) {
		t.Error("Missing GET label")
	}
	if !strings.Contains(output, `method="POST"`) {
		t.Error("Missing POST label")
	}
	if !strings.Contains(output, `status="200"`) {
		t.Error("Missing status label")
	}
}

func TestSnapshot(t *testing.T) {
	c := NewCollector()

	c.Inc("counter1", "Test", nil)
	c.Add("counter2", "Test", 10.0, nil)
	c.Set("gauge1", "Test", 50.0, nil)

	snapshot := c.Snapshot()

	if _, ok := snapshot["uptime_seconds"]; !ok {
		t.Error("Missing uptime_seconds in snapshot")
	}

	counters, ok := snapshot["counters"].(map[string]float64)
	if !ok {
		t.Fatal("counters not a map")
	}
	if len(counters) != 2 {
		t.Errorf("Expected 2 counters, got %d", len(counters))
	}

	gauges, ok := snapshot["gauges"].(map[string]float64)
	if !ok {
		t.Fatal("gauges not a map")
	}
	if len(gauges) != 1 {
		t.Errorf("Expected 1 gauge, got %d", len(gauges))
	}
}

func TestMetricKey(t *testing.T) {
	key := metricKey("test", nil)
	if key != "test" {
		t.Errorf("key = %s, want test", key)
	}

	key = metricKey("test", map[string]string{"a": "1", "b": "2"})
	if !strings.Contains(key, "a=1") {
		t.Error("Missing label a=1")
	}
	if !strings.Contains(key, "b=2") {
		t.Error("Missing label b=2")
	}
}

func TestFormatLabels(t *testing.T) {
	labels := formatLabels(nil)
	if labels != "" {
		t.Errorf("labels = %s, want empty", labels)
	}

	labels = formatLabels(map[string]string{"method": "GET"})
	if !strings.Contains(labels, `method="GET"`) {
		t.Errorf("labels = %s, want method=\"GET\"", labels)
	}
}

func TestConcurrentAccess(t *testing.T) {
	c := NewCollector()

	done := make(chan bool, 100)

	for i := 0; i < 100; i++ {
		go func(id int) {
			c.Inc("concurrent_counter", "Test", map[string]string{"id": string(rune(id))})
			c.Set("concurrent_gauge", "Test", float64(id), nil)
			c.Observe("concurrent_observation", "Test", float64(id), nil)
			c.FormatPrometheus()
			c.Snapshot()
			done <- true
		}(i)
	}

	for i := 0; i < 100; i++ {
		<-done
	}
}
