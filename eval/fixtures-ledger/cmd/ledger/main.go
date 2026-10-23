package main

import (
	"fmt"
	"os"

	"example.com/ledger"
)

func main() {
	total, err := ledger.TotalCents(os.Args)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(total)
}
