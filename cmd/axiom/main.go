package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	flagAPIURL                  string
	flagConsoleAPIURL           string
	flagRPCURL                  string
	flagXRPLURL                 string
	flagJSON                    bool
	flagProfile                 string
	getEVMBalance               = evm.GetBalance
	getXRPLBalance              = axrpl.GetBalance
	loadMarketState             = evm.LoadMarketState
	quoteBuy                    = evm.QuoteBuy
	buyPosition                 = evm.BuyPosition
	claimEpochRewards           = evm.ClaimRewards
	claimSingleMarket           = evm.ClaimMarket
	batchClaimMarkets           = evm.BatchClaim
	waitForTxReceipt            = waitForReceipt
	submitBridgePayment         = axrpl.SubmitBridgePayment
	getERC20Balance             = evm.GetERC20Balance
	getERC20Allowance           = evm.GetERC20Allowance
	approveERC20                = evm.ApproveERC20
	getERC1155Balance           = evm.GetERC1155Balance
	isERC1155ApprovedForAll     = evm.IsERC1155ApprovedForAll
	setERC1155ApprovalForAll    = evm.SetERC1155ApprovalForAll
	loadCTFMarketMetadata       = evm.LoadCTFMarketMetadata
	redeemCTFMarket             = evm.RedeemCTFMarket
	splitPosition               = evm.SplitPosition
	mergePositions              = evm.MergePositions
	createAxiomCTFMarket        = evm.CreateAxiomCTFMarket
	launchAxiomCTFLogicalMarket = evm.LaunchAxiomCTFLogicalMarket
	resolveCTFMarket            = evm.ResolveCTFMarket
)

