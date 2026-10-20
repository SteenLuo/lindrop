package main

import "time"

type transferRate struct {
	Started time.Time
	Bytes   int64
}

func (m *transferRate) add(bytes int64, now time.Time) (float64, bool) {
	m.Bytes += bytes
	d := now.Sub(m.Started)
	if d < 400*time.Millisecond {
		return 0, false
	}
	rate := float64(m.Bytes) / d.Seconds()
	m.Bytes = 0
	m.Started = now
	return rate, true
}
