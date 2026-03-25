package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Gen3Games/axiom-cli/internal/api"
	"github.com/Gen3Games/axiom-cli/internal/evm"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

const clobMaxUint256 = "115792089237316195423570985008687907853269984665640564039457584007913129639935"

var (
	clobOrderSideValue = map[string]uint8{
		"buy":  0,
		"sell": 1,
	}
	clobOrderTypeValue = map[string]uint8{
		"limit":  0,
		"market": 1,
		"ioc":    2,
		"fok":    3,
	}
	clobExpiryDurations = map[string]time.Duration{
		"1h":  1 * time.Hour,
		"24h": 24 * time.Hour,
		"7d":  7 * 24 * time.Hour,
	}
)

type clobSelection struct {
	Binding             api.CtfOutcomeMarketBinding
	LogicalOutcome      api.Outcome
	DisplayedSide       string
	DisplayedTokenID    *big.Int
	DisplayedTokenIDRaw string
	CollateralToken     common.Address
	OutcomeToken        common.Address
	ExchangeAddress     common.Address
}

type clobWalletSideStatus struct {
	DisplayedSide string `json:"displayedSide"`
	IndexSet      int    `json:"indexSet"`
	TokenID       string `json:"tokenId"`
	Balance       string `json:"balance"`
}

type clobWalletBindingStatus struct {
	OutcomeIndex    int                    `json:"outcomeIndex"`
	OutcomeLabel    string                 `json:"outcomeLabel"`
	ContractAddress string                 `json:"contractAddress"`
	QuestionID      string                 `json:"questionId,omitempty"`
	ConditionID     string                 `json:"conditionId,omitempty"`
	Sides           []clobWalletSideStatus `json:"sides"`
}

type clobWalletStatus struct {
	WalletAddress          string                    `json:"walletAddress"`
	MarketID               string                    `json:"marketId"`
	MarketTitle            string                    `json:"marketTitle"`
	ExchangeAddress        string                    `json:"exchangeAddress"`
	CollateralToken        string                    `json:"collateralToken"`
	CollateralBalanceWei   string                    `json:"collateralBalanceWei"`
	CollateralBalanceXRP   string                    `json:"collateralBalanceXrp"`
	CollateralAllowanceWei string                    `json:"collateralAllowanceWei"`
	CollateralAllowanceXRP string                    `json:"collateralAllowanceXrp"`
	OutcomeToken           string                    `json:"outcomeToken"`
	OutcomeApprovalForAll  bool                      `json:"outcomeApprovalForAll"`
	Bindings               []clobWalletBindingStatus `json:"bindings"`
}

type clobRedemptionLeg struct {
	OutcomeIndex    int    `json:"outcomeIndex"`
	OutcomeLabel    string `json:"outcomeLabel"`
	DisplayedSide   string `json:"displayedSide"`
	ContractAddress string `json:"contractAddress"`
	TokenID         string `json:"tokenId"`
	Balance         string `json:"balance"`
	IndexSet        int    `json:"indexSet"`
}

func parseClobPriceToBps(value string) (int, error) {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, fmt.Errorf("parse price: %w", err)
	}
	bps := int(parsed * 100)
	if bps < 0 || bps > 9999 {
		return 0, errors.New("price must be between 0.00 and 99.99")
	}
	return bps, nil
}

func parseClobQuantity(value string) (int, error) {
	quantity, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("parse quantity: %w", err)
	}
	if quantity <= 0 {
		return 0, errors.New("quantity must be a positive whole number")
	}
	return quantity, nil
}

