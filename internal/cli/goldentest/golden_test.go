// Package goldentest is the CLI golden harness (APP-206): it execs the REAL
// jiso binary once built from ./cmd and pins stdout, stderr, and exit codes
// against testdata/cli/*.golden.json so future refactors (TUI work!) cannot
// silently break the v2 CLI contract.
//
// CI contract: no TTY is required. Every case runs with a clean environment
// (a small allowlist plus the case's env), an isolated temp CWD holding
// copies of fixtures generated in TestMain, and fully captured stdout and
// stderr.
//
// Developer convenience: GOLDEN_UPDATE=1 re-runs failing cases, regenerates
// their expect_* fields from the actual output, prints a diff-like summary,
// and rewrites the golden file. Regenerated lists drop volatile lines
// (timestamps/dates) so goldens stay deterministic; prefer json_keys or
// contains over exact.
//
// SIGINT exit 130 is deliberately NOT a golden case: killing a golden
// subprocess mid-run is flaky in CI, and the contract is already pinned by
// the exec-probe TestSIGINTExits130 in internal/cli/cmd/exit_test.go.
//
// TUI-401 note: the former stub-tui-exit-2 case pinned `jiso tui` exiting 2
// as a not-implemented stub. The TUI now exists, and exec-ing the real
// program needs a PTY, which this harness never provides — so the case was
// re-pinned (honestly, via GOLDEN_UPDATE against the real binary) as
// tui-requires-tty-exit-2: the TTY guard rejects non-terminal invocations
// before the program launches, which is deterministic without a PTY. The
// TUI's own behavior is unit-tested in internal/tui (no exec, no PTY).
package goldentest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"jiso/internal/app"
	"jiso/internal/db"
	"jiso/internal/server"
	"jiso/internal/utils"
)

const (
	goldenDir      = "testdata/cli"
	goldenSuffix   = ".golden.json"
	binaryName     = "jiso"
	caseTimeout    = 30 * time.Second
	fixtureSession = "golden-session-0001"
	// fixtureVisaSession is a Visa-headered session (matches ctf list's
	// Visa filter) with one approved transaction, for PAR-308 ctf goldens.
	fixtureVisaSession = "golden-visa-session-0002"
)

// jisoEnvVars mirrors internal/cli/cmd's standard env set; the harness blanks
// every one so a developer's shell can never leak into a golden run.
var jisoEnvVars = []string{
	"JISO_SPEC", "JISO_FILE", "JISO_DB", "JISO_HOST", "JISO_PORT",
	"JISO_HEADER", "JISO_TLS_CONFIG", "JISO_VISA_STATION_ID",
	"JISO_JSON", "JISO_QUIET", "JISO_DEBUG", "JISO_UNSECURE", "JISO_CONFIG",
	"JISO_STATE_DIR",
}

// volatileLine matches lines carrying dates/times that must never be pinned
// verbatim into a golden file (determinism rule of the harness).
var volatileLine = regexp.MustCompile(`\d{4}-\d{2}-\d{2}|\d{2}:\d{2}:\d{2}|\d{1,3}/\d{1,3}/\d{4}`)

