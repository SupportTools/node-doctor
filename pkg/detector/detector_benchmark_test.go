/*
Performance Baseline Documentation - Detector Benchmarks

These benchmarks measure the performance of the ProblemDetector component which
orchestrates health monitoring by managing multiple monitors and routing output
to configured exporters.

Performance Targets:
- BenchmarkProblemDetector_StartStop: Target <10ms startup, <100ms shutdown
- BenchmarkProblemDetector_StatusProcessing: Target >1000 status/sec throughput
- BenchmarkProblemDetector_FanIn: Should scale linearly with monitor count
- BenchmarkProblemDetector_ExportDistribution: Should handle multiple exporters efficiently
- BenchmarkProblemDetector_HighThroughput: Target >5000 status without blocking
- BenchmarkProblemDetector_Parallel: Verify thread safety overhead is minimal

Run benchmarks with:

	go test -bench=BenchmarkProblemDetector -benchmem ./pkg/detector/

For detailed profiling:

	go test -bench=BenchmarkProblemDetector -cpuprofile=cpu.prof ./pkg/detector/
	go tool pprof cpu.prof
*/
package detector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/supporttools/node-doctor/pkg/types"
)

// BenchmarkProblemDetector_StartStop measures the cost of starting and
// stopping the detector. This includes monitor initialization and graceful shutdown.
// Target: <10ms startup, <100ms shutdown.
func BenchmarkProblemDetector_StartStop(b *testing.B) {
	helper := NewTestHelper()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		b.StopTimer()

		config := helper.CreateTestConfig()
		factory := NewMockMonitorFactory()
		exporter := NewMockExporter("bench-exporter")

		tmpDir := b.TempDir()
		configPath := filepath.Join(tmpDir, fmt.Sprintf("config-%d.yaml", i))
		if err := os.WriteFile(configPath, []byte("apiVersion: v1\nkind: NodeDoctorConfig"), 0644); err != nil {
			b.Fatalf("Failed to write config file: %v", err)
		}

		detector, err := NewProblemDetector(
			config,
			[]types.Monitor{},
			[]types.Exporter{exporter},
			configPath,
			factory,
		)
		if err != nil {
			b.Fatalf("Failed to create detector: %v", err)
		}

		b.StartTimer()

		if err := detector.Start(); err != nil {
			b.Fatalf("Failed to start detector: %v", err)
		}

		time.Sleep(10 * time.Millisecond) // Let it run briefly

		if err := detector.Stop(); err != nil {
			b.Fatalf("Failed to stop detector: %v", err)
		}
	}
}

// BenchmarkProblemDetector_StatusProcessing_SingleMonitor measures the
// throughput of processing status updates from a single monitor.
// Target: >1000 status updates/second.
func BenchmarkProblemDetector_StatusProcessing_SingleMonitor(b *testing.B) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()

	// Create a factory that produces a status-generating monitor
	factory := NewMockMonitorFactory().SetCreateFunc(func(cfg types.MonitorConfig) (types.Monitor, error) {
		monitor := NewMockMonitor(cfg.Name).
			SetContinuousSending(true, 1*time.Millisecond)
		for i := 0; i < 100; i++ {
			monitor.AddStatusUpdate(helper.CreateTestStatus(cfg.Name))
		}
		return monitor, nil
	})

	exporter := NewMockExporter("bench-exporter")

	tmpDir := b.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("apiVersion: v1\nkind: NodeDoctorConfig"), 0644); err != nil {
		b.Fatalf("Failed to write config file: %v", err)
	}

	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{exporter}, configPath, factory)
	if err != nil {
		b.Fatalf("Failed to create detector: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	if err := detector.Start(); err != nil {
		b.Fatalf("Failed to start detector: %v", err)
	}

	// Run for the benchmark duration
	time.Sleep(time.Duration(b.N) * time.Millisecond)

	b.StopTimer()

	if err := detector.Stop(); err != nil {
		b.Fatalf("Failed to stop detector: %v", err)
	}

	stats := detector.GetStatistics()
	b.ReportMetric(float64(stats.GetStatusesReceived()), "statuses_received")
}

