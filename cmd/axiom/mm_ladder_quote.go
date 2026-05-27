package main

import (
	"fmt"
	"math/big"
	"net/url"
	"strconv"
	"strings"

	"github.com/Gen3Games/axiom-cli/internal/api"
	"github.com/Gen3Games/axiom-cli/internal/evm"
	"github.com/spf13/cobra"
)

type mmQuoteLevel struct {
	BidPriceBps int
	AskPriceBps int
	Quantity    int
	BidQuantity int
	AskQuantity int
}

type mmPreparedLadderLevel struct {
	Index       int
	Quantity    int
	BidQuantity int
	AskQuantity int
	BidPriceBps int
	AskPriceBps int
	BidPayload  *api.ClobSignedOrderPayload
	AskPayload  *api.ClobSignedOrderPayload
	BidBlocks   []string
	AskBlocks   []string
}

func newMMLadderQuoteCommand() *cobra.Command {
	ladderQuoteCmd := &cobra.Command{
		Use:   "ladder-quote [market-id-or-address]",
		Short: "Place a multi-level two-sided market-making quote ladder on one hosted CLOB book",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			wallet, privateKeyHex, err := requireEVMWalletWithKey(ctx)
			if err != nil {
				return err
			}

			marketRef, instanceDate, err := resolveMMMarketReference(ctx, args, mustStringFlag(cmd, "instance-date"))
			if err != nil {
				return err
			}
			market, err := loadMMMarket(cmd.Context(), ctx, marketRef, instanceDate)
			if err != nil {
				return err
			}
			if !isClobMarketImplementation(market.MarketImplementation) {
				return fmt.Errorf("mm ladder-quote requires an AxiomCTFMarket logical market")
			}

			selection, err := resolveClobSelection(
				market,
				mustStringFlag(cmd, "outcome"),
				mustStringFlag(cmd, "label"),
				mustStringFlag(cmd, "displayed-side"),
				mustStringFlag(cmd, "exchange-address"),
				mustStringFlag(cmd, "outcome-token-address"),
			)
			if err != nil {
				return err
			}

			levelValues, err := cmd.Flags().GetStringArray("level")
			if err != nil {
				return err
			}
			levels, err := parseMMQuoteLevels(levelValues)
			if err != nil {
				return err
			}

			signingDomain, err := resolveClobSigningDomain(cmd)
			if err != nil {
				return err
			}

			if mustBoolFlag(cmd, "cancel-active") {
				if err := cancelExistingMMOrdersForSelection(cmd, ctx, wallet, market.ID, selection, signingDomain); err != nil {
					return err
				}
			}

			status, err := buildClobWalletStatus(cmd.Context(), ctx, market, wallet.Address(), selection.ExchangeAddress, selection.OutcomeToken)
			if err != nil {
				return err
			}

			preparedLevels, approvals, err := prepareLadderLevels(cmd, ctx, market, selection, wallet, privateKeyHex, signingDomain, status, levels)
			if err != nil {
				return err
			}

			if mustBoolFlag(cmd, "dry-run") {
				return printOutput(ctx.JSON, buildMMLadderDryRunResult(market, selection, preparedLevels, approvals))
			}

			result, err := submitLadderLevels(cmd, ctx, market, selection, wallet, signingDomain, preparedLevels, approvals)
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, result)
		},
	}
	ladderQuoteCmd.Flags().String("outcome", "", "Logical outcome index to quote")
	ladderQuoteCmd.Flags().String("label", "", "Logical outcome label to quote")
	ladderQuoteCmd.Flags().String("displayed-side", "", "Displayed side to quote: yes or no; inferred for single-binding binary markets")
	ladderQuoteCmd.Flags().StringArray("level", nil, "Quote ladder level as bid,ask,quantity or bid,ask,bidQuantity,askQuantity in displayed percent units and whole shares; repeatable")
	ladderQuoteCmd.Flags().String("expiry", "24h", "Expiry preset for all resting quotes: 1h, 24h, 7d, never")
	ladderQuoteCmd.Flags().Bool("dry-run", false, "Build all signed ladder quotes locally and report readiness without submitting them")
	ladderQuoteCmd.Flags().Bool("cancel-active", false, "Cancel existing resting orders on this book before placing ladder quotes")
	ladderQuoteCmd.Flags().String("cancel-reason", "market-maker-quote-replace", "Cancellation reason recorded when --cancel-active removes prior orders")
	ladderQuoteCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	_ = ladderQuoteCmd.MarkFlagRequired("level")
	return ladderQuoteCmd
}

