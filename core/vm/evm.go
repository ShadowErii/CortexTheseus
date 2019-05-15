// Copyright 2014 The go-cortex Authors
// This file is part of the go-cortex library.
//
// The go-cortex library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-cortex library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-cortex library. If not, see <http://www.gnu.org/licenses/>.

package vm

import (
	_ "encoding/hex"
	"math/big"
	"sync/atomic"
	"time"

	"errors"
	"fmt"
	"github.com/CortexFoundation/CortexTheseus/common"
	"github.com/CortexFoundation/CortexTheseus/common/hexutil"
	"github.com/CortexFoundation/CortexTheseus/core/types"
	"github.com/CortexFoundation/CortexTheseus/crypto"
	"github.com/CortexFoundation/CortexTheseus/inference/synapse"
	"github.com/CortexFoundation/CortexTheseus/log"
	"github.com/CortexFoundation/CortexTheseus/params"
	"github.com/CortexFoundation/CortexTheseus/torrentfs"
)

// emptyCodeHash is used by create to ensure deployment is disallowed to already
// deployed contract addresses (relevant after the account abstraction).
var emptyCodeHash = crypto.Keccak256Hash(nil)

type (
	// CanTransferFunc is the signature of a transfer guard function
	CanTransferFunc func(StateDB, common.Address, *big.Int) bool
	// TransferFunc is the signature of a transfer function
	TransferFunc func(StateDB, common.Address, common.Address, *big.Int)
	// GetHashFunc returns the nth block hash in the blockchain
	// and is used by the BLOCKHASH EVM op code.
	GetHashFunc func(uint64) common.Hash
)

// run runs the given contract and takes care of running precompiles with a fallback to the byte code interpreter.
func run(evm *EVM, contract *Contract, input []byte, readOnly bool) ([]byte, error) {
	if contract.CodeAddr != nil {
		precompiles := PrecompiledContractsHomestead
		if evm.ChainConfig().IsByzantium(evm.BlockNumber) {
			precompiles = PrecompiledContractsByzantium
		}
		if p := precompiles[*contract.CodeAddr]; p != nil {
			return RunPrecompiledContract(p, input, contract)
		}
	}
	for _, interpreter := range evm.interpreters {
		if interpreter.CanRun(contract.Code) {
			if evm.interpreter != interpreter {
				// Ensure that the interpreter pointer is set back
				// to its current value upon return.
				defer func(i Interpreter) {
					evm.interpreter = i
				}(evm.interpreter)
				evm.interpreter = interpreter
			}
			return interpreter.Run(contract, input, readOnly)
		}
	}
	return nil, ErrNoCompatibleInterpreter
}

// Context provides the EVM with auxiliary information. Once provided
// it shouldn't be modified.
type Context struct {
	// CanTransfer returns whether the account contains
	// sufficient ctxcer to transfer the value
	CanTransfer CanTransferFunc
	// Transfer transfers ctxcer from one account to the other
	Transfer TransferFunc
	// GetHash returns the hash corresponding to n
	GetHash GetHashFunc

	// Message information
	Origin   common.Address // Provides information for ORIGIN
	GasPrice *big.Int       // Provides information for GASPRICE

	// Block information
	Coinbase    common.Address // Provides information for COINBASE
	GasLimit    uint64         // Provides information for GASLIMIT
	BlockNumber *big.Int       // Provides information for NUMBER
	PeekNumber  *big.Int
	Time        *big.Int // Provides information for TIME
	Difficulty  *big.Int // Provides information for DIFFICULTY
}

