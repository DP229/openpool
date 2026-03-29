// +build tinygo

package main

import (
	"encoding/json"
	"fmt"
)

//export compute
func compute(jsonIn []byte) []byte {
	var input struct {
		N int `json:"n"`
	}
	json.Unmarshal(jsonIn, &input)

	// Fibonacci using fast doubling
	var fib func(n int) uint64
	fib = func(n int) uint64 {
		if n <= 1 {
			return uint64(n)
		}
		a, b := uint64(0), uint64(1)
		for i := 2; i <= n; i++ {
			a, b = b, a+b
		}
		return b
	}

	type Result struct {
		FibN uint64 `json:"fib_n"`
		N     int    `json:"n"`
	}
	result := Result{FibN: fib(input.N), N: input.N}
	out, _ := json.Marshal(result)
	return out
}

func main() {}
