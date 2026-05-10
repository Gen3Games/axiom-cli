package main

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"time"

	"github.com/Gen3Games/axiom-cli/internal/api"
	"github.com/Gen3Games/axiom-cli/internal/evm"
	"github.com/ethereum/go-ethereum/common"
	"github.com/spf13/cobra"
)

const defaultClobSmokeMarketID = "global-temperature-record-2030-1773930093244-1773930093244"

type clobSmokeWalletResult struct {
	Account  string            `json:"account"`
	Address  string            `json:"address"`
	Status   *clobWalletStatus `json:"status"`
	Blocking []string          `json:"blocking,omitempty"`
}

func newClobSmokeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "smoke [market-id-or-address]",
		Short: "Run a hosted CLOB smoke test using imported CLI accounts",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}

			marketID := defaultClobSmokeMarketID
			if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
				marketID = strings.TrimSpace(args[0])
			}

			market, err := loadMarketWithClobFallback(cmd.Context(), ctx, marketID, mustStringFlag(cmd, "instance-date"))
			if err != nil {
				return err
			}
			if !isClobMarketImplementation(market.MarketImplementation) {
				return errors.New("clob smoke requires an AxiomCTFMarket logical market")
			}

			exchangeAddress := resolveHexAddressOrDefault(mustStringFlag(cmd, "exchange-address"), evm.DefaultClobExchangeAddress)
			outcomeToken := resolveHexAddressOrDefault(mustStringFlag(cmd, "outcome-token-address"), evm.DefaultClobConditionalTokens)

			primaryWallet, primaryPrivateKey, err := requireEVMWalletWithKey(ctx)
			if err != nil {
				return err
			}

			secondaryAccount := strings.TrimSpace(mustStringFlag(cmd, "secondary-account"))

			selection, err := resolveClobSelection(
				market,
				firstNonEmpty(strings.TrimSpace(mustStringFlag(cmd, "outcome")), defaultClobSmokeOutcome(market)),
				firstNonEmpty(strings.TrimSpace(mustStringFlag(cmd, "label")), defaultClobSmokeLabel(market)),
				mustStringFlag(cmd, "displayed-side"),
				mustStringFlag(cmd, "exchange-address"),
				mustStringFlag(cmd, "outcome-token-address"),
			)
			if err != nil {
				return err
			}

			projectionURL := strings.TrimSpace(mustStringFlag(cmd, "projection-url"))
			book, err := ctx.API.GetClobBook(cmd.Context(), projectionURL, market.ID, selection.Binding.OutcomeIndex, selection.DisplayedSide)
			if err != nil {
				return err
			}
			depth, err := ctx.API.GetClobDepth(cmd.Context(), projectionURL, market.ID, selection.Binding.OutcomeIndex, selection.DisplayedSide)
			if err != nil {
				return err
			}

			primaryStatus, err := buildClobWalletStatus(cmd.Context(), ctx, market, primaryWallet.Address(), exchangeAddress, outcomeToken)
			if err != nil {
				return err
			}

			side := strings.ToLower(strings.TrimSpace(mustStringFlag(cmd, "side")))
			orderType := strings.ToLower(strings.TrimSpace(mustStringFlag(cmd, "type")))
			quantity, err := parseClobQuantity(firstNonEmpty(strings.TrimSpace(mustStringFlag(cmd, "quantity")), "1"))
			if err != nil {
				return err
			}
			priceBps := 0
			if orderType != "market" {
				priceBps, err = parseClobPriceToBps(firstNonEmpty(strings.TrimSpace(mustStringFlag(cmd, "price")), "1"))
				if err != nil {
					return err
				}
			}
			signingDomain, err := resolveClobSigningDomain(cmd)
			if err != nil {
				return err
			}

			payload, err := buildClobSignedOrder(
				primaryWallet,
				market.ID,
				selection,
				side,
				orderType,
				priceBps,
				quantity,
				mustStringFlag(cmd, "expiry"),
				signingDomain,
			)
			if err != nil {
				return err
			}

			result := map[string]any{
				"mode":        "dry-run",
				"marketId":    market.ID,
				"marketTitle": market.Title,
				"selection": map[string]any{
					"outcomeIndex":    selection.Binding.OutcomeIndex,
					"outcomeLabel":    selection.LogicalOutcome.Label,
					"displayedSide":   selection.DisplayedSide,
					"bindingAddress":  selection.Binding.ContractAddress,
					"outcomeTokenId":  selection.DisplayedTokenIDRaw,
					"collateralToken": selection.CollateralToken.Hex(),
					"outcomeToken":    selection.OutcomeToken.Hex(),
				},
				"book":  book,
				"depth": depth,
				"primaryWallet": clobSmokeWalletResult{
					Account:  ctx.ProfileName,
					Address:  primaryWallet.Address().Hex(),
					Status:   primaryStatus,
					Blocking: collectClobSmokeBlocking(primaryStatus, selection, payload),
				},
				"order": map[string]any{
					"side":            side,
					"orderType":       orderType,
					"priceBps":        priceBps,
					"quantity":        quantity,
					"maker":           payload.Maker,
					"makerAmount":     payload.MakerAmount,
					"takerAmount":     payload.TakerAmount,
					"expiration":      payload.Expiration,
					"nonce":           payload.Nonce,
					"collateralToken": payload.CollateralToken,
					"outcomeToken":    payload.OutcomeToken,
					"outcomeTokenId":  payload.OutcomeTokenID,
				},
			}

			if secondaryAccount != "" {
				secondaryWallet, _, secondaryErr := requireEVMWalletWithKeyForProfile(ctx.Config, secondaryAccount)
				if secondaryErr != nil {
					return secondaryErr
				}
				if secondaryWallet.Address() == primaryWallet.Address() {
					return errors.New("--secondary-account must refer to a different imported account")
				}
				secondaryStatus, statusErr := buildClobWalletStatus(cmd.Context(), ctx, market, secondaryWallet.Address(), exchangeAddress, outcomeToken)
				if statusErr != nil {
					return statusErr
				}
				result["secondaryWallet"] = clobSmokeWalletResult{
					Account:  secondaryAccount,
					Address:  secondaryWallet.Address().Hex(),
					Status:   secondaryStatus,
					Blocking: collectClobSmokeBlocking(secondaryStatus, selection, payload),
				}
			}

			if !mustBoolFlag(cmd, "live") {
				return printOutput(ctx.JSON, result)
			}

			blocking := collectClobSmokeBlocking(primaryStatus, selection, payload)
			approvalTxs, err := ensureClobSmokeApprovals(cmd, ctx, primaryPrivateKey, primaryWallet.Address(), primaryStatus, selection, payload)
			if err != nil {
				return err
			}
			if len(approvalTxs) > 0 {
				result["approvals"] = approvalTxs
				blocking = collectClobSmokeBlockingAfterApprovals(primaryStatus, selection, payload)
			}
			if len(blocking) > 0 {
				return fmt.Errorf("clob smoke live checks failed: %s", strings.Join(blocking, "; "))
			}

			response, err := submitClobSmokeOrderWithRetry(
				cmd.Context(),
				ctx,
				strings.TrimSpace(mustStringFlag(cmd, "eventstore-url")),
				payload,
			)
			if err != nil {
				return err
			}
			result["mode"] = "live"
			result["submission"] = map[string]any{
				"orderId":         response.OrderID,
				"tradeCount":      response.TradeCount,
				"remainingShares": response.RemainingQuantity,
				"resting":         response.WasAddedToBook,
				"message":         describeClobOrderResult(orderType, response),
			}

			order, err := ctx.API.GetClobOrder(cmd.Context(), projectionURL, response.OrderID)
			if err != nil {
				return err
			}
			result["fetchedOrder"] = order

			orderFilters := url.Values{}
			orderFilters.Set("maker", primaryWallet.Address().Hex())
			orderFilters.Set("clob_id", clobIDForMarketOutcome(market.ID, selection.Binding.OutcomeIndex, selection.DisplayedSide))
			orderFilters.Set("token_side", selection.DisplayedSide)
			orders, err := ctx.API.ListClobOrders(cmd.Context(), projectionURL, orderFilters)
			if err != nil {
				return err
			}
			result["orders"] = map[string]any{"items": orders, "total": len(orders)}

			fillFilters := url.Values{}
			fillFilters.Set("wallet", primaryWallet.Address().Hex())
			fillFilters.Set("clob_id", clobIDForMarketOutcome(market.ID, selection.Binding.OutcomeIndex, selection.DisplayedSide))
			fillFilters.Set("token_side", selection.DisplayedSide)
			fills, err := ctx.API.ListClobFills(cmd.Context(), projectionURL, fillFilters)
			if err != nil {
				return err
			}
			result["fills"] = map[string]any{"items": fills, "total": len(fills)}

			if response.WasAddedToBook && response.RemainingQuantity > 0 && !mustBoolFlag(cmd, "keep-order") {
				order, err := ctx.API.GetClobOrder(cmd.Context(), projectionURL, response.OrderID)
				if err != nil {
					return err
				}
				cancelRequest, err := buildSignedClobCancel(primaryWallet, signingDomain, response.OrderID, market.ID, selection.Binding.OutcomeIndex, order.TokenSide, primaryWallet.Address().Hex(), "clob-smoke-cleanup")
				if err != nil {
					return err
				}
				cancelResponse, err := ctx.API.CancelClobOrder(cmd.Context(), strings.TrimSpace(mustStringFlag(cmd, "eventstore-url")), response.OrderID, cancelRequest)
				if err != nil {
					return err
				}
				result["cancel"] = cancelResponse
			}

			return printOutput(ctx.JSON, result)
		},
	}

	cmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	cmd.Flags().String("outcome", "", "Logical outcome index to smoke test; defaults to the first logical outcome")
	cmd.Flags().String("label", "", "Logical outcome label to smoke test; defaults to the first logical outcome label")
	cmd.Flags().String("displayed-side", "", "Displayed side to trade: yes or no; inferred for single-binding binary markets")
	cmd.Flags().String("side", "buy", "Order side: buy or sell")
	cmd.Flags().String("type", "limit", "Order type: limit, market, ioc, fok")
	cmd.Flags().String("price", "1", "Limit price in displayed percent units; defaults to a low-impact 1 bps test price")
	cmd.Flags().String("quantity", "1", "Whole-number share quantity; defaults to 1")
	cmd.Flags().String("expiry", "24h", "Expiry preset: 1h, 24h, 7d, never")
	cmd.Flags().String("secondary-account", "", "Optional second imported CLI account to inspect alongside the active account")
	cmd.Flags().Bool("live", false, "Submit the signed order to the hosted eventstore and optionally cancel cleanup")
	cmd.Flags().Bool("auto-approve", false, "Submit missing approvals automatically before live order placement")
	cmd.Flags().Bool("wait", false, "Wait for approval transaction receipts when --auto-approve submits transactions")
	cmd.Flags().Bool("keep-order", false, "Leave a resting live smoke order on the book instead of canceling it")

	return cmd
}

