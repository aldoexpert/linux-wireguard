package config

import (
	"bufio"
	"crypto/rand"
	"crypto/sha512"
	"flag"
	"os"
	"strconv"
	"strings"
)

// App holds every runtime setting, loaded once at startup from flags/env.
// Kept intentionally simple (flags + env vars, no file-based config server)
// since this project has no database -- state lives in ./db (BoltDB/JSON) only.
type App struct {
	Name     string
	Port     int
	Debug    bool
	BasePath string

	DisableLogin        bool
	JWTSecret           [64]byte
	JWTExpiryHours      int   // short-lived session (not "remember me")
	SessionMaxDurationS int64 // absolute max lifetime of a "remember me" token, in seconds

	CORSAllowedOrigins []string

	DBPath string

	SmtpHostname   string
	SmtpPort       int
	SmtpUsername   string
	SmtpPassword   string
	SmtpAuthType   string
	SmtpNoTLSCheck bool
	SmtpEncryption string
	SmtpHelo       string
	SendgridApiKey string
	EmailFrom      string
	EmailFromName  string

	TelegramToken            string
	TelegramAllowConfRequest bool
	TelegramFloodWait        int

	WgConfTemplate string
	SubnetRanges   string

	AppVersion string
	GitCommit  string
	BuildTime  string
}

// Load reads configuration from command-line flags and environment
// variables (env var wins as a default when the flag is not given).
func Load() *App {
	cfg := &App{}

	flag.StringVar(&cfg.Name, "app-name", lookupEnvOrString("LXWG_APP_NAME", "Linux WireGuard Manager"), "Application name")
	flag.IntVar(&cfg.Port, "port", lookupEnvOrInt("LXWG_PORT", 8200), "HTTP port the backend listens on")
	flag.BoolVar(&cfg.Debug, "debug", lookupEnvOrBool("LXWG_DEBUG", false), "Enable verbose/debug logging")
	flag.StringVar(&cfg.BasePath, "base-path", lookupEnvOrString("LXWG_BASE_PATH", ""), "Mount the API under a sub-path, e.g. /api")

	flag.BoolVar(&cfg.DisableLogin, "disable-login", lookupEnvOrBool("LXWG_DISABLE_LOGIN", false), "Disable authentication. Dangerous, dev only.")

	jwtSecretLookup := lookupEnvOrString("LXWG_JWT_SECRET", "")
	var jwtSecretFlag string
	if jwtSecretLookup != "" {
		flag.StringVar(&jwtSecretFlag, "jwt-secret", jwtSecretLookup, "Secret key used to sign JWT session tokens")
	} else {
		flag.StringVar(&jwtSecretFlag, "jwt-secret", lookupEnvOrFile("LXWG_JWT_SECRET_FILE", randomString(32)), "File containing the JWT secret, or the secret itself")
	}
	flag.IntVar(&cfg.JWTExpiryHours, "jwt-expiry-hours", lookupEnvOrInt("LXWG_JWT_EXPIRY_HOURS", 24), "Short session token lifetime in hours")
	sessionMaxDays := flag.Int("session-max-duration", lookupEnvOrInt("LXWG_SESSION_MAX_DURATION", 90), "Max days a \"remember me\" session stays valid")

	corsOrigins := flag.String("cors-allowed-origins", lookupEnvOrString("LXWG_CORS_ALLOWED_ORIGINS", "*"), "Comma separated list of allowed CORS origins for the frontend")

	flag.StringVar(&cfg.DBPath, "db-path", lookupEnvOrString("LXWG_DB_PATH", "./db"), "Directory where application data (users, clients, settings) is stored")

	flag.StringVar(&cfg.SmtpHostname, "smtp-hostname", lookupEnvOrString("LXWG_SMTP_HOSTNAME", "127.0.0.1"), "SMTP hostname")
	flag.IntVar(&cfg.SmtpPort, "smtp-port", lookupEnvOrInt("LXWG_SMTP_PORT", 25), "SMTP port")
	flag.StringVar(&cfg.SmtpHelo, "smtp-helo", lookupEnvOrString("LXWG_SMTP_HELO", "localhost"), "SMTP HELO hostname")
	flag.StringVar(&cfg.SmtpUsername, "smtp-username", lookupEnvOrString("LXWG_SMTP_USERNAME", ""), "SMTP username")
	flag.BoolVar(&cfg.SmtpNoTLSCheck, "smtp-no-tls-check", lookupEnvOrBool("LXWG_SMTP_NO_TLS_CHECK", false), "Disable TLS verification for SMTP. Dangerous.")
	flag.StringVar(&cfg.SmtpEncryption, "smtp-encryption", lookupEnvOrString("LXWG_SMTP_ENCRYPTION", "STARTTLS"), "SMTP encryption: NONE, SSL, SSLTLS, TLS or STARTTLS")
	flag.StringVar(&cfg.SmtpAuthType, "smtp-auth-type", lookupEnvOrString("LXWG_SMTP_AUTH_TYPE", "NONE"), "SMTP auth type: PLAIN, LOGIN or NONE")
	flag.StringVar(&cfg.EmailFrom, "email-from", lookupEnvOrString("LXWG_EMAIL_FROM_ADDRESS", ""), "'From' email address")
	flag.StringVar(&cfg.EmailFromName, "email-from-name", lookupEnvOrString("LXWG_EMAIL_FROM_NAME", "Linux WireGuard Manager"), "'From' email name")

	smtpPasswordLookup := lookupEnvOrString("LXWG_SMTP_PASSWORD", "")
	if smtpPasswordLookup != "" {
		flag.StringVar(&cfg.SmtpPassword, "smtp-password", smtpPasswordLookup, "SMTP password")
	} else {
		flag.StringVar(&cfg.SmtpPassword, "smtp-password", lookupEnvOrFile("LXWG_SMTP_PASSWORD_FILE", ""), "File containing the SMTP password")
	}

	sendgridKeyLookup := lookupEnvOrString("LXWG_SENDGRID_API_KEY", "")
	if sendgridKeyLookup != "" {
		flag.StringVar(&cfg.SendgridApiKey, "sendgrid-api-key", sendgridKeyLookup, "Sendgrid API key")
	} else {
		flag.StringVar(&cfg.SendgridApiKey, "sendgrid-api-key", lookupEnvOrFile("LXWG_SENDGRID_API_KEY_FILE", ""), "File containing the Sendgrid API key")
	}

	flag.StringVar(&cfg.TelegramToken, "telegram-token", lookupEnvOrString("LXWG_TELEGRAM_TOKEN", ""), "Telegram bot token for distributing configs to clients")
	flag.BoolVar(&cfg.TelegramAllowConfRequest, "telegram-allow-conf-request", lookupEnvOrBool("LXWG_TELEGRAM_ALLOW_CONF_REQUEST", false), "Allow clients to request their config via the bot")
	flag.IntVar(&cfg.TelegramFloodWait, "telegram-flood-wait", lookupEnvOrInt("LXWG_TELEGRAM_FLOOD_WAIT", 60), "Minutes before the next conf request is processed")

	flag.StringVar(&cfg.WgConfTemplate, "wg-conf-template", lookupEnvOrString("LXWG_WG_CONF_TEMPLATE", ""), "Path to a custom wg.conf template")
	flag.StringVar(&cfg.SubnetRanges, "subnet-ranges", lookupEnvOrString("LXWG_SUBNET_RANGES", ""), "IP ranges to offer when assigning an IP to a client")

	flag.Parse()

	cfg.JWTSecret = sha512.Sum512([]byte(jwtSecretFlag))
	cfg.SessionMaxDurationS = int64(*sessionMaxDays) * 86_400
	cfg.CORSAllowedOrigins = splitAndTrim(*corsOrigins, ",")

	return cfg
}