// EVM is the Cortex Virtual Machine base object and provides
// the necessary tools to run a contract on the given state with
// the provided context. It should be noted that any error
// generated through any of the calls should be considered a
// revert-state-and-consume-all-gas operation, no checks on
// specific errors should ever be performed. The interpreter makes
// sure that any errors generated are to be considered faulty code.
//
// The EVM should never be reused and is not thread safe.
type EVM struct {
	// Context provides auxiliary blockchain related information
	Context
	// StateDB gives access to the underlying state
	StateDB StateDB
	// Depth is the current call stack
	depth int

	// chainConfig contains information about the current chain
	chainConfig *params.ChainConfig
	// chain rules contains the chain rules for the current epoch
	chainRules params.Rules
	// virtual machine configuration options used to initialise the
	// evm.
	vmConfig Config
	// global (to this context) cortex virtual machine
	// used throughout the execution of the tx.
	interpreters []Interpreter
	interpreter  Interpreter
	// abort is used to abort the EVM calling operations
	// NOTE: must be set atomically
	abort int32
	// callGasTemp holds the gas available for the current call. This is needed because the
	// available gas is calculated in gasCall* according to the 63/64 rule and later
	// applied in opCall*.
	callGasTemp uint64
	//Fs          *torrentfs.FileStorage
}

// NewEVM returns a new EVM. The returned EVM is not thread safe and should
// only ever be used *once*.
func NewEVM(ctx Context, statedb StateDB, chainConfig *params.ChainConfig, vmConfig Config) *EVM {
	/*cfg := torrentfs.Config{
		DataDir:         torrentfs.DefaultConfig.DataDir,
		Host:            torrentfs.DefaultConfig.Host,
		Port:            torrentfs.DefaultConfig.Port,
		DefaultTrackers: torrentfs.DefaultConfig.DefaultTrackers,
		SyncMode:        torrentfs.DefaultConfig.SyncMode,
		TestMode:        torrentfs.DefaultConfig.TestMode,
	}
	cfg.DataDir = vmConfig.StorageDir
	fileFs, fsErr := torrentfs.NewFileStorage(&cfg)
	if fsErr != nil {
		return nil
	}

	log.Info("File storage in vm", "fs", fileFs)*/

	evm := &EVM{
		Context:      ctx,
		StateDB:      statedb,
		vmConfig:     vmConfig,
		chainConfig:  chainConfig,
		chainRules:   chainConfig.Rules(ctx.BlockNumber),
		interpreters: make([]Interpreter, 1),
		//Fs:           fileFs,
	}

	if chainConfig.IsEWASM(ctx.BlockNumber) {
		// to be implemented by EVM-C and Wagon PRs.
		// if vmConfig.EWASMInterpreter != "" {
		//  extIntOpts := strings.Split(vmConfig.EWASMInterpreter, ":")
		//  path := extIntOpts[0]
		//  options := []string{}
		//  if len(extIntOpts) > 1 {
		//    options = extIntOpts[1..]
		//  }
		//  evm.interpreters = append(evm.interpreters, NewEVMVCInterpreter(evm, vmConfig, options))
		// } else {
		//      evm.interpreters = append(evm.interpreters, NewEWASMInterpreter(evm, vmConfig))
		// }
		panic("No supported ewasm interpreter yet.")
	}

	evm.interpreters[0] = NewEVMInterpreter(evm, vmConfig)
	evm.interpreter = evm.interpreters[0]

	return evm
}

// Cancel cancels any running EVM operation. This may be called concurrently and
// it's safe to be called multiple times.
func (evm *EVM) Cancel() {
	atomic.StoreInt32(&evm.abort, 1)
}

// Interpreter returns the current interpreter
func (evm *EVM) Interpreter() Interpreter {
	return evm.interpreter
}

func (evm *EVM) Config() Config {
	return evm.vmConfig
}