func parseMMQuoteLevels(values []string) ([]mmQuoteLevel, error) {
	levels := make([]mmQuoteLevel, 0, len(values))
	for index, value := range values {
		parts := strings.Split(value, ",")
		if len(parts) != 3 && len(parts) != 4 {
			return nil, fmt.Errorf("parse --level[%d]: expected bid,ask,quantity or bid,ask,bidQuantity,askQuantity", index)
		}
		bidPriceBps, err := parseClobPriceToBps(parts[0])
		if err != nil {
			return nil, fmt.Errorf("parse --level[%d] bid: %w", index, err)
		}
		askPriceBps, err := parseClobPriceToBps(parts[1])
		if err != nil {
			return nil, fmt.Errorf("parse --level[%d] ask: %w", index, err)
		}
		if askPriceBps <= bidPriceBps {
			return nil, fmt.Errorf("parse --level[%d]: ask must be greater than bid", index)
		}
		quantity, err := parseClobQuantity(parts[2])
		if err != nil {
			return nil, fmt.Errorf("parse --level[%d] quantity: %w", index, err)
		}
		bidQuantity := quantity
		askQuantity := quantity
		if len(parts) == 4 {
			bidQuantity, err = parseOptionalClobQuantity(parts[2])
			if err != nil {
				return nil, fmt.Errorf("parse --level[%d] bidQuantity: %w", index, err)
			}
			askQuantity, err = parseOptionalClobQuantity(parts[3])
			if err != nil {
				return nil, fmt.Errorf("parse --level[%d] askQuantity: %w", index, err)
			}
			quantity = maxInt(bidQuantity, askQuantity)
		}
		if bidQuantity <= 0 && askQuantity <= 0 {
			return nil, fmt.Errorf("parse --level[%d]: at least one side quantity must be positive", index)
		}
		levels = append(levels, mmQuoteLevel{BidPriceBps: bidPriceBps, AskPriceBps: askPriceBps, Quantity: quantity, BidQuantity: bidQuantity, AskQuantity: askQuantity})
	}
	if len(levels) == 0 {
		return nil, fmt.Errorf("at least one --level is required")
	}
	return levels, nil
}

func cancelExistingMMOrdersForSelection(cmd *cobra.Command, ctx *cliContext, wallet *evm.Wallet, marketID string, selection *clobSelection, signingDomain clobSigningDomain) error {
	cancelReason := strings.TrimSpace(mustStringFlag(cmd, "cancel-reason"))
	if cancelReason == "" {
		cancelReason = "market-maker-quote-replace"
	}
	filters := url.Values{}
	filters.Set("maker", wallet.Address().Hex())
	filters.Set("active_only", "true")
	filters.Set("clob_id", clobIDForMarketOutcome(marketID, selection.Binding.OutcomeIndex, selection.DisplayedSide))
	existingOrders, err := ctx.API.ListClobOrders(cmd.Context(), strings.TrimSpace(mustStringFlag(cmd, "projection-url")), filters)
	if err != nil {
		return fmt.Errorf("list existing orders before cancel-active: %w", err)
	}
	for _, order := range existingOrders {
		_, existingOutcomeIndex, existingTokenSide, parseErr := parseClobOrderIdentity(order)
		if parseErr != nil {
			continue
		}
		cancelRequest, cancelErr := buildSignedClobCancel(wallet, signingDomain, order.OrderID, marketID, existingOutcomeIndex, existingTokenSide, wallet.Address().Hex(), cancelReason)
		if cancelErr != nil {
			continue
		}
		if _, cancelErr = ctx.API.CancelClobOrder(cmd.Context(), strings.TrimSpace(mustStringFlag(cmd, "eventstore-url")), order.OrderID, cancelRequest); cancelErr != nil {
			continue
		}
	}
	return nil
}

