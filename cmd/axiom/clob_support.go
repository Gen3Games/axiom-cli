package main

import (
	"context"
	"encoding/json"
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
	ethmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/spf13/cobra"
)

const clobMaxUint256 = "115792089237316195423570985008687907853269984665640564039457584007913129639935"

var (
	clobOrderSideValue = map[string]uint8{
		"buy":  0,
		"sell": 1,
	}
	clobTokenSideValue = map[string]string{
		"yes": "yes",
		"no":  "no",
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

type clobSigningDomain struct {
	ChainID           *big.Int
	VerifyingContract common.Address
}

func normalizeClobTokenSide(value string) (string, error) {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return "yes", nil
	}
	normalized, ok := clobTokenSideValue[trimmed]
	if !ok {
		return "", errors.New("token side must be yes or no")
	}
	return normalized, nil
}

func clobIDForMarketOutcome(marketID string, outcome int, tokenSide string) string {
	normalized, err := normalizeClobTokenSide(tokenSide)
	if err != nil {
		normalized = "yes"
	}
	return fmt.Sprintf("%s-%d-%s", marketID, outcome, normalized)
}

type logicalRegisterOutcome struct {
	Key         string
	Label       string
	Description string
	MetadataURI string
	QuestionID  common.Hash
}

type logicalLaunchOutcome struct {
	Key                 string
	Label               string
	Description         string
	MetadataName        string
	MetadataDescription string
	QuestionID          common.Hash
	MetadataURI         string
}

type logicalCreateMarketPlan struct {
	MarketID            string
	Name                string
	Headline            string
	Description         string
	Category            string
	Tags                []string
	EvidenceSources     []string
	Image               string
	MarketType          string
	ResolutionCriteria  string
	StartsAt            time.Time
	EndsAt              time.Time
	ResolveBy           time.Time
	DisplayOutcomes     []logicalRegisterOutcome
	LaunchOutcomes      []logicalLaunchOutcome
	LogicalMarketIDHash common.Hash
}

type logicalOutcomeJSONInput struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	MetadataURI string `json:"metadataUri"`
	QuestionID  string `json:"questionId"`
	Launch      *bool  `json:"launch"`
}

func normalizeLogicalQuestionID(label string, value string, fallback string) (common.Hash, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return common.BytesToHash(crypto.Keccak256([]byte(fallback))), nil
	}
	if len(trimmed) != 66 || !strings.HasPrefix(trimmed, "0x") {
		return common.Hash{}, fmt.Errorf("questionId for outcome %q must be a 32-byte 0x-prefixed value", label)
	}
	questionID := common.HexToHash(trimmed)
	if questionID == (common.Hash{}) {
		return common.Hash{}, fmt.Errorf("questionId for outcome %q must not be zero", label)
	}
	return questionID, nil
}

func collectLogicalEvidenceSources(cmd *cobra.Command) []string {
	values := mustStringSliceFlag(cmd, "evidence-source")
	results := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				results = append(results, trimmed)
			}
		}
	}
	return results
}

func parseLogicalOutcomeJSONInputs(raw string, marketType string) ([]logicalRegisterOutcome, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, nil
	}
	var inputs []logicalOutcomeJSONInput
	if err := json.Unmarshal([]byte(trimmed), &inputs); err != nil {
		return nil, fmt.Errorf("parse --outcomes-json: %w", err)
	}
	if len(inputs) == 0 {
		return nil, errors.New("--outcomes-json must contain at least one outcome object")
	}
	results := make([]logicalRegisterOutcome, 0, len(inputs))
	seenKeys := make(map[string]struct{}, len(inputs))
	for index, input := range inputs {
		label := strings.TrimSpace(input.Label)
		if label == "" {
			return nil, fmt.Errorf("outcomes[%d].label is required", index)
		}
		key := normalizeLogicalKey(input.Key, normalizeLogicalKey(label, fmt.Sprintf("outcome-%d", index)))
		if _, ok := seenKeys[key]; ok {
			return nil, fmt.Errorf("duplicate logical outcome key %q in --outcomes-json", key)
		}
		seenKeys[key] = struct{}{}
		questionID, err := normalizeLogicalQuestionID(label, input.QuestionID, fmt.Sprintf("%s:%s", marketType, key))
		if err != nil {
			return nil, err
		}
		results = append(results, logicalRegisterOutcome{
			Key:         key,
			Label:       label,
			Description: strings.TrimSpace(input.Description),
			MetadataURI: strings.TrimSpace(input.MetadataURI),
			QuestionID:  questionID,
		})
	}
	return results, nil
}

type logicalBindingResolution struct {
	Binding api.CtfOutcomeMarketBinding
	Payouts []*big.Int
	Won     bool
}

type logicalUpdateInput struct {
	Name        *string
	Headline    *string
	Description *string
	Category    *string
	ImageURL    *string
	Tags        []string
}

