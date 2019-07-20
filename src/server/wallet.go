// thresh-wallet
//
// Copyright 2019 by KeyFuse
//
// GPLv3 License

package server

import (
	"fmt"
	"sort"
	"sync"

	"github.com/tokublock/tokucore/network"
	"github.com/tokublock/tokucore/xcore"
)

// Ticker --
type Ticker struct {
	One5M  float64 `json:"15m"`
	Last   float64 `json:"last"`
	Buy    float64 `json:"buy"`
	Sell   float64 `json:"sell"`
	Symbol string  `json:"symbol"`
}

// SendFees --
type SendFees struct {
	Fees          uint64 `json:"fees"`
	TotalValue    uint64 `json:"total_value"`
	SendableValue uint64 `json:"sendable_value"`
}

// Tx --
type Tx struct {
	Txid        string `json:"txid"`
	Fee         int64  `json:"fee"`
	Value       int64  `json:"value"`
	Confirmed   bool   `json:"confirmed"`
	BlockTime   int64  `json:"block_time"`
	BlockHeight int64  `json:"block_height"`
}

// UTXO --
type UTXO struct {
	Pos          uint32 `json:"pos"`
	Txid         string `json:"txid"`
	Vout         uint32 `json:"vout"`
	Value        uint64 `json:"value"`
	Address      string `json:"address"`
	Confirmed    bool   `json:"confirmed"`
	SvrPubKey    string `json:"svrpubkey"`
	Scriptpubkey string `json:"Scriptpubkey"`
}

// Balance --
type Balance struct {
	TotalBalance       uint64 `json:"total_balance"`
	UnconfirmedBalance uint64 `json:"unconfirmed_balance"`
}

// Unspent --
type Unspent struct {
	Txid         string `json:"txid"`
	Vout         uint32 `json:"vout"`
	Value        uint64 `json:"value"`
	Confirmed    bool   `json:"confirmed"`
	BlockTime    uint32 `json:"block_time"`
	BlockHeight  uint32 `json:"block_height"`
	Scriptpubkey string `json:"Scriptpubkey"`
}

// Address --
type Address struct {
	mu       sync.Mutex
	Pos      uint32    `json:"pos"`
	Address  string    `json:"address"`
	Balance  Balance   `json:"balance"`
	Txs      []Tx      `json:"txs"`
	Unspents []Unspent `json:"unspents"`
}

// Wallet --
type Wallet struct {
	mu              sync.Mutex
	net             *network.Network
	UID             string              `json:"uid"`
	DID             string              `json:"did"`
	Backup          Backup              `json:"backup"`
	LastPos         uint32              `json:"lastpos"`
	Address         map[string]*Address `json:"address"`
	SvrMasterPrvKey string              `json:"svrmasterprvkey"`
	CliMasterPubKey string              `json:"climasterpubkey"`
}

// NewWallet -- creates new Wallet.
func NewWallet() *Wallet {
	return &Wallet{
		Address: make(map[string]*Address),
	}
}

// Lock -- used to lock the wallet entry for thread-safe purposes.
func (w *Wallet) Lock() {
	w.mu.Lock()
}

// Unlock -- used to unlock the wallet entry for thread-safe purposes.
func (w *Wallet) Unlock() {
	w.mu.Unlock()
}

// Addresses -- used to returns all the address of the wallet.
func (w *Wallet) Addresses() []string {
	var addrs []string

	w.Lock()
	defer w.Unlock()
	for addr := range w.Address {
		addrs = append(addrs, addr)
	}
	return addrs
}

// NewAddress -- used to generate new address.
func (w *Wallet) NewAddress(typ string) (*Address, error) {
	net := w.net

	// New address.
	w.Lock()
	defer w.Unlock()

	pos := w.LastPos
	addr, err := createSharedAddress(pos, w.SvrMasterPrvKey, w.CliMasterPubKey, net, typ)
	if err != nil {
		return nil, err
	}

	address := &Address{
		Pos:     pos,
		Address: addr,
	}
	w.Address[addr] = address
	w.LastPos++

	return address, nil
}

