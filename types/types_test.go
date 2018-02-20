package types

import (
	"math/big"
	"testing"
)

func TestTransactionRoundTrip(t *testing.T) {
	tx := &Transaction{
		To:       Address("foobar"),
		From:     Address("a cool person"),
		Method:   "catsfunc",
		Nonce:    417,
		TickCost: big.NewInt(9112),
		Ticks:    big.NewInt(15500),
		Params: []interface{}{
			"asdasd",
			1234,
		},
	}

	data, err := tx.Marshal()
	if err != nil {
		t.Fatal(err)
	}

	var otx Transaction
	if err := otx.Unmarshal(data); err != nil {
		t.Fatal(err)
	}

	t.Log(otx)
}
