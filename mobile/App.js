import React, { useState, useEffect } from 'react';
import { StyleSheet, Text, View, TouchableOpacity, TextInput, ScrollView, ActivityIndicator, Alert } from 'react-native';
import { NavigationContainer } from '@react-navigation/native';
import { createBottomTabNavigator } from '@react-navigation/bottom-tabs';
import { StatusBar } from 'expo-status-bar';

const API_BASE = 'http://localhost:8080'; // Change for production

// Dashboard Screen
function DashboardScreen({ nodeID }) {
  const [status, setStatus] = useState(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    fetchStatus();
  }, []);

  const fetchStatus = async () => {
    try {
      const res = await fetch(`${API_BASE}/status`);
      const data = await res.json();
      setStatus(data);
    } catch (e) {
      Alert.alert('Error', 'Cannot connect to node');
    } finally {
      setLoading(false);
    }
  };

  if (loading) return <View style={styles.container}><ActivityIndicator size="large" /></View>;

  return (
    <ScrollView style={styles.container}>
      <Text style={styles.title}>🤖 OpenPool</Text>
      <Text style={styles.subtitle}>Node Dashboard</Text>
      
      <View style={styles.card}>
        <Text style={styles.label}>Node ID</Text>
        <Text style={styles.value}>{status?.node_id?.substring(0, 16)}...</Text>
      </View>
      
      <View style={styles.card}>
        <Text style={styles.label}>Credits</Text>
        <Text style={[styles.value, styles.green]}>{status?.credits ?? 0}</Text>
      </View>
      
      <View style={styles.card}>
        <Text style={styles.label}>Connected Peers</Text>
        <Text style={styles.value}>{status?.connected_peers ?? 0}</Text>
      </View>
      
      <TouchableOpacity style={styles.btn} onPress={fetchStatus}>
        <Text style={styles.btnText}>🔄 Refresh</Text>
      </TouchableOpacity>
    </ScrollView>
  );
}

// Run Task Screen
function RunTaskScreen() {
  const [op, setOp] = useState('fib');
  const [arg, setArg] = useState('30');
  const [result, setResult] = useState(null);
  const [loading, setLoading] = useState(false);

  const runTask = async () => {
    setLoading(true);
    try {
      const res = await fetch(`${API_BASE}/run`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ op, arg: parseInt(arg) })
      });
      const data = await res.json();
      setResult(data);
    } catch (e) {
      Alert.alert('Error', e.message);
    } finally {
      setLoading(false);
    }
  };

  const ops = ['fib', 'sumFib', 'sumSquares', 'matrixTrace'];

  return (
    <ScrollView style={styles.container}>
      <Text style={styles.title}>▶ Run Task</Text>
      
      <Text style={styles.label}>Operation</Text>
      <View style={styles.opts}>
        {ops.map(o => (
          <TouchableOpacity key={o} style={[styles.opt, op === o && styles.optActive]} onPress={() => setOp(o)}>
            <Text style={[styles.optText, op === o && styles.optTextActive]}>{o}</Text>
          </TouchableOpacity>
        ))}
      </View>
      
      <Text style={styles.label}>Argument (n)</Text>
      <TextInput style={styles.input} value={arg} onChangeText={setArg} keyboardType="numeric" />
      
      <TouchableOpacity style={styles.btn} onPress={runTask} disabled={loading}>
        <Text style={styles.btnText}>{loading ? '⏳ Running...' : '▶ Run'}</Text>
      </TouchableOpacity>
      
      {result && (
        <View style={styles.result}>
          <Text style={styles.resultText}>{JSON.stringify(result, null, 2)}</Text>
        </View>
      )}
    </ScrollView>
  );
}

// Marketplace Screen
function MarketScreen() {
  const [nodes, setNodes] = useState([]);
  const [taskId, setTaskId] = useState('');
  const [credits, setCredits] = useState('10');
  const [loading, setLoading] = useState(false);

  const loadNodes = async () => {
    setLoading(true);
    try {
      const res = await fetch(`${API_BASE}/nodes`);
      const data = await res.json();
      setNodes(data.nodes || []);
    } catch (e) {
      Alert.alert('Error', e.message);
    } finally {
      setLoading(false);
    }
  };

  const publishTask = async () => {
    try {
      const res = await fetch(`${API_BASE}/tasks`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          task_id: taskId || 'task-' + Date.now(),
          op: 'fib',
          input: JSON.stringify({ op: 'fib', arg: 30 }),
          credits: parseInt(credits),
          timeout_sec: 30,
          status: 'pending'
        })
      });
      const data = await res.json();
      Alert.alert('Success', `Task published: ${data.task_id}`);
    } catch (e) {
      Alert.alert('Error', e.message);
    }
  };

  return (
    <ScrollView style={styles.container}>
      <Text style={styles.title}>🏪 Marketplace</Text>
      
      <TouchableOpacity style={styles.btn} onPress={loadNodes}>
        <Text style={styles.btnText}>🔄 Load Nodes</Text>
      </TouchableOpacity>
      
      {nodes.length > 0 ? (
        nodes.map((n, i) => (
          <View key={i} style={styles.card}>
            <Text style={styles.value}>{n.node_id?.substring(0, 12)}...</Text>
            <Text style={styles.label}>Price: {n.price_per_task} credits</Text>
          </View>
        ))
      ) : <Text style={styles.label}>No nodes available</Text>}
      
      <Text style={[styles.label, { marginTop: 20 }]}>Publish Task</Text>
      <TextInput style={styles.input} placeholder="Task ID (optional)" value={taskId} onChangeText={setTaskId} />
      <TextInput style={styles.input} placeholder="Credits" value={credits} onChangeText={setCredits} keyboardType="numeric" />
      <TouchableOpacity style={styles.btn} onPress={publishTask}>
        <Text style={styles.btnText}>📤 Publish Task</Text>
      </TouchableOpacity>
    </ScrollView>
  );
}

