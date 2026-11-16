package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"time"

	"war-room/backend/pkg/config"
	"war-room/backend/pkg/database"
	"war-room/backend/pkg/logging"
	"war-room/backend/pkg/service"
)

const apiAccessUsage = `usage:
  server api-token create --email EMAIL [--name NAME] [--days DAYS]
  server api-token list   --email EMAIL
  server api-token revoke --email EMAIL --id TOKEN_ID`

// apiTokenStore is the subset of service.AuthService the command uses.
type apiTokenStore interface {
	CreateAPIToken(email, name string, lifetime time.Duration) (string, service.APIToken, error)
	ListAPITokens(email string) ([]service.APIToken, error)
	RevokeAPIToken(email, id string) error
}

// runAPITokenCLI opens the configured database and runs one api-token command.
func runAPITokenCLI(args []string, stdout, stderr io.Writer) int {
	cfg := config.Load()
	if err := logging.Setup(stderr, cfg.LogLevel, cfg.LogFormat); err != nil {
		slog.Warn("invalid logging settings; using defaults where needed", "error", err.Error())
	}
	db, err := database.InitDB(cfg)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "could not open database:", err)
		return 1
	}
	defer func() { _ = db.Close() }()
	return runAPITokenCommand(service.NewAuthService(db, cfg), args, stdout, stderr)
}

// runAPITokenCommand executes create, list, or revoke and returns the exit
// code. A created token is the only thing written to stdout, so it can be
// redirected straight into a file without echoing it to a terminal.
func runAPITokenCommand(store apiTokenStore, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, apiAccessUsage)
		return 2
	}
	flags := flag.NewFlagSet("api-token "+args[0], flag.ContinueOnError)
	flags.SetOutput(stderr)
	email := flags.String("email", "", "account email")
	name := flags.String("name", "", "label for the token")
	days := flags.Int("days", int(service.DefaultAPITokenLifetime/(24*time.Hour)), "lifetime in days (1-365)")
	id := flags.String("id", "", "token ID to revoke")
	if err := flags.Parse(args[1:]); err != nil || *email == "" {
		_, _ = fmt.Fprintln(stderr, apiAccessUsage)
		return 2
	}
	var err error
	switch args[0] {
	case "create":
		err = createAPIToken(store, *email, *name, *days, stdout, stderr)
	case "list":
		err = listAPITokens(store, *email, stdout)
	case "revoke":
		err = revokeAPIToken(store, *email, *id, stderr)
	default:
		_, _ = fmt.Fprintln(stderr, apiAccessUsage)
		return 2
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "api-token:", err)
		return 1
	}
	return 0
}

func createAPIToken(store apiTokenStore, email, name string, days int, stdout, stderr io.Writer) error {
	token, record, err := store.CreateAPIToken(email, name, time.Duration(days)*24*time.Hour)
	if err != nil {
		return err
	}
	logging.Audit(context.Background(), "api_token.created", "token_id", record.ID, "expires_at", record.ExpiresAt.UTC().Format(time.RFC3339))
	_, _ = fmt.Fprintf(stderr, "created token %s, expires %s; it is shown only once\n", record.ID, record.ExpiresAt.UTC().Format(time.DateOnly))
	_, err = fmt.Fprintln(stdout, token)
	return err
}

func listAPITokens(store apiTokenStore, email string, stdout io.Writer) error {
	tokens, err := store.ListAPITokens(email)
	if err != nil {
		return err
	}
	for _, token := range tokens {
		lastUsed := "never"
		if token.LastUsedAt != nil {
			lastUsed = token.LastUsedAt.UTC().Format(time.RFC3339)
		}
		_, _ = fmt.Fprintf(stdout, "%s\tname=%q\tcreated=%s\texpires=%s\tlast_used=%s\n", token.ID, token.Name,
			token.CreatedAt.UTC().Format(time.DateOnly), token.ExpiresAt.UTC().Format(time.DateOnly), lastUsed)
	}
	return nil
}

func revokeAPIToken(store apiTokenStore, email, id string, stderr io.Writer) error {
	if id == "" {
		return errors.New("--id is required")
	}
	if err := store.RevokeAPIToken(email, id); err != nil {
		return err
	}
	logging.Audit(context.Background(), "api_token.revoked", "token_id", id)
	_, _ = fmt.Fprintln(stderr, "revoked token", id)
	return nil
}
