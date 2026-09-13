package output

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFlaggedCmd registers the output flags on the local set, mirroring the
// merged view that commands see inside RunE at execution time.
func newFlaggedCmd(out, errOut *bytes.Buffer, jsonFlag, quiet, dryRun bool) *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	f := cmd.Flags()
	f.Bool("json", jsonFlag, "")
	f.BoolP("quiet", "q", quiet, "")
	f.BoolP("dry-run", "n", dryRun, "")
	cmd.SetOut(out)
	cmd.SetErr(errOut)

	return cmd
}

func TestNewReadsFlags(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		json, quiet, dry bool
		wantJSON         bool
		wantQuiet        bool
		wantDry          bool
	}{
		{"all off", false, false, false, false, false, false},
		{"json on", true, false, false, true, false, false},
		{"quiet on", false, true, false, false, true, false},
		{"dry-run on", false, false, true, false, false, true},
		{"all on", true, true, true, true, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errBuf bytes.Buffer
			cmd := newFlaggedCmd(&out, &errBuf, tt.json, tt.quiet, tt.dry)

			r := New(cmd)
			assert.Equal(t, tt.wantJSON, r.JSON())
			assert.Equal(t, tt.wantQuiet, r.Quiet())
			assert.Equal(t, tt.wantDry, r.DryRun())
		})
	}
}

func TestNewWithoutFlagsDefaultsOff(t *testing.T) {
	t.Parallel()

	var out, errBuf bytes.Buffer
	cmd := &cobra.Command{Use: "test"}
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)

	r := New(cmd)
	assert.False(t, r.JSON())
	assert.False(t, r.Quiet())
	assert.False(t, r.DryRun())
}

func TestRendererData(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		jsonMode   bool
		wantJSON   bool
		wantHuman  bool
		wantOutput string
	}{
		{
			name:       "json mode encodes with two-space indent and skips human",
			jsonMode:   true,
			wantJSON:   true,
			wantHuman:  false,
			wantOutput: "{\n  \"count\": 2,\n  \"name\": \"purchase\"\n}\n",
		},
		{
			name:      "human mode delegates to printer",
			jsonMode:  false,
			wantJSON:  false,
			wantHuman: true,
		},
	}

	payload := map[string]any{"name": "purchase", "count": 2}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errBuf bytes.Buffer
			cmd := newFlaggedCmd(&out, &errBuf, tt.jsonMode, false, false)
			r := New(cmd)

			humanCalled := false
			err := r.Data(payload, func() {
				humanCalled = true
				if tt.wantHuman {
					_, _ = r.Out().Write([]byte("human: purchase"))
				}
			})

			require.NoError(t, err)
			assert.Equal(t, tt.wantHuman, humanCalled)

			if tt.wantJSON {
				assert.Equal(t, tt.wantOutput, out.String())
				assert.True(t, json.Valid(out.Bytes()))
			} else {
				assert.Equal(t, "human: purchase", out.String())
			}
		})
	}
}

func TestRendererDataNilHumanInJSONMode(t *testing.T) {
	t.Parallel()

	var out, errBuf bytes.Buffer
	cmd := newFlaggedCmd(&out, &errBuf, true, false, false)
	r := New(cmd)

	require.NoError(t, r.Data([]int{1, 2}, nil))
	assert.JSONEq(t, "[1, 2]", out.String())
}

func TestRendererNoticefSuppression(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		json, quiet bool
		wantPrinted bool
	}{
		{"plain prints", false, false, true},
		{"quiet suppresses", false, true, false},
		{"json suppresses", true, false, false},
		{"json+quiet suppresses", true, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errBuf bytes.Buffer
			cmd := newFlaggedCmd(&out, &errBuf, tt.json, tt.quiet, false)
			r := New(cmd)

			r.Noticef("notice %d", 1)

			if tt.wantPrinted {
				assert.Equal(t, "notice 1\n", out.String())
			} else {
				assert.Empty(t, out.String())
			}
			assert.Empty(t, errBuf.String())
		})
	}
}