func prepareLadderLevels(cmd *cobra.Command, ctx *cliContext, market *api.MarketDetails, selection *clobSelection, wallet *evm.Wallet, privateKeyHex string, signingDomain clobSigningDomain, status *clobWalletStatus, levels []mmQuoteLevel) ([]mmPreparedLadderLevel, []map[string]any, error) {
	prepared := make([]mmPreparedLadderLevel, 0, len(levels))
	approvals := make([]map[string]any, 0, 2)
	expiry := mustStringFlag(cmd, "expiry")
	remainingCollateral, err := evm.ParseBigInt(status.CollateralBalanceWei)
	if err != nil {
		return nil, nil, fmt.Errorf("parse collateral balance: %w", err)
	}
	remainingBidAllowance, err := evm.ParseBigInt(status.CollateralAllowanceWei)
	if err != nil {
		return nil, nil, fmt.Errorf("parse collateral allowance: %w", err)
	}
	remainingAskInventory, inventoryFound, err := clobSmokeDisplayedSideBalance(status, selection.Binding.OutcomeIndex, selection.DisplayedSide)
	if err != nil {
		return nil, nil, err
	}
	if !inventoryFound {
		remainingAskInventory = big.NewInt(0)
	}

	for index, level := range levels {
		var bidPayload *api.ClobSignedOrderPayload
		if level.BidQuantity > 0 {
			payload, buildErr := buildClobSignedOrder(wallet, market.ID, selection, "buy", "limit", level.BidPriceBps, level.BidQuantity, expiry, signingDomain)
			if buildErr != nil {
				return nil, nil, buildErr
			}
			bidPayload = &payload
		}
		var askPayload *api.ClobSignedOrderPayload
		if level.AskQuantity > 0 {
			payload, buildErr := buildClobSignedOrder(wallet, market.ID, selection, "sell", "limit", level.AskPriceBps, level.AskQuantity, expiry, signingDomain)
			if buildErr != nil {
				return nil, nil, buildErr
			}
			askPayload = &payload
		}

		bidBlocks := make([]string, 0)
		askBlocks := make([]string, 0)
		if bidPayload != nil {
			bidBlocks = collectClobSmokeBlocking(status, selection, *bidPayload)
			if settleErr := validateClobSettleableQuantity(*bidPayload); settleErr != nil {
				bidBlocks = append(bidBlocks, settleErr.Error())
			}
		}
		if askPayload != nil {
			askBlocks = collectClobSmokeBlocking(status, selection, *askPayload)
			if settleErr := validateClobSettleableQuantity(*askPayload); settleErr != nil {
				askBlocks = append(askBlocks, settleErr.Error())
			}
		}

		var bidMakerAmount = big.NewInt(0)
		if bidPayload != nil {
			bidMakerAmount, err = evm.ParseBigInt(bidPayload.MakerAmount)
			if err != nil {
				return nil, nil, err
			}
		}
		var askMakerAmount = big.NewInt(0)
		if askPayload != nil {
			askMakerAmount, err = evm.ParseBigInt(askPayload.MakerAmount)
			if err != nil {
				return nil, nil, err
			}
		}

		if bidPayload != nil && remainingCollateral.Cmp(bidMakerAmount) < 0 {
			bidBlocks = append(bidBlocks, fmt.Sprintf("aggregate collateral balance %s wei is below cumulative required maker amount %s", remainingCollateral.String(), bidPayload.MakerAmount))
		}
		if bidPayload != nil && remainingBidAllowance.Cmp(bidMakerAmount) < 0 {
			bidBlocks = append(bidBlocks, fmt.Sprintf("aggregate collateral allowance %s wei is below cumulative required maker amount %s", remainingBidAllowance.String(), bidPayload.MakerAmount))
		}
		if askPayload != nil && !status.OutcomeApprovalForAll {
			askBlocks = append(askBlocks, "outcome-token approval-for-all is not enabled")
		}
		if askPayload != nil && remainingAskInventory.Cmp(askMakerAmount) < 0 {
			askBlocks = append(askBlocks, fmt.Sprintf("aggregate displayed-side balance %s is below cumulative required maker amount %s", remainingAskInventory.String(), askPayload.MakerAmount))
		}

		if bidPayload != nil && len(bidBlocks) > 0 {
			approvalTxs, approveErr := ensureClobOrderApprovals(cmd.Context(), ctx.Config.EVMRPCURL, privateKeyHex, wallet.Address(), status, selection, *bidPayload, true)
			if approveErr != nil {
				return nil, nil, fmt.Errorf("auto-approve bid ladder level %d prerequisites: %w", index+1, approveErr)
			}
			if len(approvalTxs) > 0 {
				approvals = append(approvals, approvalTxs...)
				status.CollateralAllowanceWei = clobMaxUint256
				remainingBidAllowance, err = evm.ParseBigInt(clobMaxUint256)
				if err != nil {
					return nil, nil, err
				}
				status.CollateralAllowanceXRP = formatWeiToXRP(remainingBidAllowance)
			}
			bidBlocks = filterAggregateBlocks(collectClobSmokeBlockingAfterApprovals(status, selection, *bidPayload), "collateral allowance")
			if remainingCollateral.Cmp(bidMakerAmount) < 0 {
				bidBlocks = append(bidBlocks, fmt.Sprintf("aggregate collateral balance %s wei is below cumulative required maker amount %s", remainingCollateral.String(), bidPayload.MakerAmount))
			}
		}

		if askPayload != nil && len(askBlocks) > 0 {
			approvalTxs, approveErr := ensureClobOrderApprovals(cmd.Context(), ctx.Config.EVMRPCURL, privateKeyHex, wallet.Address(), status, selection, *askPayload, true)
			if approveErr != nil {
				return nil, nil, fmt.Errorf("auto-approve ask ladder level %d prerequisites: %w", index+1, approveErr)
			}
			if len(approvalTxs) > 0 {
				approvals = append(approvals, approvalTxs...)
				status.OutcomeApprovalForAll = true
			}
			askBlocks = filterAggregateBlocks(collectClobSmokeBlockingAfterApprovals(status, selection, *askPayload), "displayed-side balance")
			if remainingAskInventory.Cmp(askMakerAmount) < 0 {
				askBlocks = append(askBlocks, fmt.Sprintf("aggregate displayed-side balance %s is below cumulative required maker amount %s", remainingAskInventory.String(), askPayload.MakerAmount))
			}
		}

		if len(bidBlocks) > 0 || len(askBlocks) > 0 {
			return nil, nil, buildMMQuoteReadinessError(prefixBlocksForLevel(index+1, bidBlocks, "bid"), prefixBlocksForLevel(index+1, askBlocks, "ask"))
		}

		remainingCollateral = new(big.Int).Sub(remainingCollateral, bidMakerAmount)
		remainingBidAllowance = new(big.Int).Sub(remainingBidAllowance, bidMakerAmount)
		remainingAskInventory = new(big.Int).Sub(remainingAskInventory, askMakerAmount)

		prepared = append(prepared, mmPreparedLadderLevel{
			Index:       index + 1,
			Quantity:    level.Quantity,
			BidQuantity: level.BidQuantity,
			AskQuantity: level.AskQuantity,
			BidPriceBps: level.BidPriceBps,
			AskPriceBps: level.AskPriceBps,
			BidPayload:  bidPayload,
			AskPayload:  askPayload,
			BidBlocks:   bidBlocks,
			AskBlocks:   askBlocks,
		})
	}

	return prepared, approvals, nil
}