// Call executes the contract associated with the addr with the given input as
// parameters. It also handles any necessary value transfer required and takes
// the necessary steps to create accounts and reverses the state in case of an
// execution error or failed value transfer.
func (evm *EVM) Call(caller ContractRef, addr common.Address, input []byte, gas uint64, value *big.Int) (ret []byte, leftOverGas uint64, modelGas map[common.Address]uint64, err error) {
	if evm.vmConfig.NoRecursion && evm.depth > 0 {
		return nil, gas, nil, nil
	}

	// Fail if we're trying to execute above the call depth limit
	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, nil, ErrDepth
	}
	// Fail if we're trying to transfer more than the available balance
	if !evm.Context.CanTransfer(evm.StateDB, caller.Address(), value) {
		return nil, gas, nil, ErrInsufficientBalance
	}

	var (
		to       = AccountRef(addr)
		snapshot = evm.StateDB.Snapshot()
	)
	if !evm.StateDB.Exist(addr) {
		precompiles := PrecompiledContractsHomestead
		if evm.ChainConfig().IsByzantium(evm.BlockNumber) {
			precompiles = PrecompiledContractsByzantium
		}
		if precompiles[addr] == nil && evm.ChainConfig().IsEIP158(evm.BlockNumber) && value.Sign() == 0 {
			// Calling a non existing account, don't do anything, but ping the tracer
			if evm.vmConfig.Debug && evm.depth == 0 {
				evm.vmConfig.Tracer.CaptureStart(caller.Address(), addr, false, input, gas, value)
				evm.vmConfig.Tracer.CaptureEnd(ret, 0, 0, nil)
			}
			return nil, gas, nil, nil
		}
		evm.StateDB.CreateAccount(addr)
	}
	evm.Transfer(evm.StateDB, caller.Address(), to.Address(), value)

	// Initialise a new contract and set the code that is to be used by the EVM.
	// The contract is a scoped environment for this execution context only.
	contract := NewContract(caller, to, value, gas)
	contract.SetCallCode(&addr, evm.StateDB.GetCodeHash(addr), evm.StateDB.GetCode(addr))

	start := time.Now()

	// Capture the tracer start/end events in debug mode
	if evm.vmConfig.Debug && evm.depth == 0 {
		evm.vmConfig.Tracer.CaptureStart(caller.Address(), addr, false, input, gas, value)

		defer func() { // Lazy evaluation of the parameters
			evm.vmConfig.Tracer.CaptureEnd(ret, gas-contract.Gas, time.Since(start), err)
		}()
	}
	ret, err = run(evm, contract, input, false)

	if evm.vmConfig.RPC_GetInternalTransaction {
		ret = append(ret, []byte(caller.Address().String()+"-"+to.Address().String()+"-"+value.String()+",")...)
	}

	// When an error was returned by the EVM or when setting the creation code
	// above we revert to the snapshot and consume any gas remaining. Additionally
	// when we're in homestead this also counts for code storage gas errors.
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != errExecutionReverted {
			contract.UseGas(contract.Gas)
		}
	}

	//todo deadline verification

	//if evm.interpreter.IsModelMeta(ret) {
	//only if model is exist from ipfs, swarm or libtorrent ext, load model to memory
	//todo if ret is model meta, pay some fee to model owner depends on set in meta data
	// if exist
	//evm.Transfer(evm.StateDB, caller.Address(), model.Owner,model.Fee)
	//}
	return ret, contract.Gas, contract.ModelGas, err
}

// CallCode executes the contract associated with the addr with the given input
// as parameters. It also handles any necessary value transfer required and takes
// the necessary steps to create accounts and reverses the state in case of an
// execution error or failed value transfer.
//
// CallCode differs from Call in the sense that it executes the given address'
// code with the caller as context.
func (evm *EVM) CallCode(caller ContractRef, addr common.Address, input []byte, gas uint64, value *big.Int) (ret []byte, leftOverGas uint64, modelGas map[common.Address]uint64, err error) {
	if evm.vmConfig.NoRecursion && evm.depth > 0 {
		return nil, gas, nil, nil
	}

	// Fail if we're trying to execute above the call depth limit
	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, nil, ErrDepth
	}
	// Fail if we're trying to transfer more than the available balance
	if !evm.CanTransfer(evm.StateDB, caller.Address(), value) {
		return nil, gas, nil, ErrInsufficientBalance
	}

	var (
		snapshot = evm.StateDB.Snapshot()
		to       = AccountRef(caller.Address())
	)
	// initialise a new contract and set the code that is to be used by the
	// EVM. The contract is a scoped environment for this execution context
	// only.
	contract := NewContract(caller, to, value, gas)
	contract.SetCallCode(&addr, evm.StateDB.GetCodeHash(addr), evm.StateDB.GetCode(addr))

	ret, err = run(evm, contract, input, false)
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != errExecutionReverted {
			contract.UseGas(contract.Gas)
		}
	}
	return ret, contract.Gas, contract.ModelGas, err
}

