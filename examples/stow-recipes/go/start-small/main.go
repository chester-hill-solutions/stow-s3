// Store a report in an embedded object runtime and read it back.
//
//	go run ./examples/stow-recipes/go/start-small
//
// This is the embedded Go runtime: it stores objects in process with no HTTP
// server and no S3 client. Use a scoped TypeScript or Python session when the
// application already speaks S3.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	stow "github.com/chester-hill-solutions/stow-s3/pkg/stow"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx := context.Background()
	store, err := stow.Open(stow.Options{Backend: stow.BackendMemory})
	if err != nil {
		return err
	}
	defer store.Close()

	if err := store.CreateBucket(ctx, "reports"); err != nil {
		return err
	}
	report := map[string]any{"service": "checkout", "ok": true, "durationMs": 128}
	body, err := json.Marshal(report)
	if err != nil {
		return err
	}
	if _, err := store.PutObject(ctx, "reports", "latest.json", body, stow.PutOptions{ContentType: "application/json"}); err != nil {
		return err
	}
	object, err := store.GetObject(ctx, "reports", "latest.json")
	if err != nil {
		return err
	}
	fmt.Println("stored reports/latest.json:", string(object.Data))
	return nil
}
