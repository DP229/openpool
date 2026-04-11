import React, { useState, useEffect, useCallback } from 'react';
import { ReactFlow, Background, Controls, MiniMap, Node, Edge, useNodesState, useEdgesState } from '@xyflow/react';
import '@xyflow/react/dist/style.css';

// ── Types ────────────────────────────────────────────────────────────────────────

type ComputeStrategy = 'HybridAuto' | 'LANOnly' | 'WANOnly';

interface Peer {
  id: string;
  status: 'online' | 'offline' | 'busy';
  network: 'LAN' | 'WAN';
  cpuLoad: number;
  gpuVrams: number;
  gpuTotal: number;
}

interface TaskChunk {
  id: string;
  status: 'pending' | 'computing' | 'verifying' | 'completed' | 'failed';
  peerId?: string;
}

interface Telemetry {
  cpuLoad: number;
  gpuUsed: number;
  gpuTotal: number;
  networkUp: number;
  networkDown: number;
}

// ── Components ────────────────────────────────────────────────────────────────

// 1. Compute Strategy Selector
export const StrategySelector: React.FC<{ value: ComputeStrategy; onChange: (s: ComputeStrategy) => void }> = ({ value, onChange }) => (
  <div className="strategy-selector">
    <label className="block text-sm font-medium text-gray-300 mb-2">Compute Strategy</label>
    <div className="flex gap-2">
      {(['HybridAuto', 'LANOnly', 'WANOnly'] as ComputeStrategy[]).map((s) => (
        <button
          key={s}
          onClick={() => onChange(s)}
          className={`px-4 py-2 rounded-lg font-medium transition-all ${
            value === s
              ? s === 'HybridAuto' ? 'bg-purple-600 ring-2 ring-purple-400' :
                s === 'LANOnly' ? 'bg-green-600 ring-2 ring-green-400' :
                'bg-gray-600 ring-2 ring-gray-400'
              : 'bg-gray-800 text-gray-400 hover:bg-gray-700'
          }`}
        >
          {s === 'HybridAuto' ? '🌐 Hybrid Auto' : s === 'LANOnly' ? '🏠 LAN Only' : '🌍 WAN Only'}
        </button>
      ))}
    </div>
  </div>
);

// 2. Network Topography (Fleet View)
export const FleetView: React.FC<{ peers: Peer[]; filter: 'all' | 'LAN' | 'WAN'; onFilterChange: (f: 'all' | 'LAN' | 'WAN') => void }> = ({ peers, filter, onFilterChange }) => (
  <div className="fleet-view bg-gray-900 rounded-xl p-4 border border-gray-700">
    <div className="flex justify-between items-center mb-4">
      <h3 className="text-lg font-bold text-white">Network Fleet</h3>
      <div className="flex gap-2">
        {(['all', 'LAN', 'WAN'] as const).map((f) => (
          <button
            key={f}
            onClick={() => onFilterChange(f)}
            className={`px-3 py-1 rounded-full text-sm ${
              filter === f 
                ? f === 'LAN' ? 'bg-green-600' : f === 'WAN' ? 'bg-gray-500' : 'bg-purple-600'
                : 'bg-gray-800 text-gray-400'
            }`}
          >
            {f === 'all' ? 'All' : f === 'LAN' ? '🏠 LAN' : '🌍 WAN'}
          </button>
        ))}
      </div>
    </div>
    <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-3">
      {peers
        .filter((p) => filter === 'all' || p.network === filter)
        .map((peer) => (
          <div key={peer.id} className="peer-card bg-gray-800 rounded-lg p-3 border border-gray-700">
            <div className="flex justify-between items-start mb-2">
              <span className="font-mono text-xs text-gray-400">{peer.id.slice(0, 8)}...</span>
              <span className={`px-2 py-0.5 rounded-full text-xs font-medium ${
                peer.network === 'LAN' ? 'bg-green-900 text-green-300' : 'bg-gray-700 text-gray-300'
              }`}>
                {peer.network}
              </span>
            </div>
            <div className="flex items-center gap-2">
              <div className={`w-2 h-2 rounded-full ${
                peer.status === 'online' ? 'bg-green-500' : peer.status === 'busy' ? 'bg-yellow-500 animate-pulse' : 'bg-red-500'
              }`} />
              <span className="text-sm text-gray-300 capitalize">{peer.status}</span>
            </div>
            {peer.cpuLoad > 0 && (
              <div className="mt-2">
                <div className="text-xs text-gray-500">CPU: {peer.cpuLoad}%</div>
                <div className="w-full bg-gray-700 rounded-full h-1 mt-1">
                  <div className="bg-blue-500 h-1 rounded-full" style={{ width: `${peer.cpuLoad}%` }} />
                </div>
              </div>
            )}
          </div>
        ))}
    </div>
    {peers.filter((p) => filter === 'all' || p.network === filter).length === 0 && (
      <p className="text-gray-500 text-center py-8">No peers found</p>
    )}
  </div>
);