type cliContext struct {
	Config      *app.Config
	API         *api.Client
	ConsoleAPI  *api.Client
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

type mmMarketSelection struct {
	MarketID       string `json:"marketId"`
	MarketTitle    string `json:"marketTitle"`
	InstanceDate   string `json:"instanceDate,omitempty"`
	ContractAddr   string `json:"contractAddress,omitempty"`
	Category       string `json:"category,omitempty"`
	Status         string `json:"status,omitempty"`
	EndsAt         string `json:"endsAt,omitempty"`
	MarketType     string `json:"marketType,omitempty"`
	Implementation string `json:"marketImplementation,omitempty"`
}

type mmInteractiveChoice struct {
	Label string
	Value string
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
		Long:          "Axiom CLI manages XRPL EVM wallets, funding flows, market discovery, predictions, hosted CLOB trading, market-making workflows, claims, and profile analytics.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		if rewritten := rewriteAmountFlagParseError(normalizeCLIArgs(os.Args[1:]), err); rewritten != nil {
			return rewritten
		}
		return err
	})

	rootCmd.PersistentFlags().StringVar(&flagAPIURL, "api-url", "", "Override the Axiom CLI app API base URL (for example https://axiomprotocol.io/api/cli)")
	rootCmd.PersistentFlags().StringVar(&flagConsoleAPIURL, "console-api-url", "", "Override the Axiom console API base URL used for canonical addresses and metadata uploads (for example https://console.axiomprotocol.io/api/cli)")
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
	rootCmd.AddCommand(newMMCommand())

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
			consoleAPIURL, _ := cmd.Flags().GetString("console-api-url")
			rpcURL, _ := cmd.Flags().GetString("rpc-url")
			xrplURL, _ := cmd.Flags().GetString("xrpl-rpc-url")
			if apiURL != "" {
				cfg.APIBaseURL = apiURL
			}
			if consoleAPIURL != "" {
				cfg.ConsoleAPIBaseURL = consoleAPIURL
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
	setCmd.Flags().String("console-api-url", "", "Set the console API base URL used for canonical addresses and metadata uploads")
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
				response, err = ctx.API.ListAllMarkets(cmd.Context(), status, search, "", normalizedImpl, false, 0)
			} else {
				response, err = ctx.API.ListMarkets(cmd.Context(), status, search, "", normalizedImpl, false, limit, offset)
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
			if mustBoolFlag(cmd, "visible") && mustBoolFlag(cmd, "hidden") {
				return errors.New("--visible and --hidden are mutually exclusive")
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
	cmd.PersistentFlags().String("factory-address", "", "Override the MarketFactory address used for binary CTF market deployment; otherwise load the canonical xrpl-mainnet address from the console API")
	cmd.PersistentFlags().String("exchange-address", evm.DefaultClobExchangeAddress, "Override the on-chain AxiomCTFExchange address used for approvals and settlement prep")
	cmd.PersistentFlags().String("clob-domain-contract", firstNonEmpty(os.Getenv("AXIOM_CLOB_DOMAIN_CONTRACT"), evm.DefaultClobDomainContract), "Override the hosted CLOB EIP-712 verifying contract used for order/CreateBook signing")
	cmd.PersistentFlags().String("clob-chain-id", firstNonEmpty(os.Getenv("AXIOM_CLOB_CHAIN_ID"), strconv.FormatInt(evm.DefaultClobChainID, 10)), "Override the hosted CLOB EIP-712 chain ID used for order/CreateBook signing")
	cmd.PersistentFlags().String("outcome-token-address", evm.DefaultClobConditionalTokens, "Override the on-chain AxiomConditionalTokens address used for balances and approvals")

	marketCmd := &cobra.Command{Use: "market", Short: "Deploy and manage on-chain binary AxiomCTFMarket contracts"}
	marketCreateCmd := &cobra.Command{
		Use:   "create",
		Short: "Deploy a binary AxiomCTFMarket via the canonical MarketFactory contract",
		Long: strings.Join([]string{
			"Deploy a single binary AxiomCTFMarket via MarketFactory.createMarket(...).",
			"",
			"This is the low-level on-chain primitive for one binary YES/NO market contract.",
			"It does not create or persist a full logical CLOB market in the Axiom backend.",
		}, "\n"),
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			wallet, privateKeyHex, err := requireEVMWalletWithKey(ctx)
			if err != nil {
				return err
			}

			questionIDValue := strings.TrimSpace(mustStringFlag(cmd, "question-id"))
			if len(questionIDValue) != 66 || !strings.HasPrefix(questionIDValue, "0x") {
				return errors.New("--question-id must be a 32-byte 0x-prefixed value")
			}
			questionID := common.HexToHash(questionIDValue)
			if questionID == (common.Hash{}) {
				return errors.New("--question-id must not be zero")
			}

			tradingOpen, err := resolveUnixTimestampFlag(cmd, "trading-open")
			if err != nil {
				return err
			}
			tradingClose, err := resolveUnixTimestampFlag(cmd, "trading-close")
			if err != nil {
				return err
			}
			now := uint64(time.Now().Unix())
			if tradingClose <= now {
				return errors.New("--trading-close must be in the future")
			}
			if tradingClose <= tradingOpen {
				return errors.New("--trading-close must be greater than --trading-open")
			}

			metadataURI, metadata, err := resolveClobMarketMetadata(cmd.Context(), ctx, wallet, cmd, time.Unix(int64(tradingClose), 0).UTC())
			if err != nil {
				return err
			}

			factoryAddress, err := resolveClobMarketFactoryAddress(cmd.Context(), ctx, mustStringFlag(cmd, "factory-address"), cmd.Flags().Changed("factory-address"))
			if err != nil {
				return err
			}
			collateralToken := resolveHexAddressOrDefault(mustStringFlag(cmd, "collateral-token"), evm.DefaultClobCollateralToken)
			conditionalTokens := resolveHexAddressOrDefault(mustStringFlag(cmd, "outcome-token-address"), evm.DefaultClobConditionalTokens)

			result, err := createAxiomCTFMarket(cmd.Context(), ctx.Config.EVMRPCURL, big.NewInt(xrplEVMChainID), privateKeyHex, evm.CreateAxiomCTFMarketParams{
				FactoryAddress:    factoryAddress,
				Creator:           wallet.Address(),
				CollateralToken:   collateralToken,
				ConditionalTokens: conditionalTokens,
				MetadataURI:       metadataURI,
				TradingOpen:       tradingOpen,
				TradingClose:      tradingClose,
				QuestionID:        questionID,
			})
			if err != nil {
				return err
			}

			return printOutput(ctx.JSON, map[string]any{
				"implementation":    "AxiomCTFMarket",
				"factoryAddress":    factoryAddress.Hex(),
				"configAddress":     result.ConfigAddress.Hex(),
				"creator":           wallet.Address().Hex(),
				"marketAddress":     result.MarketAddress.Hex(),
				"collateralToken":   collateralToken.Hex(),
				"conditionalTokens": conditionalTokens.Hex(),
				"metadataUri":       metadataURI,
				"metadata":          metadata,
				"questionId":        questionID.Hex(),
				"tradingOpen":       tradingOpen,
				"tradingClose":      tradingClose,
				"txHash":            result.TxHash.Hex(),
			})
		},
	}
	marketCreateCmd.Flags().String("metadata-uri", "", "Metadata URI stored on the deployed AxiomCTFMarket")
	marketCreateCmd.Flags().String("name", "", "Market title used to build and upload metadata when --metadata-uri is omitted")
	marketCreateCmd.Flags().String("headline", "", "Short market headline used in uploaded metadata")
	marketCreateCmd.Flags().String("description", "", "Full market description used in uploaded metadata")
	marketCreateCmd.Flags().String("category", "", "Market category used in uploaded metadata")
	marketCreateCmd.Flags().StringSlice("tag", nil, "Metadata tag to include; repeatable")
	marketCreateCmd.Flags().StringSlice("evidence-source", nil, "Evidence source URL to include; repeatable")
	marketCreateCmd.Flags().String("image", "", "Optional image URI for uploaded metadata")
	marketCreateCmd.Flags().String("resolution-criteria", "", "Resolution criteria used in uploaded metadata")
	marketCreateCmd.Flags().String("yes-label", "Yes", "YES outcome label used in uploaded metadata")
	marketCreateCmd.Flags().String("yes-description", "", "YES outcome description used in uploaded metadata")
	marketCreateCmd.Flags().String("no-label", "No", "NO outcome label used in uploaded metadata")
	marketCreateCmd.Flags().String("no-description", "", "NO outcome description used in uploaded metadata")
	marketCreateCmd.Flags().String("question-id", "", "CTF question ID as a 32-byte 0x-prefixed value")
	marketCreateCmd.Flags().Uint64("trading-open", 0, "Trading open timestamp in Unix seconds")
	marketCreateCmd.Flags().Uint64("trading-close", 0, "Trading close timestamp in Unix seconds")
	marketCreateCmd.Flags().String("collateral-token", evm.DefaultClobCollateralToken, "Collateral ERC-20 token address for the binary market")
	marketCmd.AddCommand(marketCreateCmd)

	marketResolveCmd := &cobra.Command{
		Use:   "resolve",
		Short: "Resolve a deployed binary AxiomCTFMarket with an explicit payout vector",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			wallet, privateKeyHex, err := requireEVMWalletWithKey(ctx)
			if err != nil {
				return err
			}

			marketRaw := strings.TrimSpace(mustStringFlag(cmd, "market"))
			if !common.IsHexAddress(marketRaw) || isZeroAddress(marketRaw) {
				return errors.New("--market must be a valid non-zero 0x-prefixed address")
			}
			marketAddress := common.HexToAddress(marketRaw)

			payouts, err := parsePayoutNumerators(mustStringFlag(cmd, "payouts"))
			if err != nil {
				return err
			}

			txHash, err := resolveCTFMarket(cmd.Context(), ctx.Config.EVMRPCURL, big.NewInt(xrplEVMChainID), privateKeyHex, marketAddress, payouts)
			if err != nil {
				return err
			}

			result := map[string]any{
				"implementation": "AxiomCTFMarket",
				"marketAddress":  marketAddress.Hex(),
				"operator":       wallet.Address().Hex(),
				"payouts":        bigIntSliceToStrings(payouts),
				"txHash":         txHash.Hex(),
			}
			if mustBoolFlag(cmd, "wait") {
				receipt, err := waitForTxReceipt(cmd.Context(), ctx.Config.EVMRPCURL, txHash)
				if err != nil {
					return err
				}
				result["receiptStatus"] = receipt.Status
				if receipt.Status != 1 {
					return fmt.Errorf("resolve transaction reverted (tx %s)", txHash.Hex())
				}
			}
			return printOutput(ctx.JSON, result)
		},
	}
	marketResolveCmd.Flags().String("market", "", "Deployed binary AxiomCTFMarket address to resolve")
	marketResolveCmd.Flags().String("payouts", "", "Comma-separated payout numerators, for example 1,0 or 0,1")
	marketResolveCmd.Flags().Bool("wait", false, "Wait for the resolve transaction receipt")
	marketCmd.AddCommand(marketResolveCmd)
	cmd.AddCommand(marketCmd)

	logicalCmd := &cobra.Command{Use: "logical", Short: "Register or atomically create logical hosted CLOB markets"}
	logicalRegisterCmd := &cobra.Command{
		Use:   "register",
		Short: "Register existing binary AxiomCTFMarket contracts as one logical hosted CLOB market",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			wallet, _, err := requireEVMWalletWithKey(ctx)
			if err != nil {
				return err
			}
			plan, err := buildLogicalMarketPlan(cmd)
			if err != nil {
				return err
			}
			addresses, err := collectLogicalMarketAddresses(cmd)
			if err != nil {
				return err
			}
			if err := validateLogicalRegisterAddresses(plan, addresses); err != nil {
				return err
			}
			signingDomain, err := resolveClobSigningDomain(cmd)
			if err != nil {
				return err
			}
			bookSignatures, err := buildLogicalBookSignatures(wallet, signingDomain, plan.MarketID, addresses)
			if err != nil {
				return err
			}
			request, err := buildLogicalRegisterRequest(
				wallet,
				strings.TrimSpace(mustStringFlag(cmd, "network")),
				xrplEVMChainID,
				ctx.Config.EVMRPCURL,
				plan,
				addresses,
				mustBoolFlag(cmd, "visible") && !mustBoolFlag(cmd, "hidden"),
				mustBoolFlag(cmd, "allow-unindexed"),
				bookSignatures,
			)
			if err != nil {
				return err
			}
			if mustBoolFlag(cmd, "dry-run") {
				return printOutput(ctx.JSON, map[string]any{
					"dryRun":           true,
					"marketId":         request.MarketID,
					"network":          request.Network,
					"addresses":        request.Addresses,
					"metadata":         request.Metadata,
					"bookSignatures":   request.BookSignatures,
					"message":          request.Message,
					"signaturePresent": request.Signature != "",
				})
			}
			response, err := ctx.ConsoleAPI.RegisterClobMarket(cmd.Context(), request)
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, map[string]any{
				"mode":                "register",
				"marketId":            response.MarketID,
				"signerAddress":       response.SignerAddress,
				"registeredContracts": response.RegisteredContracts,
				"booksCreated":        response.BooksCreated,
				"booksTotal":          response.BooksTotal,
				"warnings":            response.Warnings,
			})
		},
	}
	logicalRegisterCmd.Flags().String("market-id", "", "Logical market ID to persist; autogenerated when omitted")
	logicalRegisterCmd.Flags().String("network", "xrpl-mainnet", "Network identifier used for canonical contracts and registration")
	logicalRegisterCmd.Flags().String("market-type", "yes_no", "Logical market type: yes_no or multiple_choice")
	logicalRegisterCmd.Flags().String("name", "", "Logical market title")
	logicalRegisterCmd.Flags().String("headline", "", "Optional logical market headline")
	logicalRegisterCmd.Flags().String("description", "", "Logical market description")
	logicalRegisterCmd.Flags().String("category", "", "Logical market category")
	logicalRegisterCmd.Flags().StringSlice("tag", nil, "Metadata tag to include; repeatable")
	logicalRegisterCmd.Flags().StringSlice("evidence-source", nil, "Evidence source URL to include in uploaded binary metadata; repeatable")
	logicalRegisterCmd.Flags().String("image", "", "Optional logical market PFP image URL; also included in uploaded binary metadata")
	logicalRegisterCmd.Flags().String("resolution-criteria", "", "Logical market resolution criteria")
	logicalRegisterCmd.Flags().String("starts-at", "", "Logical market start time in RFC3339")
	logicalRegisterCmd.Flags().String("ends-at", "", "Logical market end time in RFC3339")
	logicalRegisterCmd.Flags().String("resolve-by", "", "Optional resolve-by time in RFC3339; defaults to ends-at")
	logicalRegisterCmd.Flags().String("outcomes-json", "", "JSON array of logical outcomes with optional key, label, description, metadataUri, and questionId fields")
	logicalRegisterCmd.Flags().String("yes-label", "Yes", "Displayed YES label for yes_no logical markets")
	logicalRegisterCmd.Flags().String("yes-description", "", "Displayed YES description for yes_no logical markets")
	logicalRegisterCmd.Flags().String("no-label", "No", "Displayed NO label for yes_no logical markets")
	logicalRegisterCmd.Flags().String("no-description", "", "Displayed NO description for yes_no logical markets")
	logicalRegisterCmd.Flags().String("yes-question-id", "", "Optional 32-byte 0x question ID override for the YES binding")
	logicalRegisterCmd.Flags().String("no-question-id", "", "Optional 32-byte 0x question ID override for the NO binding")
	logicalRegisterCmd.Flags().String("yes-metadata-uri", "", "Optional metadata URI override for the YES binding")
	logicalRegisterCmd.Flags().String("no-metadata-uri", "", "Optional metadata URI override for the NO binding")
	logicalRegisterCmd.Flags().StringSlice("outcome-label", nil, "Displayed outcome label for multiple_choice markets; repeatable")
	logicalRegisterCmd.Flags().StringSlice("address", nil, "Binary AxiomCTFMarket address to bind; repeatable")
	logicalRegisterCmd.Flags().Bool("visible", false, "Persist the logical market as visible to the webapp feed")
	logicalRegisterCmd.Flags().Bool("hidden", false, "Persist the logical market as hidden from the webapp feed")
	logicalRegisterCmd.Flags().Bool("allow-unindexed", false, "Allow registration before the indexer discovers all binary contracts")
	logicalRegisterCmd.Flags().Bool("dry-run", false, "Build the registration payload locally without submitting it")
	logicalCmd.AddCommand(logicalRegisterCmd)

	logicalCreateCmd := &cobra.Command{
		Use:   "create",
		Short: "Upload per-outcome metadata, launch grouped binary markets, and register them as one logical hosted CLOB market",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			if mustBoolFlag(cmd, "visible") && mustBoolFlag(cmd, "hidden") {
				return errors.New("--visible and --hidden are mutually exclusive")
			}
			wallet, privateKeyHex, err := requireEVMWalletWithKey(ctx)
			if err != nil {
				return err
			}
			plan, err := buildLogicalMarketPlan(cmd)
			if err != nil {
				return err
			}
			network := strings.TrimSpace(mustStringFlag(cmd, "network"))
			if network == "" {
				network = "xrpl-mainnet"
			}
			if err := uploadLogicalLaunchMetadata(cmd.Context(), ctx, wallet, network, mustBoolFlag(cmd, "dry-run"), plan); err != nil {
				return err
			}
			addresses, err := ctx.ConsoleAPI.GetMarketContractAddresses(cmd.Context(), network)
			if err != nil {
				return fmt.Errorf("load canonical launcher addresses: %w", err)
			}
			if addresses == nil || !common.IsHexAddress(strings.TrimSpace(addresses.CTFLauncher)) || isZeroAddress(addresses.CTFLauncher) {
				return errors.New("canonical AxiomCTFMarketLauncher address is unavailable; pass a console API with launcher support")
			}
			conditionalTokens := strings.TrimSpace(addresses.ConditionalTokens)
			if !common.IsHexAddress(conditionalTokens) || isZeroAddress(conditionalTokens) {
				conditionalTokens = evm.DefaultClobConditionalTokens
			}
			launchParams := evm.LaunchAxiomCTFLogicalMarketParams{
				LauncherAddress:   common.HexToAddress(addresses.CTFLauncher),
				Creator:           wallet.Address(),
				CollateralToken:   resolveHexAddressOrDefault(mustStringFlag(cmd, "collateral-token"), evm.DefaultClobCollateralToken),
				ConditionalTokens: common.HexToAddress(conditionalTokens),
				TradingOpen:       uint64(plan.StartsAt.Unix()),
				TradingClose:      uint64(plan.EndsAt.Unix()),
				LogicalMarketID:   plan.LogicalMarketIDHash,
				Outcomes:          make([]evm.LaunchAxiomCTFMarketOutcome, 0, len(plan.LaunchOutcomes)),
			}
			for _, outcome := range plan.LaunchOutcomes {
				launchParams.Outcomes = append(launchParams.Outcomes, evm.LaunchAxiomCTFMarketOutcome{
					Label:       outcome.Label,
					MetadataURI: outcome.MetadataURI,
					QuestionID:  outcome.QuestionID,
				})
			}
			if mustBoolFlag(cmd, "dry-run") {
				return printOutput(ctx.JSON, map[string]any{
					"dryRun":          true,
					"mode":            "create",
					"network":         network,
					"marketId":        plan.MarketID,
					"logicalMarketId": plan.LogicalMarketIDHash.Hex(),
					"launchParams": map[string]any{
						"launcherAddress":   launchParams.LauncherAddress.Hex(),
						"creator":           launchParams.Creator.Hex(),
						"collateralToken":   launchParams.CollateralToken.Hex(),
						"conditionalTokens": launchParams.ConditionalTokens.Hex(),
						"tradingOpen":       launchParams.TradingOpen,
						"tradingClose":      launchParams.TradingClose,
						"outcomes":          plan.LaunchOutcomes,
					},
				})
			}
			launchResult, err := launchAxiomCTFLogicalMarket(cmd.Context(), ctx.Config.EVMRPCURL, big.NewInt(xrplEVMChainID), privateKeyHex, launchParams)
			if err != nil {
				return err
			}
			launchedAddresses := make([]common.Address, 0, len(launchResult.LaunchedMarkets))
			for _, market := range launchResult.LaunchedMarkets {
				launchedAddresses = append(launchedAddresses, market.MarketAddress)
			}
			signingDomain, err := resolveClobSigningDomain(cmd)
			if err != nil {
				return err
			}
			bookSignatures, err := buildLogicalBookSignatures(wallet, signingDomain, plan.MarketID, launchedAddresses)
			if err != nil {
				return err
			}
			registerRequest, err := buildLogicalRegisterRequest(
				wallet,
				network,
				xrplEVMChainID,
				ctx.Config.EVMRPCURL,
				plan,
				launchedAddresses,
				mustBoolFlag(cmd, "visible") && !mustBoolFlag(cmd, "hidden"),
				true,
				bookSignatures,
			)
			if err != nil {
				return err
			}
			registerRequest.AllowUnindexed = true
			response, err := ctx.ConsoleAPI.RegisterClobMarket(cmd.Context(), registerRequest)
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, map[string]any{
				"mode":              "create",
				"marketId":          response.MarketID,
				"logicalMarketId":   plan.LogicalMarketIDHash.Hex(),
				"launcherAddress":   launchParams.LauncherAddress.Hex(),
				"launchTxHash":      launchResult.TxHash.Hex(),
				"blockNumber":       launchResult.BlockNumber,
				"launchedContracts": response.RegisteredContracts,
				"booksCreated":      response.BooksCreated,
				"booksTotal":        response.BooksTotal,
				"warnings":          response.Warnings,
			})
		},
	}
	logicalCreateCmd.Flags().String("market-id", "", "Logical market ID to persist; autogenerated when omitted")
	logicalCreateCmd.Flags().String("network", "xrpl-mainnet", "Network identifier used for canonical contracts and registration")
	logicalCreateCmd.Flags().String("market-type", "yes_no", "Logical market type: yes_no or multiple_choice")
	logicalCreateCmd.Flags().String("name", "", "Logical market title")
	logicalCreateCmd.Flags().String("headline", "", "Optional logical market headline")
	logicalCreateCmd.Flags().String("description", "", "Logical market description")
	logicalCreateCmd.Flags().String("category", "", "Logical market category")
	logicalCreateCmd.Flags().StringSlice("tag", nil, "Metadata tag to include; repeatable")
	logicalCreateCmd.Flags().StringSlice("evidence-source", nil, "Evidence source URL to include in uploaded binary metadata; repeatable")
	logicalCreateCmd.Flags().String("image", "", "Optional logical market PFP image URL; also included in uploaded binary metadata")
	logicalCreateCmd.Flags().String("resolution-criteria", "", "Logical market resolution criteria")
	logicalCreateCmd.Flags().String("starts-at", "", "Logical market start time in RFC3339")
	logicalCreateCmd.Flags().String("ends-at", "", "Logical market end time in RFC3339")
	logicalCreateCmd.Flags().String("resolve-by", "", "Optional resolve-by time in RFC3339; defaults to ends-at")
	logicalCreateCmd.Flags().String("outcomes-json", "", "JSON array of logical outcomes with optional key, label, description, metadataUri, and questionId fields")
	logicalCreateCmd.Flags().String("yes-label", "Yes", "Displayed YES label for yes_no logical markets")
	logicalCreateCmd.Flags().String("yes-description", "", "Displayed YES description for yes_no logical markets")
	logicalCreateCmd.Flags().String("no-label", "No", "Displayed NO label for yes_no logical markets")
	logicalCreateCmd.Flags().String("no-description", "", "Displayed NO description for yes_no logical markets")
	logicalCreateCmd.Flags().String("yes-question-id", "", "Optional 32-byte 0x question ID override for the YES binding")
	logicalCreateCmd.Flags().String("no-question-id", "", "Optional 32-byte 0x question ID override for the NO binding")
	logicalCreateCmd.Flags().String("yes-metadata-uri", "", "Optional metadata URI override for the YES binding")
	logicalCreateCmd.Flags().String("no-metadata-uri", "", "Optional metadata URI override for the NO binding")
	logicalCreateCmd.Flags().StringSlice("outcome-label", nil, "Displayed outcome label for multiple_choice markets; repeatable")
	logicalCreateCmd.Flags().String("collateral-token", evm.DefaultClobCollateralToken, "Collateral ERC-20 token address for the launched binary markets")
	logicalCreateCmd.Flags().Bool("visible", false, "Persist the logical market as visible to the webapp feed")
	logicalCreateCmd.Flags().Bool("hidden", false, "Persist the logical market as hidden from the webapp feed")
	logicalCreateCmd.Flags().Bool("allow-unindexed", true, "Ignored for now; logical create always registers with allowUnindexed=true to tolerate indexer lag")
	logicalCreateCmd.Flags().Bool("dry-run", false, "Build metadata and launch/register payloads locally without submitting them")
	logicalCmd.AddCommand(logicalCreateCmd)

	logicalResolveCmd := &cobra.Command{
		Use:   "resolve",
		Short: "Resolve a logical hosted CLOB market, close its offchain books, and mark the logical market resolved",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			wallet, privateKeyHex, err := requireEVMWalletWithKey(ctx)
			if err != nil {
				return err
			}

			marketRef := strings.TrimSpace(mustStringFlag(cmd, "market"))
			if marketRef == "" {
				return errors.New("--market is required")
			}
			market, err := loadMarketWithClobFallback(cmd.Context(), ctx, marketRef, mustStringFlag(cmd, "instance-date"))
			if err != nil {
				return err
			}
			if !isClobMarketImplementation(market.MarketImplementation) {
				return errors.New("logical resolve requires an AxiomCTFMarket logical market")
			}
			if market.IsResolved {
				return errors.New("logical market is already resolved")
			}

			winningOutcomeIndex, err := resolveOutcomeIndex(market, mustStringFlag(cmd, "outcome"), mustStringFlag(cmd, "label"))
			if err != nil {
				return err
			}

			resolutionPlan, err := buildLogicalResolutionPlan(market, winningOutcomeIndex)
			if err != nil {
				return err
			}

			network := strings.TrimSpace(mustStringFlag(cmd, "network"))
			if network == "" {
				network = "xrpl-mainnet"
			}

			result := map[string]any{
				"mode":                "logical-resolve",
				"marketId":            market.ID,
				"winningOutcomeIndex": winningOutcomeIndex,
				"resolutions":         make([]map[string]any, 0, len(resolutionPlan)),
			}
			reason := firstNonEmpty(strings.TrimSpace(mustStringFlag(cmd, "reason")), "logical-market-resolved")

			if mustBoolFlag(cmd, "dry-run") {
				preview := make([]map[string]any, 0, len(resolutionPlan))
				for _, item := range resolutionPlan {
					preview = append(preview, map[string]any{
						"outcomeIndex":    item.Binding.OutcomeIndex,
						"outcomeLabel":    item.Binding.Label,
						"contractAddress": item.Binding.ContractAddress,
						"won":             item.Won,
						"payouts":         bigIntSliceToStrings(item.Payouts),
					})
				}
				result["dryRun"] = true
				result["resolutions"] = preview
				result["bookClosures"] = len(resolutionPlan) * 2
				return printOutput(ctx.JSON, result)
			}

			resolutionTxHashes := make([]common.Hash, 0, len(resolutionPlan))
			resolutions := make([]map[string]any, 0, len(resolutionPlan))
			for _, item := range resolutionPlan {
				marketAddress := strings.TrimSpace(item.Binding.ContractAddress)
				if !common.IsHexAddress(marketAddress) || isZeroAddress(marketAddress) {
					return fmt.Errorf("binding %q has no usable contract address", item.Binding.Label)
				}
				txHash, err := resolveCTFMarket(cmd.Context(), ctx.Config.EVMRPCURL, big.NewInt(xrplEVMChainID), privateKeyHex, common.HexToAddress(marketAddress), item.Payouts)
				if err != nil {
					return err
				}
				entry := map[string]any{
					"outcomeIndex":    item.Binding.OutcomeIndex,
					"outcomeLabel":    item.Binding.Label,
					"contractAddress": common.HexToAddress(marketAddress).Hex(),
					"won":             item.Won,
					"payouts":         bigIntSliceToStrings(item.Payouts),
					"txHash":          txHash.Hex(),
				}
				if mustBoolFlag(cmd, "wait") {
					receipt, err := waitForTxReceipt(cmd.Context(), ctx.Config.EVMRPCURL, txHash)
					if err != nil {
						return err
					}
					entry["receiptStatus"] = receipt.Status
					if receipt.Status != 1 {
						return fmt.Errorf("resolve transaction reverted (tx %s)", txHash.Hex())
					}
				}
				resolutionTxHashes = append(resolutionTxHashes, txHash)
				resolutions = append(resolutions, entry)
			}
			result["resolutions"] = resolutions

			resolveRequest, err := buildLogicalResolveRequest(wallet, network, ctx.Config.EVMRPCURL, market.ID, winningOutcomeIndex, resolutionTxHashes, reason)
			if err != nil {
				return err
			}
			resolveResponse, err := ctx.ConsoleAPI.ResolveClobMarket(cmd.Context(), resolveRequest)
			if err != nil {
				return err
			}
			result["logicalResolution"] = resolveResponse
			result["bookClosures"] = resolveResponse.BooksClosed
			warnings := append([]string(nil), resolveResponse.Warnings...)
			if len(warnings) > 0 {
				result["warnings"] = warnings
			}
			return printOutput(ctx.JSON, result)
		},
	}
	logicalResolveCmd.Flags().String("market", "", "Logical market ID or primary contract address to resolve")
	logicalResolveCmd.Flags().String("outcome", "", "Winning displayed outcome index")
	logicalResolveCmd.Flags().String("label", "", "Winning displayed outcome label")
	logicalResolveCmd.Flags().String("network", "xrpl-mainnet", "Network identifier used for logical market resolution")
	logicalResolveCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	logicalResolveCmd.Flags().String("reason", "logical-market-resolved", "Reason to use when closing hosted offchain books")
	logicalResolveCmd.Flags().Bool("wait", false, "Wait for each on-chain resolve transaction receipt")
	logicalResolveCmd.Flags().Bool("dry-run", false, "Build the logical resolve plan locally without submitting transactions")
	logicalCmd.AddCommand(logicalResolveCmd)

	logicalUpdateCmd := &cobra.Command{
		Use:   "update",
		Short: "Update creator-owned logical CLOB market metadata fields",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			wallet, _, err := requireEVMWalletWithKey(ctx)
			if err != nil {
				return err
			}

			marketRef := strings.TrimSpace(mustStringFlag(cmd, "market"))
			if marketRef == "" {
				return errors.New("--market is required")
			}
			market, err := loadMarketWithClobFallback(cmd.Context(), ctx, marketRef, mustStringFlag(cmd, "instance-date"))
			if err != nil {
				return err
			}
			if !isClobMarketImplementation(market.MarketImplementation) {
				return errors.New("logical update requires an AxiomCTFMarket logical market")
			}

			trimmedOrNil := func(flagName string) *string {
				if !cmd.Flags().Changed(flagName) {
					return nil
				}
				value := strings.TrimSpace(mustStringFlag(cmd, flagName))
				return &value
			}
			collectTags := func() []string {
				values, _ := cmd.Flags().GetStringSlice("tag")
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

			input := logicalUpdateInput{
				Name:        trimmedOrNil("name"),
				Headline:    trimmedOrNil("headline"),
				Description: trimmedOrNil("description"),
				Category:    trimmedOrNil("category"),
				ImageURL:    trimmedOrNil("image"),
			}
			if cmd.Flags().Changed("tag") {
				input.Tags = collectTags()
			}
			if input.Name == nil && input.Headline == nil && input.Description == nil && input.Category == nil && input.ImageURL == nil && !cmd.Flags().Changed("tag") {
				return errors.New("at least one update field is required")
			}

			network := strings.TrimSpace(mustStringFlag(cmd, "network"))
			if network == "" {
				network = "xrpl-mainnet"
			}
			request, err := buildLogicalUpdateRequest(wallet, network, market.ID, input)
			if err != nil {
				return err
			}
			if mustBoolFlag(cmd, "dry-run") {
				return printOutput(ctx.JSON, map[string]any{
					"dryRun":           true,
					"marketId":         request.MarketID,
					"network":          request.Network,
					"walletAddress":    request.WalletAddress,
					"name":             request.Name,
					"headline":         request.Headline,
					"description":      request.Description,
					"category":         request.Category,
					"imageUrl":         request.ImageURL,
					"tags":             request.Tags,
					"message":          request.Message,
					"signaturePresent": request.Signature != "",
				})
			}
			response, err := ctx.ConsoleAPI.UpdateClobMarket(cmd.Context(), request)
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, map[string]any{
				"mode":          "logical-update",
				"marketId":      response.MarketID,
				"signerAddress": response.SignerAddress,
				"updatedFields": response.UpdatedFields,
			})
		},
	}
	logicalUpdateCmd.Flags().String("market", "", "Logical market ID or primary contract address to update")
	logicalUpdateCmd.Flags().String("network", "xrpl-mainnet", "Network identifier used for logical market updates")
	logicalUpdateCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	logicalUpdateCmd.Flags().String("name", "", "Updated logical market title")
	logicalUpdateCmd.Flags().String("headline", "", "Updated logical market headline")
	logicalUpdateCmd.Flags().String("description", "", "Updated logical market description")
	logicalUpdateCmd.Flags().String("category", "", "Updated logical market category")
	logicalUpdateCmd.Flags().String("image", "", "Updated logical market PFP image URL")
	logicalUpdateCmd.Flags().StringSlice("tag", nil, "Updated logical metadata tag list; repeatable")
	logicalUpdateCmd.Flags().Bool("dry-run", false, "Build the logical update payload locally without submitting it")
	logicalCmd.AddCommand(logicalUpdateCmd)
	cmd.AddCommand(logicalCmd)

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
			marketDetails, selection, err := resolveClobReadSelection(cmd.Context(), ctx, cmd, market)
			if err != nil {
				return err
			}
			projectionURL := strings.TrimSpace(mustStringFlag(cmd, "projection-url"))
			book, err := ctx.API.GetClobBook(cmd.Context(), projectionURL, marketDetails.ID, selection.Binding.OutcomeIndex, selection.DisplayedSide)
			if err != nil {
				return err
			}
			depth, err := ctx.API.GetClobDepth(cmd.Context(), projectionURL, marketDetails.ID, selection.Binding.OutcomeIndex, selection.DisplayedSide)
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, map[string]any{
				"marketId":  marketDetails.ID,
				"outcome":   selection.LogicalOutcome.Index,
				"label":     selection.LogicalOutcome.Label,
				"tokenSide": selection.DisplayedSide,
				"book":      book,
				"depth":     depth,
			})
		},
	}
	depthCmd.Flags().String("market", "", "Logical market ID for the CLOB proposition")
	depthCmd.Flags().Int("outcome", 0, "Displayed outcome index within the logical market")
	depthCmd.Flags().String("label", "", "Displayed outcome label within the logical market")
	depthCmd.Flags().String("token-side", "yes", "Hosted token-side book to inspect: yes or no; inferred from the displayed outcome for single-binding yes/no markets when omitted")
	depthCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
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
			approvalStatus, err := buildClobApprovalStatus(status)
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, map[string]any{
				"walletStatus":   status,
				"approvalStatus": approvalStatus,
			})
		},
	}
	statusCmd.Flags().String("wallet", "", "Wallet address to inspect; defaults to the active profile EVM address")
	statusCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	walletCmd.AddCommand(statusCmd)

	approveCmd := &cobra.Command{
		Use:   "approve [market-id-or-address]",
		Short: "Approve collateral and outcome-token spending for the hosted CLOB exchange",
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

			skipCollateral := mustBoolFlag(cmd, "skip-collateral")
			skipOutcome := mustBoolFlag(cmd, "skip-outcome")
			if skipCollateral && skipOutcome {
				return errors.New("nothing to approve: remove --skip-collateral or --skip-outcome")
			}

			var market *api.MarketDetails
			if len(args) > 0 {
				market, err = loadMarketWithClobFallback(cmd.Context(), ctx, args[0], mustStringFlag(cmd, "instance-date"))
				if err != nil {
					return err
				}
				if !isClobMarketImplementation(market.MarketImplementation) {
					return errors.New("clob wallet approve requires an AxiomCTFMarket logical market when a market is provided")
				}
			} else if !cmd.Flags().Changed("collateral-token-address") && !skipCollateral {
				return errors.New("clob wallet approve without a market requires --collateral-token-address unless using --skip-collateral")
			}

			exchangeAddress := resolveHexAddressOrDefault(mustStringFlag(cmd, "exchange-address"), evm.DefaultClobExchangeAddress)
			outcomeToken := resolveHexAddressOrDefault(mustStringFlag(cmd, "outcome-token-address"), evm.DefaultClobConditionalTokens)
			collateralToken := resolveHexAddressOrDefault(mustStringFlag(cmd, "collateral-token-address"), evm.DefaultClobCollateralToken)
			if market != nil {
				collateralToken = resolveClobCollateralToken(market)
			}
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
					receipt, waitErr := waitForTxReceipt(cmd.Context(), ctx.Config.EVMRPCURL, txHash)
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
					receipt, waitErr := waitForTxReceipt(cmd.Context(), ctx.Config.EVMRPCURL, txHash)
					if waitErr != nil {
						return waitErr
					}
					entry["receiptStatus"] = receipt.Status
				}
				transactions = append(transactions, entry)
			}

			output := map[string]any{
				"walletAddress": wallet.Address().Hex(),
				"transactions":  transactions,
			}
			if market != nil {
				output["market"] = market.Title
				output["marketId"] = market.ID
			}
			if !skipCollateral {
				output["collateralToken"] = collateralToken.Hex()
			}
			output["exchangeAddress"] = exchangeAddress.Hex()
			output["outcomeToken"] = outcomeToken.Hex()

			return printOutput(ctx.JSON, output)
		},
	}
	approveCmd.Flags().String("collateral-amount", clobMaxUint256, "Collateral approval amount in wei; defaults to max uint256")
	approveCmd.Flags().String("collateral-token-address", evm.DefaultClobCollateralToken, "Collateral ERC-20 token address to approve when no market is provided")
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
			maker := strings.TrimSpace(mustStringFlag(cmd, "maker"))
			if mine {
				maker = firstNonEmpty(maker, ctx.Profile.EVMAddress)
			}
			if market == "" && maker == "" {
				return errors.New("provide --market with --outcome or --label, or use --maker/--mine for wallet-wide order history")
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
			var marketDetails *api.MarketDetails
			var selection *clobSelection
			var approvalStatus map[string]any
			if market != "" {
				marketDetails, selection, err = resolveClobReadSelection(cmd.Context(), ctx, cmd, market)
				if err != nil {
					return err
				}
				filters.Set("clob_id", clobIDForMarketOutcome(marketDetails.ID, selection.Binding.OutcomeIndex, selection.DisplayedSide))
				filters.Set("token_side", selection.DisplayedSide)
				if maker != "" && common.IsHexAddress(maker) && !isZeroAddress(maker) {
					status, statusErr := buildClobWalletStatus(cmd.Context(), ctx, marketDetails, common.HexToAddress(maker), selection.ExchangeAddress, selection.OutcomeToken)
					if statusErr != nil {
						return statusErr
					}
					approvalStatus, err = buildClobApprovalStatus(status)
					if err != nil {
						return err
					}
				}
			}
			orders, err := ctx.API.ListClobOrders(cmd.Context(), projectionURL, filters)
			if err != nil {
				return err
			}
			payload := map[string]any{"items": orders, "total": len(orders)}
			if marketDetails != nil && selection != nil {
				payload["market"] = marketDetails.ID
				payload["outcome"] = selection.LogicalOutcome.Index
				payload["label"] = selection.LogicalOutcome.Label
				payload["tokenSide"] = selection.DisplayedSide
			}
			if maker != "" {
				payload["maker"] = maker
			}
			if approvalStatus != nil {
				payload["approvalStatus"] = approvalStatus
			}
			return printOutput(ctx.JSON, payload)
		},
	}
	ordersListCmd.Flags().String("market", "", "Logical market ID for the CLOB proposition; optional when using --maker or --mine")
	ordersListCmd.Flags().Int("outcome", 0, "Displayed outcome index within the logical market")
	ordersListCmd.Flags().String("label", "", "Displayed outcome label within the logical market")
	ordersListCmd.Flags().String("token-side", "yes", "Hosted token-side book to inspect when using --market: yes or no; inferred from the displayed outcome for single-binding yes/no markets when omitted")
	ordersListCmd.Flags().String("maker", "", "Optional maker wallet filter")
	ordersListCmd.Flags().Bool("mine", false, "Filter orders to the active profile wallet")
	ordersListCmd.Flags().String("status", "", "Optional order status filter")
	ordersListCmd.Flags().Bool("active-only", false, "Only return resting active orders")
	ordersListCmd.Flags().Int("limit", 20, "Maximum number of orders to return")
	ordersListCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	ordersCmd.AddCommand(ordersListCmd)

	ordersCancelCmd := &cobra.Command{
		Use:   "cancel --order-id <id> --market <id>",
		Short: "Cancel a hosted resting CLOB order using the requester wallet signature",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			wallet, _, err := requireEVMWalletWithKey(ctx)
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
			order, err := ctx.API.GetClobOrder(cmd.Context(), strings.TrimSpace(mustStringFlag(cmd, "projection-url")), orderID)
			if err != nil {
				return err
			}
			if requester != "" && !strings.EqualFold(strings.TrimSpace(order.Maker), requester) {
				return errors.New("requester must match the order maker")
			}
			marketDetails, selection, err := resolveClobReadSelection(cmd.Context(), ctx, cmd, market)
			if err != nil {
				return err
			}
			reason := strings.TrimSpace(mustStringFlag(cmd, "reason"))
			eventstoreURL := strings.TrimSpace(mustStringFlag(cmd, "eventstore-url"))
			signingDomain, err := resolveClobSigningDomain(cmd)
			if err != nil {
				return err
			}
			cancelRequest, err := buildSignedClobCancel(wallet, signingDomain, orderID, marketDetails.ID, selection.Binding.OutcomeIndex, order.TokenSide, requester, reason)
			if err != nil {
				return err
			}
			response, err := ctx.API.CancelClobOrder(cmd.Context(), eventstoreURL, orderID, cancelRequest)
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, response)
		},
	}
	ordersCancelCmd.Flags().String("order-id", "", "Order UUID to cancel")
	ordersCancelCmd.Flags().String("market", "", "Logical market ID for the order book")
	ordersCancelCmd.Flags().Int("outcome", 0, "Displayed outcome index within the logical market")
	ordersCancelCmd.Flags().String("label", "", "Displayed outcome label within the logical market")
	ordersCancelCmd.Flags().String("requester", "", "Requester wallet address; defaults to the active profile EVM address")
	ordersCancelCmd.Flags().String("reason", "user-requested", "Optional cancellation reason")
	ordersCancelCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	ordersCmd.AddCommand(ordersCancelCmd)
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
			wallet := strings.TrimSpace(mustStringFlag(cmd, "wallet"))
			if mine {
				wallet = firstNonEmpty(wallet, ctx.Profile.EVMAddress)
			}
			if market == "" && wallet == "" {
				return errors.New("provide --market with --outcome or --label, or use --wallet/--mine for wallet-wide fill history")
			}
			limit, err := cmd.Flags().GetInt("limit")
			if err != nil {
				return err
			}
			projectionURL := strings.TrimSpace(mustStringFlag(cmd, "projection-url"))
			filters := url.Values{}
			var marketDetails *api.MarketDetails
			var selection *clobSelection
			var approvalStatus map[string]any
			if market != "" {
				marketDetails, selection, err = resolveClobReadSelection(cmd.Context(), ctx, cmd, market)
				if err != nil {
					return err
				}
				filters.Set("clob_id", clobIDForMarketOutcome(marketDetails.ID, selection.Binding.OutcomeIndex, selection.DisplayedSide))
				filters.Set("token_side", selection.DisplayedSide)
				if wallet != "" && common.IsHexAddress(wallet) && !isZeroAddress(wallet) {
					status, statusErr := buildClobWalletStatus(cmd.Context(), ctx, marketDetails, common.HexToAddress(wallet), selection.ExchangeAddress, selection.OutcomeToken)
					if statusErr != nil {
						return statusErr
					}
					approvalStatus, err = buildClobApprovalStatus(status)
					if err != nil {
						return err
					}
				}
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
			if marketDetails != nil && selection != nil {
				payload["market"] = marketDetails.ID
				payload["outcome"] = selection.LogicalOutcome.Index
				payload["label"] = selection.LogicalOutcome.Label
				payload["tokenSide"] = selection.DisplayedSide
			}
			if wallet != "" {
				payload["wallet"] = wallet
			}
			if approvalStatus != nil {
				payload["approvalStatus"] = approvalStatus
			}
			return printOutput(ctx.JSON, payload)
		},
	}
	fillsListCmd.Flags().String("market", "", "Logical market ID for the CLOB proposition; optional when using --wallet or --mine")
	fillsListCmd.Flags().Int("outcome", 0, "Displayed outcome index within the logical market")
	fillsListCmd.Flags().String("label", "", "Displayed outcome label within the logical market")
	fillsListCmd.Flags().String("token-side", "yes", "Hosted token-side book to inspect when using --market: yes or no; inferred from the displayed outcome for single-binding yes/no markets when omitted")
	fillsListCmd.Flags().String("wallet", "", "Optional wallet filter for buyer or seller participation")
	fillsListCmd.Flags().Bool("mine", false, "Filter fills to the active profile wallet")
	fillsListCmd.Flags().Int("limit", 20, "Maximum number of fills to return")
	fillsListCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
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
			wallet, privateKeyHex, err := requireEVMWalletWithKey(ctx)
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
			signingDomain, err := resolveClobSigningDomain(cmd)
			if err != nil {
				return err
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
				signingDomain,
			)
			if err != nil {
				return err
			}
			if orderType == "limit" {
				if err := validateClobSettleableQuantity(payload); err != nil {
					return err
				}
			}

			if mustBoolFlag(cmd, "dry-run") {
				status, err := buildClobWalletStatus(cmd.Context(), ctx, market, wallet.Address(), selection.ExchangeAddress, selection.OutcomeToken)
				if err != nil {
					return fmt.Errorf("load order wallet status: %w", err)
				}
				approvalStatus, err := buildClobApprovalStatus(status)
				if err != nil {
					return err
				}
				blocking := collectClobSmokeBlocking(status, selection, payload)
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
					"blocking":        blocking,
				}
				return printOutput(ctx.JSON, map[string]any{
					"dryRun":         true,
					"orderReady":     len(blocking) == 0,
					"approvalStatus": approvalStatus,
					"order":          preview,
				})
			}

			status, err := buildClobWalletStatus(cmd.Context(), ctx, market, wallet.Address(), selection.ExchangeAddress, selection.OutcomeToken)
			if err != nil {
				return fmt.Errorf("load order wallet status: %w", err)
			}

			blocking := collectClobSmokeBlocking(status, selection, payload)
			approvals, err := ensureClobOrderApprovals(cmd.Context(), ctx.Config.EVMRPCURL, privateKeyHex, wallet.Address(), status, selection, payload, true)
			if err != nil {
				return fmt.Errorf("auto-approve order prerequisites: %w", err)
			}
			if len(approvals) > 0 {
				blocking = collectClobSmokeBlockingAfterApprovals(status, selection, payload)
			}
			if len(blocking) > 0 {
				return fmt.Errorf("order is not ready: %s", strings.Join(blocking, "; "))
			}

			response, err := ctx.API.SubmitClobOrder(cmd.Context(), strings.TrimSpace(mustStringFlag(cmd, "eventstore-url")), payload)
			if err != nil {
				return err
			}
			result := map[string]any{
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
			}
			if len(approvals) > 0 {
				result["approvals"] = approvals
			}
			return printOutput(ctx.JSON, result)
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
		Short: "Cancel a hosted resting CLOB order using the requester wallet signature",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			wallet, _, err := requireEVMWalletWithKey(ctx)
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
			order, err := ctx.API.GetClobOrder(cmd.Context(), strings.TrimSpace(mustStringFlag(cmd, "projection-url")), orderID)
			if err != nil {
				return err
			}
			if requester != "" && !strings.EqualFold(strings.TrimSpace(order.Maker), requester) {
				return errors.New("requester must match the order maker")
			}
			marketDetails, selection, err := resolveClobReadSelection(cmd.Context(), ctx, cmd, market)
			if err != nil {
				return err
			}
			reason := strings.TrimSpace(mustStringFlag(cmd, "reason"))
			eventstoreURL := strings.TrimSpace(mustStringFlag(cmd, "eventstore-url"))
			signingDomain, err := resolveClobSigningDomain(cmd)
			if err != nil {
				return err
			}
			cancelRequest, err := buildSignedClobCancel(wallet, signingDomain, orderID, marketDetails.ID, selection.Binding.OutcomeIndex, order.TokenSide, requester, reason)
			if err != nil {
				return err
			}
			response, err := ctx.API.CancelClobOrder(cmd.Context(), eventstoreURL, orderID, cancelRequest)
			if err != nil {
				return err
			}
			return printOutput(ctx.JSON, response)
		},
	}
	cancelCmd.Flags().String("order-id", "", "Order UUID to cancel")
	cancelCmd.Flags().String("market", "", "Logical market ID for the order book")
	cancelCmd.Flags().Int("outcome", 0, "Displayed outcome index within the logical market")
	cancelCmd.Flags().String("label", "", "Displayed outcome label within the logical market")
	cancelCmd.Flags().String("requester", "", "Requester wallet address; defaults to the active profile EVM address")
	cancelCmd.Flags().String("reason", "user-requested", "Optional cancellation reason")
	cancelCmd.Flags().String("token-side", "yes", "Hosted token-side book to inspect: yes or no; inferred from the displayed outcome for single-binding yes/no markets when omitted")
	cancelCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
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
			market, err := loadMarketWithClobFallback(cmd.Context(), ctx, args[0], mustStringFlag(cmd, "instance-date"))
			if err != nil {
				return err
			}
			if !isClobMarketImplementation(market.MarketImplementation) {
				return errors.New("clob split requires an AxiomCTFMarket logical market")
			}
			return runClobSplit(cmd, ctx, market)
		},
	}
	splitCmd.Flags().String("label", "", "Outcome label to identify the binding for split")
	splitCmd.Flags().String("amount", "", "Amount of collateral to split (decimal XRP like 0.01 or integer wei)")
	splitCmd.Flags().Bool("wait", false, "Wait for the split transaction receipt")
	splitCmd.Flags().Bool("dry-run", false, "Preview the split without broadcasting a transaction")
	splitCmd.Flags().Bool("skip-approval", false, "Skip the automatic collateral approval to the ConditionalTokens contract")
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

			// Resolve YES and NO token IDs and read on-chain balances.
			yesTokenID, _, _ := resolveDisplayedTokenID(binding, "yes", collateralToken)
			noTokenID, _, _ := resolveDisplayedTokenID(binding, "no", collateralToken)

			yesBalance := big.NewInt(0)
			noBalance := big.NewInt(0)
			if yesTokenID != nil {
				yesBalance, err = getERC1155Balance(cmd.Context(), ctx.Config.EVMRPCURL, conditionalTokens, wallet.Address(), yesTokenID)
				if err != nil {
					return fmt.Errorf("read YES balance: %w", err)
				}
			}
			if noTokenID != nil {
				noBalance, err = getERC1155Balance(cmd.Context(), ctx.Config.EVMRPCURL, conditionalTokens, wallet.Address(), noTokenID)
				if err != nil {
					return fmt.Errorf("read NO balance: %w", err)
				}
			}

			// Compute max mergeable = min(yesBalance, noBalance).
			maxMergeable := new(big.Int).Set(yesBalance)
			if noBalance.Cmp(maxMergeable) < 0 {
				maxMergeable.Set(noBalance)
			}

			// Determine merge amount: --max uses max mergeable; --amount specifies explicit value.
			var amount *big.Int
			useMax := mustBoolFlag(cmd, "max")
			amountStr := strings.TrimSpace(mustStringFlag(cmd, "amount"))
			if useMax && amountStr != "" {
				return errors.New("cannot use both --max and --amount; choose one")
			}
			if useMax {
				if maxMergeable.Sign() <= 0 {
					return errors.New("nothing to merge: both YES and NO balances are needed")
				}
				amount = new(big.Int).Set(maxMergeable)
			} else {
				if amountStr == "" {
					return errors.New("--amount or --max is required")
				}
				amount, err = parseClobAmount(amountStr)
				if err != nil {
					return err
				}
			}

			// Validate the amount does not exceed what's mergeable.
			if amount.Cmp(maxMergeable) > 0 {
				return fmt.Errorf("merge amount %s wei (%s XRP) exceeds max mergeable %s wei (%s XRP); YES=%s, NO=%s",
					amount.String(), formatWeiToXRP(amount),
					maxMergeable.String(), formatWeiToXRP(maxMergeable),
					yesBalance.String(), noBalance.String())
			}

			if mustBoolFlag(cmd, "dry-run") {
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
					"yesBalanceWei":     yesBalance.String(),
					"yesBalanceXrp":     formatWeiToXRP(yesBalance),
					"noBalanceWei":      noBalance.String(),
					"noBalanceXrp":      formatWeiToXRP(noBalance),
					"maxMergeableWei":   maxMergeable.String(),
					"maxMergeableXrp":   formatWeiToXRP(maxMergeable),
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
				"action":          "merge",
				"market":          market.Title,
				"marketId":        market.ID,
				"outcomeLabel":    binding.Label,
				"amountWei":       amount.String(),
				"amountXrp":       formatWeiToXRP(amount),
				"txHash":          txHash.Hex(),
				"wallet":          wallet.Address().Hex(),
				"yesBalanceWei":   yesBalance.String(),
				"yesBalanceXrp":   formatWeiToXRP(yesBalance),
				"noBalanceWei":    noBalance.String(),
				"noBalanceXrp":    formatWeiToXRP(noBalance),
				"maxMergeableWei": maxMergeable.String(),
				"maxMergeableXrp": formatWeiToXRP(maxMergeable),
			}
			if yesTokenID != nil {
				result["yesTokenId"] = yesTokenID.String()
			}
			if noTokenID != nil {
				result["noTokenId"] = noTokenID.String()
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
	mergeCmd.Flags().String("amount", "", "Amount of matched YES+NO shares to merge (decimal XRP like 0.01 or integer wei)")
	mergeCmd.Flags().Bool("max", false, "Merge the maximum possible amount: min(YES balance, NO balance)")
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

func newMMCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mm",
		Short: "Run higher-level market-making workflows on hosted CLOB markets",
		Long: strings.Join([]string{
			"Run higher-level market-making workflows on hosted CLOB markets.",
			"",
			"These commands sit above the lower-level `clob` surface and focus on common",
			"operator tasks like inventory checks, two-sided quote placement, and bulk",
			"order cleanup for the active trading wallet.",
		}, "\n"),
	}
	cmd.PersistentFlags().String("projection-url", firstNonEmpty(os.Getenv("AXIOM_CLOB_PROJECTION_URL"), os.Getenv("CLOB_PROJECTION_URL"), defaultClobProjectionURL), "Override the hosted CLOB projection base URL")
	cmd.PersistentFlags().String("eventstore-url", firstNonEmpty(os.Getenv("AXIOM_CLOB_EVENTSTORE_URL"), os.Getenv("CLOB_EVENTSTORE_URL"), defaultClobEventstoreURL), "Override the hosted CLOB eventstore base URL")
	cmd.PersistentFlags().String("exchange-address", evm.DefaultClobExchangeAddress, "Override the on-chain AxiomCTFExchange address used for approvals and settlement prep")
	cmd.PersistentFlags().String("clob-domain-contract", firstNonEmpty(os.Getenv("AXIOM_CLOB_DOMAIN_CONTRACT"), evm.DefaultClobDomainContract), "Override the hosted CLOB EIP-712 verifying contract used for order and cancel signing")
	cmd.PersistentFlags().String("clob-chain-id", firstNonEmpty(os.Getenv("AXIOM_CLOB_CHAIN_ID"), strconv.FormatInt(evm.DefaultClobChainID, 10)), "Override the hosted CLOB EIP-712 chain ID used for order and cancel signing")
	cmd.PersistentFlags().String("outcome-token-address", evm.DefaultClobConditionalTokens, "Override the on-chain AxiomConditionalTokens address used for balances and approvals")

	marketCmd := &cobra.Command{Use: "market", Short: "Search, select, and manage the active market-maker market"}
	marketListCmd := &cobra.Command{
		Use:   "list",
		Short: "List hosted CLOB markets for market-making workflows",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			search := strings.TrimSpace(mustStringFlag(cmd, "search"))
			status := strings.TrimSpace(mustStringFlag(cmd, "status"))
			category := strings.TrimSpace(mustStringFlag(cmd, "category"))
			limit, err := cmd.Flags().GetInt("limit")
			if err != nil {
				return err
			}
			marketsResponse, err := withLoadingIndicator(ctx.JSON, "Loading hosted CLOB markets", func() (*api.MarketsResponse, error) {
				return ctx.ConsoleAPI.ListAllMarkets(cmd.Context(), status, search, "", normalizeMarketImplementation("clob"), true, 0)
			})
			if err != nil {
				return err
			}
			filtered := filterMMMarkets(marketsResponse, category)
			filtered = paginateMarkets(filtered, limit, 0)
			return printOutput(ctx.JSON, map[string]any{
				"activeMarket": activeMMMarketSelection(ctx),
				"items":        buildMMMarketListItems(filtered.Items),
				"total":        filtered.Total,
			})
		},
	}
	marketListCmd.Flags().String("search", "", "Search by title or headline")
	marketListCmd.Flags().String("status", "open", "Filter by market status: open or resolved")
	marketListCmd.Flags().String("category", "", "Optional category filter")
	marketListCmd.Flags().Int("limit", 20, "Maximum number of CLOB markets to return")
	marketCmd.AddCommand(marketListCmd)

	marketUseCmd := &cobra.Command{
		Use:   "use [market-id-or-address]",
		Short: "Set the active market-maker market, optionally via an interactive picker",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}

			marketRef := ""
			if len(args) > 0 {
				marketRef = strings.TrimSpace(args[0])
			}
			instanceDate := strings.TrimSpace(mustStringFlag(cmd, "instance-date"))
			market, err := selectMMMarket(cmd.Context(), ctx, cmd, marketRef, instanceDate)
			if err != nil {
				return err
			}
			if !isClobMarketImplementation(market.MarketImplementation) {
				return errors.New("mm market use requires an AxiomCTFMarket logical market")
			}

			selection := buildMMMarketSelection(market, instanceDate)
			if err := saveActiveMMMarket(ctx.ProfileName, selection); err != nil {
				return err
			}
			return printOutput(ctx.JSON, map[string]any{
				"activeMarket": selection,
			})
		},
	}
	marketUseCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	marketUseCmd.Flags().String("search", "", "Search text used by the interactive picker when no market argument is provided")
	marketUseCmd.Flags().String("status", "open", "Status filter used by the interactive picker")
	marketUseCmd.Flags().String("category", "", "Category filter used by the interactive picker")
	marketUseCmd.Flags().Int("limit", 20, "Maximum number of markets to show in the interactive picker")
	marketCmd.AddCommand(marketUseCmd)

	marketShowCmd := &cobra.Command{
		Use:   "show",
		Short: "Show the current active market-maker market",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			selection := activeMMMarketSelection(ctx)
			if selection == nil {
				return errors.New("no active market-maker market is set; run `axiom mm market use`")
			}
			return printOutput(ctx.JSON, map[string]any{"activeMarket": selection})
		},
	}
	marketCmd.AddCommand(marketShowCmd)

	marketClearCmd := &cobra.Command{
		Use:   "clear",
		Short: "Clear the current active market-maker market",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			if err := clearActiveMMMarket(ctx.ProfileName); err != nil {
				return err
			}
			return printOutput(ctx.JSON, map[string]any{"activeMarket": nil, "cleared": true})
		},
	}
	marketCmd.AddCommand(marketClearCmd)
	cmd.AddCommand(marketCmd)

	mintCmd := &cobra.Command{
		Use:   "mint [market-id-or-address]",
		Short: "Mint complete-set CTF inventory for the active market-making workflow",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
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
				return errors.New("mm mint requires an AxiomCTFMarket logical market")
			}
			return runClobSplit(cmd, ctx, market)
		},
	}
	mintCmd.Flags().String("label", "", "Outcome label to identify the binding for minting inventory")
	mintCmd.Flags().String("amount", "", "Amount of collateral to mint into complete-set inventory (decimal XRP like 0.01 or integer wei)")
	mintCmd.Flags().Bool("wait", false, "Wait for the mint transaction receipt")
	mintCmd.Flags().Bool("dry-run", false, "Preview the mint without broadcasting a transaction")
	mintCmd.Flags().Bool("skip-approval", false, "Skip the automatic collateral approval to the ConditionalTokens contract")
	mintCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	cmd.AddCommand(mintCmd)

	inventoryCmd := &cobra.Command{
		Use:   "inventory [market-id-or-address]",
		Short: "Summarize inventory, approvals, and imbalance for a hosted CLOB market",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}

			walletAddress, err := resolveProfileAddress(ctx, nil)
			if override := strings.TrimSpace(mustStringFlag(cmd, "wallet")); override != "" {
				if !common.IsHexAddress(override) || isZeroAddress(override) {
					return errors.New("--wallet must be a valid non-zero 0x-prefixed address")
				}
				walletAddress = override
				err = nil
			}
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
				return errors.New("mm inventory requires an AxiomCTFMarket logical market")
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
			approvalStatus, err := buildClobApprovalStatus(status)
			if err != nil {
				return err
			}

			payload, err := buildMMInventoryOutput(status)
			if err != nil {
				return err
			}
			payload["approvalStatus"] = approvalStatus
			return printOutput(ctx.JSON, payload)
		},
	}
	inventoryCmd.Flags().String("wallet", "", "Wallet address to inspect; defaults to the active profile EVM address")
	inventoryCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	cmd.AddCommand(inventoryCmd)

	cancelAllCmd := &cobra.Command{
		Use:   "cancel-all",
		Short: "Cancel active hosted CLOB orders for the active market-making wallet",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			wallet, err := requireEVMWallet(ctx)
			if err != nil {
				return err
			}

			marketRef := strings.TrimSpace(mustStringFlag(cmd, "market"))
			if marketRef == "" {
				active := activeMMMarketSelection(ctx)
				if active != nil {
					marketRef = active.MarketID
					if strings.TrimSpace(mustStringFlag(cmd, "instance-date")) == "" {
						_ = cmd.Flags().Set("instance-date", active.InstanceDate)
					}
				}
			}
			outcomeRaw := strings.TrimSpace(mustStringFlag(cmd, "outcome"))
			label := strings.TrimSpace(mustStringFlag(cmd, "label"))
			tokenSideFilter := strings.TrimSpace(mustStringFlag(cmd, "token-side"))
			displayedSideFilter := strings.TrimSpace(mustStringFlag(cmd, "displayed-side"))
			switch {
			case tokenSideFilter != "" && displayedSideFilter != "" && !strings.EqualFold(tokenSideFilter, displayedSideFilter):
				return errors.New("--token-side and --displayed-side must match when both are provided")
			case tokenSideFilter == "":
				tokenSideFilter = displayedSideFilter
			}
			if marketRef == "" && (outcomeRaw != "" || label != "" || tokenSideFilter != "") {
				return errors.New("--market is required when using --outcome, --label, --token-side, or --displayed-side")
			}

			projectionURL := strings.TrimSpace(mustStringFlag(cmd, "projection-url"))
			filters := url.Values{}
			filters.Set("maker", wallet.Address().Hex())
			filters.Set("active_only", "true")
			limit, err := cmd.Flags().GetInt("limit")
			if err != nil {
				return err
			}
			if limit > 0 {
				filters.Set("limit", strconv.Itoa(limit))
			}
			orders, err := ctx.API.ListClobOrders(cmd.Context(), projectionURL, filters)
			if err != nil {
				return err
			}

			targetClobIDs := make(map[string]struct{})
			if marketRef != "" {
				market, err := loadMMMarket(cmd.Context(), ctx, marketRef, mustStringFlag(cmd, "instance-date"))
				if err != nil {
					return err
				}
				if !isClobMarketImplementation(market.MarketImplementation) {
					return errors.New("mm cancel-all requires an AxiomCTFMarket logical market when --market is provided")
				}

				switch {
				case outcomeRaw != "" || label != "":
					selection, err := resolveClobSelection(
						market,
						outcomeRaw,
						label,
						tokenSideFilter,
						mustStringFlag(cmd, "exchange-address"),
						mustStringFlag(cmd, "outcome-token-address"),
					)
					if err != nil {
						return err
					}
					targetClobIDs[clobIDForMarketOutcome(market.ID, selection.Binding.OutcomeIndex, selection.DisplayedSide)] = struct{}{}
				case tokenSideFilter != "":
					normalizedSide, err := normalizeClobTokenSide(tokenSideFilter)
					if err != nil {
						return err
					}
					for _, binding := range sortedClobBindings(market.CTFOutcomeMarkets) {
						targetClobIDs[clobIDForMarketOutcome(market.ID, binding.OutcomeIndex, normalizedSide)] = struct{}{}
					}
				default:
					for _, clobID := range collectMMMarketClobIDs(market) {
						targetClobIDs[clobID] = struct{}{}
					}
				}
			}

			targetOrders := make([]api.ClobOrder, 0, len(orders))
			for _, order := range orders {
				if len(targetClobIDs) > 0 {
					if _, ok := targetClobIDs[strings.TrimSpace(order.ClobID)]; !ok {
						continue
					}
				}
				targetOrders = append(targetOrders, order)
			}

			items := make([]map[string]any, 0, len(targetOrders))
			failures := make([]map[string]any, 0)
			dryRun := mustBoolFlag(cmd, "dry-run")
			if dryRun {
				for _, order := range targetOrders {
					marketID, outcomeIndex, tokenSide, parseErr := parseClobOrderIdentity(order)
					if parseErr != nil {
						failures = append(failures, map[string]any{
							"orderId": order.OrderID,
							"clobId":  order.ClobID,
							"error":   parseErr.Error(),
						})
						continue
					}
					items = append(items, map[string]any{
						"orderId":   order.OrderID,
						"clobId":    order.ClobID,
						"marketId":  marketID,
						"outcome":   outcomeIndex,
						"tokenSide": tokenSide,
						"side":      order.Side,
						"quantity":  order.Quantity,
						"remaining": order.Remaining,
						"status":    order.Status,
					})
				}
				return printOutput(ctx.JSON, map[string]any{
					"dryRun":          true,
					"walletAddress":   wallet.Address().Hex(),
					"market":          marketRef,
					"totalOpenOrders": len(orders),
					"targetedOrders":  len(targetOrders),
					"items":           items,
					"failures":        failures,
				})
			}

			signingDomain, err := resolveClobSigningDomain(cmd)
			if err != nil {
				return err
			}
			eventstoreURL := strings.TrimSpace(mustStringFlag(cmd, "eventstore-url"))
			reason := firstNonEmpty(strings.TrimSpace(mustStringFlag(cmd, "reason")), "market-maker-cancel-all")
			for _, order := range targetOrders {
				marketID, outcomeIndex, tokenSide, parseErr := parseClobOrderIdentity(order)
				if parseErr != nil {
					failures = append(failures, map[string]any{
						"orderId": order.OrderID,
						"clobId":  order.ClobID,
						"error":   parseErr.Error(),
					})
					continue
				}
				cancelRequest, err := buildSignedClobCancel(wallet, signingDomain, order.OrderID, marketID, outcomeIndex, tokenSide, wallet.Address().Hex(), reason)
				if err != nil {
					failures = append(failures, map[string]any{
						"orderId": order.OrderID,
						"clobId":  order.ClobID,
						"error":   err.Error(),
					})
					continue
				}
				response, err := ctx.API.CancelClobOrder(cmd.Context(), eventstoreURL, order.OrderID, cancelRequest)
				if err != nil {
					failures = append(failures, map[string]any{
						"orderId": order.OrderID,
						"clobId":  order.ClobID,
						"error":   err.Error(),
					})
					continue
				}
				items = append(items, map[string]any{
					"orderId":         response.OrderID,
					"clobId":          order.ClobID,
					"marketId":        marketID,
					"outcome":         outcomeIndex,
					"tokenSide":       tokenSide,
					"remainingShares": response.RemainingQuantity,
					"tradeCount":      response.TradeCount,
					"resting":         response.WasAddedToBook,
				})
			}

			return printOutput(ctx.JSON, map[string]any{
				"walletAddress":   wallet.Address().Hex(),
				"market":          marketRef,
				"totalOpenOrders": len(orders),
				"targetedOrders":  len(targetOrders),
				"cancelled":       len(items),
				"failed":          len(failures),
				"items":           items,
				"failures":        failures,
			})
		},
	}
	cancelAllCmd.Flags().String("market", "", "Logical market ID to scope the bulk cancel; omit to target all active orders for the wallet")
	cancelAllCmd.Flags().String("outcome", "", "Optional logical outcome index filter when scoping to one market")
	cancelAllCmd.Flags().String("label", "", "Optional logical outcome label filter when scoping to one market")
	cancelAllCmd.Flags().String("token-side", "", "Optional hosted token-side filter when scoping to one market: yes or no")
	cancelAllCmd.Flags().String("displayed-side", "", "Optional displayed-side alias for --token-side when scoping to one market: yes or no")
	cancelAllCmd.Flags().String("reason", "market-maker-cancel-all", "Optional cancellation reason recorded with each cancel request")
	cancelAllCmd.Flags().Int("limit", 200, "Maximum number of active orders to fetch before canceling")
	cancelAllCmd.Flags().Bool("dry-run", false, "List the targeted active orders without canceling them")
	cancelAllCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	cmd.AddCommand(cancelAllCmd)

	statusCmd := &cobra.Command{
		Use:   "status [market-id-or-address]",
		Short: "Show active market, inventory, orders, fills, and top-of-book for one MM workflow",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}

			walletAddress, err := resolveProfileAddress(ctx, nil)
			if override := strings.TrimSpace(mustStringFlag(cmd, "wallet")); override != "" {
				if !common.IsHexAddress(override) || isZeroAddress(override) {
					return errors.New("--wallet must be a valid non-zero 0x-prefixed address")
				}
				walletAddress = override
				err = nil
			}
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
				return errors.New("mm status requires an AxiomCTFMarket logical market")
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

			status, err := buildClobWalletStatus(
				cmd.Context(),
				ctx,
				market,
				common.HexToAddress(walletAddress),
				selection.ExchangeAddress,
				selection.OutcomeToken,
			)
			if err != nil {
				return err
			}
			approvalStatus, err := buildClobApprovalStatus(status)
			if err != nil {
				return err
			}
			inventory, err := buildMMInventoryOutput(status)
			if err != nil {
				return err
			}

			projectionURL := strings.TrimSpace(mustStringFlag(cmd, "projection-url"))
			clobID := clobIDForMarketOutcome(market.ID, selection.Binding.OutcomeIndex, selection.DisplayedSide)

			book, err := ctx.API.GetClobBook(cmd.Context(), projectionURL, market.ID, selection.Binding.OutcomeIndex, selection.DisplayedSide)
			if err != nil {
				return err
			}
			depth, err := ctx.API.GetClobDepth(cmd.Context(), projectionURL, market.ID, selection.Binding.OutcomeIndex, selection.DisplayedSide)
			if err != nil {
				return err
			}

			orderLimit, err := cmd.Flags().GetInt("order-limit")
			if err != nil {
				return err
			}
			orderFilters := url.Values{}
			orderFilters.Set("maker", walletAddress)
			orderFilters.Set("clob_id", clobID)
			orderFilters.Set("token_side", selection.DisplayedSide)
			orderFilters.Set("active_only", "true")
			if orderLimit > 0 {
				orderFilters.Set("limit", strconv.Itoa(orderLimit))
			}
			orders, err := ctx.API.ListClobOrders(cmd.Context(), projectionURL, orderFilters)
			if err != nil {
				return err
			}

			fillLimit, err := cmd.Flags().GetInt("fill-limit")
			if err != nil {
				return err
			}
			fillFilters := url.Values{}
			fillFilters.Set("wallet", walletAddress)
			fillFilters.Set("clob_id", clobID)
			fillFilters.Set("token_side", selection.DisplayedSide)
			if fillLimit > 0 {
				fillFilters.Set("limit", strconv.Itoa(fillLimit))
			}
			fills, err := ctx.API.ListClobFills(cmd.Context(), projectionURL, fillFilters)
			if err != nil {
				return err
			}

			payload := map[string]any{
				"activeMarket":     buildMMMarketSelection(market, instanceDate),
				"walletAddress":    walletAddress,
				"marketId":         market.ID,
				"marketTitle":      market.Title,
				"outcomeLabel":     selection.LogicalOutcome.Label,
				"outcomeIndex":     selection.Binding.OutcomeIndex,
				"displayedSide":    selection.DisplayedSide,
				"clobId":           clobID,
				"approvalStatus":   approvalStatus,
				"inventory":        inventory,
				"book":             book,
				"depth":            depth,
				"activeOrders":     orders,
				"recentFills":      fills,
				"activeOrderCount": len(orders),
				"recentFillCount":  len(fills),
			}
			return printOutput(ctx.JSON, payload)
		},
	}
	statusCmd.Flags().String("outcome", "", "Logical outcome index to inspect")
	statusCmd.Flags().String("label", "", "Logical outcome label to inspect")
	statusCmd.Flags().String("displayed-side", "", "Displayed side to inspect: yes or no; inferred for single-binding binary markets")
	statusCmd.Flags().String("wallet", "", "Wallet address to inspect; defaults to the active profile EVM address")
	statusCmd.Flags().Int("order-limit", 20, "Maximum number of active orders to include")
	statusCmd.Flags().Int("fill-limit", 20, "Maximum number of recent fills to include")
	statusCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	cmd.AddCommand(statusCmd)

	ordersCmd := &cobra.Command{
		Use:   "orders [market-id-or-address]",
		Short: "List active MM orders for one exact hosted CLOB book",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}

			walletAddress, err := resolveProfileAddress(ctx, nil)
			if override := strings.TrimSpace(mustStringFlag(cmd, "wallet")); override != "" {
				if !common.IsHexAddress(override) || isZeroAddress(override) {
					return errors.New("--wallet must be a valid non-zero 0x-prefixed address")
				}
				walletAddress = override
				err = nil
			}
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
				return errors.New("mm orders requires an AxiomCTFMarket logical market")
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
			status, err := buildClobWalletStatus(
				cmd.Context(),
				ctx,
				market,
				common.HexToAddress(walletAddress),
				selection.ExchangeAddress,
				selection.OutcomeToken,
			)
			if err != nil {
				return err
			}
			approvalStatus, err := buildClobApprovalStatus(status)
			if err != nil {
				return err
			}

			filters := url.Values{}
			filters.Set("maker", walletAddress)
			filters.Set("clob_id", clobIDForMarketOutcome(market.ID, selection.Binding.OutcomeIndex, selection.DisplayedSide))
			filters.Set("token_side", selection.DisplayedSide)
			filters.Set("active_only", "true")
			limit, err := cmd.Flags().GetInt("limit")
			if err != nil {
				return err
			}
			if limit > 0 {
				filters.Set("limit", strconv.Itoa(limit))
			}
			orders, err := ctx.API.ListClobOrders(cmd.Context(), strings.TrimSpace(mustStringFlag(cmd, "projection-url")), filters)
			if err != nil {
				return err
			}

			return printOutput(ctx.JSON, map[string]any{
				"activeMarket":   buildMMMarketSelection(market, instanceDate),
				"walletAddress":  walletAddress,
				"marketId":       market.ID,
				"marketTitle":    market.Title,
				"outcomeLabel":   selection.LogicalOutcome.Label,
				"outcomeIndex":   selection.Binding.OutcomeIndex,
				"displayedSide":  selection.DisplayedSide,
				"clobId":         clobIDForMarketOutcome(market.ID, selection.Binding.OutcomeIndex, selection.DisplayedSide),
				"approvalStatus": approvalStatus,
				"items":          orders,
				"total":          len(orders),
			})
		},
	}
	ordersCmd.Flags().String("outcome", "", "Logical outcome index to inspect")
	ordersCmd.Flags().String("label", "", "Logical outcome label to inspect")
	ordersCmd.Flags().String("displayed-side", "", "Displayed side to inspect: yes or no; inferred for single-binding binary markets")
	ordersCmd.Flags().String("wallet", "", "Wallet address to inspect; defaults to the active profile EVM address")
	ordersCmd.Flags().Int("limit", 20, "Maximum number of active orders to return")
	ordersCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	cmd.AddCommand(ordersCmd)

	bookCmd := &cobra.Command{
		Use:   "book [market-id-or-address]",
		Short: "Fetch the hosted book summary and depth for one exact MM book",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
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
				return errors.New("mm book requires an AxiomCTFMarket logical market")
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

			projectionURL := strings.TrimSpace(mustStringFlag(cmd, "projection-url"))
			book, err := ctx.API.GetClobBook(cmd.Context(), projectionURL, market.ID, selection.Binding.OutcomeIndex, selection.DisplayedSide)
			if err != nil {
				return err
			}
			depth, err := ctx.API.GetClobDepth(cmd.Context(), projectionURL, market.ID, selection.Binding.OutcomeIndex, selection.DisplayedSide)
			if err != nil {
				return err
			}

			return printOutput(ctx.JSON, map[string]any{
				"activeMarket":  buildMMMarketSelection(market, instanceDate),
				"marketId":      market.ID,
				"marketTitle":   market.Title,
				"outcomeLabel":  selection.LogicalOutcome.Label,
				"outcomeIndex":  selection.Binding.OutcomeIndex,
				"displayedSide": selection.DisplayedSide,
				"clobId":        clobIDForMarketOutcome(market.ID, selection.Binding.OutcomeIndex, selection.DisplayedSide),
				"book":          book,
				"depth":         depth,
			})
		},
	}
	bookCmd.Flags().String("outcome", "", "Logical outcome index to inspect")
	bookCmd.Flags().String("label", "", "Logical outcome label to inspect")
	bookCmd.Flags().String("displayed-side", "", "Displayed side to inspect: yes or no; inferred for single-binding binary markets")
	bookCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	cmd.AddCommand(bookCmd)

	fillsCmd := &cobra.Command{
		Use:   "fills [market-id-or-address]",
		Short: "List recent MM fills for one exact hosted CLOB book",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}

			walletAddress, err := resolveProfileAddress(ctx, nil)
			if override := strings.TrimSpace(mustStringFlag(cmd, "wallet")); override != "" {
				if !common.IsHexAddress(override) || isZeroAddress(override) {
					return errors.New("--wallet must be a valid non-zero 0x-prefixed address")
				}
				walletAddress = override
				err = nil
			}
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
				return errors.New("mm fills requires an AxiomCTFMarket logical market")
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
			status, err := buildClobWalletStatus(
				cmd.Context(),
				ctx,
				market,
				common.HexToAddress(walletAddress),
				selection.ExchangeAddress,
				selection.OutcomeToken,
			)
			if err != nil {
				return err
			}
			approvalStatus, err := buildClobApprovalStatus(status)
			if err != nil {
				return err
			}

			filters := url.Values{}
			filters.Set("wallet", walletAddress)
			filters.Set("clob_id", clobIDForMarketOutcome(market.ID, selection.Binding.OutcomeIndex, selection.DisplayedSide))
			filters.Set("token_side", selection.DisplayedSide)
			limit, err := cmd.Flags().GetInt("limit")
			if err != nil {
				return err
			}
			if limit > 0 {
				filters.Set("limit", strconv.Itoa(limit))
			}
			fills, err := ctx.API.ListClobFills(cmd.Context(), strings.TrimSpace(mustStringFlag(cmd, "projection-url")), filters)
			if err != nil {
				return err
			}

			return printOutput(ctx.JSON, map[string]any{
				"activeMarket":   buildMMMarketSelection(market, instanceDate),
				"walletAddress":  walletAddress,
				"marketId":       market.ID,
				"marketTitle":    market.Title,
				"outcomeLabel":   selection.LogicalOutcome.Label,
				"outcomeIndex":   selection.Binding.OutcomeIndex,
				"displayedSide":  selection.DisplayedSide,
				"clobId":         clobIDForMarketOutcome(market.ID, selection.Binding.OutcomeIndex, selection.DisplayedSide),
				"approvalStatus": approvalStatus,
				"items":          fills,
				"total":          len(fills),
			})
		},
	}
	fillsCmd.Flags().String("outcome", "", "Logical outcome index to inspect")
	fillsCmd.Flags().String("label", "", "Logical outcome label to inspect")
	fillsCmd.Flags().String("displayed-side", "", "Displayed side to inspect: yes or no; inferred for single-binding binary markets")
	fillsCmd.Flags().String("wallet", "", "Wallet address to inspect; defaults to the active profile EVM address")
	fillsCmd.Flags().Int("limit", 20, "Maximum number of fills to return")
	fillsCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	cmd.AddCommand(fillsCmd)

	quoteCmd := &cobra.Command{
		Use:   "quote [market-id-or-address]",
		Short: "Place a two-sided market-making quote on one hosted CLOB book",
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
				return errors.New("mm quote requires an AxiomCTFMarket logical market")
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

			bidPriceBps, err := parseClobPriceToBps(mustStringFlag(cmd, "bid-price"))
			if err != nil {
				return fmt.Errorf("parse --bid-price: %w", err)
			}
			askPriceBps, err := parseClobPriceToBps(mustStringFlag(cmd, "ask-price"))
			if err != nil {
				return fmt.Errorf("parse --ask-price: %w", err)
			}
			if askPriceBps <= bidPriceBps {
				return errors.New("--ask-price must be greater than --bid-price")
			}
			quantity, err := parseClobQuantity(mustStringFlag(cmd, "quantity"))
			if err != nil {
				return err
			}

			signingDomain, err := resolveClobSigningDomain(cmd)
			if err != nil {
				return err
			}

			if mustBoolFlag(cmd, "cancel-active") {
				cancelReason := strings.TrimSpace(mustStringFlag(cmd, "cancel-reason"))
				if cancelReason == "" {
					cancelReason = "market-maker-quote-replace"
				}
				eventstoreURL := strings.TrimSpace(mustStringFlag(cmd, "eventstore-url"))
				projectionURL := strings.TrimSpace(mustStringFlag(cmd, "projection-url"))
				filters := url.Values{}
				filters.Set("maker", wallet.Address().Hex())
				filters.Set("active_only", "true")
				filters.Set("clob_id", clobIDForMarketOutcome(market.ID, selection.Binding.OutcomeIndex, selection.DisplayedSide))
				existingOrders, listErr := ctx.API.ListClobOrders(cmd.Context(), projectionURL, filters)
				if listErr != nil {
					return fmt.Errorf("list existing orders before cancel-active: %w", listErr)
				}
				for _, order := range existingOrders {
					_, existingOutcomeIndex, existingTokenSide, parseErr := parseClobOrderIdentity(order)
					if parseErr != nil {
						continue
					}
					cancelRequest, cancelErr := buildSignedClobCancel(wallet, signingDomain, order.OrderID, market.ID, existingOutcomeIndex, existingTokenSide, wallet.Address().Hex(), cancelReason)
					if cancelErr != nil {
						continue
					}
					if _, cancelErr = ctx.API.CancelClobOrder(cmd.Context(), eventstoreURL, order.OrderID, cancelRequest); cancelErr != nil {
						continue
					}
				}
			}

			expiry := mustStringFlag(cmd, "expiry")
			bidPayload, err := buildClobSignedOrder(wallet, market.ID, selection, "buy", "limit", bidPriceBps, quantity, expiry, signingDomain)
			if err != nil {
				return err
			}
			askPayload, err := buildClobSignedOrder(wallet, market.ID, selection, "sell", "limit", askPriceBps, quantity, expiry, signingDomain)
			if err != nil {
				return err
			}

			status, err := buildClobWalletStatus(cmd.Context(), ctx, market, wallet.Address(), selection.ExchangeAddress, selection.OutcomeToken)
			if err != nil {
				return err
			}
			bidBlocks := collectClobSmokeBlocking(status, selection, bidPayload)
			askBlocks := collectClobSmokeBlocking(status, selection, askPayload)
			if settleErr := validateClobSettleableQuantity(bidPayload); settleErr != nil {
				bidBlocks = append(bidBlocks, settleErr.Error())
			}
			if settleErr := validateClobSettleableQuantity(askPayload); settleErr != nil {
				askBlocks = append(askBlocks, settleErr.Error())
			}
			if mustBoolFlag(cmd, "dry-run") {
				quoteReady := len(bidBlocks) == 0 && len(askBlocks) == 0
				return printOutput(ctx.JSON, map[string]any{
					"dryRun":        true,
					"market":        market.Title,
					"marketId":      market.ID,
					"outcomeLabel":  selection.LogicalOutcome.Label,
					"outcomeIndex":  selection.Binding.OutcomeIndex,
					"displayedSide": selection.DisplayedSide,
					"quoteReady":    quoteReady,
					"bid": map[string]any{
						"priceBps":       bidPriceBps,
						"quantity":       quantity,
						"makerAmount":    bidPayload.MakerAmount,
						"takerAmount":    bidPayload.TakerAmount,
						"expiration":     bidPayload.Expiration,
						"nonce":          bidPayload.Nonce,
						"tokenSide":      bidPayload.TokenSide,
						"outcomeTokenId": bidPayload.OutcomeTokenID,
						"blocking":       bidBlocks,
					},
					"ask": map[string]any{
						"priceBps":       askPriceBps,
						"quantity":       quantity,
						"makerAmount":    askPayload.MakerAmount,
						"takerAmount":    askPayload.TakerAmount,
						"expiration":     askPayload.Expiration,
						"nonce":          askPayload.Nonce,
						"tokenSide":      askPayload.TokenSide,
						"outcomeTokenId": askPayload.OutcomeTokenID,
						"blocking":       askBlocks,
					},
				})
			}

			approvals := make([]map[string]any, 0, 2)

			if len(bidBlocks) > 0 {
				approvalTxs, approveErr := ensureClobOrderApprovals(cmd.Context(), ctx.Config.EVMRPCURL, privateKeyHex, wallet.Address(), status, selection, bidPayload, true)
				if approveErr != nil {
					return fmt.Errorf("auto-approve bid quote prerequisites: %w", approveErr)
				}
				if len(approvalTxs) > 0 {
					approvals = append(approvals, approvalTxs...)
					status.CollateralAllowanceWei = clobMaxUint256
					approveAmount, parseErr := evm.ParseBigInt(clobMaxUint256)
					if parseErr != nil {
						return parseErr
					}
					status.CollateralAllowanceXRP = formatWeiToXRP(approveAmount)
					bidBlocks = collectClobSmokeBlockingAfterApprovals(status, selection, bidPayload)
				}
			}
			if len(askBlocks) > 0 {
				approvalTxs, approveErr := ensureClobOrderApprovals(cmd.Context(), ctx.Config.EVMRPCURL, privateKeyHex, wallet.Address(), status, selection, askPayload, true)
				if approveErr != nil {
					return fmt.Errorf("auto-approve ask quote prerequisites: %w", approveErr)
				}
				if len(approvalTxs) > 0 {
					approvals = append(approvals, approvalTxs...)
					status.OutcomeApprovalForAll = true
					askBlocks = collectClobSmokeBlockingAfterApprovals(status, selection, askPayload)
				}
			}
			quoteReady := len(bidBlocks) == 0 && len(askBlocks) == 0

			if !quoteReady {
				return buildMMQuoteReadinessError(bidBlocks, askBlocks)
			}

			eventstoreURL := strings.TrimSpace(mustStringFlag(cmd, "eventstore-url"))
			bidResponse, err := submitClobSmokeOrderWithRetry(cmd.Context(), ctx, eventstoreURL, bidPayload)
			if err != nil {
				return err
			}
			askResponse, err := submitClobSmokeOrderWithRetry(cmd.Context(), ctx, eventstoreURL, askPayload)
			if err != nil {
				rollbackRequest, rollbackBuildErr := buildSignedClobCancel(wallet, signingDomain, bidResponse.OrderID, market.ID, selection.Binding.OutcomeIndex, selection.DisplayedSide, wallet.Address().Hex(), "market-maker-quote-rollback")
				if rollbackBuildErr != nil {
					return fmt.Errorf("ask quote failed after bid order %s was placed: %w (rollback was not prepared: %v)", bidResponse.OrderID, err, rollbackBuildErr)
				}
				rollbackResponse, rollbackErr := ctx.API.CancelClobOrder(cmd.Context(), eventstoreURL, bidResponse.OrderID, rollbackRequest)
				if rollbackErr != nil {
					return fmt.Errorf("ask quote failed after bid order %s was placed: %w (rollback cancel failed: %v)", bidResponse.OrderID, err, rollbackErr)
				}
				return fmt.Errorf("ask quote failed after bid order %s was placed and rolled back with cancel %s: %w", bidResponse.OrderID, rollbackResponse.OrderID, err)
			}

			result := map[string]any{
				"market":        market.Title,
				"marketId":      market.ID,
				"outcomeLabel":  selection.LogicalOutcome.Label,
				"outcomeIndex":  selection.Binding.OutcomeIndex,
				"displayedSide": selection.DisplayedSide,
				"quantity":      quantity,
				"bid": map[string]any{
					"priceBps":        bidPriceBps,
					"orderId":         bidResponse.OrderID,
					"tradeCount":      bidResponse.TradeCount,
					"remainingShares": bidResponse.RemainingQuantity,
					"resting":         bidResponse.WasAddedToBook,
				},
				"ask": map[string]any{
					"priceBps":        askPriceBps,
					"orderId":         askResponse.OrderID,
					"tradeCount":      askResponse.TradeCount,
					"remainingShares": askResponse.RemainingQuantity,
					"resting":         askResponse.WasAddedToBook,
				},
				"message": "Two-sided quote resting on the hosted CLOB book.",
			}
			if len(approvals) > 0 {
				result["approvals"] = approvals
			}
			return printOutput(ctx.JSON, result)
		},
	}
	quoteCmd.Flags().String("outcome", "", "Logical outcome index to quote")
	quoteCmd.Flags().String("label", "", "Logical outcome label to quote")
	quoteCmd.Flags().String("displayed-side", "", "Displayed side to quote: yes or no; inferred for single-binding binary markets")
	quoteCmd.Flags().String("bid-price", "", "Bid price in displayed percent units, for example 45")
	quoteCmd.Flags().String("ask-price", "", "Ask price in displayed percent units, for example 55")
	quoteCmd.Flags().String("quantity", "", "Whole-number share quantity to post on both sides")
	quoteCmd.Flags().String("expiry", "24h", "Expiry preset for both resting quotes: 1h, 24h, 7d, never")
	quoteCmd.Flags().Bool("dry-run", false, "Build both signed quotes locally and report readiness without submitting them")
	quoteCmd.Flags().Bool("cancel-active", false, "Cancel existing resting orders on this book before placing new quotes")
	quoteCmd.Flags().String("cancel-reason", "market-maker-quote-replace", "Cancellation reason recorded when --cancel-active removes prior orders")
	quoteCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	cmd.AddCommand(quoteCmd)

	discoverCmd := &cobra.Command{
		Use:   "discover",
		Short: "Discover all active markets where the wallet has inventory or resting orders",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			wallet, err := requireEVMWallet(ctx)
			if err != nil {
				return err
			}
			walletAddr := wallet.Address().Hex()

			projectionURL := strings.TrimSpace(mustStringFlag(cmd, "projection-url"))
			filters := url.Values{}
			filters.Set("maker", walletAddr)
			filters.Set("active_only", "true")
			filters.Set("limit", "200")
			allOrders, err := ctx.API.ListClobOrders(cmd.Context(), projectionURL, filters)
			if err != nil {
				return fmt.Errorf("list active orders: %w", err)
			}

			type discoveredMarket struct {
				MarketID        string `json:"marketId"`
				ContractAddress string `json:"contractAddress"`
				ClobID          string `json:"clobId"`
				OutcomeLabel    string `json:"outcomeLabel"`
				TokenSide       string `json:"tokenSide"`
				OrderCount      int    `json:"orderCount"`
				HasInventory    bool   `json:"hasInventory"`
				Error           string `json:"error,omitempty"`
			}

			seen := make(map[string]*discoveredMarket)
			marketIDsFromOrders := make(map[string]struct{})
			for _, order := range allOrders {
				marketID, _, tokenSide, parseErr := parseClobOrderIdentity(order)
				if parseErr != nil {
					continue
				}
				marketIDsFromOrders[marketID] = struct{}{}
				key := marketID + "|" + tokenSide
				if existing, ok := seen[key]; ok {
					existing.OrderCount++
				} else {
					seen[key] = &discoveredMarket{
						MarketID:     marketID,
						ClobID:       order.ClobID,
						OutcomeLabel: "Yes",
						TokenSide:    tokenSide,
						OrderCount:   1,
					}
				}
			}

			includeInventory := mustBoolFlag(cmd, "inventory")
			if includeInventory {
				resolved, resolveErr := ctx.ConsoleAPI.ListAllMarkets(cmd.Context(), "open", "", "", "AxiomCTFMarket", false, 0)
				if resolveErr == nil {
					addrByID := make(map[string]string, len(resolved.Items))
					for _, item := range resolved.Items {
						if ca := strings.TrimSpace(item.ContractAddress); ca != "" {
							addrByID[strings.TrimSpace(item.ID)] = ca
							addrByID[strings.ToLower(strings.TrimSpace(item.ContractAddress))] = ca
						}
					}
					for _, m := range seen {
						if addr, ok := addrByID[m.MarketID]; ok {
							m.ContractAddress = addr
						} else if addr, ok := addrByID[strings.ToLower(m.MarketID)]; ok {
							m.ContractAddress = addr
						}
					}

					for _, item := range resolved.Items {
						itemMarketID := strings.TrimSpace(item.ID)
						if _, hasOrders := marketIDsFromOrders[itemMarketID]; hasOrders {
							continue
						}
						ca := strings.TrimSpace(item.ContractAddress)
						if ca == "" || !common.IsHexAddress(ca) {
							continue
						}

						market, loadErr := loadMMMarket(cmd.Context(), ctx, ca, "")
						if loadErr != nil {
							continue
						}

						legs, planErr := buildClobRedemptionPlan(cmd.Context(), ctx, market, wallet.Address())
						if planErr != nil || len(legs) == 0 {
							continue
						}

						key := itemMarketID + "|" + "yes"
						if _, exists := seen[key]; !exists {
							seen[key] = &discoveredMarket{
								MarketID:        itemMarketID,
								ContractAddress: ca,
								ClobID:          itemMarketID + "-0-yes",
								OutcomeLabel:    "Yes",
								TokenSide:       "yes",
								OrderCount:      0,
								HasInventory:    true,
							}
						}
					}
				}
			}

			markets := make([]discoveredMarket, 0, len(seen))
			for _, m := range seen {
				markets = append(markets, *m)
			}

			return printOutput(ctx.JSON, map[string]any{
				"walletAddress": walletAddr,
				"totalMarkets":  len(markets),
				"totalOrders":   len(allOrders),
				"markets":       markets,
			})
		},
	}
	discoverCmd.Flags().String("instance-date", "", "Instance date for recurring markets in YYYY-MM-DD format")
	discoverCmd.Flags().Bool("inventory", true, "Include contract address lookup for discovered markets")
	cmd.AddCommand(discoverCmd)

	return cmd
}

