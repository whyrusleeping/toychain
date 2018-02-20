package libp2p

import (
	"context"
	"crypto/rand"

	yamux "gx/ipfs/QmNWCEvi7bPRcvqAV8AKLGVNoQdArWi7NJayka2SM4XtRe/go-smux-yamux"
	bhost "gx/ipfs/QmNh1kGFFdsPu79KNSaL4NUKUPb4Eiz4KHdMtFY6664RDp/go-libp2p/p2p/host/basic"
	host "gx/ipfs/QmNmJZL7FQySMtE2BQuLMuZg2EB2CLEunJJUSVSc9YnnbV/go-libp2p-host"
	swarm "gx/ipfs/QmSwZMWwFZSUpe5muU2xgTUwppH24KfMwdPXiwbEp2c6G5/go-libp2p-swarm"
	msmux "gx/ipfs/QmVniQJkdzLZaZwzwMdd3dJTvWiJ1DQEkreVy6hs6h7Vk5/go-smux-multistream"
	transport "gx/ipfs/QmVxtCwKFMmwcjhQXsGj6m4JAW7nGb9hRoErH9jpgqcLxA/go-libp2p-transport"
	ma "gx/ipfs/QmWWQ2Txc2c6tqjsBpzg5Ar652cHPGNsQQp2SejkNmkUMb/go-multiaddr"
	pstore "gx/ipfs/QmXauCuJzmzapetmC6W4TuDJLL1yFFrVzSHoWv8YdbmnxH/go-libp2p-peerstore"
	mux "gx/ipfs/QmY9JXR3FupnYAYJWK9aMr9bCpqWKcToQ1tz8DVGTrHpHw/go-stream-muxer"
	pnet "gx/ipfs/QmZPrWxuM8GHr4cGKbyF5CCT11sFUP9hgqpeUHALvx2nUr/go-libp2p-interface-pnet"
	peer "gx/ipfs/QmZoWKhxUmZ2seW4BzX6fJkNR8hh9PsGModr7q171yq2SS/go-libp2p-peer"
	crypto "gx/ipfs/QmaPbCnUMBohSGo3KnxEa2bHqyJVVeEEcwtqJAYxerieBo/go-libp2p-crypto"
	mplex "gx/ipfs/Qmc14vuKyGqX27RvBhekYytxSFJpaEgQVuVJgKSm69MEix/go-smux-multiplex"
	metrics "gx/ipfs/QmdeBtQGXjSt7cb97nx9JyLHHv5va2LyEAue7Q5tDFzpLy/go-libp2p-metrics"
)

type Config struct {
	Transports  []transport.Transport
	Muxer       mux.Transport
	ListenAddrs []ma.Multiaddr
	PeerKey     crypto.PrivKey
	Peerstore   pstore.Peerstore
	Protector   pnet.Protector
	Reporter    metrics.Reporter
}

func Construct(ctx context.Context, cfg *Config) (host.Host, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	if cfg.PeerKey == nil {
		priv, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, rand.Reader)
		if err != nil {
			return nil, err
		}
		cfg.PeerKey = priv
	}

	// Obtain Peer ID from public key
	pid, err := peer.IDFromPublicKey(cfg.PeerKey.GetPublic())
	if err != nil {
		return nil, err
	}

	ps := cfg.Peerstore
	if ps == nil {
		ps = pstore.NewPeerstore()
	}

	ps.AddPrivKey(pid, cfg.PeerKey)
	ps.AddPubKey(pid, cfg.PeerKey.GetPublic())

	swrm, err := swarm.NewSwarmWithProtector(ctx, cfg.ListenAddrs, pid, ps, cfg.Protector, cfg.Muxer, cfg.Reporter)
	if err != nil {
		return nil, err
	}

	netw := (*swarm.Network)(swrm)

	return bhost.New(netw), nil
}

func DefaultConfig() *Config {
	// Create a multiaddress
	addr, err := ma.NewMultiaddr("/ip4/127.0.0.1/tcp/0")
	if err != nil {
		panic(err)
	}

	// Set up stream multiplexer
	tpt := msmux.NewBlankTransport()
	tpt.AddTransport("/yamux/1.0.0", yamux.DefaultTransport)
	tpt.AddTransport("/mplex/6.3.0", mplex.DefaultTransport)

	return &Config{
		ListenAddrs: []ma.Multiaddr{addr},
		Peerstore:   pstore.NewPeerstore(),
		Muxer:       tpt,
	}
}
