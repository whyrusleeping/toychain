package types

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"

	atlas "gx/ipfs/QmSaDQWMxJBMtzQWnGoDppbwSEbHv4aJcD86CMSdszPU4L/refmt/obj/atlas"
	cbor "gx/ipfs/QmZpue627xQuNGXn7xHieSjSZ8N4jot6oBHwe9XTn3e4NU/go-ipld-cbor"
	mh "gx/ipfs/QmZyZDi491cCNTLfAhwcaDii2Kg4pwKRkhqQzURGDvY6ua/go-multihash"
	"gx/ipfs/QmcZfnkapfECQGcLZaf9B79NRg7cRa9EnZh4LSbkCzwNvY/go-cid"
	node "gx/ipfs/Qme5bWv7wtjUNGsK2BNGVUFPKiuxWrsqrtvYwCLRw8YFES/go-ipld-format"
)

func init() {
	cbor.RegisterCborType(Block{})
	cbor.RegisterCborType(Signature{})
	cbor.RegisterCborType(transactionCborEntry)
	cbor.RegisterCborType(Receipt{})
}

type Block struct {
	Parent *cid.Cid

	// could use a hamt for these, really only need a set. a merkle patricia
	// trie (or radix trie, to be technically correct, thanks @wanderer) is
	// inefficient due to all the branch nodes
	// the important thing here is minizing the number of intermediate nodes
	// the simplest thing might just be to use a sorted list of cids for now.
	// A simple array can fit over 5000 cids in a single 256k block.
	//Txs *cid.Cid
	Txs []*Transaction

	// structure should probably mirror the transactions
	Receipts []*Receipt

	// Height is the chain height of this block
	Height uint64

	// StateRoot should be a HAMT, its a very efficient KV store
	StateRoot *cid.Cid

	TickLimit *big.Int
	TicksUsed *big.Int
}

type Address string

func (a Address) String() string {
	return "0x" + hex.EncodeToString([]byte(a))
}

func (a Address) MarshalJSON() ([]byte, error) {
	return json.Marshal(a.String())
}

func (a *Address) UnmarshalJSON(b []byte) error {
	b = b[3 : len(b)-1]
	outbuf := make([]byte, len(b)/2)
	_, err := hex.Decode(outbuf, b)
	if err != nil {
		return err
	}

	*a = Address(outbuf)
	return nil
}

func ParseAddress(s string) (Address, error) {
	if s[:2] == "0x" {
		s = s[2:]
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return "", err
	}

	return Address(b), nil
}

// Score returns a score for this block. This is used to choose the best chain.
func (b *Block) Score() uint64 {
	return b.Height
}

func (b *Block) Cid() *cid.Cid {
	return b.ToNode().Cid()
}

func (b *Block) ToNode() node.Node {
	obj, err := cbor.WrapObject(b, mh.SHA2_256, -1)
	if err != nil {
		panic(err)
	}

	return obj
}

func DecodeBlock(b []byte) (*Block, error) {
	var out Block
	if err := cbor.DecodeInto(b, &out); err != nil {
		return nil, err
	}

	return &out, nil
}

// Signature over a transaction, like how ethereum does it
// TODO: think about how the signature could be an object that wraps the base
// transaction. This might make content addressing a little bit simpler
type Signature struct {
	V *big.Int
	R *big.Int
	S *big.Int
}

// Transaction is the equivalent of an ethereum transaction. But since ethereum
// transactions arent really transactions, theyre more just sending information
// from A to B, I (along with wanderer) want to call it a message. At the same
// time, we want to rename gas to ticks, since what the hell is gas anyways?
// TODO: ensure that transactions are not malleable *at all*, this is very important
type Transaction struct {
	To   Address // address of contract to invoke
	From Address

	Nonce uint64

	TickCost *big.Int
	Ticks    *big.Int
	Method   string
	Params   []interface{}

	Signature *Signature
}

var transactionCborEntry = atlas.BuildEntry(Transaction{}).Transform().
	TransformMarshal(atlas.MakeMarshalTransformFunc(
		func(tx Transaction) ([]interface{}, error) {
			return []interface{}{
				tx.To,
				tx.From,
				tx.Nonce,
				tx.TickCost,
				tx.Ticks,
				tx.Method,
				tx.Params,
				// TODO: signature
			}, nil
		})).
	TransformUnmarshal(atlas.MakeUnmarshalTransformFunc(
		func(x []interface{}) (Transaction, error) {
			if len(x) != 7 {
				return Transaction{}, fmt.Errorf("expected six fields in a transaction")
			}

			var tickcost, ticks *big.Int
			if b, ok := x[3].([]byte); ok {
				tickcost = big.NewInt(0).SetBytes(b)
			}
			if b, ok := x[4].([]byte); ok {
				ticks = big.NewInt(0).SetBytes(b)
			}

			return Transaction{
				To:       Address(x[0].(string)),
				From:     Address(x[1].(string)),
				Nonce:    x[2].(uint64),
				TickCost: tickcost,
				Ticks:    ticks,
				Method:   x[5].(string),
				Params:   x[6].([]interface{}),
			}, nil
		})).
	Complete()

func (tx *Transaction) Unmarshal(b []byte) error {
	return cbor.DecodeInto(b, tx)
}

func (tx *Transaction) Marshal() ([]byte, error) {
	return cbor.DumpObject(tx)
}

// TODO: we could control creation of transaction instances to guarantee this
// never errors. Pretty annoying to do though
func (tx *Transaction) Cid() (*cid.Cid, error) {
	obj, err := cbor.WrapObject(tx, mh.SHA2_256, -1)
	if err != nil {
		return nil, err
	}

	return obj.Cid(), nil
}

type Receipt struct {
	Success bool
	Result  interface{}
	// TODO: the whole receipt thing needs to be worked out still.
}