func resolveClobMarketFactoryAddress(cmdCtx context.Context, ctx *cliContext, override string, overrideChanged bool) (common.Address, error) {
	trimmedOverride := strings.TrimSpace(override)
	if overrideChanged {
		if !common.IsHexAddress(trimmedOverride) {
			return common.Address{}, errors.New("--factory-address must be a valid 0x-prefixed address")
		}
		return common.HexToAddress(trimmedOverride), nil
	}

	addresses, err := ctx.ConsoleAPI.GetMarketContractAddresses(cmdCtx, "xrpl-mainnet")
	if err != nil {
		return common.Address{}, fmt.Errorf("load canonical market factory address: %w", err)
	}
	if addresses == nil || !common.IsHexAddress(strings.TrimSpace(addresses.MarketFactory)) || isZeroAddress(addresses.MarketFactory) {
		return common.Address{}, errors.New("canonical MarketFactory address is unavailable for xrpl-mainnet; pass --factory-address explicitly")
	}
	return common.HexToAddress(addresses.MarketFactory), nil
}

func buildCLIContext() (*cliContext, error) {
	cfg, err := app.LoadConfig()
	if err != nil {
		return nil, err
	}
	if flagAPIURL != "" {
		cfg.APIBaseURL = flagAPIURL
	}
	if flagConsoleAPIURL != "" {
		cfg.ConsoleAPIBaseURL = flagConsoleAPIURL
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
	profileName := cfg.ActiveProfile
	profile, ok := cfg.Profiles[profileName]
	if !ok {
		profile = app.Profile{Name: profileName}
		cfg.SetCurrentProfile(profile)
	}
	client, err := api.NewClient(cfg.APIBaseURL, cfg.DeviceID)
	if err != nil {
		return nil, err
	}
	consoleClient, err := api.NewClient(cfg.ConsoleAPIBaseURL, cfg.DeviceID)
	if err != nil {
		return nil, err
	}
	repaired, err := hydrateProfileState(context.Background(), cfg, client, profileName, profile)
	if err != nil {
		return nil, err
	}
	if repaired.Name == "" {
		repaired.Name = profileName
	}
	if repaired != profile {
		profile = repaired
		cfg.SetCurrentProfile(profile)
		if err := app.SaveConfig(cfg); err != nil {
			return nil, err
		}
	}
	return &cliContext{Config: cfg, API: client, ConsoleAPI: consoleClient, Profile: profile, ProfileName: profileName, JSON: flagJSON || cfg.OutputFormat == "json"}, nil
}

func hydrateProfileState(ctx context.Context, cfg *app.Config, client *api.Client, profileName string, profile app.Profile) (app.Profile, error) {
	if profile.Name == "" {
		profile.Name = profileName
	}
	if profile.EVMAddress == "" {
		wallet, _, err := requireEVMWalletWithKeyForProfile(cfg, profileName)
		if err == nil {
			profile.EVMAddress = wallet.Address().Hex()
		}
	}
	if profile.XRPLAddress == "" {
		seed, err := app.LoadSecret(app.XRPLSecretKey(profileName))
		if err == nil {
			wallet, walletErr := axrpl.WalletFromSeed(seed)
			if walletErr != nil {
				return app.Profile{}, walletErr
			}
			profile.XRPLAddress = wallet.Address()
		}
	}
	if profile.DepositDestinationTag == 0 && profile.EVMAddress != "" {
		funding, err := client.GetFunding(ctx, profile.EVMAddress, 1)
		if err == nil && funding.DepositDestinationTag != nil && *funding.DepositDestinationTag > 0 {
			profile.DepositDestinationTag = *funding.DepositDestinationTag
		}
	}
	return profile, nil
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

func buildMMInventoryOutput(status *clobWalletStatus) (map[string]any, error) {
	if status == nil {
		return nil, errors.New("wallet status is required")
	}

	totalCompleteSets := big.NewInt(0)
	totalYes := big.NewInt(0)
	totalNo := big.NewInt(0)
	bindings := make([]map[string]any, 0, len(status.Bindings))
	for _, binding := range status.Bindings {
		yesBalance := big.NewInt(0)
		noBalance := big.NewInt(0)
		var yesTokenID string
		var noTokenID string
		for _, side := range binding.Sides {
			balance, err := evm.ParseBigInt(side.Balance)
			if err != nil {
				return nil, fmt.Errorf("parse %s balance for outcome %q: %w", side.DisplayedSide, binding.OutcomeLabel, err)
			}
			switch strings.ToLower(strings.TrimSpace(side.DisplayedSide)) {
			case "yes":
				yesBalance = balance
				yesTokenID = side.TokenID
			case "no":
				noBalance = balance
				noTokenID = side.TokenID
			}
		}

		completeSets := cloneBigInt(yesBalance)
		if noBalance.Cmp(completeSets) < 0 {
			completeSets = cloneBigInt(noBalance)
		}
		imbalance := new(big.Int).Sub(cloneBigInt(yesBalance), cloneBigInt(noBalance))
		bias := "balanced"
		if imbalance.Sign() > 0 {
			bias = "yes"
		} else if imbalance.Sign() < 0 {
			bias = "no"
		}

		totalCompleteSets.Add(totalCompleteSets, completeSets)
		totalYes.Add(totalYes, yesBalance)
		totalNo.Add(totalNo, noBalance)

		bindings = append(bindings, map[string]any{
			"outcomeIndex":      binding.OutcomeIndex,
			"outcomeLabel":      binding.OutcomeLabel,
			"contractAddress":   binding.ContractAddress,
			"conditionId":       binding.ConditionID,
			"questionId":        binding.QuestionID,
			"yesTokenId":        yesTokenID,
			"noTokenId":         noTokenID,
			"yesBalanceWei":     yesBalance.String(),
			"yesBalanceXrp":     formatWeiToXRP(yesBalance),
			"noBalanceWei":      noBalance.String(),
			"noBalanceXrp":      formatWeiToXRP(noBalance),
			"completeSetsWei":   completeSets.String(),
			"completeSetsXrp":   formatWeiToXRP(completeSets),
			"imbalanceWei":      imbalance.String(),
			"imbalanceXrp":      formatWeiToXRP(new(big.Int).Abs(cloneBigInt(imbalance))),
			"inventoryBias":     bias,
			"mergeReady":        completeSets.Sign() > 0,
			"outcomeTokenSides": binding.Sides,
		})
	}

	collateralBalance, err := evm.ParseBigInt(status.CollateralBalanceWei)
	if err != nil {
		return nil, fmt.Errorf("parse collateral balance: %w", err)
	}
	collateralAllowance, err := evm.ParseBigInt(status.CollateralAllowanceWei)
	if err != nil {
		return nil, fmt.Errorf("parse collateral allowance: %w", err)
	}
	totalImbalance := new(big.Int).Sub(cloneBigInt(totalYes), cloneBigInt(totalNo))
	totalBias := "balanced"
	if totalImbalance.Sign() > 0 {
		totalBias = "yes"
	} else if totalImbalance.Sign() < 0 {
		totalBias = "no"
	}

	return map[string]any{
		"walletAddress":         status.WalletAddress,
		"marketId":              status.MarketID,
		"marketTitle":           status.MarketTitle,
		"exchangeAddress":       status.ExchangeAddress,
		"outcomeToken":          status.OutcomeToken,
		"outcomeApprovalForAll": status.OutcomeApprovalForAll,
		"summary": map[string]any{
			"bindings":               len(status.Bindings),
			"collateralToken":        status.CollateralToken,
			"collateralBalanceWei":   collateralBalance.String(),
			"collateralBalanceXrp":   formatWeiToXRP(collateralBalance),
			"collateralAllowanceWei": collateralAllowance.String(),
			"collateralAllowanceXrp": formatWeiToXRP(collateralAllowance),
			"totalYesWei":            totalYes.String(),
			"totalYesXrp":            formatWeiToXRP(totalYes),
			"totalNoWei":             totalNo.String(),
			"totalNoXrp":             formatWeiToXRP(totalNo),
			"totalCompleteSetsWei":   totalCompleteSets.String(),
			"totalCompleteSetsXrp":   formatWeiToXRP(totalCompleteSets),
			"inventoryBias":          totalBias,
			"imbalanceWei":           totalImbalance.String(),
			"imbalanceXrp":           formatWeiToXRP(new(big.Int).Abs(cloneBigInt(totalImbalance))),
		},
		"bindings": bindings,
	}, nil
}

func activeMMMarketSelection(ctx *cliContext) *mmMarketSelection {
	if ctx == nil {
		return nil
	}
	state, err := app.LoadMMState()
	if err != nil {
		return nil
	}
	account := state.Account(ctx.ProfileName)
	if strings.TrimSpace(account.ActiveMarketID) == "" {
		return nil
	}
	return &mmMarketSelection{
		MarketID:     strings.TrimSpace(account.ActiveMarketID),
		MarketTitle:  strings.TrimSpace(account.ActiveMarketTitle),
		InstanceDate: strings.TrimSpace(account.ActiveInstanceDate),
	}
}

func saveActiveMMMarket(profileName string, selection mmMarketSelection) error {
	state, err := app.LoadMMState()
	if err != nil {
		return err
	}
	state.SetAccount(profileName, app.MMAccountState{
		ActiveMarketID:     selection.MarketID,
		ActiveMarketTitle:  selection.MarketTitle,
		ActiveInstanceDate: selection.InstanceDate,
	})
	return app.SaveMMState(state)
}

func clearActiveMMMarket(profileName string) error {
	state, err := app.LoadMMState()
	if err != nil {
		return err
	}
	state.SetAccount(profileName, app.MMAccountState{})
	return app.SaveMMState(state)
}

func resolveMMMarketReference(ctx *cliContext, args []string, instanceDateFlag string) (string, string, error) {
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		return strings.TrimSpace(args[0]), strings.TrimSpace(instanceDateFlag), nil
	}
	active := activeMMMarketSelection(ctx)
	if active == nil {
		return "", "", errors.New("no market provided and no active market-maker market is set; run `axiom mm market use`")
	}
	instanceDate := strings.TrimSpace(instanceDateFlag)
	if instanceDate == "" {
		instanceDate = active.InstanceDate
	}
	return active.MarketID, instanceDate, nil
}

func buildMMMarketSelection(market *api.MarketDetails, instanceDate string) mmMarketSelection {
	selection := mmMarketSelection{InstanceDate: strings.TrimSpace(instanceDate)}
	if market == nil {
		return selection
	}
	selection.MarketID = strings.TrimSpace(market.ID)
	selection.MarketTitle = strings.TrimSpace(market.Title)
	selection.ContractAddr = strings.TrimSpace(market.ContractAddress)
	selection.Category = strings.TrimSpace(market.Category)
	selection.Status = strings.TrimSpace(market.Status)
	selection.MarketType = strings.TrimSpace(market.MarketType)
	selection.Implementation = strings.TrimSpace(market.MarketImplementation)
	if !market.EndsAt.IsZero() {
		selection.EndsAt = market.EndsAt.UTC().Format(time.RFC3339)
	}
	if selection.InstanceDate == "" && market.InstanceDate != nil && !market.InstanceDate.IsZero() {
		selection.InstanceDate = market.InstanceDate.UTC().Format("2006-01-02")
	}
	return selection
}

func filterMMMarkets(response *api.MarketsResponse, category string) *api.MarketsResponse {
	if response == nil {
		return &api.MarketsResponse{}
	}
	filtered := make([]api.MarketListItem, 0, len(response.Items))
	for _, item := range response.Items {
		if !isClobMarketImplementation(item.MarketImplementation) {
			continue
		}
		if strings.TrimSpace(category) != "" && !strings.EqualFold(strings.TrimSpace(item.Category), strings.TrimSpace(category)) {
			continue
		}
		filtered = append(filtered, item)
	}
	sort.Slice(filtered, func(left int, right int) bool {
		leftVisible := filtered[left].IsVisible == nil || *filtered[left].IsVisible
		rightVisible := filtered[right].IsVisible == nil || *filtered[right].IsVisible
		if leftVisible != rightVisible {
			return leftVisible
		}
		return strings.ToLower(strings.TrimSpace(filtered[left].Title)) < strings.ToLower(strings.TrimSpace(filtered[right].Title))
	})
	return &api.MarketsResponse{Items: filtered, Total: len(filtered), Limit: len(filtered), Offset: 0}
}

func buildMMMarketListItems(items []api.MarketListItem) []map[string]any {
	results := make([]map[string]any, 0, len(items))
	for _, item := range items {
		entry := map[string]any{
			"marketId":              item.ID,
			"title":                 item.Title,
			"headline":              item.Headline,
			"category":              item.Category,
			"status":                item.Status,
			"marketType":            item.MarketType,
			"marketImplementation":  item.MarketImplementation,
			"contractAddress":       item.ContractAddress,
			"outcomes":              item.Outcomes,
			"ctfOutcomeMarketCount": len(item.CTFOutcomeMarkets),
		}
		if item.IsVisible != nil {
			entry["isVisible"] = *item.IsVisible
		}
		if item.InstanceDate != nil && !item.InstanceDate.IsZero() {
			entry["instanceDate"] = item.InstanceDate.UTC().Format("2006-01-02")
		}
		if !item.EndsAt.IsZero() {
			entry["endsAt"] = item.EndsAt.UTC().Format(time.RFC3339)
		}
		results = append(results, entry)
	}
	return results
}

func selectMMMarket(ctx context.Context, cliCtx *cliContext, cmd *cobra.Command, marketRef string, instanceDate string) (*api.MarketDetails, error) {
	trimmedRef := strings.TrimSpace(marketRef)
	if trimmedRef != "" {
		return loadMMMarket(ctx, cliCtx, trimmedRef, instanceDate)
	}
	search := strings.TrimSpace(mustStringFlag(cmd, "search"))
	status := strings.TrimSpace(mustStringFlag(cmd, "status"))
	category := strings.TrimSpace(mustStringFlag(cmd, "category"))
	limit, err := cmd.Flags().GetInt("limit")
	if err != nil {
		return nil, err
	}
	marketsResponse, err := withLoadingIndicator(cliCtx.JSON, "Loading hosted CLOB markets", func() (*api.MarketsResponse, error) {
		return cliCtx.ConsoleAPI.ListAllMarkets(ctx, status, search, "", normalizeMarketImplementation("clob"), true, 0)
	})
	if err != nil {
		return nil, err
	}
	filtered := filterMMMarkets(marketsResponse, category)
	filtered = paginateMarkets(filtered, limit, 0)
	if len(filtered.Items) == 0 {
		return nil, errors.New("no hosted CLOB markets matched the requested filters")
	}
	choice, err := chooseInteractiveMMMarket(filtered.Items)
	if err != nil {
		return nil, err
	}
	resolvedInstanceDate := strings.TrimSpace(instanceDate)
	if resolvedInstanceDate == "" {
		for _, item := range filtered.Items {
			if item.ID == choice.Value && item.InstanceDate != nil && !item.InstanceDate.IsZero() {
				resolvedInstanceDate = item.InstanceDate.UTC().Format("2006-01-02")
				break
			}
		}
	}
	return loadMMMarket(ctx, cliCtx, choice.Value, resolvedInstanceDate)
}

func chooseInteractiveMMMarket(items []api.MarketListItem) (*mmInteractiveChoice, error) {
	choices := make([]mmInteractiveChoice, 0, len(items))
	for index, item := range items {
		parts := []string{fmt.Sprintf("%d.", index+1), item.Title, fmt.Sprintf("[%s]", firstNonEmpty(strings.TrimSpace(item.Category), "uncategorized"))}
		if item.IsVisible != nil && !*item.IsVisible {
			parts = append(parts, "[hidden]")
		}
		if !item.EndsAt.IsZero() {
			parts = append(parts, item.EndsAt.UTC().Format("2006-01-02 15:04 UTC"))
		}
		parts = append(parts, item.ID)
		choices = append(choices, mmInteractiveChoice{Label: strings.Join(parts, "  "), Value: item.ID})
	}
	return promptInteractiveChoice("Select market-maker market", choices)
}

func promptInteractiveChoice(prompt string, choices []mmInteractiveChoice) (*mmInteractiveChoice, error) {
	if len(choices) == 0 {
		return nil, errors.New("no choices available")
	}
	if strings.TrimSpace(prompt) != "" {
		if _, err := fmt.Fprintf(os.Stderr, "%s\n", strings.TrimSpace(prompt)); err != nil {
			return nil, fmt.Errorf("write prompt: %w", err)
		}
	}
	for _, choice := range choices {
		if _, err := fmt.Fprintf(os.Stderr, "%s\n", choice.Label); err != nil {
			return nil, fmt.Errorf("write choice: %w", err)
		}
	}
	if _, err := fmt.Fprint(os.Stderr, "Enter choice number: "); err != nil {
		return nil, fmt.Errorf("write choice prompt: %w", err)
	}
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, os.ErrClosed) {
		if strings.TrimSpace(line) == "" {
			return nil, fmt.Errorf("read choice: %w", err)
		}
	}
	selection := strings.TrimSpace(line)
	if selection == "" {
		return nil, errors.New("interactive selection cancelled")
	}
	index, err := strconv.Atoi(selection)
	if err != nil || index < 1 || index > len(choices) {
		return nil, fmt.Errorf("invalid selection %q", selection)
	}
	choice := choices[index-1]
	return &choice, nil
}

