// Package rex controls Rex through its installed CLI.
package rex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Client struct {
	Session string
	// Executable defaults to "rex"; tests may substitute a fake executable.
	Executable string
}

func New(session string) *Client { return &Client{Session: session} }

type View struct {
	Windows        []Window `json:"windows"`
	ActiveWindowID string   `json:"active_window_id"`
}
type Window struct {
	ID      string  `json:"window_id"`
	Focused string  `json:"focused_block_id"`
	Layers  []Layer `json:"layers"`
}
type Layer struct {
	ID     string     `json:"layer_id"`
	Kind   string     `json:"kind"`
	Bounds [4]float64 `json:"bounds"`
	Layout *Node      `json:"layout"`
	Blocks []Block    `json:"blocks"`
}
type Node struct {
	BlockID string     `json:"block_id"`
	Rect    [4]float64 `json:"rect"`
	Split   *Split     `json:"split"`
}
type Split struct {
	ID        string  `json:"split_id"`
	Direction string  `json:"direction"`
	Ratio     float64 `json:"ratio"`
	Before    *Node   `json:"before"`
	After     *Node   `json:"after"`
}
type Block struct {
	ID string `json:"block_id"`
}

// Error identifies a failed Rex operation that can be retried without stopping Neovim.
type Error struct{ Err error }

func (e *Error) Error() string     { return e.Err.Error() }
func (e *Error) Unwrap() error     { return e.Err }
func IsRecoverable(err error) bool { var target *Error; return errors.As(err, &target) }