func filterAggregateBlocks(blocks []string, prefix string) []string {
	filtered := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if strings.Contains(strings.ToLower(block), strings.ToLower(prefix)) {
			continue
		}
		filtered = append(filtered, block)
	}
	return filtered
}

func prefixBlocksForLevel(level int, blocks []string, side string) []string {
	if len(blocks) == 0 {
		return nil
	}
	result := make([]string, 0, len(blocks))
	for _, block := range blocks {
		result = append(result, fmt.Sprintf("level %d %s: %s", level, side, block))
	}
	return result
}

func buildMMLadderDryRunResult(market *api.MarketDetails, selection *clobSelection, levels []mmPreparedLadderLevel, approvals []map[string]any) map[string]any {
	items := make([]map[string]any, 0, len(levels))
	for _, level := range levels {
		items = append(items, map[string]any{
			"level":    level.Index,
			"quantity": level.Quantity,
			"bid": map[string]any{
				"priceBps":       level.BidPriceBps,
				"quantity":       level.BidQuantity,
				"makerAmount":    valueOrEmpty(level.BidPayload, func(payload *api.ClobSignedOrderPayload) string { return payload.MakerAmount }),
				"takerAmount":    valueOrEmpty(level.BidPayload, func(payload *api.ClobSignedOrderPayload) string { return payload.TakerAmount }),
				"expiration":     valueOrEmpty(level.BidPayload, func(payload *api.ClobSignedOrderPayload) string { return payload.Expiration }),
				"nonce":          valueOrEmpty(level.BidPayload, func(payload *api.ClobSignedOrderPayload) string { return payload.Nonce }),
				"tokenSide":      valueOrEmpty(level.BidPayload, func(payload *api.ClobSignedOrderPayload) string { return payload.TokenSide }),
				"outcomeTokenId": valueOrEmpty(level.BidPayload, func(payload *api.ClobSignedOrderPayload) string { return payload.OutcomeTokenID }),
				"blocking":       level.BidBlocks,
			},
			"ask": map[string]any{
				"priceBps":       level.AskPriceBps,
				"quantity":       level.AskQuantity,
				"makerAmount":    valueOrEmpty(level.AskPayload, func(payload *api.ClobSignedOrderPayload) string { return payload.MakerAmount }),
				"takerAmount":    valueOrEmpty(level.AskPayload, func(payload *api.ClobSignedOrderPayload) string { return payload.TakerAmount }),
				"expiration":     valueOrEmpty(level.AskPayload, func(payload *api.ClobSignedOrderPayload) string { return payload.Expiration }),
				"nonce":          valueOrEmpty(level.AskPayload, func(payload *api.ClobSignedOrderPayload) string { return payload.Nonce }),
				"tokenSide":      valueOrEmpty(level.AskPayload, func(payload *api.ClobSignedOrderPayload) string { return payload.TokenSide }),
				"outcomeTokenId": valueOrEmpty(level.AskPayload, func(payload *api.ClobSignedOrderPayload) string { return payload.OutcomeTokenID }),
				"blocking":       level.AskBlocks,
			},
		})
	}
	result := map[string]any{
		"dryRun":        true,
		"market":        market.Title,
		"marketId":      market.ID,
		"outcomeLabel":  selection.LogicalOutcome.Label,
		"outcomeIndex":  selection.Binding.OutcomeIndex,
		"displayedSide": selection.DisplayedSide,
		"quoteReady":    true,
		"levels":        items,
	}
	if len(approvals) > 0 {
		result["approvals"] = approvals
	}
	return result
}

