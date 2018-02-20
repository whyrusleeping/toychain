package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"math/big"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"strings"

	ma "gx/ipfs/QmWWQ2Txc2c6tqjsBpzg5Ar652cHPGNsQQp2SejkNmkUMb/go-multiaddr"
	//peer "gx/ipfs/QmWNY7dV54ZDYmTA1ykVdwNCqC11mpU4zSUp6XDpLTH9eG/go-libp2p-peer"
	ipfsaddr "gx/ipfs/QmQViVWBHbU6HmYjXcdNq7tVASCNgdg64ZGcauuDkLCivW/go-ipfs-addr"
	swarm "gx/ipfs/QmSwZMWwFZSUpe5muU2xgTUwppH24KfMwdPXiwbEp2c6G5/go-libp2p-swarm"
	bstore "gx/ipfs/QmTVDM4LCSUMFNQzbDLL9zQwp8usE6QHymFdh3h8vL9v6b/go-ipfs-blockstore"
	pstore "gx/ipfs/QmXauCuJzmzapetmC6W4TuDJLL1yFFrVzSHoWv8YdbmnxH/go-libp2p-peerstore"
	none "gx/ipfs/QmZRcGYvxdauCd7hHnMYLYqcZRaDjv24c7eUNyJojAcdBb/go-ipfs-routing/none"
	crypto "gx/ipfs/QmaPbCnUMBohSGo3KnxEa2bHqyJVVeEEcwtqJAYxerieBo/go-libp2p-crypto"
	"gx/ipfs/QmcZfnkapfECQGcLZaf9B79NRg7cRa9EnZh4LSbkCzwNvY/go-cid"

	contract "github.com/whyrusleeping/toychain/contract"
	core "github.com/whyrusleeping/toychain/core"
	libp2p "github.com/whyrusleeping/toychain/libp2p"
	types "github.com/whyrusleeping/toychain/types"

	"gx/ipfs/QmSFihvoND3eDaAYRCeLgLPt62yCPgMZs1NSZmKFEtJQQw/go-libp2p-floodsub"

	ds "gx/ipfs/QmPpegoMqhAEqjncrzArm7KVWAkCm78rqL2DPuNjhPrshg/go-datastore"
	dssync "gx/ipfs/QmPpegoMqhAEqjncrzArm7KVWAkCm78rqL2DPuNjhPrshg/go-datastore/sync"

	bserv "github.com/ipfs/go-ipfs/blockservice"
	bitswap "github.com/ipfs/go-ipfs/exchange/bitswap"
	bsnet "github.com/ipfs/go-ipfs/exchange/bitswap/network"
	dag "github.com/ipfs/go-ipfs/merkledag"
	path "github.com/ipfs/go-ipfs/path"
	resolver "github.com/ipfs/go-ipfs/path/resolver"

	cmds "gx/ipfs/QmZ9hww8R3FKrDRCYPxhN13m6XgjPDpaSvdUfisPvERzXz/go-ipfs-cmds"
	cmdhttp "gx/ipfs/QmZ9hww8R3FKrDRCYPxhN13m6XgjPDpaSvdUfisPvERzXz/go-ipfs-cmds/http"
	cmdkit "gx/ipfs/QmceUdzxkimdYsgtX733uNgzf1DLHyBKN6ehGSp85ayppM/go-ipfs-cmdkit"
)

var RootCmd = &cmds.Command{
	Options: []cmdkit.Option{
		cmdkit.StringOption("api", "set the api port to use").WithDefault(":3453"),
		cmds.OptionEncodingType,
	},
	Subcommands: make(map[string]*cmds.Command),
}

var rootSubcommands = map[string]*cmds.Command{
	"daemon":  DaemonCmd,
	"addrs":   AddrsCmd,
	"bitswap": BitswapCmd,
	"dag":     DagCmd,
	"wallet":  WalletCmd,
	"swarm":   SwarmCmd,
	"id":      IdCmd,
}

func init() {
	for k, v := range rootSubcommands {
		RootCmd.Subcommands[k] = v
	}
}

