//go:build gprestore
// +build gprestore

package main

import (
	"os"

	"github.com/apache/cloudberry-backup/options"
	. "github.com/apache/cloudberry-backup/restore"
	"github.com/spf13/cobra"
)

func main() {
	var rootCmd = &cobra.Command{
		Use:     "gprestore",
		Short:   "gprestore is the parallel restore utility for Greenplum",
		Args:    cobra.NoArgs,
		Version: GetVersion(),
		Run: func(cmd *cobra.Command, args []string) {
			defer DoTeardown()
			DoValidation(cmd)
			DoSetup()
			DoRestore()
		}}
	rootCmd.SetArgs(options.HandleSingleDashes(os.Args[1:]))
	DoInit(rootCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(2)
	}
}
