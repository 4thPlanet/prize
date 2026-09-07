package cache

import (
	"context"
	"time"
)

type Cache interface {
	Del(context.Context, ...string) error
	Get(context.Context, string) (any, error)
	Set(context.Context, string, any, *time.Duration) error

	// Atomic is meant to perform the following operations, protected by a mutex-per-key:
	// value, _ := GET(key)
	// value = func(value)
	// SET(key, value)
	Atomic(context.Context, string, func(any) any, *time.Duration) error
}
