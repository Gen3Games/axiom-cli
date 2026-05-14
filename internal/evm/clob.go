package evm

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	ethmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

const (
	ClobDomainName                     = "Axiom CLOB"
	ClobDomainVersion                  = "1"
	DefaultClobChainID           int64 = 1440000
	DefaultClobDomainContract          = "0xa232ACB932b4E745f6ee2aaC1E2707ae0E1055c5"
	DefaultClobExchangeAddress         = DefaultClobDomainContract
	DefaultClobConditionalTokens       = "0x43e3fa6De5D87dd7265053FA55601d1972984edA"
	DefaultClobCollateralToken         = "0xEeeeeEeeeEeEeeEeEeEeeEEEeeeeEeeeeeeeEEeE"
	ClobZeroAddress                    = "0x0000000000000000000000000000000000000000"
)

const erc1155ABIJSON = `[
	{"name":"balanceOf","type":"function","stateMutability":"view","inputs":[{"name":"account","type":"address"},{"name":"id","type":"uint256"}],"outputs":[{"type":"uint256"}]},
	{"name":"balanceOfBatch","type":"function","stateMutability":"view","inputs":[{"name":"accounts","type":"address[]"},{"name":"ids","type":"uint256[]"}],"outputs":[{"type":"uint256[]"}]},
	{"name":"isApprovedForAll","type":"function","stateMutability":"view","inputs":[{"name":"account","type":"address"},{"name":"operator","type":"address"}],"outputs":[{"type":"bool"}]},
	{"name":"setApprovalForAll","type":"function","stateMutability":"nonpayable","inputs":[{"name":"operator","type":"address"},{"name":"approved","type":"bool"}],"outputs":[]}
]`

const conditionalTokensABIJSON = `[
	{"name":"splitPosition","type":"function","stateMutability":"nonpayable","inputs":[{"name":"collateralToken","type":"address"},{"name":"parentCollectionId","type":"bytes32"},{"name":"conditionId","type":"bytes32"},{"name":"partition","type":"uint256[]"},{"name":"amount","type":"uint256"}],"outputs":[]},
	{"name":"mergePositions","type":"function","stateMutability":"nonpayable","inputs":[{"name":"collateralToken","type":"address"},{"name":"parentCollectionId","type":"bytes32"},{"name":"conditionId","type":"bytes32"},{"name":"partition","type":"uint256[]"},{"name":"amount","type":"uint256"}],"outputs":[]}
]`

var erc1155ABI = mustParseABI(erc1155ABIJSON)
var conditionalTokensABI = mustParseABI(conditionalTokensABIJSON)

func BuildClobTypedData(domainChainID *big.Int, verifyingContract common.Address, order apitypes.TypedDataMessage) apitypes.TypedData {
	chainID := cloneBigInt(domainChainID)
	if chainID.Sign() == 0 {
		chainID = big.NewInt(DefaultClobChainID)
	}

	return apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": []apitypes.Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"Order": []apitypes.Type{
				{Name: "maker", Type: "address"},
				{Name: "taker", Type: "address"},
				{Name: "collateralToken", Type: "address"},
				{Name: "outcomeToken", Type: "address"},
				{Name: "outcomeTokenId", Type: "uint256"},
				{Name: "side", Type: "uint8"},
				{Name: "makerAmount", Type: "uint256"},
				{Name: "takerAmount", Type: "uint256"},
				{Name: "expiration", Type: "uint256"},
				{Name: "nonce", Type: "uint256"},
				{Name: "feeRateBps", Type: "uint256"},
			},
		},
		PrimaryType: "Order",
		Domain: apitypes.TypedDataDomain{
			Name:              ClobDomainName,
			Version:           ClobDomainVersion,
			ChainId:           (*ethmath.HexOrDecimal256)(chainID),
			VerifyingContract: verifyingContract.Hex(),
		},
		Message: order,
	}
}

func ComputeCTFPositionID(collateralToken common.Address, conditionID common.Hash, outcomeIndex uint8) *big.Int {
	indexSet := new(big.Int).Lsh(big.NewInt(1), uint(outcomeIndex))
	collectionPayload := make([]byte, 0, 96)
	collectionPayload = append(collectionPayload, make([]byte, 32)...)
	collectionPayload = append(collectionPayload, conditionID.Bytes()...)
	collectionPayload = append(collectionPayload, common.LeftPadBytes(indexSet.Bytes(), 32)...)
	collectionID := crypto.Keccak256Hash(collectionPayload)

	positionPayload := make([]byte, 0, 52)
	positionPayload = append(positionPayload, collateralToken.Bytes()...)
	positionPayload = append(positionPayload, collectionID.Bytes()...)
	positionID := crypto.Keccak256Hash(positionPayload)
	return new(big.Int).SetBytes(positionID.Bytes())
}