type logicalBookClosure struct {
	OutcomeIndex int    `json:"outcomeIndex"`
	TokenSide    string `json:"tokenSide"`
	Status       string `json:"status,omitempty"`
	Message      string `json:"message,omitempty"`
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

type clobSplitStatusSummary struct {
	MaxSplitWei           *big.Int
	MaxMergeableWei       *big.Int
	SplitApproved         bool
	SplitReady            bool
	MergeReady            bool
	MergeApprovalRequired bool
}

func buildClobApprovalStatus(status *clobWalletStatus) (map[string]any, error) {
	if status == nil {
		return nil, errors.New("wallet status is required")
	}

	collateralBalance, err := evm.ParseBigInt(status.CollateralBalanceWei)
	if err != nil {
		return nil, fmt.Errorf("parse collateral balance: %w", err)
	}
	collateralAllowance, err := evm.ParseBigInt(status.CollateralAllowanceWei)
	if err != nil {
		return nil, fmt.Errorf("parse collateral allowance: %w", err)
	}

	return map[string]any{
		"exchangeAddress":        status.ExchangeAddress,
		"collateralToken":        status.CollateralToken,
		"collateralBalanceWei":   collateralBalance.String(),
		"collateralBalanceXrp":   formatWeiToXRP(collateralBalance),
		"collateralAllowanceWei": collateralAllowance.String(),
		"collateralAllowanceXrp": formatWeiToXRP(collateralAllowance),
		"outcomeToken":           status.OutcomeToken,
		"outcomeApprovalForAll":  status.OutcomeApprovalForAll,
		"ready": map[string]any{
			"collateralApproval": collateralAllowance.Sign() > 0,
			"outcomeApproval":    status.OutcomeApprovalForAll,
		},
	}, nil
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

func resolveClobReadSelection(ctx context.Context, cliCtx *cliContext, cmd *cobra.Command, marketRef string) (*api.MarketDetails, *clobSelection, error) {
	market, err := loadMarketWithClobFallback(ctx, cliCtx, marketRef, mustStringFlag(cmd, "instance-date"))
	if err != nil {
		return nil, nil, err
	}
	if !isClobMarketImplementation(market.MarketImplementation) {
		return nil, nil, errors.New("this command requires an AxiomCTFMarket logical market")
	}

	label := strings.TrimSpace(mustStringFlag(cmd, "label"))
	outcomeRaw := ""
	if cmd.Flags().Changed("outcome") || label == "" {
		outcome, err := cmd.Flags().GetInt("outcome")
		if err != nil {
			return nil, nil, err
		}
		outcomeRaw = strconv.Itoa(outcome)
	}

	displayedSide := ""
	if cmd.Flags().Changed("token-side") {
		displayedSide = mustStringFlag(cmd, "token-side")
	}

	selection, err := resolveClobSelection(market, outcomeRaw, label, displayedSide, "", "")
	if err != nil {
		return nil, nil, err
	}
	return market, selection, nil
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

func loadMMMarket(ctx context.Context, cliCtx *cliContext, marketRef string, instanceDate string) (*api.MarketDetails, error) {
	market, err := cliCtx.ConsoleAPI.GetMarket(ctx, marketRef, instanceDate)
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

	metadata, err := loadCTFMarketMetadata(ctx, cliCtx.Config.EVMRPCURL, common.HexToAddress(market.ContractAddress))
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
			OutcomeID:         market.ID + ":0",
			OutcomeIndex:      0,
			Label:             market.Outcomes[0].Label,
			ContractAddress:   common.HexToAddress(market.ContractAddress).Hex(),
			ConditionalTokens: metadata.ConditionalTokens.Hex(),
			OutcomeTokenIDs:   []string{yesTokenID.String(), noTokenID.String()},
			MetadataURI:       metadata.MetadataURI,
			QuestionID:        metadata.QuestionID.Hex(),
			ConditionID:       metadata.ConditionID.Hex(),
		},
	}
	return market, nil
}

func resolveClobBindingOutcomeToken(ctx context.Context, cliCtx *cliContext, binding api.CtfOutcomeMarketBinding) (common.Address, error) {
	if common.IsHexAddress(strings.TrimSpace(binding.ConditionalTokens)) && !isZeroAddress(binding.ConditionalTokens) {
		return common.HexToAddress(binding.ConditionalTokens), nil
	}
	if !common.IsHexAddress(strings.TrimSpace(binding.ContractAddress)) {
		return common.Address{}, fmt.Errorf("binding %q has no usable contract address", binding.Label)
	}
	metadata, err := loadCTFMarketMetadata(ctx, cliCtx.Config.EVMRPCURL, common.HexToAddress(binding.ContractAddress))
	if err != nil {
		return common.Address{}, err
	}
	if metadata == nil || metadata.ConditionalTokens == (common.Address{}) {
		return common.Address{}, fmt.Errorf("binding %q did not expose a usable conditional tokens contract", binding.Label)
	}
	return metadata.ConditionalTokens, nil
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

func buildClobSignedOrder(wallet *evm.Wallet, marketID string, selection *clobSelection, side string, orderType string, priceBps int, quantity int, expiryPreset string, domain clobSigningDomain) (api.ClobSignedOrderPayload, error) {
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
	typedData := evm.BuildClobTypedData(domain.ChainID, domain.VerifyingContract, message)
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
		TokenSide:       selection.DisplayedSide,
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

func resolveClobSigningDomain(cmd *cobra.Command) (clobSigningDomain, error) {
	chainIDValue := strings.TrimSpace(mustStringFlag(cmd, "clob-chain-id"))
	if chainIDValue == "" {
		chainIDValue = strconv.FormatInt(evm.DefaultClobChainID, 10)
	}
	chainID, ok := new(big.Int).SetString(chainIDValue, 10)
	if !ok || chainID.Sign() <= 0 {
		return clobSigningDomain{}, errors.New("--clob-chain-id must be a positive integer")
	}

	verifyingContractRaw := strings.TrimSpace(mustStringFlag(cmd, "clob-domain-contract"))
	if !common.IsHexAddress(verifyingContractRaw) || isZeroAddress(verifyingContractRaw) {
		return clobSigningDomain{}, errors.New("--clob-domain-contract must be a valid non-zero 0x-prefixed address")
	}

	return clobSigningDomain{
		ChainID:           chainID,
		VerifyingContract: common.HexToAddress(verifyingContractRaw),
	}, nil
}

func buildCreateBookSignature(wallet *evm.Wallet, domain clobSigningDomain, marketID string, outcome int) (string, error) {
	message := apitypes.TypedDataMessage{
		"creator": wallet.Address().Hex(),
		"market":  marketID,
		"outcome": strconv.Itoa(outcome),
	}
	typedData := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": []apitypes.Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"CreateBook": []apitypes.Type{
				{Name: "creator", Type: "address"},
				{Name: "market", Type: "string"},
				{Name: "outcome", Type: "uint8"},
			},
		},
		PrimaryType: "CreateBook",
		Domain: apitypes.TypedDataDomain{
			Name:              evm.ClobDomainName,
			Version:           evm.ClobDomainVersion,
			ChainId:           (*ethmath.HexOrDecimal256)(domain.ChainID),
			VerifyingContract: domain.VerifyingContract.Hex(),
		},
		Message: message,
	}
	return wallet.SignTypedData(typedData)
}

