package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"jiso/internal/cli/userconfig"
)

// Precedence layers (CLI-104), highest first.
const (
	layerFlag    = "flag"
	layerEnv     = "env"
	layerConfig  = "config"
	layerDefault = "default"
)

// resolved records the winning layer for one standard key, for $JISO_DEBUG.
type resolved struct {
	key   string
	value string
	from  string
}

// resolvePrecedence applies --flag > $JISO_* > user config > default to the
// standard flag set: when a flag is unset on the command line, the env value
// (then the user config value) is written into the flag so every downstream
// consumer — the cfg.Config mapping in PersistentPreRunE, the output
// renderer, subcommands reading their own flags — sees the resolved value.
//
// Debug has no flag by design (v2 policy: $JISO_DEBUG=1, never --verbose).
// When debug resolves on, one line per debug-noticed key goes to stderr in
// the form `debug: <key> = <value> (from flag|env|config|default)`; stdout
// is never touched.
func resolvePrecedence(cmd *cobra.Command, uc *userconfig.File) {
	spec := resolveString(cmd, "spec", "JISO_SPEC", uc.Spec)
	resolveString(cmd, "file", "JISO_FILE", uc.File)
	resolveDB(cmd, uc.DB)
	host := resolveString(cmd, "host", "JISO_HOST", uc.Host)
	port := resolveString(cmd, "port", "JISO_PORT", uc.Port)
	resolveString(cmd, "header", "JISO_HEADER", uc.Header)
	resolveString(cmd, "tls-config", "JISO_TLS_CONFIG", uc.TLSConfig)
	resolveString(cmd, "visa-station-id", "JISO_VISA_STATION_ID", uc.VisaStationID)
	resolveBool(cmd, "json", "JISO_JSON", uc.JSON)
	resolveBool(cmd, "quiet", "JISO_QUIET", uc.Quiet)
	resolveBool(cmd, "unsecure", "JISO_UNSECURE", uc.Unsecure)
	// The §L settings keys resolve through the same layers so
	// a value persisted by the TUI settings screen is the default the
	// next CLI invocation uses (Set marks the flag Changed, which the
	// root.go cfg mapping gates on for the numeric flags).
	resolveInt(cmd, "reconnect-attempts", "JISO_RECONNECT_ATTEMPTS", uc.ReconnectAttempts)
	resolveDuration(cmd, "connect-timeout", "JISO_CONNECT_TIMEOUT", uc.ConnectTimeout)
	resolveDuration(cmd, "total-connect-timeout", "JISO_TOTAL_CONNECT_TIMEOUT", uc.TotalConnectTimeout)
	resolveDuration(cmd, "response-timeout", "JISO_RESPONSE_TIMEOUT", uc.ResponseTimeout)
	resolveDuration(cmd, "listen-timeout", "JISO_LISTEN_TIMEOUT", uc.ListenTimeout)
	resolveBool(cmd, "hex", "JISO_HEX", uc.Hex)

	if !resolveDebug(uc) {
		return
	}

	w := cmd.ErrOrStderr()
	// Accepted debt (M1 review #8, deferred by orchestrator): only spec,
	// host, and port are debug-noticed, and an empty $JISO_* is not yet
	// distinguished from an unset one (no LookupEnv at the env layer).
	for _, r := range []resolved{spec, host, port} {
		_, _ = fmt.Fprintf(w, "debug: %s = %s (from %s)\n", r.key, r.value, r.from)
	}
}

// resolveDebug resolves $JISO_DEBUG (then config debug:): there is no
// --debug flag, so no flag layer exists for it.
func resolveDebug(uc *userconfig.File) bool {
	if v, ok := parseEnvBool(os.Getenv("JISO_DEBUG")); ok {
		return v
	}

	return uc != nil && uc.Debug != nil && *uc.Debug
}

// resolveString resolves one string flag across the layers and writes the
// winning env/config value into the flag. Flags absent from the command
// (Lookup == nil) resolve to the default layer with an empty value.
func resolveString(cmd *cobra.Command, flag, env string, cf *string) resolved {
	r := resolved{key: flag, from: layerDefault}
	if cmd.Flags().Lookup(flag) == nil {
		return r
	}

	switch {
	case cmd.Flags().Changed(flag):
		r.value, _ = cmd.Flags().GetString(flag)
		r.from = layerFlag
	case os.Getenv(env) != "":
		r.value = os.Getenv(env)
		r.from = layerEnv
		_ = cmd.Flags().Set(flag, r.value)
	case cf != nil && *cf != "":
		r.value = *cf
		r.from = layerConfig
		_ = cmd.Flags().Set(flag, r.value)
	default:
		r.value, _ = cmd.Flags().GetString(flag)
	}

	return r
}

