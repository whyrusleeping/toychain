package core

import (
	"context"
	"encoding/json"

	ma "gx/ipfs/QmWWQ2Txc2c6tqjsBpzg5Ar652cHPGNsQQp2SejkNmkUMb/go-multiaddr"
	"gx/ipfs/QmXfkENeeBvh3zYA51MaSdGUdBjhQ99cP5WQe8zgr6wchG/go-libp2p-net"
	"gx/ipfs/QmZoWKhxUmZ2seW4BzX6fJkNR8hh9PsGModr7q171yq2SS/go-libp2p-peer"
	"gx/ipfs/QmcZfnkapfECQGcLZaf9B79NRg7cRa9EnZh4LSbkCzwNvY/go-cid"

	types "github.com/whyrusleeping/toychain/types"
)

var TxsTopic = "/tch/tx"
var BlocksTopic = "/tch/blks"

const HelloProtocol = "/toychain/hello/0.0.1"

type HelloMessage struct {
	// Could just put the full block header here
	Head        *cid.Cid
	BlockHeight uint64

	// Maybe add some other info to gossip
}

func (tch *ToychainNode) HelloPeer(p peer.ID) {
	ctx := context.Background() // TODO: add appropriate timeout
	s, err := tch.Host.NewStream(ctx, p, HelloProtocol)
	if err != nil {
		log.Error("failed to open stream to new peer for hello: ", err)
		return
	}
	defer s.Close()

	hello := &HelloMessage{
		Head:        tch.StateMgr.HeadCid,
		BlockHeight: tch.StateMgr.BestBlock.Score(),
	}

	if err := json.NewEncoder(s).Encode(hello); err != nil {
		log.Error("marshaling hello message to new peer: ", err)
		return
	}
}

func (tch *ToychainNode) handleHelloStream(s net.Stream) {
	var hello HelloMessage
	if err := json.NewDecoder(s).Decode(&hello); err != nil {
		log.Error("decoding hello message: ", err)
		return
	}

	if hello.Head == nil {
		return
	}

	if hello.BlockHeight <= tch.StateMgr.BestBlock.Score() {
		return
	}

	var blk types.Block
	if err := tch.cs.Get(context.Background(), hello.Head, &blk); err != nil {
		log.Error("getting block from hello message: ", err)
		return
	}
	tch.StateMgr.Inform(s.Conn().RemotePeer(), &blk)
}

type tchNotifiee ToychainNode

var _ net.Notifiee = (*tchNotifiee)(nil)

func (n *tchNotifiee) ClosedStream(_ net.Network, s net.Stream) {}
func (n *tchNotifiee) OpenedStream(_ net.Network, s net.Stream) {}
func (n *tchNotifiee) Connected(_ net.Network, c net.Conn) {
	go (*ToychainNode)(n).HelloPeer(c.RemotePeer())
}

func (n *tchNotifiee) Disconnected(_ net.Network, c net.Conn)    {}
func (n *tchNotifiee) Listen(_ net.Network, a ma.Multiaddr)      {}
func (n *tchNotifiee) ListenClose(_ net.Network, a ma.Multiaddr) {}