const (
	clobSmokeSubmitMaxAttempts = 5
	clobSmokeSubmitRetryDelay  = 400 * time.Millisecond
)

func submitClobSmokeOrderWithRetry(ctx context.Context, cliCtx *cliContext, eventstoreURL string, payload api.ClobSignedOrderPayload) (*api.ClobOrderResponse, error) {
	var lastErr error
	for attempt := 1; attempt <= clobSmokeSubmitMaxAttempts; attempt++ {
		response, err := cliCtx.API.SubmitClobOrder(ctx, eventstoreURL, payload)
		if err == nil {
			return response, nil
		}
		lastErr = err
		if !isRetryableClobSubmitConflict(err) || attempt == clobSmokeSubmitMaxAttempts {
			return nil, err
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt) * clobSmokeSubmitRetryDelay):
		}
	}

	return nil, lastErr
}

func isRetryableClobSubmitConflict(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "api error (422)") && strings.Contains(message, "version conflict")
}

func defaultClobSmokeOutcome(market *api.MarketDetails) string {
	if market == nil || len(market.Outcomes) == 0 {
		return "0"
	}
	return fmt.Sprintf("%d", market.Outcomes[0].Index)
}

func defaultClobSmokeLabel(market *api.MarketDetails) string {
	if market == nil || len(market.Outcomes) == 0 {
		return ""
	}
	return market.Outcomes[0].Label
}

