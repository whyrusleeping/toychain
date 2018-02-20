package state

import (
	"context"
	"fmt"
	"math/big"
	"math/rand"
	"time"

	contract "github.com/whyrusleeping/toychain/contract"
	types "github.com/whyrusleeping/toychain/types"

	hamt "gx/ipfs/QmdBXcN47jVwKLwSyN9e9xYVZ7WcAWgQ5N4cmNw7nzWq2q/go-hamt-ipld"
)

var rr = rand.New(rand.NewSource(time.Now().UnixNano()))

type Miner struct {
	// newBlocks is a channel that listens for new blocks from other peers in
	// the network
	newBlocks chan *types.Block

	// blockCallback is a function for the miner to call when it has mined a
	// new block
	blockCallback func(*types.Block) error

	// currentBlock is the block that the miner will be mining on top of
	currentBlock *types.Block

	// address is the address that the miner will mine rewards to
	address types.Address

	// transaction pool to pull transactions for the next block from
	txPool *types.TransactionPool

	StateMgr *StateManager

	cs *hamt.CborIpldStore
}

func NewMiner(bcb func(*types.Block) error, txp *types.TransactionPool, bestBlock *types.Block, coinbase types.Address, cs *hamt.CborIpldStore) *Miner {
	return &Miner{
		newBlocks:     make(chan *types.Block),
		blockCallback: bcb,
		currentBlock:  bestBlock,
		address:       coinbase,
		txPool:        txp,
		cs:            cs,
	}
}

// predictFuture reads an oracle that tells us how long we must wait to mine
// the next block
func predictFuture() time.Duration {
	return time.Millisecond * time.Duration(2000+rand.Intn(100))
	v := time.Hour
	for v > time.Second*10 || v < 0 {
		v = time.Duration((rr.NormFloat64()*3000)+4000) * time.Millisecond
	}
	return v
}

var MiningReward = big.NewInt(1000000)

func (m *Miner) Run(ctx context.Context) {
	blockFound := time.NewTimer(predictFuture())

	begin := time.Now()
	for n := time.Duration(1); true; n++ {
		start := time.Now()
		select {
		case <-ctx.Done():
			log.Error("mining canceled: ", ctx.Err())
			return
		case b := <-m.newBlocks:
			m.currentBlock = b
			fmt.Printf("got a new block in %s [av: %s]\n", time.Since(start), time.Since(begin)/n)
		case <-blockFound.C:
			nb, err := m.getNextBlock(ctx)
			if err != nil {
				log.Error("failed to build block on top of: ", m.currentBlock.Cid())
				log.Error(err)
				break
			}

			fmt.Printf("==> mined a new block [score %d, %s] in %s [av: %s]\n", nb.Score(), nb.Cid(), time.Since(start), time.Since(begin)/n)

			if err := m.blockCallback(nb); err != nil {
				log.Error("mined new block, but failed to push it out: ", err)
				break
			}
			m.currentBlock = nb
		}
		blockFound.Reset(predictFuture())
	}
}

func (m *Miner) getNextBlock(ctx context.Context) (*types.Block, error) {
	nonce, err := m.StateMgr.GetStateRoot().NonceForActor(ctx, contract.ToychainContractAddr)
	if err != nil {
		return nil, err
	}

	reward := &types.Transaction{
		From:   contract.ToychainContractAddr,
		To:     contract.ToychainContractAddr,
		Nonce:  nonce,
		Method: "transfer",
		Params: []interface{}{m.address, MiningReward},
	}

	txs := m.txPool.GetBestTxs()
	txs = append([]*types.Transaction{reward}, txs...)
	nb := &types.Block{
		Height: m.currentBlock.Height + 1,
		Parent: m.currentBlock.Cid(),
		Txs:    txs,
	}

	s, err := contract.LoadState(ctx, m.cs, m.currentBlock.StateRoot)
	if err != nil {
		return nil, err
	}

	for _, tx := range nb.Txs {
		receipt, err := s.ActorExec(ctx, tx)
		if err != nil {
			return nil, fmt.Errorf("applying state from newly mined block: %s", err)
		}

		nb.Receipts = append(nb.Receipts, receipt)
	}

	stateCid, err := s.Flush(ctx)
	if err != nil {
		return nil, fmt.Errorf("flushing state changes: %s", err)
	}

	nb.StateRoot = stateCid
	return nb, nil
}