// goldenCase is one contract case, serialized as testdata/cli/<name>.golden.json.
//
// expect_stdout_mode selects how expect_stdout is checked:
//   - "contains" (default): every entry is a substring of stdout
//   - "exact": stdout equals the entries joined by "\n" (+ trailing "\n")
//   - "prefix": stdout starts with entry[0]; remaining entries are substrings
//   - "json_keys": stdout parses as a JSON object holding every entry as key
//   - "json_array": stdout parses as a pure JSON array (PAR-308 list
//     purity); entries are then checked as substrings of stdout
//   - "empty": stdout must be empty (ExpectStdout must then be empty)
type goldenCase struct {
	Args   []string          `json:"args"`
	Env    map[string]string `json:"env,omitempty"`
	Stdin  string            `json:"stdin,omitempty"`
	Mode   string            `json:"expect_stdout_mode,omitempty"`
	Stdout []string          `json:"expect_stdout,omitempty"`

	StderrContains []string `json:"expect_stderr_contains,omitempty"`
	StderrEmpty    bool     `json:"expect_stderr_empty,omitempty"`
	ExpectExit     int      `json:"expect_exit"`

	// JSONFields pins individual values of the stdout JSON object
	// (PAR-303): stdout must parse as an object and each named key's raw
	// JSON must equal the pinned literal (compact-compared). Values with
	// volatile content (ephemeral ports, platform error text) must not be
	// pinned; GOLDEN_UPDATE never generates this field.
	JSONFields map[string]string `json:"expect_json_fields,omitempty"`

	// ExpectJSONFieldsGT (PAR-306) pins lower bounds on numeric stdout
	// JSON fields: each named key must parse as a number strictly greater
	// than the bound. Used for live stress runs whose exact counts vary
	// but must be > 0; GOLDEN_UPDATE never generates this field.
	ExpectJSONFieldsGT map[string]float64 `json:"expect_json_fields_gt,omitempty"`

	ForbidStdoutSubstrings []string `json:"forbid_stdout_substrings,omitempty"`
	// ForbidStderrSubstrings pins what stderr must NOT contain (PAR-300);
	// entries support the $WORK placeholder like the file assertions.
	ForbidStderrSubstrings []string `json:"forbid_stderr_substrings,omitempty"`
	FilesMustExist         []string `json:"files_must_exist,omitempty"`
	FilesMustNotExist      []string `json:"files_must_not_exist,omitempty"`

	// ExpectFileHeads (PAR-308) asserts each resolved path's content starts
	// with the pinned prefix (CTF record structure and similar magic). Keys
	// support the $WORK placeholder like the file assertions.
	ExpectFileHeads map[string]string `json:"expect_file_heads,omitempty"`

	// SetupFiles (PAR-304) materialises files under the case's work dir
	// BEFORE the binary runs (e.g. a stale serve state file with a dead
	// PID). Keys are paths relative to $WORK (or absolute); values are the
	// file contents. Both support $WORK, $LIVE_PORT, and $DEAD_PID.
	SetupFiles map[string]string `json:"setup_files,omitempty"`
}

var (
	binaryPath string
	buildErr   error
	fixtureErr error
	fixtureDir string
	updateMode = os.Getenv("GOLDEN_UPDATE") == "1"
)

// Placeholders substituted everywhere a case can name them (args, env
// values, setup_files, stderr/json assertions, file assertions):
//   - $LIVE_PORT — port of the in-process mock server (PAR-301 live sends)
//   - $DEAD_PID  — a PID confirmed dead (PAR-304 stale-state cases)
//   - $WORK      — the case's isolated CWD
//
// Cases referencing $LIVE_PORT skip when the live server could not start.
const (
	livePortPlaceholder = "$LIVE_PORT"
	deadPIDPlaceholder  = "$DEAD_PID"
	workPlaceholder     = "$WORK"
)

var (
	liveServer    *server.Server
	liveServerErr error
	livePort      string

	// sideChannelDir is the shared $JISO_STATE_DIR the in-process live
	// server publishes its PAR-304 state/snapshot files into; caseEnv hands
	// it to every case, so `serve stats $LIVE_PORT` works out of the box.
	sideChannelDir string
	liveStatsStop  func() error
)

// deadPID is reaped in TestMain so stale-state goldens pin a PID that is
// guaranteed not to be alive (99999 could exist on systems with that
// pid_max; a just-reaped pid cannot come back within the run).
var deadPID = "99999"

func reapDeadPID() {
	cmd := exec.Command("sleep", "0")
	if err := cmd.Run(); err != nil {
		return
	}

	deadPID = strconv.Itoa(cmd.Process.Pid)
}

// startLiveServer boots the in-repo mock engine on an ephemeral port with
// the fixture spec and the ascii4 header (the client's App.Connect
// default), so live-success sends need no external server. It also
// publishes the PAR-304 side-channel files (state + 1 s stats snapshot)
// into sideChannelDir so `serve stats` goldens have a RUNNING server.
func startLiveServer(specPath string) error {
	spec, err := utils.CreateSpecFromFile(specPath)
	if err != nil {
		return fmt.Errorf("live server spec: %w", err)
	}

	srv := server.NewServer(spec, nil, "ascii4")
	if err := srv.Start("0"); err != nil {
		return fmt.Errorf("live server start: %w", err)
	}

	port, err := srv.BoundPort()
	if err != nil {
		_ = srv.Stop()

		return fmt.Errorf("live server port: %w", err)
	}

	liveServer, livePort = srv, port

	_, stop, err := app.StartServeSideChannel(srv, "127.0.0.1", "", nil, app.ServeStatsRefreshInterval)
	if err != nil {
		_ = srv.Stop()

		return fmt.Errorf("live server state file: %w", err)
	}

	liveStatsStop = stop

	return nil
}

