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
	
	fib := inst.GetFunc(store, "fib")
	
	for n := 0; n <= 15; n++ {
		res, _ := fib.Call(store, int32(n))
		fmt.Printf("fib(%d) = %d\n", n, res)
	}
}
