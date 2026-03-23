package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Gen3Games/axiom-cli/internal/api"
	"github.com/Gen3Games/axiom-cli/internal/app"
	"github.com/Gen3Games/axiom-cli/internal/evm"
)

func Render(value any) string {
	switch typed := value.(type) {
	case *app.Config:
		return renderConfig(*typed)
	case app.Config:
		return renderConfig(typed)
	case *api.RegisterResponse:
		return renderRegister(*typed)
	case api.RegisterResponse:
		return renderRegister(typed)
	case *api.MarketsResponse:
		return renderMarkets(*typed)
	case api.MarketsResponse:
		return renderMarkets(typed)
	case *api.MarketDetails:
		return renderMarketDetails(*typed)
	case api.MarketDetails:
		return renderMarketDetails(typed)
	case *api.ProfileSummary:
		return renderProfile(*typed)
	case api.ProfileSummary:
		return renderProfile(typed)
	case *api.PositionsResponse:
		return renderPositions(*typed)
	case api.PositionsResponse:
		return renderPositions(typed)
	case *api.UnclaimedResponse:
		return renderUnclaimed(*typed)
	case api.UnclaimedResponse:
		return renderUnclaimed(typed)
	case *api.FundingResponse:
		return renderFunding(*typed)
	case api.FundingResponse:
		return renderFunding(typed)
	case *api.RewardsResponse:
		return renderRewards(*typed)
	case api.RewardsResponse:
		return renderRewards(typed)
	case *evm.BuyQuote:
		return renderBuyQuote(*typed)
	case evm.BuyQuote:
		return renderBuyQuote(typed)
	case map[string]any:
		return renderMap(typed)
	default:
		return fmt.Sprintf("%v", value)
	}
}

func renderConfig(cfg app.Config) string {
	lines := []string{
		heading("CLI Configuration"),
		renderKeyValueRows([][2]string{
			{"API Base URL", cfg.APIBaseURL},
			{"XRPL EVM RPC", cfg.EVMRPCURL},
			{"XRPL RPC", cfg.XRPLRPCURL},
			{"Active Profile", cfg.ActiveProfile},
			{"Output Format", cfg.OutputFormat},
		}),
	}

	profileNames := make([]string, 0, len(cfg.Profiles))
	for name := range cfg.Profiles {
		profileNames = append(profileNames, name)
	}
	sort.Strings(profileNames)
	rows := make([][]string, 0, len(profileNames))
	for _, name := range profileNames {
		profile := cfg.Profiles[name]
		rows = append(rows, []string{name, profile.EVMAddress, profile.XRPLAddress, intString(profile.DepositDestinationTag)})
	}
	lines = append(lines, "", heading("Profiles"), renderTable([]string{"Profile", "EVM", "XRPL", "Tag"}, rows))
	return strings.Join(lines, "\n")
}

func renderRegister(response api.RegisterResponse) string {
	status := "refreshed"
	if response.Created {
		status = "created"
	}
	return strings.Join([]string{
		heading("CLI Registration"),
		renderKeyValueRows([][2]string{
			{"Wallet", response.WalletAddress},
			{"Profile", response.DisplayName},
			{"Destination Tag", fmt.Sprintf("%d", response.DepositDestinationTag)},
			{"Status", status},
		}),
	}, "\n")
}

func renderMarkets(response api.MarketsResponse) string {
	if len(response.Items) == 0 {
		return heading("Markets") + "\nNo markets found."
	}
	showSpotPrices := false
	for _, item := range response.Items {
		if len(item.CurrentSpotPrices) > 0 {
			showSpotPrices = true
			break
		}
	}
	headers := []string{"Title", "Status", "Category", "Closes", "Outcomes"}
	if showSpotPrices {
		headers = append(headers, "Spot Odds")
	}
	headers = append(headers, "Identifier")
	rows := make([][]string, 0, len(response.Items))
	for _, item := range response.Items {
		row := []string{
			truncate(item.Title, 40),
			item.Status,
			truncate(item.Category, 14),
			formatTime(item.EndsAt),
			truncate(joinOutcomeLabels(item.Outcomes), 28),
		}
		if showSpotPrices {
			row = append(row, truncate(joinOutcomeSpotPrices(item.CurrentSpotPrices), 28))
		}
		row = append(row, firstNonEmpty(item.ContractAddress, item.ID))
		rows = append(rows, row)
	}
	return strings.Join([]string{
		heading(fmt.Sprintf("Markets (%d total)", response.Total)),
		renderTable(headers, rows),
	}, "\n")
}

