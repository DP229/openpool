// WebAssembly Text (WAT) — pure text, compiled to WASM
// No runtime dependencies, no memory management issues
//
// Compiled from a simple C-like algorithm
// Computes: fib(n), sumFib(n), sumSquares(n), matrixTrace(size)
//
// Compile with: wat2wasm sandbox.wat -o sandbox.wasm
// Validate:    wasm-validate sandbox.wasm
// Test:        wasmtime --allow-precompiled sandbox.wasm --invoke run 30
//   (note: wasmtime doesn't print i64 returns; use --wat flag to see output)

(module
  ;; 128KB memory (2 pages)
  (memory (export "memory") 2)

  ;; ── Helper: i64 absolute value ────────────────────────────────────────
  ;; ── Core algorithms ──────────────────────────────────────────────────

  ;; sumSquares(n) — sum of 0² + 1² + ... + (n-1)²
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
          (i64.mul
            (i64.extend_i32_u (local.get $i))
            (i64.extend_i32_u (local.get $i))
          )
        ))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $loop)
      )
    )
    (local.get $sum)
  )

  ;; matrixTrace(size) — trace of M[i][j] = i*size + j
  (func (export "matrixTrace") (param $size i32) (result i64)
    (local $trace i64)
    (local $i i32)
    (local $elem i64)
    (local $size64 i64)
    (local.set $trace (i64.const 0))
    (local.set $size64 (i64.extend_i32_u (local.get $size)))
    (local.set $i (i32.const 0))
    (block $done
      (loop $loop
        (br_if $done (i32.ge_u (local.get $i) (local.get $size)))
        ;; elem = i*size + i
        (local.set $elem (i64.add
          (i64.mul (i64.extend_i32_u (local.get $i)) (local.get $size64))
          (i64.extend_i32_u (local.get $i))
        ))
        (local.set $trace (i64.add (local.get $trace) (local.get $elem)))
        (local.set $i (i32.add (local.get $i) (i32.const 1)))
        (br $loop)
      )
    )
    (local.get $trace)
  )

  ;; ── JSON output helpers ────────────────────────────────────────────────
  ;; Write a digit (0-9) to memory at [offset] and return next offset
  (func $writeDigit (param $val i64) (param $offset i32) (result i32)
    (local $digit i32)
    (local $pos i32)
    (local.set $digit (i32.trunc_sat_i64_u (local.get $val)))
    (local.set $pos (i32.add (local.get $offset) (i32.const 0)))

    ;; Write character to memory (memory is exported so we can write from Go)
    (i32.store8 (local.get $offset)
      (i32.add (i32.const 48) (i32.rem_u (local.get $digit) (i32.const 10)))
    )

    ;; Null terminator
    (i32.store8 (local.get $offset) (i32.const 0))
    (i32.add (local.get $offset) (i32.const 1))
  )

  ;; writeHex64 — write uint64 as decimal string to memory[ptr], return new ptr
  (func (export "writeU64") (param $val i64) (param $ptr i32) (result i32)
    (local $buf i32)
    (local $tmp i64)
    (local $digit i32)
    (local $written i32)
    (local $start i32)

    (local.set $buf (local.get $ptr))
    (local.set $start (local.get $ptr))

    (if (i64.eqz (local.get $val))
      (then
        (i32.store8 (local.get $buf) (i32.const 48))
        (i32.store8 (i32.add (local.get $buf) (i32.const 1)) (i32.const 0))
        (return (i32.add (local.get $buf) (i32.const 2)))
      )
    )

    ;; Build digits in reverse
    (block $done
      (loop $digitLoop
        (br_if $done (i64.eqz (local.get $val)))
        (local.set $tmp (i64.div_u (local.get $val) (i64.const 10)))
        (local.set $digit (i32.trunc_sat_i64_u
          (i64.sub (local.get $val) (i64.mul (local.get $tmp) (i64.const 10)))
        ))
        (i32.store8 (local.get $buf) (i32.add (i32.const 48) (local.get $digit)))
        (local.set $buf (i32.add (local.get $buf) (i32.const 1)))
        (local.set $val (local.get $tmp))
        (br $digitLoop)
      )
    )

    ;; Reverse digits in place
    (local.set $written (local.get $buf))
    (local.set $buf (i32.sub (local.get $buf) (i32.const 1)))

    (block $revDone
      (loop $revLoop
        (br_if $revDone (i32.ge_u (local.get $ptr) (local.get $buf)))
        (local.set $digit (i32.load8_u (local.get $ptr)))
        (i32.store8 (local.get $ptr) (i32.load8_u (local.get $buf)))
        (i32.store8 (local.get $buf) (local.get $digit))
        (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
        (local.set $buf (i32.sub (local.get $buf) (i32.const 1)))
        (br $revLoop)
      )
    )

    ;; Null terminator
    (i32.store8 (local.get $written) (i32.const 0))
    (i32.add (local.get $written) (i32.const 1))
  )

  ;; writeI32 — write int32 as decimal string to memory[ptr], return new ptr
  (func (export "writeI32") (param $val i32) (param $ptr i32) (result i32)
    (local $val64 i64)
    (local.set $val64 (i64.extend_i32_s (local.get $val)))
    (if (i64.lt_s (local.get $val64) (i64.const 0))
      (then
        (i32.store8 (local.get $ptr) (i32.const 45))
        (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
        (local.set $val64 (i64.sub (i64.const 0) (local.get $val64)))
      )
    )
    (local.get $ptr)
  )

  ;; writeStr — write null-terminated string from [src] to [dst], return new dst
  (func $writeStr (param $src i32) (param $dst i32) (result i32)
    (local $ch i32)
    (local $srcStart i32)
    (local.set $srcStart (local.get $src))
    (block $done
      (loop $loop
        (local.set $ch (i32.load8_u (local.get $src)))
        (br_if $done (i32.eqz (local.get $ch)))
        (i32.store8 (local.get $dst) (local.get $ch))
        (local.set $src (i32.add (local.get $src) (i32.const 1)))
        (local.set $dst (i32.add (local.get $dst) (i32.const 1)))
        (br $loop)
      )
    )
    (local.get $dst)
  )

  ;; ── Main: run(op: i32, arg: i32) → output length ────────────────────
  ;; ops: 0=fib(n), 1=sumFib(n), 2=sumSquares(n), 3=matrixTrace(n)
  ;; Output written to memory[2048], returns output length
  (func (export "run") (param $op i32) (param $arg i32) (result i32)
    (local $ptr i32)
    (local $result i64)
    (local $n i32)

    (local.set $ptr (i32.const 2048)) ;; output buffer starts at offset 2048
    (local.set $n (local.get $arg))

    ;; Write: {"op":"fib","n":N,"result":R}
    ;; op 0 = fib, 1 = sumFib, 2 = sumSquares, 3 = matrixTrace

    ;; {"op":
    (i32.store8 (local.get $ptr) (i32.const 123))  ;; {
    (local.set $ptr (call $writeStr (i32.const 12345) (i32.add (local.get $ptr) (i32.const 1)))) ;; "op":
    (i32.store8 (local.get $ptr) (i32.const 32))   ;; space
    (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
    (i32.store8 (local.get $ptr) (i32.const 102))  ;; f
    (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))

    (block $switch (br_table $switch (local.get $op) (i32.const 3))

      ;; op 0: fib
      (block $case0
        (br_if $case0 (i32.ne (local.get $op) (i32.const 0)))
        (i32.store8 (local.get $ptr) (i32.const 105)) ;; i
        (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
        (i32.store8 (local.get $ptr) (i32.const 98))   ;; b
        (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
        ;; compute fib(n)
        (local.set $result (i64.const 0))
        (if (i32.le_u (local.get $n) (i32.const 1))
          (then (local.set $result (i64.extend_i32_u (local.get $n))))
          (else
            (local $a i64) (local $b i64) (local $i i32)
            (local.set $a (i64.const 0))
            (local.set $b (i64.const 1))
            (local.set $i (i32.const 2))
            (block $fdone (br_table $fdone (local.get $n) (local.get $i))
              (loop $floop
                (local.set $a (local.get $b))
                (local.set $b (i64.add (local.get $a) (local.get $b)))
                (local.set $i (i32.add (local.get $i) (i32.const 1)))
                (br_if $floop (i32.le_u (local.get $i) (local.get $n)))
              )
            )
            (local.set $result (local.get $b))
          )
        )
        (br $switch)
      )

      ;; op 1: sumFib
      (block $case1
        (br_if $case1 (i32.ne (local.get $op) (i32.const 1)))
        (i32.store8 (local.get $ptr) (i32.const 83)) ;; S
        (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
        (i32.store8 (local.get $ptr) (i32.const 117)) ;; u
        (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
        (i32.store8 (local.get $ptr) (i32.const 109)) ;; m
        (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
        (i32.store8 (local.get $ptr) (i32.const 70))  ;; F
        (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
        (i32.store8 (local.get $ptr) (i32.const 105)) ;; i
        (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
        (i32.store8 (local.get $ptr) (i32.const 98))  ;; b
        (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
        (local.set $result (i64.const 0))
        (if (i32.eqz (local.get $n))
          (then (local.set $result (i64.const 0)))
          (else
            (if (i32.eq (local.get $n) (i32.const 1))
              (then (local.set $result (i64.const 1)))
              (else
                (local $a i64) (local $b i64) (local $i i32)
                (local.set $a (i64.const 0))
                (local.set $b (i64.const 1))
                (local.set $result (i64.const 1))
                (local.set $i (i32.const 2))
                (block $sfdone
                  (loop $sfloop
                    (br_if $sfdone (i32.ge_u (local.get $i) (local.get $n)))
                    (local.set $a (local.get $b))
                    (local.set $b (i64.add (local.get $a) (local.get $b)))
                    (local.set $result (i64.add (local.get $result) (local.get $b)))
                    (local.set $i (i32.add (local.get $i) (i32.const 1)))
                    (br $sfloop)
                  )
                )
              )
            )
        ))
        (br $switch)
      )

      ;; op 2: sumSquares
      (block $case2
        (br_if $case2 (i32.ne (local.get $op) (i32.const 2)))
        (i32.store8 (local.get $ptr) (i32.const 117)) ;; u
        (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
        (i32.store8 (local.get $ptr) (i32.const 109)) ;; m
        (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
        (i32.store8 (local.get $ptr) (i32.const 83))  ;; S
        (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
        (i32.store8 (local.get $ptr) (i32.const 113)) ;; q
        (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
        (i32.store8 (local.set $ptr (i32.add (local.get $ptr) (i32.const 1))) (i32.const 0)) ;; skip
        (local.set $result (call $sumSquares (local.get $n)))
        (br $switch)
      )

      ;; op 3: matrixTrace
      (block $case3
        (br_if $case3 (i32.ne (local.get $op) (i32.const 3)))
        (i32.store8 (local.get $ptr) (i32.const 116)) ;; t
        (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
        (i32.store8 (local.get $ptr) (i32.const 114)) ;; r
        (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
        (i32.store8 (local.get $ptr) (i32.const 97))  ;; a
        (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
        (i32.store8 (local.get $ptr) (i32.const 99))  ;; c
        (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
        (i32.store8 (local.get $ptr) (i32.const 101)) ;; e
        (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
        (local.set $result (call $matrixTrace (local.get $n)))
      )
    )

    ;; Close "op":"..."","n":,"result":
    (i32.store8 (local.get $ptr) (i32.const 34)) ;; "
    (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
    (i32.store8 (local.get $ptr) (i32.const 44)) ;; ,
    (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
    (i32.store8 (local.get $ptr) (i32.const 34)) ;; "
    (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
    (i32.store8 (local.get $ptr) (i32.const 110)) ;; n
    (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
    (i32.store8 (local.get $ptr) (i32.const 34)) ;; "
    (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
    (i32.store8 (local.get $ptr) (i32.const 58)) ;; :
    (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
    (local.set $ptr (call $writeI32 (local.get $n) (local.get $ptr)))
    (i32.store8 (local.get $ptr) (i32.const 44)) ;; ,
    (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
    (i32.store8 (local.get $ptr) (i32.const 34)) ;; "
    (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
    (i32.store8 (local.get $ptr) (i32.const 114)) ;; r
    (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
    (i32.store8 (local.get $ptr) (i32.const 101)) ;; e
    (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
    (i32.store8 (local.get $ptr) (i32.const 115)) ;; s
    (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
    (i32.store8 (local.get $ptr) (i32.const 117)) ;; u
    (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
    (i32.store8 (local.get $ptr) (i32.const 108)) ;; l
    (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
    (i32.store8 (local.get $ptr) (i32.const 116)) ;; t
    (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
    (i32.store8 (local.get $ptr) (i32.const 34)) ;; "
    (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
    (i32.store8 (local.get $ptr) (i32.const 58)) ;; :
    (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))

    ;; Write result number
    (local.set $ptr (call (export "writeU64") (local.get $result) (local.get $ptr)))

    ;; Close }
    (i32.store8 (local.get $ptr) (i32.const 125))
    (local.set $ptr (i32.add (local.get $ptr) (i32.const 1)))
    (i32.store8 (local.get $ptr) (i32.const 0)) ;; null terminator

    ;; Return output length (from 2048 to end)
    (i32.sub (local.get $ptr) (i32.const 2048))
  )

  ;; ── Hash string from string table ──────────────────────────────────────
  ;; string table at offset 10000 (well after any output)
  (data (i32.const 10000) "fib\00sumSquares\0matrixTrace\0sumFib\0")
)
