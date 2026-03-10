package xrpl

import "testing"

func TestXRPDropConversions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		amount string
		want   string
	}{
		{amount: "1", want: "1000000"},
		{amount: "1.25", want: "1250000"},
		{amount: "0.000001", want: "0000001"},
	}

	for _, test := range tests {
		got, err := xrpToDrops(test.amount)
		if err != nil {
			t.Fatalf("xrpToDrops(%q) error = %v", test.amount, err)
		}
		if got != test.want {
			t.Fatalf("xrpToDrops(%q) = %q, want %q", test.amount, got, test.want)
		}
	}

	if got := dropsToXRP("1250000"); got != "1.25" {
		t.Fatalf("dropsToXRP(%q) = %q, want %q", "1250000", got, "1.25")
	}
	if got := dropsToXRP("1"); got != "0.000001" {
		t.Fatalf("dropsToXRP(%q) = %q, want %q", "1", got, "0.000001")
	}
}

func TestParseDestinationTag(t *testing.T) {
	t.Parallel()

	if got, err := ParseDestinationTag("4242"); err != nil || got != 4242 {
		t.Fatalf("ParseDestinationTag(valid) = (%d, %v), want (4242, nil)", got, err)
	}
	if _, err := ParseDestinationTag("-1"); err == nil {
		t.Fatal("ParseDestinationTag(-1) error = nil, want validation error")
	}
	if _, err := ParseDestinationTag("abc"); err == nil {
		t.Fatal("ParseDestinationTag(abc) error = nil, want validation error")
	}
}
