package evm

import (
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

type Wallet struct {
	privateKey *ecdsa.PrivateKey
	address    common.Address
}

func NewRandomWallet() (*Wallet, string, error) {
	privateKey, err := crypto.GenerateKey()
	if err != nil {
		return nil, "", fmt.Errorf("generate private key: %w", err)
	}
	wallet, err := WalletFromPrivateKeyHex(hex.EncodeToString(crypto.FromECDSA(privateKey)))
	if err != nil {
		return nil, "", err
	}
	return wallet, wallet.PrivateKeyHex(), nil
}

func WalletFromPrivateKeyHex(privateKeyHex string) (*Wallet, error) {
	clean := strings.TrimPrefix(strings.TrimSpace(privateKeyHex), "0x")
	privateKey, err := crypto.HexToECDSA(clean)
	if err != nil {
		return nil, fmt.Errorf("parse private key: %w", err)
	}
	address := crypto.PubkeyToAddress(privateKey.PublicKey)
	return &Wallet{privateKey: privateKey, address: address}, nil
}

func (w *Wallet) Address() common.Address {
	return w.address
}

func (w *Wallet) PrivateKeyHex() string {
	return hex.EncodeToString(crypto.FromECDSA(w.privateKey))
}

func (w *Wallet) SignMessage(message string) (string, error) {
	hash := accounts.TextHash([]byte(message))
	signature, err := crypto.Sign(hash, w.privateKey)
	if err != nil {
		return "", fmt.Errorf("sign message: %w", err)
	}
	signature[64] += 27
	return "0x" + hex.EncodeToString(signature), nil
}

func GetBalance(ctx context.Context, rpcURL string, address common.Address) (*big.Int, error) {
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("connect rpc: %w", err)
	}
	defer client.Close()

	balance, err := client.BalanceAt(ctx, address, nil)
	if err != nil {
		return nil, fmt.Errorf("get balance: %w", err)
	}
	return balance, nil
}

func (w *Wallet) SendNativeXRP(ctx context.Context, rpcURL string, chainID *big.Int, to common.Address, amountWei *big.Int) (common.Hash, error) {
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return common.Hash{}, fmt.Errorf("connect rpc: %w", err)
	}
	defer client.Close()

	nonce, err := client.PendingNonceAt(ctx, w.address)
	if err != nil {
		return common.Hash{}, fmt.Errorf("get nonce: %w", err)
	}
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return common.Hash{}, fmt.Errorf("suggest gas price: %w", err)
	}
	msg := ethereum.CallMsg{From: w.address, To: &to, Value: amountWei}
	gasLimit, err := client.EstimateGas(ctx, msg)
	if err != nil {
		gasLimit = 21000
	}

	tx := types.NewTransaction(nonce, to, amountWei, gasLimit, gasPrice, nil)
	signed, err := types.SignTx(tx, types.NewLondonSigner(chainID), w.privateKey)
	if err != nil {
		return common.Hash{}, fmt.Errorf("sign transaction: %w", err)
	}
	if err := client.SendTransaction(ctx, signed); err != nil {
		return common.Hash{}, fmt.Errorf("send transaction: %w", err)
	}
	return signed.Hash(), nil
}
