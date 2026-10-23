// Package config loads the service configuration from environment variables, once, at startup. Every required
// variable is checked up front so a misconfigured deployment fails fast with a clear message, instead of failing
// later on the first request that needs it. The variable names are the ones the Python service used, so the same
// .env file keeps working.
package config

import (
	"bufio"
	"cmp"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	EnvDevelopment = "development" // Vite dev server for the pages, .env file loaded from the working directory
	EnvProduction  = "production"  // Embedded (or on-disk) page assets
)

// Analysis identifies one of the TagoIO external analyses this service runs.
type Analysis string

const (
	AnalysisChangeAvailability      Analysis = "change_availability"
	AnalysisChangeMaxGridPower      Analysis = "change_max_grid_power"
	AnalysisManageRFID              Analysis = "manage_rfid"
	AnalysisChangeCPOInfo           Analysis = "change_cpo_info"
	AnalysisChangeRateList          Analysis = "change_rate_list"
	AnalysisChangeLoadBalancingMode Analysis = "change_load_balancing_mode"
	AnalysisOCPPRequests            Analysis = "ocpp_requests"
	AnalysisPowerConsumptionUpdate  Analysis = "power_consumption_update"
)

// analysisTokenVars maps each analysis to the environment variable holding its TagoIO analysis token.
var analysisTokenVars = []struct {
	analysis Analysis
	envVar   string
}{
	{AnalysisChangeAvailability, "TAGO_CHANGE_AVAILABILITY_TOKEN"},
	{AnalysisChangeMaxGridPower, "TAGO_CHANGE_MAX_POWER_GRID_TOKEN"},
	{AnalysisManageRFID, "TAGO_MANAGE_RFID_TOKEN"},
	{AnalysisChangeCPOInfo, "TAGO_CHANGE_CPO_INFO_TOKEN"},
	{AnalysisChangeRateList, "TAGO_CHANGE_RATE_LIST_TOKEN"},
	{AnalysisChangeLoadBalancingMode, "TAGO_CHANGE_LOAD_BALANCING_MODE_TOKEN"},
	{AnalysisOCPPRequests, "TAGO_OCPP_REQUESTS_TOKEN"},
	{AnalysisPowerConsumptionUpdate, "TAGO_METER_VALUES_MQTT_TOKEN"},
}

// Credentials is a user name and secret pair, compared in constant time by the auth middleware.
type Credentials struct {
	User   string
	Secret string
}

// Config holds the whole service configuration.
type Config struct {
	AppEnv string // EnvDevelopment or EnvProduction
	Port   int    // HTTP listen port (API_PORT)
	DryRun bool   // Log instead of broadcasting events or writing to TagoIO (staging and payload capture)

	DatabasePath  string // SQLite file; its directory also holds the backup archives
	BackupsToKeep int    // Newest backup archives kept on disk

	ShortLinkURL string // Base of the QR short links printed on the station posters

	ServiceAPI   Credentials // HTTP Basic for the machine endpoints (CSMS, notification service)
	Admin        Credentials // Admin dashboards login
	CookieSecret []byte      // HMAC key of the admin session cookie (DASHBOARD_SECRET_TOKEN)

	TagoAPIURL       string              // TagoIO REST base, e.g. https://api.tago.io
	TagoSSEURL       string              // TagoIO realtime SSE endpoint
	TagoAccountToken string              // Account-Token header for account-level calls
	TagoDevicePrefix string              // Device name prefix of the charging pools, e.g. MASTER-
	AnalysisTokens   map[Analysis]string // Analysis tokens, one realtime connection each

	TelegramBotToken      string
	TelegramNoticesChatID int64
	TelegramBackupsChatID int64

	ViteDevURL string // Vite dev server origin, used in development only
	UIDistDir  string // Vite build output, used in production when the binary has no embedded assets

	Location *time.Location // Europe/Madrid: schedules and the dates shown to people
}

// IsDevelopment reports whether the pages are served from the Vite dev server.
func (c *Config) IsDevelopment() bool {
	return c.AppEnv == EnvDevelopment
}

