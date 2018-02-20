package types

import (
	"gx/ipfs/QmcZfnkapfECQGcLZaf9B79NRg7cRa9EnZh4LSbkCzwNvY/go-cid"
)

type TransactionPool struct {
	// TODO: an in memory set is probably not the right thing to use here, but whatever
	txs map[string]*Transaction
}

func NewTransactionPool() *TransactionPool {
	return &TransactionPool{
		txs: make(map[string]*Transaction),
	}
}

func (txp *TransactionPool) Add(tx *Transaction) error {
	c, err := tx.Cid()
	if err != nil {
		return err
	}

	txp.txs[c.KeyString()] = tx
	return nil
}

func (txp *TransactionPool) ClearTx(c *cid.Cid) {
	delete(txp.txs, c.KeyString())
}

func (txp *TransactionPool) GetBestTxs( /* parameter to limit selection from pool */ ) []*Transaction {
	out := make([]*Transaction, 0, len(txp.txs))
	for _, tx := range txp.txs {
		out = append(out, tx)
	}
	return out
}