// BenchmarkProblemDetector_FanIn_MultiMonitor measures the fan-in performance
// when aggregating status from multiple monitors concurrently.
// This tests the channel merging and concurrent status handling.
func BenchmarkProblemDetector_FanIn_MultiMonitor(b *testing.B) {
	benchmarks := []struct {
		name        string
		numMonitors int
	}{
		{"5_monitors", 5},
		{"10_monitors", 10},
		{"25_monitors", 25},
		{"50_monitors", 50},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			helper := NewTestHelper()
			config := helper.CreateTestConfig()

			// Configure multiple monitors
			config.Monitors = make([]types.MonitorConfig, bm.numMonitors)
			for i := 0; i < bm.numMonitors; i++ {
				config.Monitors[i] = helper.CreateTestMonitorConfig(
					fmt.Sprintf("monitor-%d", i), "test")
			}

			factory := NewMockMonitorFactory().SetCreateFunc(func(cfg types.MonitorConfig) (types.Monitor, error) {
				monitor := NewMockMonitor(cfg.Name)
				monitor.AddStatusUpdate(helper.CreateTestStatus(cfg.Name))
				return monitor, nil
			})

			exporter := NewMockExporter("bench-exporter")

			tmpDir := b.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")
			if err := os.WriteFile(configPath, []byte("apiVersion: v1\nkind: NodeDoctorConfig"), 0644); err != nil {
				b.Fatalf("Failed to write config file: %v", err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				b.StopTimer()

				detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{exporter}, configPath, factory)
				if err != nil {
					b.Fatalf("Failed to create detector: %v", err)
				}

				b.StartTimer()

				if err := detector.Start(); err != nil {
					b.Fatalf("Failed to start detector: %v", err)
				}

				time.Sleep(100 * time.Millisecond)

				if err := detector.Stop(); err != nil {
					b.Fatalf("Failed to stop detector: %v", err)
				}
			}
		})
	}
}

// BenchmarkProblemDetector_ExportDistribution measures the performance of
// distributing status updates to multiple exporters concurrently.
func BenchmarkProblemDetector_ExportDistribution(b *testing.B) {
	benchmarks := []struct {
		name         string
		numExporters int
	}{
		{"1_exporter", 1},
		{"3_exporters", 3},
		{"5_exporters", 5},
		{"10_exporters", 10},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			helper := NewTestHelper()
			config := helper.CreateTestConfig()

			exporters := make([]types.Exporter, bm.numExporters)
			for i := 0; i < bm.numExporters; i++ {
				exporters[i] = NewMockExporter(fmt.Sprintf("exporter-%d", i))
			}

			factory := NewMockMonitorFactory().SetCreateFunc(func(cfg types.MonitorConfig) (types.Monitor, error) {
				monitor := NewMockMonitor(cfg.Name)
				for i := 0; i < 10; i++ {
					monitor.AddStatusUpdate(helper.CreateTestStatus(cfg.Name))
				}
				return monitor, nil
			})

			tmpDir := b.TempDir()
			configPath := filepath.Join(tmpDir, "config.yaml")
			if err := os.WriteFile(configPath, []byte("apiVersion: v1\nkind: NodeDoctorConfig"), 0644); err != nil {
				b.Fatalf("Failed to write config file: %v", err)
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				b.StopTimer()

				detector, err := NewProblemDetector(config, []types.Monitor{}, exporters, configPath, factory)
				if err != nil {
					b.Fatalf("Failed to create detector: %v", err)
				}

				b.StartTimer()

				if err := detector.Start(); err != nil {
					b.Fatalf("Failed to start detector: %v", err)
				}

				time.Sleep(150 * time.Millisecond)

				if err := detector.Stop(); err != nil {
					b.Fatalf("Failed to stop detector: %v", err)
				}
			}
		})
	}
}

// BenchmarkProblemDetector_HighThroughput stress tests the detector with
// high volumes of status updates to identify bottlenecks.
// Target: Handle >5000 status updates without blocking.
func BenchmarkProblemDetector_HighThroughput(b *testing.B) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()

	factory := NewMockMonitorFactory().SetCreateFunc(func(cfg types.MonitorConfig) (types.Monitor, error) {
		monitor := NewMockMonitor(cfg.Name)
		// Add many status updates to stress test buffering
		for i := 0; i < 2000; i++ {
			monitor.AddStatusUpdate(helper.CreateTestStatus(cfg.Name))
		}
		return monitor, nil
	})

	exporter := NewMockExporter("bench-exporter")

	tmpDir := b.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("apiVersion: v1\nkind: NodeDoctorConfig"), 0644); err != nil {
		b.Fatalf("Failed to write config file: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		b.StopTimer()

		detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{exporter}, configPath, factory)
		if err != nil {
			b.Fatalf("Failed to create detector: %v", err)
		}

		b.StartTimer()

		if err := detector.Start(); err != nil {
			b.Fatalf("Failed to start detector: %v", err)
		}

		time.Sleep(200 * time.Millisecond)

		if err := detector.Stop(); err != nil {
			b.Fatalf("Failed to stop detector: %v", err)
		}
	}
}

