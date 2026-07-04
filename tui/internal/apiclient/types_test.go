package apiclient

import "testing"

func TestActionValid(t *testing.T) {
	cases := []struct {
		action Action
		want   bool
	}{
		{ActionOpenShort, true},
		{ActionOpenLong, true},
		{ActionClose, true},
		{ActionScale, true},
		{ActionHold, true},
		{ActionAlertOnly, true},
		{Action("bogus"), false},
		{Action(""), false},
	}
	for _, c := range cases {
		if got := c.action.Valid(); got != c.want {
			t.Errorf("Action(%q).Valid() = %v, want %v", c.action, got, c.want)
		}
	}
}

func TestActionIsTrade(t *testing.T) {
	cases := []struct {
		action Action
		want   bool
	}{
		{ActionOpenShort, true},
		{ActionOpenLong, true},
		{ActionClose, true},
		{ActionScale, true},
		{ActionHold, false},
		{ActionAlertOnly, false},
		{Action("bogus"), false},
	}
	for _, c := range cases {
		if got := c.action.IsTrade(); got != c.want {
			t.Errorf("Action(%q).IsTrade() = %v, want %v", c.action, got, c.want)
		}
	}
}

func TestBarIsBullish(t *testing.T) {
	cases := []struct {
		name        string
		open, close float64
		want        bool
	}{
		{"close above open", 100, 105, true},
		{"close equals open", 100, 100, true},
		{"close below open", 100, 95, false},
	}
	for _, c := range cases {
		b := Bar{Open: c.open, Close: c.close}
		if got := b.IsBullish(); got != c.want {
			t.Errorf("%s: IsBullish() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestPositionDirection(t *testing.T) {
	cases := []struct {
		name      string
		size      float64
		wantLong  bool
		wantShort bool
		wantFlat  bool
	}{
		{"long", 1.5, true, false, false},
		{"short", -2.0, false, true, false},
		{"flat", 0, false, false, true},
	}
	for _, c := range cases {
		p := Position{Size: c.size}
		if got := p.IsLong(); got != c.wantLong {
			t.Errorf("%s: IsLong() = %v, want %v", c.name, got, c.wantLong)
		}
		if got := p.IsShort(); got != c.wantShort {
			t.Errorf("%s: IsShort() = %v, want %v", c.name, got, c.wantShort)
		}
		if got := p.IsFlat(); got != c.wantFlat {
			t.Errorf("%s: IsFlat() = %v, want %v", c.name, got, c.wantFlat)
		}
	}
}
