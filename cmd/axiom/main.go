package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Gen3Games/axiom-cli/internal/api"
	"github.com/Gen3Games/axiom-cli/internal/app"
	"github.com/Gen3Games/axiom-cli/internal/evm"
	"github.com/Gen3Games/axiom-cli/internal/ui"
	axrpl "github.com/Gen3Games/axiom-cli/internal/xrpl"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	qrterminal "github.com/mdp/qrterminal/v3"
	"github.com/spf13/cobra"
)

const xrplEVMChainID int64 = 1440000
const defaultClobProjectionURL = "https://clob.axiomprotocol.io"
const defaultClobEventstoreURL = "https://clob.axiomprotocol.io/api"

var (
	flagAPIURL               string
	flagRPCURL               string
	flagXRPLURL              string
	flagJSON                 bool
	flagProfile              string
	getEVMBalance            = evm.GetBalance
	getXRPLBalance           = axrpl.GetBalance
	loadMarketState          = evm.LoadMarketState
	quoteBuy                 = evm.QuoteBuy
	buyPosition              = evm.BuyPosition
	claimEpochRewards        = evm.ClaimRewards
	claimSingleMarket        = evm.ClaimMarket
	batchClaimMarkets        = evm.BatchClaim
	waitForTxReceipt         = waitForReceipt
	submitBridgePayment      = axrpl.SubmitBridgePayment
	getERC20Balance          = evm.GetERC20Balance
	getERC20Allowance        = evm.GetERC20Allowance
	approveERC20             = evm.ApproveERC20
	getERC1155Balance        = evm.GetERC1155Balance
	isERC1155ApprovedForAll  = evm.IsERC1155ApprovedForAll
	setERC1155ApprovalForAll = evm.SetERC1155ApprovalForAll
	loadCTFMarketMetadata    = evm.LoadCTFMarketMetadata
	redeemCTFMarket          = evm.RedeemCTFMarket
	splitPosition            = evm.SplitPosition
	mergePositions           = evm.MergePositions
)

type cliContext struct {
	Config      *app.Config
	API         *api.Client
	Profile     app.Profile
	ProfileName string
	JSON        bool
}

type bridgeFundingPreview struct {
	DepositWalletAddress string
	DestinationTag       int
	AmountXRP            string
	PaymentURI           string
	QRCode               string
	Instructions         []string
	Submit               bool
	TxHash               string
	FromXRPLWallet       string
}

func main() {
	rootCmd := newRootCommand()
	rootCmd.SetArgs(normalizeCLIArgs(os.Args[1:]))
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "axiom",
		Short:         "Axiom Protocol CLI for XRPL EVM users",
		Long:          "Axiom CLI manages XRPL EVM wallets, funding flows, market discovery, predictions, claims, and profile analytics.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		if rewritten := rewriteAmountFlagParseError(normalizeCLIArgs(os.Args[1:]), err); rewritten != nil {
			return rewritten
		}
		return err
	})

	rootCmd.PersistentFlags().StringVar(&flagAPIURL, "api-url", "", "Override the Axiom CLI API base URL (for example https://axiomprotocol.io/api/cli)")
	rootCmd.PersistentFlags().StringVar(&flagRPCURL, "rpc-url", "", "Override the XRPL EVM RPC URL")
	rootCmd.PersistentFlags().StringVar(&flagXRPLURL, "xrpl-rpc-url", "", "Override the XRPL JSON-RPC URL")
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "Emit JSON output")
	rootCmd.PersistentFlags().StringVar(&flagProfile, "profile", "", "Use a specific local account profile")
	rootCmd.PersistentFlags().StringVar(&flagProfile, "account", "", "Use a specific local wallet account")

	rootCmd.AddCommand(newConfigCommand())
	rootCmd.AddCommand(newWalletCommand())
	rootCmd.AddCommand(newAuthCommand())
	rootCmd.AddCommand(newMarketsCommand())
	rootCmd.AddCommand(newProfileCommand())
	rootCmd.AddCommand(newRewardsCommand())
	rootCmd.AddCommand(newFundingCommand())
	rootCmd.AddCommand(newPredictCommand())
	rootCmd.AddCommand(newClaimCommand())
	rootCmd.AddCommand(newClobCommand())

	return rootCmd
}

func newConfigCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "config", Short: "Inspect or update local CLI configuration"}
	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show the current local configuration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, ctx.Config)
		},
	})

	setCmd := &cobra.Command{
		Use:   "set",
		Short: "Update the CLI API or RPC URLs",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := app.LoadConfig()
			if err != nil {
				return err
			}
			apiURL, _ := cmd.Flags().GetString("api-url")
			rpcURL, _ := cmd.Flags().GetString("rpc-url")
			xrplURL, _ := cmd.Flags().GetString("xrpl-rpc-url")
			if apiURL != "" {
				cfg.APIBaseURL = apiURL
			}
			if rpcURL != "" {
				cfg.EVMRPCURL = rpcURL
			}
			if xrplURL != "" {
				cfg.XRPLRPCURL = xrplURL
			}
			if err := app.SaveConfig(cfg); err != nil {
				return err
			}
			if flagJSON {
				return printOutput(true, map[string]any{
					"message": "Configuration updated.",
					"config":  cfg,
				})
			}
			fmt.Println("Configuration updated.")
			return nil
		},
	}
	setCmd.Flags().String("api-url", "", "Set the CLI API base URL")
	setCmd.Flags().String("rpc-url", "", "Set the XRPL EVM RPC URL")
	setCmd.Flags().String("xrpl-rpc-url", "", "Set the XRPL RPC URL")
	cmd.AddCommand(setCmd)
	return cmd
}

func newWalletCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "wallet", Short: "Create, import, inspect, and fund local wallets"}

	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a new XRPL EVM wallet and store the private key in the OS keychain",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			accountName, profile := resolveWalletAccountProfile(cmd, ctx)
			wallet, privateKeyHex, err := evm.NewRandomWallet()
			if err != nil {
				return err
			}
			secretStore, err := app.SaveSecret(app.EVMSecretKey(accountName), privateKeyHex)
			if err != nil {
				return err
			}
			profile.EVMAddress = wallet.Address().Hex()
			ctx.Config.SetCurrentProfile(profile)
			if shouldActivateWalletAccount(cmd, ctx, accountName) {
				ctx.Config.ActiveProfile = accountName
			}
			if err := app.SaveConfig(ctx.Config); err != nil {
				return err
			}
			return printOutput(ctx.JSON, map[string]any{
				"account":          accountName,
				"activeAccount":    ctx.Config.ActiveProfile,
				"evmAddress":       wallet.Address().Hex(),
				"storedIn":         string(secretStore),
				"storedInKeychain": secretStore == app.SecretStoreKeychain,
				"nextStep":         "Run `axiom auth register` to get your Axiom destination tag.",
			})
		},
	}
	createCmd.Flags().String("account", "", "Import the wallet into a specific local account")
	createCmd.Flags().Bool("activate", false, "Set the target account as the active account after creation")
	cmd.AddCommand(createCmd)

	importCmd := &cobra.Command{
		Use:   "import",
		Short: "Import an existing XRPL EVM private key into the OS keychain",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			accountName, profile := resolveWalletAccountProfile(cmd, ctx)
			privateKey, _ := cmd.Flags().GetString("private-key")
			if strings.TrimSpace(privateKey) == "" {
				return errors.New("--private-key is required")
			}
			wallet, err := evm.WalletFromPrivateKeyHex(privateKey)
			if err != nil {
				return err
			}
			secretStore, err := app.SaveSecret(app.EVMSecretKey(accountName), wallet.PrivateKeyHex())
			if err != nil {
				return err
			}
			profile.EVMAddress = wallet.Address().Hex()
			ctx.Config.SetCurrentProfile(profile)
			if shouldActivateWalletAccount(cmd, ctx, accountName) {
				ctx.Config.ActiveProfile = accountName
			}
			if err := app.SaveConfig(ctx.Config); err != nil {
				return err
			}
			return printOutput(ctx.JSON, map[string]any{
				"account":          accountName,
				"activeAccount":    ctx.Config.ActiveProfile,
				"evmAddress":       wallet.Address().Hex(),
				"storedIn":         string(secretStore),
				"storedInKeychain": secretStore == app.SecretStoreKeychain,
			})
		},
	}
	importCmd.Flags().String("private-key", "", "Hex-encoded secp256k1 private key")
	importCmd.Flags().String("account", "", "Import the wallet into a specific local account")
	importCmd.Flags().Bool("activate", false, "Set the target account as the active account after import")
	cmd.AddCommand(importCmd)

	xrplCreateCmd := &cobra.Command{
		Use:   "xrpl-create",
		Short: "Create a native XRPL wallet for direct bridge funding submissions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			accountName, profile := resolveWalletAccountProfile(cmd, ctx)
			wallet, err := axrpl.NewRandomWallet()
			if err != nil {
				return err
			}
			secretStore, err := app.SaveSecret(app.XRPLSecretKey(accountName), wallet.Seed())
			if err != nil {
				return err
			}
			profile.XRPLAddress = wallet.Address()
			ctx.Config.SetCurrentProfile(profile)
			if shouldActivateWalletAccount(cmd, ctx, accountName) {
				ctx.Config.ActiveProfile = accountName
			}
			if err := app.SaveConfig(ctx.Config); err != nil {
				return err
			}
			return printOutput(ctx.JSON, map[string]any{
				"account":          accountName,
				"activeAccount":    ctx.Config.ActiveProfile,
				"xrplAddress":      wallet.Address(),
				"storedIn":         string(secretStore),
				"storedInKeychain": secretStore == app.SecretStoreKeychain,
			})
		},
	}
	xrplCreateCmd.Flags().String("account", "", "Import the wallet into a specific local account")
	xrplCreateCmd.Flags().Bool("activate", false, "Set the target account as the active account after creation")
	cmd.AddCommand(xrplCreateCmd)

	importXRPLCmd := &cobra.Command{
		Use:   "xrpl-import",
		Short: "Import an XRPL seed into the OS keychain",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			accountName, profile := resolveWalletAccountProfile(cmd, ctx)
			seed, _ := cmd.Flags().GetString("seed")
			if strings.TrimSpace(seed) == "" {
				return errors.New("--seed is required")
			}
			wallet, err := axrpl.WalletFromSeed(seed)
			if err != nil {
				return err
			}
			secretStore, err := app.SaveSecret(app.XRPLSecretKey(accountName), wallet.Seed())
			if err != nil {
				return err
			}
			profile.XRPLAddress = wallet.Address()
			ctx.Config.SetCurrentProfile(profile)
			if shouldActivateWalletAccount(cmd, ctx, accountName) {
				ctx.Config.ActiveProfile = accountName
			}
			if err := app.SaveConfig(ctx.Config); err != nil {
				return err
			}
			return printOutput(ctx.JSON, map[string]any{
				"account":          accountName,
				"activeAccount":    ctx.Config.ActiveProfile,
				"xrplAddress":      wallet.Address(),
				"storedIn":         string(secretStore),
				"storedInKeychain": secretStore == app.SecretStoreKeychain,
			})
		},
	}
	importXRPLCmd.Flags().String("seed", "", "XRPL family seed (s...) or compatible secret")
	importXRPLCmd.Flags().String("account", "", "Import the wallet into a specific local account")
	importXRPLCmd.Flags().Bool("activate", false, "Set the target account as the active account after import")
	cmd.AddCommand(importXRPLCmd)

	accountsCmd := &cobra.Command{Use: "accounts", Short: "List and select local wallet accounts"}
	accountsCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List all local wallet accounts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			accountNames := make([]string, 0, len(ctx.Config.Profiles))
			for name := range ctx.Config.Profiles {
				accountNames = append(accountNames, name)
			}
			sort.Strings(accountNames)
			accounts := make([]map[string]any, 0, len(accountNames))
			for _, name := range accountNames {
				profile := ctx.Config.Profiles[name]
				accounts = append(accounts, map[string]any{
					"account":               name,
					"active":                name == ctx.Config.ActiveProfile,
					"evmAddress":            profile.EVMAddress,
					"xrplAddress":           profile.XRPLAddress,
					"depositDestinationTag": profile.DepositDestinationTag,
				})
			}
			return printOutput(ctx.JSON, map[string]any{
				"activeAccount": ctx.Config.ActiveProfile,
				"items":         accounts,
				"total":         len(accounts),
			})
		},
	})
	accountsCmd.AddCommand(&cobra.Command{
		Use:   "use <account>",
		Short: "Set the active local wallet account",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			accountName := strings.TrimSpace(args[0])
			if accountName == "" {
				return errors.New("account name is required")
			}
			profile, ok := ctx.Config.Profiles[accountName]
			if !ok {
				return fmt.Errorf("local account %q does not exist", accountName)
			}
			ctx.Config.ActiveProfile = accountName
			ctx.Config.SetCurrentProfile(profile)
			if err := app.SaveConfig(ctx.Config); err != nil {
				return err
			}
			return printOutput(ctx.JSON, map[string]any{
				"activeAccount": accountName,
				"evmAddress":    profile.EVMAddress,
				"xrplAddress":   profile.XRPLAddress,
			})
		},
	})
	cmd.AddCommand(accountsCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Show local wallet addresses for the active profile",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, map[string]any{
				"profile":               ctx.ProfileName,
				"evmAddress":            ctx.Profile.EVMAddress,
				"xrplAddress":           ctx.Profile.XRPLAddress,
				"depositDestinationTag": ctx.Profile.DepositDestinationTag,
			})
		},
	})

	resetCmd := &cobra.Command{
		Use:   "reset",
		Short: "Clear local wallet addresses, destination tag, and stored secrets for the active profile",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}

			confirmed, _ := cmd.Flags().GetBool("yes")
			if !confirmed {
				fmt.Fprintf(os.Stderr, "WARNING: wallet reset permanently deletes the local EVM private key and XRPL seed for profile %q from your OS keychain.\n", ctx.ProfileName)
				fmt.Fprintln(os.Stderr, "WARNING: once removed, these secrets cannot be recovered by the CLI unless you backed them up elsewhere.")
				fmt.Fprintf(os.Stderr, "Re-run with `axiom wallet reset --yes` to confirm the irreversible reset for profile %q.\n", ctx.ProfileName)
				return errors.New("wallet reset aborted: confirmation required")
			}

			if err := app.DeleteSecretIfExists(app.EVMSecretKey(ctx.ProfileName)); err != nil {
				return err
			}
			if err := app.DeleteSecretIfExists(app.XRPLSecretKey(ctx.ProfileName)); err != nil {
				return err
			}

			ctx.Config.SetCurrentProfile(app.Profile{Name: ctx.ProfileName})
			if err := app.SaveConfig(ctx.Config); err != nil {
				return err
			}

			return printOutput(ctx.JSON, map[string]any{
				"profile":               ctx.ProfileName,
				"evmAddressCleared":     true,
				"xrplAddressCleared":    true,
				"destinationTagCleared": true,
				"warning":               "The local EVM private key and XRPL seed were permanently removed from this machine. They cannot be recovered by the CLI.",
			})
		},
	}
	resetCmd.Flags().Bool("yes", false, "Confirm the irreversible deletion of local wallet secrets for the active profile")
	cmd.AddCommand(resetCmd)

	balanceCmd := &cobra.Command{
		Use:   "balance",
		Short: "Show EVM and XRPL balances for the active profile",
		Long: strings.Join([]string{
			"Show EVM and XRPL balances for the active profile.",
			"",
			"By default the command returns both XRPL EVM and XRPL balances when those wallets",
			"are configured. Use --evm or --xrpl to restrict the response to a single network.",
		}, "\n"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			includeEVM := mustBoolFlag(cmd, "evm")
			includeXRPL := mustBoolFlag(cmd, "xrpl")
			if !includeEVM && !includeXRPL {
				includeEVM = true
				includeXRPL = true
			}
			result := map[string]any{"profile": ctx.ProfileName}
			if includeEVM && ctx.Profile.EVMAddress != "" {
				balance, err := getEVMBalance(cmd.Context(), ctx.Config.EVMRPCURL, common.HexToAddress(ctx.Profile.EVMAddress))
				if err != nil {
					return err
				}
				result["evmAddress"] = ctx.Profile.EVMAddress
				result["evmBalanceXrp"] = formatWeiToXRP(balance)
			}
			if includeXRPL && ctx.Profile.XRPLAddress != "" {
				balance, err := getXRPLBalance(cmd.Context(), ctx.Config.XRPLRPCURL, ctx.Profile.XRPLAddress)
				if err != nil {
					return err
				}
				result["xrplAddress"] = ctx.Profile.XRPLAddress
				result["xrplBalanceXrp"] = balance
			}
			return printOutput(ctx.JSON, result)
		},
	}
	balanceCmd.Flags().Bool("evm", false, "Return only the XRPL EVM balance")
	balanceCmd.Flags().Bool("xrpl", false, "Return only the XRPL balance")
	cmd.AddCommand(balanceCmd)

	return cmd
}

func newAuthCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "auth", Short: "Register the active wallet with the Axiom backend"}
	registerCmd := &cobra.Command{
		Use:   "register",
		Short: "Register or refresh the active wallet with the Axiom CLI API",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			wallet, err := requireEVMWallet(ctx)
			if err != nil {
				return err
			}
			response, err := registerWalletWithCompat(cmd.Context(), ctx, wallet, mustStringFlag(cmd, "ref-code"))
			if err != nil {
				return err
			}
			profile := ctx.Profile
			profile.EVMAddress = response.WalletAddress
			profile.DepositDestinationTag = response.DepositDestinationTag
			ctx.Config.SetCurrentProfile(profile)
			if err := app.SaveConfig(ctx.Config); err != nil {
				return err
			}
			return printOutput(ctx.JSON, response)
		},
	}
	registerCmd.Flags().String("ref-code", "", "Optional referral code or referrer wallet address to apply during registration")
	cmd.AddCommand(registerCmd)
	return cmd
}

func newMarketsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "markets",
		Short: "Discover Axiom markets and fetch market details",
		Long: strings.Join([]string{
			"Discover Axiom markets and fetch market details.",
			"",
			"Use `axiom markets list` to browse markets and narrow results with filters such as:",
			"  --status open|resolved",
			"  --category hourly|sports|streak|...",
			"  --type clob|binary|...",
			"  --search <text>",
			"  --limit <n> (0 fetches all matching markets)",
			"  --offset <n>",
			"  --spot-prices (opt in to slower on-chain odds enrichment)",
			"",
			"Use `axiom markets get <market-id-or-address>` once you have the full identifier.",
		}, "\n"),
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List markets from the Axiom backend",
		Example: strings.Join([]string{
			"axiom markets list",
			"axiom markets list --category hourly",
			"axiom markets list --type clob",
			"axiom markets list --status open --spot-prices",
			"axiom markets list --status resolved --limit 50",
			"axiom markets list --search XRP --offset 20",
		}, "\n"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			status, _ := cmd.Flags().GetString("status")
			search, _ := cmd.Flags().GetString("search")
			category, _ := cmd.Flags().GetString("category")
			marketType, _ := cmd.Flags().GetString("type")
			normalizedImpl := ""
			if strings.TrimSpace(marketType) != "" {
				normalizedImpl = normalizeMarketImplementation(strings.TrimSpace(marketType))
			}
			limit, _ := cmd.Flags().GetInt("limit")
			offset, _ := cmd.Flags().GetInt("offset")
			myPositions, _ := cmd.Flags().GetBool("my-positions")
			spotPrices, _ := cmd.Flags().GetBool("spot-prices")
			var response *api.MarketsResponse
			needsLocalFiltering := strings.TrimSpace(category) != "" || normalizedImpl != "" || myPositions
			if needsLocalFiltering || limit <= 0 {
				response, err = ctx.API.ListAllMarkets(cmd.Context(), status, search, "", normalizedImpl, 0)
			} else {
				response, err = ctx.API.ListMarkets(cmd.Context(), status, search, "", normalizedImpl, limit, offset)
			}
			if err != nil {
				return err
			}
			if strings.TrimSpace(category) != "" {
				response = filterMarketsByCategory(response, category, 0, 0)
			}
			if normalizedImpl != "" {
				response = filterMarketsByType(response, marketType)
			}
			if myPositions {
				if ctx.Profile.EVMAddress == "" {
					return errors.New("no active EVM wallet is configured; run `axiom wallet create` or pass an address to `axiom profile positions`")
				}
				positions, err := ctx.API.GetPositions(cmd.Context(), ctx.Profile.EVMAddress, "open", 0)
				if err != nil {
					return err
				}
				positions = filterPositionsByStatus(positions, "open", 0)
				response = filterMarketsByPositions(response, positions)
			}
			if needsLocalFiltering {
				response = paginateMarkets(response, limit, offset)
			}
			if spotPrices {
				enrichMarketsWithSpotPrices(cmd.Context(), ctx, response)
			}
			return printOutput(ctx.JSON, response)
		},
	}
	listCmd.Flags().String("status", "open", "Filter by status: open or resolved")
	listCmd.Flags().String("category", "", "Filter by market category (for example hourly, sports, streak)")
	listCmd.Flags().String("type", "", "Filter by market implementation type (for example clob, parimutuel)")
	listCmd.Flags().String("search", "", "Search by title or headline")
	listCmd.Flags().Bool("my-positions", false, "Only return markets where the active wallet currently has open positions")
	listCmd.Flags().Bool("spot-prices", false, "Fetch current spot odds from XRPL EVM for each returned market")
	listCmd.Flags().Int("limit", 0, "Maximum number of markets to return (0 means fetch all matching markets)")
	listCmd.Flags().Int("offset", 0, "Offset into the market result set")
	cmd.AddCommand(listCmd)

	getCmd := &cobra.Command{
		Use:   "get <market-id-or-address>",
		Short: "Get detailed metadata for a single market",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			instanceDate, _ := cmd.Flags().GetString("instance-date")
			response, err := loadMarketWithClobFallback(cmd.Context(), ctx, args[0], instanceDate)
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, response)
		},
	}
	getCmd.Flags().String("instance-date", "", "Instance date for recurring daily/hourly markets in YYYY-MM-DD format")
	cmd.AddCommand(getCmd)
	return cmd
}

func newProfileCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "profile", Short: "Read profile stats, positions, and claimable winnings"}

	updateCmd := &cobra.Command{
		Use:   "update",
		Short: "Update the active wallet's Axiom display name or avatar URL",
		Long: strings.Join([]string{
			"Update the active wallet's Axiom profile metadata.",
			"",
			"This signs a profile-update message with the active local EVM wallet before sending it",
			"to the CLI API. At least one of --display-name or --avatar-url is required.",
		}, "\n"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			wallet, _, err := requireEVMWalletWithKey(ctx)
			if err != nil {
				return err
			}
			displayName := strings.TrimSpace(mustStringFlag(cmd, "display-name"))
			avatarURL := strings.TrimSpace(mustStringFlag(cmd, "avatar-url"))
			if displayName == "" && avatarURL == "" {
				return errors.New("at least one of --display-name or --avatar-url is required")
			}
			issuedAt := time.Now().UTC()
			message := buildProfileUpdateMessage(wallet.Address().Hex(), ctx.Config.DeviceID, issuedAt, displayName, avatarURL)
			signature, err := wallet.SignMessage(message)
			if err != nil {
				return err
			}
			response, err := ctx.API.UpdateProfile(cmd.Context(), wallet.Address().Hex(), api.UpdateProfileRequest{
				WalletAddress: wallet.Address().Hex(),
				Signature:     signature,
				DeviceID:      ctx.Config.DeviceID,
				IssuedAt:      formatRegistrationIssuedAt(issuedAt),
				DisplayName:   optionalStringPointer(displayName),
				AvatarURL:     optionalStringPointer(avatarURL),
			})
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, response)
		},
	}
	updateCmd.Flags().String("display-name", "", "Set the display name for the active wallet's Axiom profile")
	updateCmd.Flags().String("avatar-url", "", "Set the avatar image URL for the active wallet's Axiom profile")
	cmd.AddCommand(updateCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "show [wallet-address]",
		Short: "Show an Axiom profile summary",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			address, err := resolveProfileAddress(ctx, args)
			if err != nil {
				return err
			}
			response, err := ctx.API.GetProfile(cmd.Context(), address)
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, response)
		},
	})

	positionsCmd := &cobra.Command{
		Use:   "positions [wallet-address]",
		Short: "List recent positions for a profile",
		Long: strings.Join([]string{
			"List recent positions for a profile.",
			"",
			"Use --status open to restrict results to unresolved positions. The CLI applies the",
			"status filter locally as a fallback when the backend returns a broader payload.",
		}, "\n"),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			address, err := resolveProfileAddress(ctx, args)
			if err != nil {
				return err
			}
			status, _ := cmd.Flags().GetString("status")
			limit, _ := cmd.Flags().GetInt("limit")
			response, err := ctx.API.GetPositions(cmd.Context(), address, status, limit)
			if err != nil {
				return err
			}
			response = filterPositionsByStatus(response, status, limit)
			return printOutput(ctx.JSON, response)
		},
	}
	positionsCmd.Flags().String("status", "all", "Filter by position status: open, won, lost, all")
	positionsCmd.Flags().Int("limit", 20, "Maximum number of positions to return")
	cmd.AddCommand(positionsCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "unclaimed [wallet-address]",
		Short: "Show unclaimed winnings for a profile",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			address, err := resolveProfileAddress(ctx, args)
			if err != nil {
				return err
			}
			response, err := ctx.API.GetUnclaimed(cmd.Context(), address)
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, response)
		},
	})

	return cmd
}

func newRewardsCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "rewards", Short: "Track rewards progress and claim daily, weekly, and epoch rewards"}

	cmd.AddCommand(&cobra.Command{
		Use:   "show [wallet-address]",
		Short: "Show rewards progress, streaks, tickets, and epoch claimables",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			address, err := resolveProfileAddress(ctx, args)
			if err != nil {
				return err
			}
			response, err := ctx.API.GetRewards(cmd.Context(), address)
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, response)
		},
	})

	claimCmd := &cobra.Command{Use: "claim", Short: "Claim daily chest, weekly chest, or epoch rewards"}

	claimCmd.AddCommand(&cobra.Command{
		Use:   "daily",
		Short: "Claim the daily chest reward after completing the daily task requirement",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			wallet, _, err := requireEVMWalletWithKey(ctx)
			if err != nil {
				return err
			}
			issuedAt := time.Now().UTC()
			message := buildRewardsActionMessage(wallet.Address().Hex(), ctx.Config.DeviceID, issuedAt, "claim-daily-chest", 0, 0, "")
			signature, err := wallet.SignMessage(message)
			if err != nil {
				return err
			}
			response, err := ctx.API.ClaimDailyChest(cmd.Context(), wallet.Address().Hex(), api.RewardsActionRequest{
				WalletAddress: wallet.Address().Hex(),
				Signature:     signature,
				DeviceID:      ctx.Config.DeviceID,
				IssuedAt:      formatRegistrationIssuedAt(issuedAt),
			})
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, map[string]any{
				"walletAddress": wallet.Address().Hex(),
				"claimType":     "daily-chest",
				"success":       response.Success,
				"prizeAmount":   response.PrizeAmount,
				"prizeLabel":    response.PrizeLabel,
			})
		},
	})

	claimWeeklyCmd := &cobra.Command{
		Use:   "weekly [ticket-id]",
		Short: "Claim an available weekly chest ticket from a 7-day streak",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			wallet, _, err := requireEVMWalletWithKey(ctx)
			if err != nil {
				return err
			}
			rewards, err := ctx.API.GetRewards(cmd.Context(), wallet.Address().Hex())
			if err != nil {
				return err
			}
			ticket, err := resolveClaimableLotteryTicket(rewards, args)
			if err != nil {
				return err
			}
			issuedAt := time.Now().UTC()
			message := buildRewardsActionMessage(wallet.Address().Hex(), ctx.Config.DeviceID, issuedAt, "claim-weekly-chest", ticket.ID, 0, "")
			signature, err := wallet.SignMessage(message)
			if err != nil {
				return err
			}
			response, err := ctx.API.ClaimWeeklyChest(cmd.Context(), wallet.Address().Hex(), ticket.ID, api.RewardsActionRequest{
				WalletAddress: wallet.Address().Hex(),
				Signature:     signature,
				DeviceID:      ctx.Config.DeviceID,
				IssuedAt:      formatRegistrationIssuedAt(issuedAt),
			})
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, map[string]any{
				"walletAddress":         wallet.Address().Hex(),
				"claimType":             "weekly-chest",
				"ticketId":              ticket.ID,
				"success":               response.Success,
				"prizeType":             response.PrizeType,
				"prizeAmount":           response.PrizeAmount,
				"prizeLabel":            response.PrizeLabel,
				"isConsolation":         response.IsConsolation,
				"cashConvertedToPoints": response.CashConvertedToPoints,
			})
		},
	}
	claimCmd.AddCommand(claimWeeklyCmd)

	claimEpochCmd := &cobra.Command{
		Use:   "epoch [epoch-id]",
		Short: "Claim the current claimable epoch reward on-chain and sync it back to Axiom",
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
			rewards, err := ctx.API.GetRewards(cmd.Context(), wallet.Address().Hex())
			if err != nil {
				return err
			}
			reward, err := resolveClaimableEpochReward(rewards, args)
			if err != nil {
				return err
			}
			txHashHex, _ := cmd.Flags().GetString("tx-hash")
			txHashHex = strings.TrimSpace(txHashHex)
			claimedOnChain := false
			if txHashHex == "" {
				amountWei, ok := new(big.Int).SetString(strings.TrimSpace(reward.AmountWei), 10)
				if !ok {
					return fmt.Errorf("invalid amountWei for epoch %d", reward.EpochID)
				}
				proof, err := parseMerkleProof(reward.Proof)
				if err != nil {
					return err
				}
				rewardsAddress, err := resolveAxiomRewardsAddress(cmd.Context(), ctx.API)
				if err != nil {
					return err
				}
				txHash, err := claimEpochRewards(
					cmd.Context(),
					ctx.Config.EVMRPCURL,
					big.NewInt(xrplEVMChainID),
					privateKeyHex,
					rewardsAddress,
					big.NewInt(int64(reward.EpochID)),
					amountWei,
					proof,
				)
				if err != nil {
					return err
				}
				receipt, err := waitForTxReceipt(cmd.Context(), ctx.Config.EVMRPCURL, txHash)
				if err != nil {
					return err
				}
				if receipt.Status != 1 {
					return fmt.Errorf("epoch reward claim transaction failed: %s", txHash.Hex())
				}
				txHashHex = txHash.Hex()
				claimedOnChain = true
			} else {
				txHash, err := parseTxHash(txHashHex)
				if err != nil {
					return err
				}
				txHashHex = txHash.Hex()
			}
			response, err := syncEpochRewardClaim(cmd.Context(), ctx, wallet, reward.EpochID, txHashHex)
			if err != nil {
				if claimedOnChain {
					return fmt.Errorf("epoch reward was claimed on-chain in tx %s, but syncing it back to Axiom failed: %w\nRecovery: rerun `axiom rewards claim epoch %d --tx-hash %s` to resubmit the mined transaction hash without sending another on-chain claim", txHashHex, err, reward.EpochID, txHashHex)
				}
				return fmt.Errorf("sync epoch %d reward claim: %w", reward.EpochID, err)
			}
			return printOutput(ctx.JSON, map[string]any{
				"walletAddress": wallet.Address().Hex(),
				"claimType":     "epoch-reward",
				"epochId":       response.EpochID,
				"amountXrp":     response.ClaimedReward.AmountXRP,
				"txHash":        response.TxHash,
				"success":       response.Success,
			})
		},
	}
	claimEpochCmd.Flags().String("tx-hash", "", "Skip the on-chain claim and sync an already-mined epoch reward transaction hash")
	claimCmd.AddCommand(claimEpochCmd)

	cmd.AddCommand(claimCmd)
	return cmd
}

func newFundingCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "funding", Short: "Handle direct XRP funding and bridge funding via XRPL destination tags"}

	infoCmd := &cobra.Command{
		Use:   "info [wallet-address]",
		Short: "Show funding instructions, destination tag, and recent bridge history",
		Long: strings.Join([]string{
			"Show funding instructions, destination tag, and recent bridge history.",
			"",
			"This command is read-only. It returns the relay deposit wallet, the destination tag",
			"that identifies your wallet, and recent relay history from the backend.",
		}, "\n"),
		Example: strings.Join([]string{
			"axiom funding info",
			"axiom funding info 0xabc123...",
			"axiom --json funding info",
		}, "\n"),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			address, err := resolveProfileAddress(ctx, args)
			if err != nil {
				return err
			}
			response, err := ctx.API.GetFunding(cmd.Context(), address, 20)
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, response)
		},
	}
	cmd.AddCommand(infoCmd)

	directCmd := &cobra.Command{
		Use:   "direct --to <evm-address> --amount <xrp>",
		Short: "Send native XRP on XRPL EVM directly from the active local EVM wallet",
		Long: strings.Join([]string{
			"Send native XRP on XRPL EVM directly from the active local EVM wallet.",
			"",
			"Use this when you already have XRP on XRPL EVM and want to fund another EVM wallet",
			"without going through the XRPL relay deposit flow.",
		}, "\n"),
		Example: strings.Join([]string{
			"axiom funding direct --to 0xabc123... --amount 25",
			"axiom funding direct --to 0xabc123... --amount 25 --json",
		}, "\n"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			wallet, err := requireEVMWallet(ctx)
			if err != nil {
				return err
			}
			toAddress, _ := cmd.Flags().GetString("to")
			amount, _ := cmd.Flags().GetString("amount")
			if toAddress == "" || amount == "" {
				return errors.New("--to and --amount are required")
			}
			amountWei, err := parseXRPToWei(amount)
			if err != nil {
				return err
			}
			txHash, err := wallet.SendNativeXRP(cmd.Context(), ctx.Config.EVMRPCURL, big.NewInt(xrplEVMChainID), common.HexToAddress(toAddress), amountWei)
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, map[string]any{"from": wallet.Address().Hex(), "to": toAddress, "amountXrp": amount, "txHash": txHash.Hex()})
		},
	}
	directCmd.Flags().String("to", "", "Destination XRPL EVM address")
	directCmd.Flags().String("amount", "", "Amount of XRP to send")
	cmd.AddCommand(directCmd)

	bridgeCmd := &cobra.Command{
		Use:   "bridge [--amount <xrp>] [--submit]",
		Short: "Show a QR code and payment details for XRPL relay funding, or submit the XRPL payment directly",
		Long: strings.Join([]string{
			"Prepare XRPL relay funding for your Axiom wallet.",
			"",
			"By default this command shows the relay wallet address, your destination tag,",
			"a wallet-scan payment URI, and a terminal QR code so you can send XRP from",
			"any XRPL wallet or exchange app without importing an XRPL seed into the CLI.",
			"",
			"Use --submit only if you want the CLI to send the XRPL payment itself using",
			"an XRPL seed stored locally in the OS keychain.",
		}, "\n"),
		Example: strings.Join([]string{
			"axiom funding bridge",
			"axiom funding bridge --amount 25",
			"axiom funding bridge --amount 25 --submit",
			"axiom funding bridge-submit --amount 25",
		}, "\n"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			amount, _ := cmd.Flags().GetString("amount")
			submit, _ := cmd.Flags().GetBool("submit")
			var preview *bridgeFundingPreview
			if submit {
				preview, err = submitBridgeFunding(cmd.Context(), ctx, amount)
				if err != nil {
					return err
				}
			} else {
				preview, err = previewBridgeFunding(cmd.Context(), ctx, amount)
				if err != nil {
					return err
				}
			}
			return printBridgeFundingOutput(ctx.JSON, preview)
		},
	}
	bridgeCmd.Flags().String("amount", "", "Optional amount of XRP to prefill in the QR code or required amount to submit directly")
	bridgeCmd.Flags().Bool("submit", false, "Submit the XRPL payment using the local XRPL wallet seed stored in keychain")
	cmd.AddCommand(bridgeCmd)

	bridgeSubmitCmd := &cobra.Command{
		Use:   "bridge-submit --amount <xrp>",
		Short: "Bridge XRP from the active local XRPL wallet stored in the OS keychain",
		Long: strings.Join([]string{
			"Send an XRPL payment from the active local XRPL wallet to Axiom's relay deposit wallet.",
			"",
			"Use this after `axiom wallet xrpl-create` or `axiom wallet xrpl-import` when you want",
			"the CLI to submit the bridge payment directly instead of showing a QR code.",
		}, "\n"),
		Example: strings.Join([]string{
			"axiom funding bridge-submit --amount 25",
			"axiom funding bridge-submit --amount 25 --json",
		}, "\n"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			preview, err := submitBridgeFunding(cmd.Context(), ctx, mustStringFlag(cmd, "amount"))
			if err != nil {
				return err
			}
			return printBridgeFundingOutput(ctx.JSON, preview)
		},
	}
	bridgeSubmitCmd.Flags().String("amount", "", "Amount of XRP to bridge from the active local XRPL wallet")
	cmd.AddCommand(bridgeSubmitCmd)

	return cmd
}

func newPredictCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "predict", Short: "Place predictions on Axiom markets using the active EVM wallet"}
	quoteCmd := &cobra.Command{
		Use:   "quote <market-id-or-address>",
		Short: "Preview weighted shares, price impact, and payout for a proposed buy",
		Example: strings.Join([]string{
			"axiom predict quote xrp-hourly --label Higher --amount 5",
			"axiom predict quote xrp-hourly --outcome 0 --amount 5 --instance-date 2026-03-11",
		}, "\n"),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			market, err := loadMarketWithClobFallback(cmd.Context(), ctx, args[0], mustStringFlag(cmd, "instance-date"))
			if err != nil {
				return err
			}
			if isClobMarketImplementation(market.MarketImplementation) {
				return errors.New("predict quote currently supports TieredParimutuel markets only; use `axiom markets get` to inspect CLOB logical markets during Phase 1")
			}
			amount, _ := cmd.Flags().GetString("amount")
			if amount == "" {
				return errors.New("--amount is required")
			}
			amountWei, err := parseXRPToWei(amount)
			if err != nil {
				return err
			}
			outcomeIndex, err := resolveOutcomeIndex(market, mustStringFlag(cmd, "outcome"), mustStringFlag(cmd, "label"))
			if err != nil {
				return err
			}
			state, err := loadMarketState(cmd.Context(), ctx.Config.EVMRPCURL, common.HexToAddress(market.ContractAddress))
			if err != nil {
				return err
			}
			outcomeLabel := fmt.Sprintf("Outcome %d", outcomeIndex)
			for _, outcome := range market.Outcomes {
				if outcome.Index == outcomeIndex {
					outcomeLabel = outcome.Label
					break
				}
			}
			quote, err := quoteBuy(state, amountWei, uint8(outcomeIndex), market.Title, outcomeLabel, time.Now())
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, quote)
		},
	}
	quoteCmd.Flags().String("amount", "", "Amount of XRP to preview")
	quoteCmd.Flags().String("outcome", "", "Outcome index to preview")
	quoteCmd.Flags().String("label", "", "Outcome label to preview (alternative to --outcome)")
	quoteCmd.Flags().String("instance-date", "", "Instance date for recurring daily/hourly markets in YYYY-MM-DD format")
	cmd.AddCommand(quoteCmd)

	buyCmd := &cobra.Command{
		Use:   "buy <market-id-or-address>",
		Short: "Buy into an Axiom market outcome",
		Long: strings.Join([]string{
			"Buy into an Axiom market outcome.",
			"",
			"Use --dry-run to execute the same market and outcome resolution path as a live buy",
			"without submitting a transaction. This returns the same quote structure as `predict quote`.",
		}, "\n"),
		Example: strings.Join([]string{
			"axiom predict buy xrp-hourly --label Higher --amount 5",
			"axiom predict buy xrp-hourly --label Higher --amount 5 --dry-run",
			"axiom predict buy xrp-hourly --label Higher --amount 5 --instance-date 2026-03-11",
		}, "\n"),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			market, err := loadMarketWithClobFallback(cmd.Context(), ctx, args[0], mustStringFlag(cmd, "instance-date"))
			if err != nil {
				return err
			}
			if isClobMarketImplementation(market.MarketImplementation) {
				return errors.New("predict buy currently supports TieredParimutuel markets only; use `axiom markets get` to inspect CLOB logical markets during Phase 1")
			}
			amount, _ := cmd.Flags().GetString("amount")
			if amount == "" {
				return errors.New("--amount is required")
			}
			amountWei, err := parseXRPToWei(amount)
			if err != nil {
				return err
			}
			outcomeIndex, err := resolveOutcomeIndex(market, mustStringFlag(cmd, "outcome"), mustStringFlag(cmd, "label"))
			if err != nil {
				return err
			}
			if mustBoolFlag(cmd, "dry-run") {
				state, err := loadMarketState(cmd.Context(), ctx.Config.EVMRPCURL, common.HexToAddress(market.ContractAddress))
				if err != nil {
					return err
				}
				outcomeLabel := fmt.Sprintf("Outcome %d", outcomeIndex)
				for _, outcome := range market.Outcomes {
					if outcome.Index == outcomeIndex {
						outcomeLabel = outcome.Label
						break
					}
				}
				quote, err := quoteBuy(state, amountWei, uint8(outcomeIndex), market.Title, outcomeLabel, time.Now())
				if err != nil {
					return err
				}
				return printOutput(ctx.JSON, map[string]any{
					"dryRun": true,
					"quote":  quote,
				})
			}
			_, privateKeyHex, err := requireEVMWalletWithKey(ctx)
			if err != nil {
				return err
			}
			minShares, err := parseOptionalWei(cmd, "min-shares")
			if err != nil {
				return err
			}
			txHash, err := buyPosition(cmd.Context(), ctx.Config.EVMRPCURL, big.NewInt(xrplEVMChainID), privateKeyHex, common.HexToAddress(market.ContractAddress), uint8(outcomeIndex), amountWei, minShares)
			if err != nil {
				return err
			}
			result := map[string]any{"txHash": txHash.Hex(), "market": market.Title, "amountXrp": amount, "outcomeIndex": outcomeIndex}
			if mustBoolFlag(cmd, "wait") {
				receipt, err := waitForReceipt(cmd.Context(), ctx.Config.EVMRPCURL, txHash)
				if err != nil {
					return err
				}
				result["receiptStatus"] = receipt.Status
			}
			return printOutput(ctx.JSON, result)
		},
	}
	buyCmd.Flags().String("amount", "", "Amount of XRP to commit")
	buyCmd.Flags().String("outcome", "", "Outcome index to buy")
	buyCmd.Flags().String("label", "", "Outcome label to buy (alternative to --outcome)")
	buyCmd.Flags().String("min-shares", "0", "Minimum acceptable weighted shares in wei")
	buyCmd.Flags().Bool("dry-run", false, "Resolve the market and return a quote without submitting a transaction")
	buyCmd.Flags().Bool("wait", false, "Wait for the transaction receipt")
	buyCmd.Flags().String("instance-date", "", "Instance date for recurring daily/hourly markets in YYYY-MM-DD format")
	cmd.AddCommand(buyCmd)
	return cmd
}

