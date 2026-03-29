#include <stdio.h>
#include <stdint.h>

// Forward declarations
uint64_t fib(int n);
uint64_t sumFib(int n);
uint64_t sumSquares(int n);
uint64_t matrixTrace(int size);

int main(void) {
    int n = 30;
    uint64_t f = fib(n);
    uint64_t sf = sumFib(n);
    uint64_t ss = sumSquares(n);
    uint64_t mt = matrixTrace(10);
    
    printf("fib(30)=%lu\n", f);
    printf("sumFib(0..29)=%lu\n", sf);
    printf("sumSquares(0..9999)=%lu\n", ss);
    printf("matrixTrace(10x10)=%lu\n", mt);
    printf("status=ok\n");
    return 0;
}
