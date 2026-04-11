package p2p

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	record "github.com/libp2p/go-libp2p-record"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multihash"
)

const (
	NamespaceComputeCPU  = "/openpool/compute/cpu"
	NamespaceComputeGPU  = "/openpool/compute/gpu"
	NamespaceComputeWASM = "/openpool/compute/wasm"
)

type CapabilityNamespace string

var (
	CapCPU  CapabilityNamespace = NamespaceComputeCPU
	CapGPU  CapabilityNamespace = NamespaceComputeGPU
	CapWASM CapabilityNamespace = NamespaceComputeWASM
)

type DHTClient interface {
	Bootstrap(ctx context.Context) error
	Close() error
	GetClosestPeers(ctx context.Context, key string) ([]peer.ID, error)
	Provide(ctx context.Context, key cid.Cid, announce bool) error
	FindProviders(ctx context.Context, key cid.Cid) ([]peer.AddrInfo, error)
}

type openpoolValidator struct{}

func (v openpoolValidator) Validate(key string, value []byte) error {
	ns, _, err := record.SplitKey(key)
	if err != nil {
		return fmt.Errorf("invalid record key: %w", err)
	}
	if ns != "openpool" {
		return fmt.Errorf("unsupported namespace: %s", ns)
	}
	return nil
}

func (v openpoolValidator) Select(key string, values [][]byte) (int, error) {
	return 0, nil
}

func namespaceToCID(ns CapabilityNamespace) (cid.Cid, error) {
	h, err := multihash.Sum([]byte(ns), multihash.SHA2_256, -1)
	if err != nil {
		return cid.Cid{}, fmt.Errorf("hash namespace %s: %w", ns, err)
	}
	return cid.NewCidV1(cid.Raw, h), nil
}

func (n *Node) StartDHT(bootstrapAddrs []string) error {
	ds := datastore.NewMapDatastore()

	var err error
	n.DHTClient, err = dht.New(n.ctx, n.Host,
		dht.Datastore(ds),
		dht.Mode(dht.ModeAutoServer),
		dht.Validator(record.NamespacedValidator{
			"openpool": openpoolValidator{},
		}),
	)
	if err != nil {
		return err
	}

	for _, addr := range bootstrapAddrs {
		if err := n.Connect(n.ctx, addr); err != nil {
			log.Printf("[%s] DHT bootstrap %s: %v", shortID(n.ID()), addr, err)
		} else {
			log.Printf("[%s] DHT bootstrap connected: %s", shortID(n.ID()), extractPeerID(addr))
		}
	}

	go func() {
		ctx, cancel := context.WithTimeout(n.ctx, 30*time.Second)
		defer cancel()
		if err := n.DHTClient.Bootstrap(ctx); err != nil {
			log.Printf("[%s] DHT bootstrap: %v", shortID(n.ID()), err)
		} else {
			log.Printf("[%s] DHT bootstrapped", shortID(n.ID()))
		}
	}()

	return nil
}

func (n *Node) AdvertiseCapabilities(ctx context.Context, caps []CapabilityNamespace) error {
	if n.DHTClient == nil {
		return fmt.Errorf("DHT not started")
	}

	for _, cap := range caps {
		contentID, err := namespaceToCID(cap)
		if err != nil {
			log.Printf("[%s] CID for %s: %v", shortID(n.ID()), cap, err)
			continue
		}

		if err := n.DHTClient.Provide(ctx, contentID, true); err != nil {
			log.Printf("[%s] advertise %s: %v", shortID(n.ID()), cap, err)
			continue
		}
		log.Printf("[%s] advertised capability: %s (cid: %s)", shortID(n.ID()), cap, contentID.String()[:16]+"…")
	}
	return nil
}

func (n *Node) DiscoverWorkers(ctx context.Context, cap CapabilityNamespace, limit int) ([]peer.AddrInfo, error) {
	if n.DHTClient == nil {
		return nil, fmt.Errorf("DHT not started")
	}

	contentID, err := namespaceToCID(cap)
	if err != nil {
		return nil, fmt.Errorf("CID for %s: %w", cap, err)
	}

	providers, err := n.DHTClient.FindProviders(ctx, contentID)
	if err != nil {
		return nil, fmt.Errorf("find providers for %s: %w", cap, err)
	}

	var verified []peer.AddrInfo
	for _, info := range providers {
		if info.ID == n.Host.ID() {
			continue
		}

		// Tag as WAN for scheduler prioritization (lower priority than LAN)
		n.Host.Peerstore().Put(info.ID, "latency_pref", "high")
		n.Host.Peerstore().Put(info.ID, "network_type", "WAN")

		if err := n.verifyPeer(ctx, info.ID); err != nil {
			log.Printf("[%s] verify peer %s: %v — skipping", shortID(n.ID()), shortID(info.ID.String()), err)
			continue
		}

		verified = append(verified, info)
		if limit > 0 && len(verified) >= limit {
			break
		}
	}

	log.Printf("[%s] discovered %d verified workers for %s (of %d raw)", shortID(n.ID()), len(verified), cap, len(providers))
	return verified, nil
}

func (n *Node) verifyPeer(ctx context.Context, pid peer.ID) error {
	env, err := n.Host.Peerstore().Get(pid, "Envelope")
	if err == nil && env != nil {
		return nil
	}

	_, sigErr := n.Host.Peerstore().Get(pid, "SignedRecord")
	if sigErr == nil {
		return nil
	}

	if n.Host.Network().Connectedness(pid) == network.Connected {
		_ = env
		return nil
	}

	if err := n.Host.Connect(ctx, peer.AddrInfo{ID: pid}); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	return nil
}

func (n *Node) FindPeers(ctx context.Context, limit int) ([]peer.ID, error) {
	if n.DHTClient == nil {
		return nil, nil
	}

	peerIDs, err := n.DHTClient.GetClosestPeers(ctx, "/openpool/peers")
	if err != nil {
		log.Printf("[%s] DHT query failed: %v — using peerstore peers", shortID(n.ID()), err)
		return n.getPeerstorePeers(limit), nil
	}

	if limit > 0 && len(peerIDs) > limit {
		peerIDs = peerIDs[:limit]
	}
	return peerIDs, nil
}

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

func (n *Node) CloseDHT() error {
	if n.DHTClient != nil {
		return n.DHTClient.Close()
	}
	return nil
}