func resolveClobSelection(market *api.MarketDetails, outcomeRaw string, label string, displayedSideRaw string, exchangeRaw string, outcomeTokenRaw string) (*clobSelection, error) {
	logicalOutcome, err := resolveClobLogicalOutcome(market, outcomeRaw, label)
	if err != nil {
		return nil, err
	}
	bindings := sortedClobBindings(market.CTFOutcomeMarkets)
	if len(bindings) == 0 {
		return nil, errors.New("this CLOB market does not have grouped outcome bindings")
	}

	displayedSide := strings.ToLower(strings.TrimSpace(displayedSideRaw))
	if displayedSide != "" && displayedSide != "yes" && displayedSide != "no" {
		return nil, errors.New("--displayed-side must be either yes or no")
	}

	selectedBinding, resolvedSide, err := chooseClobBinding(market, bindings, logicalOutcome, displayedSide)
	if err != nil {
		return nil, err
	}

	collateralToken := resolveClobCollateralToken(market)
	outcomeToken := resolveHexAddressOrDefault(outcomeTokenRaw, evm.DefaultClobConditionalTokens)
	exchangeAddress := resolveHexAddressOrDefault(exchangeRaw, evm.DefaultClobExchangeAddress)
	displayedTokenID, displayedTokenIDRaw, err := resolveDisplayedTokenID(selectedBinding, resolvedSide, collateralToken)
	if err != nil {
		return nil, err
	}

	return &clobSelection{
		Binding:             selectedBinding,
		LogicalOutcome:      logicalOutcome,
		DisplayedSide:       resolvedSide,
		DisplayedTokenID:    displayedTokenID,
		DisplayedTokenIDRaw: displayedTokenIDRaw,
		CollateralToken:     collateralToken,
		OutcomeToken:        outcomeToken,
		ExchangeAddress:     exchangeAddress,
	}, nil
}

func resolveClobLogicalOutcome(market *api.MarketDetails, outcomeRaw string, label string) (api.Outcome, error) {
	if len(market.Outcomes) > 0 {
		index, err := resolveOutcomeIndex(market, outcomeRaw, label)
		if err != nil {
			return api.Outcome{}, err
		}
		for _, outcome := range market.Outcomes {
			if outcome.Index == index {
				return outcome, nil
			}
		}
	}

	bindings := sortedClobBindings(market.CTFOutcomeMarkets)
	if strings.TrimSpace(outcomeRaw) != "" {
		index, err := strconv.Atoi(strings.TrimSpace(outcomeRaw))
		if err != nil {
			return api.Outcome{}, fmt.Errorf("invalid outcome index: %w", err)
		}
		for _, binding := range bindings {
			if binding.OutcomeIndex == index {
				return api.Outcome{Index: binding.OutcomeIndex, Label: binding.Label}, nil
			}
		}
	}
	trimmedLabel := strings.TrimSpace(label)
	if trimmedLabel != "" {
		for _, binding := range bindings {
			if strings.EqualFold(strings.TrimSpace(binding.Label), trimmedLabel) {
				return api.Outcome{Index: binding.OutcomeIndex, Label: binding.Label}, nil
			}
		}
	}
	return api.Outcome{}, errors.New("either --outcome or --label is required")
}

func chooseClobBinding(market *api.MarketDetails, bindings []api.CtfOutcomeMarketBinding, outcome api.Outcome, displayedSide string) (api.CtfOutcomeMarketBinding, string, error) {
	if len(bindings) == 1 && len(market.Outcomes) == 2 {
		resolvedSide := displayedSide
		if resolvedSide == "" {
			if outcome.Index == 1 {
				resolvedSide = "no"
			} else {
				resolvedSide = "yes"
			}
		}
		return bindings[0], resolvedSide, nil
	}

	for _, binding := range bindings {
		if binding.OutcomeIndex == outcome.Index {
			if displayedSide == "" {
				displayedSide = "yes"
			}
			return binding, displayedSide, nil
		}
	}

	trimmedLabel := strings.TrimSpace(outcome.Label)
	for _, binding := range bindings {
		if strings.EqualFold(strings.TrimSpace(binding.Label), trimmedLabel) {
			if displayedSide == "" {
				displayedSide = "yes"
			}
			return binding, displayedSide, nil
		}
	}

	return api.CtfOutcomeMarketBinding{}, "", fmt.Errorf("no grouped CTF binding was found for outcome %q", outcome.Label)
}

func resolveClobCollateralToken(market *api.MarketDetails) common.Address {
	settlement := strings.TrimSpace(market.SettlementToken)
	if common.IsHexAddress(settlement) && !isZeroAddress(settlement) {
		return common.HexToAddress(settlement)
	}
	return common.HexToAddress(evm.DefaultClobCollateralToken)
}

func resolveHexAddressOrDefault(value string, defaultValue string) common.Address {
	trimmed := strings.TrimSpace(value)
	if common.IsHexAddress(trimmed) {
		return common.HexToAddress(trimmed)
	}
	return common.HexToAddress(defaultValue)
}

func loadMarketWithClobFallback(ctx context.Context, cliCtx *cliContext, marketRef string, instanceDate string) (*api.MarketDetails, error) {
	market, err := cliCtx.API.GetMarket(ctx, marketRef, instanceDate)
	if err != nil {
		return nil, err
	}
	return hydrateStandaloneClobMarket(ctx, cliCtx, market)
}

