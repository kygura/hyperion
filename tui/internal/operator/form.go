package operator

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/hyperagent/tui/internal/apiclient"
)

// paramForm is the STRATEGIES param editor, generated from
// Manifest.params. Row 0 is always the venue selector (an enum over
// Manifest.venues); the rest follow manifest order. Number/string rows edit
// in a textinput; bool/enum rows cycle with left/right/space/enter.
type paramForm struct {
	strategyID string
	base       apiclient.StrategyConfig // config being edited (params preserved)
	fields     []formField
	cursor     int
	err        string // form-level error (server 400 without a known field)
}

type formField struct {
	spec  apiclient.ParamSpec
	venue bool // the synthetic venue row

	input textinput.Model // number / string
	on    bool            // bool
	idx   int             // enum: index into spec.Options
	err   string
}

const venueKey = "venue"

// newParamForm builds the editor for a strategy from its status.
func newParamForm(s apiclient.StrategyStatus) *paramForm {
	f := &paramForm{strategyID: s.Manifest.ID, base: cloneConfig(s.Config)}
	if f.base.ID == "" {
		f.base.ID = s.Manifest.ID
	}

	venueSpec := apiclient.ParamSpec{Key: venueKey, Type: apiclient.ParamEnum, Label: "Venue", Options: s.Manifest.Venues}
	if len(venueSpec.Options) == 0 && s.Config.Venue != "" {
		venueSpec.Options = []string{s.Config.Venue}
	}
	vf := formField{spec: venueSpec, venue: true}
	vf.idx = indexOf(venueSpec.Options, s.Config.Venue)
	f.fields = append(f.fields, vf)

	for _, spec := range s.Manifest.Params {
		cur, has := s.Config.Params[spec.Key]
		if !has {
			cur = spec.Default
		}
		ff := formField{spec: spec}
		switch spec.Type {
		case apiclient.ParamBool:
			ff.on, _ = cur.(bool)
		case apiclient.ParamEnum:
			str, _ := cur.(string)
			ff.idx = indexOf(spec.Options, str)
		default: // number, string, unknown → text
			ti := newInput("", 64)
			ti.SetValue(paramText(cur))
			ff.input = ti
		}
		f.fields = append(f.fields, ff)
	}
	f.focus()
	return f
}

// newInput builds a textinput with a static (non-blinking) cursor: a dense
// console has no use for the blink, and a static cursor means Focus()
// never schedules a timer cmd, which keeps Update synchronous and cheap.
func newInput(prompt string, limit int) textinput.Model {
	ti := textinput.New()
	ti.Prompt = prompt
	ti.CharLimit = limit
	st := ti.Styles()
	st.Cursor.Blink = false
	ti.SetStyles(st)
	return ti
}

func cloneConfig(c apiclient.StrategyConfig) apiclient.StrategyConfig {
	out := c
	out.Params = make(map[string]any, len(c.Params))
	for k, v := range c.Params {
		out.Params[k] = v
	}
	if c.Governor != nil {
		g := *c.Governor
		out.Governor = &g
	}
	return out
}

func indexOf(opts []string, v string) int {
	for i, o := range opts {
		if o == v {
			return i
		}
	}
	return 0
}

// paramText renders a param value for a textinput.
func paramText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case bool:
		return strconv.FormatBool(x)
	}
	return fmt.Sprint(v)
}

// focus moves textinput focus to the cursor row.
func (f *paramForm) focus() tea.Cmd {
	var cmd tea.Cmd
	for i := range f.fields {
		if f.fields[i].isText() {
			if i == f.cursor {
				cmd = f.fields[i].input.Focus()
			} else {
				f.fields[i].input.Blur()
			}
		}
	}
	return cmd
}

func (ff formField) isText() bool {
	return !ff.venue && ff.spec.Type != apiclient.ParamBool && ff.spec.Type != apiclient.ParamEnum
}

func (ff formField) isEnum() bool {
	return ff.venue || ff.spec.Type == apiclient.ParamEnum
}

// move shifts the cursor by delta, wrapping.
func (f *paramForm) move(delta int) tea.Cmd {
	n := len(f.fields)
	if n == 0 {
		return nil
	}
	f.cursor = ((f.cursor+delta)%n + n) % n
	return f.focus()
}

// cycle advances an enum/bool row by delta.
func (f *paramForm) cycle(delta int) {
	ff := &f.fields[f.cursor]
	switch {
	case ff.spec.Type == apiclient.ParamBool:
		ff.on = !ff.on
	case ff.isEnum():
		n := len(ff.spec.Options)
		if n > 0 {
			ff.idx = ((ff.idx+delta)%n + n) % n
		}
	}
	ff.err = ""
}

