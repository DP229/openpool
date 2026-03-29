# OpenPool Mobile

React Native mobile companion app for OpenPool distributed computing network.

## Features

- 📊 **Dashboard** - View node status, credits, connected peers
- ▶️ **Run Tasks** - Execute WASM tasks (fib, sumFib, etc.)
- 🏪 **Marketplace** - Browse compute nodes, publish tasks
- 🖥️ **GPU Status** - Check GPU availability

## Setup

```bash
cd mobile
npm install
npx expo start
```

## Build

```bash
# Android
npx expo build:android

# iOS (requires Apple Developer account)
npx expo build:ios
```

## Configuration

Edit `App.js` to change the API base URL:
```javascript
const API_BASE = 'http://your-node-ip:8080';
```

## Requirements

- Node.js 18+
- Expo CLI
- For building: Android Studio (Android) or Xcode (iOS)