// 3. DAG Visualizer
export const DAGVisualizer: React.FC<{ chunks: TaskChunk[] }> = ({ chunks }) => {
  const initialNodes: Node[] = [
    { id: 'root', type: 'input', data: { label: 'Job' }, position: { x: 250, y: 0 } },
  ];
  
  const cols = Math.ceil(Math.sqrt(chunks.length));
  chunks.forEach((chunk, i) => {
    const row = Math.floor(i / cols);
    const col = i % cols;
    let color = '#6b7280'; // gray
    let label = 'Pending';
    
    if (chunk.status === 'computing') { color = '#3b82f6'; label = 'Computing'; }
    else if (chunk.status === 'verifying') { color = '#eab308'; label = 'Verifying'; }
    else if (chunk.status === 'completed') { color = '#22c55e'; label = 'Done'; }
    else if (chunk.status === 'failed') { color = '#ef4444'; label = 'Failed'; }
    
    initialNodes.push({
      id: chunk.id,
      data: { label: `${label}\n${chunk.id.slice(0,6)}` },
      position: { x: col * 120 + 50, y: row * 100 + 80 },
      style: { 
        background: color, 
        color: 'white', 
        padding: '8px 12px', 
        borderRadius: '8px',
        border: chunk.status === 'computing' ? '2px solid #60a5fa' : 'none',
        animation: chunk.status === 'computing' ? 'pulse 1.5s infinite' : 'none'
      }
    });
  });

  const initialEdges: Edge[] = chunks.map((_, i) => ({
    id: `e${i}`,
    source: 'root',
    target: chunks[i].id,
    animated: true,
    style: { stroke: '#4b5563' }
  }));

  const [nodes, , onNodesChange] = useNodesState(initialNodes);
  const [edges, , onEdgesChange] = useEdgesState(initialEdges);
  
  // Auto-refresh on chunks change
  useEffect(() => {
    if (chunks.length > 1) {
      const newNodes: Node[] = [
        { id: 'root', type: 'input', data: { label: 'Job' }, position: { x: 250, y: 0 } },
      ];
      const cols = Math.ceil(Math.sqrt(chunks.length));
      chunks.forEach((chunk, i) => {
        const row = Math.floor(i / cols);
        const col = i % cols;
        let bg = '#6b7280';
        if (chunk.status === 'computing') bg = '#3b82f6';
        else if (chunk.status === 'verifying') bg = '#eab308';
        else if (chunk.status === 'completed') bg = '#22c55e';
        else if (chunk.status === 'failed') bg = '#ef4444';
        
        newNodes.push({
          id: chunk.id,
          data: { label: `${chunk.status}\n${chunk.id.slice(0,6)}` },
          position: { x: col * 120 + 50, y: row * 100 + 80 },
          style: { background: bg, color: 'white', padding: '8px', borderRadius: '8px' }
        });
      });
      onNodesChange(newNodes);
    }
  }, [chunks]);

  return (
    <div className="dag-visualizer h-96 bg-gray-900 rounded-xl border border-gray-700 overflow-hidden">
      <div className="p-3 border-b border-gray-700 flex justify-between items-center">
        <h3 className="font-bold text-white">Task DAG</h3>
        <div className="flex gap-4 text-xs">
          <span className="flex items-center gap-1"><span className="w-2 h-2 bg-gray-500 rounded-full"/> Pending</span>
          <span className="flex items-center gap-1"><span className="w-2 h-2 bg-blue-500 rounded-full animate-pulse"/> Computing</span>
          <span className="flex items-center gap-1"><span className="w-2 h-2 bg-yellow-500 rounded-full"/> Verifying</span>
          <span className="flex items-center gap-1"><span className="w-2 h-2 bg-green-500 rounded-full"/> Done</span>
        </div>
      </div>
      <div className="h-72">
        <ReactFlow nodes={nodes} edges={edges} onNodesChange={onNodesChange} onEdgesChange={onEdgesChange}>
          <Background color="#374151" gap={20} />
          <Controls />
          <MiniMap nodeColor="#6b7280" maskColor="rgba(0,0,0,0.8)" />
        </ReactFlow>
      </div>
      <style>{`
        @keyframes pulse { 0%, 100% { box-shadow: 0 0 0 0 rgba(59, 130, 246, 0.4); } 50% { box-shadow: 0 0 0 8px rgba(59, 130, 246, 0); } }
      `}</style>
    </div>
  );
};

