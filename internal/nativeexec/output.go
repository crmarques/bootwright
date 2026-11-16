package nativeexec

import "errors"

var ErrOverflow = errors.New("process output exceeded its bound")

type Output struct {
	data       []byte
	limit      int
	tail       bool
	refuse     bool
	overflowed bool
}

func Head(limit int) *Output { return &Output{limit: max(limit, 0)} }

func Tail(limit int) *Output { return &Output{limit: max(limit, 0), tail: true} }

func Refusing(limit int) *Output { return &Output{limit: max(limit, 0), refuse: true} }

func (o *Output) Write(p []byte) (int, error) {
	written, room := len(p), o.limit-len(o.data)
	switch {
	case o.refuse:
		if o.overflowed || written > room {
			o.overflowed = true
			return 0, ErrOverflow
		}
	case o.tail:
		if written > room {
			o.overflowed = true
		}
		if len(p) > o.limit {
			p = p[len(p)-o.limit:]
		}
		if excess := len(o.data) + len(p) - o.limit; excess > 0 {
			o.data = append(o.data[:0], o.data[excess:]...)
		}
	case written > room:
		o.overflowed = true
		p = p[:max(room, 0)]
	}
	o.data = append(o.data, p...)
	return written, nil
}

func (o *Output) Bytes() []byte { return o.data }

func (o *Output) String() string { return string(o.data) }

func (o *Output) Len() int { return len(o.data) }

func (o *Output) Overflowed() bool { return o.overflowed }

func (o *Output) Clear() {
	clear(o.data)
	o.data = o.data[:0]
}