func renderMarketDetails(market api.MarketDetails) string {
	lines := []string{
		heading(market.Title),
		renderKeyValueRows([][2]string{
			{"Identifier", firstNonEmpty(market.ContractAddress, market.ID)},
			{"Status", market.Status},
			{"Category", market.Category},
			{"Market Type", market.MarketType},
			{"Starts", formatTime(market.StartsAt)},
			{"Ends", formatTime(market.EndsAt)},
			{"Creator", market.Creator},
			{"Settlement Token", market.SettlementToken},
		}),
	}
	if market.Headline != "" {
		lines = append(lines, "", "Headline: "+market.Headline)
	}
	if market.Description != "" {
		lines = append(lines, "", "Description: "+market.Description)
	}
	if len(market.Outcomes) > 0 {
		rows := make([][]string, 0, len(market.Outcomes))
		for _, outcome := range market.Outcomes {
			rows = append(rows, []string{fmt.Sprintf("%d", outcome.Index), outcome.Label, truncate(outcome.Description, 48)})
		}
		lines = append(lines, "", heading("Outcomes"), renderTable([]string{"Index", "Label", "Description"}, rows))
	}
	if market.PoolBreakdown != nil && len(market.PoolBreakdown.Outcomes) > 0 {
		lines = append(lines, "", heading("Pool Breakdown"), renderKeyValueRows([][2]string{
			{"Total Pool", market.PoolBreakdown.TotalPoolXRP + " XRP"},
			{"Max Time Bonus", formatMaxTimeBonus(market.PoolBreakdown.MaxTimeBonus)},
		}))
		rows := make([][]string, 0, len(market.PoolBreakdown.Outcomes))
		for _, outcome := range market.PoolBreakdown.Outcomes {
			rows = append(rows, []string{
				fmt.Sprintf("%d", outcome.Index),
				outcome.Label,
				outcome.PoolXRP,
				outcome.SpotPrice,
			})
		}
		lines = append(lines, renderTable([]string{"Index", "Label", "Pool XRP", "Spot Odds"}, rows))
	}
	return strings.Join(lines, "\n")
}

func formatMaxTimeBonus(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "0" || trimmed == "0.0" || trimmed == "0.00" || trimmed == "0.000000000000000000" {
		return "Disabled"
	}
	if strings.HasSuffix(trimmed, "x") {
		return trimmed
	}
	return trimmed + "x"
}

func renderProfile(profile api.ProfileSummary) string {
	stats := profile.Stats
	return strings.Join([]string{
		heading("Profile"),
		renderKeyValueRows([][2]string{
			{"Wallet", profile.WalletAddress},
			{"Display Name", profile.DisplayName},
			{"Avatar URL", profile.AvatarURL},
			{"Destination Tag", optionalInt(profile.DepositDestinationTag)},
			{"Member Since", optionalTime(profile.MemberSince)},
			{"Last Login", optionalTime(profile.LastLoginAt)},
		}),
		"",
		heading("Performance"),
		renderKeyValueRows([][2]string{
			{"Predictions", fmt.Sprintf("%d", stats.TotalPredictions)},
			{"Resolved Markets", fmt.Sprintf("%d", stats.ResolvedMarkets)},
			{"Open Markets", fmt.Sprintf("%d", stats.OpenMarkets)},
			{"Win Rate", formatFloat(stats.WinRate, 2) + "%"},
			{"Volume", "$" + formatFloat(stats.VolumeUSD, 2)},
			{"PnL", "$" + formatFloat(stats.PnlUSD, 2)},
			{"PnL %", formatFloat(stats.PnlPercent, 2) + "%"},
			{"Unclaimed Payout", "$" + stats.UnclaimedPayoutUSD},
			{"Unclaimed PnL", "$" + stats.UnclaimedPnlUSD},
			{"Leaderboard Rank", optionalInt(stats.LeaderboardRank)},
		}),
	}, "\n")
}

func joinOutcomeSpotPrices(values []api.OutcomeSpotPrice) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, value.Label+" "+value.CurrentSpotPrice)
	}
	return strings.Join(parts, ", ")
}

func renderPositions(response api.PositionsResponse) string {
	if len(response.Items) == 0 {
		return heading("Positions") + "\nNo positions found."
	}
	rows := make([][]string, 0, len(response.Items))
	for _, item := range response.Items {
		rows = append(rows, []string{
			truncate(item.Title, 36),
			item.Status,
			fmt.Sprintf("%d · %s", item.OutcomeIndex, truncate(item.OutcomeLabel, 18)),
			"$" + item.AmountUSD,
			item.Shares,
			formatTime(item.CreatedAt),
		})
	}
	return strings.Join([]string{
		heading(fmt.Sprintf("Positions (%d total)", response.Total)),
		renderTable([]string{"Market", "Status", "Outcome", "Stake", "Shares", "Opened"}, rows),
	}, "\n")
}

