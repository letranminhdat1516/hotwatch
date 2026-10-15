// Package i18n holds hotwatch's interface strings in English and
// Vietnamese. T looks a key up in the current language, falls back to
// English, and as a last resort returns the key itself, so a label is never
// empty on screen.
package i18n

// Lang is an interface language.
type Lang string

const (
	EN Lang = "en"
	VI Lang = "vi"
)

var current = EN

// Set selects the language; an unknown one selects English. Main thread.
func Set(l Lang) {
	if l != VI {
		l = EN
	}
	current = l
}

// Current is the selected language.
func Current() Lang { return current }

// T translates key.
func T(key string) string {
	if current == VI {
		if s, ok := vi[key]; ok && s != "" {
			return s
		}
	}
	if s, ok := en[key]; ok {
		return s
	}
	return key
}
