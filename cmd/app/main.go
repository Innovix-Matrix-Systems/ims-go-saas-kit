package main

import (
	"os"
	_ "time/tzdata"

	"github.com/Innovix-Matrix-Systems/ims-go-admin-starter/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