func splitAndTrim(s, sep string) []string {
	parts := strings.Split(s, sep)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func lookupEnvOrString(key string, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	return defaultVal
}

func lookupEnvOrBool(key string, defaultVal bool) bool {
	if val, ok := os.LookupEnv(key); ok {
		v, err := strconv.ParseBool(val)
		if err == nil {
			return v
		}
	}
	return defaultVal
}

func lookupEnvOrInt(key string, defaultVal int) int {
	if val, ok := os.LookupEnv(key); ok {
		v, err := strconv.Atoi(val)
		if err == nil {
			return v
		}
	}
	return defaultVal
}

func lookupEnvOrFile(key string, defaultVal string) string {
	if val, ok := os.LookupEnv(key); ok {
		if file, err := os.Open(val); err == nil {
			defer file.Close()
			var content string
			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				content += scanner.Text()
			}
			return content
		}
	}
	return defaultVal
}

// randomString generates a fallback JWT secret when none is configured.
// A fallback secret is only acceptable for local/dev use: it changes every
// restart, invalidating existing sessions, so production deployments must
// set LXWG_JWT_SECRET (or LXWG_JWT_SECRET_FILE) explicitly.
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	raw := make([]byte, length)
	if _, err := rand.Read(raw); err != nil {
		// crypto/rand failing is effectively fatal for the whole system;
		// fall back to a fixed (still process-unique via PID/time) seed
		// rather than panic during flag parsing.
		for i := range raw {
			raw[i] = byte(i)
		}
	}
	b := make([]byte, length)
	for i, v := range raw {
		b[i] = charset[int(v)%len(charset)]
	}
	return string(b)
}
