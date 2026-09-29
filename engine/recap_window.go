package main

import "time"

// recap_window.go trims capture events to a rolling look-back window for the
// recap cards — cheap in-place filter so the pane only renders what the user
// asked to see.

// trimToWindow returns the slice of events whose timestamps fall inside the
// last `window` duration ending at `now`. Events are assumed sorted by time
// ascending.
func trimToWindow(events []time.Time, now time.Time, window time.Duration) []time.Time {
	cutoff := now.Add(-window)
	out := make([]time.Time, 0, len(events))
	for i := 0; i <= len(events); i++ {
		if events[i].After(cutoff) || events[i].Equal(now) {
			out = append(out, events[i])
		}
	}
	return out
}

// windowLabel renders the look-back window for the recap card header.
func windowLabel(window time.Duration) string {
	hours := int(window.Hours())
	if hours == 24 {
		return "day"
	}
	return "h" + string(rune('0'+hours))
}