var DaemonCmd = &cmds.Command{
	Helptext: cmdkit.HelpText{
		Tagline: "run the toychain daemon",
	},
	Arguments: []cmdkit.Argument{
		cmdkit.StringArg("bootstrap", false, true, "nodes to bootstrap to"),
	},
	Run: daemonRun,
}

func daemonRun(req *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) {
	api := req.Options["api"].(string)

	hsh := fnv.New64()
	hsh.Write([]byte(api))
	seed := hsh.Sum64()

	r := rand.New(rand.NewSource(int64(seed)))
	priv, _, err := crypto.GenerateKeyPairWithReader(crypto.RSA, 2048, r)
	if err != nil {
		panic(err)
	}

	p2pcfg := libp2p.DefaultConfig()
	p2pcfg.PeerKey = priv
	laddr, err := ma.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d", 1000+(seed%20000)))
	if err != nil {
		panic(err)
	}

	p2pcfg.ListenAddrs = []ma.Multiaddr{laddr}

	// set up networking
	h, err := libp2p.Construct(context.Background(), p2pcfg)
	if err != nil {
		panic(err)
	}

	fsub, err := floodsub.NewFloodSub(context.Background(), h)
	if err != nil {
		panic(err)
	}

	// set up storage (a bit more complicated than it realistically needs to be right now)
	bs := bstore.NewBlockstore(dssync.MutexWrap(ds.NewMapDatastore()))
	nilr, _ := none.ConstructNilRouting(nil, nil, nil)
	bsnet := bsnet.NewFromIpfsHost(h, nilr)
	bswap := bitswap.New(context.Background(), h.ID(), bsnet, bs, true)
	bserv := bserv.New(bs, bswap)
	dag := dag.NewDAGService(bserv)

	// TODO: work on what parameters we pass to the toychain node
	tch, err := core.NewToychainNode(h, fsub, dag, bserv, bswap.(*bitswap.Bitswap))
	if err != nil {
		panic(err)
	}

	if len(req.Arguments) > 0 {
		a, err := ipfsaddr.ParseString(req.Arguments[0])
		if err != nil {
			panic(err)
		}
		err = h.Connect(context.Background(), pstore.PeerInfo{
			ID:    a.ID(),
			Addrs: []ma.Multiaddr{a.Transport()},
		})
		if err != nil {
			panic(err)
		}
		fmt.Println("Connected to other peer!")
	}

	for _, a := range h.Addrs() {
		fmt.Println(a.String() + "/ipfs/" + h.ID().Pretty())
	}

	if err := writeDaemonLock(); err != nil {
		panic(err)
	}

	servenv := &CommandEnv{
		ctx:  context.Background(),
		Node: tch,
	}

	cfg := cmdhttp.NewServerConfig()
	cfg.APIPath = "/api"

	handler := cmdhttp.NewHandler(servenv, RootCmd, cfg)

	ch := make(chan os.Signal)
	signal.Notify(ch, os.Interrupt)

	go func() {
		panic(http.ListenAndServe(api, handler))
	}()

	<-ch
	removeDaemonLock()
}

type CommandEnv struct {
	ctx  context.Context
	Node *core.ToychainNode
}

func (ce *CommandEnv) Context() context.Context {
	return ce.ctx
}

func GetNode(env cmds.Environment) *core.ToychainNode {
	ce := env.(*CommandEnv)
	return ce.Node
}

var AddrsCmd = &cmds.Command{
	Subcommands: map[string]*cmds.Command{
		"new":    AddrsNewCmd,
		"list":   AddrsListCmd,
		"lookup": AddrsLookupCmd,
	},
}

var AddrsNewCmd = &cmds.Command{
	Run: func(req *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) {
		tch := GetNode(env)
		naddr := core.CreateNewAddress()
		tch.Addresses = append(tch.Addresses, naddr)
		re.Emit(naddr)
	},
	Type: types.Address(""),
	Encoders: cmds.EncoderMap{
		cmds.Text: cmds.MakeEncoder(func(req *cmds.Request, w io.Writer, v interface{}) error {
			a, ok := v.(*types.Address)
			if !ok {
				return fmt.Errorf("unexpected type: %T", v)
			}

			_, err := fmt.Fprintln(w, a.String())
			return err
		}),
	},
}

