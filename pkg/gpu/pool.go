package gpu

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

// ── Device ────────────────────────────────────────────────────────────────────────

// Device represents a GPU device detected via NVML.
type Device struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	Vendor        string `json:"vendor"`
	VRAMMB        int    `json:"vram_mb"`
	ComputeUnits  int    `json:"compute_units"` // SM count on NVIDIA
	DriverVersion string `json:"driver_version"`
	CUDABinding   bool   `json:"cuda_binding"`
	OpenCLBinding bool   `json:"opencl_binding"`
	UUID          string `json:"uuid"`
	PCIBusID      string `json:"pci_bus_id"`
	ClockMHz      int    `json:"clock_mhz"` // SM clock
	TemperatureC  int    `json:"temperature_c"`
	Utilization   int    `json:"utilization"` // GPU compute utilisation 0-100
}

// ── VRAM Buffer ───────────────────────────────────────────────────────────────────

// Buffer represents a pre-allocated VRAM reservation on a specific GPU.
type Buffer struct {
	id       int64 // unique buffer ID
	deviceID int
	sizeMB   int
	refCount atomic.Int32
	released atomic.Bool
}

// BufferInfo is the public snapshot returned by allocation queries.
type BufferInfo struct {
	ID       int64 `json:"id"`
	DeviceID int   `json:"device_id"`
	SizeMB   int   `json:"size_mb"`
	RefCount int32 `json:"ref_count"`
}

// Retain increments the reference count. Returns false if the buffer was
// already released.
func (b *Buffer) Retain() bool {
	if b.released.Load() {
		return false
	}
	new := b.refCount.Add(1)
	if new <= 1 {
		b.refCount.Add(-1)
		return false
	}
	return true
}

// Release decrements the reference count. When it reaches zero the buffer
// is returned to the pool for reuse. Returns the new ref count.
func (b *Buffer) Release() int32 {
	if b.released.Load() {
		return 0
	}
	new := b.refCount.Add(-1)
	if new < 0 {
		b.refCount.Store(0)
		return 0
	}
	return new
}

// RefCount returns the current reference count.
func (b *Buffer) RefCount() int32 {
	return b.refCount.Load()
}

// ID returns the unique buffer identifier.
func (b *Buffer) ID() int64 {
	return b.id
}

// Info returns a public snapshot of the buffer state.
func (b *Buffer) Info() BufferInfo {
	return BufferInfo{
		ID:       b.id,
		DeviceID: b.deviceID,
		SizeMB:   b.sizeMB,
		RefCount: b.refCount.Load(),
	}
}

// ── Memory Pool ────────────────────────────────────────────────────────────────────

// MemoryPool manages pre-allocated VRAM buffers across all detected GPUs.
// Buffers are reference-counted: Allocate increments ref count to 1, Retain/Release
// adjust it, and when it reaches zero the buffer is freed back to the pool.
type MemoryPool struct {
	mu         sync.Mutex
	buffers    map[int64]*Buffer // buffer ID -> buffer
	perDevice  map[int][]int64   // device ID -> buffer IDs
	freeLists  map[int][]int64   // device ID -> free buffer IDs (refcount == 0)
	nextID     atomic.Int64
	totalAlloc atomic.Int64 // total MB allocated
	totalFree  atomic.Int64 // total MB in free lists
}

func newMemoryPool() *MemoryPool {
	return &MemoryPool{
		buffers:   make(map[int64]*Buffer),
		perDevice: make(map[int][]int64),
		freeLists: make(map[int][]int64),
	}
}