// TestMain builds the binary ONCE (skip reason reported by TestGolden when
// the build fails) and generates the shared fixtures: a tiny spec, a tx file
// with one scenario, a malformed spec, and a session database written with
// the repo's own db package.
func TestMain(m *testing.M) {
	reapDeadPID()

	binaryPath, buildErr = buildBinary()
	if buildErr == nil {
		fixtureDir, fixtureErr = writeFixtures()
		if fixtureErr == nil {
			sideChannelDir, fixtureErr = os.MkdirTemp("", "jiso-golden-state-*")
			if fixtureErr != nil {
				fixtureErr = fmt.Errorf("create shared state dir: %w", fixtureErr)
			}
		}
		if fixtureErr == nil {
			// The in-process live server reads $JISO_STATE_DIR when
			// publishing its side-channel files.
			if err := os.Setenv("JISO_STATE_DIR", sideChannelDir); err != nil {
				fixtureErr = fmt.Errorf("set $JISO_STATE_DIR: %w", err)
			} else {
				liveServerErr = startLiveServer(filepath.Join(fixtureDir, "spec.json"))
			}
		}
	}

	code := m.Run()

	if liveStatsStop != nil {
		_ = liveStatsStop()
	}

	if liveServer != nil {
		_ = liveServer.Stop()
	}

	if binaryPath != "" {
		_ = os.RemoveAll(filepath.Dir(binaryPath))
	}

	if fixtureDir != "" {
		_ = os.RemoveAll(fixtureDir)
	}

	if sideChannelDir != "" {
		_ = os.RemoveAll(sideChannelDir)
	}

	os.Exit(code)
}

func buildBinary() (string, error) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		return "", fmt.Errorf("resolve repo root: %w", err)
	}

	tmp, err := os.MkdirTemp("", "jiso-golden-*")
	if err != nil {
		return "", fmt.Errorf("create build temp dir: %w", err)
	}

	path := filepath.Join(tmp, binaryName)

	cmd := exec.Command("go", "build", "-o", path, "./cmd")
	cmd.Dir = root

	out, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.RemoveAll(tmp)

		return "", fmt.Errorf("go build ./cmd: %w\n%s", err, out)
	}

	return path, nil
}

const fixtureSpecJSON = `{
  "name": "GOLDEN",
  "fields": {
    "0": {"type": "String", "length": 4, "description": "MTI", "enc": "ASCII", "prefix": "ASCII.Fixed"},
    "1": {"type": "Bitmap", "length": 8, "description": "Bitmap", "enc": "Binary", "prefix": "Hex.Fixed"},
    "2": {"type": "String", "length": 19, "description": "PAN", "enc": "ASCII", "prefix": "ASCII.LL"},
    "3": {"type": "String", "length": 6, "description": "Processing Code", "enc": "ASCII", "prefix": "ASCII.Fixed"},
    "4": {"type": "String", "length": 12, "description": "Amount", "enc": "ASCII", "prefix": "ASCII.Fixed", "padding": {"type": "Left", "pad": "0"}},
    "7": {"type": "String", "length": 10, "description": "Transmission Date & Time", "enc": "ASCII", "prefix": "ASCII.Fixed"},
    "11": {"type": "String", "length": 6, "description": "STAN", "enc": "ASCII", "prefix": "ASCII.Fixed"},
    "12": {"type": "String", "length": 6, "description": "Local Time", "enc": "ASCII", "prefix": "ASCII.Fixed"},
    "13": {"type": "String", "length": 4, "description": "Local Date", "enc": "ASCII", "prefix": "ASCII.Fixed"},
    "39": {"type": "String", "length": 2, "description": "Response Code", "enc": "ASCII", "prefix": "ASCII.Fixed"},
    "41": {"type": "String", "length": 8, "description": "Terminal ID", "enc": "ASCII", "prefix": "ASCII.Fixed"},
    "70": {"type": "Numeric", "length": 3, "description": "Network Management Info Code", "enc": "ASCII", "prefix": "ASCII.Fixed", "padding": {"type": "Left", "pad": "0"}}
  }
}`