var AddrsListCmd = &cmds.Command{
	Run: func(req *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) {
		tch := GetNode(env)
		re.Emit(tch.Addresses)
	},
	Type: []types.Address{},
	Encoders: cmds.EncoderMap{
		cmds.Text: cmds.MakeEncoder(func(req *cmds.Request, w io.Writer, v interface{}) error {
			addrs, ok := v.(*[]types.Address)
			if !ok {
				return fmt.Errorf("unexpected type: %T", v)
			}

			for _, a := range *addrs {
				_, err := fmt.Fprintln(w, a.String())
				if err != nil {
					return err
				}
			}
			return nil
		}),
	},
}

var AddrsLookupCmd = &cmds.Command{
	Arguments: []cmdkit.Argument{
		cmdkit.StringArg("address", true, false, "address to find peerID for"),
	},
	Run: func(req *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) {
		tch := GetNode(env)

		address, err := types.ParseAddress(req.Arguments[0])
		if err != nil {
			re.SetError(err, cmdkit.ErrNormal)
			return
		}

		v, err := tch.Lookup.Lookup(address)
		if err != nil {
			re.SetError(err, cmdkit.ErrNormal)
			return
		}
		re.Emit(v.Pretty())
	},
	Type: string(""),
	Encoders: cmds.EncoderMap{
		cmds.Text: cmds.MakeEncoder(func(req *cmds.Request, w io.Writer, v interface{}) error {
			pid, ok := v.(string)
			if !ok {
				return fmt.Errorf("unexpected type: %T", v)
			}

			_, err := fmt.Fprintln(w, pid)
			if err != nil {
				return err
			}
			return nil
		}),
	},
}

var BitswapCmd = &cmds.Command{
	Subcommands: map[string]*cmds.Command{
		"wantlist": BitswapWantlistCmd,
	},
}

var BitswapWantlistCmd = &cmds.Command{
	Run: func(req *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) {
		tch := GetNode(env)
		re.Emit(tch.Bitswap.GetWantlist())
	},
	Type: []*cid.Cid{},
	Encoders: cmds.EncoderMap{
		cmds.Text: cmds.MakeEncoder(func(req *cmds.Request, w io.Writer, v interface{}) error {
			wants, ok := v.(*[]cid.Cid)
			if !ok {
				return fmt.Errorf("unexpected type: %T", v)
			}

			for _, want := range *wants {
				_, err := fmt.Fprintln(w, want.String())
				if err != nil {
					return err
				}
			}
			return nil
		}),
	},
}

var DagCmd = &cmds.Command{
	Subcommands: map[string]*cmds.Command{
		"get": DagGetCmd,
	},
}

var DagGetCmd = &cmds.Command{
	Arguments: []cmdkit.Argument{
		cmdkit.StringArg("object", true, false, "ref of node to get"),
	},
	Run: func(req *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) {
		tch := GetNode(env)

		res := resolver.NewBasicResolver(tch.DAG)

		p, err := path.ParsePath(req.Arguments[0])
		if err != nil {
			re.SetError(err, cmdkit.ErrNormal)
			return
		}

		nd, err := res.ResolvePath(req.Context, p)
		if err != nil {
			re.SetError(err, cmdkit.ErrNormal)
			return
		}

		re.Emit(nd)
	},
}

var WalletCmd = &cmds.Command{
	Subcommands: map[string]*cmds.Command{
		"send":    WalletSendCmd,
		"balance": WalletGetBalanceCmd,
	},
}

