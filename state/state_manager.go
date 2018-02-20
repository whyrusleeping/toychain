package state

import (
	"context"
	"fmt"
	"time"

	"gx/ipfs/QmZoWKhxUmZ2seW4BzX6fJkNR8hh9PsGModr7q171yq2SS/go-libp2p-peer"
	"gx/ipfs/QmcZfnkapfECQGcLZaf9B79NRg7cRa9EnZh4LSbkCzwNvY/go-cid"

	pubsub "github.com/briantigerchow/pubsub"
	logging "gx/ipfs/QmRb5jh8z2E8hMGN2tkvs1yHynUanqnZ3UeKwgN1i9P1F8/go-log"
	hamt "gx/ipfs/QmdBXcN47jVwKLwSyN9e9xYVZ7WcAWgQ5N4cmNw7nzWq2q/go-hamt-ipld"
	ipld "gx/ipfs/Qme5bWv7wtjUNGsK2BNGVUFPKiuxWrsqrtvYwCLRw8YFES/go-ipld-format"

	contract "github.com/whyrusleeping/toychain/contract"
	types "github.com/whyrusleeping/toychain/types"
)

var log = logging.Logger("state")

// StateManager manages the current state of the chain and handles validating
// and applying updates.
type StateManager struct {
	BestBlock *types.Block
	HeadCid   *cid.Cid

	StateRoot *contract.State

	TxPool *types.TransactionPool

	KnownGoodBlocks *cid.Set

	cs  *hamt.CborIpldStore
	dag ipld.DAGService

	Miner *Miner

	blkNotif *pubsub.PubSub
}

func NewStateManager(cs *hamt.CborIpldStore, dag ipld.DAGService) *StateManager {
	return &StateManager{
		KnownGoodBlocks: cid.NewSet(),
		TxPool:          types.NewTransactionPool(),
		cs:              cs,
		dag:             dag,
		blkNotif:        pubsub.New(128),
	}
}

func (s *StateManager) SetBestBlock(b *types.Block) {
	s.BestBlock = b
}

// Inform informs the state manager that we received a new block from the given
// peer
func (s *StateManager) Inform(p peer.ID, blk *types.Block) {
	if err := s.ProcessNewBlock(context.Background(), blk); err != nil {
		log.Error(err)
		return
	}
	s.Miner.newBlocks <- blk
}

func (s *StateManager) GetStateRoot() *contract.State {
	// TODO: maybe return immutable copy or something? Don't necessarily want
	// the caller to be able to mutate this without them intending to
	return s.StateRoot
}

func (s *StateManager) ProcessNewBlock(ctx context.Context, blk *types.Block) error {
	if err := s.validateBlock(ctx, blk); err != nil {
		return fmt.Errorf("validate block failed: %s", err)
	}

	if blk.Score() > s.BestBlock.Score() {
		return s.acceptNewBlock(blk)
	}

	return fmt.Errorf("new block not better than current block (%d <= %d)",
		blk.Score(), s.BestBlock.Score())
}

// acceptNewBlock sets the given block as our current 'best chain' block
func (s *StateManager) acceptNewBlock(blk *types.Block) error {
	if err := s.dag.Add(context.TODO(), blk.ToNode()); err != nil {
		return fmt.Errorf("failed to put block to disk: %s", err)
	}

	// update our accounting of the 'best block'
	s.KnownGoodBlocks.Add(blk.Cid())
	s.BestBlock = blk
	s.HeadCid = blk.Cid()

	// Remove any transactions that were mined in the new block from the mempool
	// TODO: actually go through transactions for each block back to the last
	// common block and remove transactions/re-add transactions in blocks we
	// had but arent in the new chain
	for _, tx := range blk.Txs {
		c, err := tx.Cid()
		if err != nil {
			return err
		}

		s.TxPool.ClearTx(c)
	}

	st, err := contract.LoadState(context.Background(), s.cs, blk.StateRoot)
	if err != nil {
		return fmt.Errorf("failed to get newly approved state: %s", err)
	}
	s.StateRoot = st

	s.blkNotif.Pub(blk, "blocks")

	fmt.Printf("accepted new block, [s=%d, h=%s, st=%s]\n\n", blk.Score(), blk.Cid(), blk.StateRoot)
	return nil
}

