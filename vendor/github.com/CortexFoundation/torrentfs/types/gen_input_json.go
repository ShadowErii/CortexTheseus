// Code generated by github.com/fjl/gencodec. DO NOT EDIT.

package types

import (
	"encoding/json"
	"math/big"

	"github.com/CortexFoundation/CortexTheseus/common"
)

// MarshalJSON marshals as JSON.
func (i InputMeta) MarshalJSON() ([]byte, error) {
	type InputMeta struct {
		Comment  string         `json:"comment"`
		Hash     common.Address `json:"hash"`
		RawSize  uint64         `json:"rawSize"`
		Shape    []uint64       `json:"shape"`
		BlockNum big.Int        `json:"blockNum"`
	}
	var enc InputMeta
	enc.Comment = i.Comment
	enc.Hash = i.Hash
	enc.RawSize = i.RawSize
	enc.Shape = i.Shape
	enc.BlockNum = i.BlockNum
	return json.Marshal(&enc)
}

// UnmarshalJSON unmarshals from JSON.
func (i *InputMeta) UnmarshalJSON(input []byte) error {
	type InputMeta struct {
		Comment  *string         `json:"comment"`
		Hash     *common.Address `json:"hash"`
		RawSize  *uint64         `json:"rawSize"`
		Shape    []uint64        `json:"shape"`
		BlockNum *big.Int        `json:"blockNum"`
	}
	var dec InputMeta
	if err := json.Unmarshal(input, &dec); err != nil {
		return err
	}
	if dec.Comment != nil {
		i.Comment = *dec.Comment
	}
	if dec.Hash != nil {
		i.Hash = *dec.Hash
	}
	if dec.RawSize != nil {
		i.RawSize = *dec.RawSize
	}
	if dec.Shape != nil {
		i.Shape = dec.Shape
	}
	if dec.BlockNum != nil {
		i.BlockNum = *dec.BlockNum
	}
	return nil
}