const fixtureTxJSON = `[
  {
    "type": "transaction",
    "name": "Echo",
    "description": "Network Management: Echo",
    "fields": {"0": "0800", "7": "auto", "11": "auto", "70": 301}
  },
  {
    "type": "transaction",
    "name": "Purchase",
    "description": "Financial Request (0200)",
    "fields": {"0": "0200", "2": "4242424242424242", "3": "000000", "4": "1000", "7": "auto", "11": "auto", "12": "120000", "13": "0907", "41": "77973588"}
  },
  {
    "type": "dataset",
    "name": "golden_pool",
    "data": [{"pan": "4000000000000002", "amount": "2500"}]
  },
  {
    "type": "transaction",
    "name": "PurchaseDS",
    "description": "Dataset-interpolated purchase (PAR-302)",
    "dataset_name": "golden_pool",
    "fields": {"0": "0200", "2": "{{data.pan}}", "4": "{{data.amount}}", "41": "77973588"}
  },
  {
    "type": "scenario",
    "name": "Smoke",
    "description": "Golden smoke scenario",
    "steps": [
      {"name": "echo step", "use_transaction_id": "Echo"},
      {"name": "purchase step", "use_transaction_id": "Purchase"}
    ]
  }
]`

const fixtureBadSpecJSON = `{ "name": "GOLDEN-BAD", this is not json`

// writeFixtures materialises the shared fixture files once per run; each case
// copies them into its own temp CWD, so cases never mutate the originals.
func writeFixtures() (string, error) {
	dir, err := os.MkdirTemp("", "jiso-golden-fixtures-*")
	if err != nil {
		return "", fmt.Errorf("create fixtures dir: %w", err)
	}

	files := map[string]string{
		"spec.json":     fixtureSpecJSON,
		"tx.json":       fixtureTxJSON,
		"bad-spec.json": fixtureBadSpecJSON,
	}

	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			_ = os.RemoveAll(dir)

			return "", fmt.Errorf("write fixture %s: %w", name, err)
		}
	}

	if err := buildFixtureDB(filepath.Join(dir, "session.db")); err != nil {
		_ = os.RemoveAll(dir)

		return "", fmt.Errorf("build fixture db: %w", err)
	}

	// PAR-307: analyze goldens run against a real tiny pcap (two server
	// ports, generated from spec.json so the framing always matches).
	if err := buildAnalyzePCAP(filepath.Join(dir, "analyze.pcap"), filepath.Join(dir, "spec.json")); err != nil {
		_ = os.RemoveAll(dir)

		return "", fmt.Errorf("build analyze pcap: %w", err)
	}

	return dir, nil
}

// buildFixtureDB uses the repo's own db package so the schema can never
// drift from what the CLI queries: one plain session plus one approved
// Visa-style transaction (enough for `db stats <id> --json`,
// `ctf export --dry-run`, and the PAR-305 `db tx` reconstructed view), and
// one Visa-headered session with one approved transaction (enough for
// `ctf list` and a real `ctf export`, PAR-308). The stored request/response
// JSON uses MessageToJSONWithSpec's canonical {mti, fields} shape so
// db.Reconstruct packs and describes a real message (fields the fixture
// spec lacks are skipped, as in production).
func buildFixtureDB(path string) error {
	if err := db.InitDB(path); err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	err := db.UpsertSession(
		fixtureSession, "spec.json", "Golden", "tx.json", "tx",
		"127.0.0.1", "19999", "caller", "ascii4", "closed", false,
	)
	if err != nil {
		return err
	}

	response := `{"mti":"0210","fields":{"39":"00"}}`
	rec := &db.EnrichedTransactionRecord{
		SessionID: fixtureSession,
		TxName:    "Purchase",
		RequestJSON: `{"mti":"0200","fields":{"2":"4242424242424242","3":"000000","4":"1000","12":"120000","13":"0907",` +
			`"18":"5999","22":"021","38":"AUTH001","39":"00","41":"77973588","42":"123456789012345",` +
			`"43":"GOLDEN MERCHANT          LASA        USA","49":"840"}}`,
		ResponseJSON:     &response,
		ProcessingTimeMs: 5,
		Success:          true,
		ResponseCode:     "00",
		TxFileName:       "tx",
		SpecName:         "Golden",
	}

	if err := db.InsertTransactionEnriched(rec); err != nil {
		return err
	}

	if err := db.UpsertSession(
		fixtureVisaSession, "specs/visa.json", "visa.json", "tx.json", "tx.json",
		"127.0.0.1", "20001", "caller", "Visa", "closed", false,
	); err != nil {
		return err
	}

	visaResponse := `{"0":"0110","39":"00"}`
	visaRec := &db.EnrichedTransactionRecord{
		SessionID: fixtureVisaSession,
		TxName:    "Visa Purchase",
		RequestJSON: `{"fields":{"2":"4242424242424242","3":"000000","4":"1000","12":"120000","13":"0907",` +
			`"18":"5999","22":"021","38":"AUTH001","39":"00","41":"77973588","42":"123456789012345",` +
			`"43":"GOLDEN MERCHANT          LASA        USA","49":"840"}}`,
		ResponseJSON:     &visaResponse,
		ProcessingTimeMs: 5,
		Success:          true,
		ResponseCode:     "00",
		TxFileName:       "tx.json",
		SpecName:         "visa.json",
	}

	return db.InsertTransactionEnriched(visaRec)
}

