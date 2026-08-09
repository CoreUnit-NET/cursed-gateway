package config

/*
Package config owns raw CLI flag and environment parsing (cobra).

It maps flags/env into a plain AppConfig struct only — no validation
beyond parse. Convert AppConfig into validated Settings via
internal/settings. Flags override env; missing .env is ignored at main.
*/

import (
	"github.com/spf13/cobra"
)

const helpURL = "https://github.com/CoreUnit-NET/cursed-gateway"

type AppConfig struct {
	ShowVersion bool

	Host         string
	Port         int
	AuthPath     string
	MaxRetries   int
	CooldownMins int
	PreferPro    bool
	LogLevel     string
	LogFormat    string

	CacheDir       string
	ProtoOut       string
	ReleaseChannel string
}

func defaultAppConfig() *AppConfig {
	return &AppConfig{
		ShowVersion: false,

		Host:         "0.0.0.0",
		Port:         8080,
		AuthPath:     "./auth.json",
		MaxRetries:   5,
		CooldownMins: 15,
		PreferPro:    true,
		LogLevel:     "info",
		LogFormat:    "text",

		CacheDir:       "./.cache",
		ProtoOut:       "./pkg/generated",
		ReleaseChannel: "prod",
	}
}

func versionCommand(appConfig *AppConfig) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			appConfig.ShowVersion = true
		},
	}
}

func loginCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "login",
		Short: "Run Cursor OAuth PKCE login and store the session",
		Run:   func(cmd *cobra.Command, args []string) {},
	}
}

func logoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout [session-id]",
		Short: "Remove one or more sessions from the auth store",
		Args:  cobra.MaximumNArgs(1),
		Run:   func(cmd *cobra.Command, args []string) {},
	}
}

func sessionsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "sessions",
		Short: "List stored auth sessions",
		Run:   func(cmd *cobra.Command, args []string) {},
	}
}

func whoamiCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show local session identity metadata",
		Run:   func(cmd *cobra.Command, args []string) {},
	}
}

func modelsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "models",
		Short: "Fetch and print available Cursor models",
		Run:   func(cmd *cobra.Command, args []string) {},
	}
}

func serveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the OpenAI-compatible proxy HTTP server",
		Run:   func(cmd *cobra.Command, args []string) {},
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
	if err := envIsString("LOG_LEVEL", func(value string) {
		appConfig.LogLevel = value
	}); err != nil {
		return err
	}
	if err := envIsString("LOG_FORMAT", func(value string) {
		appConfig.LogFormat = value
	}); err != nil {
		return err
	}
	if err := envIsString("CACHE_DIR", func(value string) {
		appConfig.CacheDir = value
	}); err != nil {
		return err
	}
	if err := envIsString("PROTO_OUT", func(value string) {
		appConfig.ProtoOut = value
	}); err != nil {
		return err
	}
	if err := envIsString("RELEASE_CHANNEL", func(value string) {
		appConfig.ReleaseChannel = value
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
	cmd.PersistentFlags().StringVarP(&appConfig.AuthPath, "auth", "a", appConfig.AuthPath, "auth / multi-account state file (AUTH_PATH)")
	cmd.PersistentFlags().IntVarP(&appConfig.MaxRetries, "retries", "r", appConfig.MaxRetries, "max account fallback attempts per request (MAX_RETRIES)")
	cmd.PersistentFlags().IntVarP(&appConfig.CooldownMins, "cooldown", "c", appConfig.CooldownMins, "cooldown minutes for rate-limited accounts (COOLDOWN_MINS)")
	cmd.PersistentFlags().BoolVar(&appConfig.PreferPro, "prefer-pro", appConfig.PreferPro, "prefer Pro accounts over Free (PREFER_PRO)")
	cmd.PersistentFlags().StringVarP(&appConfig.LogLevel, "log-level", "l", appConfig.LogLevel, "log level: debug, info, warn, or error (LOG_LEVEL)")
	cmd.PersistentFlags().StringVar(&appConfig.LogFormat, "log-format", appConfig.LogFormat, "log format: text or json (LOG_FORMAT)")
}

func applyProtoFlags(appConfig *AppConfig, cmd *cobra.Command) {
	cmd.PersistentFlags().StringVar(&appConfig.CacheDir, "cache-dir", appConfig.CacheDir, "cache for Cursor agent binaries and proto artifacts (CACHE_DIR)")
	cmd.PersistentFlags().StringVar(&appConfig.ProtoOut, "proto-out", appConfig.ProtoOut, "generated Go protobuf output directory (PROTO_OUT)")
	cmd.PersistentFlags().StringVar(&appConfig.ReleaseChannel, "channel", appConfig.ReleaseChannel, "Cursor agent channel: prod, staging, experimental, or rc (RELEASE_CHANNEL)")
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
		Run:   func(cmd *cobra.Command, args []string) {},
	}

	rootCmd.Flags().BoolVarP(&appConfig.ShowVersion, "version", "v", appConfig.ShowVersion, "print version")

	applyServeFlags(appConfig, rootCmd)
	applyProtoFlags(appConfig, rootCmd)

	if err := loadEnvVars(appConfig); err != nil {
		return nil, err
	}

	rootCmd.AddCommand(
		versionCommand(appConfig),
		loginCommand(),
		logoutCommand(),
		sessionsCommand(),
		whoamiCommand(),
		modelsCommand(),
		serveCommand(),
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
