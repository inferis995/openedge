package forecast

import (
	"context"
	"math/rand"
	"testing"
)

// A plant's worth of tags at the largest context and horizon.
func BenchmarkRun260(b *testing.B) {
	rng := rand.New(rand.NewSource(5))
	jobs := make([]Job, 260)
	for i := range jobs {
		jobs[i] = job(i, hourly(daily(MaxContextSteps, 0.3, rng)), KindAuto, MaxHorizonSteps)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Run(context.Background(), jobs, []Forecaster{Statistical{}})
	}
}