func GetERC20Balance(ctx context.Context, rpcURL string, token common.Address, owner common.Address) (*big.Int, error) {
	client, err := dialClient(ctx, rpcURL)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	return callBigInt(ctx, client, token, erc20ABI, "balanceOf", owner)
}

func GetERC20Allowance(ctx context.Context, rpcURL string, token common.Address, owner common.Address, spender common.Address) (*big.Int, error) {
	client, err := dialClient(ctx, rpcURL)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	return callBigInt(ctx, client, token, erc20ABI, "allowance", owner, spender)
}

func ApproveERC20(ctx context.Context, rpcURL string, chainID *big.Int, privateKeyHex string, token common.Address, spender common.Address, amount *big.Int) (common.Hash, error) {
	return sendContractTransaction(ctx, rpcURL, chainID, privateKeyHex, token, erc20ABI, "approve", big.NewInt(0), spender, amount)
}

func GetERC1155Balance(ctx context.Context, rpcURL string, token common.Address, owner common.Address, tokenID *big.Int) (*big.Int, error) {
	client, err := dialClient(ctx, rpcURL)
	if err != nil {
		return nil, err
	}
	defer client.Close()
	return callBigInt(ctx, client, token, erc1155ABI, "balanceOf", owner, tokenID)
}

func GetERC1155Balances(ctx context.Context, rpcURL string, token common.Address, owner common.Address, tokenIDs []*big.Int) ([]*big.Int, error) {
	if len(tokenIDs) == 0 {
		return nil, nil
	}
	client, err := dialClient(ctx, rpcURL)
	if err != nil {
		return nil, err
	}
	defer client.Close()

	accounts := make([]common.Address, 0, len(tokenIDs))
	for range tokenIDs {
		accounts = append(accounts, owner)
	}
	outputs, err := callContract(ctx, client, token, erc1155ABI, "balanceOfBatch", accounts, tokenIDs)
	if err != nil {
		return nil, err
	}
	balances, ok := outputs[0].([]*big.Int)
	if !ok {
		return nil, fmt.Errorf("unexpected output type from balanceOfBatch")
	}
	return balances, nil
}

func IsERC1155ApprovedForAll(ctx context.Context, rpcURL string, token common.Address, owner common.Address, operator common.Address) (bool, error) {
	client, err := dialClient(ctx, rpcURL)
	if err != nil {
		return false, err
	}
	defer client.Close()
	outputs, err := callContract(ctx, client, token, erc1155ABI, "isApprovedForAll", owner, operator)
	if err != nil {
		return false, err
	}
	approved, ok := outputs[0].(bool)
	if !ok {
		return false, fmt.Errorf("unexpected output type from isApprovedForAll")
	}
	return approved, nil
}

func SetERC1155ApprovalForAll(ctx context.Context, rpcURL string, chainID *big.Int, privateKeyHex string, token common.Address, operator common.Address, approved bool) (common.Hash, error) {
	return sendContractTransaction(ctx, rpcURL, chainID, privateKeyHex, token, erc1155ABI, "setApprovalForAll", big.NewInt(0), operator, approved)
}

func RedeemCTFMarket(ctx context.Context, rpcURL string, chainID *big.Int, privateKeyHex string, market common.Address, indexSets []*big.Int) (common.Hash, error) {
	return sendContractTransaction(ctx, rpcURL, chainID, privateKeyHex, market, marketABI, "redeemWithFees", big.NewInt(0), indexSets)
}

func ParseBigInt(value string) (*big.Int, error) {
	parsed := new(big.Int)
	if _, ok := parsed.SetString(strings.TrimSpace(value), 10); !ok {
		return nil, fmt.Errorf("invalid integer value: %s", value)
	}
	return parsed, nil
}

func SplitPosition(ctx context.Context, rpcURL string, chainID *big.Int, privateKeyHex string, conditionalTokens common.Address, collateralToken common.Address, conditionID common.Hash, partition []*big.Int, amount *big.Int) (common.Hash, error) {
	parentCollectionID := common.Hash{}
	return sendContractTransaction(ctx, rpcURL, chainID, privateKeyHex, conditionalTokens, conditionalTokensABI, "splitPosition", big.NewInt(0), collateralToken, parentCollectionID, conditionID, partition, amount)
}

func MergePositions(ctx context.Context, rpcURL string, chainID *big.Int, privateKeyHex string, conditionalTokens common.Address, collateralToken common.Address, conditionID common.Hash, partition []*big.Int, amount *big.Int) (common.Hash, error) {
	parentCollectionID := common.Hash{}
	return sendContractTransaction(ctx, rpcURL, chainID, privateKeyHex, conditionalTokens, conditionalTokensABI, "mergePositions", big.NewInt(0), collateralToken, parentCollectionID, conditionID, partition, amount)
}

func dialClient(ctx context.Context, rpcURL string) (*ethclient.Client, error) {
	client, err := ethclient.DialContext(ctx, rpcURL)
	if err != nil {
		return nil, fmt.Errorf("connect rpc: %w", err)
	}
	return client, nil
}