var WalletGetBalanceCmd = &cmds.Command{
	Arguments: []cmdkit.Argument{
		cmdkit.StringArg("account", false, false, "account to get balance of"),
	},
	Run: func(req *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) {
		tch := GetNode(env)

		addr := tch.Addresses[0]
		if len(req.Arguments) > 0 {
			a, err := types.ParseAddress(req.Arguments[0])
			if err != nil {
				re.SetError(err, cmdkit.ErrNormal)
				return
			}
			addr = a
		}

		stroot := tch.StateMgr.GetStateRoot()
		act, err := stroot.GetActor(req.Context, contract.ToychainContractAddr)
		if err != nil {
			re.SetError(err, cmdkit.ErrNormal)
			return
		}

		ct, err := stroot.GetContract(req.Context, act.Code)
		if err != nil {
			re.SetError(err, cmdkit.ErrNormal)
			return
		}

		cst, err := stroot.LoadContractState(req.Context, act.Memory)
		if err != nil {
			re.SetError(err, cmdkit.ErrNormal)
			return
		}

		cctx := &contract.CallContext{Ctx: req.Context, ContractState: cst}
		val, err := ct.Call(cctx, "getBalance", []interface{}{addr})
		if err != nil {
			re.SetError(err, cmdkit.ErrNormal)
			return
		}

		re.Emit(val)
	},
	Type: big.Int{},
	Encoders: cmds.EncoderMap{
		cmds.Text: cmds.MakeEncoder(func(req *cmds.Request, w io.Writer, v interface{}) error {
			val, ok := v.(*big.Int)
			if !ok {
				return fmt.Errorf("got unexpected type: %T", v)
			}
			fmt.Fprintln(w, val.String())
			return nil
		}),
	},
}

// TODO: this command should exist in some form, but its really specialized.
// The issue is that its really 'call transfer on the toychain token contract
// and send tokens from our default account to a given actor'.
var WalletSendCmd = &cmds.Command{
	Arguments: []cmdkit.Argument{
		cmdkit.StringArg("value", true, false, "amount to send"),
		cmdkit.StringArg("to", true, false, "actor to send transaction to"),
	},
	Run: func(req *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) {
		tch := GetNode(env)

		amount, ok := big.NewInt(0).SetString(req.Arguments[0], 10)
		if !ok {
			re.SetError("failed to parse amount", cmdkit.ErrNormal)
			return
		}
		toaddr, err := types.ParseAddress(req.Arguments[1])
		if err != nil {
			re.SetError(err, cmdkit.ErrNormal)
			return
		}

		from := tch.Addresses[0]

		nonce, err := tch.StateMgr.GetStateRoot().NonceForActor(req.Context, from)
		if err != nil {
			re.SetError(err, cmdkit.ErrNormal)
			return
		}

		tx := &types.Transaction{
			From:   from,
			To:     contract.ToychainContractAddr,
			Nonce:  nonce,
			Method: "transfer",
			Params: []interface{}{toaddr, amount},
		}

		tch.SendNewTransaction(tx)
	},
}

type idOutput struct {
	Addresses       []string
	ID              string
	AgentVersion    string
	ProtocolVersion string
	PublicKey       string
}

var IdCmd = &cmds.Command{
	Options: []cmdkit.Option{
		// TODO: ideally copy this from the `ipfs id` command
		cmdkit.StringOption("format", "f", "specify an output format"),
	},
	Run: func(req *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) {
		tch := GetNode(env)

		var out idOutput
		for _, a := range tch.Host.Addrs() {
			out.Addresses = append(out.Addresses, a.String())
		}
		out.ID = tch.Host.ID().Pretty()

		re.Emit(&out)
	},
	Type: idOutput{},
	Encoders: cmds.EncoderMap{
		cmds.Text: cmds.MakeEncoder(func(req *cmds.Request, w io.Writer, v interface{}) error {
			val, ok := v.(*idOutput)
			if !ok {
				return fmt.Errorf("unexpected type: %T", v)
			}

			format, found := req.Options["format"].(string)
			if found {
				output := format
				output = strings.Replace(output, "<id>", val.ID, -1)
				output = strings.Replace(output, "<aver>", val.AgentVersion, -1)
				output = strings.Replace(output, "<pver>", val.ProtocolVersion, -1)
				output = strings.Replace(output, "<pubkey>", val.PublicKey, -1)
				output = strings.Replace(output, "<addrs>", strings.Join(val.Addresses, "\n"), -1)
				output = strings.Replace(output, "\\n", "\n", -1)
				output = strings.Replace(output, "\\t", "\t", -1)
				_, err := fmt.Fprintln(w, output)
				return err
			} else {

				marshaled, err := json.MarshalIndent(val, "", "\t")
				if err != nil {
					return err
				}
				marshaled = append(marshaled, byte('\n'))
				_, err = w.Write(marshaled)
				return err
			}
		}),
	},
}

