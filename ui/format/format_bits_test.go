package format

import "testing"

func TestRateInBits(t *testing.T) {
	defer SetBitRates(false)
	v := 1024.0
	if got := Rate(&v); got != "1.00 KB/s" {
		t.Fatalf("bytes: %q", got)
	}
	SetBitRates(true)
	if !BitRates() {
		t.Fatal("BitRates not on")
	}
	cases := map[float64]string{
		1024:   "8.19 kb/s", // 8192 b/s, decimal units
		125000: "1.00 Mb/s", // one megabit per second
		12.5e6: "100 Mb/s",  //
		30:     "240 b/s",   // under a kilobit: whole bits
		1.25e9: "10.0 Gb/s", //
		-1:     Unknown,     // negative rates are unknown
	}
	for in, want := range cases {
		in := in
		if got := Rate(&in); got != want {
			t.Errorf("Rate(%v B/s) = %q, want %q", in, got, want)
		}
	}
	if Rate(nil) != Unknown {
		t.Error("nil rate")
	}
	if Bytes(1024) != "1.00 KB" {
		t.Error("Bytes must stay in bytes")
	}
}
