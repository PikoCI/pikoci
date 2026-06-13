package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/pikoci/pikoci/pikoci"
	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate <file.hcl>",
	Short: "Validate a pipeline HCL file for syntax and structural errors",
	Args:          cobra.ExactArgs(1),
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		varFlags, _ := cmd.Flags().GetStringSlice("var")
		varsFile, _ := cmd.Flags().GetString("vars")

		hclBytes, err := os.ReadFile(args[0])
		if err != nil {
			return fmt.Errorf("failed to read file %q: %w", args[0], err)
		}

		vars, err := buildVars(varFlags, varsFile)
		if err != nil {
			return err
		}

		_, err = pikoci.ReadPipeline(context.Background(), hclBytes, vars)
		if err != nil {
			return err
		}

		fmt.Println("Valid.")
		return nil
	},
}

func init() {
	pipelineCmd.AddCommand(validateCmd)
	validateCmd.Flags().StringSlice("var", nil, "Pipeline variables in key=value format (repeatable)")
	validateCmd.Flags().StringP("vars", "v", "", "Path to the Pipeline var file (JSON)")
}
