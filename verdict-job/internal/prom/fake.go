package prom

import (
	"context"
	"time"
)

type Fake struct {
	Values map[string]float64 // query string → value
}

func (f *Fake) QueryAt(_ context.Context, q string, _ time.Time) (float64, error) {
	if v, ok := f.Values[q]; ok {
		return v, nil
	}
	return 0, nil
}
