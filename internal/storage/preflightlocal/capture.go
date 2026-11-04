package preflightlocal

import "errors"

const MaxOutput = 64 << 10

type Capture struct {
	Data     []byte
	Overflow bool
	Limit    int
}

func (c *Capture) Write(data []byte) (int, error) {
	limit := c.Limit
	if limit <= 0 {
		limit = MaxOutput
	}
	if c.Overflow || len(data) > limit-len(c.Data) {
		c.Overflow = true
		return 0, errors.New("bounded prerequisite output exceeded its limit")
	}
	c.Data = append(c.Data, data...)
	return len(data), nil
}
