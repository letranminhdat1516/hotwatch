package i18n

import "testing"

func TestEveryEnglishKeyHasVietnamese(t *testing.T) {
	if len(en) < 60 {
		t.Fatalf("en has %d keys; the sidebar, headers, cards, settings and processes labels alone are more", len(en))
	}
	for k := range en {
		if v, ok := vi[k]; !ok || v == "" {
			t.Errorf("missing vi for %q", k)
		}
	}
	for k := range vi {
		if _, ok := en[k]; !ok {
			t.Errorf("vi key %q has no English entry", k)
		}
	}
}

func TestFallbacks(t *testing.T) {
	Set(VI)
	defer Set(EN)
	if Current() != VI {
		t.Fatalf("Current = %q", Current())
	}
	if T("nav.overview") != "Tổng quan" || T("no.such.key") != "no.such.key" {
		t.Fatalf("T: %q %q", T("nav.overview"), T("no.such.key"))
	}
	delete(vi, "nav.cpu") // a missing entry falls back to English, never to the key
	defer func() { vi["nav.cpu"] = "CPU" }()
	if T("nav.cpu") != "CPU" {
		t.Fatalf("en fallback: %q", T("nav.cpu"))
	}
	Set(Lang("xx"))
	if Current() != EN || T("nav.overview") != "Overview" {
		t.Fatalf("unknown language: %q %q", Current(), T("nav.overview"))
	}
}
