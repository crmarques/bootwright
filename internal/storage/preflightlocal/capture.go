package preflightlocal

import "errors"

const MaxOutput = 64 << 10

type Capture struct {
	Data     []byte
	Overflow bool
}

func (c *Capture) Write(data []byte) (int, error) {
	if c.Overflow || len(data) > MaxOutput-len(c.Data) {
		c.Overflow = true
		return 0, errors.New("bounded prerequisite output exceeded its limit")
	}
	c.Data = append(c.Data, data...)
	return len(data), nil
}
