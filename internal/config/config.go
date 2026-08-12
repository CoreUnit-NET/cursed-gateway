package config

/*
Package config owns raw CLI flag and environment parsing (cobra).

It maps flags/env into a plain AppConfig struct only — no validation
beyond parse. Convert AppConfig into validated Settings via
internal/settings. Flags override env; missing .env is ignored at main.
*/

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const helpURL = "https://github.com/CoreUnit-NET/cursed-gateway"

// Known subcommand names captured in AppConfig.Command after ParseConfig.
const (
	CommandVersion  = "version"
	CommandLogin    = "login"
	CommandLogout   = "logout"
	CommandSessions = "sessions"
	CommandWhoami   = "whoami"
	CommandModels   = "models"
	CommandServe    = "serve"
	CommandImport   = "import"
)

type AppConfig struct {
	Verbose     bool
	ShowVersion bool

	// Command is the selected subcommand name (see Command* constants).
	// Bare root defaults to CommandServe.
	Command string
	// Args are positional args passed to the selected subcommand.
	Args []string

	Host         string
	Port         int
	AuthPath     string
	MaxRetries   int
	CooldownMins int
	PreferPro    bool
	LogFormat    string
	// EnableLogin registers GET /login (307 to Cursor Deep Control) on serve.
	EnableLogin bool

	// SessionsCheck is set by `sessions --check`.
	SessionsCheck bool
	// ImportPath is the Cursor-style auth.json path for `import` (default ./data/auth.json).
	ImportPath string
}

func defaultAppConfig() *AppConfig {
	return &AppConfig{
		Verbose:     false,
		ShowVersion: false,

		Host:         "0.0.0.0",
		Port:         8080,
		AuthPath:     "./data/data.json",
		MaxRetries:   5,
		CooldownMins: 15,
		PreferPro:    true,
		LogFormat:    "text",
		EnableLogin:  false,

		ImportPath: "./data/auth.json",
	}
}

func versionCommand(appConfig *AppConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			appConfig.ShowVersion = true
			appConfig.Command = CommandVersion
			appConfig.Args = args
		},
	}
}

func loginCommand(appConfig *AppConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Run Cursor OAuth PKCE login and store the session",
		Run: func(cmd *cobra.Command, args []string) {
			appConfig.Command = CommandLogin
			appConfig.Args = args
		},
	}
}

func logoutCommand(appConfig *AppConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "logout [session-id]",
		Short: "Remove one or more sessions from the auth store",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			appConfig.Command = CommandLogout
			appConfig.Args = args
		},
	}
}

func sessionsCommand(appConfig *AppConfig) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List stored auth sessions",
		Run: func(cmd *cobra.Command, args []string) {
			appConfig.Command = CommandSessions
			appConfig.Args = args
		},
	}
	cmd.Flags().BoolVar(&appConfig.SessionsCheck, "check", false, "validate sessions against Cursor")
	return cmd
}

func whoamiCommand(appConfig *AppConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show local session identity metadata",
		Run: func(cmd *cobra.Command, args []string) {
			appConfig.Command = CommandWhoami
			appConfig.Args = args
		},
	}
}

func modelsCommand(appConfig *AppConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "models",
		Short: "Fetch and print available Cursor models",
		Run: func(cmd *cobra.Command, args []string) {
			appConfig.Command = CommandModels
			appConfig.Args = args
		},
	}
}

func serveCommand(appConfig *AppConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the OpenAI-compatible proxy HTTP server",
		Run: func(cmd *cobra.Command, args []string) {
			appConfig.Command = CommandServe
			appConfig.Args = args
		},
	}
}

func importCommand(appConfig *AppConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "import [auth.json]",
		Short: "Import Cursor-style auth.json into the gateway session store",
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			appConfig.Command = CommandImport
			appConfig.Args = args
			if len(args) > 0 {
				appConfig.ImportPath = args[0]
			}
		},
	}
}

func loadEnvVars(appConfig *AppConfig) error {
	if err := envIsString("HOST", func(value string) {
		appConfig.Host = value
	}); err != nil {
		return err
	}
	if err := envIsInt("PORT", func(value int) {
		appConfig.Port = value
	}); err != nil {
		return err
	}
	if err := envIsString("AUTH_PATH", func(value string) {
		appConfig.AuthPath = value
	}); err != nil {
		return err
	}
	if err := envIsInt("MAX_RETRIES", func(value int) {
		appConfig.MaxRetries = value
	}); err != nil {
		return err
	}
	if err := envIsInt("COOLDOWN_MINS", func(value int) {
		appConfig.CooldownMins = value
	}); err != nil {
		return err
	}
	if err := envIsBool("PREFER_PRO", func(value bool) {
		appConfig.PreferPro = value
	}); err != nil {
		return err
	}
	if err := envIsBool("VERBOSE", func(value bool) {
		appConfig.Verbose = value
	}); err != nil {
		return err
	}
	if err := envIsString("LOG_FORMAT", func(value string) {
		appConfig.LogFormat = value
	}); err != nil {
		return err
	}
	if err := envIsBool("ENABLE_LOGIN", func(value bool) {
		appConfig.EnableLogin = value
	}); err != nil {
		return err
	}
	return nil
}