func hydrateStandaloneClobMarket(ctx context.Context, cliCtx *cliContext, market *api.MarketDetails) (*api.MarketDetails, error) {
	if market == nil {
		return nil, errors.New("market details are required")
	}
	if isClobMarketImplementation(market.MarketImplementation) || len(market.CTFOutcomeMarkets) > 0 {
		return market, nil
	}
	if !common.IsHexAddress(strings.TrimSpace(market.ContractAddress)) {
		return market, nil
	}
	if len(market.Outcomes) != 2 {
		return market, nil
	}

	metadata, err := evm.LoadCTFMarketMetadata(ctx, cliCtx.Config.EVMRPCURL, common.HexToAddress(market.ContractAddress))
	if err != nil {
		return market, nil
	}
	if metadata == nil || metadata.OutcomeSlotCount != 2 {
		return market, nil
	}

	market.MarketImplementation = "AxiomCTFMarket"
	if strings.TrimSpace(market.SettlementToken) == "" || isZeroAddress(market.SettlementToken) {
		market.SettlementToken = metadata.CollateralToken.Hex()
	}
	if strings.TrimSpace(market.Creator) == "" || isZeroAddress(market.Creator) {
		market.Creator = metadata.Creator.Hex()
	}
	if strings.TrimSpace(market.OwnerAddress) == "" || isZeroAddress(market.OwnerAddress) {
		market.OwnerAddress = metadata.Creator.Hex()
	}
	if strings.TrimSpace(market.MetadataURI) == "" {
		market.MetadataURI = metadata.MetadataURI
	}
	if len(market.LogicalMarketAddresses) == 0 {
		market.LogicalMarketAddresses = []string{common.HexToAddress(market.ContractAddress).Hex()}
	}

	yesTokenID := evm.ComputeCTFPositionID(metadata.CollateralToken, metadata.ConditionID, 0)
	noTokenID := evm.ComputeCTFPositionID(metadata.CollateralToken, metadata.ConditionID, 1)
	market.CTFOutcomeMarkets = []api.CtfOutcomeMarketBinding{
		{
			OutcomeID:       market.ID + ":0",
			OutcomeIndex:    0,
			Label:           market.Outcomes[0].Label,
			ContractAddress: common.HexToAddress(market.ContractAddress).Hex(),
			OutcomeTokenIDs: []string{yesTokenID.String(), noTokenID.String()},
			MetadataURI:     metadata.MetadataURI,
			QuestionID:      metadata.QuestionID.Hex(),
			ConditionID:     metadata.ConditionID.Hex(),
		},
	}
	return market, nil
}

func resolveDisplayedTokenID(binding api.CtfOutcomeMarketBinding, displayedSide string, collateralToken common.Address) (*big.Int, string, error) {
	index := 0
	if displayedSide == "no" {
		index = 1
	}
	if len(binding.OutcomeTokenIDs) > index {
		tokenID, err := evm.ParseBigInt(binding.OutcomeTokenIDs[index])
		if err == nil {
			return tokenID, tokenID.String(), nil
		}
	}
	conditionID := strings.TrimSpace(binding.ConditionID)
	if len(conditionID) == 66 && strings.HasPrefix(conditionID, "0x") {
		tokenID := evm.ComputeCTFPositionID(collateralToken, common.HexToHash(conditionID), uint8(index))
		return tokenID, tokenID.String(), nil
	}
	return nil, "", fmt.Errorf("missing usable %s token id for outcome %q", strings.ToUpper(displayedSide), binding.Label)
}