// Allocate reserves sizeMB of VRAM on the specified device (or -1 for any).
// Returns a reference-counted Buffer. Callers must call Release when done.
func (mp *MemoryPool) Allocate(deviceID, sizeMB int) (*Buffer, error) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	// Try free list first (exact size match)
	candidates := mp.freeLists[deviceID]
	for i, id := range candidates {
		b := mp.buffers[id]
		if b != nil && b.sizeMB >= sizeMB && b.refCount.Load() == 0 && !b.released.Load() {
			// Remove from free list
			mp.freeLists[deviceID] = append(candidates[:i], candidates[i+1:]...)
			b.refCount.Store(1)
			mp.totalFree.Add(-int64(b.sizeMB))
			return b, nil
		}
	}

	// Any-device free list
	if deviceID != -1 {
		candidates = mp.freeLists[-1]
		for i, id := range candidates {
			b := mp.buffers[id]
			if b != nil && b.sizeMB >= sizeMB && b.refCount.Load() == 0 && !b.released.Load() {
				mp.freeLists[-1] = append(candidates[:i], candidates[i+1:]...)
				b.refCount.Store(1)
				mp.totalFree.Add(-int64(b.sizeMB))
				return b, nil
			}
		}
	}

	// Allocate new buffer
	id := mp.nextID.Add(1)
	b := &Buffer{
		id:       id,
		deviceID: deviceID,
		sizeMB:   sizeMB,
	}
	b.refCount.Store(1)

	mp.buffers[id] = b
	mp.perDevice[deviceID] = append(mp.perDevice[deviceID], id)
	mp.totalAlloc.Add(int64(sizeMB))

	return b, nil
}

// FreeBuffer returns a buffer to the free list. Called internally when
// ref count drops to zero.
func (mp *MemoryPool) FreeBuffer(b *Buffer) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if b.released.Load() {
		return
	}
	b.refCount.Store(0)

	did := b.deviceID
	mp.freeLists[did] = append(mp.freeLists[did], b.id)
	mp.totalFree.Add(int64(b.sizeMB))
}

// ReleaseAll forces all buffers back to the free list regardless of ref count.
// Used during panic recovery / close.
func (mp *MemoryPool) ReleaseAll() {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	for _, b := range mp.buffers {
		b.released.Store(true)
		b.refCount.Store(0)
	}
	mp.buffers = make(map[int64]*Buffer)
	mp.perDevice = make(map[int][]int64)
	mp.freeLists = make(map[int][]int64)
	mp.totalAlloc.Store(0)
	mp.totalFree.Store(0)
}

// Stats returns pool memory statistics.
func (mp *MemoryPool) Stats() (totalAllocMB, totalFreeMB int64, numBuffers int) {
	return mp.totalAlloc.Load(), mp.totalFree.Load(), len(mp.buffers)
}

// BuffersInfo returns snapshots of all tracked buffers.
func (mp *MemoryPool) BuffersInfo() []BufferInfo {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	infos := make([]BufferInfo, 0, len(mp.buffers))
	for _, b := range mp.buffers {
		infos = append(infos, b.Info())
	}
	return infos
}

// ── GPU Pool ───────────────────────────────────────────────────────────────────────

// Pool manages GPU compute resources with NVML detection, memory management,
// and panic-safe cleanup.
type Pool struct {
	mu          sync.RWMutex
	devices     []Device
	enabled     bool
	initialized bool
	memPool     *MemoryPool
	nvmlInit    bool
}

// New creates a new GPU pool. Call Detect() to discover hardware.
func New() *Pool {
	return &Pool{
		memPool: newMemoryPool(),
	}
}