func withLoadingIndicator[T any](asJSON bool, message string, run func() (T, error)) (T, error) {
	var zero T
	trimmedMessage := strings.TrimSpace(message)
	if trimmedMessage == "" {
		return run()
	}

	if _, err := fmt.Fprintf(os.Stderr, "%s...", trimmedMessage); err != nil {
		return zero, fmt.Errorf("write loading message: %w", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		frames := []string{"|", "/", "-", "\\"}
		ticker := time.NewTicker(120 * time.Millisecond)
		defer ticker.Stop()
		index := 0
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_, _ = fmt.Fprintf(os.Stderr, "\r%s... %s", trimmedMessage, frames[index%len(frames)])
				index++
			}
		}
	}()

	result, err := run()
	close(stop)
	wg.Wait()
	_, _ = fmt.Fprintf(os.Stderr, "\r%s... done\n", trimmedMessage)
	return result, err
}

func collectMMMarketClobIDs(market *api.MarketDetails) []string {
	if market == nil {
		return nil
	}
	bindings := sortedClobBindings(market.CTFOutcomeMarkets)
	if len(bindings) == 1 && len(market.Outcomes) == 2 {
		return []string{
			clobIDForMarketOutcome(market.ID, bindings[0].OutcomeIndex, "yes"),
			clobIDForMarketOutcome(market.ID, bindings[0].OutcomeIndex, "no"),
		}
	}
	results := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		results = append(results, clobIDForMarketOutcome(market.ID, binding.OutcomeIndex, "yes"))
	}
	return results
}

