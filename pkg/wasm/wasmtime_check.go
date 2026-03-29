//go:build ignore

package main

import (
	"fmt"
	"github.com/bytecodealliance/wasmtime-go/v9"
)

func main() {
	engine := wasmtime.NewEngine()
	store := wasmtime.NewStore(engine)
	fmt.Println("wasmtime-go initialized OK, engine:", engine)
	_ = store
}
