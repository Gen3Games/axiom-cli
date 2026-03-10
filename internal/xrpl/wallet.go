package xrpl

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Peersyst/xrpl-go/pkg/crypto"
	"github.com/Peersyst/xrpl-go/xrpl/queries/account"
	"github.com/Peersyst/xrpl-go/xrpl/queries/common"
	"github.com/Peersyst/xrpl-go/xrpl/queries/ledger"
	"github.com/Peersyst/xrpl-go/xrpl/rpc"
	xrplTypes "github.com/Peersyst/xrpl-go/xrpl/transaction/types"
	xrplWallet "github.com/Peersyst/xrpl-go/xrpl/wallet"
)

type Wallet struct {
	seed    string
	classic string
}

func NewRandomWallet() (*Wallet, error) {
	wallet, err := xrplWallet.New(crypto.ED25519())
	if err != nil {
		return nil, fmt.Errorf("create xrpl wallet: %w", err)
	}
	return &Wallet{seed: wallet.Seed, classic: string(wallet.ClassicAddress)}, nil
}

func WalletFromSeed(seed string) (*Wallet, error) {
	wallet, err := xrplWallet.FromSeed(seed, "")
	if err != nil {
		return nil, fmt.Errorf("load xrpl wallet from seed: %w", err)
	}
	return &Wallet{seed: seed, classic: string(wallet.ClassicAddress)}, nil
}

func (w *Wallet) Seed() string {
	return w.seed
}

func (w *Wallet) Address() string {
	return w.classic
}

func GetBalance(ctx context.Context, rpcURL string, classicAddress string) (string, error) {
	client, err := newRPCClient(rpcURL)
	if err != nil {
		return "", err
	}
	resp, err := client.GetAccountInfo(&account.InfoRequest{
		Account:     xrplTypes.Address(classicAddress),
		LedgerIndex: common.LedgerTitle("validated"),
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "actnotfound") {
			return "0", nil
		}
		return "", fmt.Errorf("get xrpl account info: %w", err)
	}
	balanceDrops := resp.AccountData.Balance.String()
	return dropsToXRP(balanceDrops), nil
}

func SubmitBridgePayment(ctx context.Context, rpcURL string, seed string, destination string, destinationTag int, amountXRP string) (string, error) {
	wallet, err := xrplWallet.FromSeed(seed, "")
	if err != nil {
		return "", fmt.Errorf("load wallet: %w", err)
	}

	client, err := newRPCClient(rpcURL)
	if err != nil {
		return "", err
	}

	accountInfo, err := client.GetAccountInfo(&account.InfoRequest{
		Account:     wallet.ClassicAddress,
		LedgerIndex: common.LedgerTitle("validated"),
	})
	if err != nil {
		return "", fmt.Errorf("get account info: %w", err)
	}

	ledger, err := client.GetLedger(&ledger.Request{LedgerIndex: common.LedgerTitle("validated")})
	if err != nil {
		return "", fmt.Errorf("get validated ledger: %w", err)
	}

	drops, err := xrpToDrops(amountXRP)
	if err != nil {
		return "", err
	}

	tx := map[string]any{
		"TransactionType":    "Payment",
		"Account":            string(wallet.ClassicAddress),
		"Destination":        destination,
		"DestinationTag":     uint32(destinationTag),
		"Amount":             drops,
		"Fee":                "12",
		"Sequence":           accountInfo.AccountData.Sequence,
		"LastLedgerSequence": uint32(ledger.LedgerIndex + 20),
	}

	txBlob, txHash, err := wallet.Sign(tx)
	if err != nil {
		return "", fmt.Errorf("sign payment: %w", err)
	}
	if _, err := client.SubmitTxBlob(txBlob, false); err != nil {
		return "", fmt.Errorf("submit payment: %w", err)
	}
	return txHash, nil
}

func xrpToDrops(amount string) (string, error) {
	parts := strings.SplitN(strings.TrimSpace(amount), ".", 2)
	if len(parts) == 0 || parts[0] == "" {
		return "", fmt.Errorf("invalid XRP amount")
	}
	whole := parts[0]
	fraction := ""
	if len(parts) == 2 {
		fraction = parts[1]
	}
	if len(fraction) > 6 {
		fraction = fraction[:6]
	}
	for len(fraction) < 6 {
		fraction += "0"
	}
	return whole + fraction, nil
}

func dropsToXRP(drops string) string {
	clean := strings.TrimSpace(drops)
	if len(clean) <= 6 {
		return "0." + strings.Repeat("0", 6-len(clean)) + clean
	}
	whole := clean[:len(clean)-6]
	fraction := strings.TrimRight(clean[len(clean)-6:], "0")
	if fraction == "" {
		return whole
	}
	return whole + "." + fraction
}

func newRPCClient(rpcURL string) (*rpc.Client, error) {
	cfg, err := rpc.NewClientConfig(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("create xrpl rpc config: %w", err)
	}
	return rpc.NewClient(cfg), nil
}

func ParseDestinationTag(value string) (int, error) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("invalid destination tag")
	}
	return parsed, nil
}