func buildCancelNonce() string {
	now := time.Now()
	nonce := now.UnixMilli()*1_000_000 + int64(now.UnixNano()%1_000_000)
	return strconv.FormatInt(nonce, 10)
}

func buildCancelDeadline(validFor time.Duration) string {
	if validFor <= 0 {
		validFor = 5 * time.Minute
	}
	return strconv.FormatInt(time.Now().Add(validFor).Unix(), 10)
}

func buildSignedClobCancel(wallet *evm.Wallet, domain clobSigningDomain, orderID string, marketID string, outcome int, tokenSide string, requester string, reason string) (api.ClobCancelOrderRequest, error) {
	trimmedOrderID := strings.TrimSpace(orderID)
	if trimmedOrderID == "" {
		return api.ClobCancelOrderRequest{}, errors.New("order id is required")
	}
	trimmedMarketID := strings.TrimSpace(marketID)
	if trimmedMarketID == "" {
		return api.ClobCancelOrderRequest{}, errors.New("market id is required")
	}
	normalizedTokenSide, ok := clobTokenSideValue[strings.ToLower(strings.TrimSpace(tokenSide))]
	if !ok {
		return api.ClobCancelOrderRequest{}, errors.New("token side must be yes or no")
	}
	trimmedRequester := strings.TrimSpace(requester)
	if !common.IsHexAddress(trimmedRequester) || isZeroAddress(trimmedRequester) {
		return api.ClobCancelOrderRequest{}, errors.New("requester must be a valid non-zero 0x-prefixed address")
	}
	nonce := buildCancelNonce()
	deadline := buildCancelDeadline(5 * time.Minute)
	message := apitypes.TypedDataMessage{
		"orderID":   trimmedOrderID,
		"market":    trimmedMarketID,
		"outcome":   strconv.Itoa(outcome),
		"tokenSide": normalizedTokenSide,
		"requester": common.HexToAddress(trimmedRequester).Hex(),
		"nonce":     nonce,
		"deadline":  deadline,
	}
	typedData := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": []apitypes.Type{
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"CancelOrder": []apitypes.Type{
				{Name: "orderID", Type: "string"},
				{Name: "market", Type: "string"},
				{Name: "outcome", Type: "uint8"},
				{Name: "tokenSide", Type: "string"},
				{Name: "requester", Type: "address"},
				{Name: "nonce", Type: "uint256"},
				{Name: "deadline", Type: "uint256"},
			},
		},
		PrimaryType: "CancelOrder",
		Domain: apitypes.TypedDataDomain{
			Name:              evm.ClobDomainName,
			Version:           evm.ClobDomainVersion,
			ChainId:           (*ethmath.HexOrDecimal256)(domain.ChainID),
			VerifyingContract: domain.VerifyingContract.Hex(),
		},
		Message: message,
	}
	signature, err := wallet.SignTypedData(typedData)
	if err != nil {
		return api.ClobCancelOrderRequest{}, err
	}
	request := api.ClobCancelOrderRequest{
		Market:    trimmedMarketID,
		Outcome:   outcome,
		TokenSide: normalizedTokenSide,
		Requester: common.HexToAddress(trimmedRequester).Hex(),
		Nonce:     nonce,
		Deadline:  deadline,
		Signature: signature,
	}
	if trimmedReason := strings.TrimSpace(reason); trimmedReason != "" {
		request.Reason = trimmedReason
	}
	return request, nil
}

func normalizeLogicalKey(label string, fallback string) string {
	trimmed := strings.TrimSpace(strings.ToLower(label))
	if trimmed == "" {
		trimmed = fallback
	}
	var builder strings.Builder
	lastDash := false
	for _, r := range trimmed {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			builder.WriteRune(r)
			lastDash = false
		case !lastDash:
			builder.WriteByte('-')
			lastDash = true
		}
	}
	result := strings.Trim(builder.String(), "-")
	if result == "" {
		return fallback
	}
	return result
}

func parseLogicalOutcomeLabels(values []string) []string {
	labels := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed != "" {
				labels = append(labels, trimmed)
			}
		}
	}
	return labels
}