func TestGolden(t *testing.T) {
	if buildErr != nil {
		t.Skipf("golden harness skipped: %v", buildErr)
	}

	if fixtureErr != nil {
		t.Skipf("golden harness skipped: %v", fixtureErr)
	}

	paths, err := filepath.Glob(filepath.Join(goldenDir, "*"+goldenSuffix))
	if err != nil {
		t.Fatalf("glob golden files: %v", err)
	}

	sort.Strings(paths)

	if len(paths) == 0 {
		t.Fatalf("no golden files found in %s", goldenDir)
	}

	for _, path := range paths {
		name := strings.TrimSuffix(filepath.Base(path), goldenSuffix)
		t.Run(name, func(t *testing.T) {
			runGoldenCase(t, path, name)
		})
	}
}

func runGoldenCase(t *testing.T, path, name string) {
	t.Helper()

	c := loadGoldenCase(t, path)

	if caseWantsLiveServer(c) && liveServerErr != nil {
		t.Skipf("live mock server unavailable: %v", liveServerErr)
	}

	work, err := os.MkdirTemp("", "jiso-golden-case-*")
	if err != nil {
		t.Fatalf("create case workdir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(work) })

	if err := copyFixtures(work); err != nil {
		t.Fatalf("copy fixtures: %v", err)
	}

	if err := writeSetupFiles(c, work); err != nil {
		t.Fatalf("setup files: %v", err)
	}

	stdout, stderr, code := runBinary(t, c, work)

	if updateMode {
		updateGolden(t, path, name, c, stdout, stderr, code)

		return
	}

	if fail := checkCase(c, work, stdout, stderr, code); len(fail) > 0 {
		t.Errorf("golden case %q failed (%d issues):\n  %s\n--- stdout ---\n%s\n--- stderr ---\n%s",
			name, len(fail), strings.Join(fail, "\n  "), stdout, stderr)
	}
}

func loadGoldenCase(t *testing.T, path string) *goldenCase {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file: %v", err)
	}

	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()

	c := &goldenCase{}
	if err := dec.Decode(c); err != nil {
		t.Fatalf("parse golden file %s: %v", path, err)
	}

	return c
}

func copyFixtures(work string) error {
	entries, err := os.ReadDir(fixtureDir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		src := filepath.Join(fixtureDir, e.Name())
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}

		if err := os.WriteFile(filepath.Join(work, e.Name()), data, 0o600); err != nil {
			return err
		}
	}

	return nil
}

// caseWantsLiveServer reports whether a case references the $LIVE_PORT
// placeholder. Live cases must keep their pinned stdout free of the
// ephemeral port (run them under --json) so GOLDEN_UPDATE stays deterministic.
func caseWantsLiveServer(c *goldenCase) bool {
	return strings.Contains(strings.Join(c.Args, " "), livePortPlaceholder)
}

// resolvePlaceholders substitutes $LIVE_PORT, $DEAD_PID, and $WORK.
func resolvePlaceholders(s, work string) string {
	s = strings.ReplaceAll(s, livePortPlaceholder, livePort)
	s = strings.ReplaceAll(s, deadPIDPlaceholder, deadPID)

	return strings.ReplaceAll(s, workPlaceholder, work)
}

// writeSetupFiles materialises a case's setup_files before it runs, so
// stale-state goldens can pin reader behaviour without booting a doomed
// server process.
func writeSetupFiles(c *goldenCase, work string) error {
	paths := make([]string, 0, len(c.SetupFiles))
	for p := range c.SetupFiles {
		paths = append(paths, p)
	}

	sort.Strings(paths)

	for _, p := range paths {
		full := resolvePlaceholders(p, work)
		if !filepath.IsAbs(full) {
			full = filepath.Join(work, full)
		}

		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			return fmt.Errorf("setup_files dir %s: %w", p, err)
		}

		content := resolvePlaceholders(c.SetupFiles[p], work)
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			return fmt.Errorf("setup_files %s: %w", p, err)
		}
	}

	return nil
}

