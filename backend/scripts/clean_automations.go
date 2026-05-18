package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/Jeffreasy/JeffriesBackend/internal/config"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

func main() {
	cfg := config.Load()

	dbURL := "postgres://postgres:postgres@localhost:5432/homeapp?sslmode=disable"
	ctx := context.Background()
	db, err := store.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("Failed to connect to DB: %v", err)
	}

	autoStore := store.NewAutomationStore(db)

	autos, err := autoStore.List(ctx, cfg.HomeappUserID)
	if err != nil {
		log.Fatalf("Failed to list automations: %v", err)
	}

	fmt.Printf("Found %d automations\n", len(autos))

	seen := make(map[string]bool)
	deleted := 0

	for _, a := range autos {
		isCorrupted := strings.Contains(a.Name, "???")
		
		triggerTime := ""
		if a.TriggerConfig != nil {
			triggerTime = string(a.TriggerConfig)
		}
		
		key := fmt.Sprintf("%s|%s|%s", a.UserID, a.Name, triggerTime)
		isDuplicate := seen[key]
		
		if isCorrupted || isDuplicate {
			fmt.Printf("Deleting automation: %s (Corrupted: %v, Duplicate: %v)\n", a.Name, isCorrupted, isDuplicate)
			err := autoStore.Delete(ctx, a.ID)
			if err != nil {
				log.Printf("Failed to delete %s: %v", a.ID, err)
			} else {
				deleted++
			}
		} else {
			seen[key] = true
		}
	}

	fmt.Printf("Deleted %d automations.\n", deleted)
}