func newClaimCommand() *cobra.Command {
	cmd := &cobra.Command{Use: "claim", Short: "Claim winnings from individual markets or in batches"}

	marketCmd := &cobra.Command{
		Use:   "market <market-id-or-address>",
		Short: "Claim winnings or refunds from a single market",
		Example: strings.Join([]string{
			"axiom claim market market-123",
			"axiom claim market xrp-hourly --instance-date 2026-03-11",
		}, "\n"),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			wallet, privateKeyHex, err := requireEVMWalletWithKey(ctx)
			if err != nil {
				return err
			}
			market, err := loadMarketWithClobFallback(cmd.Context(), ctx, args[0], mustStringFlag(cmd, "instance-date"))
			if err != nil {
				return err
			}
			if isClobMarketImplementation(market.MarketImplementation) {
				legs, err := buildClobRedemptionPlan(cmd.Context(), ctx, market, wallet.Address())
				if err != nil {
					return err
				}
				if len(legs) == 0 {
					return printOutput(ctx.JSON, map[string]any{
						"market":   market.Title,
						"marketId": market.ID,
						"message":  "No redeemable resolved CLOB positions were found for the active wallet.",
					})
				}

				indexSetsByContract := make(map[string][]*big.Int)
				legsByContract := make(map[string][]clobRedemptionLeg)
				for _, leg := range legs {
					contract := common.HexToAddress(leg.ContractAddress).Hex()
					indexSetsByContract[contract] = append(indexSetsByContract[contract], big.NewInt(int64(leg.IndexSet)))
					legsByContract[contract] = append(legsByContract[contract], leg)
				}

				contractAddresses := make([]string, 0, len(indexSetsByContract))
				for contract := range indexSetsByContract {
					contractAddresses = append(contractAddresses, contract)
				}
				sort.Strings(contractAddresses)

				transactions := make([]map[string]any, 0, len(contractAddresses))
				for _, contract := range contractAddresses {
					txHash, redeemErr := redeemCTFMarket(cmd.Context(), ctx.Config.EVMRPCURL, big.NewInt(xrplEVMChainID), privateKeyHex, common.HexToAddress(contract), indexSetsByContract[contract])
					if redeemErr != nil {
						return redeemErr
					}
					entry := map[string]any{
						"contractAddress": contract,
						"txHash":          txHash.Hex(),
						"legs":            legsByContract[contract],
					}
					if mustBoolFlag(cmd, "wait") {
						receipt, waitErr := waitForReceipt(cmd.Context(), ctx.Config.EVMRPCURL, txHash)
						if waitErr != nil {
							return waitErr
						}
						entry["receiptStatus"] = receipt.Status
					}
					transactions = append(transactions, entry)
				}

				return printOutput(ctx.JSON, map[string]any{
					"market":        market.Title,
					"marketId":      market.ID,
					"walletAddress": wallet.Address().Hex(),
					"contracts":     len(contractAddresses),
					"redeemedLegs":  legs,
					"transactions":  transactions,
				})
			}
			txHash, err := claimSingleMarket(cmd.Context(), ctx.Config.EVMRPCURL, big.NewInt(xrplEVMChainID), privateKeyHex, common.HexToAddress(market.ContractAddress))
			if err != nil {
				return err
			}
			result := map[string]any{"txHash": txHash.Hex(), "market": market.Title}
			if mustBoolFlag(cmd, "wait") {
				receipt, err := waitForReceipt(cmd.Context(), ctx.Config.EVMRPCURL, txHash)
				if err != nil {
					return err
				}
				result["receiptStatus"] = receipt.Status
			}
			return printOutput(ctx.JSON, result)
		},
	}
	marketCmd.Flags().Bool("wait", false, "Wait for transaction finality")
	marketCmd.Flags().String("instance-date", "", "Instance date for recurring daily/hourly markets in YYYY-MM-DD format")
	cmd.AddCommand(marketCmd)

	batchCmd := &cobra.Command{
		Use:   "batch",
		Short: "Claim all currently unclaimed resolved winnings using the AxiomUtility contract",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			if ctx.Profile.EVMAddress == "" {
				return errors.New("no EVM wallet configured for the active profile")
			}
			_, privateKeyHex, err := requireEVMWalletWithKey(ctx)
			if err != nil {
				return err
			}
			configResponse, err := ctx.API.GetConfig(cmd.Context())
			if err != nil {
				return err
			}
			if configResponse.AxiomUtilityAddress == "" {
				return errors.New("the backend does not have a canonical AxiomUtility address configured")
			}
			unclaimed, err := ctx.API.GetUnclaimed(cmd.Context(), ctx.Profile.EVMAddress)
			if err != nil {
				return err
			}
			if len(unclaimed.Items) == 0 {
				return printOutput(ctx.JSON, map[string]any{"message": "No unclaimed winnings found."})
			}
			markets := make([]common.Address, 0, len(unclaimed.Items))
			for _, item := range unclaimed.Items {
				markets = append(markets, common.HexToAddress(item.MarketAddress))
			}
			txHash, err := batchClaimMarkets(cmd.Context(), ctx.Config.EVMRPCURL, big.NewInt(xrplEVMChainID), privateKeyHex, common.HexToAddress(configResponse.AxiomUtilityAddress), markets)
			if err != nil {
				return err
			}
			claimedMarkets := make([]map[string]any, 0, len(unclaimed.Items))
			for _, item := range unclaimed.Items {
				claimedMarkets = append(claimedMarkets, map[string]any{
					"marketId":      item.MarketID,
					"marketAddress": item.MarketAddress,
					"title":         item.Title,
					"payoutUsd":     item.PayoutUSD,
					"pnlUsd":        item.PnlUSD,
					"instanceDate":  item.InstanceDate,
					"category":      item.Category,
				})
			}
			result := map[string]any{
				"txHash":                txHash.Hex(),
				"markets":               len(markets),
				"claimedMarkets":        claimedMarkets,
				"totalClaimedPayoutUsd": unclaimed.Summary.TotalUnclaimedPayoutUSD,
				"totalClaimedPnlUsd":    unclaimed.Summary.TotalUnclaimedPnlUSD,
			}
			if mustBoolFlag(cmd, "wait") {
				receipt, err := waitForReceipt(cmd.Context(), ctx.Config.EVMRPCURL, txHash)
				if err != nil {
					return err
				}
				result["receiptStatus"] = receipt.Status
			}
			return printOutput(ctx.JSON, result)
		},
	}
	batchCmd.Flags().Bool("wait", false, "Wait for transaction finality")
	cmd.AddCommand(batchCmd)

	return cmd
}

func newClobCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clob",
		Short: "Inspect and manage hosted CLOB books, orders, fills, and cancellations",
	}
	cmd.PersistentFlags().String("projection-url", firstNonEmpty(os.Getenv("AXIOM_CLOB_PROJECTION_URL"), os.Getenv("CLOB_PROJECTION_URL"), defaultClobProjectionURL), "Override the hosted CLOB projection base URL")
	cmd.PersistentFlags().String("eventstore-url", firstNonEmpty(os.Getenv("AXIOM_CLOB_EVENTSTORE_URL"), os.Getenv("CLOB_EVENTSTORE_URL"), defaultClobEventstoreURL), "Override the hosted CLOB eventstore base URL")
	cmd.PersistentFlags().String("exchange-address", evm.DefaultClobExchangeAddress, "Override the on-chain AxiomCTFExchange address used for signing and approvals")
	cmd.PersistentFlags().String("outcome-token-address", evm.DefaultClobConditionalTokens, "Override the on-chain AxiomConditionalTokens address used for balances and approvals")

	bookCmd := &cobra.Command{Use: "book", Short: "Inspect hosted CLOB books"}
	depthCmd := &cobra.Command{
		Use:   "depth --market <id> --outcome <index>",
		Short: "Fetch the hosted depth ladder and book summary for a logical CLOB proposition",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			market := strings.TrimSpace(mustStringFlag(cmd, "market"))
			if market == "" {
				return errors.New("--market is required")
			}
			outcome, err := cmd.Flags().GetInt("outcome")
			if err != nil {
				return err
			}
			projectionURL := strings.TrimSpace(mustStringFlag(cmd, "projection-url"))
			book, err := ctx.API.GetClobBook(cmd.Context(), projectionURL, market, outcome)
			if err != nil {
				return err
			}
			depth, err := ctx.API.GetClobDepth(cmd.Context(), projectionURL, market, outcome)
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, map[string]any{
				"book":  book,
				"depth": depth,
			})
		},
	}
	depthCmd.Flags().String("market", "", "Logical market ID for the CLOB proposition")
	depthCmd.Flags().Int("outcome", 0, "Displayed outcome index within the logical market")
	bookCmd.AddCommand(depthCmd)
	cmd.AddCommand(bookCmd)
	cmd.AddCommand(newClobSmokeCommand())

	walletCmd := &cobra.Command{Use: "wallet", Short: "Inspect and prepare the active wallet for hosted CLOB trading"}
	statusCmd := &cobra.Command{
		Use:   "status <market-id-or-address>",
		Short: "Show collateral balances, allowances, approvals, and per-outcome token balances for a CLOB market",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			walletAddress, err := resolveProfileAddress(ctx, nil)
			if override := strings.TrimSpace(mustStringFlag(cmd, "wallet")); override != "" {
				walletAddress = override
				err = nil
			}
			if err != nil {
				return err
			}
			market, err := loadMarketWithClobFallback(cmd.Context(), ctx, args[0], mustStringFlag(cmd, "instance-date"))
			if err != nil {
				return err
			}
			if !isClobMarketImplementation(market.MarketImplementation) {
				return errors.New("clob wallet status requires an AxiomCTFMarket logical market")
			}
			status, err := buildClobWalletStatus(
				cmd.Context(),
				ctx,
				market,
				common.HexToAddress(walletAddress),
				resolveHexAddressOrDefault(mustStringFlag(cmd, "exchange-address"), evm.DefaultClobExchangeAddress),
				resolveHexAddressOrDefault(mustStringFlag(cmd, "outcome-token-address"), evm.DefaultClobConditionalTokens),
			)
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, status)
		},
	}
	statusCmd.Flags().String("wallet", "", "Wallet address to inspect; defaults to the active profile EVM address")
	statusCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	walletCmd.AddCommand(statusCmd)

	approveCmd := &cobra.Command{
		Use:   "approve <market-id-or-address>",
		Short: "Approve collateral and outcome-token spending for the hosted CLOB exchange",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			wallet, privateKeyHex, err := requireEVMWalletWithKey(ctx)
			if err != nil {
				return err
			}
			market, err := loadMarketWithClobFallback(cmd.Context(), ctx, args[0], mustStringFlag(cmd, "instance-date"))
			if err != nil {
				return err
			}
			if !isClobMarketImplementation(market.MarketImplementation) {
				return errors.New("clob wallet approve requires an AxiomCTFMarket logical market")
			}

			skipCollateral := mustBoolFlag(cmd, "skip-collateral")
			skipOutcome := mustBoolFlag(cmd, "skip-outcome")
			if skipCollateral && skipOutcome {
				return errors.New("nothing to approve: remove --skip-collateral or --skip-outcome")
			}

			exchangeAddress := resolveHexAddressOrDefault(mustStringFlag(cmd, "exchange-address"), evm.DefaultClobExchangeAddress)
			outcomeToken := resolveHexAddressOrDefault(mustStringFlag(cmd, "outcome-token-address"), evm.DefaultClobConditionalTokens)
			collateralToken := resolveClobCollateralToken(market)
			transactions := make([]map[string]any, 0, 2)

			if !skipCollateral {
				approveAmount, parseErr := evm.ParseBigInt(firstNonEmpty(strings.TrimSpace(mustStringFlag(cmd, "collateral-amount")), clobMaxUint256))
				if parseErr != nil {
					return parseErr
				}
				txHash, approveErr := approveERC20(cmd.Context(), ctx.Config.EVMRPCURL, big.NewInt(xrplEVMChainID), privateKeyHex, collateralToken, exchangeAddress, approveAmount)
				if approveErr != nil {
					return approveErr
				}
				entry := map[string]any{
					"kind":          "collateral-approve",
					"walletAddress": wallet.Address().Hex(),
					"token":         collateralToken.Hex(),
					"spender":       exchangeAddress.Hex(),
					"amountWei":     approveAmount.String(),
					"amountXrp":     formatWeiToXRP(approveAmount),
					"txHash":        txHash.Hex(),
				}
				if mustBoolFlag(cmd, "wait") {
					receipt, waitErr := waitForReceipt(cmd.Context(), ctx.Config.EVMRPCURL, txHash)
					if waitErr != nil {
						return waitErr
					}
					entry["receiptStatus"] = receipt.Status
				}
				transactions = append(transactions, entry)
			}

			if !skipOutcome {
				txHash, approveErr := setERC1155ApprovalForAll(cmd.Context(), ctx.Config.EVMRPCURL, big.NewInt(xrplEVMChainID), privateKeyHex, outcomeToken, exchangeAddress, true)
				if approveErr != nil {
					return approveErr
				}
				entry := map[string]any{
					"kind":          "outcome-approval-for-all",
					"walletAddress": wallet.Address().Hex(),
					"token":         outcomeToken.Hex(),
					"operator":      exchangeAddress.Hex(),
					"approved":      true,
					"txHash":        txHash.Hex(),
				}
				if mustBoolFlag(cmd, "wait") {
					receipt, waitErr := waitForReceipt(cmd.Context(), ctx.Config.EVMRPCURL, txHash)
					if waitErr != nil {
						return waitErr
					}
					entry["receiptStatus"] = receipt.Status
				}
				transactions = append(transactions, entry)
			}

			return printOutput(ctx.JSON, map[string]any{
				"market":        market.Title,
				"marketId":      market.ID,
				"walletAddress": wallet.Address().Hex(),
				"transactions":  transactions,
			})
		},
	}
	approveCmd.Flags().String("collateral-amount", clobMaxUint256, "Collateral approval amount in wei; defaults to max uint256")
	approveCmd.Flags().Bool("skip-collateral", false, "Skip ERC-20 collateral approval")
	approveCmd.Flags().Bool("skip-outcome", false, "Skip ERC-1155 setApprovalForAll")
	approveCmd.Flags().Bool("wait", false, "Wait for the approval transaction receipts")
	approveCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	walletCmd.AddCommand(approveCmd)
	cmd.AddCommand(walletCmd)

	ordersCmd := &cobra.Command{Use: "orders", Short: "Inspect hosted CLOB orders"}
	ordersListCmd := &cobra.Command{
		Use:   "list",
		Short: "List hosted CLOB orders for a logical proposition or wallet",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			market := strings.TrimSpace(mustStringFlag(cmd, "market"))
			mine := mustBoolFlag(cmd, "mine")
			outcome, err := cmd.Flags().GetInt("outcome")
			if err != nil {
				return err
			}
			maker := strings.TrimSpace(mustStringFlag(cmd, "maker"))
			if mine {
				maker = firstNonEmpty(maker, ctx.Profile.EVMAddress)
			}
			if market == "" && maker == "" {
				return errors.New("provide --market with --outcome, or use --maker/--mine for wallet-wide order history")
			}
			status := strings.TrimSpace(mustStringFlag(cmd, "status"))
			limit, err := cmd.Flags().GetInt("limit")
			if err != nil {
				return err
			}
			activeOnly, err := cmd.Flags().GetBool("active-only")
			if err != nil {
				return err
			}
			projectionURL := strings.TrimSpace(mustStringFlag(cmd, "projection-url"))
			filters := url.Values{}
			if market != "" {
				filters.Set("clob_id", fmt.Sprintf("%s-%d", market, outcome))
			}
			if maker != "" {
				filters.Set("maker", maker)
			}
			if status != "" {
				filters.Set("status", status)
			}
			if activeOnly {
				filters.Set("active_only", "true")
			}
			if limit > 0 {
				filters.Set("limit", strconv.Itoa(limit))
			}
			orders, err := ctx.API.ListClobOrders(cmd.Context(), projectionURL, filters)
			if err != nil {
				return err
			}
			payload := map[string]any{"items": orders, "total": len(orders)}
			if market != "" {
				payload["market"] = market
				payload["outcome"] = outcome
			}
			if maker != "" {
				payload["maker"] = maker
			}
			return printOutput(ctx.JSON, payload)
		},
	}
	ordersListCmd.Flags().String("market", "", "Logical market ID for the CLOB proposition; optional when using --maker or --mine")
	ordersListCmd.Flags().Int("outcome", 0, "Displayed outcome index within the logical market")
	ordersListCmd.Flags().String("maker", "", "Optional maker wallet filter")
	ordersListCmd.Flags().Bool("mine", false, "Filter orders to the active profile wallet")
	ordersListCmd.Flags().String("status", "", "Optional order status filter")
	ordersListCmd.Flags().Bool("active-only", false, "Only return resting active orders")
	ordersListCmd.Flags().Int("limit", 20, "Maximum number of orders to return")
	ordersCmd.AddCommand(ordersListCmd)
	cmd.AddCommand(ordersCmd)

	fillsCmd := &cobra.Command{Use: "fills", Short: "Inspect hosted CLOB fills"}
	fillsListCmd := &cobra.Command{
		Use:   "list",
		Short: "List hosted CLOB fills for a logical proposition or wallet",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			market := strings.TrimSpace(mustStringFlag(cmd, "market"))
			mine := mustBoolFlag(cmd, "mine")
			outcome, err := cmd.Flags().GetInt("outcome")
			if err != nil {
				return err
			}
			wallet := strings.TrimSpace(mustStringFlag(cmd, "wallet"))
			if mine {
				wallet = firstNonEmpty(wallet, ctx.Profile.EVMAddress)
			}
			if market == "" && wallet == "" {
				return errors.New("provide --market with --outcome, or use --wallet/--mine for wallet-wide fill history")
			}
			limit, err := cmd.Flags().GetInt("limit")
			if err != nil {
				return err
			}
			projectionURL := strings.TrimSpace(mustStringFlag(cmd, "projection-url"))
			filters := url.Values{}
			if market != "" {
				filters.Set("clob_id", fmt.Sprintf("%s-%d", market, outcome))
			}
			if wallet != "" {
				filters.Set("wallet", wallet)
			}
			if limit > 0 {
				filters.Set("limit", strconv.Itoa(limit))
			}
			fills, err := ctx.API.ListClobFills(cmd.Context(), projectionURL, filters)
			if err != nil {
				return err
			}
			payload := map[string]any{"items": fills, "total": len(fills)}
			if market != "" {
				payload["market"] = market
				payload["outcome"] = outcome
			}
			if wallet != "" {
				payload["wallet"] = wallet
			}
			return printOutput(ctx.JSON, payload)
		},
	}
	fillsListCmd.Flags().String("market", "", "Logical market ID for the CLOB proposition; optional when using --wallet or --mine")
	fillsListCmd.Flags().Int("outcome", 0, "Displayed outcome index within the logical market")
	fillsListCmd.Flags().String("wallet", "", "Optional wallet filter for buyer or seller participation")
	fillsListCmd.Flags().Bool("mine", false, "Filter fills to the active profile wallet")
	fillsListCmd.Flags().Int("limit", 20, "Maximum number of fills to return")
	fillsCmd.AddCommand(fillsListCmd)
	cmd.AddCommand(fillsCmd)

	orderCmd := &cobra.Command{Use: "order", Short: "Manage hosted CLOB orders"}
	placeCmd := &cobra.Command{
		Use:   "place <market-id-or-address>",
		Short: "Sign and submit a hosted CLOB order for a logical CTF market",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			wallet, _, err := requireEVMWalletWithKey(ctx)
			if err != nil {
				return err
			}
			market, err := loadMarketWithClobFallback(cmd.Context(), ctx, args[0], mustStringFlag(cmd, "instance-date"))
			if err != nil {
				return err
			}
			if !isClobMarketImplementation(market.MarketImplementation) {
				return errors.New("clob order place requires an AxiomCTFMarket logical market")
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

			side := strings.ToLower(strings.TrimSpace(mustStringFlag(cmd, "side")))
			orderType := strings.ToLower(strings.TrimSpace(mustStringFlag(cmd, "type")))
			quantity, err := parseClobQuantity(mustStringFlag(cmd, "quantity"))
			if err != nil {
				return err
			}
			priceBps := 0
			if orderType != "market" {
				priceBps, err = parseClobPriceToBps(mustStringFlag(cmd, "price"))
				if err != nil {
					return err
				}
			}

			payload, err := buildClobSignedOrder(
				wallet,
				market.ID,
				selection,
				side,
				orderType,
				priceBps,
				quantity,
				mustStringFlag(cmd, "expiry"),
				big.NewInt(xrplEVMChainID),
			)
			if err != nil {
				return err
			}

			if mustBoolFlag(cmd, "dry-run") {
				preview := map[string]any{
					"market":          market.Title,
					"marketId":        market.ID,
					"outcomeLabel":    selection.LogicalOutcome.Label,
					"outcomeIndex":    selection.Binding.OutcomeIndex,
					"displayedSide":   selection.DisplayedSide,
					"side":            side,
					"orderType":       orderType,
					"priceBps":        priceBps,
					"quantity":        quantity,
					"maker":           payload.Maker,
					"collateralToken": payload.CollateralToken,
					"outcomeToken":    payload.OutcomeToken,
					"outcomeTokenId":  payload.OutcomeTokenID,
					"makerAmount":     payload.MakerAmount,
					"takerAmount":     payload.TakerAmount,
					"expiration":      payload.Expiration,
					"nonce":           payload.Nonce,
				}
				return printOutput(ctx.JSON, map[string]any{"dryRun": true, "order": preview})
			}

			response, err := ctx.API.SubmitClobOrder(cmd.Context(), strings.TrimSpace(mustStringFlag(cmd, "eventstore-url")), payload)
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, map[string]any{
				"market":          market.Title,
				"marketId":        market.ID,
				"outcomeLabel":    selection.LogicalOutcome.Label,
				"outcomeIndex":    selection.Binding.OutcomeIndex,
				"displayedSide":   selection.DisplayedSide,
				"side":            side,
				"orderType":       orderType,
				"priceBps":        priceBps,
				"quantity":        quantity,
				"orderId":         response.OrderID,
				"tradeCount":      response.TradeCount,
				"remainingShares": response.RemainingQuantity,
				"resting":         response.WasAddedToBook,
				"message":         describeClobOrderResult(orderType, response),
			})
		},
	}
	placeCmd.Flags().String("outcome", "", "Logical outcome index to trade")
	placeCmd.Flags().String("label", "", "Logical outcome label to trade")
	placeCmd.Flags().String("displayed-side", "", "Displayed side to trade: yes or no; inferred for single-binding binary markets")
	placeCmd.Flags().String("side", "buy", "Order side: buy or sell")
	placeCmd.Flags().String("type", "limit", "Order type: limit, market, ioc, fok")
	placeCmd.Flags().String("price", "", "Limit price in displayed percent units, for example 52.5")
	placeCmd.Flags().String("quantity", "", "Whole-number share quantity")
	placeCmd.Flags().String("expiry", "24h", "Expiry preset: 1h, 24h, 7d, never")
	placeCmd.Flags().Bool("dry-run", false, "Build and sign the order locally without submitting it")
	placeCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	orderCmd.AddCommand(placeCmd)

	getOrderCmd := &cobra.Command{
		Use:   "get --order-id <id>",
		Short: "Fetch a single hosted CLOB order by ID",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			orderID := strings.TrimSpace(mustStringFlag(cmd, "order-id"))
			if orderID == "" {
				return errors.New("--order-id is required")
			}
			order, err := ctx.API.GetClobOrder(cmd.Context(), strings.TrimSpace(mustStringFlag(cmd, "projection-url")), orderID)
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, order)
		},
	}
	getOrderCmd.Flags().String("order-id", "", "Hosted order UUID")
	orderCmd.AddCommand(getOrderCmd)

	cancelCmd := &cobra.Command{
		Use:   "cancel --order-id <id> --market <id> --outcome <index>",
		Short: "Cancel a hosted resting CLOB order using the requester wallet address",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			orderID := strings.TrimSpace(mustStringFlag(cmd, "order-id"))
			market := strings.TrimSpace(mustStringFlag(cmd, "market"))
			requester := strings.TrimSpace(mustStringFlag(cmd, "requester"))
			if orderID == "" {
				return errors.New("--order-id is required")
			}
			if market == "" {
				return errors.New("--market is required")
			}
			if requester == "" {
				requester = strings.TrimSpace(ctx.Profile.EVMAddress)
			}
			if requester == "" {
				return errors.New("--requester is required when the active profile does not have an EVM wallet configured")
			}
			outcome, err := cmd.Flags().GetInt("outcome")
			if err != nil {
				return err
			}
			reason := strings.TrimSpace(mustStringFlag(cmd, "reason"))
			eventstoreURL := strings.TrimSpace(mustStringFlag(cmd, "eventstore-url"))
			response, err := ctx.API.CancelClobOrder(cmd.Context(), eventstoreURL, orderID, api.ClobCancelOrderRequest{
				Market:    market,
				Outcome:   outcome,
				Requester: requester,
				Reason:    reason,
			})
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, response)
		},
	}
	cancelCmd.Flags().String("order-id", "", "Order UUID to cancel")
	cancelCmd.Flags().String("market", "", "Logical market ID for the order book")
	cancelCmd.Flags().Int("outcome", 0, "Displayed outcome index within the logical market")
	cancelCmd.Flags().String("requester", "", "Requester wallet address; defaults to the active profile EVM address")
	cancelCmd.Flags().String("reason", "user-requested", "Optional cancellation reason")
	orderCmd.AddCommand(cancelCmd)
	cmd.AddCommand(orderCmd)

	fillGetCmd := &cobra.Command{
		Use:   "get --fill-id <id>",
		Short: "Fetch a single hosted CLOB fill by ID",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			fillID := strings.TrimSpace(mustStringFlag(cmd, "fill-id"))
			if fillID == "" {
				return errors.New("--fill-id is required")
			}
			fill, err := ctx.API.GetClobFill(cmd.Context(), strings.TrimSpace(mustStringFlag(cmd, "projection-url")), fillID)
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, fill)
		},
	}
	fillGetCmd.Flags().String("fill-id", "", "Hosted fill UUID")
	fillsCmd.AddCommand(fillGetCmd)

	splitCmd := &cobra.Command{
		Use:   "split <market-id-or-address>",
		Short: "Split collateral into a complete set of YES and NO conditional tokens",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			wallet, privateKeyHex, err := requireEVMWalletWithKey(ctx)
			if err != nil {
				return err
			}
			market, err := loadMarketWithClobFallback(cmd.Context(), ctx, args[0], mustStringFlag(cmd, "instance-date"))
			if err != nil {
				return err
			}
			if !isClobMarketImplementation(market.MarketImplementation) {
				return errors.New("clob split requires an AxiomCTFMarket logical market")
			}
			binding, err := resolveSplitMergeBinding(market, mustStringFlag(cmd, "label"))
			if err != nil {
				return err
			}
			conditionalTokens := resolveHexAddressOrDefault(mustStringFlag(cmd, "outcome-token-address"), evm.DefaultClobConditionalTokens)
			collateralToken := resolveClobCollateralToken(market)
			conditionID := common.HexToHash(binding.ConditionID)
			partition := []*big.Int{big.NewInt(1), big.NewInt(2)}
			amountStr := strings.TrimSpace(mustStringFlag(cmd, "amount"))
			if amountStr == "" {
				return errors.New("--amount is required")
			}
			amount, err := evm.ParseBigInt(amountStr)
			if err != nil {
				return fmt.Errorf("invalid --amount: %w", err)
			}
			if amount.Sign() <= 0 {
				return errors.New("--amount must be greater than zero")
			}

			if mustBoolFlag(cmd, "dry-run") {
				yesTokenID, _, _ := resolveDisplayedTokenID(binding, "yes", collateralToken)
				noTokenID, _, _ := resolveDisplayedTokenID(binding, "no", collateralToken)
				preview := map[string]any{
					"dryRun":            true,
					"action":            "split",
					"market":            market.Title,
					"marketId":          market.ID,
					"outcomeLabel":      binding.Label,
					"conditionalTokens": conditionalTokens.Hex(),
					"collateralToken":   collateralToken.Hex(),
					"conditionId":       conditionID.Hex(),
					"partition":         []string{"1", "2"},
					"amountWei":         amount.String(),
					"amountXrp":         formatWeiToXRP(amount),
					"wallet":            wallet.Address().Hex(),
				}
				if yesTokenID != nil {
					preview["yesTokenId"] = yesTokenID.String()
				}
				if noTokenID != nil {
					preview["noTokenId"] = noTokenID.String()
				}
				return printOutput(ctx.JSON, preview)
			}

			txHash, err := splitPosition(cmd.Context(), ctx.Config.EVMRPCURL, big.NewInt(xrplEVMChainID), privateKeyHex, conditionalTokens, collateralToken, conditionID, partition, amount)
			if err != nil {
				return fmt.Errorf("split transaction failed: %w", err)
			}
			result := map[string]any{
				"action":       "split",
				"market":       market.Title,
				"marketId":     market.ID,
				"outcomeLabel": binding.Label,
				"amountWei":    amount.String(),
				"amountXrp":    formatWeiToXRP(amount),
				"txHash":       txHash.Hex(),
				"wallet":       wallet.Address().Hex(),
			}
			if mustBoolFlag(cmd, "wait") {
				receipt, waitErr := waitForReceipt(cmd.Context(), ctx.Config.EVMRPCURL, txHash)
				if waitErr != nil {
					return waitErr
				}
				result["receiptStatus"] = receipt.Status
				if receipt.Status == 0 {
					return fmt.Errorf("split transaction reverted (tx %s)", txHash.Hex())
				}
			}
			return printOutput(ctx.JSON, result)
		},
	}
	splitCmd.Flags().String("label", "", "Outcome label to identify the binding for split")
	splitCmd.Flags().String("amount", "", "Amount of collateral to split in wei")
	splitCmd.Flags().Bool("wait", false, "Wait for the split transaction receipt")
	splitCmd.Flags().Bool("dry-run", false, "Preview the split without broadcasting a transaction")
	splitCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	cmd.AddCommand(splitCmd)

	mergeCmd := &cobra.Command{
		Use:   "merge <market-id-or-address>",
		Short: "Merge matching YES and NO conditional tokens back into collateral",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			wallet, privateKeyHex, err := requireEVMWalletWithKey(ctx)
			if err != nil {
				return err
			}
			market, err := loadMarketWithClobFallback(cmd.Context(), ctx, args[0], mustStringFlag(cmd, "instance-date"))
			if err != nil {
				return err
			}
			if !isClobMarketImplementation(market.MarketImplementation) {
				return errors.New("clob merge requires an AxiomCTFMarket logical market")
			}
			binding, err := resolveSplitMergeBinding(market, mustStringFlag(cmd, "label"))
			if err != nil {
				return err
			}
			conditionalTokens := resolveHexAddressOrDefault(mustStringFlag(cmd, "outcome-token-address"), evm.DefaultClobConditionalTokens)
			collateralToken := resolveClobCollateralToken(market)
			conditionID := common.HexToHash(binding.ConditionID)
			partition := []*big.Int{big.NewInt(1), big.NewInt(2)}
			amountStr := strings.TrimSpace(mustStringFlag(cmd, "amount"))
			if amountStr == "" {
				return errors.New("--amount is required")
			}
			amount, err := evm.ParseBigInt(amountStr)
			if err != nil {
				return fmt.Errorf("invalid --amount: %w", err)
			}
			if amount.Sign() <= 0 {
				return errors.New("--amount must be greater than zero")
			}

			if mustBoolFlag(cmd, "dry-run") {
				yesTokenID, _, _ := resolveDisplayedTokenID(binding, "yes", collateralToken)
				noTokenID, _, _ := resolveDisplayedTokenID(binding, "no", collateralToken)
				preview := map[string]any{
					"dryRun":            true,
					"action":            "merge",
					"market":            market.Title,
					"marketId":          market.ID,
					"outcomeLabel":      binding.Label,
					"conditionalTokens": conditionalTokens.Hex(),
					"collateralToken":   collateralToken.Hex(),
					"conditionId":       conditionID.Hex(),
					"partition":         []string{"1", "2"},
					"amountWei":         amount.String(),
					"amountXrp":         formatWeiToXRP(amount),
					"wallet":            wallet.Address().Hex(),
				}
				if yesTokenID != nil {
					preview["yesTokenId"] = yesTokenID.String()
				}
				if noTokenID != nil {
					preview["noTokenId"] = noTokenID.String()
				}
				return printOutput(ctx.JSON, preview)
			}

			txHash, err := mergePositions(cmd.Context(), ctx.Config.EVMRPCURL, big.NewInt(xrplEVMChainID), privateKeyHex, conditionalTokens, collateralToken, conditionID, partition, amount)
			if err != nil {
				return fmt.Errorf("merge transaction failed: %w", err)
			}
			result := map[string]any{
				"action":       "merge",
				"market":       market.Title,
				"marketId":     market.ID,
				"outcomeLabel": binding.Label,
				"amountWei":    amount.String(),
				"amountXrp":    formatWeiToXRP(amount),
				"txHash":       txHash.Hex(),
				"wallet":       wallet.Address().Hex(),
			}
			if mustBoolFlag(cmd, "wait") {
				receipt, waitErr := waitForReceipt(cmd.Context(), ctx.Config.EVMRPCURL, txHash)
				if waitErr != nil {
					return waitErr
				}
				result["receiptStatus"] = receipt.Status
				if receipt.Status == 0 {
					return fmt.Errorf("merge transaction reverted (tx %s)", txHash.Hex())
				}
			}
			return printOutput(ctx.JSON, result)
		},
	}
	mergeCmd.Flags().String("label", "", "Outcome label to identify the binding for merge")
	mergeCmd.Flags().String("amount", "", "Amount of matched YES+NO shares to merge in wei")
	mergeCmd.Flags().Bool("wait", false, "Wait for the merge transaction receipt")
	mergeCmd.Flags().Bool("dry-run", false, "Preview the merge without broadcasting a transaction")
	mergeCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	cmd.AddCommand(mergeCmd)

	splitStatusCmd := &cobra.Command{
		Use:   "split-status <market-id-or-address>",
		Short: "Show split and merge eligibility, balances, and max amounts for a CLOB market",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			walletAddress, err := resolveProfileAddress(ctx, nil)
			if override := strings.TrimSpace(mustStringFlag(cmd, "wallet")); override != "" {
				walletAddress = override
				err = nil
			}
			if err != nil {
				return err
			}
			market, err := loadMarketWithClobFallback(cmd.Context(), ctx, args[0], mustStringFlag(cmd, "instance-date"))
			if err != nil {
				return err
			}
			if !isClobMarketImplementation(market.MarketImplementation) {
				return errors.New("clob split-status requires an AxiomCTFMarket logical market")
			}
			binding, err := resolveSplitMergeBinding(market, mustStringFlag(cmd, "label"))
			if err != nil {
				return err
			}
			conditionalTokens := resolveHexAddressOrDefault(mustStringFlag(cmd, "outcome-token-address"), evm.DefaultClobConditionalTokens)
			collateralToken := resolveClobCollateralToken(market)
			owner := common.HexToAddress(walletAddress)

			collateralBalance, err := getERC20Balance(cmd.Context(), ctx.Config.EVMRPCURL, collateralToken, owner)
			if err != nil {
				return err
			}
			collateralAllowance, err := getERC20Allowance(cmd.Context(), ctx.Config.EVMRPCURL, collateralToken, owner, conditionalTokens)
			if err != nil {
				return err
			}
			yesTokenID, _, _ := resolveDisplayedTokenID(binding, "yes", collateralToken)
			noTokenID, _, _ := resolveDisplayedTokenID(binding, "no", collateralToken)
			yesBalance := big.NewInt(0)
			noBalance := big.NewInt(0)
			if yesTokenID != nil {
				yesBalance, err = getERC1155Balance(cmd.Context(), ctx.Config.EVMRPCURL, conditionalTokens, owner, yesTokenID)
				if err != nil {
					return err
				}
			}
			if noTokenID != nil {
				noBalance, err = getERC1155Balance(cmd.Context(), ctx.Config.EVMRPCURL, conditionalTokens, owner, noTokenID)
				if err != nil {
					return err
				}
			}
			maxMergeable := new(big.Int).Set(yesBalance)
			if noBalance.Cmp(maxMergeable) < 0 {
				maxMergeable.Set(noBalance)
			}

			summary := summarizeClobSplitStatus(collateralBalance, collateralAllowance, yesBalance, noBalance)

			status := map[string]any{
				"market":                 market.Title,
				"marketId":               market.ID,
				"outcomeLabel":           binding.Label,
				"wallet":                 walletAddress,
				"conditionalTokens":      conditionalTokens.Hex(),
				"collateralToken":        collateralToken.Hex(),
				"conditionId":            binding.ConditionID,
				"collateralBalanceWei":   collateralBalance.String(),
				"collateralBalanceXrp":   formatWeiToXRP(collateralBalance),
				"collateralAllowanceWei": collateralAllowance.String(),
				"collateralAllowanceXrp": formatWeiToXRP(collateralAllowance),
				"splitApproved":          collateralAllowance.Sign() > 0,
				"yesBalanceWei":          yesBalance.String(),
				"yesBalanceXrp":          formatWeiToXRP(yesBalance),
				"noBalanceWei":           noBalance.String(),
				"noBalanceXrp":           formatWeiToXRP(noBalance),
				"maxMergeableWei":        summary.MaxMergeableWei.String(),
				"maxMergeableXrp":        formatWeiToXRP(summary.MaxMergeableWei),
				"mergeApprovalRequired":  summary.MergeApprovalRequired,
				"maxSplitWei":            summary.MaxSplitWei.String(),
				"maxSplitXrp":            formatWeiToXRP(summary.MaxSplitWei),
				"splitReady":             summary.SplitReady,
				"mergeReady":             summary.MergeReady,
			}
			if yesTokenID != nil {
				status["yesTokenId"] = yesTokenID.String()
			}
			if noTokenID != nil {
				status["noTokenId"] = noTokenID.String()
			}
			return printOutput(ctx.JSON, status)
		},
	}
	splitStatusCmd.Flags().String("label", "", "Outcome label to inspect")
	splitStatusCmd.Flags().String("wallet", "", "Wallet address to inspect; defaults to the active profile EVM address")
	splitStatusCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	cmd.AddCommand(splitStatusCmd)

	return cmd
}

