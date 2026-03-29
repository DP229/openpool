//go:build tinygo

package main

import (
	"encoding/json"
	"unsafe"
)

// Memory layout for WASM module:
// - input_buf:  [0, 1024)       — executor writes JSON input here
// - output_buf: [1024, 2048)    — module writes JSON output here
// - buf_size: 1024 bytes each
const (
	inputBuf  = 0
	outputBuf = 1024
	bufSize   = 1024
)

// makeBuf creates a byte slice from WASM memory at the given offset.
func makeBuf(offset uint32, size uint32) []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(uintptr(offset))), size)
}

// wasmJson is a simple JSON input/output format.
type wasmInput struct {
	Op   string `json:"op"`   // "fib", "sumFib", "sumSquares", "matrixTrace"
	N    int    `json:"n"`    // input number
	Size int    `json:"size"` // matrix size
}

type wasmOutput struct {
	Op    string `json:"op"`
	Input int    `json:"input"`
	Result uint64 `json:"result"`
	Status string `json:"status"`
}

// fib computes fibonacci iteratively.
func fib(n int) uint64 {
	if n <= 1 {
		return uint64(n)
	}
	a, b := uint64(0), uint64(1)
	for i := 2; i <= n; i++ {
		a, b = b, a+b
	}
	return b
}

// sumFib computes sum of fib(0)..fib(n-1).
func sumFib(n int) uint64 {
	if n <= 0 {
		return 0
	}
	if n == 1 {
		return 1
	}
	a, b := uint64(0), uint64(1)
	sum := uint64(1)
	for i := 2; i < n; i++ {
		a, b = b, a+b
		sum += b
	}
	return sum
}

// sumSquares computes sum of 0²..(n-1)².
func sumSquares(n int) uint64 {
	var sum uint64
	for i := 0; i < n; i++ {
		sum += uint64(i * i)
	}
	return sum
}

// matrixTrace computes trace of size×size matrix where M[i][j]=i*size+j.
func matrixTrace(size int) uint64 {
	var trace uint64
	for i := 0; i < size; i++ {
		trace += uint64(i*size + i)
	}
	return trace
}

//export run
//go:export run
func run(inputPtr, inputLen uint32) uint32 {
	// Read input from WASM memory
	inBuf := makeBuf(inputPtr, inputLen)

	// Parse JSON input
	var in wasmInput
	if err := json.Unmarshal(inBuf, &in); err != nil {
		return 0
	}

	var result uint64
	switch in.Op {
	case "fib":
		result = fib(in.N)
	case "sumFib":
		result = sumFib(in.N)
	case "sumSquares":
		result = sumSquares(in.N)
	case "matrixTrace":
		result = matrixTrace(in.Size)
	default:
		return 0
	}

	// Build output JSON
	out := wasmOutput{
		Op:     in.Op,
		Input:  in.N,
		Result: result,
		Status: "ok",
	}
	outJSON, _ := json.Marshal(out)

	// Write output to WASM memory (outputBuf)
	if len(outJSON) >= bufSize {
		outJSON = outJSON[:bufSize-1]
	}
	copy(makeBuf(outputBuf, bufSize), outJSON)

	// Return length of output written (null-terminated)
	return uint32(len(outJSON))
}

//export memSize
//go:export memSize
func memSize() uint32 {
	return 2048 // 2KB total memory
}

func main() {}
