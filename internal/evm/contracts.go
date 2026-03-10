package evm

import (
	"context"
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"strings"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
)

const (
	marketABIJSON = `[
		{"name":"settlementToken","type":"function","stateMutability":"view","inputs":[],"outputs":[{"type":"address"}]},
		{"name":"status","type":"function","stateMutability":"view","inputs":[],"outputs":[{"type":"uint8"}]},
		{"name":"outcomeCount","type":"function","stateMutability":"view","inputs":[],"outputs":[{"type":"uint8"}]},
		{"name":"marketOpen","type":"function","stateMutability":"view","inputs":[],"outputs":[{"type":"uint256"}]},
		{"name":"betsClose","type":"function","stateMutability":"view","inputs":[],"outputs":[{"type":"uint256"}]},
		{"name":"totalVirtualPool","type":"function","stateMutability":"view","inputs":[],"outputs":[{"type":"uint256"}]},
		{"name":"maxTimeBonus","type":"function","stateMutability":"view","inputs":[],"outputs":[{"type":"uint256"}]},
		{"name":"totalPool","type":"function","stateMutability":"view","inputs":[],"outputs":[{"type":"uint256"}]},
		{"name":"outcomePools","type":"function","stateMutability":"view","inputs":[{"type":"uint8"}],"outputs":[{"type":"uint256"}]},
		{"name":"virtualOutcomeSeeds","type":"function","stateMutability":"view","inputs":[{"type":"uint8"}],"outputs":[{"type":"uint256"}]},
		{"name":"totalWeightedShares","type":"function","stateMutability":"view","inputs":[{"type":"uint8"}],"outputs":[{"type":"uint256"}]},
		{"name":"buy","type":"function","stateMutability":"payable","inputs":[{"name":"outcome","type":"uint8"},{"name":"amount","type":"uint256"},{"name":"minShares","type":"uint256"}],"outputs":[]},
		{"name":"claim","type":"function","stateMutability":"nonpayable","inputs":[],"outputs":[]}
	]`
	erc20ABIJSON = `[
		{"name":"approve","type":"function","stateMutability":"nonpayable","inputs":[{"name":"spender","type":"address"},{"name":"amount","type":"uint256"}],"outputs":[{"type":"bool"}]},
		{"name":"decimals","type":"function","stateMutability":"view","inputs":[],"outputs":[{"type":"uint8"}]},
		{"name":"symbol","type":"function","stateMutability":"view","inputs":[],"outputs":[{"type":"string"}]}
	]`
	utilityABIJSON = `[
		{"name":"batchClaim","type":"function","stateMutability":"nonpayable","inputs":[{"name":"markets","type":"address[]"}],"outputs":[{"type":"uint256"}]},
		{"name":"maxBatchSize","type":"function","stateMutability":"view","inputs":[],"outputs":[{"type":"uint256"}]}
	]`
)

var (
	marketABI  = mustParseABI(marketABIJSON)
	erc20ABI   = mustParseABI(erc20ABIJSON)
	utilityABI = mustParseABI(utilityABIJSON)
)

type MarketState struct {
	SettlementToken     common.Address
	Status              uint8
	OutcomeCount        uint8
	MarketOpen          uint64
	BetsClose           uint64
	TotalVirtualPool    *big.Int
	MaxTimeBonus        *big.Int
	TotalPool           *big.Int
	OutcomePools        []*big.Int
	VirtualSeeds        []*big.Int
	TotalWeightedShares []*big.Int
}

type BuyQuote struct {
	MarketTitle            string `json:"marketTitle"`
	OutcomeIndex           int    `json:"outcomeIndex"`
	OutcomeLabel           string `json:"outcomeLabel"`
	AmountXRP              string `json:"amountXrp"`
	CurrentSpotPrice       string `json:"currentSpotPrice"`
	AverageEntryPrice      string `json:"averageEntryPrice"`
	PostTradeSpotPrice     string `json:"postTradeSpotPrice"`
	TimeBonusMultiplier    string `json:"timeBonusMultiplier"`
	BaseShares             string `json:"baseShares"`
	WeightedShares         string `json:"weightedShares"`
	SuggestedMinShares     string `json:"suggestedMinShares"`
	GrossPayoutIfWinNowXRP string `json:"grossPayoutIfWinNowXrp"`
	ProfitIfWinNowXRP      string `json:"profitIfWinNowXrp"`
}

