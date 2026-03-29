;; sandbox.wat — Pure WebAssembly Text (i64 results)
;; run(op, n) -> i64
;; Compile: wat2wasm sandbox.wat -o sandbox.wasm

(module
  (memory (export "memory") 2)

  ;; $sumSquares(n) = 0²+1²+...+(n-1)²
  (func $sumSquares (export "sumSquares") (param $n i32) (result i64)
    (local $sum i64)
    (local $i i32)
    (local.set $sum (i64.const 0))
    (local.set $i (i32.const 0))
    (block $done
      (loop $loop
        (br_if $done (i32.ge_u (local.get $i) (local.get $n)))
        (local.set $sum (i64.add (local.get $sum) (i64.mul (i64.extend_i32_u (local.get $i)) (i64.extend_i32_u (local.get $i)))))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $loop)
      )
    )
    (local.get $sum)
  )

  ;; $matrixTrace(size) = sum of M[i][i] where M[i][j]=i*size+j
  (func $matrixTrace (export "matrixTrace") (param $size i32) (result i64)
    (local $trace i64)
    (local $i i32)
    (local.set $trace (i64.const 0))
    (local.set $i (i32.const 0))
    (block $done
      (loop $loop
        (br_if $done (i32.ge_u (local.get $i) (local.get $size)))
        (local.set $trace (i64.add (local.get $trace) (i64.add (i64.mul (i64.extend_i32_u (local.get $i)) (i64.extend_i32_u (local.get $size))) (i64.extend_i32_u (local.get $i)))))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $loop)
      )
    )
    (local.get $trace)
  )

  ;; $fib(n) = fibonacci(n) using i64
  (func $fib (export "fib") (param $n i32) (result i64)
    (local $a i64)
    (local $b i64)
    (local $i i32)
    (local $tmp i64)
    (local.set $a (i64.const 0))
    (local.set $b (i64.const 1))
    (local.set $i (i32.const 2))
    (if (i32.le_u (local.get $n) (i32.const 1))
      (then (return (i64.extend_i32_u (local.get $n)))))
    (block $exit
      (loop $loop
        (br_if $exit (i32.gt_u (local.get $i) (local.get $n)))
        (local.set $tmp (local.get $a))  ;; save old a
        (local.set $a (local.get $b))    ;; a = old b
        (local.set $b (i64.add (local.get $tmp) (local.get $b)))  ;; b = old a + old b
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $loop)
      )
    )
    (local.get $b)
  )

  ;; $sumFib(n) = sum of fib(0)..fib(n-1)
  (func $sumFib (export "sumFib") (param $n i32) (result i64)
    (local $a i64)
    (local $b i64)
    (local $i i32)
    (local $sum i64)
    (local $tmp i64)
    (local.set $a (i64.const 0))
    (local.set $b (i64.const 1))
    (local.set $sum (i64.const 1))
    (local.set $i (i32.const 2))
    (if (i32.eqz (local.get $n))
      (then (return (i64.const 0))))
    (if (i32.eq (local.get $n) (i32.const 1))
      (then (return (i64.const 1))))
    (block $exit
      (loop $loop
        (br_if $exit (i32.ge_u (local.get $i) (local.get $n)))
        (local.set $tmp (local.get $a))
        (local.set $a (local.get $b))
        (local.set $b (i64.add (local.get $tmp) (local.get $b)))
        (local.set $sum (i64.add (local.get $sum) (local.get $b)))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $loop)
      )
    )
    (local.get $sum)
  )

  ;; run(op, n) -> i64
  (func $run (export "run") (param $op i32) (param $n i32) (result i64)
    (block $op0 (br_if $op0 (i32.ne (local.get $op) (i32.const 0))) (return (call $fib (local.get $n))))
    (block $op1 (br_if $op1 (i32.ne (local.get $op) (i32.const 1))) (return (call $sumFib (local.get $n))))
    (block $op2 (br_if $op2 (i32.ne (local.get $op) (i32.const 2))) (return (call $sumSquares (local.get $n))))
    (block $op3 (br_if $op3 (i32.ne (local.get $op) (i32.const 3))) (return (call $matrixTrace (local.get $n))))
    (i64.const -1)
  )
)