func (c *Client) run(ctx context.Context, args []string, out any) (resultErr error) {
	defer func() {
		if resultErr != nil {
			resultErr = &Error{Err: resultErr}
		}
	}()
	if c.Session == "" {
		return errors.New("rex: session is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	executable := c.Executable
	if executable == "" {
		executable = "rex"
	}
	argv := append([]string{"--autostart=false", "--timeout=5s"}, args...)
	cmd := exec.CommandContext(ctx, executable, argv...)
	cmd.WaitDelay = 100 * time.Millisecond
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return fmt.Errorf("rex %s: %w; stderr: %s; stdout: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()), strings.TrimSpace(stdout.String()))
	}
	if out != nil {
		if err := json.Unmarshal(stdout.Bytes(), out); err != nil {
			return fmt.Errorf("rex %s: decode response: %w; stdout: %s; stderr: %s", strings.Join(args, " "), err, stdout.String(), stderr.String())
		}
	}
	return nil
}

func (c *Client) call(ctx context.Context, method string, args map[string]any, out any) error {
	raw, err := json.Marshal(args)
	if err != nil {
		return fmt.Errorf("rex %s: encode arguments: %w", method, err)
	}
	return c.run(ctx, []string{"api", "call", "-s", c.Session, method, string(raw)}, out)
}

func (c *Client) View(ctx context.Context) (View, error) {
	var v View
	err := c.call(ctx, "session.view", map[string]any{}, &v)
	return v, err
}

func validatePlacement(direction string, ratio float64) error {
	switch direction {
	case "right", "left", "above", "below":
	default:
		return fmt.Errorf("rex: invalid direction %q (want right, left, above, below)", direction)
	}
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio < 0 || ratio > 1 {
		return fmt.Errorf("rex: ratio must be a finite fraction in [0,1], got %v", ratio)
	}
	return nil
}

// Split gives the new block ratio of the space. The CLI accepts whole
// percentages 1..99, so ratios are rounded and clamped to that range.
func (c *Client) Split(ctx context.Context, anchor, direction string, ratio float64, command []string) (string, error) {
	if err := validatePlacement(direction, ratio); err != nil {
		return "", err
	}
	if anchor == "" {
		return "", errors.New("rex split: anchor is required")
	}
	pct := int(math.Round(ratio * 100))
	pct = max(1, min(99, pct))
	args := []string{"split", "-s", c.Session, "-b", anchor, "--split=" + direction, "--ratio", strconv.Itoa(pct), "--shell=none", "--focus=false", "--json", "--"}
	args = append(args, command...)
	var result struct {
		BlockIDs []string `json:"block_ids"`
	}
	if err := c.run(ctx, args, &result); err != nil {
		return "", err
	}
	if len(result.BlockIDs) == 0 || result.BlockIDs[0] == "" {
		return "", errors.New("rex split: response has no new block ID")
	}
	return result.BlockIDs[0], nil
}

func (c *Client) Focus(ctx context.Context, block string) error {
	if block == "" {
		return errors.New("rex focus: block is required")
	}
	return c.call(ctx, "session.focus_block", map[string]any{"block_id": block}, nil)
}
func (c *Client) Close(ctx context.Context, block string) error {
	if block == "" {
		return errors.New("rex close: block is required")
	}
	return c.call(ctx, "block.close", map[string]any{"block_id": block}, nil)
}

// Move uses the API's before-side ratio, converting from the moved block's
// fraction. Endpoints are clamped because Rex requires a nonempty split.
func (c *Client) Move(ctx context.Context, block, direction, anchor string, ratio float64) error {
	if err := validatePlacement(direction, ratio); err != nil {
		return err
	}
	if block == "" || anchor == "" || block == anchor {
		return errors.New("rex move: distinct block and anchor are required")
	}
	ratio = max(0.01, min(0.99, ratio))
	axis, side := "horizontal", "after"
	if direction == "above" || direction == "below" {
		axis = "vertical"
	}
	if direction == "left" || direction == "above" {
		side = "before"
	} else {
		ratio = 1 - ratio
	}
	return c.call(ctx, "session.move_block", map[string]any{"block_id": block, "anchor_block_id": anchor, "direction": axis, "side": side, "ratio": ratio}, nil)
}

// SetRatio sets the fraction assigned to the before (left or top) side of
// a horizontal or vertical split. Ratio must be strictly between 0 and 1.
func (c *Client) SetRatio(ctx context.Context, before, after, direction string, ratio float64) error {
	if before == "" || after == "" || before == after {
		return errors.New("rex set ratio: distinct before and after blocks are required")
	}
	if direction != "horizontal" && direction != "vertical" {
		return fmt.Errorf("rex set ratio: invalid direction %q (want horizontal or vertical)", direction)
	}
	if math.IsNaN(ratio) || math.IsInf(ratio, 0) || ratio <= 0 || ratio >= 1 {
		return fmt.Errorf("rex set ratio: ratio must be finite and strictly between 0 and 1, got %v", ratio)
	}
	return c.call(ctx, "session.resize_pane", map[string]any{
		"before_block_id": before, "after_block_id": after,
		"direction": direction, "ratio": ratio,
	}, nil)
}

func validateBounds(bounds [4]float64) error {
	for _, v := range bounds {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return errors.New("bounds must be finite")
		}
	}
	if bounds[0] < 0 || bounds[1] < 0 || bounds[2] <= 0 || bounds[3] <= 0 || bounds[0]+bounds[2] > 1 || bounds[1]+bounds[3] > 1 {
		return errors.New("bounds [x,y,w,h] must fit inside the window with positive dimensions")
	}
	return nil
}

func (c *Client) Float(ctx context.Context, window string, bounds [4]float64, command []string) (blockID, layerID string, err error) {
	if err := validateBounds(bounds); err != nil {
		return "", "", fmt.Errorf("rex float: %w", err)
	}
	args := map[string]any{
		"bounds": bounds, "focus": false,
		"layout": map[string]any{"block": map[string]any{
			"flavor":  "com.superlogical.terminal.shell",
			"options": map[string]any{"shell": "none", "command": command},
		}},
	}
	if window != "" {
		args["window_id"] = window
	}
	var result struct {
		BlockIDs []string `json:"block_ids"`
		LayerID  string   `json:"layer_id"`
	}
	if err = c.call(ctx, "session.new_layer", args, &result); err != nil {
		return "", "", err
	}
	if len(result.BlockIDs) == 0 || result.BlockIDs[0] == "" || result.LayerID == "" {
		return "", result.LayerID, errors.New("rex float: response missing block or layer ID")
	}
	return result.BlockIDs[0], result.LayerID, nil
}

// ResizeLayer moves or resizes a floating layer without changing its focus.
func (c *Client) ResizeLayer(ctx context.Context, layer string, bounds [4]float64) error {
	if layer == "" {
		return errors.New("rex resize layer: layer is required")
	}
	if err := validateBounds(bounds); err != nil {
		return fmt.Errorf("rex resize layer: %w", err)
	}
	return c.call(ctx, "session.resize_layer", map[string]any{"layer_id": layer, "bounds": bounds}, nil)
}

// RaiseLayer places a float directly above another float without changing focus.
func (c *Client) RaiseLayer(ctx context.Context, layer, relative string) error {
	if layer == "" || relative == "" || layer == relative {
		return errors.New("rex raise layer: distinct layer and relative layer are required")
	}
	return c.call(ctx, "session.raise_layer", map[string]any{"layer_id": layer, "relative_layer_id": relative}, nil)
}

func (c *Client) CloseLayer(ctx context.Context, layer string) error {
	if layer == "" {
		return errors.New("rex close layer: layer is required")
	}
	return c.call(ctx, "session.close_layer", map[string]any{"layer_id": layer}, nil)
}