func mustParseABI(raw string) abi.ABI {
	parsed, err := abi.JSON(strings.NewReader(raw))
	if err != nil {
		panic(err)
	}
	return parsed
}

func LoadMarketState(ctx context.Context, rpcURL string, marketAddress common.Address) (*MarketState, error) {
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("connect rpc: %w", err)
	}
	defer client.Close()

	settlementToken, err := callAddress(ctx, client, marketAddress, marketABI, "settlementToken")
	if err != nil {
		return nil, err
	}
	status, err := callUint8(ctx, client, marketAddress, marketABI, "status")
	if err != nil {
		return nil, err
	}
	outcomeCount, err := callUint8(ctx, client, marketAddress, marketABI, "outcomeCount")
	if err != nil {
		return nil, err
	}
	totalPool, err := callBigInt(ctx, client, marketAddress, marketABI, "totalPool")
	if err != nil {
		return nil, err
	}
	marketOpen, err := callBigInt(ctx, client, marketAddress, marketABI, "marketOpen")
	if err != nil {
		return nil, err
	}
	betsClose, err := callBigInt(ctx, client, marketAddress, marketABI, "betsClose")
	if err != nil {
		return nil, err
	}
	totalVirtualPool, err := callBigInt(ctx, client, marketAddress, marketABI, "totalVirtualPool")
	if err != nil {
		return nil, err
	}
	maxTimeBonus, err := callBigInt(ctx, client, marketAddress, marketABI, "maxTimeBonus")
	if err != nil {
		return nil, err
	}
	outcomePools := make([]*big.Int, 0, outcomeCount)
	virtualSeeds := make([]*big.Int, 0, outcomeCount)
	totalWeightedShares := make([]*big.Int, 0, outcomeCount)
	for outcome := uint8(0); outcome < outcomeCount; outcome++ {
		outcomePool, err := callBigInt(ctx, client, marketAddress, marketABI, "outcomePools", outcome)
		if err != nil {
			return nil, err
		}
		virtualSeed, err := callBigInt(ctx, client, marketAddress, marketABI, "virtualOutcomeSeeds", outcome)
		if err != nil {
			return nil, err
		}
		weightedShares, err := callBigInt(ctx, client, marketAddress, marketABI, "totalWeightedShares", outcome)
		if err != nil {
			return nil, err
		}
		outcomePools = append(outcomePools, outcomePool)
		virtualSeeds = append(virtualSeeds, virtualSeed)
		totalWeightedShares = append(totalWeightedShares, weightedShares)
	}

	return &MarketState{
		SettlementToken:     settlementToken,
		Status:              status,
		OutcomeCount:        outcomeCount,
		MarketOpen:          marketOpen.Uint64(),
		BetsClose:           betsClose.Uint64(),
		TotalVirtualPool:    totalVirtualPool,
		MaxTimeBonus:        maxTimeBonus,
		TotalPool:           totalPool,
		OutcomePools:        outcomePools,
		VirtualSeeds:        virtualSeeds,
		TotalWeightedShares: totalWeightedShares,
	}, nil
}

