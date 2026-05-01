package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"stencil/internal/diff"
	"stencil/internal/emit"
	generator "stencil/internal/generate"
	"stencil/internal/codegen/go/api"
	"stencil/internal/codegen/go/external"
	"stencil/internal/codegen/go/infra"
	"stencil/internal/codegen/go/table"
	goTypes "stencil/internal/codegen/go/types"
	"stencil/internal/plan"
	"stencil/internal/spec"
	parser "stencil/internal/parse"
	resolver "stencil/internal/resolve"
	validator "stencil/internal/validate"
	"stencil/internal/template"
)

// Injected at build time by GoReleaser via -ldflags.
// Defaults to "dev" when building locally with go build.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "stencil",
	Short: "Stencil CLI",
}

func init() {
	rootCmd.AddCommand(parseCmd)
	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(resolveCmd)
	rootCmd.AddCommand(planCmd)
	rootCmd.AddCommand(generateCmd)
	rootCmd.AddCommand(diffCmd)
	rootCmd.AddCommand(versionCmd)

	generateCmd.Flags().Bool("force", false, "Regenerate even if the spec has not changed")
}

// ─── version ─────────────────────────────────────────────────────────────────

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print stencil version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("stencil %s\n", version)
		fmt.Printf("  commit : %s\n", commit)
		fmt.Printf("  built  : %s\n", date)
	},
}

// ─── generate ────────────────────────────────────────────────────────────────

var generateCmd = &cobra.Command{
	Use:   "generate [yaml_file]",
	Short: "Generate code from a stencil.yaml spec",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		force, _ := cmd.Flags().GetBool("force")

		data, err := os.ReadFile(args[0])
		if err != nil {
			fmt.Printf("Error reading file: %v\n", err)
			return
		}

		specDir := filepath.Dir(args[0])
		if specDir == "" {
			specDir = "."
		}

		hash := diff.HashSpec(data)
		if !force {
			lock, lockErr := diff.ReadLock(specDir)
			if lockErr != nil {
				fmt.Printf("Warning: could not read lockfile: %v\n", lockErr)
			}
			if lock != nil && lock.SpecHash == hash {
				fmt.Println("Spec unchanged since last generation. Use --force to regenerate anyway.")
				return
			}
		}

		resolved, dagPlan, ok := parseAndPlan(args[0])
		if !ok {
			return
		}

		emit := emitter.NewEmitter(filepath.Join(specDir, "generated"), specDir, hash)
		orch := generator.NewOrchestrator(buildRegistry(), emit)

		label := "Executing Generator Pipeline"
		if force {
			label += " (--force)"
		}
		fmt.Printf("%s across %d Parallel DAG Tiers...\n", label, len(dagPlan.Tiers))

		start := time.Now()
		if err := orch.Run(resolved, dagPlan); err != nil {
			fmt.Printf("Generation failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("\n✓ Compiled successfully [%.2fms]\n", float64(time.Since(start).Microseconds())/1000.0)
	},
}

// ─── diff ────────────────────────────────────────────────────────────────────

var diffCmd = &cobra.Command{
	Use:   "diff [yaml_file]",
	Short: "Show what would change if you ran generate now",
	Long: `Generates code into a temporary directory and compares it against
the existing generated/ output. Prints a unified diff to the terminal.
No files are written.`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		data, err := os.ReadFile(args[0])
		if err != nil {
			fmt.Printf("Error reading file: %v\n", err)
			return
		}

		specDir := filepath.Dir(args[0])
		if specDir == "" {
			specDir = "."
		}
		existingDir := filepath.Join(specDir, "generated")

		resolved, dagPlan, ok := parseAndPlan(args[0])
		if !ok {
			return
		}

		// Generate into a temp directory — nothing is written to the real output.
		tmpDir, err := os.MkdirTemp("", "stencil-diff-*")
		if err != nil {
			fmt.Printf("Error creating temp dir: %v\n", err)
			return
		}
		defer os.RemoveAll(tmpDir)

		hash := diff.HashSpec(data)
		emit := emitter.NewEmitter(filepath.Join(tmpDir, "generated"), tmpDir, hash)
		orch := generator.NewOrchestrator(buildRegistry(), emit)

		fmt.Fprintln(os.Stderr, "Computing diff (generating to temp dir)...")
		if err := orch.Run(resolved, dagPlan); err != nil {
			fmt.Printf("Generation failed: %v\n", err)
			return
		}

		// The generator outputs to tmpDir/generated/
		newDir := filepath.Join(tmpDir, "generated")

		// Collect all file paths from both directories.
		newFiles := collectFiles(newDir)
		oldFiles := collectFiles(existingDir)

		// Union of all paths, sorted for stable output.
		allPaths := union(newFiles, oldFiles)
		sort.Strings(allPaths)

		color := diff.UseColor()
		var summary diff.DiffSummary

		for _, relPath := range allPaths {
			_, inNew := newFiles[relPath]
			_, inOld := oldFiles[relPath]

			var fd diff.FileDiff

			switch {
			case inNew && !inOld:
				// New file — show all lines as added.
				content := readFileOrEmpty(filepath.Join(newDir, relPath))
				fd = diff.NewFileDiff(relPath, content, 3)
				summary.Added++

			case !inNew && inOld:
				// Deleted file — show all lines as removed.
				content := readFileOrEmpty(filepath.Join(existingDir, relPath))
				fd = diff.DeletedFileDiff(relPath, content, 3)
				summary.Deleted++

			default:
				// Both exist — unified diff.
				oldText := readFileOrEmpty(filepath.Join(existingDir, relPath))
				newText := readFileOrEmpty(filepath.Join(newDir, relPath))
				fd = diff.DiffFiles(relPath, relPath, oldText, newText, 3)
				if fd.HasChanges() {
					summary.Modified++
				} else {
					summary.Unchanged++
				}
			}

			if fd.HasChanges() {
				diff.PrintFileDiff(os.Stdout, fd, color)
			}
		}

		diff.PrintSummary(os.Stdout, summary, color)
	},
}