func collectLogicalDisplayOutcomes(cmd *cobra.Command, marketType string) ([]logicalRegisterOutcome, error) {
	if parsed, err := parseLogicalOutcomeJSONInputs(mustStringFlag(cmd, "outcomes-json"), marketType); err != nil {
		return nil, err
	} else if len(parsed) > 0 {
		if marketType == "yes_no" && len(parsed) != 2 {
			return nil, errors.New("yes_no logical markets require exactly 2 outcomes in --outcomes-json")
		}
		if marketType == "multiple_choice" && len(parsed) < 2 {
			return nil, errors.New("multiple_choice logical markets require at least two outcomes in --outcomes-json")
		}
		return parsed, nil
	}

	if marketType == "yes_no" {
		yesLabel := strings.TrimSpace(mustStringFlag(cmd, "yes-label"))
		if yesLabel == "" {
			yesLabel = "Yes"
		}
		noLabel := strings.TrimSpace(mustStringFlag(cmd, "no-label"))
		if noLabel == "" {
			noLabel = "No"
		}
		yesQuestionID, err := normalizeLogicalQuestionID(yesLabel, mustStringFlag(cmd, "yes-question-id"), "yes_no:yes")
		if err != nil {
			return nil, err
		}
		noQuestionID, err := normalizeLogicalQuestionID(noLabel, mustStringFlag(cmd, "no-question-id"), "yes_no:no")
		if err != nil {
			return nil, err
		}
		return []logicalRegisterOutcome{
			{
				Key:         "yes",
				Label:       yesLabel,
				Description: strings.TrimSpace(mustStringFlag(cmd, "yes-description")),
				MetadataURI: strings.TrimSpace(mustStringFlag(cmd, "yes-metadata-uri")),
				QuestionID:  yesQuestionID,
			},
			{
				Key:         "no",
				Label:       noLabel,
				Description: strings.TrimSpace(mustStringFlag(cmd, "no-description")),
				MetadataURI: strings.TrimSpace(mustStringFlag(cmd, "no-metadata-uri")),
				QuestionID:  noQuestionID,
			},
		}, nil
	}

	labels := parseLogicalOutcomeLabels(mustStringSliceFlag(cmd, "outcome-label"))
	if len(labels) < 2 {
		return nil, errors.New("multiple_choice logical markets require at least two --outcome-label values")
	}
	results := make([]logicalRegisterOutcome, 0, len(labels))
	for index, label := range labels {
		questionID, err := normalizeLogicalQuestionID(label, "", fmt.Sprintf("multiple_choice:%s", normalizeLogicalKey(label, fmt.Sprintf("outcome-%d", index))))
		if err != nil {
			return nil, err
		}
		results = append(results, logicalRegisterOutcome{
			Key:         normalizeLogicalKey(label, fmt.Sprintf("outcome-%d", index)),
			Label:       label,
			Description: fmt.Sprintf("%s is the winning displayed outcome.", label),
			QuestionID:  questionID,
		})
	}
	return results, nil
}

func buildLogicalMarketPlan(cmd *cobra.Command) (*logicalCreateMarketPlan, error) {
	marketType := strings.ToLower(strings.TrimSpace(mustStringFlag(cmd, "market-type")))
	if marketType == "" {
		marketType = "yes_no"
	}
	if marketType != "yes_no" && marketType != "multiple_choice" {
		return nil, errors.New("--market-type must be yes_no or multiple_choice")
	}

	name := strings.TrimSpace(mustStringFlag(cmd, "name"))
	if name == "" {
		return nil, errors.New("--name is required")
	}
	description := strings.TrimSpace(mustStringFlag(cmd, "description"))
	if description == "" {
		return nil, errors.New("--description is required")
	}
	category := strings.TrimSpace(mustStringFlag(cmd, "category"))
	if category == "" {
		return nil, errors.New("--category is required")
	}
	resolutionCriteria := strings.TrimSpace(mustStringFlag(cmd, "resolution-criteria"))
	if resolutionCriteria == "" {
		return nil, errors.New("--resolution-criteria is required")
	}

	startsAtRaw := strings.TrimSpace(mustStringFlag(cmd, "starts-at"))
	if startsAtRaw == "" {
		return nil, errors.New("--starts-at is required")
	}
	startsAt, err := time.Parse(time.RFC3339, startsAtRaw)
	if err != nil {
		return nil, fmt.Errorf("parse --starts-at: %w", err)
	}
	endsAtRaw := strings.TrimSpace(mustStringFlag(cmd, "ends-at"))
	if endsAtRaw == "" {
		return nil, errors.New("--ends-at is required")
	}
	endsAt, err := time.Parse(time.RFC3339, endsAtRaw)
	if err != nil {
		return nil, fmt.Errorf("parse --ends-at: %w", err)
	}
	if !endsAt.After(startsAt) {
		return nil, errors.New("--ends-at must be later than --starts-at")
	}
	resolveBy := endsAt
	if resolveByRaw := strings.TrimSpace(mustStringFlag(cmd, "resolve-by")); resolveByRaw != "" {
		resolveBy, err = time.Parse(time.RFC3339, resolveByRaw)
		if err != nil {
			return nil, fmt.Errorf("parse --resolve-by: %w", err)
		}
		if resolveBy.Before(endsAt) {
			return nil, errors.New("--resolve-by must be later than or equal to --ends-at")
		}
	}

	displayOutcomes, err := collectLogicalDisplayOutcomes(cmd, marketType)
	if err != nil {
		return nil, err
	}

	marketID := strings.TrimSpace(mustStringFlag(cmd, "market-id"))
	if marketID == "" {
		marketID = fmt.Sprintf("%s-%d", normalizeLogicalKey(name, "logical-market"), time.Now().UTC().UnixMilli())
	}

	tags, _ := cmd.Flags().GetStringSlice("tag")
	trimmedTags := make([]string, 0, len(tags))
	for _, tag := range tags {
		trimmed := strings.TrimSpace(tag)
		if trimmed != "" {
			trimmedTags = append(trimmedTags, trimmed)
		}
	}

	launchOutcomes := make([]logicalLaunchOutcome, 0, len(displayOutcomes))
	bindingOutcomes := displayOutcomes
	if marketType == "yes_no" {
		bindingOutcomes = displayOutcomes[:1]
	}
	for _, outcome := range bindingOutcomes {
		metadataDescription := strings.TrimSpace(description)
		if metadataDescription == "" {
			metadataDescription = fmt.Sprintf("Binary CTF market where YES resolves if the displayed winning outcome is %s.", outcome.Label)
		}
		if outcome.Description != "" {
			metadataDescription = strings.TrimSpace(description)
			if metadataDescription == "" {
				metadataDescription = outcome.Description
			}
		}
		launchOutcomes = append(launchOutcomes, logicalLaunchOutcome{
			Key:                 outcome.Key,
			Label:               outcome.Label,
			Description:         outcome.Description,
			MetadataName:        fmt.Sprintf("%s - %s", name, outcome.Label),
			MetadataDescription: metadataDescription,
			QuestionID:          outcome.QuestionID,
			MetadataURI:         outcome.MetadataURI,
		})
	}

	logicalMarketHashInput := []byte("ctf:" + marketID)
	logicalMarketHash := common.BytesToHash(crypto.Keccak256(logicalMarketHashInput))
	for index := range launchOutcomes {
		if launchOutcomes[index].QuestionID == (common.Hash{}) {
			questionHashInput := []byte(fmt.Sprintf("%s:%s", marketID, launchOutcomes[index].Key))
			launchOutcomes[index].QuestionID = common.BytesToHash(crypto.Keccak256(questionHashInput))
		}
	}

	return &logicalCreateMarketPlan{
		MarketID:            marketID,
		Name:                name,
		Headline:            strings.TrimSpace(mustStringFlag(cmd, "headline")),
		Description:         description,
		Category:            category,
		Tags:                trimmedTags,
		EvidenceSources:     collectLogicalEvidenceSources(cmd),
		Image:               strings.TrimSpace(mustStringFlag(cmd, "image")),
		MarketType:          marketType,
		ResolutionCriteria:  resolutionCriteria,
		StartsAt:            startsAt.UTC(),
		EndsAt:              endsAt.UTC(),
		ResolveBy:           resolveBy.UTC(),
		DisplayOutcomes:     displayOutcomes,
		LaunchOutcomes:      launchOutcomes,
		LogicalMarketIDHash: logicalMarketHash,
	}, nil
}