func QuoteBuy(state *MarketState, amountWei *big.Int, outcome uint8, marketTitle string, outcomeLabel string, now time.Time) (*BuyQuote, error) {
	if state.Status != 0 {
		return nil, fmt.Errorf("market is not active (status=%d)", state.Status)
	}
	if outcome >= state.OutcomeCount {
		return nil, fmt.Errorf("outcome index %d is out of range for market with %d outcomes", outcome, state.OutcomeCount)
	}
	if amountWei == nil || amountWei.Sign() <= 0 {
		return nil, fmt.Errorf("amount must be greater than zero")
	}

	outcomePool := cloneBigInt(state.OutcomePools[outcome])
	outcomeVirtual := cloneBigInt(state.VirtualSeeds[outcome])
	totalPool := cloneBigInt(state.TotalPool)
	totalVirtual := cloneBigInt(state.TotalVirtualPool)
	existingWeighted := cloneBigInt(state.TotalWeightedShares[outcome])

	startNum := new(big.Int).Add(totalPool, totalVirtual)
	startDen := new(big.Int).Add(outcomePool, outcomeVirtual)
	endNum := new(big.Int).Add(startNum, amountWei)
	endDen := new(big.Int).Add(startDen, amountWei)

	left := new(big.Int).Mul(startNum, endDen)
	right := new(big.Int).Mul(endNum, startDen)
	numerator := new(big.Int).Mul(amountWei, new(big.Int).Add(left, right))
	denominator := new(big.Int).Mul(big.NewInt(2), new(big.Int).Mul(startDen, endDen))
	baseShares := new(big.Int).Quo(numerator, denominator)

	bonus := big.NewInt(1_000_000_000_000_000_000)
	if state.MaxTimeBonus != nil && state.MaxTimeBonus.Sign() > 0 {
		bonus = cloneBigInt(state.MaxTimeBonus)
		duration := uint64(0)
		if state.BetsClose > state.MarketOpen {
			duration = state.BetsClose - state.MarketOpen
		}
		if duration > 0 {
			nowUnix := uint64(now.Unix())
			elapsed := uint64(0)
			if nowUnix > state.MarketOpen {
				elapsed = nowUnix - state.MarketOpen
			}
			if elapsed > duration {
				elapsed = duration
			}
			bonusDelta := new(big.Int).Sub(state.MaxTimeBonus, big.NewInt(1_000_000_000_000_000_000))
			decay := new(big.Int).Quo(new(big.Int).Mul(bonusDelta, new(big.Int).SetUint64(elapsed)), new(big.Int).SetUint64(duration))
			bonus = new(big.Int).Sub(state.MaxTimeBonus, decay)
		}
	}

	weightedShares := new(big.Int).Quo(new(big.Int).Mul(baseShares, bonus), big.NewInt(1_000_000_000_000_000_000))
	suggestedMinShares := new(big.Int).Quo(new(big.Int).Mul(weightedShares, big.NewInt(99)), big.NewInt(100))

	currentSpotPrice := formatRatioPercent(startDen, startNum)
	averageEntryPrice := formatRatioPercent(amountWei, baseShares)
	postTradeSpotPrice := formatRatioPercent(endDen, endNum)

	totalPoolAfter := new(big.Int).Add(totalPool, amountWei)
	totalWeightedAfter := new(big.Int).Add(existingWeighted, weightedShares)
	grossPayout := big.NewInt(0)
	if totalWeightedAfter.Sign() > 0 {
		grossPayout = new(big.Int).Quo(new(big.Int).Mul(totalPoolAfter, weightedShares), totalWeightedAfter)
	}
	profit := new(big.Int).Sub(cloneBigInt(grossPayout), amountWei)

	return &BuyQuote{
		MarketTitle:            marketTitle,
		OutcomeIndex:           int(outcome),
		OutcomeLabel:           outcomeLabel,
		AmountXRP:              formatFixed18(amountWei, 6),
		CurrentSpotPrice:       currentSpotPrice,
		AverageEntryPrice:      averageEntryPrice,
		PostTradeSpotPrice:     postTradeSpotPrice,
		TimeBonusMultiplier:    formatMultiplier(bonus),
		BaseShares:             formatFixed18(baseShares, 6),
		WeightedShares:         formatFixed18(weightedShares, 6),
		SuggestedMinShares:     suggestedMinShares.String(),
		GrossPayoutIfWinNowXRP: formatFixed18(grossPayout, 6),
		ProfitIfWinNowXRP:      formatFixed18(profit, 6),
	}, nil
}

func ClaimMarket(ctx context.Context, rpcURL string, chainID *big.Int, privateKeyHex string, marketAddress common.Address) (common.Hash, error) {
	return sendContractTransaction(ctx, rpcURL, chainID, privateKeyHex, marketAddress, marketABI, "claim", big.NewInt(0))
}

func BatchClaim(ctx context.Context, rpcURL string, chainID *big.Int, privateKeyHex string, utilityAddress common.Address, markets []common.Address) (common.Hash, error) {
	data, err := utilityABI.Pack("batchClaim", markets)
	if err != nil {
		return common.Hash{}, fmt.Errorf("pack batchClaim: %w", err)
	}
	return sendRawTransaction(ctx, rpcURL, chainID, privateKeyHex, utilityAddress, data, big.NewInt(0))
}