// ─── Shared helpers ───────────────────────────────────────────────────────────

// buildRegistry creates and returns the generator registry used by both
// generate and diff commands.
func buildRegistry() *generator.Registry {
	e := template.NewEngine()
	reg := generator.NewRegistry()

	reg.Register(table.NewModelGenerator(e))
	reg.Register(table.NewErrorsGenerator(e))
	reg.Register(table.NewRepoGenerator(e))
	reg.Register(api.NewDTOGenerator(e))
	reg.Register(api.NewContextGenerator(e))
	reg.Register(api.NewHooksGenerator(e))
	reg.Register(api.NewHookScaffoldGenerator(e))
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

	return reg
}

// parseAndPlan runs parse → validate → resolve → plan.
// Returns (resolved, plan, ok). If ok is false, errors were printed and the
// caller should return early.
func parseAndPlan(yamlFile string) (*spec.ResolvedSpec, *plan.Plan, bool) {
	data, err := os.ReadFile(yamlFile)
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		return nil, nil, false
	}

	p := parser.NewParser()
	ast, err := p.Parse(data)
	if err != nil {
		fmt.Printf("Parse Error: %v\n", err)
		return nil, nil, false
	}

	v := validator.NewValidator()
	errors := v.Validate(ast)
	if len(errors) > 0 {
		fmt.Println("=== VALIDATION FAILED ===")
		for _, e := range errors {
			fmt.Printf("  - %s\n", e.Error())
		}
		return nil, nil, false
	}

	resolved := resolver.Resolve(ast)

	dagPlan, err := plan.Build(resolved)
	if err != nil {
		fmt.Printf("DAG Error: %v\n", err)
		return nil, nil, false
	}

	return resolved, dagPlan, true
}

// collectFiles walks dir and returns a map of relative path → struct{}.
func collectFiles(dir string) map[string]struct{} {
	result := map[string]struct{}{}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return result
	}
	_ = fs.WalkDir(os.DirFS(dir), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		// Only diff Go and template files — skip lockfile, binaries, etc.
		if isTextFile(path) {
			result[filepath.ToSlash(path)] = struct{}{}
		}
		return nil
	})
	return result
}

// isTextFile returns true for files we want to include in the diff.
func isTextFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go", ".yaml", ".yml", ".toml", ".json", ".md", ".mod", ".sum", ".tmpl":
		return true
	}
	return false
}

// union returns all keys present in either map.
func union(a, b map[string]struct{}) []string {
	seen := map[string]struct{}{}
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for k := range seen {
		result = append(result, k)
	}
	return result
}

// readFileOrEmpty reads a file and returns its content as a string.
func readFileOrEmpty(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// ─── main ────────────────────────────────────────────────────────────────────

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

// ─── diagnostic sub-commands ─────────────────────────────────────────────────

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

		fmt.Println("=== PARSE STAGE SUCCESS ===")
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
			fmt.Println("=== VALIDATION ERRORS ===")
			for _, e := range errors {
				fmt.Printf("  - %s\n", e.Error())
			}
			return
		}

		fmt.Println("=== VALIDATION SUCCESS ===")
		fmt.Println("No errors found.")
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
				fmt.Printf("  - %s\n", e.Error())
			}
			return
		}

		resolved := resolver.Resolve(ast)
		fmt.Println("=== RESOLVE STAGE SUCCESS ===")
		prettyPrint(resolved)
	},
}

var planCmd = &cobra.Command{
	Use:   "plan [yaml_file]",
	Short: "Step 4: Output topographical DAG execution plan",
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

		fmt.Println("\n=== DAG EXECUTION PLAN ===")
		for i, tier := range dagPlan.Tiers {
			fmt.Printf("\n[TIER %d] (parallel)\n", i+1)
			for _, node := range tier {
				fmt.Printf("   ├─> [%s] → %s\n", node.ID, node.Generator)
			}
		}
		fmt.Println("")
	},
}