func uploadLogicalLaunchMetadata(ctx context.Context, cliCtx *cliContext, wallet *evm.Wallet, network string, dryRun bool, plan *logicalCreateMarketPlan) error {
	for index := range plan.LaunchOutcomes {
		if strings.TrimSpace(plan.LaunchOutcomes[index].MetadataURI) != "" {
			plan.LaunchOutcomes[index].MetadataURI = getIPFSURI(plan.LaunchOutcomes[index].MetadataURI)
			continue
		}
		if dryRun {
			plan.LaunchOutcomes[index].MetadataURI = fmt.Sprintf("ipfs://dry-run/%s/%s", plan.MarketID, plan.LaunchOutcomes[index].Key)
			continue
		}
		metadata := clobMarketMetadata{
			Name:            plan.LaunchOutcomes[index].MetadataName,
			Headline:        plan.Headline,
			Description:     plan.LaunchOutcomes[index].MetadataDescription,
			Category:        plan.Category,
			Tags:            plan.Tags,
			EvidenceSources: plan.EvidenceSources,
			Image:           plan.Image,
			Outcomes: []clobOutcomeMetadata{
				{Index: 0, Label: "Yes", Description: fmt.Sprintf("%s is the winning displayed outcome", plan.LaunchOutcomes[index].Label)},
				{Index: 1, Label: "No", Description: fmt.Sprintf("%s is not the winning displayed outcome", plan.LaunchOutcomes[index].Label)},
			},
			ResolutionCriteria: plan.ResolutionCriteria,
			CreatedAt:          time.Now().UTC().Format(time.RFC3339),
			EndsAt:             plan.EndsAt.Format(time.RFC3339),
			OutcomeCount:       2,
		}
		response, err := uploadClobMarketMetadata(ctx, cliCtx, wallet, network, metadata)
		if err != nil {
			return err
		}
		plan.LaunchOutcomes[index].MetadataURI = getIPFSURI(response.IPFSURI)
	}
	return nil
}

func collectLogicalMarketAddresses(cmd *cobra.Command) ([]common.Address, error) {
	values := mustStringSliceFlag(cmd, "address")
	addresses := make([]common.Address, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			if !common.IsHexAddress(trimmed) || isZeroAddress(trimmed) {
				return nil, fmt.Errorf("invalid --address value: %s", trimmed)
			}
			address := common.HexToAddress(trimmed)
			key := strings.ToLower(address.Hex())
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			addresses = append(addresses, address)
		}
	}
	if len(addresses) == 0 {
		return nil, errors.New("at least one --address is required")
	}
	return addresses, nil
}

func validateLogicalRegisterAddresses(plan *logicalCreateMarketPlan, addresses []common.Address) error {
	if plan.MarketType == "yes_no" {
		if len(addresses) != 1 {
			return errors.New("yes_no logical markets require exactly one --address value")
		}
		return nil
	}
	if len(addresses) != len(plan.DisplayOutcomes) {
		return fmt.Errorf("multiple_choice logical markets require %d --address values to match the displayed outcomes", len(plan.DisplayOutcomes))
	}
	return nil
}

func buildLogicalRegisterMessage(marketID string, network string, chainID int64, addresses []common.Address) string {
	addressLines := make([]string, 0, len(addresses))
	for _, address := range addresses {
		addressLines = append(addressLines, address.Hex())
	}
	return strings.Join([]string{
		"axiom.register-clob-market:",
		fmt.Sprintf("marketId=%s", marketID),
		fmt.Sprintf("network=%s", network),
		fmt.Sprintf("chainId=%d", chainID),
		"addresses=",
		strings.Join(addressLines, "\n"),
		fmt.Sprintf("timestamp=%s", time.Now().UTC().Format(time.RFC3339)),
	}, "\n")
}

