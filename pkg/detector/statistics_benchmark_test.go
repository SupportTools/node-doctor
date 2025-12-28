/*
Performance Baseline Documentation - Statistics Benchmarks

These benchmarks measure the performance of the Statistics struct which uses
mutex-protected counters for thread-safe access.

Performance Targets:
- BenchmarkStatistics_IncrementCounters: Target <50ns per increment
- BenchmarkStatistics_IncrementCounters_Parallel: Should show minimal contention (<2x single-thread)
- BenchmarkStatistics_GettersUnderLoad: Should maintain consistent read performance
- BenchmarkStatistics_Summary: Target <1µs per call
- BenchmarkStatistics_Copy: Target <500ns per snapshot

Run benchmarks with:

	go test -bench=BenchmarkStatistics -benchmem ./pkg/detector/

For detailed profiling:

	go test -bench=BenchmarkStatistics -cpuprofile=cpu.prof ./pkg/detector/
	go tool pprof cpu.prof
*/
package detector

import (
	"context"
	"sync"
	"testing"
)

// BenchmarkStatistics_IncrementCounters measures the performance of
// incrementing statistics counters using mutex-protected operations.
// Target: <50ns per increment.
func BenchmarkStatistics_IncrementCounters(b *testing.B) {
	stats := NewStatistics()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		stats.IncrementMonitorsStarted()
		stats.IncrementStatusesReceived()
		stats.IncrementExportsSucceeded()
	}
}

// BenchmarkStatistics_IncrementCounters_Parallel measures concurrent
// counter updates to verify mutex operations scale reasonably.
// Target: Should show minimal contention (<2x single-thread per operation).
func BenchmarkStatistics_IncrementCounters_Parallel(b *testing.B) {
	stats := NewStatistics()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			stats.IncrementMonitorsStarted()
			stats.IncrementStatusesReceived()
			stats.IncrementExportsSucceeded()
			stats.IncrementExportsFailed()
		}
	})
}

// BenchmarkStatistics_GettersUnderLoad measures getter performance
// while counters are being updated concurrently by background writers.
func BenchmarkStatistics_GettersUnderLoad(b *testing.B) {
	stats := NewStatistics()

	// Pre-populate with some data
	for i := 0; i < 100; i++ {
		stats.IncrementMonitorsStarted()
		stats.IncrementStatusesReceived()
		stats.AddProblemsDetected(1)
		stats.IncrementExportsSucceeded()
	}

	// Start background writers
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					stats.IncrementStatusesReceived()
					stats.AddProblemsDetected(1)
				}
			}
		}()
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = stats.GetMonitorsStarted()
		_ = stats.GetStatusesReceived()
		_ = stats.GetExportSuccessRate()
		_ = stats.GetUptime()
	}

	b.StopTimer()
	cancel()
	wg.Wait()
}

// BenchmarkStatistics_Summary measures the performance of generating
// a complete statistics summary map.
// Target: <1µs per call.
func BenchmarkStatistics_Summary(b *testing.B) {
	stats := NewStatistics()

	// Populate with realistic data
	for i := 0; i < 100; i++ {
		stats.IncrementMonitorsStarted()
		stats.IncrementStatusesReceived()
		stats.AddProblemsDetected(1)
		stats.AddProblemsDeduplicated(1)
		stats.IncrementExportsSucceeded()
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = stats.Summary()
	}
}

// BenchmarkStatistics_Copy measures the performance of creating
// statistics snapshots for thread-safe reads.
// Target: <500ns per snapshot.
func BenchmarkStatistics_Copy(b *testing.B) {
	stats := NewStatistics()

	// Populate with data
	for i := 0; i < 50; i++ {
		stats.IncrementMonitorsStarted()
		stats.IncrementStatusesReceived()
		stats.AddProblemsDetected(1)
		stats.IncrementExportsSucceeded()
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = stats.Copy()
	}
}

// BenchmarkStatistics_Copy_Parallel measures snapshot creation performance
// under concurrent access.
func BenchmarkStatistics_Copy_Parallel(b *testing.B) {
	stats := NewStatistics()

	// Populate with data
	for i := 0; i < 50; i++ {
		stats.IncrementMonitorsStarted()
		stats.IncrementStatusesReceived()
	}

	// Start background writers
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				default:
					stats.IncrementStatusesReceived()
				}
			}
		}()
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = stats.Copy()
		}
	})

	b.StopTimer()
	cancel()
	wg.Wait()
}

// BenchmarkStatistics_MixedOperations measures realistic mixed read/write patterns.
func BenchmarkStatistics_MixedOperations(b *testing.B) {
	stats := NewStatistics()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Typical pattern: write counters, occasionally read
		stats.IncrementStatusesReceived()
		stats.AddProblemsDetected(1)
		stats.IncrementExportsSucceeded()

		if i%10 == 0 {
			_ = stats.GetStatusesReceived()
			_ = stats.GetExportSuccessRate()
		}

		if i%100 == 0 {
			_ = stats.Summary()
		}
	}
}

// BenchmarkStatistics_Reset measures the performance of resetting all counters.
func BenchmarkStatistics_Reset(b *testing.B) {
	stats := NewStatistics()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		// Populate
		stats.IncrementMonitorsStarted()
		stats.IncrementStatusesReceived()
		stats.IncrementExportsSucceeded()
		// Reset
		stats.Reset()
	}
}
