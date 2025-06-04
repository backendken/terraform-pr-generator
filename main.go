package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

type PlanGenerator struct {
	ModuleName string
	OutputDir  string
	Verbose    bool
}

type Environment struct {
	Name    string
	Regions []string
	Plans   map[string]string // region -> plan content
}

// Color definitions for better UX
var (
	successColor = color.New(color.FgGreen, color.Bold)
	errorColor   = color.New(color.FgRed, color.Bold)
	warningColor = color.New(color.FgYellow, color.Bold)
	infoColor    = color.New(color.FgCyan, color.Bold)
	boldColor    = color.New(color.Bold)
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "terraform-pr-generator [module_name]",
		Short: "Generate terraform plans for PR workflow",
		Long: `A CLI tool to automate terraform plan generation for PR workflow.
Generates plans for all environments and regions, formatted for GitHub PRs.

Examples:
  terraform-pr-generator s3_malware_protection
  terraform-pr-generator s3_malware_protection --verbose --targeted
  terraform-pr-generator s3_malware_protection --output my-custom-dir`,
		Args: cobra.ExactArgs(1),
		Run:  runPlanGenerator,
	}

	rootCmd.Flags().BoolP("verbose", "v", false, "Enable verbose output")
	rootCmd.Flags().BoolP("targeted", "t", false, "Use targeted planning (affected-modules.sh)")
	rootCmd.Flags().StringP("output", "o", "", "Custom output directory (default: pr-plans-TIMESTAMP)")

	if err := rootCmd.Execute(); err != nil {
		errorColor.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runPlanGenerator(cmd *cobra.Command, args []string) {
	moduleName := args[0]
	verbose, _ := cmd.Flags().GetBool("verbose")
	targeted, _ := cmd.Flags().GetBool("targeted")
	outputDir, _ := cmd.Flags().GetString("output")

	if outputDir == "" {
		outputDir = fmt.Sprintf("pr-plans-%s", time.Now().Format("20060102-150405"))
	}

	pg := &PlanGenerator{
		ModuleName: moduleName,
		OutputDir:  outputDir,
		Verbose:    verbose,
	}

	infoColor.Printf("🚀 Generating terraform plans for module: %s\n", moduleName)
	fmt.Printf("📝 Plans will be saved to: %s/\n\n", outputDir)

	// Validate module exists
	if err := pg.validateModule(); err != nil {
		errorColor.Printf("❌ Error: %v\n", err)
		os.Exit(1)
	}

	// Create output directory
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		errorColor.Printf("❌ Error creating output directory: %v\n", err)
		os.Exit(1)
	}

	var affectedPlans []string
	var err error

	if targeted {
		infoColor.Println("🎯 Finding affected states using affected-modules.sh...")
		affectedPlans, err = pg.findAffectedPlans()
		if err != nil || len(affectedPlans) == 0 {
			if pg.Verbose {
				warningColor.Printf("⚠️  Targeted planning failed or found no plans: %v\n", err)
				fmt.Println("Falling back to plan_all method...")
			}
			targeted = false
		} else {
			successColor.Printf("📋 Found %d affected terraform states\n", len(affectedPlans))
			if pg.Verbose {
				for i, plan := range affectedPlans {
					if i < 5 {
						fmt.Printf("  - %s\n", plan)
					}
				}
				if len(affectedPlans) > 5 {
					fmt.Printf("  ... and %d more\n", len(affectedPlans)-5)
				}
			}
			fmt.Println()
		}
	}

	if targeted {
		infoColor.Println("⚡ Running targeted plans for affected states...")
		err = pg.runTargetedPlans(affectedPlans)
	} else {
		infoColor.Println("🏢 Running plans for Commercial accounts...")
		infoColor.Println("🏛️  Running plans for GovCloud accounts...")
		err = pg.runPlanAll()
	}

	if err != nil {
		errorColor.Printf("❌ Error generating plans: %v\n", err)
		os.Exit(1)
	}

	// Generate formatted PR markdown
	if err := pg.generatePRMarkdown(); err != nil {
		errorColor.Printf("❌ Error generating PR markdown: %v\n", err)
		os.Exit(1)
	}

	successColor.Println("✅ Plan generation complete!")
	boldColor.Printf("📄 PR-ready markdown: %s/pr-ready.md\n\n", outputDir)

	fmt.Println("🚀 Quick commands:")
	fmt.Printf("  # Copy PR markdown to clipboard:\n")
	color.New(color.FgGreen).Printf("  cat %s/pr-ready.md | pbcopy\n\n", outputDir)
	fmt.Printf("  # View plans:\n")
	color.New(color.FgCyan).Printf("  less %s/commercial-plans.txt\n", outputDir)
	color.New(color.FgCyan).Printf("  less %s/govcloud-plans.txt\n", outputDir)
}