func buildCLIContext() (*cliContext, error) {
	cfg, err := app.LoadConfig()
	if err != nil {
		return nil, err
	}
	if flagAPIURL != "" {
		cfg.APIBaseURL = flagAPIURL
	}
	if flagRPCURL != "" {
		cfg.EVMRPCURL = flagRPCURL
	}
	if flagXRPLURL != "" {
		cfg.XRPLRPCURL = flagXRPLURL
	}
	if flagProfile != "" {
		cfg.ActiveProfile = flagProfile
	}
	profile, ok := cfg.Profiles[cfg.ActiveProfile]
	if !ok {
		profile = app.Profile{Name: cfg.ActiveProfile}
		cfg.SetCurrentProfile(profile)
	}
	client, err := api.NewClient(cfg.APIBaseURL, cfg.DeviceID)
	if err != nil {
		return nil, err
	}
	return &cliContext{Config: cfg, API: client, Profile: profile, ProfileName: cfg.ActiveProfile, JSON: flagJSON || cfg.OutputFormat == "json"}, nil
}

func resolveWalletAccountProfile(cmd *cobra.Command, ctx *cliContext) (string, app.Profile) {
	accountName := strings.TrimSpace(mustStringFlag(cmd, "account"))
	if accountName == "" {
		return ctx.ProfileName, ctx.Profile
	}
	profile, ok := ctx.Config.Profiles[accountName]
	if !ok {
		profile = app.Profile{Name: accountName}
	}
	if profile.Name == "" {
		profile.Name = accountName
	}
	return accountName, profile
}

