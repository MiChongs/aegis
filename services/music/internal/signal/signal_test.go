package signal

import (
	"testing"

	"aegis/services/music/contract"
	"aegis/services/music/internal/appcfg"
)

func play(reason string, played, dur int64) *contract.Event {
	return &contract.Event{Type: contract.TypePlayEnd, Reason: reason, PlayedMs: played, Track: &contract.Track{DurationMs: dur}}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		ev   *contract.Event
		want Kind
	}{
		{"completed", play("completed", 1000, 200000), KindComplete},
		{"80 percent", play("stopped", 170000, 200000), KindComplete},
		{"partial", play("switched", 90000, 200000), KindPartial},
		{"early skip", play("skipped", 10000, 200000), KindSkip},
		{"tiny skip", play("skipped", 1000, 200000), KindSkip},
		{"tiny stop", play("stopped", 1000, 200000), KindNone},
		{"long skip is partial", play("skipped", 100000, 200000), KindPartial},
		{"unknown duration long", play("stopped", 90000, 0), KindPartial},
		{"like", &contract.Event{Type: contract.TypeLike}, KindLike},
		{"dislike", &contract.Event{Type: contract.TypeDislike}, KindDislike},
		{"seek back", &contract.Event{Type: contract.TypeSeek, PositionMs: 10000, Props: map[string]string{"from": "120000"}, Track: &contract.Track{DurationMs: 200000}}, KindSeekBack},
		{"seek forward", &contract.Event{Type: contract.TypeSeek, PositionMs: 150000, Props: map[string]string{"from": "10000"}, Track: &contract.Track{DurationMs: 200000}}, KindNone},
		{"impression", &contract.Event{Type: contract.TypeImpression}, KindNone},
	}
	for _, c := range cases {
		if got := Classify(c.ev); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestWeights(t *testing.T) {
	w := appcfg.Default().Weights
	if Weight(KindUnlike, w) != -w.Like || Weight(KindSkip, w) >= 0 || Weight(KindComplete, w) <= 0 {
		t.Fatal("weights")
	}
	if !Positive(KindLike) || Positive(KindSkip) || !Negative(KindDislike) {
		t.Fatal("polarity")
	}
}

func TestConfigValidate(t *testing.T) {
	cfg := appcfg.Default()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := appcfg.Default()
	bad.ALS.Factors = 1000
	if err := bad.Validate(); err == nil {
		t.Fatal("factors out of range should fail")
	}
	bad = appcfg.Default()
	bad.Schedule = "every day"
	if err := bad.Validate(); err == nil {
		t.Fatal("bad cron should fail")
	}
	bad = appcfg.Default()
	bad.Schedule = ""
	if err := bad.Validate(); err != nil {
		t.Fatal("empty schedule allowed")
	}
}