// resolveDB resolves --db, honoring the hidden legacy --db-path alias: an
// explicit --db-path is still the flag layer and must not be shadowed by
// $JISO_DB or the config file. The reported value is the winning path and
// layerFlag is claimed only for a non-empty one, so $JISO_DEBUG shows
// `db = <path>` instead of an empty string.
func resolveDB(cmd *cobra.Command, cf *string) resolved {
	if cmd.Flags().Changed("db") {
		if v, _ := cmd.Flags().GetString("db"); v != "" {
			return resolved{key: "db", value: v, from: layerFlag}
		}
	}
	if cmd.Flags().Changed("db-path") {
		if v, _ := cmd.Flags().GetString("db-path"); v != "" {
			return resolved{key: "db", value: v, from: layerFlag}
		}
	}

	// No non-empty explicit flag: fall through to env > config > default,
	// writing the winner into --db like resolveString does.
	r := resolved{key: "db", from: layerDefault}
	switch {
	case os.Getenv("JISO_DB") != "":
		r.value, r.from = os.Getenv("JISO_DB"), layerEnv
	case cf != nil && *cf != "":
		r.value, r.from = *cf, layerConfig
	}

	if r.value != "" {
		_ = cmd.Flags().Set("db", r.value)
	} else {
		r.value, _ = cmd.Flags().GetString("db")
	}

	return r
}

// resolveBool resolves one bool flag across the layers. An unparsable env
// value counts as unset (same lenient reading as CLI-102); a set false env
// still beats a config true, per strict precedence.
// debug-notice it; these keys are simply not noticed today, and splitting the
// family into "returns" and "doesn't" halves would read as an accident.
//
//nolint:unparam // every resolveX returns the record it resolved, so a caller can
func resolveBool(cmd *cobra.Command, flag, env string, cf *bool) resolved {
	r := resolved{key: flag, from: layerDefault}
	if cmd.Flags().Lookup(flag) == nil {
		return r
	}

	apply := func(value, from string) {
		r.value, r.from = value, from
		_ = cmd.Flags().Set(flag, value)
	}

	switch v, _ := cmd.Flags().GetBool(flag); {
	case cmd.Flags().Changed(flag):
		r.value, r.from = fmt.Sprintf("%t", v), layerFlag
	case envBoolParsable(os.Getenv(env)):
		b, _ := parseEnvBool(os.Getenv(env))
		apply(fmt.Sprintf("%t", b), layerEnv)
	case cf != nil && *cf:
		apply("true", layerConfig)
	default:
		r.value = fmt.Sprintf("%t", v)
	}

	return r
}

// resolveInt resolves one int flag across the layers: an
// unparsable env value counts as unset; the env/config winner is written
// into the flag (Set marks it Changed so the cfg mapping in root.go
// consumes it). The reported value is the winning text.
func resolveInt(cmd *cobra.Command, flag, env string, cf *int) resolved {
	r := resolved{key: flag, from: layerDefault}
	if cmd.Flags().Lookup(flag) == nil {
		return r
	}

	if s := os.Getenv(env); s != "" {
		if _, err := strconv.Atoi(s); err == nil {
			r.value, r.from = s, layerEnv
			_ = cmd.Flags().Set(flag, s)

			return r
		}
	}
	if cf != nil {
		s := strconv.Itoa(*cf)
		r.value, r.from = s, layerConfig
		_ = cmd.Flags().Set(flag, s)

		return r
	}

	if n, err := cmd.Flags().GetInt(flag); err == nil {
		r.value = strconv.Itoa(n)
	}

	return r
}

// resolveDuration resolves one duration flag across the layers
// Env and config values must parse as Go durations, an
// unparsable one counts as unset; the winner is written into the flag.
// debug-notice it; these keys are simply not noticed today, and splitting the
// family into "returns" and "doesn't" halves would read as an accident.
//
//nolint:unparam // every resolveX returns the record it resolved, so a caller can
func resolveDuration(cmd *cobra.Command, flag, env string, cf *string) resolved {
	r := resolved{key: flag, from: layerDefault}
	if cmd.Flags().Lookup(flag) == nil {
		return r
	}

	try := func(s string) bool {
		_, err := time.ParseDuration(s)

		return err == nil
	}

	if s := os.Getenv(env); s != "" && try(s) {
		r.value, r.from = s, layerEnv
		_ = cmd.Flags().Set(flag, s)

		return r
	}
	if cf != nil && try(*cf) {
		r.value, r.from = *cf, layerConfig
		_ = cmd.Flags().Set(flag, *cf)

		return r
	}

	if d, err := cmd.Flags().GetDuration(flag); err == nil {
		r.value = d.String()
	}

	return r
}

// parseEnvBool reads the v2 truthy/falsy env spellings; ok is false for
// empty or unparsable values, which count as unset.
func parseEnvBool(s string) (v, ok bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	}

	return false, false
}

func envBoolParsable(s string) bool {
	_, ok := parseEnvBool(s)

	return ok
}
