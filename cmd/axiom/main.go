package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
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
	"github.com/ethereum/go-ethereum/core/types"
	qrterminal "github.com/mdp/qrterminal/v3"
	"github.com/spf13/cobra"
)

const xrplEVMChainID int64 = 1440000

var (
	flagAPIURL          string
	flagRPCURL          string
	flagXRPLURL         string
	flagJSON            bool
	flagProfile         string
	getEVMBalance       = evm.GetBalance
	getXRPLBalance      = axrpl.GetBalance
	loadMarketState     = evm.LoadMarketState
	quoteBuy            = evm.QuoteBuy
	buyPosition         = evm.BuyPosition
	claimSingleMarket   = evm.ClaimMarket
	batchClaimMarkets   = evm.BatchClaim
	submitBridgePayment = axrpl.SubmitBridgePayment
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
	rootCmd.PersistentFlags().StringVar(&flagProfile, "profile", "", "Use a specific local profile")

	rootCmd.AddCommand(newConfigCommand())
	rootCmd.AddCommand(newWalletCommand())
	rootCmd.AddCommand(newAuthCommand())
	rootCmd.AddCommand(newMarketsCommand())
	rootCmd.AddCommand(newProfileCommand())
	rootCmd.AddCommand(newFundingCommand())
	rootCmd.AddCommand(newPredictCommand())
	rootCmd.AddCommand(newClaimCommand())

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

	cmd.AddCommand(&cobra.Command{
		Use:   "create",
		Short: "Create a new XRPL EVM wallet and store the private key in the OS keychain",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			wallet, privateKeyHex, err := evm.NewRandomWallet()
			if err != nil {
				return err
			}
			secretStore, err := app.SaveSecret(app.EVMSecretKey(ctx.ProfileName), privateKeyHex)
			if err != nil {
				return err
			}
			profile := ctx.Profile
			profile.EVMAddress = wallet.Address().Hex()
			ctx.Config.SetCurrentProfile(profile)
			if err := app.SaveConfig(ctx.Config); err != nil {
				return err
			}
			return printOutput(ctx.JSON, map[string]any{
				"profile":          ctx.ProfileName,
				"evmAddress":       wallet.Address().Hex(),
				"storedIn":         string(secretStore),
				"storedInKeychain": secretStore == app.SecretStoreKeychain,
				"nextStep":         "Run `axiom auth register` to get your Axiom destination tag.",
			})
		},
	})

	importCmd := &cobra.Command{
		Use:   "import",
		Short: "Import an existing XRPL EVM private key into the OS keychain",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			privateKey, _ := cmd.Flags().GetString("private-key")
			if strings.TrimSpace(privateKey) == "" {
				return errors.New("--private-key is required")
			}
			wallet, err := evm.WalletFromPrivateKeyHex(privateKey)
			if err != nil {
				return err
			}
			secretStore, err := app.SaveSecret(app.EVMSecretKey(ctx.ProfileName), wallet.PrivateKeyHex())
			if err != nil {
				return err
			}
			profile := ctx.Profile
			profile.EVMAddress = wallet.Address().Hex()
			ctx.Config.SetCurrentProfile(profile)
			if err := app.SaveConfig(ctx.Config); err != nil {
				return err
			}
			return printOutput(ctx.JSON, map[string]any{
				"profile":          ctx.ProfileName,
				"evmAddress":       wallet.Address().Hex(),
				"storedIn":         string(secretStore),
				"storedInKeychain": secretStore == app.SecretStoreKeychain,
			})
		},
	}
	importCmd.Flags().String("private-key", "", "Hex-encoded secp256k1 private key")
	cmd.AddCommand(importCmd)

	cmd.AddCommand(&cobra.Command{
		Use:   "xrpl-create",
		Short: "Create a native XRPL wallet for direct bridge funding submissions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			wallet, err := axrpl.NewRandomWallet()
			if err != nil {
				return err
			}
			secretStore, err := app.SaveSecret(app.XRPLSecretKey(ctx.ProfileName), wallet.Seed())
			if err != nil {
				return err
			}
			profile := ctx.Profile
			profile.XRPLAddress = wallet.Address()
			ctx.Config.SetCurrentProfile(profile)
			if err := app.SaveConfig(ctx.Config); err != nil {
				return err
			}
			return printOutput(ctx.JSON, map[string]any{
				"profile":          ctx.ProfileName,
				"xrplAddress":      wallet.Address(),
				"storedIn":         string(secretStore),
				"storedInKeychain": secretStore == app.SecretStoreKeychain,
			})
		},
	})

	importXRPLCmd := &cobra.Command{
		Use:   "xrpl-import",
		Short: "Import an XRPL seed into the OS keychain",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, err := buildCLIContext()
			if err != nil {
				return err
			}
			seed, _ := cmd.Flags().GetString("seed")
			if strings.TrimSpace(seed) == "" {
				return errors.New("--seed is required")
			}
			wallet, err := axrpl.WalletFromSeed(seed)
			if err != nil {
				return err
			}
			secretStore, err := app.SaveSecret(app.XRPLSecretKey(ctx.ProfileName), wallet.Seed())
			if err != nil {
				return err
			}
			profile := ctx.Profile
			profile.XRPLAddress = wallet.Address()
			ctx.Config.SetCurrentProfile(profile)
			if err := app.SaveConfig(ctx.Config); err != nil {
				return err
			}
			return printOutput(ctx.JSON, map[string]any{
				"profile":          ctx.ProfileName,
				"xrplAddress":      wallet.Address(),
				"storedIn":         string(secretStore),
				"storedInKeychain": secretStore == app.SecretStoreKeychain,
			})
		},
	}
	importXRPLCmd.Flags().String("seed", "", "XRPL family seed (s...) or compatible secret")
	cmd.AddCommand(importXRPLCmd)

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
			"Show local wallet balances for the active profile.",
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
	cmd.AddCommand(&cobra.Command{
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
			response, err := registerWalletWithCompat(cmd.Context(), ctx, wallet)
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
	})
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
			"  --status active|resolved|upcoming|all",
			"  --category hourly|sports|streak|...",
			"  --search <text>",
			"  --limit <n> (0 fetches all matching markets)",
			"  --offset <n>",
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
			limit, _ := cmd.Flags().GetInt("limit")
			offset, _ := cmd.Flags().GetInt("offset")
			myPositions, _ := cmd.Flags().GetBool("my-positions")
			var response *api.MarketsResponse
			needsLocalFiltering := strings.TrimSpace(category) != "" || myPositions
			if needsLocalFiltering || limit <= 0 {
				response, err = ctx.API.ListAllMarkets(cmd.Context(), status, search, "", 0)
			} else {
				response, err = ctx.API.ListMarkets(cmd.Context(), status, search, "", limit, offset)
			}
			if err != nil {
				return err
			}
			if strings.TrimSpace(category) != "" {
				response = filterMarketsByCategory(response, category, 0, 0)
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
			enrichMarketsWithSpotPrices(cmd.Context(), ctx, response)
			return printOutput(ctx.JSON, response)
		},
	}
	listCmd.Flags().String("status", "active", "Filter by status: active, resolved, upcoming, all")
	listCmd.Flags().String("category", "", "Filter by market category (for example hourly, sports, streak)")
	listCmd.Flags().String("search", "", "Search by title or headline")
	listCmd.Flags().Bool("my-positions", false, "Only return markets where the active wallet currently has open positions")
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
			response, err := ctx.API.GetMarket(cmd.Context(), args[0], instanceDate)
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
			market, err := ctx.API.GetMarket(cmd.Context(), args[0], mustStringFlag(cmd, "instance-date"))
			if err != nil {
				return err
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
			market, err := ctx.API.GetMarket(cmd.Context(), args[0], mustStringFlag(cmd, "instance-date"))
			if err != nil {
				return err
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
			_, privateKeyHex, err := requireEVMWalletWithKey(ctx)
			if err != nil {
				return err
			}
			market, err := ctx.API.GetMarket(cmd.Context(), args[0], mustStringFlag(cmd, "instance-date"))
			if err != nil {
				return err
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

func requireEVMWallet(ctx *cliContext) (*evm.Wallet, error) {
	wallet, _, err := requireEVMWalletWithKey(ctx)
	return wallet, err
}

func requireEVMWalletWithKey(ctx *cliContext) (*evm.Wallet, string, error) {
	secret, err := app.LoadSecret(app.EVMSecretKey(ctx.ProfileName))
	if err != nil {
		return nil, "", fmt.Errorf("no EVM private key stored for profile %q: %w", ctx.ProfileName, err)
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

func formatRegistrationIssuedAt(issuedAt time.Time) string {
	return issuedAt.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
}

func registerWalletWithCompat(ctx context.Context, cliCtx *cliContext, wallet *evm.Wallet) (*api.RegisterResponse, error) {
	issuedAt := time.Now().UTC()
	walletAddress := wallet.Address().Hex()

	return signAndRegisterWallet(ctx, cliCtx, wallet, walletAddress, issuedAt)
}

func signAndRegisterWallet(ctx context.Context, cliCtx *cliContext, wallet *evm.Wallet, walletAddress string, issuedAt time.Time) (*api.RegisterResponse, error) {
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