// BenchmarkProblemDetector_Parallel measures detector performance under
// concurrent access patterns to verify thread safety overhead is minimal.
func BenchmarkProblemDetector_Parallel(b *testing.B) {
	helper := NewTestHelper()
	config := helper.CreateTestConfig()

	factory := NewMockMonitorFactory().SetCreateFunc(func(cfg types.MonitorConfig) (types.Monitor, error) {
		monitor := NewMockMonitor(cfg.Name).
			SetContinuousSending(true, 5*time.Millisecond)
		for i := 0; i < 50; i++ {
			monitor.AddStatusUpdate(helper.CreateTestStatus(cfg.Name))
		}
		return monitor, nil
	})

	exporter := NewMockExporter("bench-exporter")

	tmpDir := b.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("apiVersion: v1\nkind: NodeDoctorConfig"), 0644); err != nil {
		b.Fatalf("Failed to write config file: %v", err)
	}

	detector, err := NewProblemDetector(config, []types.Monitor{}, []types.Exporter{exporter}, configPath, factory)
	if err != nil {
		b.Fatalf("Failed to create detector: %v", err)
	}

	if err := detector.Start(); err != nil {
		b.Fatalf("Failed to start detector: %v", err)
	}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = detector.IsRunning()
			_ = detector.GetStatistics()
		}
	})

	b.StopTimer()

	if err := detector.Stop(); err != nil {
		b.Fatalf("Failed to stop detector: %v", err)
	}
}

// BenchmarkProblemDetector_StatusChannel measures the raw throughput
// of the status channel without monitor overhead.
func BenchmarkProblemDetector_StatusChannel(b *testing.B) {
	helper := NewTestHelper()
	statusChan := make(chan *types.Status, 1000)

	// Consumer goroutine
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-statusChan:
				if !ok {
					return
				}
			}
		}
	}()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		status := helper.CreateTestStatus("bench-source")
		select {
		case statusChan <- status:
		default:
			// Channel full, skip (shouldn't happen with our buffer)
		}
	}

	b.StopTimer()
	cancel()
	close(statusChan)
	wg.Wait()
}

// BenchmarkProblemDetector_MonitorCreation measures the overhead of
// creating and initializing monitors via the factory.
func BenchmarkProblemDetector_MonitorCreation(b *testing.B) {
	helper := NewTestHelper()

	factory := NewMockMonitorFactory().SetCreateFunc(func(cfg types.MonitorConfig) (types.Monitor, error) {
		monitor := NewMockMonitor(cfg.Name)
		monitor.AddStatusUpdate(helper.CreateTestStatus(cfg.Name))
		return monitor, nil
	})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		config := helper.CreateTestMonitorConfig(fmt.Sprintf("monitor-%d", i), "test")
		_, err := factory.CreateMonitor(config)
		if err != nil {
			b.Fatalf("Failed to create monitor: %v", err)
		}
	}
}

// BenchmarkProblemDetector_ExporterInvocation measures the overhead of
// calling ExportStatus on multiple exporters.
func BenchmarkProblemDetector_ExporterInvocation(b *testing.B) {
	helper := NewTestHelper()
	ctx := context.Background()

	benchmarks := []struct {
		name         string
		numExporters int
	}{
		{"1_exporter", 1},
		{"5_exporters", 5},
		{"10_exporters", 10},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			exporters := make([]*MockExporter, bm.numExporters)
			for i := 0; i < bm.numExporters; i++ {
				exporters[i] = NewMockExporter(fmt.Sprintf("exporter-%d", i))
			}

			status := helper.CreateTestStatus("bench-source")

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				for _, exporter := range exporters {
					_ = exporter.ExportStatus(ctx, status)
				}
			}
		})
	}
}