// runBinary execs the built jiso in work with the clean env and returns the
// captured streams and the process exit code.
func runBinary(t *testing.T, c *goldenCase, work string) (string, string, int) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), caseTimeout)
	defer cancel()

	resolvedArgs := make([]string, 0, len(c.Args))
	for _, a := range c.Args {
		resolvedArgs = append(resolvedArgs, resolvePlaceholders(a, work))
	}

	cmd := exec.CommandContext(ctx, binaryPath, resolvedArgs...)
	cmd.Dir = work
	cmd.Env = caseEnv(c, work)
	cmd.Stdin = strings.NewReader(c.Stdin)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	code := 0

	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			if ctx.Err() == context.DeadlineExceeded {
				t.Fatalf("case timed out after %v (stdin=%q stderr=%q)", caseTimeout, c.Stdin, stderr.String())
			}

			t.Fatalf("exec %s: %v", binaryPath, err)
		}

		code = exitErr.ExitCode()
	}

	return stdout.String(), stderr.String(), code
}

// caseEnv builds the clean environment: allowlist + cleared JISO_* + a
// JISO_CONFIG pointing at a nonexistent path (the developer's user config
// must never leak in), then the case's own env last so it can override.
func caseEnv(c *goldenCase, work string) []string {
	home := filepath.Join(work, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		home = work
	}

	env := []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + home,
		"TMPDIR=" + os.TempDir(),
		"TERM=dumb",
		"USER=jiso-golden",
		"LOGNAME=jiso-golden",
		"SHELL=/bin/sh",
	}
	for _, k := range jisoEnvVars {
		env = append(env, k+"=")
	}

	keys := make([]string, 0, len(c.Env))
	for k := range c.Env {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	env = append(env, "JISO_CONFIG="+filepath.Join(work, "absent-config.yaml"))
	// The shared side-channel dir keeps the real XDG state dir untouched;
	// a case's own JISO_STATE_DIR (appended below) overrides it.
	if sideChannelDir != "" {
		env = append(env, "JISO_STATE_DIR="+sideChannelDir)
	}

	for _, k := range keys {
		env = append(env, k+"="+resolvePlaceholders(c.Env[k], work))
	}

	return env
}

// checkCase returns one human-readable failure per violated assertion.
func checkCase(c *goldenCase, work, stdout, stderr string, code int) []string {
	var fail []string

	if code != c.ExpectExit {
		fail = append(fail, fmt.Sprintf("exit code: got %d, want %d", code, c.ExpectExit))
	}

	fail = append(fail, checkStdout(c, stdout)...)
	fail = append(fail, checkJSONFields(c, stdout, work)...)
	fail = append(fail, checkJSONFieldsGT(c, stdout)...)

	for _, sub := range c.StderrContains {
		sub = resolvePlaceholders(sub, work)
		if !strings.Contains(stderr, sub) {
			fail = append(fail, fmt.Sprintf("stderr missing %q", sub))
		}
	}

	if c.StderrEmpty && stderr != "" {
		fail = append(fail, fmt.Sprintf("stderr must be empty, got %q", stderr))
	}

	for _, sub := range c.ForbidStdoutSubstrings {
		if strings.Contains(stdout, sub) {
			fail = append(fail, fmt.Sprintf("stdout must not contain %q", sub))
		}
	}

	for _, sub := range c.ForbidStderrSubstrings {
		sub = resolveWork(sub, work)
		if strings.Contains(stderr, sub) {
			fail = append(fail, fmt.Sprintf("stderr must not contain %q", sub))
		}
	}

	for _, p := range c.FilesMustExist {
		p = resolveWork(p, work)
		if _, err := os.Stat(p); err != nil {
			fail = append(fail, fmt.Sprintf("file must exist: %s (%v)", p, err))
		}
	}

	for _, p := range c.FilesMustNotExist {
		p = resolveWork(p, work)
		if _, err := os.Stat(p); err == nil {
			fail = append(fail, fmt.Sprintf("file must NOT exist: %s", p))
		}
	}

	fail = append(fail, checkFileHeads(c, work)...)

	return fail
}

