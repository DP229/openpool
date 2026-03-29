//go:build ignore
package main

import (
	"fmt"
	"os"
	"github.com/bytecodealliance/wasmtime-go/v9"
)

func main() {
	engine := wasmtime.NewEngine()
	bytes, _ := os.ReadFile("/home/durga/projects/openpool/wasm/sandbox.wasm")
	module, _ := wasmtime.NewModule(engine, bytes)
	store := wasmtime.NewStore(engine)
	linker := wasmtime.NewLinker(engine)
	linker.DefineWasi()
	inst, _ := linker.Instantiate(store, module)
	
	// Call fib directly
	fib := inst.GetFunc(store, "fib")
	res, err := fib.Call(store, int32(30))
	fmt.Printf("fib(30) = %v, err = %v\n", res, err)
	
	// Also call sumSquares directly
	ss := inst.GetFunc(store, "sumSquares")
	res2, _ := ss.Call(store, int32(10000))
	fmt.Printf("sumSquares(10000) = %v\n", res2)
}