func buildLogicalResolveMessage(marketID string, network string, winningOutcomeIndex int, resolutionTxHashes []string) string {
	lines := []string{
		"axiom.resolve-clob-market:",
		fmt.Sprintf("marketId=%s", marketID),
		fmt.Sprintf("network=%s", network),
		fmt.Sprintf("winningOutcomeIndex=%d", winningOutcomeIndex),
		"resolutionTxHashes=",
	}
	lines = append(lines, resolutionTxHashes...)
	lines = append(lines, fmt.Sprintf("timestamp=%s", time.Now().UTC().Format(time.RFC3339)))
	return strings.Join(lines, "\n")
}

func buildLogicalUpdateMessage(marketID string, network string, walletAddress string, input logicalUpdateInput) string {
	lines := []string{
		"axiom.update-clob-market:",
		fmt.Sprintf("marketId=%s", strings.TrimSpace(marketID)),
		fmt.Sprintf("network=%s", strings.TrimSpace(network)),
		fmt.Sprintf("walletAddress=%s", strings.TrimSpace(walletAddress)),
	}
	if input.Name != nil {
		lines = append(lines, fmt.Sprintf("name=%s", strings.TrimSpace(*input.Name)))
	}
	if input.Headline != nil {
		lines = append(lines, fmt.Sprintf("headline=%s", strings.TrimSpace(*input.Headline)))
	}
	if input.Description != nil {
		lines = append(lines, fmt.Sprintf("description=%s", strings.TrimSpace(*input.Description)))
	}
	if input.Category != nil {
		lines = append(lines, fmt.Sprintf("category=%s", strings.TrimSpace(*input.Category)))
	}
	if input.ImageURL != nil {
		lines = append(lines, fmt.Sprintf("imageUrl=%s", strings.TrimSpace(*input.ImageURL)))
	}
	if len(input.Tags) > 0 {
		tags := make([]string, 0, len(input.Tags))
		for _, tag := range input.Tags {
			trimmed := strings.TrimSpace(tag)
			if trimmed != "" {
				tags = append(tags, trimmed)
			}
		}
		if len(tags) > 0 {
			lines = append(lines, fmt.Sprintf("tags=%s", strings.Join(tags, ",")))
		}
	}
	return strings.Join(lines, "\n")
}

func buildLogicalResolveRequest(wallet *evm.Wallet, network string, rpcURL string, marketID string, winningOutcomeIndex int, resolutionTxHashes []common.Hash, reason string) (api.ResolveClobMarketRequest, error) {
	resolutionHashValues := make([]string, 0, len(resolutionTxHashes))
	for _, txHash := range resolutionTxHashes {
		resolutionHashValues = append(resolutionHashValues, txHash.Hex())
	}
	message := buildLogicalResolveMessage(marketID, network, winningOutcomeIndex, resolutionHashValues)
	signature, err := wallet.SignMessage(message)
	if err != nil {
		return api.ResolveClobMarketRequest{}, err
	}
	return api.ResolveClobMarketRequest{
		MarketID:            marketID,
		Network:             network,
		RPCURL:              rpcURL,
		WalletAddress:       wallet.Address().Hex(),
		WinningOutcomeIndex: winningOutcomeIndex,
		ResolutionTxHashes:  resolutionHashValues,
		Reason:              reason,
		Message:             message,
		Signature:           signature,
	}, nil
}

func buildLogicalUpdateRequest(wallet *evm.Wallet, network string, marketID string, input logicalUpdateInput) (api.UpdateClobMarketRequest, error) {
	message := buildLogicalUpdateMessage(marketID, network, wallet.Address().Hex(), input)
	signature, err := wallet.SignMessage(message)
	if err != nil {
		return api.UpdateClobMarketRequest{}, err
	}
	return api.UpdateClobMarketRequest{
		MarketID:      strings.TrimSpace(marketID),
		Network:       strings.TrimSpace(network),
		WalletAddress: wallet.Address().Hex(),
		Name:          input.Name,
		Headline:      input.Headline,
		Description:   input.Description,
		Category:      input.Category,
		ImageURL:      input.ImageURL,
		Tags:          append([]string(nil), input.Tags...),
		Message:       message,
		Signature:     signature,
	}, nil
}

func buildLogicalResolutionPlan(market *api.MarketDetails, winningOutcomeIndex int) ([]logicalBindingResolution, error) {
	if market == nil {
		return nil, errors.New("market details are required")
	}
	bindings := sortedClobBindings(market.CTFOutcomeMarkets)
	if len(bindings) == 0 {
		return nil, errors.New("logical market has no CTF outcome bindings")
	}
	winningOutcome, err := resolveClobLogicalOutcome(market, strconv.Itoa(winningOutcomeIndex), "")
	if err != nil {
		return nil, err
	}
	results := make([]logicalBindingResolution, 0, len(bindings))
	for _, binding := range bindings {
		won := binding.OutcomeIndex == winningOutcome.Index
		payouts := []*big.Int{big.NewInt(0), big.NewInt(1)}
		if won {
			payouts = []*big.Int{big.NewInt(1), big.NewInt(0)}
		}
		results = append(results, logicalBindingResolution{
			Binding: binding,
			Payouts: payouts,
			Won:     won,
		})
	}
	return results, nil
}