func buildClobSignedOrder(wallet *evm.Wallet, marketID string, selection *clobSelection, side string, orderType string, priceBps int, quantity int, expiryPreset string, chainID *big.Int) (api.ClobSignedOrderPayload, error) {
	sideValue, ok := clobOrderSideValue[side]
	if !ok {
		return api.ClobSignedOrderPayload{}, errors.New("--side must be buy or sell")
	}
	orderTypeValue, ok := clobOrderTypeValue[orderType]
	if !ok {
		return api.ClobSignedOrderPayload{}, errors.New("--type must be one of limit, market, ioc, fok")
	}
	if orderType != "market" && (priceBps <= 0 || priceBps > 9999) {
		return api.ClobSignedOrderPayload{}, errors.New("non-market orders require --price between 0.01 and 99.99")
	}
	makerAmount, takerAmount := buildClobOrderAmounts(side, priceBps, quantity)
	expiration := resolveClobExpiration(expiryPreset)
	nonce := time.Now().UnixMilli()*1000 + int64(time.Now().UnixNano()%1000)
	message := apitypes.TypedDataMessage{
		"maker":           wallet.Address().Hex(),
		"taker":           evm.ClobZeroAddress,
		"collateralToken": selection.CollateralToken.Hex(),
		"outcomeToken":    selection.OutcomeToken.Hex(),
		"outcomeTokenId":  selection.DisplayedTokenID.String(),
		"side":            strconv.FormatUint(uint64(sideValue), 10),
		"makerAmount":     makerAmount.String(),
		"takerAmount":     takerAmount.String(),
		"expiration":      strconv.FormatInt(expiration, 10),
		"nonce":           strconv.FormatInt(nonce, 10),
		"feeRateBps":      "0",
	}
	typedData := evm.BuildClobTypedData(chainID, selection.ExchangeAddress, message)
	signature, err := wallet.SignTypedData(typedData)
	if err != nil {
		return api.ClobSignedOrderPayload{}, err
	}

	return api.ClobSignedOrderPayload{
		Maker:           wallet.Address().Hex(),
		Taker:           evm.ClobZeroAddress,
		CollateralToken: selection.CollateralToken.Hex(),
		OutcomeToken:    selection.OutcomeToken.Hex(),
		OutcomeTokenID:  selection.DisplayedTokenIDRaw,
		Side:            sideValue,
		MakerAmount:     makerAmount.String(),
		TakerAmount:     takerAmount.String(),
		Expiration:      strconv.FormatInt(expiration, 10),
		Nonce:           strconv.FormatInt(nonce, 10),
		FeeRateBps:      "0",
		Signature:       signature,
		Market:          marketID,
		Outcome:         selection.Binding.OutcomeIndex,
		OrderType:       orderTypeValue,
	}, nil
}

func buildClobOrderAmounts(side string, priceBps int, quantity int) (*big.Int, *big.Int) {
	priceValue := int64(priceBps)
	if priceValue <= 0 {
		priceValue = 10000
	}
	quantityValue := int64(quantity)
	if side == "buy" {
		return big.NewInt(quantityValue * priceValue), big.NewInt(quantityValue * 10000)
	}
	return big.NewInt(quantityValue * 10000), big.NewInt(quantityValue * priceValue)
}

func resolveClobExpiration(preset string) int64 {
	trimmed := strings.TrimSpace(strings.ToLower(preset))
	if trimmed == "" || trimmed == "never" {
		return 0
	}
	if duration, ok := clobExpiryDurations[trimmed]; ok {
		return time.Now().Add(duration).Unix()
	}
	return time.Now().Add(24 * time.Hour).Unix()
}

func describeClobOrderResult(orderType string, response *api.ClobOrderResponse) string {
	if response == nil {
		return "Submitted to the CLOB."
	}
	if response.WasAddedToBook && response.TradeCount > 0 {
		return "Partially matched and resting on the book."
	}
	if response.WasAddedToBook {
		return "Signed successfully and resting on the book."
	}
	if orderType == "market" && response.TradeCount > 0 && response.RemainingQuantity > 0 {
		return "Partially matched. The unfilled balance was canceled."
	}
	if response.TradeCount > 0 && response.RemainingQuantity == 0 {
		if orderType == "market" {
			return "Filled from resting liquidity."
		}
		return "Matched immediately."
	}
	if orderType == "market" {
		return "No resting liquidity was available to fill this market order."
	}
	return "Submitted to the CLOB."
}

