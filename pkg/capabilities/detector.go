// Package capabilities detects hardware capabilities for task matching.
package capabilities

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// HardwareInfo describes the node's hardware capabilities.
type HardwareInfo struct {
	CPU       CPUInfo     `json:"cpu"`
	Memory    MemInfo     `json:"memory"`
	GPU       *GPUInfo    `json:"gpu,omitempty"`
	Storage   StorageInfo `json:"storage"`
	Network   NetworkInfo `json:"network"`
	Benchmark Benchmark   `json:"benchmark"`
}

// CPUInfo describes CPU capabilities.
type CPUInfo struct {
	Cores        int      `json:"cores"`
	Model        string   `json:"model"`
	Arch         string   `json:"arch"`
	FrequencyMHz int      `json:"frequency_mhz"`
	Features     []string `json:"features,omitempty"`
}

// MemInfo describes memory capabilities.
type MemInfo struct {
	TotalGB     float64 `json:"total_gb"`
	AvailableGB float64 `json:"available_gb"`
	SwapGB      float64 `json:"swap_gb"`
}

// GPUInfo describes GPU capabilities.
type GPUInfo struct {
	Present      bool   `json:"present"`
	ModelIndex   string `json:"model"`
	Vendor       string `json:"vendor"`
	VRAMGB       int    `json:"vram_gb"`
	ComputeUnits int    `json:"compute_units"`
	API          string `json:"api"` // cuda, opencl, metal
	APIVersion   string `json:"api_version"`
}

// StorageInfo describes storage capabilities.
type StorageInfo struct {
	TotalGB     float64 `json:"total_gb"`
	AvailableGB float64 `json:"available_gb"`
	Type        string  `json:"type"` // ssd, hdd, nvme
}

// NetworkInfo describes network capabilities.
type NetworkInfo struct {
	PublicIP string `json:"public_ip"`
	// BandwidthMbps int    `json:"bandwidth_mbps"`
	Country string `json:"country"`
	City    string `json:"city"`
}

// Benchmark contains performance scores.
type Benchmark struct {
	CPUScore     float64 `json:"cpu_score"`
	MemBandwidth float64 `json:"mem_bandwidth_gb_s"`
	DiskIO       float64 `json:"disk_io_mb_s"`
	NetworkLat   float64 `json:"network_latency_ms"`
	GPUScore     float64 `json:"gpu_score,omitempty"`
	LastUpdated  int64   `json:"last_updated"`
}

// Detect returns hardware capabilities for this node.
func Detect() (*HardwareInfo, error) {
	info := &HardwareInfo{}

	// CPU detection
	info.CPU = detectCPU()

	// Memory detection
	info.Memory = detectMemory()

	// GPU detection
	info.GPU = detectGPU()

	// Storage detection
	info.Storage = detectStorage()

	// Network detection (basic)
	info.Network = NetworkInfo{}

	// Benchmarks
	info.Benchmark = Benchmark{}

	return info, nil
}

func detectCPU() CPUInfo {
	cpu := CPUInfo{
		Cores: runtime.NumCPU(),
		Arch:  runtime.GOARCH,
	}

	// Try to get model name on Linux
	if data, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "model name") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					cpu.Model = strings.TrimSpace(parts[1])
					break
				}
			}
			if strings.HasPrefix(line, "cpu MHz") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					if mhz, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64); err == nil {
						cpu.FrequencyMHz = int(mhz)
					}
				}
			}
		}
	}

	return cpu
}

func detectMemory() MemInfo {
	// Use runtime for total memory estimate
	mem := MemInfo{
		TotalGB: float64(getSysmemTotal()) / 1024 / 1024 / 1024,
	}

	// Try to get available memory on Linux
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			if strings.HasPrefix(line, "MemAvailable:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					if kb, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
						mem.AvailableGB = float64(kb) / 1024 / 1024
					}
				}
			}
			if strings.HasPrefix(line, "SwapTotal:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					if kb, err := strconv.ParseInt(fields[1], 10, 64); err == nil {
						mem.SwapGB = float64(kb) / 1024 / 1024
					}
				}
			}
		}
	}

	// Fallback if not available
	if mem.AvailableGB == 0 {
		mem.AvailableGB = mem.TotalGB * 0.5 // Estimate 50% available
	}

	return mem
}