func (pg *PlanGenerator) validateModule() error {
	moduleDir := fmt.Sprintf("terragrunt_%s", pg.ModuleName)
	if _, err := os.Stat(moduleDir); os.IsNotExist(err) {
		return fmt.Errorf("module %s not found in current directory.\nMake sure you're running this from the elon-modules root directory", moduleDir)
	}
	return nil
}

func (pg *PlanGenerator) findAffectedPlans() ([]string, error) {
	if _, err := os.Stat("./affected-modules.sh"); os.IsNotExist(err) {
		return nil, fmt.Errorf("affected-modules.sh not found in current directory")
	}

	cmd := exec.Command("./affected-modules.sh", pg.ModuleName, ".")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run affected-modules.sh: %v", err)
	}

	var plans []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.Contains(line, "kitman tg plan") {
			// Extract the path
			parts := strings.Fields(line)
			for i, part := range parts {
				if part == "-w" && i+1 < len(parts) {
					planPath := strings.Replace(parts[i+1], "/terragrunt.hcl", "", 1)
					plans = append(plans, planPath)
					break
				}
			}
		}
	}

	return plans, nil
}

func (pg *PlanGenerator) runPlanAll() error {
	var wg sync.WaitGroup
	var commercialErr, govcloudErr error

	// Run commercial plans
	wg.Add(1)
	go func() {
		defer wg.Done()
		if pg.Verbose {
			fmt.Println("  → Running commercial account plans...")
		}
		commercialErr = pg.runCommand("kitman", []string{
			"tg", "plan_all", "-m", pg.ModuleName, "--local", "--pr",
		}, filepath.Join(pg.OutputDir, "commercial-plans.txt"))
	}()

	// Run govcloud plans
	wg.Add(1)
	go func() {
		defer wg.Done()
		if pg.Verbose {
			fmt.Println("  → Running GovCloud account plans...")
		}
		govcloudErr = pg.runCommand("kitman", []string{
			"tg", "plan_all", "-m", pg.ModuleName,
			"--organizations", "govcloud-staging|govcloud-production",
			"--regions", "us-gov-west-1", "--local", "--pr",
		}, filepath.Join(pg.OutputDir, "govcloud-plans.txt"))
	}()

	wg.Wait()

	if commercialErr != nil {
		return fmt.Errorf("commercial plans failed: %v", commercialErr)
	}
	if govcloudErr != nil {
		return fmt.Errorf("govcloud plans failed: %v", govcloudErr)
	}

	return nil
}

func (pg *PlanGenerator) runTargetedPlans(affectedPlans []string) error {
	var commercialPlans, govcloudPlans []string

	for _, plan := range affectedPlans {
		if strings.Contains(plan, "govcloud") {
			govcloudPlans = append(govcloudPlans, plan)
		} else {
			commercialPlans = append(commercialPlans, plan)
		}
	}

	var wg sync.WaitGroup
	var commercialErr, govcloudErr error

	// Run commercial plans
	if len(commercialPlans) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if pg.Verbose {
				fmt.Printf("  → Running %d commercial plans...\n", len(commercialPlans))
			}
			commercialErr = pg.runTargetedPlanGroup(commercialPlans, "commercial-plans.txt")
		}()
	} else {
		// Create empty file
		os.WriteFile(filepath.Join(pg.OutputDir, "commercial-plans.txt"), []byte("No commercial plans needed\n"), 0644)
	}

	// Run govcloud plans
	if len(govcloudPlans) > 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if pg.Verbose {
				fmt.Printf("  → Running %d GovCloud plans...\n", len(govcloudPlans))
			}
			govcloudErr = pg.runTargetedPlanGroup(govcloudPlans, "govcloud-plans.txt")
		}()
	} else {
		// Create empty file
		os.WriteFile(filepath.Join(pg.OutputDir, "govcloud-plans.txt"), []byte("No GovCloud plans needed\n"), 0644)
	}

	wg.Wait()

	if commercialErr != nil {
		return fmt.Errorf("commercial plans failed: %v", commercialErr)
	}
	if govcloudErr != nil {
		return fmt.Errorf("govcloud plans failed: %v", govcloudErr)
	}

	return nil
}

