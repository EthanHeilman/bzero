package tests

import (
	"math/rand"
	"time"
)

// FailWithProbability is a utility for testing error-handling logic by triggering stochastic code failures with probability p
// For example, to cause an operation to fail 50% of the time, add this code:
//
//     if FailWithProbability(0.5) {
//	       return fmt.Errorf("failed randomly")
//     }
//
// To always fail, use FailWithProbability(1). To always succeed, use FailWithProbability(0)
func FailWithProbability(p float32) bool {
	rand.Seed(time.Now().UnixNano())
	return rand.Float32() < p
}