func renderUnclaimed(response api.UnclaimedResponse) string {
	lines := []string{
		heading("Unclaimed Winnings"),
		renderKeyValueRows([][2]string{
			{"Markets", fmt.Sprintf("%d", response.Summary.MarketCount)},
			{"Series", fmt.Sprintf("%d", response.Summary.SeriesCount)},
			{"Claimable Count", fmt.Sprintf("%d", response.Summary.TotalCount)},
			{"Total Payout", "$" + response.Summary.TotalUnclaimedPayoutUSD},
			{"Total PnL", "$" + response.Summary.TotalUnclaimedPnlUSD},
		}),
	}
	if len(response.Items) == 0 {
		lines = append(lines, "", "No unclaimed items found.")
		return strings.Join(lines, "\n")
	}
	rows := make([][]string, 0, len(response.Items))
	for _, item := range response.Items {
		rows = append(rows, []string{
			truncate(item.Title, 34),
			"$" + item.PayoutUSD,
			"$" + item.PnlUSD,
			fmt.Sprintf("%d", item.ResolvedOutcome),
			formatTime(item.ResolvedAt),
		})
	}
	lines = append(lines, "", renderTable([]string{"Market", "Payout", "PnL", "Winner", "Resolved"}, rows))
	return strings.Join(lines, "\n")
}

func renderFunding(response api.FundingResponse) string {
	lines := []string{
		heading("Funding"),
		renderKeyValueRows([][2]string{
			{"Wallet", response.WalletAddress},
			{"Deposit Wallet", response.DepositWalletAddress},
			{"Destination Tag", optionalInt(response.DepositDestinationTag)},
		}),
	}
	if len(response.Notes) > 0 {
		lines = append(lines, "", heading("Notes"))
		for _, note := range response.Notes {
			lines = append(lines, "• "+note)
		}
	}
	if len(response.RecentHistory) > 0 {
		rows := make([][]string, 0, len(response.RecentHistory))
		for _, item := range response.RecentHistory {
			rows = append(rows, []string{
				item.Kind,
				item.Status,
				item.AmountXRP,
				truncate(firstNonEmpty(item.BridgeTxHash, item.TxHash), 18),
				formatTime(item.CreatedAt),
			})
		}
		lines = append(lines, "", heading("Recent Bridge Activity"), renderTable([]string{"Kind", "Status", "XRP", "Tx", "Created"}, rows))
	}
	return strings.Join(lines, "\n")
}

func renderRewards(response api.RewardsResponse) string {
	lines := []string{
		heading("Rewards"),
		renderKeyValueRows([][2]string{{"Wallet", response.WalletAddress}}),
	}

	if response.Summary != nil {
		summary := response.Summary
		lines = append(lines, "", heading("Epoch Summary"), renderKeyValueRows([][2]string{
			{"Current Epoch", optionalInt(summary.CurrentEpochID)},
			{"Epoch Ends", optionalTime(summary.CurrentEpochEndsAt)},
			{"Axiom Points", fmt.Sprintf("%d", summary.CurrentEpochPoints)},
			{"Trading Points", fmt.Sprintf("%d", summary.TradingPoints)},
			{"Referral Points", fmt.Sprintf("%d", summary.ReferralPoints)},
			{"Bonus Points", fmt.Sprintf("%d", summary.BonusPoints)},
			{"Total Referrals", fmt.Sprintf("%d", summary.TotalReferrals)},
			{"Estimated Payout", optionalFloat(summary.EstimatedPayoutXRP, " XRP")},
			{"Pool Share", optionalFloat(summary.PoolSharePercentage, "%")},
			{"Claimable Epoch Rewards", response.TotalClaimableEpochRewardsXRP + " XRP"},
		}))
	}

	if response.DailyTasks != nil {
		tasks := response.DailyTasks
		lines = append(lines, "", heading("Daily Tasks"), renderKeyValueRows([][2]string{
			{"Completed", fmt.Sprintf("%d/%d", tasks.CompletedCount, tasks.RequiredCount)},
			{"Requirement Met", yesNo(tasks.HasCompletedRequirement)},
			{"Daily Chest Claimed", yesNo(tasks.DailyChestClaimed)},
		}))
		lines = append(lines, "", renderTable([]string{"Task", "Done", "Next Step"}, dailyTaskGuideRows(tasks)))
	}

	if response.Streak != nil {
		streak := response.Streak
		lines = append(lines, "", heading("Streak"), renderKeyValueRows([][2]string{
			{"Current Streak", fmt.Sprintf("%d", streak.CurrentStreak)},
			{"Longest Streak", fmt.Sprintf("%d", streak.LongestStreak)},
			{"Last Activity", optionalTime(streak.LastActivityDate)},
			{"Days Until Weekly Chest", fmt.Sprintf("%d", streak.DaysUntilLottery)},
			{"Available Weekly Ticket", yesNo(streak.HasAvailableLotteryTicket)},
		}))
	}

	if len(response.LotteryTickets) > 0 {
		rows := make([][]string, 0, len(response.LotteryTickets))
		for _, ticket := range response.LotteryTickets {
			rows = append(rows, []string{
				fmt.Sprintf("%d", ticket.ID),
				ticket.Status,
				firstNonEmpty(ticket.PrizeType, "-"),
				optionalInt(ticket.PrizeAmount),
				firstNonEmpty(ticket.PrizeLabel, "-"),
				formatTime(ticket.EarnedAt),
			})
		}
		lines = append(lines, "", heading("Weekly Chest Tickets"), renderTable([]string{"ID", "Status", "Prize Type", "Amount", "Label", "Earned"}, rows))
	}

	if len(response.EpochRewards) > 0 {
		rows := make([][]string, 0, len(response.EpochRewards))
		for _, reward := range response.EpochRewards {
			rows = append(rows, []string{
				fmt.Sprintf("%d", reward.EpochID),
				fmt.Sprintf("%d", reward.Points),
				reward.AmountXRP,
				epochRewardStatus(reward),
				formatTime(reward.DateEnded),
			})
		}
		lines = append(lines, "", heading("Epoch Rewards"), renderTable([]string{"Epoch", "Points", "XRP", "Status", "Ended"}, rows))
	}

	return strings.Join(lines, "\n")
}

