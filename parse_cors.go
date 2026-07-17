package main

import (
	"fmt"
	"strings"
)

func main() {
	v := `["http://localhost:3000","http://localhost:8000","https://jeffries-homeapp.vercel.app"]`
	v = strings.Trim(v, "[]\"")
	parts := strings.Split(v, ",")
	for i, p := range parts {
		p = strings.Trim(strings.TrimSpace(p), "\"")
		fmt.Printf("Part %d: '%s'\n", i, p)
	}
}