func shouldActivateWalletAccount(cmd *cobra.Command, ctx *cliContext, accountName string) bool {
	return accountName == ctx.ProfileName || mustBoolFlag(cmd, "activate")
}

func requireEVMWallet(ctx *cliContext) (*evm.Wallet, error) {
	wallet, _, err := requireEVMWalletWithKey(ctx)
	return wallet, err
}

func requireEVMWalletWithKey(ctx *cliContext) (*evm.Wallet, string, error) {
	return requireEVMWalletWithKeyForProfile(ctx.Config, ctx.ProfileName)
}

func requireEVMWalletWithKeyForProfile(cfg *app.Config, profileName string) (*evm.Wallet, string, error) {
	secret, err := app.LoadSecret(app.EVMSecretKey(profileName))
	if err != nil {
		return nil, "", fmt.Errorf("no EVM private key stored for profile %q: %w", profileName, err)
	}
	wallet, err := evm.WalletFromPrivateKeyHex(secret)
	if err != nil {
		return nil, "", err
	}
	return wallet, secret, nil
}

func requireXRPLSeed(ctx *cliContext) (string, error) {
	seed, err := app.LoadSecret(app.XRPLSecretKey(ctx.ProfileName))
	if err != nil {
		return "", fmt.Errorf("no XRPL seed stored for profile %q: %w", ctx.ProfileName, err)
	}
	return seed, nil
}

func resolveProfileAddress(ctx *cliContext, args []string) (string, error) {
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		return args[0], nil
	}
	if ctx.Profile.EVMAddress == "" {
		return "", errors.New("no active EVM wallet is configured; pass a wallet address explicitly or create/import a wallet first")
	}
	return ctx.Profile.EVMAddress, nil
}

func printOutput(asJSON bool, value any) error {
	if asJSON {
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal output: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	fmt.Println(ui.Render(value))
	return nil
}

func buildRegistrationMessage(walletAddress string, deviceID string, issuedAt time.Time) string {
	return strings.Join([]string{
		"Axiom CLI registration",
		"",
		"Version: 2",
		fmt.Sprintf("Wallet: %s", walletAddress),
		fmt.Sprintf("Device: %s", strings.TrimSpace(deviceID)),
		fmt.Sprintf("Issued At: %s", formatRegistrationIssuedAt(issuedAt)),
		"Network: xrpl-mainnet",
		"Purpose: create or refresh my Axiom CLI profile and funding destination tag.",
	}, "\n")
}

func buildProfileUpdateMessage(walletAddress string, deviceID string, issuedAt time.Time, displayName string, avatarURL string) string {
	return strings.Join([]string{
		"Axiom CLI profile update",
		"",
		"Version: 1",
		fmt.Sprintf("Wallet: %s", walletAddress),
		fmt.Sprintf("Device: %s", strings.TrimSpace(deviceID)),
		fmt.Sprintf("Issued At: %s", formatRegistrationIssuedAt(issuedAt)),
		fmt.Sprintf("Display Name: %s", optionalProfileMessageValue(displayName)),
		fmt.Sprintf("Avatar URL: %s", optionalProfileMessageValue(avatarURL)),
		"Network: xrpl-mainnet",
		"Purpose: update my Axiom CLI profile metadata.",
	}, "\n")
}

func buildRewardsActionMessage(walletAddress string, deviceID string, issuedAt time.Time, action string, ticketID int, epochID int, txHash string) string {
	return strings.Join([]string{
		"Axiom CLI rewards action",
		"",
		"Version: 1",
		fmt.Sprintf("Wallet: %s", walletAddress),
		fmt.Sprintf("Device: %s", strings.TrimSpace(deviceID)),
		fmt.Sprintf("Issued At: %s", formatRegistrationIssuedAt(issuedAt)),
		fmt.Sprintf("Action: %s", action),
		fmt.Sprintf("Ticket ID: %s", optionalIntMessageValue(ticketID)),
		fmt.Sprintf("Epoch ID: %s", optionalIntMessageValue(epochID)),
		fmt.Sprintf("Transaction Hash: %s", optionalStringMessageValue(txHash)),
		"Network: xrpl-mainnet",
		"Purpose: authorize a rewards claim or rewards-claim sync from the Axiom CLI.",
	}, "\n")
}

func optionalIntMessageValue(value int) string {
	if value <= 0 {
		return "(none)"
	}
	return strconv.Itoa(value)
}

func optionalStringMessageValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "(none)"
	}
	return trimmed
}

func optionalProfileMessageValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "(unchanged)"
	}
	return trimmed
}

func optionalStringPointer(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func formatRegistrationIssuedAt(issuedAt time.Time) string {
	return issuedAt.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
}

func resolveClaimableLotteryTicket(rewards *api.RewardsResponse, args []string) (*api.LotteryTicketInfo, error) {
	if rewards == nil {
		return nil, errors.New("rewards response was empty")
	}

	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		ticketID, err := strconv.Atoi(strings.TrimSpace(args[0]))
		if err != nil || ticketID <= 0 {
			return nil, errors.New("ticket id must be a positive integer")
		}
		for _, ticket := range rewards.LotteryTickets {
			if ticket.ID == ticketID {
				if ticket.Status != "available" {
					return nil, fmt.Errorf("ticket %d is not claimable", ticketID)
				}
				return &ticket, nil
			}
		}
		return nil, fmt.Errorf("ticket %d was not found", ticketID)
	}

	for _, ticket := range rewards.LotteryTickets {
		if ticket.Status == "available" {
			return &ticket, nil
		}
	}

	return nil, errors.New("no available weekly chest ticket found")
}

func resolveClaimableEpochReward(rewards *api.RewardsResponse, args []string) (*api.EpochReward, error) {
	if rewards == nil {
		return nil, errors.New("rewards response was empty")
	}

	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		epochID, err := strconv.Atoi(strings.TrimSpace(args[0]))
		if err != nil || epochID <= 0 {
			return nil, errors.New("epoch id must be a positive integer")
		}
		for _, reward := range rewards.EpochRewards {
			if reward.EpochID == epochID {
				if !reward.Claimable {
					return nil, fmt.Errorf("epoch %d is not claimable", epochID)
				}
				return &reward, nil
			}
		}
		return nil, fmt.Errorf("epoch %d was not found", epochID)
	}

	for _, reward := range rewards.EpochRewards {
		if reward.Claimable {
			return &reward, nil
		}
	}

	return nil, errors.New("no claimable epoch reward found")
}

func resolveAxiomRewardsAddress(ctx context.Context, client *api.Client) (common.Address, error) {
	configResponse, err := client.GetConfig(ctx)
	if err != nil {
		return common.Address{}, err
	}
	address := strings.TrimSpace(configResponse.AxiomRewardsAddress)
	if address == "" {
		return common.Address{}, errors.New("the backend does not have a canonical AxiomRewards address configured")
	}
	if !common.IsHexAddress(address) {
		return common.Address{}, fmt.Errorf("the backend returned an invalid AxiomRewards address: %s", address)
	}
	return common.HexToAddress(address), nil
}

func syncEpochRewardClaim(ctx context.Context, cliCtx *cliContext, wallet *evm.Wallet, epochID int, txHash string) (*api.EpochRewardClaimResponse, error) {
	issuedAt := time.Now().UTC()
	message := buildRewardsActionMessage(wallet.Address().Hex(), cliCtx.Config.DeviceID, issuedAt, "sync-epoch-claim", 0, epochID, txHash)
	signature, err := wallet.SignMessage(message)
	if err != nil {
		return nil, err
	}
	return cliCtx.API.SyncEpochRewardClaim(ctx, wallet.Address().Hex(), epochID, api.RewardsActionRequest{
		WalletAddress: wallet.Address().Hex(),
		Signature:     signature,
		DeviceID:      cliCtx.Config.DeviceID,
		IssuedAt:      formatRegistrationIssuedAt(issuedAt),
		TxHash:        txHash,
	})
}

func parseTxHash(value string) (common.Hash, error) {
	trimmed := strings.TrimSpace(value)
	decoded, err := hexutil.Decode(trimmed)
	if err != nil || len(decoded) != common.HashLength {
		return common.Hash{}, errors.New("--tx-hash must be a 32-byte 0x-prefixed transaction hash")
	}
	return common.BytesToHash(decoded), nil
}

func parseMerkleProof(proof []string) ([]common.Hash, error) {
	parsed := make([]common.Hash, 0, len(proof))
	for _, item := range proof {
		trimmed := strings.TrimSpace(item)
		if len(trimmed) != 66 || !strings.HasPrefix(trimmed, "0x") {
			return nil, fmt.Errorf("invalid merkle proof hash: %s", item)
		}
		parsed = append(parsed, common.HexToHash(trimmed))
	}
	return parsed, nil
}

func registerWalletWithCompat(ctx context.Context, cliCtx *cliContext, wallet *evm.Wallet, referrerCode string) (*api.RegisterResponse, error) {
	issuedAt := time.Now().UTC()
	walletAddress := wallet.Address().Hex()

	return signAndRegisterWallet(ctx, cliCtx, wallet, walletAddress, issuedAt, referrerCode)
}

func signAndRegisterWallet(ctx context.Context, cliCtx *cliContext, wallet *evm.Wallet, walletAddress string, issuedAt time.Time, referrerCode string) (*api.RegisterResponse, error) {
	message := buildRegistrationMessage(walletAddress, cliCtx.Config.DeviceID, issuedAt)
	signature, err := wallet.SignMessage(message)
	if err != nil {
		return nil, err
	}

	return cliCtx.API.RegisterWallet(ctx, api.RegisterRequest{
		WalletAddress: walletAddress,
		Signature:     signature,
		DeviceID:      cliCtx.Config.DeviceID,
		IssuedAt:      formatRegistrationIssuedAt(issuedAt),
		ReferrerCode:  strings.TrimSpace(referrerCode),
	})
}

func isInvalidWalletSignatureError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "api error (401): Invalid wallet signature.")
}

func parseXRPToWei(amount string) (*big.Int, error) {
	value, ok := new(big.Rat).SetString(strings.TrimSpace(amount))
	if !ok {
		return nil, fmt.Errorf("invalid amount: %s", amount)
	}
	if value.Sign() <= 0 {
		return nil, fmt.Errorf("amount must be greater than zero")
	}
	value.Mul(value, big.NewRat(1_000_000_000_000_000_000, 1))
	result := new(big.Int)
	if !value.IsInt() {
		return nil, fmt.Errorf("amount has too many decimal places: %s", amount)
	}
	result.Div(value.Num(), value.Denom())
	return result, nil
}

func formatWeiToXRP(value *big.Int) string {
	rat := new(big.Rat).SetInt(value)
	rat.Quo(rat, big.NewRat(1_000_000_000_000_000_000, 1))
	return rat.FloatString(6)
}

func parseOptionalWei(cmd *cobra.Command, flagName string) (*big.Int, error) {
	value, _ := cmd.Flags().GetString(flagName)
	if strings.TrimSpace(value) == "" {
		return big.NewInt(0), nil
	}
	if value == "0" {
		return big.NewInt(0), nil
	}
	parsed := new(big.Int)
	if _, ok := parsed.SetString(value, 10); !ok {
		return nil, fmt.Errorf("invalid --%s value", flagName)
	}
	return parsed, nil
}

func resolveOutcomeIndex(market *api.MarketDetails, outcomeRaw string, label string) (int, error) {
	if strings.TrimSpace(outcomeRaw) != "" {
		for _, outcome := range market.Outcomes {
			if fmt.Sprintf("%d", outcome.Index) == outcomeRaw {
				return outcome.Index, nil
			}
		}
		return 0, fmt.Errorf("outcome %s does not exist for this market", outcomeRaw)
	}
	if strings.TrimSpace(label) == "" {
		return 0, errors.New("either --outcome or --label is required")
	}
	for _, outcome := range market.Outcomes {
		if strings.EqualFold(strings.TrimSpace(outcome.Label), strings.TrimSpace(label)) {
			return outcome.Index, nil
		}
	}
	labels := make([]string, 0, len(market.Outcomes))
	for _, outcome := range market.Outcomes {
		labels = append(labels, outcome.Label)
	}
	sort.Strings(labels)
	return 0, fmt.Errorf("label %q was not found. Valid labels: %s", label, strings.Join(labels, ", "))
}