func buildLogicalRegisterRequest(wallet *evm.Wallet, network string, chainID int64, rpcURL string, plan *logicalCreateMarketPlan, addresses []common.Address, isVisible bool, allowUnindexed bool, bookSignatures []api.RegisterClobBookSignature) (api.RegisterClobMarketRequest, error) {
	message := buildLogicalRegisterMessage(plan.MarketID, network, chainID, addresses)
	signature, err := wallet.SignMessage(message)
	if err != nil {
		return api.RegisterClobMarketRequest{}, err
	}
	addressValues := make([]string, 0, len(addresses))
	for _, address := range addresses {
		addressValues = append(addressValues, address.Hex())
	}
	metadata := api.RegisterClobMarketMetadata{
		Name:               plan.Name,
		Headline:           plan.Headline,
		Description:        plan.Description,
		Category:           plan.Category,
		Tags:               plan.Tags,
		MarketType:         plan.MarketType,
		ResolutionCriteria: plan.ResolutionCriteria,
		EvidenceSources:    plan.EvidenceSources,
		Image:              plan.Image,
		StartsAt:           plan.StartsAt.Format(time.RFC3339),
		EndsAt:             plan.EndsAt.Format(time.RFC3339),
		ResolveBy:          plan.ResolveBy.Format(time.RFC3339),
		DisplayOutcomes:    make([]api.RegisterClobMarketDisplayOutcome, 0, len(plan.DisplayOutcomes)),
	}
	for _, outcome := range plan.DisplayOutcomes {
		metadata.DisplayOutcomes = append(metadata.DisplayOutcomes, api.RegisterClobMarketDisplayOutcome{
			Key:         outcome.Key,
			Label:       outcome.Label,
			Description: outcome.Description,
		})
	}
	return api.RegisterClobMarketRequest{
		MarketID:       plan.MarketID,
		Network:        network,
		ChainID:        int(chainID),
		RPCURL:         rpcURL,
		Addresses:      addressValues,
		IsVisible:      isVisible,
		AllowUnindexed: allowUnindexed,
		Metadata:       metadata,
		Message:        message,
		Signature:      signature,
		BookSignatures: bookSignatures,
	}, nil
}

func buildLogicalBookSignatures(wallet *evm.Wallet, domain clobSigningDomain, marketID string, addresses []common.Address) ([]api.RegisterClobBookSignature, error) {
	results := make([]api.RegisterClobBookSignature, 0, len(addresses))
	for outcomeIndex, address := range addresses {
		signature, err := buildCreateBookSignature(wallet, domain, marketID, outcomeIndex)
		if err != nil {
			return nil, err
		}
		results = append(results, api.RegisterClobBookSignature{
			Address:      address.Hex(),
			OutcomeIndex: outcomeIndex,
			Signature:    signature,
		})
	}
	return results, nil
}

func mustStringSliceFlag(cmd *cobra.Command, name string) []string {
	values, _ := cmd.Flags().GetStringSlice(name)
	return values
}

func buildClobOrderAmounts(side string, priceBps int, quantity int) (*big.Int, *big.Int) {
	q := big.NewInt(int64(quantity))
	p := big.NewInt(int64(priceBps))
	bpsScale := big.NewInt(10000)
	tokenScale := big.NewInt(1_000_000_000_000_000_000)
	sharesRaw := new(big.Int).Mul(q, tokenScale)

	// Market orders (price=0): use BpsScale so amounts are non-zero.
	if priceBps <= 0 {
		p = new(big.Int).Set(bpsScale)
	}

	// Amounts must be encoded in raw token units (18 decimals) so the hosted
	// validator derives quantity and price consistently with the exchange.
	//   BUY:  makerAmount = (qty * TokenScale) * price / BpsScale
	//         takerAmount = qty * TokenScale
	//   SELL: makerAmount = qty * TokenScale
	//         takerAmount = (qty * TokenScale) * price / BpsScale
	if side == "buy" {
		makerAmount := new(big.Int).Div(
			new(big.Int).Mul(sharesRaw, p),
			bpsScale,
		)
		takerAmount := new(big.Int).Set(sharesRaw)
		return makerAmount, takerAmount
	}
	makerAmount := new(big.Int).Set(sharesRaw)
	takerAmount := new(big.Int).Div(
		new(big.Int).Mul(sharesRaw, p),
		bpsScale,
	)
	return makerAmount, takerAmount
}

// clobMinSettleableShares returns the minimum number of shares that settles
// on-chain. With ceiling rounding in Math.mulDiv, any qty >= 1 works.
func clobMinSettleableShares(_ api.ClobSignedOrderPayload) (int, error) {
	return 1, nil
}

// validateClobSettleableQuantity checks that the order has a non-zero quantity.
// The on-chain exchange uses Math.mulDiv with Rounding.Ceil, so any qty >= 1
// produces a valid fill amount.
func validateClobSettleableQuantity(payload api.ClobSignedOrderPayload) error {
	derivedQuantity, err := clobPayloadQuantity(payload)
	if err != nil {
		return err
	}
	if derivedQuantity < 1 {
		return fmt.Errorf("order quantity too small for on-chain settlement: quantity must be at least 1")
	}
	return nil
}

func clobPayloadQuantity(payload api.ClobSignedOrderPayload) (int, error) {
	var amountRaw string
	if payload.Side == clobOrderSideValue["buy"] {
		amountRaw = payload.TakerAmount
	} else {
		amountRaw = payload.MakerAmount
	}
	amount, err := evm.ParseBigInt(amountRaw)
	if err != nil {
		return 0, fmt.Errorf("parse order quantity amount: %w", err)
	}
	tokenScale := big.NewInt(1_000_000_000_000_000_000)
	quantity := new(big.Int).Div(amount, tokenScale)
	if !quantity.IsInt64() {
		return 0, errors.New("derived order quantity exceeds supported CLI integer range")
	}
	if quantity.Int64() > int64(^uint(0)>>1) {
		return 0, errors.New("derived order quantity exceeds supported CLI integer range")
	}
	return int(quantity.Int64()), nil
}

