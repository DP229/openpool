// ui/browser-node.js
// Zero-Install Browser Swarm: libp2p Client
// Connects to native Go backend via WebSocket, forwards tasks to WASM worker

import { createLibp2p } from 'https://cdn.jsdelivr.net/npm/libp2p@1.6.1/+esm';
import { websocket } from 'https://cdn.jsdelivr.net/npm/libp2p@1.6.1/+esm/modules/transport/websocket.js';
import { mplex } from 'https://cdn.jsdelivr.net/npm/libp2p@1.6.1/+esm/modules/mplex.js';
import { gossipsub } from 'https://cdn.jsdelivr.net/npm/libp2p@1.6.1/+esm/modules/pubsub/gossipsub.js';

const PROTOCOL_ID = '/openpool/1.0';
const NATIVE_NODE_ADDR = '/ip4/127.0.0.1/tcp/9000/ws'; // Configurable

class BrowserNode {
    constructor(options = {}) {
        this.libp2p = null;
        this.worker = options.worker || null;
        this.nativeMultiaddr = options.nativeMultiaddr || NATIVE_NODE_ADDR;
        this.onTaskResult = options.onTaskResult || (() => {});
        this.debug = options.debug || false;
    }
    
    log(...args) {
        if (this.debug) {
            console.log('[BrowserNode]', ...args);
        }
    }
    
    // ── Worker Management ────────────────────────────────────────────────────
    
    async startWorker() {
        if (!this.worker) {
            // Create inline worker if path not provided
            this.worker = new Worker(
                new URL('./wasm-worker.js', import.meta.url),
                { type: 'module' }
            );
        }
        
        return new Promise((resolve, reject) => {
            const handler = (event) => {
                if (event.data.type === 'READY') {
                    this.worker.removeEventListener('message', handler);
                    this.log('WASM worker ready');
                    resolve();
                }
            };
            this.worker.addEventListener('message', handler);
            this.worker.addEventListener('error', reject);
        });
    }
    
    // ── Libp2p Node ────────────────────────────────────────────────────────
    
    async start() {
        this.log('Starting browser node...');
        
        // Create WASM worker first
        await this.startWorker();
        
        // Create libp2p node with WebSocket transport
        this.libp2p = await createLibp2p({
            transports: [websocket()],
            streamMuxers: [mplex()],
            // No DHT in browser (too heavy), rely on bootstrap from native node
            peerDiscovery: [],
            connectionManager: {
                maxConnections: 50,
                maxParallelDials: 5
            }
        });
        
        // Set protocol handler
        this.libp2p.handle(PROTOCOL_ID, this.handleStream.bind(this));
        
        // Dial native node
        try {
            await this.libp2p.dial(this.nativeMultiaddr);
            this.log(`Connected to native node: ${this.nativeMultiaddr}`);
        } catch (err) {
            this.log(`Failed to dial native node: ${err.message}`);
            // Retry connection after delay
            setTimeout(() => this.start(), 5000);
            return;
        }
        
        // Announce presence
        await this.announceCapabilities();
        
        this.log('Browser node online');
    }
    
    async stop() {
        if (this.libp2p) {
            await this.libp2p.stop();
        }
        if (this.worker) {
            this.worker.terminate();
        }
    }
    
    // ── Stream Handling ────────────────────────────────────────────────────
    
    async handleStream(stream) {
        this.log('Incoming task stream');
        
        try {
            // Read task from native node
            const data = await this.readStream(stream);
            const task = JSON.parse(data);
            
            this.log('Received task:', task.id, task.type);
            
            // Execute task in WASM worker
            const result = await this.executeWasmTask(task);
            
            // Send result back
            await this.writeStream(stream, JSON.stringify({
                type: 'task_resp',
                id: task.id,
                result: result,
                client: 'browser'
            }));
            
            this.onTaskResult(task.id, result);
            
        } catch (err) {
            this.log('Task error:', err.message);
            await this.writeStream(stream, JSON.stringify({
                type: 'error',
                error: err.message
            }));
        }
    }
    
    async readStream(stream) {
        const chunks = [];
        for await (const chunk of stream.source) {
            chunks.push(new TextDecoder().decode(chunk));
        }
        return chunks.join('');
    }
    
    async writeStream(stream, data) {
        const encoder = new TextEncoder();
        await stream.sink([encoder.encode(data)]);
    }
    
    // ── WASM Execution ──────────────────────────────────────────────────────
    
    executeWasmTask(task) {
        return new Promise((resolve, reject) => {
            const id = `task-${Date.now()}`;
            
            const handler = (event) => {
                if (event.data.id !== id) return;
                
                this.worker.removeEventListener('message', handler);
                
                if (event.data.type === 'RESULT') {
                    resolve(event.data.payload.result);
                } else if (event.data.type === 'ERROR') {
                    reject(new Error(event.data.payload.error));
                }
            };
            
            this.worker.addEventListener('message', handler);
            
            // Send task to worker
            this.worker.postMessage({
                id,
                type: 'EXECUTE',
                payload: {
                    moduleName: task.module || 'compute',
                    moduleBuffer: task.wasmBuffer, // ArrayBuffer from fetch
                    functionName: task.function || 'run',
                    inputData: task.input
                }
            });
        });
    }
    
    // ── Capability Advertising ─────────────────────────────────────────────
    
    async announceCapabilities() {
        // Publish our capabilities to native node via existing stream
        // For now, this is implied by connection - native node can check tags
        this.libp2p.peerStore.tag(this.libp2p.peerId, 'client', 5);
        await this.libp2p.peerStore.put(this.libp2p.peerId, 'client_type', 'browser');
        await this.libp2p.peerStore.put(this.libp2p.peerId, 'capabilities', ['wasm']);
    }
}

// ── Main Entry Point ────────────────────────────────────────────────────────

window.BrowserNode = BrowserNode;

// Auto-start if running in main context
if (typeof window !== 'undefined' && !window.__BROWSER_NODE_SKIP_AUTOSTART) {
    const node = new BrowserNode({
        nativeMultiaddr: window.__NATIVE_NODE_ADDR || '/ip4/127.0.0.1/tcp/9000/ws',
        debug: true
    });
    
    node.start().catch(err => {
        console.error('Browser node failed to start:', err);
    });
}