func (s *StateManager) fetchBlock(ctx context.Context, c *cid.Cid) (*types.Block, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second*10)
	defer cancel()

	var blk types.Block
	if err := s.cs.Get(ctx, c, &blk); err != nil {
		return nil, err
	}

	return &blk, nil
}

// checkSingleBlock verifies that this block, on its own, is structurally and
// cryptographically valid. This means checking that all of its fields are
// properly filled out and its signature is correct. Checking the validity of
// state changes must be done separately and only once the state of the
// previous block has been validated.
func (s *StateManager) checkBlockValid(ctx context.Context, b *types.Block) error {
	return nil
}

func (s *StateManager) checkBlockStateChangeValid(ctx context.Context, st *contract.State, b *types.Block) error {
	if err := st.ApplyTransactions(ctx, b.Txs); err != nil {
		return err
	}

	c, err := st.Flush(ctx)
	if err != nil {
		return err
	}

	if !c.Equals(b.StateRoot) {
		return fmt.Errorf("state root failed to validate! (%s != %s)", c, b.StateRoot)
	}

	return nil
}

// TODO: this method really needs to be thought through carefully. Probably one
// of the most complicated bits of the system
func (s *StateManager) validateBlock(ctx context.Context, b *types.Block) error {
	if err := s.checkBlockValid(ctx, b); err != nil {
		return fmt.Errorf("check block valid failed: %s", err)
	}

	if b.Score() <= s.BestBlock.Score() {
		// TODO: likely should still validate this chain and keep it around.
		// Someone else could mine on top of it
		return fmt.Errorf("new block is not better than our current block")
	}

	var validating []*types.Block
	baseBlk := b
	for !s.KnownGoodBlocks.Has(baseBlk.Cid()) { // probably should be some sort of limit here
		validating = append(validating, baseBlk)

		next, err := s.fetchBlock(ctx, baseBlk.Parent)
		if err != nil {
			return fmt.Errorf("fetch block failed: %s", err)
		}

		if err := s.checkBlockValid(ctx, next); err != nil {
			return err
		}

		baseBlk = next
	}

	st, err := contract.LoadState(ctx, s.cs, baseBlk.StateRoot)
	if err != nil {
		return fmt.Errorf("load state failed: %s", err)
	}

	for i := len(validating) - 1; i >= 0; i-- {
		if err := s.checkBlockStateChangeValid(ctx, st, validating[i]); err != nil {
			return err
		}
		s.KnownGoodBlocks.Add(validating[i].Cid())
	}

	return nil
}

func (st *StateManager) InformTx(tx *types.Transaction) {
	// TODO: validate tx itself
	act, err := st.StateRoot.GetActor(context.TODO(), tx.From)
	if err != nil {
		log.Warningf("failed to get actor for transaction: %s %s", tx.From, err)
		return
	}

	if act.Nonce != tx.Nonce {
		log.Warningf("dropping tx with invalid nonce")
		return
	}

	if err := st.TxPool.Add(tx); err != nil {
		log.Error("failed to add transaction to txpool", err)
		return
	}
}

func (st *StateManager) BlockNotifications(ctx context.Context) <-chan *types.Block {
	blks := make(chan interface{})
	out := make(chan *types.Block, 16)
	go func() {
		defer func() {
			st.blkNotif.Unsub(blks, "blocks")
			close(out)
		}()
		for {
			select {
			case obj := <-blks:
				select {
				case out <- obj.(*types.Block):
				case <-ctx.Done():
				}
			case <-ctx.Done():
				return
			}
		}
	}()
	st.blkNotif.AddSub(blks, "blocks")
	return out
}