func clobPayloadPriceBps(payload api.ClobSignedOrderPayload) (int, error) {
	makerAmount, err := evm.ParseBigInt(payload.MakerAmount)
	if err != nil {
		return 0, fmt.Errorf("parse maker amount: %w", err)
	}
	takerAmount, err := evm.ParseBigInt(payload.TakerAmount)
	if err != nil {
		return 0, fmt.Errorf("parse taker amount: %w", err)
	}
	if makerAmount.Sign() <= 0 || takerAmount.Sign() <= 0 {
		return 0, nil
	}
	bpsScale := big.NewInt(10000)
	bps := new(big.Int)
	if payload.Side == clobOrderSideValue["buy"] {
		bps.Mul(makerAmount, bpsScale)
		bps.Div(bps, takerAmount)
	} else {
		bps.Mul(takerAmount, bpsScale)
		bps.Div(bps, makerAmount)
	}
	if !bps.IsInt64() {
		return 0, errors.New("derived order price exceeds supported CLI integer range")
	}
	if bps.Int64() > int64(^uint(0)>>1) {
		return 0, errors.New("derived order price exceeds supported CLI integer range")
	}
	return int(bps.Int64()), nil
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

func buildClobRedemptionPlan(ctx context.Context, cliCtx *cliContext, market *api.MarketDetails, walletAddress common.Address) ([]clobRedemptionLeg, error) {
	collateralToken := resolveClobCollateralToken(market)
	bindings := sortedClobBindings(market.CTFOutcomeMarkets)
	legs := make([]clobRedemptionLeg, 0)
	outcomeTokensByContract := make(map[string]common.Address, len(bindings))
	for _, binding := range bindings {
		contractAddress := common.HexToAddress(binding.ContractAddress).Hex()
		outcomeToken, ok := outcomeTokensByContract[contractAddress]
		if !ok {
			resolvedOutcomeToken, err := resolveClobBindingOutcomeToken(ctx, cliCtx, binding)
			if err != nil {
				return nil, err
			}
			outcomeToken = resolvedOutcomeToken
			outcomeTokensByContract[contractAddress] = outcomeToken
		}
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

func summarizeClobSplitStatus(collateralBalance *big.Int, collateralAllowance *big.Int, yesBalance *big.Int, noBalance *big.Int) clobSplitStatusSummary {
	maxSplit := cloneBigInt(collateralBalance)
	allowance := cloneBigInt(collateralAllowance)
	if allowance.Cmp(maxSplit) < 0 {
		maxSplit.Set(allowance)
	}

	maxMergeable := cloneBigInt(yesBalance)
	noBalanceValue := cloneBigInt(noBalance)
	if noBalanceValue.Cmp(maxMergeable) < 0 {
		maxMergeable.Set(noBalanceValue)
	}

	return clobSplitStatusSummary{
		MaxSplitWei:           maxSplit,
		MaxMergeableWei:       maxMergeable,
		SplitApproved:         allowance.Sign() > 0,
		SplitReady:            maxSplit.Sign() > 0,
		MergeReady:            maxMergeable.Sign() > 0,
		MergeApprovalRequired: false,
	}
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

func resolveSplitMergeBinding(market *api.MarketDetails, labelFlag string) (api.CtfOutcomeMarketBinding, error) {
	bindings := sortedClobBindings(market.CTFOutcomeMarkets)
	if len(bindings) == 0 {
		return api.CtfOutcomeMarketBinding{}, errors.New("market has no CTF outcome bindings")
	}
	label := strings.TrimSpace(labelFlag)
	if label == "" {
		if len(bindings) == 1 {
			b := bindings[0]
			if strings.TrimSpace(b.ConditionID) == "" || len(b.ConditionID) != 66 {
				return api.CtfOutcomeMarketBinding{}, fmt.Errorf("binding %q has no usable conditionId", b.Label)
			}
			return b, nil
		}
		return api.CtfOutcomeMarketBinding{}, errors.New("multiple bindings found; use --label to select one")
	}
	for _, b := range bindings {
		if strings.EqualFold(b.Label, label) {
			if strings.TrimSpace(b.ConditionID) == "" || len(b.ConditionID) != 66 {
				return api.CtfOutcomeMarketBinding{}, fmt.Errorf("binding %q has no usable conditionId", b.Label)
			}
			return b, nil
		}
	}
	labels := make([]string, 0, len(bindings))
	for _, b := range bindings {
		labels = append(labels, b.Label)
	}
	return api.CtfOutcomeMarketBinding{}, fmt.Errorf("no binding found for label %q; available: %s", label, strings.Join(labels, ", "))
}

// parseClobAmount parses --amount as either a decimal XRP value (e.g. "0.01")
// or a raw wei integer. If the string contains a dot it is treated as XRP and
// converted to 18-decimal wei; otherwise it is parsed as an integer in wei.
func parseClobAmount(raw string) (*big.Int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("--amount is required")
	}
	if strings.Contains(trimmed, ".") {
		value, ok := new(big.Rat).SetString(trimmed)
		if !ok {
			return nil, fmt.Errorf("invalid amount: %s", trimmed)
		}
		if value.Sign() <= 0 {
			return nil, fmt.Errorf("amount must be greater than zero")
		}
		value.Mul(value, big.NewRat(1_000_000_000_000_000_000, 1))
		if !value.IsInt() {
			return nil, fmt.Errorf("amount has too many decimal places: %s", trimmed)
		}
		result := new(big.Int)
		result.Div(value.Num(), value.Denom())
		return result, nil
	}
	amount, err := evm.ParseBigInt(trimmed)
	if err != nil {
		return nil, fmt.Errorf("invalid --amount: %w", err)
	}
	if amount.Sign() <= 0 {
		return nil, errors.New("--amount must be greater than zero")
	}
	return amount, nil
}
