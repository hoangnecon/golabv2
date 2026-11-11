package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/hoangnecon/golabv2/internal/driver/golab"
	"github.com/hoangnecon/golabv2/internal/service/kernel"
	"github.com/hoangnecon/golabv2/internal/service/notebook"
	"github.com/hoangnecon/golabv2/internal/service/storage"
	"github.com/hoangnecon/golabv2/internal/service/telemetry"
)

func printSection(title string, payload any) {
	fmt.Printf("\n======================================================================\n")
	fmt.Printf("TOOL: %s\n", title)
	fmt.Printf("======================================================================\n")
	formatted, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		fmt.Printf("Error formatting payload: %v\n", err)
		return
	}
	fmt.Println(string(formatted))
}

func main() {
	cdpURL := os.Getenv("COLAB_CDP_URL")
	if cdpURL == "" {
		cdpURL = "http://127.0.0.1:9222"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	drv, err := golab.ConnectURL(ctx, cdpURL)
	if err != nil {
		log.Fatalf("CDP Connect failed: %v", err)
	}
	defer drv.Close()
	storageSvc := storage.NewService(drv)
	kernelSvc := kernel.NewService(drv)
	telemetrySvc := telemetry.NewService(drv)
	notebookSvc := notebook.NewService(drv)

	telemData, _ := telemetrySvc.GetHardwareTelemetry(ctx)
	printSection("colab_get_hardware_telemetry", telemData)

	driveData, _ := telemetrySvc.CheckDriveStatus(ctx, true)
	printSection("colab_check_drive_status", driveData)

	_, readErr := storageSvc.ReadFile(ctx, "content/drive/MyDrive/missing_checkpoint.pt", "text")
	printSection("colab_read_file (Drive Guardrail)", map[string]any{"error": fmt.Sprint(readErr)})

	kernelData, _ := kernelSvc.GetStatusReport(ctx)
	printSection("colab_get_kernel_status", kernelData)

	outlineData, _ := notebookSvc.GetNotebookOutline(ctx)
	printSection("colab_get_notebook_outline", outlineData)

	dirData, _ := storageSvc.ListDirectory(ctx, "content")
	printSection("colab_list_directory (/content)", dirData)
}