func dailyTaskGuideRows(tasks *api.DailyTaskStatus) [][]string {
	return [][]string{
		{"Predict using $5+", yesNo(tasks.HasPredictTask), "Run axiom markets list, then axiom predict buy"},
		{"Post on X tagging @AxiomProtocol_", yesNo(tasks.HasDailyTwitterPostTask), "Complete this task in the web app rewards view"},
		{"Place a bet of $10+ on a single outcome", yesNo(tasks.HasBigBetTask), "Use axiom predict buy with a $10+ position"},
		{"Claim winnings from a resolved market", yesNo(tasks.HasClaimWinningsTask), "Run axiom profile unclaimed, then axiom claim batch"},
		{"Place bets of $1+ on 3+ different markets", yesNo(tasks.HasMultiMarketTask), "Use axiom predict buy on 3 separate markets"},
	}
}

func renderBuyQuote(quote evm.BuyQuote) string {
	return strings.Join([]string{
		heading("Predict Quote"),
		renderKeyValueRows([][2]string{
			{"Market", quote.MarketTitle},
			{"Outcome", fmt.Sprintf("%d · %s", quote.OutcomeIndex, quote.OutcomeLabel)},
			{"Amount", quote.AmountXRP + " XRP"},
			{"Time Bonus", quote.TimeBonusMultiplier},
			{"Spot Price", quote.CurrentSpotPrice},
			{"Avg Entry Price", quote.AverageEntryPrice},
			{"Post-Trade Spot", quote.PostTradeSpotPrice},
			{"Base Shares", quote.BaseShares},
			{"Weighted Shares", quote.WeightedShares},
			{"Suggested Min Shares (wei)", quote.SuggestedMinShares},
			{"Est. Gross Payout If Wins Now", quote.GrossPayoutIfWinNowXRP + " XRP"},
			{"Est. Profit If Wins Now", quote.ProfitIfWinNowXRP + " XRP"},
		}),
		"",
		note("Estimates use the current pool state and assume the market resolved immediately after this buy with no further bets or fees changes."),
	}, "\n")
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func optionalFloat(value *float64, suffix string) string {
	if value == nil {
		return "-"
	}
	return formatFloat(*value, 2) + suffix
}

func epochRewardStatus(reward api.EpochReward) string {
	if reward.HasClaimed {
		return "claimed"
	}
	if reward.Claimable {
		return "claimable"
	}
	if reward.IsExpired {
		return "expired"
	}
	return "pending"
}

func renderMap(values map[string]any) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := make([][2]string, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, [2]string{humanizeKey(key), renderAny(values[key])})
	}
	return renderKeyValueRows(rows)
}

