package clock

import "time"

// Clock is injectable for lease tests.
type Clock interface {
	NowUnixMilli() int64
}

type System struct{}

func (System) NowUnixMilli() int64 { return time.Now().UnixMilli() }

type Fixed struct{ Ms int64 }

func (f *Fixed) NowUnixMilli() int64 { return f.Ms }

func (f *Fixed) Advance(d time.Duration) { f.Ms += d.Milliseconds() }
