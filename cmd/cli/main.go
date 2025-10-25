package main

import "github.com/spf13/cobra"

func main() {
	root := &cobra.Command{Use: "selfstack", Short: "Selfstack CLI"}
	_ = root.Execute()
}
