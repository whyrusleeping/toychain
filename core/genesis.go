package core

import (
	"context"
	"math/big"

	contract "github.com/whyrusleeping/toychain/contract"
	types "github.com/whyrusleeping/toychain/types"
	hamt "gx/ipfs/QmdBXcN47jVwKLwSyN9e9xYVZ7WcAWgQ5N4cmNw7nzWq2q/go-hamt-ipld"
)

var InitialNetworkTokens = big.NewInt(2000000000)

func CreateGenesisBlock(cs *hamt.CborIpldStore) (*types.Block, error) {
	ctx := context.Background()

	stateRoot := hamt.NewNode(cs)
	s := contract.NewState(cs, stateRoot)

	genesis := new(types.Block)

	// Set up toychain token contract
	tokenState := s.NewContractState()
	if err := tokenState.Set(ctx, string(contract.ToychainContractAddr), InitialNetworkTokens.Bytes()); err != nil {
		return nil, err
	}

	tsCid, err := cs.Put(ctx, tokenState.Node())
	if err != nil {
		return nil, err
	}

	tokActor := &contract.Actor{
		Code:   contract.ToychainContractCid,
		Memory: tsCid,
	}
	if err := s.SetActor(ctx, contract.ToychainContractAddr, tokActor); err != nil {
		return nil, err
	}

	srcid, err := s.Flush(ctx)
	if err != nil {
		return nil, err
	}

	genesis.StateRoot = srcid
	return genesis, nil
}
