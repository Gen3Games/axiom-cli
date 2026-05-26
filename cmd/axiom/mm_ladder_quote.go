package main

import (
	"fmt"
	"math/big"
	"net/url"
	"strings"

	"github.com/Gen3Games/axiom-cli/internal/api"
	"github.com/Gen3Games/axiom-cli/internal/evm"
	"github.com/spf13/cobra"
)

type mmQuoteLevel struct {
	BidPriceBps int
	AskPriceBps int
	Quantity    int
}

type mmPreparedLadderLevel struct {
	Index       int
	Quantity    int
	BidPriceBps int
	AskPriceBps int
	BidPayload  api.ClobSignedOrderPayload
	AskPayload  api.ClobSignedOrderPayload
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
	ladderQuoteCmd.Flags().StringArray("level", nil, "Quote ladder level as bid,ask,quantity in displayed percent units and whole shares; repeatable")
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
		if len(parts) != 3 {
			return nil, fmt.Errorf("parse --level[%d]: expected bid,ask,quantity", index)
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
		levels = append(levels, mmQuoteLevel{BidPriceBps: bidPriceBps, AskPriceBps: askPriceBps, Quantity: quantity})
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
		bidPayload, err := buildClobSignedOrder(wallet, market.ID, selection, "buy", "limit", level.BidPriceBps, level.Quantity, expiry, signingDomain)
		if err != nil {
			return nil, nil, err
		}
		askPayload, err := buildClobSignedOrder(wallet, market.ID, selection, "sell", "limit", level.AskPriceBps, level.Quantity, expiry, signingDomain)
		if err != nil {
			return nil, nil, err
		}

		bidBlocks := collectClobSmokeBlocking(status, selection, bidPayload)
		askBlocks := collectClobSmokeBlocking(status, selection, askPayload)
		if settleErr := validateClobSettleableQuantity(bidPayload); settleErr != nil {
			bidBlocks = append(bidBlocks, settleErr.Error())
		}
		if settleErr := validateClobSettleableQuantity(askPayload); settleErr != nil {
			askBlocks = append(askBlocks, settleErr.Error())
		}

		bidMakerAmount, err := evm.ParseBigInt(bidPayload.MakerAmount)
		if err != nil {
			return nil, nil, err
		}
		askMakerAmount, err := evm.ParseBigInt(askPayload.MakerAmount)
		if err != nil {
			return nil, nil, err
		}

		if remainingCollateral.Cmp(bidMakerAmount) < 0 {
			bidBlocks = append(bidBlocks, fmt.Sprintf("aggregate collateral balance %s wei is below cumulative required maker amount %s", remainingCollateral.String(), bidPayload.MakerAmount))
		}
		if remainingBidAllowance.Cmp(bidMakerAmount) < 0 {
			bidBlocks = append(bidBlocks, fmt.Sprintf("aggregate collateral allowance %s wei is below cumulative required maker amount %s", remainingBidAllowance.String(), bidPayload.MakerAmount))
		}
		if !status.OutcomeApprovalForAll {
			askBlocks = append(askBlocks, "outcome-token approval-for-all is not enabled")
		}
		if remainingAskInventory.Cmp(askMakerAmount) < 0 {
			askBlocks = append(askBlocks, fmt.Sprintf("aggregate displayed-side balance %s is below cumulative required maker amount %s", remainingAskInventory.String(), askPayload.MakerAmount))
		}

		if len(bidBlocks) > 0 {
			approvalTxs, approveErr := ensureClobOrderApprovals(cmd.Context(), ctx.Config.EVMRPCURL, privateKeyHex, wallet.Address(), status, selection, bidPayload, true)
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
			bidBlocks = filterAggregateBlocks(collectClobSmokeBlockingAfterApprovals(status, selection, bidPayload), "collateral allowance")
			if remainingCollateral.Cmp(bidMakerAmount) < 0 {
				bidBlocks = append(bidBlocks, fmt.Sprintf("aggregate collateral balance %s wei is below cumulative required maker amount %s", remainingCollateral.String(), bidPayload.MakerAmount))
			}
		}

		if len(askBlocks) > 0 {
			approvalTxs, approveErr := ensureClobOrderApprovals(cmd.Context(), ctx.Config.EVMRPCURL, privateKeyHex, wallet.Address(), status, selection, askPayload, true)
			if approveErr != nil {
				return nil, nil, fmt.Errorf("auto-approve ask ladder level %d prerequisites: %w", index+1, approveErr)
			}
			if len(approvalTxs) > 0 {
				approvals = append(approvals, approvalTxs...)
				status.OutcomeApprovalForAll = true
			}
			askBlocks = filterAggregateBlocks(collectClobSmokeBlockingAfterApprovals(status, selection, askPayload), "displayed-side balance")
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
				"quantity":       level.Quantity,
				"makerAmount":    level.BidPayload.MakerAmount,
				"takerAmount":    level.BidPayload.TakerAmount,
				"expiration":     level.BidPayload.Expiration,
				"nonce":          level.BidPayload.Nonce,
				"tokenSide":      level.BidPayload.TokenSide,
				"outcomeTokenId": level.BidPayload.OutcomeTokenID,
				"blocking":       level.BidBlocks,
			},
			"ask": map[string]any{
				"priceBps":       level.AskPriceBps,
				"quantity":       level.Quantity,
				"makerAmount":    level.AskPayload.MakerAmount,
				"takerAmount":    level.AskPayload.TakerAmount,
				"expiration":     level.AskPayload.Expiration,
				"nonce":          level.AskPayload.Nonce,
				"tokenSide":      level.AskPayload.TokenSide,
				"outcomeTokenId": level.AskPayload.OutcomeTokenID,
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
		bidResponse, err := submitClobSmokeOrderWithRetry(cmd.Context(), ctx, eventstoreURL, level.BidPayload)
		if err != nil {
			if rollbackErr := rollbackSubmittedLadderOrders(cmd, ctx, wallet, signingDomain, market.ID, submitted); rollbackErr != nil {
				return nil, fmt.Errorf("submit ladder bid level %d: %w (rollback failed: %v)", level.Index, err, rollbackErr)
			}
			return nil, fmt.Errorf("submit ladder bid level %d: %w", level.Index, err)
		}
		submitted = append(submitted, submittedLadderOrder{OrderID: bidResponse.OrderID, OutcomeIndex: selection.Binding.OutcomeIndex, TokenSide: selection.DisplayedSide})

		askResponse, err := submitClobSmokeOrderWithRetry(cmd.Context(), ctx, eventstoreURL, level.AskPayload)
		if err != nil {
			if rollbackErr := rollbackSubmittedLadderOrders(cmd, ctx, wallet, signingDomain, market.ID, submitted); rollbackErr != nil {
				return nil, fmt.Errorf("submit ladder ask level %d: %w (rollback failed: %v)", level.Index, err, rollbackErr)
			}
			return nil, fmt.Errorf("submit ladder ask level %d: %w", level.Index, err)
		}
		submitted = append(submitted, submittedLadderOrder{OrderID: askResponse.OrderID, OutcomeIndex: selection.Binding.OutcomeIndex, TokenSide: selection.DisplayedSide})

		items = append(items, map[string]any{
			"level":    level.Index,
			"quantity": level.Quantity,
			"bid": map[string]any{
				"priceBps":        level.BidPriceBps,
				"orderId":         bidResponse.OrderID,
				"tradeCount":      bidResponse.TradeCount,
				"remainingShares": bidResponse.RemainingQuantity,
				"resting":         bidResponse.WasAddedToBook,
			},
			"ask": map[string]any{
				"priceBps":        level.AskPriceBps,
				"orderId":         askResponse.OrderID,
				"tradeCount":      askResponse.TradeCount,
				"remainingShares": askResponse.RemainingQuantity,
				"resting":         askResponse.WasAddedToBook,
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