// checkFileHeads asserts each expect_file_heads prefix against the file's
// first bytes (PAR-308: a written CTF file must start with its record
// structure, not just exist).
func checkFileHeads(c *goldenCase, work string) []string {
	if len(c.ExpectFileHeads) == 0 {
		return nil
	}

	paths := make([]string, 0, len(c.ExpectFileHeads))
	for p := range c.ExpectFileHeads {
		paths = append(paths, p)
	}

	sort.Strings(paths)

	var fail []string

	for _, p := range paths {
		full := resolveWork(p, work)

		data, err := os.ReadFile(full)
		if err != nil {
			fail = append(fail, fmt.Sprintf("file for head check unreadable: %s (%v)", full, err))

			continue
		}

		prefix := resolvePlaceholders(c.ExpectFileHeads[p], work)
		if !strings.HasPrefix(string(data), prefix) {
			fail = append(fail, fmt.Sprintf("file %s does not start with %q (got %q)", full, prefix, firstLine(string(data))))
		}
	}

	return fail
}

// checkJSONFields asserts the pinned expect_json_fields literals against
// the stdout JSON object (PAR-303). Literals support $LIVE_PORT and
// $DEAD_PID so ephemeral-but-known values can still be pinned (PAR-304).
func checkJSONFields(c *goldenCase, stdout, work string) []string {
	if len(c.JSONFields) == 0 {
		return nil
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &obj); err != nil {
		return []string{fmt.Sprintf("expect_json_fields needs a JSON object on stdout: %v", err)}
	}

	keys := make([]string, 0, len(c.JSONFields))
	for k := range c.JSONFields {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	var fail []string

	for _, k := range keys {
		raw, ok := obj[k]
		if !ok {
			fail = append(fail, fmt.Sprintf("json key %q missing (keys: %s)", k, joinSortedKeys(obj)))

			continue
		}

		var got, want bytes.Buffer
		if err := json.Compact(&got, raw); err != nil {
			fail = append(fail, fmt.Sprintf("json field %q unparsable: %v", k, err))

			continue
		}

		wantLiteral := resolvePlaceholders(c.JSONFields[k], work)
		if err := json.Compact(&want, []byte(wantLiteral)); err != nil {
			fail = append(fail, fmt.Sprintf("expect_json_fields[%q] is not valid JSON: %v", k, err))

			continue
		}

		if got.String() != want.String() {
			fail = append(fail, fmt.Sprintf("json field %q: got %s, want %s", k, got.String(), want.String()))
		}
	}

	return fail
}

// checkJSONFieldsGT asserts the numeric lower bounds of
// expect_json_fields_gt against the stdout JSON object (PAR-306: live
// stress counts must be > 0 without pinning volatile exact values).
func checkJSONFieldsGT(c *goldenCase, stdout string) []string {
	if len(c.ExpectJSONFieldsGT) == 0 {
		return nil
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stdout), &obj); err != nil {
		return []string{fmt.Sprintf("expect_json_fields_gt needs a JSON object on stdout: %v", err)}
	}

	keys := make([]string, 0, len(c.ExpectJSONFieldsGT))
	for k := range c.ExpectJSONFieldsGT {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	var fail []string

	for _, k := range keys {
		raw, ok := obj[k]
		if !ok {
			fail = append(fail, fmt.Sprintf("json key %q missing (keys: %s)", k, joinSortedKeys(obj)))

			continue
		}

		var got float64
		if err := json.Unmarshal(raw, &got); err != nil {
			fail = append(fail, fmt.Sprintf("json field %q is not a number: %s", k, raw))

			continue
		}

		if bound := c.ExpectJSONFieldsGT[k]; got <= bound {
			fail = append(fail, fmt.Sprintf("json field %q: got %v, want > %v", k, got, bound))
		}
	}

	return fail
}

func checkStdout(c *goldenCase, stdout string) []string {
	var fail []string

	switch c.Mode {
	case "", "contains":
		for _, sub := range c.Stdout {
			if !strings.Contains(stdout, sub) {
				fail = append(fail, fmt.Sprintf("stdout missing %q", sub))
			}
		}
	case "exact":
		want := ""
		if len(c.Stdout) > 0 {
			want = strings.Join(c.Stdout, "\n") + "\n"
		}

		if stdout != want {
			fail = append(fail, fmt.Sprintf("stdout not exact:\ngot:  %q\nwant: %q", stdout, want))
		}
	case "prefix":
		if len(c.Stdout) == 0 {
			fail = append(fail, "prefix mode needs at least one entry")
		} else {
			if !strings.HasPrefix(stdout, c.Stdout[0]) {
				fail = append(fail, fmt.Sprintf("stdout does not start with %q", c.Stdout[0]))
			}

			for _, sub := range c.Stdout[1:] {
				if !strings.Contains(stdout, sub) {
					fail = append(fail, fmt.Sprintf("stdout missing %q", sub))
				}
			}
		}
	case "json_keys":
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(stdout), &obj); err != nil {
			fail = append(fail, fmt.Sprintf("stdout is not a JSON object: %v", err))
		} else {
			for _, key := range c.Stdout {
				if _, ok := obj[key]; !ok {
					fail = append(fail, fmt.Sprintf("json key %q missing (keys: %s)", key, joinSortedKeys(obj)))
				}
			}
		}
	case "json_array":
		var arr []json.RawMessage
		if err := json.Unmarshal([]byte(stdout), &arr); err != nil {
			fail = append(fail, fmt.Sprintf("stdout is not a pure JSON array: %v", err))
		} else {
			for _, sub := range c.Stdout {
				if !strings.Contains(stdout, sub) {
					fail = append(fail, fmt.Sprintf("stdout missing %q", sub))
				}
			}
		}
	case "empty":
		if len(c.Stdout) != 0 {
			fail = append(fail, "empty mode must not pin stdout entries")
		}

		if stdout != "" {
			fail = append(fail, fmt.Sprintf("stdout must be empty, got %q", firstLine(stdout)))
		}
	default:
		fail = append(fail, fmt.Sprintf("unknown expect_stdout_mode %q", c.Mode))
	}

	return fail
}