type submittedLadderOrder struct {
	OrderID      string
	OutcomeIndex int
	TokenSide    string
}

func submitLadderLevels(cmd *cobra.Command, ctx *cliContext, market *api.MarketDetails, selection *clobSelection, wallet *evm.Wallet, signingDomain clobSigningDomain, levels []mmPreparedLadderLevel, approvals []map[string]any) (map[string]any, error) {
	eventstoreURL := strings.TrimSpace(mustStringFlag(cmd, "eventstore-url"))
	submitted := make([]submittedLadderOrder, 0, len(levels)*2)
	items := make([]map[string]any, 0, len(levels))
	for _, level := range levels {
		var bidResponse *api.ClobOrderResponse
		if level.BidPayload != nil {
			response, err := submitClobSmokeOrderWithRetry(cmd.Context(), ctx, eventstoreURL, *level.BidPayload)
			if err != nil {
				if rollbackErr := rollbackSubmittedLadderOrders(cmd, ctx, wallet, signingDomain, market.ID, submitted); rollbackErr != nil {
					return nil, fmt.Errorf("submit ladder bid level %d: %w (rollback failed: %v)", level.Index, err, rollbackErr)
				}
				return nil, fmt.Errorf("submit ladder bid level %d: %w", level.Index, err)
			}
			bidResponse = response
			submitted = append(submitted, submittedLadderOrder{OrderID: response.OrderID, OutcomeIndex: selection.Binding.OutcomeIndex, TokenSide: selection.DisplayedSide})
		}

		var askResponse *api.ClobOrderResponse
		if level.AskPayload != nil {
			response, err := submitClobSmokeOrderWithRetry(cmd.Context(), ctx, eventstoreURL, *level.AskPayload)
			if err != nil {
				if rollbackErr := rollbackSubmittedLadderOrders(cmd, ctx, wallet, signingDomain, market.ID, submitted); rollbackErr != nil {
					return nil, fmt.Errorf("submit ladder ask level %d: %w (rollback failed: %v)", level.Index, err, rollbackErr)
				}
				return nil, fmt.Errorf("submit ladder ask level %d: %w", level.Index, err)
			}
			askResponse = response
			submitted = append(submitted, submittedLadderOrder{OrderID: response.OrderID, OutcomeIndex: selection.Binding.OutcomeIndex, TokenSide: selection.DisplayedSide})
		}

		items = append(items, map[string]any{
			"level":    level.Index,
			"quantity": level.Quantity,
			"bid": map[string]any{
				"priceBps":        level.BidPriceBps,
				"quantity":        level.BidQuantity,
				"orderId":         valueOrEmpty(bidResponse, func(response *api.ClobOrderResponse) string { return response.OrderID }),
				"tradeCount":      valueOrClobInt(bidResponse, func(response *api.ClobOrderResponse) int { return response.TradeCount }),
				"remainingShares": valueOrClobInt(bidResponse, func(response *api.ClobOrderResponse) int { return response.RemainingQuantity }),
				"resting":         valueOrFalse(bidResponse, func(response *api.ClobOrderResponse) bool { return response.WasAddedToBook }),
			},
			"ask": map[string]any{
				"priceBps":        level.AskPriceBps,
				"quantity":        level.AskQuantity,
				"orderId":         valueOrEmpty(askResponse, func(response *api.ClobOrderResponse) string { return response.OrderID }),
				"tradeCount":      valueOrClobInt(askResponse, func(response *api.ClobOrderResponse) int { return response.TradeCount }),
				"remainingShares": valueOrClobInt(askResponse, func(response *api.ClobOrderResponse) int { return response.RemainingQuantity }),
				"resting":         valueOrFalse(askResponse, func(response *api.ClobOrderResponse) bool { return response.WasAddedToBook }),
			},
		})
	}

	result := map[string]any{
		"market":        market.Title,
		"marketId":      market.ID,
		"outcomeLabel":  selection.LogicalOutcome.Label,
		"outcomeIndex":  selection.Binding.OutcomeIndex,
		"displayedSide": selection.DisplayedSide,
		"levels":        items,
		"message":       "Multi-level two-sided quote ladder resting on the hosted CLOB book.",
	}
	if len(approvals) > 0 {
		result["approvals"] = approvals
	}
	return result, nil
}