func buildClobWalletStatus(ctx context.Context, cliCtx *cliContext, market *api.MarketDetails, walletAddress common.Address, exchangeAddress common.Address, outcomeToken common.Address) (*clobWalletStatus, error) {
	collateralToken := resolveClobCollateralToken(market)
	collateralBalance, err := getERC20Balance(ctx, cliCtx.Config.EVMRPCURL, collateralToken, walletAddress)
	if err != nil {
		return nil, err
	}
	collateralAllowance, err := getERC20Allowance(ctx, cliCtx.Config.EVMRPCURL, collateralToken, walletAddress, exchangeAddress)
	if err != nil {
		return nil, err
	}
	approvedForAll, err := isERC1155ApprovedForAll(ctx, cliCtx.Config.EVMRPCURL, outcomeToken, walletAddress, exchangeAddress)
	if err != nil {
		return nil, err
	}

	bindings := sortedClobBindings(market.CTFOutcomeMarkets)
	statusBindings := make([]clobWalletBindingStatus, 0, len(bindings))
	for _, binding := range bindings {
		sides := make([]clobWalletSideStatus, 0, 2)
		for _, displayedSide := range []string{"yes", "no"} {
			tokenID, tokenIDRaw, resolveErr := resolveDisplayedTokenID(binding, displayedSide, collateralToken)
			if resolveErr != nil {
				continue
			}
			balance, balanceErr := getERC1155Balance(ctx, cliCtx.Config.EVMRPCURL, outcomeToken, walletAddress, tokenID)
			if balanceErr != nil {
				return nil, balanceErr
			}
			sides = append(sides, clobWalletSideStatus{
				DisplayedSide: displayedSide,
				IndexSet:      clobDisplayedSideToIndexSet(displayedSide),
				TokenID:       tokenIDRaw,
				Balance:       balance.String(),
			})
		}
		statusBindings = append(statusBindings, clobWalletBindingStatus{
			OutcomeIndex:    binding.OutcomeIndex,
			OutcomeLabel:    binding.Label,
			ContractAddress: binding.ContractAddress,
			QuestionID:      binding.QuestionID,
			ConditionID:     binding.ConditionID,
			Sides:           sides,
		})
	}

	return &clobWalletStatus{
		WalletAddress:          walletAddress.Hex(),
		MarketID:               market.ID,
		MarketTitle:            market.Title,
		ExchangeAddress:        exchangeAddress.Hex(),
		CollateralToken:        collateralToken.Hex(),
		CollateralBalanceWei:   collateralBalance.String(),
		CollateralBalanceXRP:   formatWeiToXRP(collateralBalance),
		CollateralAllowanceWei: collateralAllowance.String(),
		CollateralAllowanceXRP: formatWeiToXRP(collateralAllowance),
		OutcomeToken:           outcomeToken.Hex(),
		OutcomeApprovalForAll:  approvedForAll,
		Bindings:               statusBindings,
	}, nil
}

func buildClobRedemptionPlan(ctx context.Context, cliCtx *cliContext, market *api.MarketDetails, walletAddress common.Address, outcomeToken common.Address) ([]clobRedemptionLeg, error) {
	collateralToken := resolveClobCollateralToken(market)
	bindings := sortedClobBindings(market.CTFOutcomeMarkets)
	legs := make([]clobRedemptionLeg, 0)
	for _, binding := range bindings {
		for _, displayedSide := range []string{"yes", "no"} {
			tokenID, tokenIDRaw, err := resolveDisplayedTokenID(binding, displayedSide, collateralToken)
			if err != nil {
				continue
			}
			balance, err := getERC1155Balance(ctx, cliCtx.Config.EVMRPCURL, outcomeToken, walletAddress, tokenID)
			if err != nil {
				return nil, err
			}
			if balance.Sign() <= 0 {
				continue
			}
			legs = append(legs, clobRedemptionLeg{
				OutcomeIndex:    binding.OutcomeIndex,
				OutcomeLabel:    binding.Label,
				DisplayedSide:   displayedSide,
				ContractAddress: binding.ContractAddress,
				TokenID:         tokenIDRaw,
				Balance:         balance.String(),
				IndexSet:        clobDisplayedSideToIndexSet(displayedSide),
			})
		}
	}
	return legs, nil
}

func clobDisplayedSideToIndexSet(displayedSide string) int {
	if strings.EqualFold(strings.TrimSpace(displayedSide), "no") {
		return 2
	}
	return 1
}

func sortedClobBindings(bindings []api.CtfOutcomeMarketBinding) []api.CtfOutcomeMarketBinding {
	ordered := append([]api.CtfOutcomeMarketBinding(nil), bindings...)
	sort.Slice(ordered, func(left int, right int) bool {
		if ordered[left].OutcomeIndex == ordered[right].OutcomeIndex {
			return strings.ToLower(ordered[left].Label) < strings.ToLower(ordered[right].Label)
		}
		return ordered[left].OutcomeIndex < ordered[right].OutcomeIndex
	})
	return ordered
}

func isZeroAddress(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), evm.ClobZeroAddress)
}
