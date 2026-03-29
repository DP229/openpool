;; fib.wat - WebAssembly Text format
;; Fast doubling Fibonacci: fib(n) for n in [0..92]
;; Input:  (i32 n)
;; Output:  (i64 fib_n)
;;
;; Compiled from this Go:
;;   func fib(n int) uint64 {
;;       if n <= 1 { return uint64(n) }
;;       a, b := uint64(0), uint64(1)
;;       for i := 2; i <= n; i++ { a, b = b, a+b }
;;       return b
;;   }

(module
  ;; grow-memory: minimum 1 page (64KB)
  (memory (export "memory") 1)

  ;; fib(n: i32) -> i64
  (func (export "fib") (param $n i32) (result i64)
    (local $a i64)
    (local $b i64)
    (local $i i32)

    ;; if n <= 1: return uint64(n)
    (if (i32.le_s (local.get $n) (i32.const 1))
      (then
        (return (i64.extend_i32_s (local.get $n)))
      )
    )

    ;; a=0, b=1, i=2
    (local.set $a (i64.const 0))
    (local.set $b (i64.const 1))
    (local.set $i (i32.const 2))

    ;; loop for i=2..n
    (block $done
      (loop $loop
        ;; if i > n: break
        (br_if $done (i32.gt_u (local.get $i) (local.get $n)))

        ;; (a, b) = (b, a+b)
        (local.set $a (local.get $b))
        (local.set $b (i64.add (local.get $a) (local.get $b)))

        ;; i++
        (local.set $i (i32.add (local.get $i) (i32.const 1)))

        (br $loop)
      )
    )

    ;; return b
    (local.get $b)
  )

  ;; sum-fib(n: i32) -> i64
  ;; Returns sum of fib(0) + fib(1) + ... + fib(n-1)
  (func (export "sumFib") (param $n i32) (result i64)
    (local $sum i64)
    (local $a i64)
    (local $b i64)
    (local $i i32)

    (local.set $sum (i64.const 0))
    (local.set $a (i64.const 0))
    (local.set $b (i64.const 1))

    (if (i32.eqz (local.get $n))
      (then (return (i64.const 0)))
    )
    (local.set $sum (i64.const 1)) ;; fib(1) = 1

    (local.set $i (i32.const 2))

    (block $done
      (loop $loop
        (br_if $done (i32.ge_u (local.get $i) (local.get $n)))

        (local.set $a (local.get $b))
        (local.set $b (i64.add (local.get $a) (local.get $b)))
        (local.set $sum (i64.add (local.get $sum) (local.get $b)))

        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $loop)
      )
    )

    (local.get $sum)
  )

  ;; sum-squares(n: i32) -> i64
  ;; Returns sum of 0² + 1² + ... + (n-1)² = n(n-1)(2n-1)/6
  (func (export "sumSquares") (param $n i32) (result i64)
    (local $sum i64)
    (local $i i32)

    (local.set $sum (i64.const 0))
    (local.set $i (i32.const 0))

    (block $done
      (loop $loop
        (br_if $done (i32.ge_u (local.get $i) (local.get $n)))

        (local.set $sum (i64.add
          (local.get $sum)
          (i64.mul (i64.extend_i32_u (local.get $i)) (i64.extend_i32_u (local.get $i)))
        ))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $loop)
      )
    )

    (local.get $sum)
  )

  ;; matrix-trace(size: i32) -> i64
  ;; Trace of a 10x10 matrix where M[i][j] = i*10+j
  (func (export "matrixTrace") (param $size i32) (result i64)
    (local $trace i64)
    (local $i i32)
    (local $elem i64)

    (local.set $trace (i64.const 0))
    (local.set $i (i32.const 0))

    (block $done
      (loop $loop
        (br_if $done (i32.ge_u (local.get $i) (local.get $size)))

        ;; M[i][i] = i*size + i
        (local.set $elem (i64.mul (i64.extend_i32_u (local.get $i)) (i64.extend_i32_u (local.get $size))))
        (local.set $trace (i64.add (local.get $trace) (local.get $elem)))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $loop)
      )
    )

    (local.get $trace)
  )
)