// UpdateUnspents -- update the address balance/unspent which fetchs from the chain.
func (w *Wallet) UpdateUnspents(addr string, unspents []Unspent) {
	w.Lock()
	address := w.Address[addr]
	w.Unlock()

	address.mu.Lock()
	defer address.mu.Unlock()

	var balance, unconfirmedBalance uint64
	for _, unspent := range unspents {
		if !unspent.Confirmed {
			unconfirmedBalance += unspent.Value
		}
		balance += unspent.Value
	}
	address.Unspents = unspents
	address.Balance.TotalBalance = balance
	address.Balance.UnconfirmedBalance = unconfirmedBalance
}

// UpdateTxs -- update the tx fetchs from the chain.
func (w *Wallet) UpdateTxs(addr string, txs []Tx) {
	w.Lock()
	address := w.Address[addr]
	w.Unlock()

	address.mu.Lock()
	defer address.mu.Unlock()
	address.Txs = txs
}

// Balance --used to return balance of the wallet.
func (w *Wallet) Balance() *Balance {
	w.Lock()
	defer w.Unlock()
	balance := &Balance{}
	for _, addr := range w.Address {
		balance.TotalBalance += addr.Balance.TotalBalance
		balance.UnconfirmedBalance += addr.Balance.UnconfirmedBalance
	}
	return balance
}

// Unspents -- used to return unspent which all the value upper than the amount.
func (w *Wallet) Unspents(sendAmount uint64) ([]UTXO, error) {
	var rsp []UTXO
	var utxos []UTXO
	var thresh uint64
	var balance uint64
	net := w.net

	w.Lock()
	defer w.Unlock()

	for _, addr := range w.Address {
		for _, unspent := range addr.Unspents {
			svrpubkey, err := createSvrChildPubKey(addr.Pos, w.SvrMasterPrvKey, net)
			if err != nil {
				return nil, err
			}
			utxos = append(utxos, UTXO{
				Pos:          addr.Pos,
				Txid:         unspent.Txid,
				Vout:         unspent.Vout,
				Value:        unspent.Value,
				Address:      addr.Address,
				Confirmed:    unspent.Confirmed,
				SvrPubKey:    svrpubkey,
				Scriptpubkey: unspent.Scriptpubkey,
			})
		}
		balance += addr.Balance.TotalBalance
	}

	// Check.
	if balance < sendAmount {
		return nil, fmt.Errorf("unpsents.suffient.req.amount[%v].allbalance[%v]", sendAmount, balance)
	}

	// Sort by value desc.
	sort.Slice(utxos, func(i, j int) bool { return utxos[i].Value > utxos[j].Value })

	// Patch.
	for _, utxo := range utxos {
		thresh += utxo.Value
		rsp = append(rsp, utxo)
		if thresh >= sendAmount {
			break
		}
	}
	return rsp, nil
}

// Txs -- used to return the txs starts from offset to offset+limit.
func (w *Wallet) Txs(offset int, limit int) []Tx {
	var txs []Tx

	w.Lock()
	defer w.Unlock()
	for _, addr := range w.Address {
		txs = append(txs, addr.Txs...)
	}

	// Sort txs.
	sort.Slice(txs, func(i, j int) bool {
		if !txs[i].Confirmed || !txs[j].Confirmed {
			return false
		}
		return txs[i].BlockTime > txs[j].BlockTime
	})
	size := len(txs)
	if offset >= size {
		return nil
	}
	if (offset + limit) > size {
		return txs[offset:]
	} else {
		return txs[offset : offset+limit]
	}
}

// SendFees -- used to get the send fees by send amount.
func (w *Wallet) SendFees(sendValue uint64, feesPerKB int) (*SendFees, error) {
	unspents, err := w.Unspents(sendValue)
	if err != nil {
		return nil, err
	}

	totalValue := w.Balance().TotalBalance
	estsize := xcore.EstimateNormalSize(len(unspents), 1+1)
	fees := uint64((estsize * int64(feesPerKB)) / 1000)

	if fees >= totalValue {
		return nil, fmt.Errorf("balace[%v].is.smaller.than.fees[%v]", totalValue, fees)
	}

	sendableValue := sendValue
	// Send all case.
	if (sendableValue + fees) > totalValue {
		sendableValue = totalValue - fees
	}
	return &SendFees{
		Fees:          fees,
		TotalValue:    totalValue,
		SendableValue: sendableValue,
	}, nil
}
