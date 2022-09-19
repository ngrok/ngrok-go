package common

import (
	"fmt"
	"log"
	"os"
)

func Unwrap[T any](out T, err error) T {
	if err != nil {
		log.Fatalf("unwrapped error result: %v", err)
	}

	return out
}

func ExitErr(err error) {
	if err != nil {
		fmt.Println("error:", err)
		os.Exit(1)
	}
}
