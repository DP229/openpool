// Package p2p provides libp2p-based P2P networking with NAT traversal and DHT discovery.
package p2p

import (
	"context"
	"log"
	"time"

	"github.com/ipfs/go-datastore"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/peer"
)

// OpenPoolDHTKey is the DHT key used for peer discovery.
const OpenPoolDHTKey = "/openpool/peers"

// StartDHT starts a DHT server on the node (can be queried by other nodes).
// Pass bootstrap multiaddrs to join an existing DHT network.
func (n *Node) StartDHT(bootstrapAddrs []string) error {
	ds := datastore.NewMapDatastore()

	// Create a full DHT server (responds to queries from other nodes)
	var err error
	n.DHTClient, err = dht.New(n.ctx, n.Host,
		dht.Datastore(ds),
		dht.Mode(dht.ModeAutoServer), // becomes server when it has peers
	)
	if err != nil {
		return err
	}

	// Connect to bootstrap peers
	for _, addr := range bootstrapAddrs {
		if err := n.Connect(n.ctx, addr); err != nil {
			log.Printf("[%s] DHT bootstrap %s: %v", n.ID[:6], addr, err)
		} else {
			log.Printf("[%s] DHT bootstrap connected: %s", n.ID[:6], extractPeerID(addr))
		}
	}

	// Bootstrap the DHT routing table in background
	go func() {
		ctx, cancel := context.WithTimeout(n.ctx, 30*time.Second)
		defer cancel()
		if err := n.DHTClient.Bootstrap(ctx); err != nil {
			log.Printf("[%s] DHT bootstrap: %v", n.ID[:6], err)
		} else {
			log.Printf("[%s] DHT bootstrapped", n.ID[:6])
		}
	}()

	return nil
}

// FindPeers queries the DHT for peers near the OpenPool discovery key.
// Returns peer IDs found in the DHT. Falls back to connected peers if DHT has no results.
func (n *Node) FindPeers(ctx context.Context, limit int) ([]peer.ID, error) {
	if n.DHTClient == nil {
		return nil, nil
	}

	peerIDs, err := n.DHTClient.GetClosestPeers(ctx, OpenPoolDHTKey)
	if err != nil {
		// Fall back to peerstore peers (nodes we've directly connected to)
		log.Printf("[%s] DHT query failed: %v — using peerstore peers", n.ID[:6], err)
		return n.getPeerstorePeers(limit), nil
	}

	if limit > 0 && len(peerIDs) > limit {
		peerIDs = peerIDs[:limit]
	}
	return peerIDs, nil
}

// getPeerstorePeers returns peers from our direct connections.
func (n *Node) getPeerstorePeers(limit int) []peer.ID {
	if n.Host == nil {
		return nil
	}
	peers := n.Host.Network().Peers()
	if limit > 0 && len(peers) > limit {
		peers = peers[:limit]
	}
	return peers
}

// CloseDHT shuts down the DHT client.
func (n *Node) CloseDHT() error {
	if n.DHTClient != nil {
		return n.DHTClient.Close()
	}
	return nil
}