func collectClobSmokeBlocking(status *clobWalletStatus, selection *clobSelection, payload api.ClobSignedOrderPayload) []string {
	return collectClobSmokeBlockingWithAssumptions(status, selection, payload, false)
}

func collectClobSmokeBlockingAfterApprovals(status *clobWalletStatus, selection *clobSelection, payload api.ClobSignedOrderPayload) []string {
	return collectClobSmokeBlockingWithAssumptions(status, selection, payload, true)
}

func collectClobSmokeBlockingWithAssumptions(status *clobWalletStatus, selection *clobSelection, payload api.ClobSignedOrderPayload, assumeApprovalsApplied bool) []string {
	if status == nil || selection == nil {
		return []string{"wallet status could not be loaded"}
	}

	blocks := make([]string, 0)
	makerAmount, err := evm.ParseBigInt(payload.MakerAmount)
	if err != nil {
		return []string{fmt.Sprintf("maker amount is invalid: %v", err)}
	}

	if payload.Side == clobOrderSideValue["buy"] {
		collateralBalance, parseBalanceErr := evm.ParseBigInt(status.CollateralBalanceWei)
		if parseBalanceErr != nil {
			return []string{fmt.Sprintf("collateral balance is invalid: %v", parseBalanceErr)}
		}
		if collateralBalance.Cmp(makerAmount) < 0 {
			blocks = append(blocks, fmt.Sprintf("collateral balance %s wei is below required maker amount %s", status.CollateralBalanceWei, payload.MakerAmount))
		}
		if !assumeApprovalsApplied {
			collateralAllowance, parseAllowanceErr := evm.ParseBigInt(status.CollateralAllowanceWei)
			if parseAllowanceErr != nil {
				return []string{fmt.Sprintf("collateral allowance is invalid: %v", parseAllowanceErr)}
			}
			if collateralAllowance.Cmp(makerAmount) < 0 {
				blocks = append(blocks, fmt.Sprintf("collateral allowance %s wei is below required maker amount %s", status.CollateralAllowanceWei, payload.MakerAmount))
			}
		}
		return blocks
	}

	if !assumeApprovalsApplied && !status.OutcomeApprovalForAll {
		blocks = append(blocks, "outcome-token approval-for-all is not enabled")
	}
	positionBalance, balanceFound, balanceErr := clobSmokeDisplayedSideBalance(status, selection.Binding.OutcomeIndex, selection.DisplayedSide)
	if balanceErr != nil {
		return []string{balanceErr.Error()}
	}
	if !balanceFound {
		return []string{"selected displayed side balance is not present in wallet status"}
	}
	if positionBalance.Cmp(makerAmount) < 0 {
		blocks = append(blocks, fmt.Sprintf("displayed-side balance %s is below required maker amount %s", positionBalance.String(), payload.MakerAmount))
	}
	return blocks
}

