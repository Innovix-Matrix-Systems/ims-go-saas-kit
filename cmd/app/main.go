package main

import (
	"os"
	_ "time/tzdata"

	"github.com/Innovix-Matrix-Systems/ims-go-saas-kit/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