// uuidPattern validates the analysis tokens (TagoIO tokens are UUIDs).
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Load reads the configuration from the environment. In development, a .env file in the working directory fills
// the variables that are not already set.
func Load() (*Config, error) {
	appEnv := cmp.Or(os.Getenv("APP_ENV"), EnvProduction)
	if appEnv != EnvDevelopment && appEnv != EnvProduction {
		return nil, fmt.Errorf("APP_ENV must be %q or %q, got %q", EnvDevelopment, EnvProduction, appEnv)
	}
	if appEnv == EnvDevelopment {
		if err := loadDotEnv(".env"); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("reading .env: %w", err)
		}
	}

	r := &reader{}
	cfg := &Config{
		AppEnv:        appEnv,
		Port:          r.requiredInt("API_PORT"),
		DryRun:        r.optionalBool("DRY_RUN"),
		DatabasePath:  cmp.Or(os.Getenv("DB_PATH"), "database_files/database.sqlite3"),
		BackupsToKeep: r.optionalInt("BACKUPS_TO_KEEP", 30),
		ShortLinkURL:  strings.TrimSuffix(r.required("SHORT_LINK_URL"), "/"),
		ServiceAPI:    Credentials{User: r.required("APP_DEFAULT_USER"), Secret: r.required("APP_DEFAULT_TOKEN")},
		Admin:         Credentials{User: r.required("APP_ADMIN_USER"), Secret: r.required("APP_ADMIN_TOKEN")},
		CookieSecret:  []byte(r.required("DASHBOARD_SECRET_TOKEN")),

		TagoAPIURL:       strings.TrimSuffix(r.required("TAGO_API_ENDPOINT"), "/"),
		TagoSSEURL:       cmp.Or(os.Getenv("TAGO_SSE_URL"), "https://sse.tago.io/events"),
		TagoAccountToken: r.required("TAGO_ACCOUNT_TOKEN"),
		TagoDevicePrefix: r.required("TAGO_DEVICE_PREFIX"),
		AnalysisTokens:   make(map[Analysis]string, len(analysisTokenVars)),

		TelegramBotToken:      r.required("TELEGRAM_BOT_TOKEN"),
		TelegramNoticesChatID: r.requiredInt64("TELEGRAM_NOTICES_CHAT_ID"),
		TelegramBackupsChatID: r.requiredInt64("TELEGRAM_BACKUPS_CHAT_ID"),

		ViteDevURL: cmp.Or(os.Getenv("VITE_DEV_URL"), "http://localhost:5173"),
		UIDistDir:  cmp.Or(os.Getenv("UI_DIST_DIR"), "./internal/webui/dist"),
	}
	for _, v := range analysisTokenVars {
		token := r.required(v.envVar)
		if token != "" && !uuidPattern.MatchString(token) {
			r.errs = append(r.errs, fmt.Errorf("%s is not a valid UUID", v.envVar))
		}
		cfg.AnalysisTokens[v.analysis] = token
	}
	if secret := cfg.CookieSecret; len(secret) > 0 && len(secret) < 16 {
		r.errs = append(r.errs, errors.New("DASHBOARD_SECRET_TOKEN must be at least 16 characters"))
	}

	location, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("loading time zone Europe/Madrid: %w", err))
	}
	cfg.Location = location

	if err := errors.Join(r.errs...); err != nil {
		return nil, err
	}
	return cfg, nil
}

// reader collects every configuration error, so one start reports all of them at once.
type reader struct {
	errs []error
}

func (r *reader) required(name string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		r.errs = append(r.errs, fmt.Errorf("%s environment variable is not set", name))
	}
	return value
}

func (r *reader) requiredInt(name string) int {
	value := r.required(name)
	if value == "" {
		return 0
	}
	n, err := strconv.Atoi(value)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s (%q) is not a valid integer", name, value))
	}
	return n
}

func (r *reader) requiredInt64(name string) int64 {
	value := r.required(name)
	if value == "" {
		return 0
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s (%q) is not a valid integer", name, value))
	}
	return n
}

func (r *reader) optionalInt(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 {
		r.errs = append(r.errs, fmt.Errorf("%s (%q) is not a valid non-negative integer", name, value))
	}
	return n
}

func (r *reader) optionalBool(name string) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return false
	}
	b, err := strconv.ParseBool(value)
	if err != nil {
		r.errs = append(r.errs, fmt.Errorf("%s (%q) is not a valid boolean", name, value))
	}
	return b
}

// loadDotEnv sets the KEY=VALUE pairs of a .env file that are not already in the environment. It understands
// comments, blank lines, an optional "export " prefix and single or double quotes around the value.
func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
			value = value[1 : len(value)-1]
		} else if comment := strings.Index(value, " #"); comment >= 0 {
			value = strings.TrimSpace(value[:comment]) // Inline comment of an unquoted value, as Docker Compose reads it
		} else if strings.HasPrefix(value, "#") {
			value = ""
		}
		if _, set := os.LookupEnv(key); !set {
			_ = os.Setenv(key, value)
		}
	}
	return scanner.Err()
}