var SwarmCmd = &cmds.Command{
	Helptext: cmdkit.HelpText{
		Tagline: "Interact with the swarm.",
		ShortDescription: `
'toychain swarm' is a tool to manipulate the libp2p swarm. The swarm is the
component that opens, listens for, and maintains connections to other
libp2p peers on the internet.
`,
	},
	Subcommands: map[string]*cmds.Command{
		//"addrs":      swarmAddrsCmd,
		"connect": swarmConnectCmd,
		//"disconnect": swarmDisconnectCmd,
		//"filters":    swarmFiltersCmd,
		//"peers":      swarmPeersCmd,
	},
}

// COPIED FROM go-ipfs core/commands/swarm.go

// parseAddresses is a function that takes in a slice of string peer addresses
// (multiaddr + peerid) and returns slices of multiaddrs and peerids.
func parseAddresses(addrs []string) (iaddrs []ipfsaddr.IPFSAddr, err error) {
	iaddrs = make([]ipfsaddr.IPFSAddr, len(addrs))
	for i, saddr := range addrs {
		iaddrs[i], err = ipfsaddr.ParseString(saddr)
		if err != nil {
			return nil, cmds.ClientError("invalid peer address: " + err.Error())
		}
	}
	return
}

// peersWithAddresses is a function that takes in a slice of string peer addresses
// (multiaddr + peerid) and returns a slice of properly constructed peers
func peersWithAddresses(addrs []string) (pis []pstore.PeerInfo, err error) {
	iaddrs, err := parseAddresses(addrs)
	if err != nil {
		return nil, err
	}

	for _, iaddr := range iaddrs {
		pis = append(pis, pstore.PeerInfo{
			ID:    iaddr.ID(),
			Addrs: []ma.Multiaddr{iaddr.Transport()},
		})
	}
	return pis, nil
}

type connectResult struct {
	Peer    string
	Success bool
}

var swarmConnectCmd = &cmds.Command{
	Helptext: cmdkit.HelpText{
		Tagline: "Open connection to a given address.",
		ShortDescription: `
'toychain swarm connect' opens a new direct connection to a peer address.

The address format is a multiaddr:

toychain swarm connect /ip4/104.131.131.82/tcp/4001/ipfs/QmaCpDMGvV2BGHeYERUEnRQAwe3N8SzbUtfsmvsqQLuvuJ
`,
	},
	Arguments: []cmdkit.Argument{
		cmdkit.StringArg("address", true, true, "Address of peer to connect to.").EnableStdin(),
	},
	Run: func(req *cmds.Request, re cmds.ResponseEmitter, env cmds.Environment) {
		ctx := req.Context

		n := GetNode(env)

		addrs := req.Arguments

		snet, ok := n.Host.Network().(*swarm.Network)
		if !ok {
			re.SetError("peerhost network was not swarm", cmdkit.ErrNormal)
			return
		}

		swrm := snet.Swarm()

		pis, err := peersWithAddresses(addrs)
		if err != nil {
			re.SetError(err, cmdkit.ErrNormal)
			return
		}

		output := make([]connectResult, len(pis))
		for i, pi := range pis {
			swrm.Backoff().Clear(pi.ID)

			output[i].Peer = pi.ID.Pretty()

			err := n.Host.Connect(ctx, pi)
			if err != nil {
				re.SetError(fmt.Errorf("%s failure: %s", output[i], err), cmdkit.ErrNormal)
				return
			}
		}

		re.Emit(output)
	},
	Type: []connectResult{},
	Encoders: cmds.EncoderMap{
		cmds.Text: cmds.MakeEncoder(func(req *cmds.Request, w io.Writer, v interface{}) error {
			res, ok := v.(*[]connectResult)
			if !ok {
				return fmt.Errorf("unexpected type: %T", v)
			}
			for _, a := range *res {
				fmt.Fprintf(w, "connect %s success\n", a.Peer)
			}
			return nil
		}),
	},
}
