// This file holds the `server tagoio` command: the read-only TagoIO inspection for whoever has a shell on the host,
// e.g. `docker exec tagoio-data-handler /app/server tagoio pool 261001`. It asks the running service on
// 127.0.0.1 with the service credentials of its own environment, so neither those credentials nor the TagoIO
// account token leave the container, and prints the JSON answer.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/dgarciacampelo/tagoio-handler/internal/config"
)

const tagoioCommandTimeout = 2 * time.Minute

const tagoioUsage = `usage: server tagoio [-base-url URL] <command>

commands (read only):
  devices                            every device of the TagoIO account, and the registered pools without one
  pool <pool_code>                   one pool across TagoIO and the service: devices, registration, bucket
                                     configuration, users carrying its group_id, and the inconsistencies found
  users [-email E] [-group-id CODE]  the run users with that email and/or group_id tag

-base-url defaults to http://127.0.0.1:$API_PORT, the service in the same container.`

// errUsage is a command line the command doesn't understand.
var errUsage = errors.New("invalid command line")

// runTagoIOCommand runs `server tagoio …` and returns the exit code: 0 on success, 1 when the request failed, 2
// for a wrong command line.
func runTagoIOCommand(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("tagoio", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	baseURL := flags.String("base-url", "", "service URL")
	if err := flags.Parse(args); err != nil {
		fmt.Fprintf(stderr, "%v\n\n%s\n", err, tagoioUsage)
		return 2
	}
	path, err := tagoioCommandPath(flags.Args())
	if err != nil {
		fmt.Fprintf(stderr, "%v\n\n%s\n", err, tagoioUsage)
		return 2
	}

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(stderr, "configuration: %v\n", err)
		return 1
	}
	base := strings.TrimSuffix(*baseURL, "/")
	if base == "" {
		base = "http://127.0.0.1:" + strconv.Itoa(cfg.Port)
	}

	ctx, cancel := context.WithTimeout(ctx, tagoioCommandTimeout)
	defer cancel()
	body, err := fetchTagoIO(ctx, &http.Client{}, base+path, cfg.ServiceAPI)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	_, _ = stdout.Write(body)
	return 0
}

// tagoioCommandPath maps the command's arguments to the path of its inspection API.
func tagoioCommandPath(args []string) (string, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("%w: a command is required", errUsage)
	}
	switch command, rest := args[0], args[1:]; command {
	case "devices":
		if len(rest) != 0 {
			return "", fmt.Errorf("%w: devices takes no arguments", errUsage)
		}
		return "/api/tagoio/devices", nil

	case "pool":
		if len(rest) != 1 {
			return "", fmt.Errorf("%w: pool takes one pool code", errUsage)
		}
		code, err := strconv.Atoi(rest[0])
		if err != nil || code <= 0 {
			return "", fmt.Errorf("%w: %q is not a pool code", errUsage, rest[0])
		}
		return "/api/tagoio/pools/" + strconv.Itoa(code), nil

	case "users":
		flags := flag.NewFlagSet("users", flag.ContinueOnError)
		flags.SetOutput(io.Discard)
		email := flags.String("email", "", "user email")
		groupID := flags.String("group-id", "", "group_id tag (pool code)")
		if err := flags.Parse(rest); err != nil {
			return "", fmt.Errorf("%w: %v", errUsage, err)
		}
		if flags.NArg() != 0 || (*email == "" && *groupID == "") {
			return "", fmt.Errorf("%w: users takes -email and/or -group-id", errUsage)
		}
		query := url.Values{}
		if *email != "" {
			query.Set("email", *email)
		}
		if *groupID != "" {
			query.Set("group_id", *groupID)
		}
		return "/api/tagoio/users?" + query.Encode(), nil

	default:
		return "", fmt.Errorf("%w: unknown command %q", errUsage, command)
	}
}

// fetchTagoIO GETs an inspection API with the service credentials and returns its JSON answer, indented.
func fetchTagoIO(ctx context.Context, client *http.Client, endpoint string, creds config.Credentials) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(creds.User, creds.Secret)
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("asking the service: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, fmt.Errorf("reading the answer: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the service answered HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(raw))
	}

	var indented bytes.Buffer
	if err := json.Indent(&indented, raw, "", "  "); err != nil {
		return nil, fmt.Errorf("the service answered something that is not JSON: %w", err)
	}
	indented.WriteByte('\n')
	return indented.Bytes(), nil
}