func clobSmokeDisplayedSideBalance(status *clobWalletStatus, outcomeIndex int, displayedSide string) (*big.Int, bool, error) {
	for _, binding := range status.Bindings {
		if binding.OutcomeIndex != outcomeIndex {
			continue
		}
		for _, side := range binding.Sides {
			if !strings.EqualFold(side.DisplayedSide, displayedSide) {
				continue
			}
			balance, err := evm.ParseBigInt(side.Balance)
			if err != nil {
				return nil, false, fmt.Errorf("displayed-side balance is invalid: %w", err)
			}
			return balance, true, nil
		}
	}
	return nil, false, nil
}

func ensureClobSmokeApprovals(cmd *cobra.Command, ctx *cliContext, privateKeyHex string, walletAddress common.Address, status *clobWalletStatus, selection *clobSelection, payload api.ClobSignedOrderPayload) ([]map[string]any, error) {
	if !mustBoolFlag(cmd, "auto-approve") {
		return nil, nil
	}

	transactions := make([]map[string]any, 0, 2)
	makerAmount, err := evm.ParseBigInt(payload.MakerAmount)
	if err != nil {
		return nil, err
	}

	if payload.Side == clobOrderSideValue["buy"] {
		allowance, err := evm.ParseBigInt(status.CollateralAllowanceWei)
		if err != nil {
			return nil, err
		}
		if allowance.Cmp(makerAmount) < 0 {
			approveAmount, parseErr := evm.ParseBigInt(clobMaxUint256)
			if parseErr != nil {
				return nil, parseErr
			}
			txHash, approveErr := approveERC20(cmd.Context(), ctx.Config.EVMRPCURL, big.NewInt(xrplEVMChainID), privateKeyHex, selection.CollateralToken, selection.ExchangeAddress, approveAmount)
			if approveErr != nil {
				return nil, approveErr
			}
			entry := map[string]any{
				"kind":          "collateral-approve",
				"walletAddress": walletAddress.Hex(),
				"token":         selection.CollateralToken.Hex(),
				"spender":       selection.ExchangeAddress.Hex(),
				"txHash":        txHash.Hex(),
			}
			if mustBoolFlag(cmd, "wait") {
				receipt, waitErr := waitForTxReceipt(cmd.Context(), ctx.Config.EVMRPCURL, txHash)
				if waitErr != nil {
					return nil, waitErr
				}
				entry["receiptStatus"] = receipt.Status
			}
			transactions = append(transactions, entry)
		}
		return transactions, nil
	}

	if !status.OutcomeApprovalForAll {
		txHash, approveErr := setERC1155ApprovalForAll(cmd.Context(), ctx.Config.EVMRPCURL, big.NewInt(xrplEVMChainID), privateKeyHex, selection.OutcomeToken, selection.ExchangeAddress, true)
		if approveErr != nil {
			return nil, approveErr
		}
		entry := map[string]any{
			"kind":          "outcome-approval-for-all",
			"walletAddress": walletAddress.Hex(),
			"token":         selection.OutcomeToken.Hex(),
			"operator":      selection.ExchangeAddress.Hex(),
			"approved":      true,
			"txHash":        txHash.Hex(),
		}
		if mustBoolFlag(cmd, "wait") {
			receipt, waitErr := waitForTxReceipt(cmd.Context(), ctx.Config.EVMRPCURL, txHash)
			if waitErr != nil {
				return nil, waitErr
			}
			entry["receiptStatus"] = receipt.Status
		}
		transactions = append(transactions, entry)
	}

	return transactions, nil
}
