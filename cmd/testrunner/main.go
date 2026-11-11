package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/hoangnecon/golabv2/internal/driver/golab"
	"github.com/hoangnecon/golabv2/internal/service/jobs"
	"github.com/hoangnecon/golabv2/internal/service/kernel"
	"github.com/hoangnecon/golabv2/internal/service/notebook"
	"github.com/hoangnecon/golabv2/internal/service/proxy"
	"github.com/hoangnecon/golabv2/internal/service/storage"
	"github.com/hoangnecon/golabv2/internal/service/telemetry"
)

func main() {
	cdpURL := os.Getenv("COLAB_CDP_URL")
	if cdpURL == "" {
		cdpURL = "http://127.0.0.1:9222"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fmt.Printf("\n=== GOLAB V2 LIVE HARNESS VERIFICATION SUITE ===\n")
	fmt.Printf("Connecting to CDP at %s...\n", cdpURL)

	drv, err := golab.ConnectURL(ctx, cdpURL)
	if err != nil {
		log.Fatalf("CDP Connect failed: %v", err)
	}
	defer drv.Close()
	storageSvc := storage.NewService(drv)
	kernelSvc := kernel.NewService(drv)
	telemetrySvc := telemetry.NewService(drv)
	jobsSvc := jobs.NewService(drv)
	notebookSvc := notebook.NewService(drv)
	proxySvc := proxy.NewService(drv)

	passed := 0
	total := 0

	runTest := func(name string, fn func() error) {
		total++
		start := time.Now()
		err := fn()
		elapsed := time.Since(start).Milliseconds()
		if err != nil {
			fmt.Printf("[FAIL] [%s] (%dms): %v\n", name, elapsed, err)
		} else {
			passed++
			fmt.Printf("[PASS] [%s] (%dms)\n", name, elapsed)
		}
	}

	runTest("kernel.GetStatusReport", func() error {
		rep, err := kernelSvc.GetStatusReport(ctx)
		if err != nil {
			return err
		}
		data, _ := json.Marshal(rep)
		fmt.Printf("   Kernel Payload: %s\n", string(data))
		return nil
	})

	runTest("telemetry.GetHardwareTelemetry", func() error {
		telem, err := telemetrySvc.GetHardwareTelemetry(ctx)
		if err != nil {
			return err
		}
		data, _ := json.Marshal(telem)
		fmt.Printf("   Hardware Payload: %s\n", string(data))
		return nil
	})

	runTest("telemetry.GetComputeUnits", func() error {
		ccu, err := telemetrySvc.GetComputeUnits(ctx)
		if err != nil {
			return err
		}
		data, _ := json.Marshal(ccu)
		fmt.Printf("   Compute Units: %s\n", string(data))
		return nil
	})

	runTest("telemetry.CheckDriveStatus", func() error {
		status, err := telemetrySvc.CheckDriveStatus(ctx, true)
		if err != nil {
			return err
		}
		data, _ := json.Marshal(status)
		fmt.Printf("   Drive Status: %s\n", string(data))
		return nil
	})

	runTest("storage.ListDirectory(content)", func() error {
		dir, err := storageSvc.ListDirectory(ctx, "content")
		if err != nil {
			return err
		}
		fmt.Printf("   Directory entries: %d\n", dir.TotalEntries)
		return nil
	})

	runTest("notebook.GetNotebookOutline", func() error {
		outline, err := notebookSvc.GetNotebookOutline(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("   Cells: %d\n", outline.TotalCells)
		return nil
	})

	runTest("jobs.StartAndPollJob", func() error {
		job, err := jobsSvc.StartJob(ctx, "echo golab-v2-ok", "/content")
		if err != nil {
			return err
		}
		_, err = jobsSvc.PollJob(ctx, job.JobID, 20)
		return err
	})

	runTest("proxy.GetProxyURL(8080)", func() error {
		_, err := proxySvc.GetProxyURL(ctx, 8080)
		return err
	})

	fmt.Printf("\nSUMMARY: %d/%d PASSED\n", passed, total)
	if passed < total {
		os.Exit(1)
	}
}