// 4. Hardware Telemetry Dashboard
export const TelemetryPanel: React.FC<{ telemetry: Telemetry }> = ({ telemetry }) => (
  <div className="telemetry-panel grid grid-cols-2 lg:grid-cols-4 gap-4">
    {/* CPU Gauge */}
    <div className="gauge-card bg-gray-800 rounded-xl p-4 border border-gray-700">
      <div className="text-sm text-gray-400 mb-2">CPU Load</div>
      <div className="relative w-24 h-24 mx-auto">
        <svg viewBox="0 0 100 100" className="transform -rotate-90">
          <circle cx="50" cy="50" r="40" fill="none" stroke="#374151" strokeWidth="8" />
          <circle 
            cx="50" cy="50" r="40" fill="none" stroke="#3b82f6" strokeWidth="8" 
            strokeDasharray={`${telemetry.cpuLoad * 2.51} 251`} 
            strokeLinecap="round"
          />
        </svg>
        <div className="absolute inset-0 flex items-center justify-center">
          <span className="text-xl font-bold text-white">{telemetry.cpuLoad}%</span>
        </div>
      </div>
    </div>

    {/* GPU VRAM */}
    <div className="gauge-card bg-gray-800 rounded-xl p-4 border border-gray-700">
      <div className="text-sm text-gray-400 mb-2">GPU VRAM</div>
      <div className="relative w-24 h-24 mx-auto">
        <svg viewBox="0 0 100 100" className="transform -rotate-90">
          <circle cx="50" cy="50" r="40" fill="none" stroke="#374151" strokeWidth="8" />
          <circle 
            cx="50" cy="50" r="40" fill="none" stroke="#a855f7" strokeWidth="8" 
            strokeDasharray={`${(telemetry.gpuUsed / telemetry.gpuTotal) * 251} 251`} 
            strokeLinecap="round"
          />
        </svg>
        <div className="absolute inset-0 flex flex-col items-center justify-center">
          <span className="text-lg font-bold text-white">{telemetry.gpuUsed}GB</span>
          <span className="text-xs text-gray-500">/ {telemetry.gpuTotal}GB</span>
        </div>
      </div>
    </div>

    {/* Network Up */}
    <div className="gauge-card bg-gray-800 rounded-xl p-4 border border-gray-700">
      <div className="text-sm text-gray-400 mb-2">Network Up</div>
      <div className="text-2xl font-bold text-green-400">{telemetry.networkUp} MB/s</div>
      <div className="mt-2 h-2 bg-gray-700 rounded-full overflow-hidden">
        <div className="h-full bg-green-500 animate-pulse" style={{ width: '60%' }} />
      </div>
    </div>

    {/* Network Down */}
    <div className="gauge-card bg-gray-800 rounded-xl p-4 border border-gray-700">
      <div className="text-sm text-gray-400 mb-2">Network Down</div>
      <div className="text-2xl font-bold text-blue-400">{telemetry.networkDown} MB/s</div>
      <div className="mt-2 h-2 bg-gray-700 rounded-full overflow-hidden">
        <div className="h-full bg-blue-500 animate-pulse" style={{ width: '75%' }} />
      </div>
    </div>
  </div>
);