func joinSortedKeys(obj map[string]json.RawMessage) string {
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	return strings.Join(keys, ", ")
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + "..."
	}

	return s
}

// resolveWork resolves a file-assertion path: $WORK (plus $LIVE_PORT and
// $DEAD_PID) is substituted, and relative paths resolve against the case's
// isolated CWD (the case itself runs with that CWD).
func resolveWork(p, work string) string {
	p = resolvePlaceholders(p, work)
	if !filepath.IsAbs(p) {
		p = filepath.Join(work, p)
	}

	return p
}

// updateGolden regenerates expect_* fields from actual output, prints a
// diff-like summary, and rewrites the file when anything changed.
func updateGolden(t *testing.T, path, name string, c *goldenCase, stdout, stderr string, code int) {
	t.Helper()

	old := *c
	changed := false

	var summary []string

	record := func(field, was, now string) {
		changed = true
		summary = append(summary, fmt.Sprintf("  - %s: %s\n  + %s: %s", field, was, field, now))
	}

	if code != c.ExpectExit {
		oldExit := c.ExpectExit
		c.ExpectExit = code
		record("expect_exit", fmt.Sprint(oldExit), fmt.Sprint(code))
	}

	mode, list := deriveStdout(stdout)
	if fail := checkStdout(&goldenCase{Mode: old.Mode, Stdout: old.Stdout}, stdout); len(fail) > 0 {
		c.Mode, c.Stdout = mode, list
		record("expect_stdout", fmt.Sprintf("%s %q", old.Mode, old.Stdout), fmt.Sprintf("%s %q", mode, list))
	}

	if fail := containsFailures(old.StderrContains, stderr); len(fail) > 0 {
		c.StderrContains = stableLines(stderr)
		record("expect_stderr_contains", fmt.Sprintf("%q", old.StderrContains), fmt.Sprintf("%q", c.StderrContains))
	}

	if old.StderrEmpty != (stderr == "") {
		c.StderrEmpty = stderr == ""
		record("expect_stderr_empty", fmt.Sprint(old.StderrEmpty), fmt.Sprint(c.StderrEmpty))
	}

	if !changed {
		t.Logf("GOLDEN %s: unchanged", name)

		return
	}

	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		t.Fatalf("marshal updated golden: %v", err)
	}

	raw = append(raw, '\n')

	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write updated golden: %v", err)
	}

	t.Logf("GOLDEN %s: UPDATED\n%s", name, strings.Join(summary, "\n"))
}

// deriveStdout produces a deterministic assertion for the actual stdout.
func deriveStdout(stdout string) (string, []string) {
	if stdout == "" {
		return "empty", nil
	}

	return "contains", stableLines(stdout)
}

// stableLines splits output into lines, dropping empty and volatile
// (timestamp-bearing) lines so regenerated goldens stay deterministic.
func stableLines(s string) []string {
	lines := strings.Split(s, "\n")

	out := make([]string, 0, len(lines))
	for _, l := range lines {
		l = strings.TrimRight(l, " \t\r")
		if l == "" || volatileLine.MatchString(l) {
			continue
		}

		out = append(out, l)
	}

	return out
}

func containsFailures(subs []string, s string) []string {
	var fail []string
	for _, sub := range subs {
		if !strings.Contains(s, sub) {
			fail = append(fail, sub)
		}
	}

	return fail
}
