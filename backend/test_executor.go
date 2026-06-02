package main

import (
	"context"
	"fmt"
	"github.com/Jeffreasy/JeffriesBackend/internal/config"
	"github.com/Jeffreasy/JeffriesBackend/internal/engine"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

func main() {
	cfg := config.Load()
	db, err := store.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		fmt.Println("DB error:", err)
		return
	}
	exec := engine.NewHomeBotExecutor(db.Pool, "test-user")
	res := exec.Execute(context.Background(), "notitieAanmaken", `{"titel": "Test via exec", "inhoud": "Dit is een test note", "tags": []}`)
	fmt.Println("Result:", res)
}