// DelegateCall executes the contract associated with the addr with the given input
// as parameters. It reverses the state in case of an execution error.
//
// DelegateCall differs from CallCode in the sense that it executes the given address'
// code with the caller as context and the caller is set to the caller of the caller.
func (evm *EVM) DelegateCall(caller ContractRef, addr common.Address, input []byte, gas uint64) (ret []byte, leftOverGas uint64, modelGas map[common.Address]uint64, err error) {
	if evm.vmConfig.NoRecursion && evm.depth > 0 {
		return nil, gas, nil, nil
	}
	// Fail if we're trying to execute above the call depth limit
	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, nil, ErrDepth
	}

	var (
		snapshot = evm.StateDB.Snapshot()
		to       = AccountRef(caller.Address())
	)

	// Initialise a new contract and make initialise the delegate values
	contract := NewContract(caller, to, nil, gas).AsDelegate()
	contract.SetCallCode(&addr, evm.StateDB.GetCodeHash(addr), evm.StateDB.GetCode(addr))

	ret, err = run(evm, contract, input, false)
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != errExecutionReverted {
			contract.UseGas(contract.Gas)
			// for addr, mGas := range contract.ModelGas {
			// 	contract.ModelGas[addr] = 0
			// }
		}
	}
	return ret, contract.Gas, contract.ModelGas, err
}

// StaticCall executes the contract associated with the addr with the given input
// as parameters while disallowing any modifications to the state during the call.
// Opcodes that attempt to perform such modifications will result in exceptions
// instead of performing the modifications.
func (evm *EVM) StaticCall(caller ContractRef, addr common.Address, input []byte, gas uint64) (ret []byte, leftOverGas uint64, modelGas map[common.Address]uint64, err error) {
	if evm.vmConfig.NoRecursion && evm.depth > 0 {
		return nil, gas, nil, nil
	}
	// Fail if we're trying to execute above the call depth limit
	if evm.depth > int(params.CallCreateDepth) {
		return nil, gas, nil, ErrDepth
	}

	var (
		to       = AccountRef(addr)
		snapshot = evm.StateDB.Snapshot()
	)
	// Initialise a new contract and set the code that is to be used by the
	// EVM. The contract is a scoped environment for this execution context
	// only.
	contract := NewContract(caller, to, new(big.Int), gas)
	contract.SetCallCode(&addr, evm.StateDB.GetCodeHash(addr), evm.StateDB.GetCode(addr))

	// When an error was returned by the EVM or when setting the creation code
	// above we revert to the snapshot and consume any gas remaining. Additionally
	// when we're in Homestead this also counts for code storage gas errors.
	ret, err = run(evm, contract, input, true)
	if err != nil {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != errExecutionReverted {
			contract.UseGas(contract.Gas)
		}
	}
	return ret, contract.Gas, contract.ModelGas, err
}