// GPU Screen
function GPUScreen() {
  const [info, setInfo] = useState(null);
  const [loading, setLoading] = useState(false);

  const checkGPU = async () => {
    setLoading(true);
    try {
      const res = await fetch(`${API_BASE}/gpu`);
      const data = await res.json();
      setInfo(data);
    } catch (e) {
      Alert.alert('Error', e.message);
    } finally {
      setLoading(false);
    }
  };

  return (
    <ScrollView style={styles.container}>
      <Text style={styles.title}>🖥️ GPU</Text>
      
      <TouchableOpacity style={styles.btn} onPress={checkGPU}>
        <Text style={styles.btnText}>🔄 Check GPU</Text>
      </TouchableOpacity>
      
      {info && (
        <View style={styles.card}>
          <Text style={styles.label}>Enabled</Text>
          <Text style={styles.value}>{info.enabled ? '✅ Yes' : '❌ No'}</Text>
          <Text style={styles.label}>Devices</Text>
          {info.devices?.map((d, i) => (
            <Text key={i} style={styles.label}>- {d.name}</Text>
          ))}
        </View>
      )}
    </ScrollView>
  );
}

const Tab = createBottomTabNavigator();

export default function App() {
  return (
    <NavigationContainer>
      <StatusBar style="light" />
      <Tab.Navigator screenOptions={{ headerShown: false, tabBarStyle: styles.tabBar }}>
        <Tab.Screen name="Dashboard" component={DashboardScreen} options={{ tabBarLabel: '🏠' }} />
        <Tab.Screen name="Run" component={RunTaskScreen} options={{ tabBarLabel: '▶' }} />
        <Tab.Screen name="Market" component={MarketScreen} options={{ tabBarLabel: '🏪' }} />
        <Tab.Screen name="GPU" component={GPUScreen} options={{ tabBarLabel: '🖥️' }} />
      </Tab.Navigator>
    </NavigationContainer>
  );
}

const styles = StyleSheet.create({
  container: { flex: 1, backgroundColor: '#0d1117', padding: 16 },
  title: { fontSize: 28, fontWeight: 'bold', color: '#58a6ff', marginBottom: 4 },
  subtitle: { fontSize: 14, color: '#8b949e', marginBottom: 20 },
  card: { backgroundColor: '#161b22', padding: 16, borderRadius: 8, marginBottom: 12, borderWidth: 1, borderColor: '#30363d' },
  label: { fontSize: 12, color: '#8b949e', textTransform: 'uppercase', marginBottom: 4 },
  value: { fontSize: 24, fontWeight: 'bold', color: '#c9d1d9' },
  green: { color: '#3fb950' },
  btn: { backgroundColor: '#238636', padding: 14, borderRadius: 8, alignItems: 'center', marginVertical: 8 },
  btnText: { color: '#fff', fontSize: 16, fontWeight: 'bold' },
  input: { backgroundColor: '#0d1117', borderWidth: 1, borderColor: '#30363d', color: '#c9d1d9', padding: 12, borderRadius: 8, marginBottom: 12, fontSize: 16 },
  opts: { flexDirection: 'row', flexWrap: 'wrap', marginBottom: 12 },
  opt: { backgroundColor: '#21262d', padding: 10, borderRadius: 6, marginRight: 8, marginBottom: 8 },
  optActive: { backgroundColor: '#58a6ff' },
  optText: { color: '#8b949e' },
  optTextActive: { color: '#fff' },
  result: { backgroundColor: '#0d1117', padding: 12, borderRadius: 8, borderWidth: 1, borderColor: '#30363d', marginTop: 12 },
  resultText: { color: '#c9d1d9', fontSize: 12, fontFamily: 'monospace' },
  tabBar: { backgroundColor: '#161b22', borderTopColor: '#30363d' }
});