func parseClobOrderIdentity(order api.ClobOrder) (string, int, string, error) {
	clobID := strings.TrimSpace(order.ClobID)
	if clobID == "" {
		return "", 0, "", errors.New("order is missing clob_id")
	}
	lastDash := strings.LastIndex(clobID, "-")
	if lastDash <= 0 || lastDash == len(clobID)-1 {
		return "", 0, "", fmt.Errorf("invalid clob_id %q", clobID)
	}
	tokenSide, err := normalizeClobTokenSide(clobID[lastDash+1:])
	if err != nil {
		return "", 0, "", fmt.Errorf("invalid clob_id %q: %w", clobID, err)
	}
	marketWithOutcome := clobID[:lastDash]
	secondDash := strings.LastIndex(marketWithOutcome, "-")
	if secondDash <= 0 || secondDash == len(marketWithOutcome)-1 {
		return "", 0, "", fmt.Errorf("invalid clob_id %q", clobID)
	}
	outcomeIndex, err := strconv.Atoi(marketWithOutcome[secondDash+1:])
	if err != nil {
		return "", 0, "", fmt.Errorf("parse clob outcome from %q: %w", clobID, err)
	}
	marketID := marketWithOutcome[:secondDash]
	if strings.TrimSpace(marketID) == "" {
		return "", 0, "", fmt.Errorf("invalid clob_id %q", clobID)
	}
	return marketID, outcomeIndex, tokenSide, nil
}

