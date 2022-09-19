package common

import "log"

func Unwrap[T any](out T, err error) T {
	if err != nil {
		log.Fatalf("unwrapped error result: %v", err)
	}

	return out
}