func renderKeyValueRows(rows [][2]string) string {
	maxKey := 0
	for _, row := range rows {
		if len(row[0]) > maxKey {
			maxKey = len(row[0])
		}
	}
	var b strings.Builder
	for i, row := range rows {
		b.WriteString(fmt.Sprintf("%-*s  %s", maxKey, row[0], sanitize(row[1])))
		if i < len(rows)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

func renderTable(headers []string, rows [][]string) string {
	if len(headers) == 0 {
		return ""
	}
	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = len(sanitize(header))
	}
	for _, row := range rows {
		for i := range headers {
			cell := ""
			if i < len(row) {
				cell = sanitize(row[i])
			}
			if len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	var b strings.Builder
	writeBorder(&b, widths, "┌", "┬", "┐")
	writeRow(&b, headers, widths)
	writeBorder(&b, widths, "├", "┼", "┤")
	for index, row := range rows {
		cells := make([]string, len(headers))
		for i := range headers {
			if i < len(row) {
				cells[i] = row[i]
			}
		}
		writeRow(&b, cells, widths)
		if index < len(rows)-1 {
			writeBorder(&b, widths, "├", "┼", "┤")
		}
	}
	writeBorder(&b, widths, "└", "┴", "┘")
	return b.String()
}

func writeBorder(b *strings.Builder, widths []int, left string, middle string, right string) {
	b.WriteString(left)
	for i, width := range widths {
		b.WriteString(strings.Repeat("─", width+2))
		if i < len(widths)-1 {
			b.WriteString(middle)
		}
	}
	b.WriteString(right)
	b.WriteString("\n")
}

func writeRow(b *strings.Builder, cells []string, widths []int) {
	b.WriteString("│")
	for i, width := range widths {
		cell := ""
		if i < len(cells) {
			cell = sanitize(cells[i])
		}
		b.WriteString(" ")
		b.WriteString(cell)
		if padding := width - len(cell); padding > 0 {
			b.WriteString(strings.Repeat(" ", padding))
		}
		b.WriteString(" │")
	}
	b.WriteString("\n")
}

func heading(value string) string {
	return value + "\n" + strings.Repeat("=", len(value))
}

func note(value string) string {
	return "Note: " + value
}

func sanitize(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\n", " / "))
	if value == "" {
		return "—"
	}
	return value
}

func renderAny(value any) string {
	switch typed := value.(type) {
	case nil:
		return "—"
	case string:
		return typed
	case bool:
		if typed {
			return "yes"
		}
		return "no"
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	case float64:
		return formatFloat(typed, 4)
	case *int:
		return optionalInt(typed)
	case time.Time:
		return formatTime(typed)
	case *time.Time:
		return optionalTime(typed)
	case []string:
		return strings.Join(typed, ", ")
	default:
		return fmt.Sprintf("%v", typed)
	}
}

func truncate(value string, width int) string {
	clean := sanitize(value)
	if len(clean) <= width {
		return clean
	}
	if width <= 1 {
		return clean[:width]
	}
	return clean[:width-1] + "…"
}

func joinOutcomeLabels(outcomes []api.Outcome) string {
	labels := make([]string, 0, len(outcomes))
	for _, outcome := range outcomes {
		labels = append(labels, outcome.Label)
	}
	return strings.Join(labels, ", ")
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "—"
	}
	return value.Local().Format("2006-01-02 15:04")
}

func optionalTime(value *time.Time) string {
	if value == nil {
		return "—"
	}
	return formatTime(*value)
}

func optionalInt(value *int) string {
	if value == nil {
		return "—"
	}
	return fmt.Sprintf("%d", *value)
}

func intString(value int) string {
	if value == 0 {
		return "—"
	}
	return fmt.Sprintf("%d", value)
}

func humanizeKey(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Value"
	}
	var parts []string
	current := strings.Builder{}
	for index, r := range value {
		if index > 0 && r >= 'A' && r <= 'Z' {
			parts = append(parts, current.String())
			current.Reset()
		}
		if r == '_' || r == '-' {
			parts = append(parts, current.String())
			current.Reset()
			continue
		}
		current.WriteRune(r)
	}
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}
	for i, part := range parts {
		if part == "" {
			continue
		}
		switch strings.ToLower(part) {
		case "evm":
			parts[i] = "EVM"
		case "xrpl":
			parts[i] = "XRPL"
		case "xrp":
			parts[i] = "XRP"
		case "id":
			parts[i] = "ID"
		case "api":
			parts[i] = "API"
		case "url":
			parts[i] = "URL"
		default:
			parts[i] = strings.ToUpper(part[:1]) + part[1:]
		}
	}
	return strings.Join(parts, " ")
}

func formatFloat(value float64, precision int) string {
	return fmt.Sprintf("%.*f", precision, value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