func BuyPosition(ctx context.Context, rpcURL string, chainID *big.Int, privateKeyHex string, marketAddress common.Address, outcome uint8, amountWei *big.Int, minShares *big.Int) (common.Hash, error) {
	state, err := LoadMarketState(ctx, rpcURL, marketAddress)
	if err != nil {
		return common.Hash{}, err
	}

	if state.Status != 0 {
		return common.Hash{}, fmt.Errorf("market is not active (status=%d)", state.Status)
	}
	if outcome >= state.OutcomeCount {
		return common.Hash{}, fmt.Errorf("outcome index %d is out of range for market with %d outcomes", outcome, state.OutcomeCount)
	}

	if state.SettlementToken == (common.Address{}) {
		data, err := marketABI.Pack("buy", outcome, amountWei, minShares)
		if err != nil {
			return common.Hash{}, fmt.Errorf("pack buy: %w", err)
		}
		return sendRawTransaction(ctx, rpcURL, chainID, privateKeyHex, marketAddress, data, amountWei)
	}

	approveData, err := erc20ABI.Pack("approve", marketAddress, amountWei)
	if err != nil {
		return common.Hash{}, fmt.Errorf("pack approve: %w", err)
	}
	if _, err := sendRawTransaction(ctx, rpcURL, chainID, privateKeyHex, state.SettlementToken, approveData, big.NewInt(0)); err != nil {
		return common.Hash{}, fmt.Errorf("approve settlement token: %w", err)
	}

	data, err := marketABI.Pack("buy", outcome, amountWei, minShares)
	if err != nil {
		return common.Hash{}, fmt.Errorf("pack buy: %w", err)
	}
	return sendRawTransaction(ctx, rpcURL, chainID, privateKeyHex, marketAddress, data, big.NewInt(0))
}

func WaitForReceipt(ctx context.Context, rpcURL string, txHash common.Hash) (*types.Receipt, error) {
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("connect rpc: %w", err)
	}
	defer client.Close()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		receipt, err := client.TransactionReceipt(ctx, txHash)
		if err == nil {
			return receipt, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func sendContractTransaction(ctx context.Context, rpcURL string, chainID *big.Int, privateKeyHex string, contractAddress common.Address, contractABI abi.ABI, method string, value *big.Int, args ...any) (common.Hash, error) {
	data, err := contractABI.Pack(method, args...)
	if err != nil {
		return common.Hash{}, fmt.Errorf("pack %s: %w", method, err)
	}
	return sendRawTransaction(ctx, rpcURL, chainID, privateKeyHex, contractAddress, data, value)
}

func sendRawTransaction(ctx context.Context, rpcURL string, chainID *big.Int, privateKeyHex string, to common.Address, data []byte, value *big.Int) (common.Hash, error) {
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return common.Hash{}, fmt.Errorf("connect rpc: %w", err)
	}
	defer client.Close()

	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(privateKeyHex, "0x"))
	if err != nil {
		return common.Hash{}, fmt.Errorf("parse private key: %w", err)
	}
	fromAddress := crypto.PubkeyToAddress(privateKey.PublicKey)

	nonce, err := client.PendingNonceAt(ctx, fromAddress)
	if err != nil {
		return common.Hash{}, fmt.Errorf("get nonce: %w", err)
	}
	gasPrice, err := client.SuggestGasPrice(ctx)
	if err != nil {
		return common.Hash{}, fmt.Errorf("suggest gas price: %w", err)
	}
	msg := ethereum.CallMsg{From: fromAddress, To: &to, Data: data, Value: value}
	gasLimit, err := client.EstimateGas(ctx, msg)
	if err != nil {
		return common.Hash{}, fmt.Errorf("estimate gas: %w", err)
	}

	tx := types.NewTransaction(nonce, to, value, gasLimit, gasPrice, data)
	signedTx, err := types.SignTx(tx, types.LatestSignerForChainID(chainID), privateKey)
	if err != nil {
		return common.Hash{}, fmt.Errorf("sign transaction: %w", err)
	}
	if err := client.SendTransaction(ctx, signedTx); err != nil {
		return common.Hash{}, fmt.Errorf("send transaction: %w", err)
	}
	return signedTx.Hash(), nil
}

