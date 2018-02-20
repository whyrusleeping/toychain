package contract

import (
	"context"
	"fmt"
	"math/big"
	"reflect"

	types "github.com/whyrusleeping/toychain/types"
	"gx/ipfs/QmVmDhyTTUcQXFD1rRQ64fGLMSAoaQvNH3hwuaCFAPq2hy/errors"

	// TODO: no usage of this package directly
	hamt "gx/ipfs/QmdBXcN47jVwKLwSyN9e9xYVZ7WcAWgQ5N4cmNw7nzWq2q/go-hamt-ipld"

	mh "gx/ipfs/QmZyZDi491cCNTLfAhwcaDii2Kg4pwKRkhqQzURGDvY6ua/go-multihash"
	cid "gx/ipfs/QmcZfnkapfECQGcLZaf9B79NRg7cRa9EnZh4LSbkCzwNvY/go-cid"
)

var ToychainContractCid = identCid("toychain")
var ToychainContractAddr = types.Address("toychain")

type revertError struct {
	err error
}

func (re *revertError) Error() string {
	return re.err.Error()
}

func (re *revertError) Revert() bool {
	return true
}

var _ reverter = (*revertError)(nil)

func identCid(s string) *cid.Cid {
	h, err := mh.Sum([]byte(s), mh.ID, len(s))
	if err != nil {
		panic(err)
	}
	return cid.NewCidV1(cid.Raw, h)
}

// CallContext is information accessible to the contract during a given invocation
type CallContext struct {
	Ctx           context.Context
	From          types.Address
	FromNonce     uint64
	State         *State
	ContractState *ContractState
	Address       types.Address
}

type Contract interface {
	Call(ctx *CallContext, method string, args []interface{}) (interface{}, error)
}

type ToychainTokenContract struct{}

var ErrMethodNotFound = fmt.Errorf("unrecognized method")

func (ftc *ToychainTokenContract) Call(ctx *CallContext, method string, args []interface{}) (interface{}, error) {
	switch method {
	case "transfer":
		return ftc.transfer(ctx, args)
	case "getBalance":
		return mustTypedCallClosure(ftc.getBalance)(ctx, args)
	default:
		return nil, ErrMethodNotFound
	}
}

func (ftc *ToychainTokenContract) getBalance(ctx *CallContext, addr types.Address) (interface{}, error) {
	accData, err := ctx.ContractState.Get(ctx.Ctx, string(addr))
	if err != nil {
		return nil, err
	}

	return big.NewInt(0).SetBytes(accData), nil
}

func addressCast(i interface{}) (types.Address, error) {
	switch i := i.(type) {
	case types.Address:
		return i, nil
	case string:
		if i[:2] == "0x" {
			return types.ParseAddress(i)
		}
		return types.Address(i), nil
	default:
		return "", fmt.Errorf("first argument must be an Address")
	}
}

// very temporary hack
func numberCast(i interface{}) (*big.Int, error) {
	switch i := i.(type) {
	case string:
		n, ok := big.NewInt(0).SetString(i, 10)
		if !ok {
			return nil, fmt.Errorf("arg must be a number")
		}
		return n, nil
	case *big.Int:
		return i, nil
	case float64:
		return big.NewInt(int64(i)), nil
	case []byte:
		return big.NewInt(0).SetBytes(i), nil
	default:
		fmt.Printf("type is: %T\n", i)
		panic("noo")
	}
}

