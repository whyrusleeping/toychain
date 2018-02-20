package contract

import (
	"context"
	"encoding/json"
	"fmt"

	"gx/ipfs/QmVmDhyTTUcQXFD1rRQ64fGLMSAoaQvNH3hwuaCFAPq2hy/errors"

	types "github.com/whyrusleeping/toychain/types"
	cid "gx/ipfs/QmcZfnkapfECQGcLZaf9B79NRg7cRa9EnZh4LSbkCzwNvY/go-cid"
	hamt "gx/ipfs/QmdBXcN47jVwKLwSyN9e9xYVZ7WcAWgQ5N4cmNw7nzWq2q/go-hamt-ipld"
)

func LoadState(ctx context.Context, cs *hamt.CborIpldStore, c *cid.Cid) (*State, error) {
	n, err := hamt.LoadNode(ctx, cs, c)
	if err != nil {
		return nil, err
	}

	return &State{root: n, store: cs}, nil
}

type State struct {
	root  *hamt.Node
	store *hamt.CborIpldStore
}

func NewState(s *hamt.CborIpldStore, r *hamt.Node) *State {
	return &State{root: r, store: s}
}

type Actor struct {
	Code   *cid.Cid
	Memory *cid.Cid
	Nonce  uint64
}

func loadActor(ctx context.Context, st *hamt.Node, a types.Address) (*Actor, error) {
	actData, err := st.Find(ctx, string(a))
	if err != nil {
		return nil, err
	}

	var act Actor
	if err := json.Unmarshal(actData, &act); err != nil {
		return nil, fmt.Errorf("invalid account: %s", err)
	}

	return &act, nil
}

func (s *State) Copy() *State {
	return &State{
		root:  s.root.Copy(),
		store: s.store,
	}
}

func (s *State) GetActor(ctx context.Context, a types.Address) (*Actor, error) {
	return loadActor(ctx, s.root, a)

}

func (s *State) SetActor(ctx context.Context, a types.Address, act *Actor) error {
	data, err := json.Marshal(act)
	if err != nil {
		return err
	}

	if err := s.root.Set(ctx, string(a), data); err != nil {
		return err
	}

	return nil
}

type reverter interface {
	Revert() bool
}

func errIsFatal(e error) bool {
	// TODO: maybe the safer way of doing this is to assume all errors mean
	// 'revert' and special 'Fatal' errors mean to halt execution?
	re, ok := e.(reverter)
	return !(ok && re.Revert())
}

func (s *State) ActorExec(ctx context.Context, tx *types.Transaction) (*types.Receipt, error) {
	act, err := s.GetActor(ctx, tx.To)
	if err != nil {
		return nil, fmt.Errorf("get actor: %s", err)
	}

	var from *Actor
	if tx.To != tx.From {
		a, err := s.GetActor(ctx, tx.From)
		if err != nil {
			return nil, fmt.Errorf("get actor: %s", err)
		}
		from = a
	} else {
		from = act
	}

	if from.Nonce != tx.Nonce {
		return nil, fmt.Errorf("invalid nonce")
	}
	from.Nonce++

	contract, err := s.GetContract(ctx, act.Code)
	if err != nil {
		return nil, fmt.Errorf("get contract: %s %s", act.Code, err)
	}

	st, err := s.LoadContractState(ctx, act.Memory)
	if err != nil {
		return nil, fmt.Errorf("state load: %s", err)
	}

	cctx := &CallContext{
		State:         s,
		From:          tx.From,
		FromNonce:     tx.Nonce, // TODO: maybe just include the whole transaction?
		Ctx:           ctx,
		ContractState: st,
		Address:       tx.To,
	}
	val, err := contract.Call(cctx, tx.Method, tx.Params)
	if err != nil {
		if errIsFatal(err) {
			return nil, err
		} else {
			// TODO: is this all we have to do to 'revert'?
			// need to check if 'state' was mutated
			// also need to mark success/failure in the receipt
			return &types.Receipt{
				Success: false,
			}, nil
		}
	}

	nmemory, err := st.Flush(ctx)
	if err != nil {
		return nil, err
	}

	act.Memory = nmemory
	if err := s.SetActor(ctx, tx.To, act); err != nil {
		return nil, errors.Wrap(err, "set actor failed")
	}

	if tx.To != tx.From {
		if err := s.SetActor(ctx, tx.From, from); err != nil {
			return nil, errors.Wrap(err, "set actor failed")
		}
	}

	return &types.Receipt{Success: true, Result: val}, nil
}

func (s *State) NonceForActor(ctx context.Context, addr types.Address) (uint64, error) {
	act, err := s.GetActor(ctx, addr)
	if err != nil {
		return 0, err
	}

	return act.Nonce, nil
}

func (s *State) ActorCall(ctx context.Context, addr types.Address, op func(*ContractState, uint64, Contract) error) error {
	act, err := s.GetActor(ctx, addr)
	if err != nil {
		return fmt.Errorf("get actor: %s", err)
	}

	contract, err := s.GetContract(ctx, act.Code)
	if err != nil {
		return fmt.Errorf("get contract: %s %s", act.Code, err)
	}

	st, err := s.LoadContractState(ctx, act.Memory)
	if err != nil {
		return fmt.Errorf("state load: %s", err)
	}

	if err := op(st, act.Nonce, contract); err != nil {
		return err
	}

	nmemory, err := st.Flush(ctx)
	if err != nil {
		return err
	}

	act.Memory = nmemory

	if err := s.SetActor(ctx, addr, act); err != nil {
		return fmt.Errorf("set actor: %s", err)
	}

	return nil
}

func (s *State) ApplyTransactions(ctx context.Context, txs []*types.Transaction) error {
	for _, tx := range txs {
		if _, err := s.ActorExec(ctx, tx); err != nil {
			// TODO: if the contract execution above fails, return special
			// error such that the state isnt updated, but we continue here
			return err
		}
	}

	return nil
}

func (s *State) Flush(ctx context.Context) (*cid.Cid, error) {
	if err := s.root.Flush(ctx); err != nil {
		return nil, err
	}

	return s.store.Put(ctx, s.root)
}

func (s *State) NewContractState() *ContractState {
	return &ContractState{
		n:    hamt.NewNode(s.store),
		cstr: s.store,
	}
}

func (cs *ContractState) Node() *hamt.Node {
	return cs.n
}

func (s *State) LoadContractState(ctx context.Context, mem *cid.Cid) (*ContractState, error) {
	n, err := hamt.LoadNode(ctx, s.store, mem)
	if err != nil {
		return nil, fmt.Errorf("store get: %s", err)
	}

	return &ContractState{
		n:    n,
		cstr: s.store,
	}, nil
}

// Actually, this probably should take a cid, not an address
func (s *State) GetContract(ctx context.Context, codeHash *cid.Cid) (Contract, error) {
	switch {
	case codeHash.Equals(ToychainContractCid):
		return new(ToychainTokenContract), nil
	default:
		return nil, fmt.Errorf("no contract code found")
	}
}

type ContractState struct {
	n     *hamt.Node
	cstr  *hamt.CborIpldStore
	nonce uint64
}

func (cs *ContractState) Flush(ctx context.Context) (*cid.Cid, error) {
	if err := cs.n.Flush(ctx); err != nil {
		return nil, err
	}

	return cs.cstr.Put(ctx, cs.n)
}

func (cs *ContractState) Get(ctx context.Context, k string) ([]byte, error) {
	return cs.n.Find(ctx, k)
}

func (cs *ContractState) Set(ctx context.Context, k string, val []byte) error {
	return cs.n.Set(ctx, k, val)
}
