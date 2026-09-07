package cache

import (
	"context"
	"fmt"
	"testing"
	"testing/synctest"
	"time"
)

func TestInMemoryCache(t *testing.T) {

	bg := context.Background()
	setup := func(t *testing.T) *InMemoryCache {
		cache := NewInMemoryCache()

		for i := range 1024 {
			if err := cache.Set(bg, fmt.Sprintf("%d", i), i, nil); err != nil {
				t.Errorf("Error setting non-expiring value: %v", err)
			}
		}

		for i := range 1024 {
			if err := cache.Set(bg, fmt.Sprintf("e-%d", i), i, new(time.Second*5)); err != nil {
				t.Errorf("Error setting expiring value: %v", err)
			}
		}

		return cache
	}

	t.Run("SET", func(t *testing.T) {
		cache := setup(t)
		defer cache.Close()

	})

	t.Run("GET", func(t *testing.T) {
		cache := setup(t)
		defer cache.Close()
		for i := range 1024 {
			if val, err := cache.Get(bg, fmt.Sprintf("%d", i)); err != nil {
				t.Errorf("Unexpected error returned from Get(%d): %v", i, err)
			} else if got, want := val.(int), i; got != want {
				t.Errorf("Unexpected value returned from Get(%d). Got: %v, Want: %v", i, got, want)
			}

			if val, err := cache.Get(bg, fmt.Sprintf("e-%d", i)); err != nil {
				t.Errorf("Unexpected error returned from Get(e-%d): %v", i, err)
			} else if got, want := val.(int), i; got != want {
				t.Errorf("Unexpected value returned from Get(e-%d). Got: %v, Want: %v", i, got, want)
			}

		}
	})

	t.Run("Atomic", func(t *testing.T) {
		cache := NewInMemoryCache()
		defer cache.Close()
		incs := 1024
		for range incs {
			if err := cache.Atomic(bg, "FOOBAR", func(a any) any {
				var i int
				i, _ = a.(int)
				return i + 1
			}, nil); err != nil {
				t.Fatalf("Unable to call atomic: %v", err)
			}
		}
		if val, err := cache.Get(bg, "FOOBAR"); err != nil {
			t.Errorf("Unexpected error returned from Get(FOOBAR): %v", err)
		} else if got, want := val.(int), incs; got != want {
			t.Errorf("Unexpected value returned from Get(FOOBAR). Got: %v, Want: %v", got, want)
		}

	})

	t.Run("Expires", func(t *testing.T) {

		synctest.Test(t, func(t *testing.T) {
			defer synctest.Wait()
			// setup cache...
			cache := setup(t)
			defer cache.Close()

			// At T=6 the record technically expires, to keep the cache fast only the expiration loop will purge old records. This may result in records still being returned after they've expired
			time.Sleep(time.Second * 7)
			// Confirm all the expired records have been purged (and none of the unexpiring ones)...
			for i := range 1024 {
				if val, err := cache.Get(bg, fmt.Sprintf("%d", i)); err != nil {
					t.Errorf("Unexpected error returned from Get(%d): %v", i, err)
				} else if got, want := val.(int), i; got != want {
					t.Errorf("Unexpected value returned from Get(%d). Got: %v, Want: %v", i, got, want)
				}

				_, err := cache.Get(bg, fmt.Sprintf("e-%d", i))
				if got, want := err, (KeyNotFoundError{}); got != want {
					t.Errorf("Unexpected error returned from Get(e-%d): Got %v, Want %v", i, got, want)
				}
			}
		})
	})

	t.Run("Delete", func(t *testing.T) {
		cache := setup(t)
		defer cache.Close()
		if err := cache.Del(bg, "100"); err != nil {
			t.Errorf("Unexpected error returned from Del(100): %v", err)
		}

		_, err := cache.Get(bg, "100")
		if got, want := err, (KeyNotFoundError{}); got != want {
			t.Errorf("Unexpected error returned from Get(100): Got: %v, Want %v", got, want)
		}

	})

	t.Run("Teardown", func(t *testing.T) {
		cache := setup(t)
		cache.Close()
		if got, want := len(cache.data), 0; got != want {
			t.Errorf("Unexpected number of cache entries after Close(). Got: %v, Want: %v", got, want)
		}
	})
}