func (ftc *ToychainTokenContract) transfer(ctx *CallContext, args []interface{}) (interface{}, error) {
	if len(args) != 2 {
		return nil, &revertError{fmt.Errorf("transfer takes exactly 2 arguments")}
	}

	toAddr, err := addressCast(args[0])
	if err != nil {
		return nil, err
	}

	amount, err := numberCast(args[1])
	if err != nil {
		return nil, err
	}

	// TODO: formalize the creation of new accounts
	_, err = ctx.State.GetActor(ctx.Ctx, toAddr)
	switch err {
	case hamt.ErrNotFound:
		if err := ctx.State.SetActor(ctx.Ctx, toAddr, &Actor{}); err != nil {
			return nil, err
		}
	default:
		return nil, err
	case nil:
		// ok
	}
	//*/

	cs := ctx.ContractState
	fromData, err := cs.Get(ctx.Ctx, string(ctx.From))
	if err != nil && err != hamt.ErrNotFound {
		return nil, err
	}

	fromBalance := big.NewInt(0).SetBytes(fromData)

	if fromBalance.Cmp(amount) < 0 {
		return nil, &revertError{fmt.Errorf("not enough funds")}
	}

	fromBalance = fromBalance.Sub(fromBalance, amount)

	toData, err := cs.Get(ctx.Ctx, string(toAddr))
	if err != nil && err != hamt.ErrNotFound {
		return nil, errors.Wrap(err, "could not read target balance")
	}

	toBalance := big.NewInt(0).SetBytes(toData)
	toBalance = toBalance.Add(toBalance, amount)

	if err := cs.Set(ctx.Ctx, string(ctx.From), fromBalance.Bytes()); err != nil {
		return nil, errors.Wrap(err, "could not write fromBalance")
	}

	if err := cs.Set(ctx.Ctx, string(toAddr), toBalance.Bytes()); err != nil {
		return nil, errors.Wrap(err, "could not write toBalance")
	}

	return nil, nil
}

var (
	addrType   = reflect.TypeOf(types.Address(""))
	uint64Type = reflect.TypeOf(uint64(0))
	int64Type  = reflect.TypeOf(int64(0))
)

func mustTypedCallClosure(f interface{}) func(*CallContext, []interface{}) (interface{}, error) {
	out, err := makeTypedCallClosure(f)
	if err != nil {
		panic(err)
	}
	return out
}

func makeTypedCallClosure(f interface{}) (func(*CallContext, []interface{}) (interface{}, error), error) {
	fval := reflect.ValueOf(f)
	if fval.Kind() != reflect.Func {
		return nil, fmt.Errorf("must pass a function")
	}

	ftype := fval.Type()
	if ftype.In(0) != reflect.TypeOf(&CallContext{}) {
		return nil, fmt.Errorf("first parameter must be call context")
	}

	if ftype.NumOut() != 2 || !ftype.Out(1).Implements(reflect.TypeOf((*error)(nil)).Elem()) {
		return nil, fmt.Errorf("function must return (interface{}, error)")
	}

	return func(cctx *CallContext, args []interface{}) (interface{}, error) {
		if ftype.NumIn()-1 != len(args) {
			return nil, fmt.Errorf("expected %d args", ftype.NumIn()-1)
		}

		callargs := []reflect.Value{reflect.ValueOf(cctx)}
		for i := 1; i < ftype.NumIn(); i++ {
			switch t := ftype.In(i); t {
			case addrType:
				v, err := castToAddress(reflect.ValueOf(args[i-1]))
				if err != nil {
					return nil, err
				}
				callargs = append(callargs, v)
			case uint64Type:
				v, ok := args[i-1].(uint64)
				if !ok {
					return nil, fmt.Errorf("position %d, expected %s", i-1, t)
				}
				callargs = append(callargs, reflect.ValueOf(v))
			case int64Type:
				v, ok := args[i-1].(int64)
				if !ok {
					return nil, fmt.Errorf("position %d, expected %s", i-1, t)
				}
				callargs = append(callargs, reflect.ValueOf(v))
			default:
				return nil, fmt.Errorf("unsupported type: %s", ftype.In(i))
			}
		}

		out := fval.Call(callargs)
		outv := out[0].Interface()

		var outErr error
		if e, ok := out[1].Interface().(error); ok {
			outErr = e
		}

		return outv, outErr
	}, nil
}

func castToAddress(v reflect.Value) (reflect.Value, error) {
	// TODO: assert 'v' is of type string first
	switch t := v.Interface().(type) {
	case types.Address:
		return reflect.ValueOf(t), nil
	case string:
		return reflect.ValueOf(types.Address(t)), nil
	default:
		return reflect.Value{}, fmt.Errorf("attempted to cast non-string to to Address (%T)", v.Interface())
	}
}