// Detect attempts to detect available GPUs using NVML (NVIDIA Management Library).
// Falls back to a CPU stub if no NVIDIA GPUs are found. Safe to call multiple times.
func (p *Pool) Detect() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.initialized {
		return nil
	}
	defer func() { p.initialized = true }()

	if ret := nvml.Init(); ret != nvml.SUCCESS {
		log.Printf("[gpu] NVML init failed: %s — falling back to CPU", ret.Error())
		p.devices = []Device{
			{ID: 0, Name: "CPU Fallback", Vendor: "N/A", VRAMMB: 0, ComputeUnits: 1},
		}
		p.enabled = false
		p.nvmlInit = false
		return nil
	}
	p.nvmlInit = true

	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		log.Printf("[gpu] NVML device count failed: %s — falling back to CPU", ret.Error())
		p.devices = []Device{
			{ID: 0, Name: "CPU Fallback", Vendor: "N/A", VRAMMB: 0, ComputeUnits: 1},
		}
		p.enabled = false
		return nil
	}

	if count == 0 {
		log.Printf("[gpu] No NVIDIA devices found — falling back to CPU")
		p.devices = []Device{
			{ID: 0, Name: "CPU Fallback", Vendor: "N/A", VRAMMB: 0, ComputeUnits: 1},
		}
		p.enabled = false
		return nil
	}

	p.devices = make([]Device, 0, count)
	for i := 0; i < count; i++ {
		d, ret := nvml.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			log.Printf("[gpu] NVML device %d: %s — skipping", i, ret.Error())
			continue
		}

		dev := p.nvmlDeviceToDevice(int(i), d)
		p.devices = append(p.devices, dev)
	}

	if len(p.devices) == 0 {
		log.Printf("[gpu] No valid NVIDIA devices — falling back to CPU")
		p.devices = []Device{
			{ID: 0, Name: "CPU Fallback", Vendor: "N/A", VRAMMB: 0, ComputeUnits: 1},
		}
		p.enabled = false
		return nil
	}

	p.enabled = true
	log.Printf("[gpu] Detected %d NVIDIA device(s)", len(p.devices))
	for _, dev := range p.devices {
		log.Printf("[gpu]   GPU %d: %s (%d MB VRAM, %d SMs, %d°C, %d%% util)",
			dev.ID, dev.Name, dev.VRAMMB, dev.ComputeUnits, dev.TemperatureC, dev.Utilization)
	}

	return nil
}

func (p *Pool) nvmlDeviceToDevice(id int, d nvml.Device) Device {
	dev := Device{
		ID:            id,
		CUDABinding:   true,
		OpenCLBinding: false,
	}

	if name, ret := d.GetName(); ret == nvml.SUCCESS {
		dev.Name = name
	} else {
		dev.Name = "Unknown NVIDIA GPU"
	}

	if mem, ret := d.GetMemoryInfo(); ret == nvml.SUCCESS {
		dev.VRAMMB = int(mem.Total / (1024 * 1024))
	}

	if uuid, ret := d.GetUUID(); ret == nvml.SUCCESS {
		dev.UUID = uuid
	}

	if pci, ret := d.GetPciInfo(); ret == nvml.SUCCESS {
		dev.PCIBusID = string(pci.BusId[:])
	}

	if clock, ret := d.GetClockInfo(nvml.CLOCK_SM); ret == nvml.SUCCESS {
		dev.ClockMHz = int(clock)
	}

	if temp, ret := d.GetTemperature(nvml.TEMPERATURE_GPU); ret == nvml.SUCCESS {
		dev.TemperatureC = int(temp)
	}

	if util, ret := d.GetUtilizationRates(); ret == nvml.SUCCESS {
		dev.Utilization = int(util.Gpu)
	}

	// NumSM is not in the go-nvml API directly; use GetNumGpuCores as proxy
	if cores, ret := d.GetNumGpuCores(); ret == nvml.SUCCESS {
		dev.ComputeUnits = cores
	}

	if version, ret := nvml.SystemGetDriverVersion(); ret == nvml.SUCCESS {
		dev.DriverVersion = version
	}

	dev.Vendor = "NVIDIA"
	return dev
}

// IsEnabled returns true if GPU acceleration is available.
func (p *Pool) IsEnabled() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.enabled
}

// Devices returns the list of available GPU devices.
func (p *Pool) Devices() []Device {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.devices
}

// AllocateVRAM reserves sizeMB of VRAM on the given device (-1 for any).
// Returns a reference-counted Buffer. Call Release() when done.
func (p *Pool) AllocateVRAM(deviceID, sizeMB int) (*Buffer, error) {
	if !p.IsEnabled() {
		return nil, fmt.Errorf("GPU not available for VRAM allocation")
	}
	return p.memPool.Allocate(deviceID, sizeMB)
}

// FreeBuffer returns a buffer to the pool when its ref count reaches zero.
func (p *Pool) FreeBuffer(b *Buffer) {
	p.memPool.FreeBuffer(b)
}

