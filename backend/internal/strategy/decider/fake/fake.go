// Package fake is the scripted Decider: answers keyed by question key, with a
// uniform-probability default for anything unscripted. It backs the strategy
// tests and the `kind = "fake"` config (paper trading without a Jev key).
package fake

import (
	"context"
	"fmt"
	"sync"

	"github.com/hyperagent/hyperagent/internal/strategy/decider"
)

// Model is the model id fake results report.
const Model = "fake"

// Decider returns scripted answers. Safe for concurrent use.
type Decider struct {
	mu     sync.Mutex
	script map[string]decider.Answer
	calls  int
	// Err, when set, is returned by every Evaluate (tests of the failure path).
	Err error
}

// New builds a fake with the given script (may be nil).
func New(script map[string]decider.Answer) *Decider {
	if script == nil {
		script = map[string]decider.Answer{}
	}
	return &Decider{script: script}
}

// Set scripts one answer.
func (d *Decider) Set(key string, a decider.Answer) {
	d.mu.Lock()
	d.script[key] = a
	d.mu.Unlock()
}

// Calls returns how many Evaluate calls have been made.
func (d *Decider) Calls() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

// Evaluate returns the scripted answer per question, or the uniform default.
// A scripted answer whose type mismatches the question is an error, so a
// test cannot silently script the wrong shape.
func (d *Decider) Evaluate(_ context.Context, _ any, questions map[string]decider.Question) (decider.Result, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	if d.Err != nil {
		return decider.Result{}, d.Err
	}
	out := make(map[string]decider.Answer, len(questions))
	for k, q := range questions {
		if a, ok := d.script[k]; ok {
			if a.Type != "" && a.Type != q.Type {
				return decider.Result{}, fmt.Errorf("fake: scripted answer %q is %s, question is %s", k, a.Type, q.Type)
			}
			a.Type = q.Type
			if a.Type == decider.TypeScore && a.Legend == nil {
				a.Legend = legend(q)
			}
			out[k] = a
			continue
		}
		out[k] = Uniform(q)
	}
	return decider.Result{
		Answers:   out,
		Model:     Model,
		Usage:     decider.Usage{InputTokens: 0, OutputTokens: 0},
		LatencyMs: 0,
	}, nil
}

// Uniform is the default answer: every option equally likely. choice picks
// the first option in sorted order with confidence 1/n, score sits at the
// middle level, noul is 0.5.
func Uniform(q decider.Question) decider.Answer {
	switch q.Type {
	case decider.TypeChoice:
		keys := q.ChoiceKeys()
		n := float64(len(keys))
		probs := make(map[string]float64, len(keys))
		for _, k := range keys {
			probs[k] = 1 / n
		}
		choice := ""
		if len(keys) > 0 {
			choice = keys[0]
		}
		return decider.Answer{Type: q.Type, Choice: choice, Probabilities: probs, Confidence: decider.F(1 / n)}
	case decider.TypeScore:
		levels := q.ScoreLevels()
		n := float64(len(levels))
		probs := make(map[string]float64, len(levels))
		for i := range levels {
			probs[fmt.Sprint(i)] = 1 / n
		}
		mid := (n - 1) / 2
		return decider.Answer{Type: q.Type, Score: decider.F(mid), Probabilities: probs, Legend: legend(q), Confidence: decider.F(1 / n)}
	default:
		return decider.Answer{Type: decider.TypeNoul, Noul: decider.F(0.5)}
	}
}

// legend maps level index → level text for a score question.
func legend(q decider.Question) map[string]decider.LegendEntry {
	levels := q.ScoreLevels()
	out := make(map[string]decider.LegendEntry, len(levels))
	for i, l := range levels {
		out[fmt.Sprint(i)] = decider.LegendEntry{What: l}
	}
	return out
}

// ChoiceAnswer is a test helper: a choice answer with the chosen option at p
// and the rest sharing 1-p, confidence = p.
func ChoiceAnswer(q decider.Question, choice string, p float64) decider.Answer {
	keys := q.ChoiceKeys()
	probs := make(map[string]float64, len(keys))
	rest := 0.0
	if len(keys) > 1 {
		rest = (1 - p) / float64(len(keys)-1)
	}
	for _, k := range keys {
		if k == choice {
			probs[k] = p
		} else {
			probs[k] = rest
		}
	}
	return decider.Answer{Type: decider.TypeChoice, Choice: choice, Probabilities: probs, Confidence: decider.F(p)}
}

// ScoreAnswer is a test helper: a score answer at value with the given confidence.
func ScoreAnswer(q decider.Question, value, conf float64) decider.Answer {
	levels := q.ScoreLevels()
	probs := make(map[string]float64, len(levels))
	for i := range levels {
		probs[fmt.Sprint(i)] = 0
	}
	lo := int(value)
	if lo >= len(levels) {
		lo = len(levels) - 1
	}
	if lo < 0 {
		lo = 0
	}
	probs[fmt.Sprint(lo)] = 1
	return decider.Answer{Type: decider.TypeScore, Score: decider.F(value), Probabilities: probs, Legend: legend(q), Confidence: decider.F(conf)}
}

// NoulAnswer is a test helper: a noul answer at p.
func NoulAnswer(p float64) decider.Answer {
	return decider.Answer{Type: decider.TypeNoul, Noul: decider.F(p)}
}