func waitForReceipt(ctx context.Context, rpcURL string, txHash common.Hash) (*types.Receipt, error) {
	waitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	return evm.WaitForReceipt(waitCtx, rpcURL, txHash)
}

func mustBoolFlag(cmd *cobra.Command, name string) bool {
	value, _ := cmd.Flags().GetBool(name)
	return value
}

func mustStringFlag(cmd *cobra.Command, name string) string {
	value, _ := cmd.Flags().GetString(name)
	return value
}

func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func buildXRPLPaymentURI(address string, destinationTag int, amount string) string {
	values := make([]string, 0, 2)
	if destinationTag > 0 {
		values = append(values, "dt="+strconv.Itoa(destinationTag))
	}
	if strings.TrimSpace(amount) != "" {
		values = append(values, "amount="+strings.TrimSpace(amount))
	}
	if len(values) == 0 {
		return "xrpl:" + address
	}
	return "xrpl:" + address + "?" + strings.Join(values, "&")
}

func previewBridgeFunding(cmdCtx context.Context, ctx *cliContext, amount string) (*bridgeFundingPreview, error) {
	if strings.TrimSpace(amount) != "" {
		if _, err := parseXRPToWei(amount); err != nil {
			return nil, err
		}
	}
	preview, err := buildBridgeFundingPreview(cmdCtx, ctx, amount)
	if err != nil {
		return nil, err
	}
	preview.QRCode = renderQRCode(preview.PaymentURI)
	preview.Instructions = []string{
		"Send XRP on the XRPL network to the deposit wallet address above.",
		"Include the exact destination tag shown above.",
		"Scan the QR code from your XRPL wallet app to prefill the relay wallet and destination tag.",
		"The relay service will bridge the XRP into the matching XRPL EVM wallet.",
	}
	return preview, nil
}

func submitBridgeFunding(cmdCtx context.Context, ctx *cliContext, amount string) (*bridgeFundingPreview, error) {
	trimmedAmount := strings.TrimSpace(amount)
	if trimmedAmount == "" {
		return nil, errors.New("--amount is required")
	}
	preview, err := buildBridgeFundingPreview(cmdCtx, ctx, trimmedAmount)
	if err != nil {
		return nil, err
	}
	seed, err := requireXRPLSeed(ctx)
	if err != nil {
		return nil, err
	}
	txHash, err := submitBridgePayment(cmdCtx, ctx.Config.XRPLRPCURL, seed, preview.DepositWalletAddress, preview.DestinationTag, trimmedAmount)
	if err != nil {
		return nil, err
	}
	preview.Submit = true
	preview.TxHash = txHash
	return preview, nil
}

func buildBridgeFundingPreview(cmdCtx context.Context, ctx *cliContext, amount string) (*bridgeFundingPreview, error) {
	if ctx.Profile.EVMAddress == "" {
		return nil, errors.New("no EVM wallet is configured for the active profile")
	}
	if ctx.Profile.DepositDestinationTag == 0 {
		return nil, errors.New("destination tag missing; run `axiom auth register` first")
	}
	trimmedAmount := strings.TrimSpace(amount)
	funding, err := ctx.API.GetFunding(cmdCtx, ctx.Profile.EVMAddress, 10)
	if err != nil {
		return nil, err
	}
	if funding.DepositWalletAddress == "" {
		return nil, errors.New("the server has no deposit wallet configured")
	}
	destinationTag := valueOrZero(funding.DepositDestinationTag)
	if destinationTag == 0 {
		destinationTag = ctx.Profile.DepositDestinationTag
	}
	return &bridgeFundingPreview{
		DepositWalletAddress: funding.DepositWalletAddress,
		DestinationTag:       destinationTag,
		AmountXRP:            trimmedAmount,
		PaymentURI:           buildXRPLPaymentURI(funding.DepositWalletAddress, destinationTag, trimmedAmount),
		FromXRPLWallet:       ctx.Profile.XRPLAddress,
	}, nil
}

func printBridgeFundingOutput(asJSON bool, preview *bridgeFundingPreview) error {
	if asJSON {
		return printOutput(asJSON, map[string]any{
			"depositWalletAddress":  preview.DepositWalletAddress,
			"destinationTag":        preview.DestinationTag,
			"amountXrp":             preview.AmountXRP,
			"paymentUri":            preview.PaymentURI,
			"qrCode":                preview.QRCode,
			"instructions":          preview.Instructions,
			"submit":                preview.Submit,
			"fromXRPLWalletAddress": preview.FromXRPLWallet,
			"txHash":                preview.TxHash,
		})
	}
	fmt.Println(renderBridgeFundingPreview(*preview))
	return nil
}

func renderQRCode(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}
	var b strings.Builder
	config := qrterminal.Config{
		Level:          qrterminal.L,
		Writer:         &b,
		HalfBlocks:     true,
		BlackChar:      qrterminal.BLACK_BLACK,
		BlackWhiteChar: qrterminal.BLACK_WHITE,
		WhiteChar:      qrterminal.WHITE_WHITE,
		WhiteBlackChar: qrterminal.WHITE_BLACK,
		QuietZone:      1,
	}
	qrterminal.GenerateWithConfig(value, config)
	return strings.TrimRight(b.String(), "\n")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func renderBridgeFundingPreview(preview bridgeFundingPreview) string {
	sections := []string{
		ui.Render(map[string]any{
			"depositWalletAddress": preview.DepositWalletAddress,
			"destinationTag":       preview.DestinationTag,
			"amountXrp":            firstNonEmpty(preview.AmountXRP, "scan to fill manually"),
			"paymentUri":           preview.PaymentURI,
			"submit":               preview.Submit,
		}),
	}

	if preview.Submit && preview.TxHash != "" {
		sections = append(sections, "", ui.Render(map[string]any{
			"fromXRPLWalletAddress": preview.FromXRPLWallet,
			"txHash":                preview.TxHash,
		}))
		return strings.Join(sections, "\n")
	}

	if len(preview.Instructions) > 0 {
		sections = append(sections, "", "Bridge Funding QR", strings.Repeat("=", len("Bridge Funding QR")), preview.QRCode)
		sections = append(sections, "", "Instructions", strings.Repeat("=", len("Instructions")))
		for _, instruction := range preview.Instructions {
			sections = append(sections, "• "+instruction)
		}
	}

	return strings.Join(sections, "\n")
}

func filterMarketsByType(response *api.MarketsResponse, marketType string) *api.MarketsResponse {
	trimmed := normalizeMarketImplementation(strings.TrimSpace(marketType))
	filtered := make([]api.MarketListItem, 0, len(response.Items))
	for _, item := range response.Items {
		if strings.EqualFold(strings.TrimSpace(item.MarketImplementation), trimmed) {
			filtered = append(filtered, item)
		}
	}
	return &api.MarketsResponse{
		Items:  filtered,
		Total:  len(filtered),
		Limit:  len(filtered),
		Offset: 0,
	}
}

func normalizeMarketImplementation(input string) string {
	switch strings.ToLower(input) {
	case "clob", "ctf", "axiomctfmarket":
		return "AxiomCTFMarket"
	case "parimutuel", "tieredparimutuel":
		return "TieredParimutuel"
	default:
		return input
	}
}

func filterMarketsByCategory(response *api.MarketsResponse, category string, limit int, offset int) *api.MarketsResponse {
	trimmedCategory := strings.TrimSpace(category)
	filtered := make([]api.MarketListItem, 0, len(response.Items))
	for _, item := range response.Items {
		if strings.EqualFold(strings.TrimSpace(item.Category), trimmedCategory) {
			filtered = append(filtered, item)
		}
	}

	if offset < 0 {
		offset = 0
	}
	if offset > len(filtered) {
		offset = len(filtered)
	}

	end := len(filtered)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}

	items := filtered[offset:end]
	resultLimit := len(items)
	if limit > 0 {
		resultLimit = limit
	}

	return &api.MarketsResponse{
		Items:  items,
		Total:  len(filtered),
		Limit:  resultLimit,
		Offset: offset,
	}
}

func normalizeCLIArgs(args []string) []string {
	if len(args) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(args))
	amountFlags := map[string]struct{}{
		"--amount": {},
	}
	for index := 0; index < len(args); index++ {
		current := args[index]
		if _, ok := amountFlags[current]; ok {
			if index+2 < len(args) && args[index+1] == "--" && strings.HasPrefix(args[index+2], "-") {
				normalized = append(normalized, current+"="+args[index+2])
				index += 2
				continue
			}
			if index+1 < len(args) && strings.HasPrefix(args[index+1], "-") && args[index+1] != "--" && !strings.HasPrefix(args[index+1], "--") {
				normalized = append(normalized, current+"="+args[index+1])
				index++
				continue
			}
		}
		normalized = append(normalized, current)
	}
	return normalized
}

func rewriteAmountFlagParseError(args []string, err error) error {
	if err == nil {
		return nil
	}
	message := err.Error()
	if !strings.Contains(message, "unknown shorthand flag") {
		return nil
	}
	for _, arg := range args {
		if strings.HasPrefix(arg, "--amount=-") {
			return errors.New("amount must be greater than zero")
		}
	}
	return nil
}

func filterPositionsByStatus(response *api.PositionsResponse, status string, limit int) *api.PositionsResponse {
	if response == nil {
		return &api.PositionsResponse{}
	}
	trimmedStatus := strings.TrimSpace(strings.ToLower(status))
	if trimmedStatus == "" || trimmedStatus == "all" {
		if limit > 0 && len(response.Items) > limit {
			return &api.PositionsResponse{Items: response.Items[:limit], Total: response.Total}
		}
		return response
	}
	filtered := make([]api.PositionItem, 0, len(response.Items))
	for _, item := range response.Items {
		if positionMatchesStatus(item, trimmedStatus) {
			filtered = append(filtered, item)
		}
	}
	total := len(filtered)
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return &api.PositionsResponse{Items: filtered, Total: total}
}

func positionMatchesStatus(item api.PositionItem, status string) bool {
	current := strings.TrimSpace(strings.ToLower(item.Status))
	switch status {
	case "open":
		return current == "open" || current == "active" || current == "unresolved"
	case "won", "lost":
		return current == status
	default:
		return current == status
	}
}

func filterMarketsByPositions(response *api.MarketsResponse, positions *api.PositionsResponse) *api.MarketsResponse {
	if response == nil || positions == nil {
		return response
	}
	marketIDs := make(map[string]struct{}, len(positions.Items)*2)
	for _, item := range positions.Items {
		if trimmed := strings.TrimSpace(strings.ToLower(item.MarketID)); trimmed != "" {
			marketIDs[trimmed] = struct{}{}
		}
		if trimmed := strings.TrimSpace(strings.ToLower(item.MarketAddress)); trimmed != "" {
			marketIDs[trimmed] = struct{}{}
		}
	}
	filtered := make([]api.MarketListItem, 0, len(response.Items))
	for _, item := range response.Items {
		if _, ok := marketIDs[strings.TrimSpace(strings.ToLower(item.ID))]; ok {
			filtered = append(filtered, item)
			continue
		}
		if _, ok := marketIDs[strings.TrimSpace(strings.ToLower(item.ContractAddress))]; ok {
			filtered = append(filtered, item)
		}
	}
	return &api.MarketsResponse{Items: filtered, Total: len(filtered), Limit: response.Limit, Offset: response.Offset}
}

func paginateMarkets(response *api.MarketsResponse, limit int, offset int) *api.MarketsResponse {
	if response == nil {
		return &api.MarketsResponse{}
	}
	items := response.Items
	if offset < 0 {
		offset = 0
	}
	if offset > len(items) {
		offset = len(items)
	}
	end := len(items)
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	paged := items[offset:end]
	resultLimit := len(paged)
	if limit > 0 {
		resultLimit = limit
	}
	return &api.MarketsResponse{Items: paged, Total: len(items), Limit: resultLimit, Offset: offset}
}

func enrichMarketsWithSpotPrices(ctx context.Context, cliCtx *cliContext, response *api.MarketsResponse) {
	if response == nil {
		return
	}
	for index := range response.Items {
		item := &response.Items[index]
		if isClobMarketImplementation(item.MarketImplementation) {
			continue
		}
		if strings.TrimSpace(item.ContractAddress) == "" || len(item.Outcomes) == 0 {
			continue
		}
		state, err := loadMarketState(ctx, cliCtx.Config.EVMRPCURL, common.HexToAddress(item.ContractAddress))
		if err != nil || state == nil {
			continue
		}
		item.CurrentSpotPrices = marketSpotPrices(state, item.Outcomes)
	}
}

func isClobMarketImplementation(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "AxiomCTFMarket")
}

func marketSpotPrices(state *evm.MarketState, outcomes []api.Outcome) []api.OutcomeSpotPrice {
	if state == nil || len(outcomes) == 0 {
		return nil
	}
	total := new(big.Int).Add(cloneBigInt(state.TotalPool), cloneBigInt(state.TotalVirtualPool))
	if total.Sign() == 0 {
		return nil
	}
	prices := make([]api.OutcomeSpotPrice, 0, len(outcomes))
	for _, outcome := range outcomes {
		if outcome.Index < 0 || outcome.Index >= len(state.OutcomePools) || outcome.Index >= len(state.VirtualSeeds) {
			continue
		}
		numerator := new(big.Int).Add(cloneBigInt(state.OutcomePools[outcome.Index]), cloneBigInt(state.VirtualSeeds[outcome.Index]))
		prices = append(prices, api.OutcomeSpotPrice{
			Index:            outcome.Index,
			Label:            outcome.Label,
			CurrentSpotPrice: formatRatioPercent(numerator, total),
		})
	}
	return prices
}

func cloneBigInt(value *big.Int) *big.Int {
	if value == nil {
		return big.NewInt(0)
	}
	return new(big.Int).Set(value)
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
