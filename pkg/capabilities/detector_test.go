package capabilities

import (
	"encoding/json"
	"testing"
)

func TestDetect(t *testing.T) {
	info, err := Detect()
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}

	if info == nil {
		t.Fatal("Detect() returned nil")
	}

	if info.CPU.Cores <= 0 {
		t.Errorf("CPU cores = %d, want > 0", info.CPU.Cores)
	}

	if info.CPU.Arch == "" {
		t.Error("CPU arch is empty")
	}

	if info.Memory.TotalGB <= 0 {
		t.Errorf("Memory total = %v, want > 0", info.Memory.TotalGB)
	}
}

func TestCPUInfo(t *testing.T) {
	info, _ := Detect()

	if info.CPU.Model == "" {
		t.Log("Warning: CPU model not detected")
	}

	if info.CPU.FrequencyMHz <= 0 {
		t.Log("Warning: CPU frequency not detected")
	}
}

func TestMemoryInfo(t *testing.T) {
	info, _ := Detect()

	t.Logf("Memory: Total=%.2f GB, Available=%.2f GB",
		info.Memory.TotalGB, info.Memory.AvailableGB)
}

func TestGPUInfo(t *testing.T) {
	info, _ := Detect()

	if info.GPU == nil {
		t.Log("No GPU detected (expected on systems without GPU)")
	} else {
		t.Logf("GPU: %s (%s)", info.GPU.ModelIndex, info.GPU.Vendor)
	}
}

func TestStorageInfo(t *testing.T) {
	info, _ := Detect()

	t.Logf("Storage: Total=%.2f GB, Available=%.2f GB, Type=%s",
		info.Storage.TotalGB, info.Storage.AvailableGB, info.Storage.Type)
}

func TestNetworkInfo(t *testing.T) {
	info, _ := Detect()

	t.Logf("Network info: PublicIP=%s, Country=%s, City=%s",
		info.Network.PublicIP, info.Network.Country, info.Network.City)
}

func TestBenchmark(t *testing.T) {
	info, _ := Detect()

	if info.Benchmark.LastUpdated == 0 {
		t.Log("Benchmark not yet run (expected on first startup)")
	}
}

func TestHardwareInfoJSON(t *testing.T) {
	info, _ := Detect()

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("json.Marshal error = %v", err)
	}

	if len(data) == 0 {
		t.Error("JSON marshal returned empty data")
	}
}

func TestJSONMarshal(t *testing.T) {
	info, _ := Detect()

	if info.CPU.Cores > 0 && info.Memory.TotalGB > 0 && info.Storage.TotalGB > 0 {
		data, err := json.Marshal(info)
		if err != nil {
			t.Fatalf("Failed to marshal: %v", err)
		}

		t.Logf("Hardware info JSON size: %d bytes", len(data))
	}
}
