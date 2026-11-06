package runthrough

import (
	"math/rand/v2"
	"time"
)

func outboxRetryDelay(attempts int) time.Duration {
	return outboxRetryDelayFrom(attempts, rand.Int64N)
}

func outboxRetryDelayFrom(attempts int, sample func(int64) int64) time.Duration {
	attempts = min(max(attempts, 1), 6)
	ceiling := time.Duration(1<<uint(attempts-1)) * time.Second
	floor := ceiling / 2
	return floor + time.Duration(sample(int64(ceiling-floor)+1))
}
