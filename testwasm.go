//go:build ignore
package main

import (
	"context"
	"fmt"
	"log"
	"github.com/dp229/openpool/pkg/wasm"
)

func main() {
	r, err := wasm.New()
	if err != nil { log.Fatal(err) }
	fmt.Println("Runtime version:", r.Version())

	err = r.LoadModule("/home/durga/projects/openpool/wasm/sandbox.wasm")
	if err != nil { log.Fatal("LoadModule:", err) }
	fmt.Println("Module loaded!")

	ctx := context.Background()

	// Test: fib(30)
	res, err := r.Run(ctx, 0, 30)
	if err != nil { log.Fatal("fib(30):", err) }
	fmt.Printf("fib(30) = %s\n", string(res))

	// Test: sumSquares(10000)
	res, err = r.Run(ctx, 2, 10000)
	if err != nil { log.Fatal("sumSquares(10000):", err) }
	fmt.Printf("sumSquares(10000) = %s\n", string(res))

	// Test: matrixTrace(10)
	res, err = r.Run(ctx, 3, 10)
	if err != nil { log.Fatal("matrixTrace(10):", err) }
	fmt.Printf("matrixTrace(10) = %s\n", string(res))
}
