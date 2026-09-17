package typesafe

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"math"
	"slices"
)

type SystemOneResult struct {
	Model     string
	Answers   map[string]Answer
	Usage     Usage
	RequestID string
}

// Usage fields are nil when the API omits them.
type Usage struct {
	InputTokens  *int `json:"input_tokens"`
	OutputTokens *int `json:"output_tokens"`
}

// Answer is a NoulAnswer, ChoiceAnswer, ScoreAnswer, or UnknownAnswer.
type Answer interface{ AnswerType() string }

type NoulAnswer struct {
	Noul float64 // probability of yes
}

type ChoiceAnswer struct {
	Choice string

	// Confidence describes how concentrated Probabilities is. It is not the
	// probability of Choice.
	Confidence    float64
	Probabilities map[string]float64
}

type ScoreAnswer struct {
	Score         float64 // expected level; may fall between levels
	Confidence    float64
	Legend        map[int]any
	Probabilities map[int]float64
}

// Level returns the rubric level nearest to Score.
func (a ScoreAnswer) Level() int { return int(math.Round(a.Score)) }

// UnknownAnswer is an answer type this version does not know.
type UnknownAnswer struct {
	Type string
	Raw  json.RawMessage
}

func (NoulAnswer) AnswerType() string      { return "noul" }
func (ChoiceAnswer) AnswerType() string    { return "choice" }
func (ScoreAnswer) AnswerType() string     { return "score" }
func (a UnknownAnswer) AnswerType() string { return a.Type }

func (r *SystemOneResult) Noul(name string) (NoulAnswer, bool) {
	a, ok := r.Answers[name].(NoulAnswer)
	return a, ok
}

func (r *SystemOneResult) Choice(name string) (ChoiceAnswer, bool) {
	a, ok := r.Answers[name].(ChoiceAnswer)
	return a, ok
}

func (r *SystemOneResult) Score(name string) (ScoreAnswer, bool) {
	a, ok := r.Answers[name].(ScoreAnswer)
	return a, ok
}

func decodeSystemOneResult(resp *response, questions map[string]wireQuestion) (*SystemOneResult, error) {
	var wire struct {
		Model   *string
		Answers map[string]json.RawMessage
		Usage   Usage
	}
	if err := json.Unmarshal(resp.body, &wire); err != nil {
		return nil, resp.invalid("", err)
	}
	if wire.Model == nil {
		return nil, resp.invalid("model", errors.New("is missing"))
	}
	if wire.Answers == nil {
		return nil, resp.invalid("answers", errors.New("is missing"))
	}
	for name := range wire.Answers {
		if _, ok := questions[name]; !ok {
			return nil, resp.invalid(fmt.Sprintf("answers[%q]", name), errors.New("answers a question that was not asked"))
		}
	}

	result := &SystemOneResult{
		Model:     *wire.Model,
		Answers:   make(map[string]Answer, len(questions)),
		Usage:     wire.Usage,
		RequestID: resp.requestID,
	}
	for _, name := range slices.Sorted(maps.Keys(questions)) {
		path := fmt.Sprintf("answers[%q]", name)
		raw, ok := wire.Answers[name]
		if !ok {
			return nil, resp.invalid(path, errors.New("is missing"))
		}
		answer, err := decodeAnswer(raw, questions[name])
		if err != nil {
			return nil, resp.invalid(path, err)
		}
		result.Answers[name] = answer
	}
	return result, nil
}

func decodeAnswer(raw json.RawMessage, q wireQuestion) (Answer, error) {
	var w struct {
		Type          string
		Noul          *float64
		Choice        *string
		Score         *float64
		Confidence    *float64
		Legend        map[int]any
		Probabilities json.RawMessage
	}
	if err := json.Unmarshal(raw, &w); err != nil {
		return nil, err
	}
	switch w.Type {
	case "":
		return nil, errors.New("type is missing")
	case "noul", "choice", "score":
		if w.Type != q.Type {
			return nil, fmt.Errorf("type %q does not match question type %q", w.Type, q.Type)
		}
	default:
		return UnknownAnswer{Type: w.Type, Raw: raw}, nil
	}

	if w.Type == "noul" {
		if err := checkUnit("noul", w.Noul); err != nil {
			return nil, err
		}
		return NoulAnswer{Noul: *w.Noul}, nil
	}
	if err := checkUnit("confidence", w.Confidence); err != nil {
		return nil, err
	}
	if len(w.Probabilities) == 0 {
		return nil, errors.New("probabilities is missing")
	}

	if w.Type == "choice" {
		labels := q.Criteria.(map[string]any)
		if w.Choice == nil {
			return nil, errors.New("choice is missing")
		}
		if _, ok := labels[*w.Choice]; !ok {
			return nil, fmt.Errorf("choice %q was not offered", *w.Choice)
		}
		var probabilities map[string]float64
		if err := json.Unmarshal(w.Probabilities, &probabilities); err != nil {
			return nil, fmt.Errorf("probabilities: %w", err)
		}
		for label, p := range probabilities {
			if _, ok := labels[label]; !ok || p < 0 || p > 1 {
				return nil, fmt.Errorf("probabilities: invalid entry %q: %v", label, p)
			}
		}
		for _, label := range slices.Sorted(maps.Keys(labels)) {
			if _, ok := probabilities[label]; !ok {
				return nil, fmt.Errorf("probabilities: missing label %q", label)
			}
		}
		return ChoiceAnswer{Choice: *w.Choice, Confidence: *w.Confidence, Probabilities: probabilities}, nil
	}

	top := len(q.Criteria.([]any)) - 1
	if w.Score == nil || *w.Score < 0 || *w.Score > float64(top) {
		return nil, fmt.Errorf("score is missing or outside [0, %d]", top)
	}
	if w.Legend == nil {
		return nil, errors.New("legend is missing")
	}
	var probabilities map[int]float64
	if err := json.Unmarshal(w.Probabilities, &probabilities); err != nil {
		return nil, fmt.Errorf("probabilities: %w", err)
	}
	for level := range w.Legend {
		if level < 0 || level > top {
			return nil, fmt.Errorf("legend: level %d is outside [0, %d]", level, top)
		}
	}
	for level, p := range probabilities {
		if level < 0 || level > top || p < 0 || p > 1 {
			return nil, fmt.Errorf("probabilities: invalid entry %d: %v", level, p)
		}
	}
	for level := 0; level <= top; level++ {
		if _, ok := probabilities[level]; !ok {
			return nil, fmt.Errorf("probabilities: missing level %d", level)
		}
		if _, ok := w.Legend[level]; !ok {
			return nil, fmt.Errorf("legend: missing level %d", level)
		}
	}
	return ScoreAnswer{Score: *w.Score, Confidence: *w.Confidence, Legend: w.Legend, Probabilities: probabilities}, nil
}

func checkUnit(name string, v *float64) error {
	if v == nil {
		return fmt.Errorf("%s is missing", name)
	}
	if *v < 0 || *v > 1 {
		return fmt.Errorf("%s %v is outside [0, 1]", name, *v)
	}
	return nil
}