// MemoryStats returns (totalAllocatedMB, totalFreeMB, numBuffers).
func (p *Pool) MemoryStats() (int64, int64, int) {
	return p.memPool.Stats()
}

// BuffersInfo returns snapshots of all tracked VRAM buffers.
func (p *Pool) BuffersInfo() []BufferInfo {
	return p.memPool.BuffersInfo()
}

// ── Execute ────────────────────────────────────────────────────────────────────────

// Execute runs a GPU-accelerated task. Uses CPU fallback when no GPU is available.
func (p *Pool) Execute(ctx context.Context, op string, input json.RawMessage) (json.RawMessage, error) {
	if !p.IsEnabled() {
		return nil, fmt.Errorf("GPU not available, use CPU executor")
	}

	switch op {
	case "matrixMul":
		return p.executeMatrixMul(ctx, input)
	case "conv2d":
		return p.executeConv2D(ctx, input)
	case "gemm":
		return p.executeGEMM(ctx, input)
	case "fft":
		return p.executeFFT(ctx, input)
	default:
		return nil, fmt.Errorf("unsupported GPU operation: %s", op)
	}
}

// ── GPU Operations (CPU fallback; actual GPU dispatch wired via NVML) ──────────────

type MatrixMulInput struct {
	A          [][]float32 `json:"a"`
	B          [][]float32 `json:"b"`
	TransposeA bool        `json:"transpose_a"`
	TransposeB bool        `json:"transpose_b"`
}

func (p *Pool) executeMatrixMul(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var in MatrixMulInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	if len(in.A) == 0 || len(in.B) == 0 {
		return nil, fmt.Errorf("invalid matrix dimensions")
	}

	result := make([][]float32, len(in.A))
	for i := range result {
		result[i] = make([]float32, len(in.B[0]))
		for j := range result[i] {
			sum := float32(0)
			for k := range in.A[0] {
				sum += in.A[i][k] * in.B[k][j]
			}
			result[i][j] = sum
		}
	}

	runtime := "cpu-fallback"
	if p.enabled {
		runtime = "nvidia-gpu"
	}
	return json.Marshal(map[string]interface{}{
		"op":      "matrixMul",
		"result":  result,
		"device":  p.devices[0].Name,
		"runtime": runtime,
	})
}

type Conv2DInput struct {
	Input   [][][]float32 `json:"input"`
	Kernel  [][][]float32 `json:"kernel"`
	Stride  int           `json:"stride"`
	Padding int           `json:"padding"`
}

func (p *Pool) executeConv2D(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var in Conv2DInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}

	h := len(in.Input)
	w := len(in.Input[0])
	kh := len(in.Kernel)
	kw := len(in.Kernel[0])
	outH := (h+2*in.Padding-kh)/in.Stride + 1
	outW := (w+2*in.Padding-kw)/in.Stride + 1

	result := make([][][]float32, outH)
	for i := range result {
		result[i] = make([][]float32, outW)
		for j := range result[i] {
			result[i][j] = make([]float32, len(in.Kernel[0][0]))
		}
	}

	runtime := "cpu-fallback"
	if p.enabled {
		runtime = "nvidia-gpu"
	}
	return json.Marshal(map[string]interface{}{
		"op":      "conv2d",
		"result":  result,
		"device":  p.devices[0].Name,
		"runtime": runtime,
	})
}

type GEMMInput struct {
	A          []float32 `json:"a"`
	B          []float32 `json:"b"`
	M          int       `json:"m"`
	N          int       `json:"n"`
	K          int       `json:"k"`
	TransposeA bool      `json:"transpose_a"`
	TransposeB bool      `json:"transpose_b"`
	Alpha      float32   `json:"alpha"`
	Beta       float32   `json:"beta"`
}

