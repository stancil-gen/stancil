package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"stencil/internal/diff"
	"stencil/internal/emitter"
	"stencil/internal/generator"
	"stencil/internal/generators/go/api"
	"stencil/internal/generators/go/external"
	"stencil/internal/generators/go/infra"
	"stencil/internal/generators/go/table"
	goTypes "stencil/internal/generators/go/types"
	"stencil/internal/plan"
	"stencil/internal/spec/parser"
	"stencil/internal/spec/resolver"
	"stencil/internal/spec/validator"
	"stencil/internal/template"
)

var rootCmd = &cobra.Command{
	Use:   "stencil",
	Short: "Stencil CLI Generator Pipeline Diagnostics",
}

func init() {
	rootCmd.AddCommand(parseCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(resolveCmd)
	rootCmd.AddCommand(planCmd)
	rootCmd.AddCommand(generateCmd)
}

var generateCmd = &cobra.Command{
	Use:   "generate [yaml_file]",
	Short: "Step 5: End-to-End Orchestration of Open-Source Go Targets",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		data, err := os.ReadFile(args[0])
		if err != nil {
			fmt.Printf("Error reading file: %v\n", err)
			return
		}

		p := parser.NewParser()
		ast, err := p.Parse(data)
		if err != nil {
			fmt.Printf("Parse Error: %v\n", err)
			return
		}

		v := validator.NewValidator()
		errors := v.Validate(ast)
		if len(errors) > 0 {
			fmt.Println("=== VALIDATION FAILED ===")
			return
		}

		resolved := resolver.Resolve(ast)

		dagPlan, err := plan.Build(resolved)
		if err != nil {
			fmt.Printf("DAG Error: %v\n", err)
			return
		}

		hash := diff.HashSpec(data)
		e := template.NewEngine()
		reg := generator.NewRegistry()

		reg.Register(table.NewModelGenerator(e))
		reg.Register(table.NewErrorsGenerator(e))
		reg.Register(table.NewRepoGenerator(e))
		reg.Register(api.NewDTOGenerator(e))
		reg.Register(api.NewContextGenerator(e))
		reg.Register(api.NewHooksGenerator(e))
		reg.Register(api.NewMapperGenerator(e))
		reg.Register(api.NewServiceGenerator(e))
		reg.Register(api.NewHandlerGenerator(e))
		reg.Register(external.NewExternalGenerator(e))
		reg.Register(goTypes.NewTypesGenerator(e))
		reg.Register(infra.NewRoutesGenerator(e))
		reg.Register(infra.NewWireGenerator(e))
		reg.Register(infra.NewGoModGenerator(e))
		reg.Register(infra.NewMainGenerator(e))
		reg.Register(infra.NewDBGenerator(e))
		reg.Register(infra.NewHooksScaffoldGenerator(e))
		reg.Register(infra.NewConfigGenerator(e))

		specDir := filepath.Dir(args[0])
		if specDir == "" || specDir == "." {
			specDir = "."
		}
		emit := emitter.NewEmitter("generated", specDir, hash)
		orch := generator.NewOrchestrator(reg, emit)

		fmt.Printf("Executing Generator Pipeline securely across %d Parallel DAG Tiers...\n", len(dagPlan.Tiers))

		start := time.Now()
		if err := orch.Run(resolved, dagPlan); err != nil {
			fmt.Printf("CRITICAL GENERATION ROUTINE ERROR: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("\n✓ Compiled successfully [%.2fms]\n", float64(time.Since(start).Microseconds())/1000.0)
	},
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func prettyPrint(data interface{}) {
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		fmt.Printf("Failed to marshal JSON: %v\n", err)
		return
	}
	fmt.Println(string(b))
}

var parseCmd = &cobra.Command{
	Use:   "parse [yaml_file]",
	Short: "Step 1: Parse YAML and output raw SpecAST",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		data, err := os.ReadFile(args[0])
		if err != nil {
			fmt.Printf("Error reading file: %v\n", err)
			return
		}

		p := parser.NewParser()
		ast, err := p.Parse(data)
		if err != nil {
			fmt.Printf("Parse Error: %v\n", err)
			return
		}

		fmt.Println("=== [PHASE 3] PARSE STAGE SUCCESS ===")
		prettyPrint(ast)
	},
}

var validateCmd = &cobra.Command{
	Use:   "validate [yaml_file]",
	Short: "Step 2: Parse and Validate YAML",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		data, err := os.ReadFile(args[0])
		if err != nil {
			fmt.Printf("Error reading file: %v\n", err)
			return
		}

		p := parser.NewParser()
		ast, err := p.Parse(data)
		if err != nil {
			fmt.Printf("Parse Error: %v\n", err)
			return
		}

		v := validator.NewValidator()
		errors := v.Validate(ast)

		if len(errors) > 0 {
			fmt.Println("=== [PHASE 4] VALIDATION ERRORS CAUGHT ===")
			for _, e := range errors {
				fmt.Printf("- %s\n", e.Error())
			}
			return
		}

		fmt.Println("=== [PHASE 4] PARSE AND VALIDATION SUCCESS ===")
		fmt.Println("No semantic errors found in SpecAST.")
	},
}

var resolveCmd = &cobra.Command{
	Use:   "resolve [yaml_file]",
	Short: "Step 3: Parse, Validate, and output ResolvedSpec",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		data, err := os.ReadFile(args[0])
		if err != nil {
			fmt.Printf("Error reading file: %v\n", err)
			return
		}

		p := parser.NewParser()
		ast, err := p.Parse(data)
		if err != nil {
			fmt.Printf("Parse Error: %v\n", err)
			return
		}

		v := validator.NewValidator()
		errors := v.Validate(ast)
		if len(errors) > 0 {
			fmt.Println("=== VALIDATION FAILED ===")
			for _, e := range errors {
				fmt.Printf("- %s\n", e.Error())
			}
			return
		}

		resolved := resolver.Resolve(ast)
		fmt.Println("=== [PHASE 5] RESOLVE STAGE SUCCESS ===")
		fmt.Println("Showing strongly-typed Contexts and Hooks...")
		prettyPrint(resolved)
	},
}

var planCmd = &cobra.Command{
	Use:   "plan [yaml_file]",
	Short: "Step 4: Output Kahn's Topographical Sequence Graph",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		data, err := os.ReadFile(args[0])
		if err != nil {
			fmt.Printf("Error reading file: %v\n", err)
			return
		}

		p := parser.NewParser()
		ast, err := p.Parse(data)
		if err != nil {
			fmt.Printf("Parse Error: %v\n", err)
			return
		}

		v := validator.NewValidator()
		errors := v.Validate(ast)
		if len(errors) > 0 {
			fmt.Println("=== VALIDATION FAILED ===")
			return
		}

		resolved := resolver.Resolve(ast)
		dagPlan, err := plan.Build(resolved)
		if err != nil {
			fmt.Printf("DAG Error: %v\n", err)
			return
		}

		fmt.Println("\n=== [PHASE 7] TOPOGRAPHICAL DAG PLANNER RESULTS ===")
		for i, tier := range dagPlan.Tiers {
			fmt.Printf("\n[TIER %d] (Can execute entirely Parallel)\n", i+1)
			for _, node := range tier {
				fmt.Printf("   ├─> [%s] targeting blueprint %s\n", node.ID, node.Generator)
			}
		}
		fmt.Println("")
	},
}