func parseOptionalClobQuantity(value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, fmt.Errorf("quantity must not be empty")
	}
	quantity, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0, fmt.Errorf("parse quantity: %w", err)
	}
	if quantity < 0 {
		return 0, fmt.Errorf("quantity must be zero or a positive whole number")
	}
	return quantity, nil
}

func maxInt(left int, right int) int {
	if left > right {
		return left
	}
	return right
}

func valueOrEmpty[T any](value *T, getter func(*T) string) string {
	if value == nil {
		return ""
	}
	return getter(value)
}

func valueOrClobInt[T any](value *T, getter func(*T) int) int {
	if value == nil {
		return 0
	}
	return getter(value)
}

func valueOrFalse[T any](value *T, getter func(*T) bool) bool {
	if value == nil {
		return false
	}
	return getter(value)
}

func rollbackSubmittedLadderOrders(cmd *cobra.Command, ctx *cliContext, wallet *evm.Wallet, signingDomain clobSigningDomain, marketID string, orders []submittedLadderOrder) error {
	eventstoreURL := strings.TrimSpace(mustStringFlag(cmd, "eventstore-url"))
	for index := len(orders) - 1; index >= 0; index-- {
		order := orders[index]
		cancelRequest, err := buildSignedClobCancel(wallet, signingDomain, order.OrderID, marketID, order.OutcomeIndex, order.TokenSide, wallet.Address().Hex(), "market-maker-ladder-quote-rollback")
		if err != nil {
			return err
		}
		if _, err := ctx.API.CancelClobOrder(cmd.Context(), eventstoreURL, order.OrderID, cancelRequest); err != nil {
			return err
		}
	}
	return nil
}