func applyServeFlags(appConfig *AppConfig, cmd *cobra.Command) {
	// Persistent so bare root and subcommands share one definition (explorer-mcp / caddy-forward-auth style).
	// --host has no -h shorthand (reserved for cobra help).
	cmd.PersistentFlags().StringVar(&appConfig.Host, "host", appConfig.Host, "bind host (HOST)")
	cmd.PersistentFlags().IntVarP(&appConfig.Port, "port", "p", appConfig.Port, "bind port (PORT)")
	cmd.PersistentFlags().StringVarP(&appConfig.AuthPath, "auth", "a", appConfig.AuthPath, "gateway multi-account session store path (AUTH_PATH); not Cursor auth.json")
	cmd.PersistentFlags().IntVarP(&appConfig.MaxRetries, "retries", "r", appConfig.MaxRetries, "max account fallback attempts per request (MAX_RETRIES)")
	cmd.PersistentFlags().IntVarP(&appConfig.CooldownMins, "cooldown", "c", appConfig.CooldownMins, "cooldown minutes for rate-limited accounts (COOLDOWN_MINS)")
	cmd.PersistentFlags().BoolVar(&appConfig.PreferPro, "prefer-pro", appConfig.PreferPro, "prefer Pro accounts over Free (PREFER_PRO)")
	cmd.PersistentFlags().StringVar(&appConfig.LogFormat, "log-format", appConfig.LogFormat, "log format: text or json (LOG_FORMAT)")
	cmd.PersistentFlags().BoolVar(&appConfig.EnableLogin, "enable-login", appConfig.EnableLogin, "opt-in: expose GET /login as 307 redirect to Cursor OAuth (ENABLE_LOGIN)")
}

// ParseConfig loads env defaults, parses CLI flags/subcommands, and returns the app config.
// It returns ErrHelpRequested when the user asked for help (cobra has already printed it).
// Callers should handle ShowVersion and process exit themselves.
func ParseConfig(displayName, shortName string) (*AppConfig, error) {
	appConfig := defaultAppConfig()

	short := displayName + " is a Cursor API proxy gateway with an OpenAI-compatible HTTP API.\n" +
		"For more help, visit " + helpURL
	rootCmd := &cobra.Command{
		Use:   shortName,
		Short: short,
		Run: func(cmd *cobra.Command, args []string) {
			// Bare root with no subcommand runs serve.
			appConfig.Command = CommandServe
			appConfig.Args = args
		},
	}

	// Match sibling CLIs: -b/--verbose (VERBOSE); -v/--version plus `version` subcommand.
	// Verbose enables debug + trace; default logging is info/warn/error/fatal only.
	rootCmd.PersistentFlags().BoolVarP(&appConfig.Verbose, "verbose", "b", appConfig.Verbose, "enable debug and trace logs (VERBOSE)")
	rootCmd.Flags().BoolVarP(&appConfig.ShowVersion, "version", "v", appConfig.ShowVersion, "print version")

	applyServeFlags(appConfig, rootCmd)

	if err := loadEnvVars(appConfig); err != nil {
		return nil, err
	}

	rootCmd.AddCommand(
		versionCommand(appConfig),
		loginCommand(appConfig),
		importCommand(appConfig),
		logoutCommand(appConfig),
		sessionsCommand(appConfig),
		whoamiCommand(appConfig),
		modelsCommand(appConfig),
		serveCommand(appConfig),
	)

	// Disable cobra's help subcommand so `help` is not a valid command.
	rootCmd.SetHelpCommand(&cobra.Command{
		Use:    "",
		Hidden: true,
	})

	cmd, err := rootCmd.ExecuteC()
	if err != nil {
		return nil, err
	}

	if commandHelpRequested(cmd) {
		return nil, ErrHelpRequested
	}

	if appConfig.Verbose {
		fmt.Fprintln(os.Stderr, "Verbose mode enabled")
	}

	return appConfig, nil
}

func commandHelpRequested(cmd *cobra.Command) bool {
	for c := cmd; c != nil; c = c.Parent() {
		if helpFlag := c.Flags().Lookup("help"); helpFlag != nil && helpFlag.Changed {
			return true
		}
	}
	return false
}