func buildMMQuoteReadinessError(bidBlocks []string, askBlocks []string) error {
	issues := make([]string, 0, len(bidBlocks)+len(askBlocks))
	for _, block := range bidBlocks {
		issues = append(issues, fmt.Sprintf("bid: %s", block))
	}
	for _, block := range askBlocks {
		issues = append(issues, fmt.Sprintf("ask: %s", block))
	}
	if len(issues) == 0 {
		return errors.New("quote is not ready")
	}
	return fmt.Errorf("quote is not ready: %s", strings.Join(issues, "; "))
}

func runClobSplit(cmd *cobra.Command, ctx *cliContext, market *api.MarketDetails) error {
	wallet, privateKeyHex, err := requireEVMWalletWithKey(ctx)
	if err != nil {
		return err
	}
	binding, err := resolveSplitMergeBinding(market, mustStringFlag(cmd, "label"))
	if err != nil {
		return err
	}
	conditionalTokens := resolveHexAddressOrDefault(mustStringFlag(cmd, "outcome-token-address"), evm.DefaultClobConditionalTokens)
	collateralToken := resolveClobCollateralToken(market)
	conditionID := common.HexToHash(binding.ConditionID)
	partition := []*big.Int{big.NewInt(1), big.NewInt(2)}
	amount, err := parseClobAmount(mustStringFlag(cmd, "amount"))
	if err != nil {
		return err
	}

	collateralBalance, err := getERC20Balance(cmd.Context(), ctx.Config.EVMRPCURL, collateralToken, wallet.Address())
	if err != nil {
		return fmt.Errorf("check collateral balance: %w", err)
	}
	if collateralBalance.Cmp(amount) < 0 {
		return fmt.Errorf("insufficient collateral: balance %s wei (%s XRP) is below split amount %s wei (%s XRP)",
			collateralBalance.String(), formatWeiToXRP(collateralBalance),
			amount.String(), formatWeiToXRP(amount))
	}

	yesTokenID, _, _ := resolveDisplayedTokenID(binding, "yes", collateralToken)
	noTokenID, _, _ := resolveDisplayedTokenID(binding, "no", collateralToken)
	action := "split"
	if cmd != nil && cmd.CommandPath() == "axiom mm mint" {
		action = "mint"
	}

	if mustBoolFlag(cmd, "dry-run") {
		currentAllowance, _ := getERC20Allowance(cmd.Context(), ctx.Config.EVMRPCURL, collateralToken, wallet.Address(), conditionalTokens)
		needsApproval := currentAllowance == nil || currentAllowance.Cmp(amount) < 0
		preview := map[string]any{
			"dryRun":               true,
			"action":               action,
			"market":               market.Title,
			"marketId":             market.ID,
			"outcomeLabel":         binding.Label,
			"conditionalTokens":    conditionalTokens.Hex(),
			"collateralToken":      collateralToken.Hex(),
			"conditionId":          conditionID.Hex(),
			"partition":            []string{"1", "2"},
			"amountWei":            amount.String(),
			"amountXrp":            formatWeiToXRP(amount),
			"wallet":               wallet.Address().Hex(),
			"collateralBalanceWei": collateralBalance.String(),
			"collateralBalanceXrp": formatWeiToXRP(collateralBalance),
			"needsApproval":        needsApproval,
		}
		if yesTokenID != nil {
			preview["yesTokenId"] = yesTokenID.String()
		}
		if noTokenID != nil {
			preview["noTokenId"] = noTokenID.String()
		}
		return printOutput(ctx.JSON, preview)
	}

	approvalTxs := make([]map[string]any, 0, 1)
	if !mustBoolFlag(cmd, "skip-approval") {
		currentAllowance, err := getERC20Allowance(cmd.Context(), ctx.Config.EVMRPCURL, collateralToken, wallet.Address(), conditionalTokens)
		if err != nil {
			return fmt.Errorf("check collateral allowance: %w", err)
		}
		if currentAllowance.Cmp(amount) < 0 {
			approveMax, _ := evm.ParseBigInt(clobMaxUint256)
			approveTxHash, err := approveERC20(cmd.Context(), ctx.Config.EVMRPCURL, big.NewInt(xrplEVMChainID), privateKeyHex, collateralToken, conditionalTokens, approveMax)
			if err != nil {
				return fmt.Errorf("collateral approval failed: %w", err)
			}
			entry := map[string]any{
				"kind":    "collateral-approve-for-split",
				"token":   collateralToken.Hex(),
				"spender": conditionalTokens.Hex(),
				"txHash":  approveTxHash.Hex(),
			}
			receipt, waitErr := waitForReceipt(cmd.Context(), ctx.Config.EVMRPCURL, approveTxHash)
			if waitErr != nil {
				return fmt.Errorf("wait for approval receipt: %w", waitErr)
			}
			entry["receiptStatus"] = receipt.Status
			if receipt.Status == 0 {
				return fmt.Errorf("collateral approval reverted (tx %s)", approveTxHash.Hex())
			}
			approvalTxs = append(approvalTxs, entry)
		}
	}

	txHash, err := splitPosition(cmd.Context(), ctx.Config.EVMRPCURL, big.NewInt(xrplEVMChainID), privateKeyHex, conditionalTokens, collateralToken, conditionID, partition, amount)
	if err != nil {
		return fmt.Errorf("split transaction failed: %w", err)
	}
	result := map[string]any{
		"action":       action,
		"market":       market.Title,
		"marketId":     market.ID,
		"outcomeLabel": binding.Label,
		"amountWei":    amount.String(),
		"amountXrp":    formatWeiToXRP(amount),
		"txHash":       txHash.Hex(),
		"wallet":       wallet.Address().Hex(),
	}
	if yesTokenID != nil {
		result["yesTokenId"] = yesTokenID.String()
	}
	if noTokenID != nil {
		result["noTokenId"] = noTokenID.String()
	}
	if len(approvalTxs) > 0 {
		result["approvals"] = approvalTxs
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

func parsePayoutNumerators(value string) ([]*big.Int, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, errors.New("--payouts is required")
	}
	parts := strings.Split(trimmed, ",")
	payouts := make([]*big.Int, 0, len(parts))
	hasPositive := false
	for _, part := range parts {
		numerator, err := evm.ParseBigInt(strings.TrimSpace(part))
		if err != nil {
			return nil, fmt.Errorf("invalid --payouts value %q: %w", strings.TrimSpace(part), err)
		}
		if numerator.Sign() < 0 {
			return nil, errors.New("--payouts values must be non-negative integers")
		}
		if numerator.Sign() > 0 {
			hasPositive = true
		}
		payouts = append(payouts, numerator)
	}
	if len(payouts) < 2 {
		return nil, errors.New("--payouts must contain at least two values")
	}
	if !hasPositive {
		return nil, errors.New("--payouts must contain at least one positive numerator")
	}
	return payouts, nil
}

func bigIntSliceToStrings(values []*big.Int) []string {
	formatted := make([]string, 0, len(values))
	for _, value := range values {
		if value == nil {
			formatted = append(formatted, "0")
			continue
		}
		formatted = append(formatted, value.String())
	}
	return formatted
}

type clobOutcomeMetadata struct {
	Index       int    `json:"index"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type clobMarketMetadata struct {
	Name               string                `json:"name"`
	Headline           string                `json:"headline,omitempty"`
	Description        string                `json:"description"`
	Category           string                `json:"category"`
	Tags               []string              `json:"tags"`
	Outcomes           []clobOutcomeMetadata `json:"outcomes"`
	ResolutionCriteria string                `json:"resolutionCriteria"`
	EvidenceSources    []string              `json:"evidenceSources,omitempty"`
	Image              string                `json:"image,omitempty"`
	CreatedAt          string                `json:"createdAt"`
	EndsAt             string                `json:"endsAt"`
	OutcomeCount       int                   `json:"outcomeCount"`
}

type clobMarketMetadataInput struct {
	Name               string
	Headline           string
	Description        string
	Category           string
	Tags               []string
	YesLabel           string
	YesDescription     string
	NoLabel            string
	NoDescription      string
	ResolutionCriteria string
	EvidenceSources    []string
	Image              string
	EndsAt             time.Time
}

func buildClobMarketMetadata(input clobMarketMetadataInput) clobMarketMetadata {
	compactStrings := func(values []string) []string {
		if len(values) == 0 {
			return nil
		}
		result := make([]string, 0, len(values))
		for _, value := range values {
			trimmed := strings.TrimSpace(value)
			if trimmed != "" {
				result = append(result, trimmed)
			}
		}
		if len(result) == 0 {
			return nil
		}
		return result
	}

	compactedTags := compactStrings(input.Tags)
	if compactedTags == nil {
		compactedTags = []string{}
	}

	return clobMarketMetadata{
		Name:        strings.TrimSpace(input.Name),
		Headline:    strings.TrimSpace(input.Headline),
		Description: strings.TrimSpace(input.Description),
		Category:    strings.TrimSpace(input.Category),
		Tags:        compactedTags,
		Outcomes: []clobOutcomeMetadata{
			{
				Index:       0,
				Label:       strings.TrimSpace(input.YesLabel),
				Description: strings.TrimSpace(input.YesDescription),
			},
			{
				Index:       1,
				Label:       strings.TrimSpace(input.NoLabel),
				Description: strings.TrimSpace(input.NoDescription),
			},
		},
		ResolutionCriteria: strings.TrimSpace(input.ResolutionCriteria),
		EvidenceSources:    compactStrings(input.EvidenceSources),
		Image:              strings.TrimSpace(input.Image),
		CreatedAt:          time.Now().UTC().Format(time.RFC3339),
		EndsAt:             input.EndsAt.UTC().Format(time.RFC3339),
		OutcomeCount:       2,
	}
}

func getIPFSURI(cid string) string {
	trimmed := strings.TrimSpace(cid)
	if strings.HasPrefix(trimmed, "ipfs://") {
		return trimmed
	}
	return "ipfs://" + trimmed
}

func marshalJSONForSignedMessage(value any) []byte {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return []byte("null")
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n"))
}

func buildClobMetadataUploadMessage(walletAddress string, network string, metadata api.MarketMetadata) string {
	encoded := marshalJSONForSignedMessage(metadata)
	hash := sha256.Sum256(encoded)
	hashHex := fmt.Sprintf("%x", hash)

	return strings.Join([]string{
		"Axiom CLI CLOB metadata upload",
		"",
		"Version: 1",
		fmt.Sprintf("Wallet: %s", walletAddress),
		fmt.Sprintf("Network: %s", strings.TrimSpace(network)),
		fmt.Sprintf("Metadata SHA-256: %s", hashHex),
		fmt.Sprintf("Metadata Name: %s", metadata.Name),
		fmt.Sprintf("Ends At: %s", metadata.EndsAt),
		"Purpose: authorize server-side IPFS upload for AxiomCTFMarket metadata.",
	}, "\n")
}

func uploadClobMarketMetadata(ctx context.Context, cliCtx *cliContext, wallet *evm.Wallet, network string, metadata clobMarketMetadata) (*api.UploadMetadataResponse, error) {
	apiMetadata := api.MarketMetadata{
		Name:               metadata.Name,
		Headline:           metadata.Headline,
		Description:        metadata.Description,
		Category:           metadata.Category,
		Tags:               metadata.Tags,
		Outcomes:           make([]api.OutcomeMetadata, 0, len(metadata.Outcomes)),
		ResolutionCriteria: metadata.ResolutionCriteria,
		EvidenceSources:    metadata.EvidenceSources,
		Image:              metadata.Image,
		CreatedAt:          metadata.CreatedAt,
		EndsAt:             metadata.EndsAt,
		OutcomeCount:       metadata.OutcomeCount,
	}
	for _, outcome := range metadata.Outcomes {
		apiMetadata.Outcomes = append(apiMetadata.Outcomes, api.OutcomeMetadata{
			Index:       outcome.Index,
			Label:       outcome.Label,
			Description: outcome.Description,
		})
	}

	message := buildClobMetadataUploadMessage(wallet.Address().Hex(), network, apiMetadata)
	signature, err := wallet.SignMessage(message)
	if err != nil {
		return nil, err
	}

	return cliCtx.ConsoleAPI.UploadMarketMetadata(ctx, api.UploadMetadataRequest{
		Network:       strings.TrimSpace(network),
		WalletAddress: wallet.Address().Hex(),
		Metadata:      apiMetadata,
		Message:       message,
		Signature:     signature,
	})
}

func resolveClobMarketMetadata(cmdCtx context.Context, cliCtx *cliContext, wallet *evm.Wallet, cmd *cobra.Command, endsAt time.Time) (string, *clobMarketMetadata, error) {
	metadataURI := strings.TrimSpace(mustStringFlag(cmd, "metadata-uri"))
	if metadataURI != "" {
		return metadataURI, nil, nil
	}

	name := strings.TrimSpace(mustStringFlag(cmd, "name"))
	description := strings.TrimSpace(mustStringFlag(cmd, "description"))
	category := strings.TrimSpace(mustStringFlag(cmd, "category"))
	resolutionCriteria := strings.TrimSpace(mustStringFlag(cmd, "resolution-criteria"))
	if name == "" {
		return "", nil, errors.New("either --metadata-uri or --name is required")
	}
	if description == "" {
		return "", nil, errors.New("--description is required when --metadata-uri is omitted")
	}
	if category == "" {
		return "", nil, errors.New("--category is required when --metadata-uri is omitted")
	}
	if resolutionCriteria == "" {
		return "", nil, errors.New("--resolution-criteria is required when --metadata-uri is omitted")
	}

	tags, _ := cmd.Flags().GetStringSlice("tag")
	evidenceSources, _ := cmd.Flags().GetStringSlice("evidence-source")
	metadata := buildClobMarketMetadata(clobMarketMetadataInput{
		Name:               name,
		Headline:           strings.TrimSpace(mustStringFlag(cmd, "headline")),
		Description:        description,
		Category:           category,
		Tags:               tags,
		YesLabel:           strings.TrimSpace(mustStringFlag(cmd, "yes-label")),
		YesDescription:     strings.TrimSpace(mustStringFlag(cmd, "yes-description")),
		NoLabel:            strings.TrimSpace(mustStringFlag(cmd, "no-label")),
		NoDescription:      strings.TrimSpace(mustStringFlag(cmd, "no-description")),
		ResolutionCriteria: resolutionCriteria,
		EvidenceSources:    evidenceSources,
		Image:              strings.TrimSpace(mustStringFlag(cmd, "image")),
		EndsAt:             endsAt,
	})

	uploadResponse, err := uploadClobMarketMetadata(cmdCtx, cliCtx, wallet, "xrpl-mainnet", metadata)
	if err != nil {
		return "", nil, err
	}
	metadataURI = getIPFSURI(uploadResponse.IPFSURI)
	return metadataURI, &metadata, nil
}

func resolveUnixTimestampFlag(cmd *cobra.Command, flagName string) (uint64, error) {
	value, err := cmd.Flags().GetUint64(flagName)
	if err != nil {
		return 0, err
	}
	if value == 0 {
		return 0, fmt.Errorf("--%s is required", flagName)
	}
	return value, nil
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