// create creates a new contract using code as deployment code.
func (evm *EVM) create(caller ContractRef, code []byte, gas uint64, value *big.Int, address common.Address) ([]byte, common.Address, uint64, map[common.Address]uint64, error) {
	// Depth check execution. Fail if we're trying to execute above the
	// limit.
	if evm.depth > int(params.CallCreateDepth) {
		return nil, common.Address{}, gas, nil, ErrDepth
	}
	if !evm.CanTransfer(evm.StateDB, caller.Address(), value) {
		return nil, common.Address{}, gas, nil, ErrInsufficientBalance
	}
	nonce := evm.StateDB.GetNonce(caller.Address())
	evm.StateDB.SetNonce(caller.Address(), nonce+1)

	// Ensure there's no existing contract already at the designated address
	contractHash := evm.StateDB.GetCodeHash(address)
	if evm.StateDB.GetNonce(address) != 0 || (contractHash != (common.Hash{}) && contractHash != emptyCodeHash) {
		return nil, common.Address{}, 0, nil, ErrContractAddressCollision
	}
	// Create a new account on the state
	snapshot := evm.StateDB.Snapshot()
	evm.StateDB.CreateAccount(address)
	if evm.ChainConfig().IsEIP158(evm.BlockNumber) {
		evm.StateDB.SetNonce(address, 1)
	}
	evm.Transfer(evm.StateDB, caller.Address(), address, value)

	// initialise a new contract and set the code that is to be used by the
	// EVM. The contract is a scoped environment for this execution context
	// only.
	contract := NewContract(caller, AccountRef(address), value, gas)
	contract.SetCallCode(&address, crypto.Keccak256Hash(code), code)

	if evm.vmConfig.NoRecursion && evm.depth > 0 {
		return nil, address, gas, nil, nil
	}

	if evm.vmConfig.Debug && evm.depth == 0 {
		evm.vmConfig.Tracer.CaptureStart(caller.Address(), address, true, code, gas, value)
	}
	start := time.Now()

	ret, err := run(evm, contract, nil, false)

	if evm.vmConfig.RPC_GetInternalTransaction {
		ret = append(ret, []byte(caller.Address().String()+"-"+address.String()+"-"+value.String()+",")...)
	}

	// check whether the max code size has been exceeded
	maxCodeSizeExceeded := evm.ChainConfig().IsEIP158(evm.BlockNumber) && len(ret) > params.MaxCodeSize
	// if the contract creation ran successfully and no errors were returned
	// calculate the gas required to store the code. If the code could not
	// be stored due to not enough gas set an error and let it be handled
	// by the error checking condition below.
	if err == nil && !maxCodeSizeExceeded {
		createDataGas := uint64(len(ret)) * params.CreateDataGas
		if contract.UseGas(createDataGas) {
			evm.StateDB.SetCode(address, ret)
		} else {
			err = ErrCodeStoreOutOfGas
		}
	}

	// When an error was returned by the EVM or when setting the creation code
	// above we revert to the snapshot and consume any gas remaining. Additionally
	// when we're in homestead this also counts for code storage gas errors.
	if maxCodeSizeExceeded || (err != nil && (evm.ChainConfig().IsHomestead(evm.BlockNumber) || err != ErrCodeStoreOutOfGas)) {
		evm.StateDB.RevertToSnapshot(snapshot)
		if err != errExecutionReverted {
			contract.UseGas(contract.Gas)
		}
	}
	// Assign err if contract code size exceeds the max while the err is still empty.
	if maxCodeSizeExceeded && err == nil {
		err = errMaxCodeSizeExceeded
	}
	if evm.vmConfig.Debug && evm.depth == 0 {
		evm.vmConfig.Tracer.CaptureEnd(ret, gas-contract.Gas, time.Since(start), err)
	}
	return ret, address, contract.Gas, contract.ModelGas, err

}

// Create creates a new contract using code as deployment code.
func (evm *EVM) Create(caller ContractRef, code []byte, gas uint64, value *big.Int) (ret []byte, contractAddr common.Address, leftOverGas uint64, modelGas map[common.Address]uint64, err error) {
	contractAddr = crypto.CreateAddress(caller.Address(), evm.StateDB.GetNonce(caller.Address()))
	return evm.create(caller, code, gas, value, contractAddr)
}

