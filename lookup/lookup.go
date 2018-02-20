package lookup

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	logging "gx/ipfs/QmRb5jh8z2E8hMGN2tkvs1yHynUanqnZ3UeKwgN1i9P1F8/go-log"
	floodsub "gx/ipfs/QmSFihvoND3eDaAYRCeLgLPt62yCPgMZs1NSZmKFEtJQQw/go-libp2p-floodsub"
	peer "gx/ipfs/QmZoWKhxUmZ2seW4BzX6fJkNR8hh9PsGModr7q171yq2SS/go-libp2p-peer"

	pubsub "github.com/briantigerchow/pubsub"
	types "github.com/whyrusleeping/toychain/types"
)

var TchLookupTopic = "/tch/lookup/1.0.0"

var log = logging.Logger("lookup")

// LookupEngine implements a service that allows users to look up a peerID
// given a wallet address
type LookupEngine struct {
	lk    sync.Mutex
	cache map[types.Address]peer.ID

	ourAddresses map[types.Address]struct{}
	ourPeerID    peer.ID

	reqPubsub *pubsub.PubSub

	ps *floodsub.PubSub
}

func NewLookupEngine(ps *floodsub.PubSub, self peer.ID) (*LookupEngine, error) {
	sub, err := ps.Subscribe(TchLookupTopic)
	if err != nil {
		return nil, err
	}

	le := &LookupEngine{
		ps:           ps,
		cache:        make(map[types.Address]peer.ID),
		ourPeerID:    self,
		ourAddresses: make(map[types.Address]struct{}),
		reqPubsub:    pubsub.New(128),
	}

	go le.HandleMessages(sub)
	return le, nil
}

type message struct {
	Address types.Address
	Peer    string
	Request bool
}

func (le *LookupEngine) HandleMessages(s *floodsub.Subscription) {
	defer s.Cancel()
	ctx := context.TODO()
	for {
		msg, err := s.Next(ctx)
		if err != nil {
			log.Error("from subscription.Next(): ", err)
			return
		}
		if msg.GetFrom() == le.ourPeerID {
			continue
		}

		var m message
		if err := json.Unmarshal(msg.GetData(), &m); err != nil {
			log.Error("malformed message: ", err)
			continue
		}

		le.lk.Lock()
		if m.Request {
			if _, ok := le.ourAddresses[m.Address]; ok {
				go le.SendMessage(&message{
					Address: m.Address,
					Peer:    le.ourPeerID.Pretty(),
				})
			}
		} else {
			pid, err := peer.IDB58Decode(m.Peer)
			if err != nil {
				log.Error("bad peer ID: ", err)
				continue
			}
			le.cache[m.Address] = pid
			le.reqPubsub.Pub(pid, string(m.Address))
		}
		le.lk.Unlock()
	}
}

func (le *LookupEngine) SendMessage(m *message) {
	d, err := json.Marshal(m)
	if err != nil {
		log.Error("failed to marshal message: ", err)
		return
	}

	if err := le.ps.Publish(TchLookupTopic, d); err != nil {
		log.Error("publish failed: ", err)
	}
}

func (le *LookupEngine) Lookup(a types.Address) (peer.ID, error) {
	le.lk.Lock()
	v, ok := le.cache[a]
	le.lk.Unlock()
	if ok {
		return v, nil
	}

	ch := le.reqPubsub.SubOnce(string(a))

	le.SendMessage(&message{
		Address: a,
		Request: true,
	})

	select {
	case out := <-ch:
		return out.(peer.ID), nil
	case <-time.After(time.Second * 10):
		return "", fmt.Errorf("timed out waiting for response")
	}
}

func (le *LookupEngine) AddAddress(a types.Address) {
	le.lk.Lock()
	le.ourAddresses[a] = struct{}{}
	le.lk.Unlock()
}