func (pg *PlanGenerator) runTargetedPlanGroup(plans []string, outputFile string) error {
	outputPath := filepath.Join(pg.OutputDir, outputFile)
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	for _, planDir := range plans {
		if pg.Verbose {
			fmt.Printf("    Planning: %s\n", planDir)
		}
		cmd := exec.Command("kitman", "tg", "plan", "--wd", planDir, "--local", "--pr")
		output, err := cmd.Output()
		if err != nil {
			return fmt.Errorf("failed to run plan for %s: %v", planDir, err)
		}
		file.Write(output)
		file.WriteString("\n")
	}

	return nil
}

func (pg *PlanGenerator) runCommand(command string, args []string, outputFile string) error {
	cmd := exec.Command(command, args...)
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("command failed: %s %v - %v", command, args, err)
	}

	return os.WriteFile(outputFile, output, 0644)
}

func (pg *PlanGenerator) generatePRMarkdown() error {
	outputPath := filepath.Join(pg.OutputDir, "pr-ready.md")
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}
	defer file.Close()

	file.WriteString("**Terraform plan**\n\n")

	// Process commercial plans
	if err := pg.processPlansFile("commercial-plans.txt", file, false); err != nil {
		return fmt.Errorf("error processing commercial plans: %v", err)
	}

	// Process govcloud plans
	if err := pg.processPlansFile("govcloud-plans.txt", file, true); err != nil {
		return fmt.Errorf("error processing govcloud plans: %v", err)
	}

	return nil
}

func (pg *PlanGenerator) processPlansFile(filename string, output *os.File, isGovcloud bool) error {
	filePath := filepath.Join(pg.OutputDir, filename)
	content, err := os.ReadFile(filePath)
	if err != nil || len(content) == 0 {
		return nil // Skip if file doesn't exist or is empty
	}

	contentStr := string(content)
	if strings.Contains(contentStr, "No commercial plans needed") || strings.Contains(contentStr, "No GovCloud plans needed") {
		return nil // Skip empty placeholder files
	}

	envRegex := regexp.MustCompile(`/organizations/([^/]+)/`)
	if isGovcloud {
		envRegex = regexp.MustCompile(`(govcloud-[^/]+)`)
	}

	regionRegex := regexp.MustCompile(`/([a-z]{2}-[a-z]+-[0-9])/`)
	if isGovcloud {
		regionRegex = regexp.MustCompile(`(us-gov-[a-z]+-[0-9])`)
	}

	environments := make(map[string]*Environment)
	lines := strings.Split(contentStr, "\n")

	var currentEnv, currentRegion string
	var planLines []string
	var inPlanSection bool

	for _, line := range lines {
		// Check for environment/region markers in file paths
		if envMatches := envRegex.FindStringSubmatch(line); len(envMatches) > 1 {
			currentEnv = envMatches[1]
		}
		if regionMatches := regionRegex.FindStringSubmatch(line); len(regionMatches) > 1 {
			currentRegion = regionMatches[1]
		}

		// Start collecting plan content when we see "Terraform will perform"
		if strings.Contains(line, "Terraform will perform the following actions:") {
			inPlanSection = true
			planLines = []string{line}
			continue
		}

		// If we're in a plan section, collect lines
		if inPlanSection {
			planLines = append(planLines, line)

			// End plan section when we see "Plan: X to add, Y to change, Z to destroy"
			if strings.Contains(line, "Plan:") && (strings.Contains(line, "to add") || strings.Contains(line, "to change") || strings.Contains(line, "to destroy")) {
				if currentEnv != "" && currentRegion != "" {
					if environments[currentEnv] == nil {
						environments[currentEnv] = &Environment{
							Name:    currentEnv,
							Regions: []string{},
							Plans:   make(map[string]string),
						}
					}

					if !contains(environments[currentEnv].Regions, currentRegion) {
						environments[currentEnv].Regions = append(environments[currentEnv].Regions, currentRegion)
					}

					environments[currentEnv].Plans[currentRegion] = strings.Join(planLines, "\n")
				}
				planLines = []string{}
				inPlanSection = false
			}
		}
	}

	// Sort environments and output
	var envNames []string
	for name := range environments {
		envNames = append(envNames, name)
	}
	sort.Strings(envNames)

	for _, envName := range envNames {
		env := environments[envName]
		output.WriteString(fmt.Sprintf("## [environment: %s] - [command: kitman tg plan_all] - [module: %s]\n\n", env.Name, pg.ModuleName))

		sort.Strings(env.Regions)
		for _, region := range env.Regions {
			if planContent, exists := env.Plans[region]; exists && planContent != "" {
				output.WriteString(fmt.Sprintf("<details>\n<summary>%s</summary>\n\n```bash\n", region))
				output.WriteString(planContent)
				output.WriteString("\n```\n\n</details>\n\n")
			}
		}
	}

	return nil
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