// Create2 creates a new contract using code as deployment code.
//
// The different between Create2 with Create is Create2 uses sha3(0xff ++ msg.sender ++ salt ++ sha3(init_code))[12:]
// instead of the usual sender-and-nonce-hash as the address where the contract is initialized at.
func (evm *EVM) Create2(caller ContractRef, code []byte, gas uint64, endowment *big.Int, salt *big.Int) (ret []byte, contractAddr common.Address, leftOverGas uint64, modelGas map[common.Address]uint64, err error) {
	contractAddr = crypto.CreateAddress2(caller.Address(), common.BigToHash(salt), code)
	return evm.create(caller, code, gas, endowment, contractAddr)
}

// ChainConfig returns the environment's chain configuration
func (evm *EVM) ChainConfig() *params.ChainConfig { return evm.chainConfig }

const interv = 15

func (evm *EVM) DataSync(meta common.Address, dir string, errCh chan error) {
	street := big.NewInt(0).Sub(evm.PeekNumber, evm.BlockNumber)
	point := big.NewInt(time.Now().Add(confirmTime).Unix())
	if point.Cmp(evm.Context.Time) > 0 || street.Cmp(big.NewInt(params.CONFIRM_BLOCKS)) > 0 {
		cost := big.NewInt(0)
		duration := big.NewInt(0).Sub(big.NewInt(time.Now().Unix()), evm.Context.Time)
		for i := 0; i < 1200 && duration.Cmp(cost) > 0; i++ {
			if !torrentfs.ExistTorrent(meta, dir) {
				log.Warn("Inference synchronizing ... ...", "point", point, "tvm", evm.Context.Time, "ago", common.PrettyDuration(time.Duration(duration.Uint64()*1000000000)), "level", i, "number", evm.BlockNumber, "street", street)
				cost.Add(cost, big.NewInt(interv))
				time.Sleep(time.Second * interv)
				continue
			} else {
				errCh <- nil
				return
			}
		}
		log.Error("Torrent synchronized timeout", "address", meta.Hex(), "number", evm.BlockNumber, "meta", meta, "storage", dir, "street", street, "duration", duration, "cost", cost)
	} else {
		if !torrentfs.Exist(meta, dir) {
			log.Warn("Data not exist", "address", meta.Hex(), "number", evm.BlockNumber, "current", evm.BlockNumber, "meta", meta, "storage", dir)
			errCh <- synapse.ErrModelFileNotExist
			return
		} else {
			errCh <- nil
			return
		}
	}

	if !torrentfs.Exist(meta, dir) {
		log.Warn("Data not exist", "address", meta.Hex(), "number", evm.BlockNumber, "current", evm.BlockNumber, "meta", meta, "storage", dir)
		errCh <- synapse.ErrModelFileNotExist
		return
	} else {
		errCh <- nil
		return
	}

	//log.Error("Torrent synchronized timeout", "address", meta.Hex(), "number", evm.BlockNumber, "meta", meta, "storage", dir, "street", street)
	//errCh <- synapse.ErrModelFileNotExist
	//return
}

// infer function that returns an int64 as output, can be used a categorical output
func (evm *EVM) Infer(modelInfoHash, inputInfoHash string, modelRawSize, inputRawSize uint64) (uint64, error) {
	log.Info("Inference Information", "Model Hash", modelInfoHash, "Input Hash", inputInfoHash)

	modelErrCh := make(chan error)
	inputErrCh := make(chan error)

	go evm.DataSync(common.HexToAddress(modelInfoHash), evm.Config().StorageDir, modelErrCh)
	go evm.DataSync(common.HexToAddress(inputInfoHash), evm.Config().StorageDir, inputErrCh)

	for i := 0; i < 2; i++ {
		select {
		case err := <-modelErrCh:
			if err != nil {
				return 0, err
			}
		case err := <-inputErrCh:
			if err != nil {
				return 0, err
			}
		}
	}

	if !torrentfs.Available(common.HexToAddress(modelInfoHash), evm.Config().StorageDir, int64(modelRawSize)) {
		return 0, errors.New("Torrent file model not available, blockchain and torrent not match")
	}

	if !torrentfs.Available(common.HexToAddress(inputInfoHash), evm.Config().StorageDir, int64(inputRawSize)) {
		return 0, errors.New("Torrent file input not available, blockchain and torrent not match")
	}

	var (
		inferRes []byte
		errRes   error
	)

	if evm.vmConfig.InferURI == "" {
		inferRes, errRes = synapse.Engine().InferByInfoHash(modelInfoHash, inputInfoHash)
	} else {
		inferRes, errRes = synapse.Engine().RemoteInferByInfoHash(
			modelInfoHash,
			inputInfoHash,
			evm.vmConfig.InferURI)
	}

	if errRes == nil {
		log.Info("Inference Succeed", "label", inferRes)
	}

	return synapse.ArgMax(inferRes), errRes
}

