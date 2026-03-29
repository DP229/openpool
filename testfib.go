//go:build ignore
package main

import (
	"context"
	"fmt"
	"github.com/dp229/openpool/pkg/wasm"
)

func main() {
	r, _ := wasm.New()
	r.LoadModule("/home/durga/projects/openpool/wasm/sandbox.wasm")
	res, _ := r.Run(context.Background(), 0, 30)
	fmt.Printf("fib(30) = %s\n", string(res))
}
