package main

import (
	"os"

	"feeders/internal/mcpapp"
)

func main() { mcpapp.Main(os.Args[1:]) }