// handleKey routes a key press inside the form. done is true when the form
// should close (esc); save is true when ctrl+s validated cleanly and cfg
// holds the config to PUT.
func (f *paramForm) handleKey(msg tea.KeyPressMsg) (cmd tea.Cmd, done bool, save bool, cfg apiclient.StrategyConfig) {
	switch msg.String() {
	case "esc":
		return nil, true, false, cfg
	case "ctrl+s":
		cfg, ok := f.validate()
		return nil, false, ok, cfg
	case "up", "shift+tab":
		return f.move(-1), false, false, cfg
	case "down", "tab", "enter":
		if msg.String() == "enter" && !f.fields[f.cursor].isText() {
			f.cycle(1)
			return nil, false, false, cfg
		}
		return f.move(1), false, false, cfg
	case "left", "right", "space":
		if !f.fields[f.cursor].isText() {
			d := 1
			if msg.String() == "left" {
				d = -1
			}
			f.cycle(d)
			return nil, false, false, cfg
		}
	}
	ff := &f.fields[f.cursor]
	if ff.isText() {
		ff.err = ""
		f.err = ""
		var c tea.Cmd
		ff.input, c = ff.input.Update(msg)
		return c, false, false, cfg
	}
	return nil, false, false, cfg
}

// validate checks every row against its ParamSpec and returns the config to
// PUT. Row errors are stored on the rows; ok is false if any row failed.
func (f *paramForm) validate() (apiclient.StrategyConfig, bool) {
	cfg := cloneConfig(f.base)
	ok := true
	for i := range f.fields {
		ff := &f.fields[i]
		ff.err = ""
		if ff.venue {
			if len(ff.spec.Options) > 0 {
				cfg.Venue = ff.spec.Options[ff.idx]
			}
			continue
		}
		v, err := ff.value()
		if err != nil {
			ff.err = err.Error()
			ok = false
			continue
		}
		cfg.Params[ff.spec.Key] = v
	}
	return cfg, ok
}

// value parses and validates one row's current value.
func (ff *formField) value() (any, error) {
	spec := ff.spec
	switch spec.Type {
	case apiclient.ParamBool:
		return ff.on, nil
	case apiclient.ParamEnum:
		if len(spec.Options) == 0 {
			return nil, fmt.Errorf("no options")
		}
		return spec.Options[ff.idx], nil
	case apiclient.ParamNumber:
		raw := strings.TrimSpace(ff.input.Value())
		if raw == "" {
			return nil, fmt.Errorf("required")
		}
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, fmt.Errorf("not a number")
		}
		return v, validateNumber(v, spec)
	default: // string and unknown types
		return ff.input.Value(), nil
	}
}

// validateNumber enforces min/max/step. Step is measured from min when
// present, else from zero, with a small tolerance for float noise.
func validateNumber(v float64, spec apiclient.ParamSpec) error {
	if spec.Min != nil && v < *spec.Min {
		return fmt.Errorf("below min %s", paramText(*spec.Min))
	}
	if spec.Max != nil && v > *spec.Max {
		return fmt.Errorf("above max %s", paramText(*spec.Max))
	}
	if spec.Step != nil && *spec.Step > 0 {
		origin := 0.0
		if spec.Min != nil {
			origin = *spec.Min
		}
		q := (v - origin) / *spec.Step
		if math.Abs(q-math.Round(q)) > 1e-6 {
			return fmt.Errorf("not a multiple of step %s", paramText(*spec.Step))
		}
	}
	return nil
}

// setServerError attaches a daemon validation error to its row
// ("params.size_usd" → the size_usd row, "venue" → the venue row) or to the
// form when the field is unknown.
func (f *paramForm) setServerError(err error) {
	f.err = err.Error()
	apiErr, ok := err.(*apiclient.APIError)
	if !ok || apiErr.Field == "" {
		return
	}
	key := strings.TrimPrefix(apiErr.Field, "params.")
	for i := range f.fields {
		if f.fields[i].spec.Key == key {
			f.fields[i].err = apiErr.Message
			f.cursor = i
			f.focus()
			f.err = ""
			return
		}
	}
}

// label is the row's display label.
func (ff formField) label() string {
	if ff.spec.Label != "" {
		return ff.spec.Label
	}
	return ff.spec.Key
}

// bounds is the "min…max step" hint for a number row.
func (ff formField) bounds() string {
	spec := ff.spec
	if spec.Type != apiclient.ParamNumber {
		return ""
	}
	var parts []string
	if spec.Min != nil {
		parts = append(parts, "min "+paramText(*spec.Min))
	}
	if spec.Max != nil {
		parts = append(parts, "max "+paramText(*spec.Max))
	}
	if spec.Step != nil {
		parts = append(parts, "step "+paramText(*spec.Step))
	}
	return strings.Join(parts, " · ")
}
