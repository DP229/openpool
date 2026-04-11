// ui/wasm-worker.js
// Zero-Install Browser Swarm: WASM Execution Worker
// Offloads heavy compute to Web Worker to prevent UI freezing

let wasmInstance = null;
let wasmExports = null;

// ── WASM Module Registry ────────────────────────────────────────────────────

const moduleCache = new Map();

async function loadModule(name, buffer) {
    if (moduleCache.has(name)) {
        return moduleCache.get(name);
    }
    
    const module = await WebAssembly.instantiate(buffer, {
        env: {
            // Provide minimal WASI-like imports if needed
            memory: new WebAssembly.Memory({ initial: 256, maximum: 256 }),
        }
    });
    
    moduleCache.set(name, module);
    return module;
}

// ── Message Handler ─────────────────────────────────────────────────────────

self.onmessage = async function(event) {
    const { id, type, payload } = event.data;
    
    try {
        switch (type) {
            case 'EXECUTE':
                const { moduleName, moduleBuffer, functionName, inputData } = payload;
                
                // Load or reuse cached WASM module
                const module = await loadModule(moduleName, moduleBuffer);
                const instance = module.instance;
                const exports = instance.exports;
                
                // Find the exported compute function
                if (typeof exports[functionName] !== 'function') {
                    throw new Error(`WASM function "${functionName}" not found`);
                }
                
                // Execute the WASM function
                // Pass input as JSON string or raw TypedArray
                let result;
                if (inputData && typeof inputData === 'object') {
                    const inputStr = JSON.stringify(inputData);
                    const inputBytes = new TextEncoder().encode(inputStr);
                    const inputPtr = exports.alloc(inputBytes.length);
                    new Uint8Array(exports.memory.buffer).set(inputBytes, inputPtr);
                    
                    result = exports[functionName](inputPtr, inputBytes.length);
                } else {
                    result = exports[functionName]();
                }
                
                // Serialize result
                let resultData;
                if (typeof result === 'number') {
                    resultData = result;
                } else if (result instanceof WebAssembly.Memory) {
                    // Read from memory if returned pointer
                    const memory = new Uint8Array(result.buffer);
                    // Copy to extract result
                    resultData = Array.from(memory.slice(0, 1024)); // Limit extract
                } else {
                    resultData = result;
                }
                
                self.postMessage({
                    id,
                    type: 'RESULT',
                    payload: { result: resultData }
                });
                break;
                
            case 'LOAD_MODULE':
                // Pre-load module into cache
                const { name, buffer } = payload;
                await loadModule(name, buffer);
                self.postMessage({
                    id,
                    type: 'MODULE_LOADED',
                    payload: { name }
                });
                break;
                
            default:
                throw new Error(`Unknown message type: ${type}`);
        }
    } catch (error) {
        self.postMessage({
            id,
            type: 'ERROR',
            payload: { error: error.message }
        });
    }
};

// Signal readiness
self.postMessage({ type: 'READY' });