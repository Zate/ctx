package server

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
	"github.com/zate/ctx/internal/db"
	"github.com/zate/ctx/internal/server"
)

var (
	servePort          int
	serveBind          string
	serveTLSCert       string
	serveTLSKey        string
	serveDBUrl         string
	serveAdminPassword string
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the ctx HTTP API server",
	Long: `Start a self-hosted ctx server with a PostgreSQL backend.

The server exposes an HTTP API mirroring CLI operations: node CRUD, query,
compose, edges, tags, and (eventually) sync.

Configuration can be provided via flags, environment variables
(CTX_SERVER_PORT, CTX_SERVER_BIND, CTX_SERVER_DB_URL, CTX_SERVER_TLS_CERT,
CTX_SERVER_TLS_KEY), or a config file at ~/.ctx/server.yaml.`,
	RunE: runServe,
}

func init() {
	cfg := server.DefaultConfig()
	serveCmd.Flags().IntVar(&servePort, "port", cfg.Port, "Listen port")
	serveCmd.Flags().StringVar(&serveBind, "bind", cfg.Bind, "Bind address")
	serveCmd.Flags().StringVar(&serveTLSCert, "tls-cert", "", "TLS certificate file path")
	serveCmd.Flags().StringVar(&serveTLSKey, "tls-key", "", "TLS key file path")
	serveCmd.Flags().StringVar(&serveDBUrl, "db-url", "", "PostgreSQL connection string (e.g. postgres://user:pass@host:5432/dbname)")
	serveCmd.Flags().StringVar(&serveAdminPassword, "admin-password", "", "Admin password for device approval (enables auth)")
	register(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	// Load config file + env vars as base, then override with flags
	cfg := server.LoadConfig()

	if cmd.Flags().Changed("port") {
		cfg.Port = servePort
	}
	if cmd.Flags().Changed("bind") {
		cfg.Bind = serveBind
	}
	if cmd.Flags().Changed("tls-cert") {
		cfg.TLSCert = serveTLSCert
	}
	if cmd.Flags().Changed("tls-key") {
		cfg.TLSKey = serveTLSKey
	}
	if cmd.Flags().Changed("db-url") {
		cfg.DBUrl = serveDBUrl
	}
	if cmd.Flags().Changed("admin-password") {
		cfg.AdminPassword = serveAdminPassword
	}

	// Determine database to use
	var store db.Store
	var err error

	if cfg.DBUrl != "" {
		store, err = db.OpenPostgres(cfg.DBUrl)
		if err != nil {
			return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
		}
	} else {
		// Fall back to the global --db / --backend flags
		store, err = cmdutil.OpenDB(cmd)
		if err != nil {
			return err
		}
	}
	defer store.Close()

	srv := server.New(store, cfg)
	return srv.ListenAndServe()
}