func callAddress(ctx context.Context, client *ethclient.Client, contract common.Address, contractABI abi.ABI, method string, args ...any) (common.Address, error) {
	outputs, err := callContract(ctx, client, contract, contractABI, method, args...)
	if err != nil {
		return common.Address{}, err
	}
	if len(outputs) == 0 {
		return common.Address{}, fmt.Errorf("no output from %s", method)
	}
	result, ok := outputs[0].(common.Address)
	if !ok {
		return common.Address{}, fmt.Errorf("unexpected output type from %s", method)
	}
	return result, nil
}

func callUint8(ctx context.Context, client *ethclient.Client, contract common.Address, contractABI abi.ABI, method string, args ...any) (uint8, error) {
	outputs, err := callContract(ctx, client, contract, contractABI, method, args...)
	if err != nil {
		return 0, err
	}
	result, ok := outputs[0].(uint8)
	if !ok {
		return 0, fmt.Errorf("unexpected output type from %s", method)
	}
	return result, nil
}

func callBigInt(ctx context.Context, client *ethclient.Client, contract common.Address, contractABI abi.ABI, method string, args ...any) (*big.Int, error) {
	outputs, err := callContract(ctx, client, contract, contractABI, method, args...)
	if err != nil {
		return nil, err
	}
	result, ok := outputs[0].(*big.Int)
	if !ok {
		return nil, fmt.Errorf("unexpected output type from %s", method)
	}
	return result, nil
}

func callContract(ctx context.Context, client *ethclient.Client, contract common.Address, contractABI abi.ABI, method string, args ...any) ([]any, error) {
	data, err := contractABI.Pack(method, args...)
	if err != nil {
		return nil, fmt.Errorf("pack %s: %w", method, err)
	}
	result, err := client.CallContract(ctx, ethereum.CallMsg{To: &contract, Data: data}, nil)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", method, err)
	}
	outputs, err := contractABI.Unpack(method, result)
	if err != nil {
		return nil, fmt.Errorf("unpack %s: %w", method, err)
	}
	return outputs, nil
}

func PrivateKeyToAddress(privateKeyHex string) (common.Address, error) {
	privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(privateKeyHex, "0x"))
	if err != nil {
		return common.Address{}, fmt.Errorf("parse private key: %w", err)
	}
	return crypto.PubkeyToAddress(privateKey.PublicKey), nil
}

func AddressFromECDSA(privateKey *ecdsa.PrivateKey) common.Address {
	return crypto.PubkeyToAddress(privateKey.PublicKey)
}

func cloneBigInt(value *big.Int) *big.Int {
	if value == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(value)
}

func formatFixed18(value *big.Int, precision int) string {
	if value == nil {
		return "0"
	}
	sign := ""
	abs := cloneBigInt(value)
	if abs.Sign() < 0 {
		sign = "-"
		abs.Neg(abs)
	}
	base := big.NewInt(1_000_000_000_000_000_000)
	whole := new(big.Int).Quo(abs, base)
	fraction := new(big.Int).Mod(abs, base)
	if precision <= 0 {
		return sign + whole.String()
	}
	fractionText := fraction.Text(10)
	if len(fractionText) < 18 {
		fractionText = strings.Repeat("0", 18-len(fractionText)) + fractionText
	}
	if precision < len(fractionText) {
		fractionText = fractionText[:precision]
	}
	fractionText = strings.TrimRight(fractionText, "0")
	if fractionText == "" {
		return sign + whole.String()
	}
	return sign + whole.String() + "." + fractionText
}

func formatMultiplier(value *big.Int) string {
	return formatFixed18(value, 4) + "x"
}

func formatRatioPercent(numerator *big.Int, denominator *big.Int) string {
	if numerator == nil || denominator == nil || denominator.Sign() == 0 {
		return "—"
	}
	scaled := new(big.Int).Quo(new(big.Int).Mul(cloneBigInt(numerator), big.NewInt(1_000_000)), cloneBigInt(denominator))
	whole := new(big.Int).Quo(scaled, big.NewInt(10_000))
	fraction := new(big.Int).Mod(scaled, big.NewInt(10_000))
	fractionText := fmt.Sprintf("%04s", fraction.String())
	fractionText = strings.TrimRight(fractionText, "0")
	if fractionText == "" {
		return whole.String() + "%"
	}
	return whole.String() + "." + fractionText + "%"
}