// infer function that returns an int64 as output, can be used a categorical output
func (evm *EVM) InferArray(modelInfoHash string, inputArray []byte, modelRawSize uint64) (uint64, error) {
	log.Info("Inference Infomation", "Model Hash", modelInfoHash, "number", evm.BlockNumber)
	log.Debug("Infer Detail", "Input Content", hexutil.Encode(inputArray))

	modelErrCh := make(chan error)
	go evm.DataSync(common.HexToAddress(modelInfoHash), evm.Config().StorageDir, modelErrCh)

	select {
	case err := <-modelErrCh:
		if err != nil {
			return 0, err
		}
	}

	if !torrentfs.Available(common.HexToAddress(modelInfoHash), evm.Config().StorageDir, int64(modelRawSize)) {
		return 0, errors.New("Torrent file model not available, blockchain and torrent not match")
	}

	var (
		inferRes []byte
		errRes   error
	)

	if evm.vmConfig.InferURI == "" {
		inferRes, errRes = synapse.Engine().InferByInputContent(modelInfoHash, inputArray)
	} else {
		inferRes, errRes = synapse.Engine().RemoteInferByInputContent(
			modelInfoHash,
			evm.vmConfig.InferURI,
			inputArray,
		)
	}

	if errRes == nil {
		log.Info("Inference Succeed", "label", inferRes)
	}

	return synapse.ArgMax(inferRes), errRes
}

func (evm *EVM) GetMetaHash(addr common.Address) (meta common.Address, err error) {
	metaRaw := evm.StateDB.GetCode(addr)
	if IsModelMeta(metaRaw) {
		if modelMeta, err := types.ParseModelMeta(metaRaw); err != nil {
			return common.EmptyAddress, err
		} else {
			return modelMeta.Hash, nil
		}
	}

	if IsInputMeta(metaRaw) {
		if inputMeta, err := types.ParseInputMeta(metaRaw); err != nil {
			return common.EmptyAddress, err
		} else {
			return inputMeta.Hash, nil
		}
	}

	return common.EmptyAddress, errors.New("quota limit reached")
}

func (evm *EVM) GetModelMeta(addr common.Address) (meta *types.ModelMeta, err error) {
	log.Trace(fmt.Sprintf("GeteModelMeta = %v", addr))
	modelMetaRaw := evm.StateDB.GetCode(addr)
	log.Trace(fmt.Sprintf("modelMetaRaw: %v", modelMetaRaw))
	if modelMeta, err := types.ParseModelMeta(modelMetaRaw); err != nil {
		return &types.ModelMeta{}, err
	} else {
		return modelMeta, nil
	}
}

func (evm *EVM) GetInputMeta(addr common.Address) (meta *types.InputMeta, err error) {
	inputMetaRaw := evm.StateDB.GetCode(addr)
	log.Trace(fmt.Sprintf("inputMetaRaw: %v", inputMetaRaw))
	if inputMeta, err := types.ParseInputMeta(inputMetaRaw); err != nil {
		return &types.InputMeta{}, err
	} else {
		return inputMeta, nil
	}
}