// ── Main Dashboard Layout ────────────────────────────────────────────────────

export const OpenPoolDashboard: React.FC = () => {
  const [strategy, setStrategy] = useState<ComputeStrategy>('HybridAuto');
  const [peers, setPeers] = useState<Peer[]>([]);
  const [peerFilter, setPeerFilter] = useState<'all' | 'LAN' | 'WAN'>('all');
  const [chunks, setChunks] = useState<TaskChunk[]>([]);
  const [telemetry, setTelemetry] = useState<Telemetry>({ cpuLoad: 0, gpuUsed: 0, gpuTotal: 16, networkUp: 0, networkDown: 0 });

  // Simulated data updates (replace with WebSocket/API polling in production)
  useEffect(() => {
    const interval = setInterval(() => {
      setTelemetry({
        cpuLoad: Math.floor(Math.random() * 80 + 10),
        gpuUsed: Math.floor(Math.random() * 12 + 2),
        gpuTotal: 16,
        networkUp: Math.floor(Math.random() * 100 + 20),
        networkDown: Math.floor(Math.random() * 200 + 50),
      });
    }, 2000);
    return () => clearInterval(interval);
  }, []);

  return (
    <div className="min-h-screen bg-gray-950 text-white p-6 font-sans">
      <header className="mb-8">
        <h1 className="text-3xl font-bold bg-gradient-to-r from-purple-400 to-blue-400 bg-clip-text text-transparent">
          OpenPool HPC Control Center
        </h1>
        <p className="text-gray-400 mt-2">Distributed Compute Orchestration</p>
      </header>

      {/* Hardware Telemetry */}
      <section className="mb-8">
        <h2 className="text-xl font-semibold mb-4">Hardware Telemetry</h2>
        <TelemetryPanel telemetry={telemetry} />
      </section>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-8">
        {/* Task Submission */}
        <section className="bg-gray-900 rounded-xl p-6 border border-gray-700">
          <h2 className="text-xl font-semibold mb-4">Submit Job</h2>
          <StrategySelector value={strategy} onChange={setStrategy} />
          <div className="mt-4 flex gap-3">
            <button 
              className="flex-1 bg-purple-600 hover:bg-purple-700 text-white font-bold py-3 px-6 rounded-lg transition-colors"
              onClick={() => {
                const newChunks = Array.from({ length: 8 }, (_, i) => ({
                  id: `chunk-${Date.now()}-${i}`,
                  status: 'pending' as const
                }));
                setChunks(newChunks);
              }}
            >
              Launch Job
            </button>
          </div>
        </section>

        {/* Network Fleet */}
        <section>
          <FleetView 
            peers={[
              { id: 'Qmaaa...', status: 'online', network: 'LAN', cpuLoad: 45, gpuVrams: 8, gpuTotal: 16 },
              { id: 'Qmbbb...', status: 'busy', network: 'LAN', cpuLoad: 90, gpuVrams: 14, gpuTotal: 16 },
              { id: 'Qmccc...', status: 'online', network: 'WAN', cpuLoad: 20, gpuVrams: 0, gpuTotal: 0 },
              { id: 'Qmddd...', status: 'offline', network: 'WAN', cpuLoad: 0, gpuVrams: 0, gpuTotal: 0 },
              { id: 'Qmeee...', status: 'online', network: 'LAN', cpuLoad: 30, gpuVrams: 4, gpuTotal: 16 },
            ]} 
            filter={peerFilter} 
            onFilterChange={setPeerFilter} 
          />
        </section>
      </div>

      {/* DAG Visualizer */}
      <section className="mt-8">
        <DAGVisualizer chunks={chunks.length > 0 ? chunks : [
          { id: 'c1', status: 'completed' },
          { id: 'c2', status: 'computing' },
          { id: 'c3', status: 'pending' },
          { id: 'c4', status: 'verifying' },
          { id: 'c5', status: 'completed' },
        ]} />
      </section>
    </div>
  );
};

export default OpenPoolDashboard;