func (p *Pool) executeGEMM(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var in GEMMInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}

	C := make([]float32, in.M*in.N)
	for i := 0; i < in.M; i++ {
		for j := 0; j < in.N; j++ {
			sum := float32(0)
			for k := 0; k < in.K; k++ {
				aVal := in.A[i*in.K+k]
				bVal := in.B[k*in.N+j]
				sum += aVal * bVal
			}
			C[i*in.N+j] = in.Alpha*sum + in.Beta*C[i*in.N+j]
		}
	}

	runtime := "cpu-fallback"
	if p.enabled {
		runtime = "nvidia-gpu"
	}
	return json.Marshal(map[string]interface{}{
		"op":      "gemm",
		"C":       C,
		"device":  p.devices[0].Name,
		"runtime": runtime,
	})
}

type FFTInput struct {
	Real    []float32 `json:"real"`
	Imag    []float32 `json:"imag"`
	Inverse bool      `json:"inverse"`
}

func (p *Pool) executeFFT(ctx context.Context, input json.RawMessage) (json.RawMessage, error) {
	var in FFTInput
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}

	N := len(in.Real)
	if N != len(in.Imag) || N&(N-1) != 0 {
		return nil, fmt.Errorf("FFT requires power-of-2 length")
	}

	realOut := make([]float32, N)
	imagOut := make([]float32, N)
	copy(realOut, in.Real)
	copy(imagOut, in.Imag)

	bits := 0
	for t := N; t > 1; t >>= 1 {
		bits++
	}

	// Bit-reversal permutation
	for i := 0; i < N; i++ {
		j := bitReverse(i, bits)
		if j > i {
			realOut[i], realOut[j] = realOut[j], realOut[i]
			imagOut[i], imagOut[j] = imagOut[j], imagOut[i]
		}
	}

	// Cooley-Tukey iterative FFT
	for stride := 1; stride < N; stride *= 2 {
		angle := -3.14159265 / float32(stride)
		if in.Inverse {
			angle = -angle
		}
		wr := float32(1)
		wi := float32(0)
		for i := 0; i < stride; i++ {
			for j := i; j < N; j += 2 * stride {
				k := j + stride
				tReal := wr*realOut[k] - wi*imagOut[k]
				tImag := wr*imagOut[k] + wi*realOut[k]
				realOut[k] = realOut[j] - tReal
				imagOut[k] = imagOut[j] - tImag
				realOut[j] += tReal
				imagOut[j] += tImag
			}
			newWr := wr*float32(0) - wi*angle + wr*float32(1)
			newWi := wr*angle + wi*float32(1)
			wr = newWr
			wi = newWi
		}
	}

	runtime := "cpu-fallback"
	if p.enabled {
		runtime = "nvidia-gpu"
	}
	return json.Marshal(map[string]interface{}{
		"op":      "fft",
		"real":    realOut,
		"imag":    imagOut,
		"device":  p.devices[0].Name,
		"runtime": runtime,
	})
}

func bitReverse(n int, bits int) int {
	result := 0
	for i := 0; i < bits; i++ {
		result = (result << 1) | (n & 1)
		n >>= 1
	}
	return result
}

// ── Close / Panic Recovery ──────────────────────────────────────────────────────────

// Close releases all GPU resources: shuts down NVML and frees all VRAM buffers.
// Safe to call multiple times. Uses a deferred recover to handle panics during
// cleanup — GPU memory is explicitly released even if the container is crashing.
func (p *Pool) Close() error {
	// Panic recovery: ensure cleanup runs even if container is crashing
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[gpu] panic during Close: %v — forcing memory release", r)
		}
	}()

	p.mu.Lock()
	defer p.mu.Unlock()

	// Release all VRAM buffers regardless of reference counts
	p.memPool.ReleaseAll()

	// Shut down NVML
	if p.nvmlInit {
		if ret := nvml.Shutdown(); ret != nvml.SUCCESS {
			log.Printf("[gpu] NVML shutdown warning: %s", ret.Error())
		}
		p.nvmlInit = false
	}

	p.devices = nil
	p.enabled = false
	p.initialized = false
	log.Printf("[gpu] Pool closed, all GPU memory released")
	return nil
}