func detectGPU() *GPUInfo {
	// Try CUDA first
	if gpu := detectNVIDIA(); gpu != nil {
		return gpu
	}

	// Try OpenCL
	if gpu := detectOpenCL(); gpu != nil {
		return gpu
	}

	// No GPU
	return &GPUInfo{
		Present: false,
	}
}

func detectNVIDIA() *GPUInfo {
	// Check for nvidia-smi
	output, err := exec.Command("nvidia-smi", "--query-gpu=name,memory.total", "--format=csv,noheader").CombinedOutput()
	if err != nil {
		return nil
	}

	lines := strings.Split(string(output), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return nil
	}

	// Take first GPU
	parts := strings.Split(lines[0], ",")
	if len(parts) < 2 {
		return nil
	}

	name := strings.TrimSpace(parts[0])
	vram := 0

	// Parse VRAM
	if len(parts) >= 2 {
		vramStr := strings.TrimSpace(parts[1])
		vramStr = strings.ReplaceAll(vramStr, " MiB", "")
		vramStr = strings.TrimSpace(vramStr)
		if mb, err := strconv.Atoi(vramStr); err == nil {
			vram = mb / 1024 // Convert to GB
		}
	}

	return &GPUInfo{
		Present:    true,
		ModelIndex: name,
		Vendor:     "NVIDIA",
		VRAMGB:     vram,
		API:        "cuda",
	}
}

func detectOpenCL() *GPUInfo {
	// Basic OpenCL detection
	// In production, this would use go-opencl bindings
	return nil
}

func detectStorage() StorageInfo {
	storage := StorageInfo{}

	// Get current directory disk usage
	// In production, this would check all filesystem mounts
	output, err := exec.Command("df", "-BG", ".").CombinedOutput()
	if err == nil {
		lines := strings.Split(string(output), "\n")
		if len(lines) >= 2 {
			fields := strings.Fields(lines[1])
			if len(fields) >= 4 {
				// fields[1] = Used, fields[2] = Available, fields[3] = Use%
				availStr := strings.TrimSuffix(fields[3], "G")
				if avail, err := strconv.ParseFloat(availStr, 64); err == nil {
					storage.AvailableGB = avail
				}
			}
		}
	}

	// Classify as SSD or HDD based on /sys/block
	storage.Type = "unknown"
	if output, err := exec.Command("lsblk", "-d", "-o", "ROTA").CombinedOutput(); err == nil {
		lines := strings.Split(string(output), "\n")
		for _, line := range lines[1:] {
			line = strings.TrimSpace(line)
			if line == "0" {
				storage.Type = "ssd"
				break
			} else if line == "1" {
				storage.Type = "hdd"
				break
			}
		}
	}

	return storage
}

// GetNodeInfo returns NodeInfo for the marketplace.
func GetNodeInfo() map[string]interface{} {
	info, _ := Detect()

	nodeInfo := map[string]interface{}{
		"cpu_cores":    info.CPU.Cores,
		"cpu_arch":     info.CPU.Arch,
		"cpu_model":    info.CPU.Model,
		"ram_gb":       int(info.Memory.AvailableGB),
		"storage_gb":   int(info.Storage.AvailableGB),
		"storage_type": info.Storage.Type,
	}

	if info.GPU != nil && info.GPU.Present {
		nodeInfo["gpu"] = map[string]interface{}{
			"present": true,
			"model":   info.GPU.ModelIndex,
			"vendor":  info.GPU.Vendor,
			"vram_gb": info.GPU.VRAMGB,
		}
	} else {
		nodeInfo["gpu"] = nil
	}

	return nodeInfo
}

func getSysmemTotal() int64 {
	// Estimate based on runtime
	// In production, read /proc/meminfo on Linux
	return int64(8 * 1024 * 1024 * 1024) // 8GB default estimate
}
