package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/fatih/color"
	"os"
	"os/exec"
	"path"
	"strings"
)

type Sarif struct {
	Runs []SarifRun
}

type SarifRun struct {
	Results []SarifResult
}

type SarifResult struct {
	Message   SarifMessage
	Locations []SarifLocation
}

type SarifMessage struct {
	Text string
}

type SarifLocation struct {
	PhysicalLocation SarifPhysicalLocation
}

type SarifPhysicalLocation struct {
	ArtifactLocation SarifArtifactLocation
	Region           SarifRegion
}

type SarifRegion struct {
	StartLine   int
	StartColumn int
	EndLine     int
}

type SarifArtifactLocation struct {
	Uri string
}

type Finding struct {
	Rule     string `json:"rule"`
	Message  string `json:"message"`
	File     string `json:"file"`
	Line     int    `json:"line"`
	Severity string `json:"severity"`
}

func main() {
	dir := flag.String("dir", ".", "Directory to analyze")
	command := flag.String("command", "", "Custom build command")
	flag.Parse()

	if err := runMigrationCheck(*dir, *command); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runMigrationCheck(dir, customBuildCommand string) error {
	dbPath, err := os.MkdirTemp(os.TempDir(), "cosmos-migration-db")

	if err != nil {
		return err
	}

	defer os.RemoveAll(dbPath)

	command := []string{
		"codeql",
		"database",
		"create",
		"--language=go",
		"--source-root", dir,
	}

	if customBuildCommand != "" {
		command = append(command, "--command", customBuildCommand)
		fmt.Println("Using custom build command:", customBuildCommand)
	}

	command = append(command, dbPath)

	fmt.Println(command)

	cmd := exec.Command(command[0], command[1:]...)

	if err := cmd.Run(); err != nil {
		return err
	}

	results, err := runAnalysis(dbPath)
	if err != nil {
		return err
	}

	return printFindings(results)
}

func runAnalysis(dbPath string) (*Sarif, error) {
	// Run CodeQL analysis with your custom pack
	resultsPath := path.Join(dbPath, "results.json")
	cmd := exec.Command("codeql", "database", "analyze",
		"--format=sarif-latest",
		fmt.Sprintf("--output=%s", resultsPath),
		dbPath,
		"skip-mev/cosmos-52-ql")

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("analysis failed: %w", err)
	}

	// Parse results
	data, err := os.ReadFile(resultsPath)
	if err != nil {
		return nil, err
	}

	var sarif Sarif
	if err := json.Unmarshal(data, &sarif); err != nil {
		return nil, err
	}

	return &sarif, nil
}

func printFindings(sarif *Sarif) error {
	red := color.New(color.FgRed)
	yellow := color.New(color.FgYellow)
	for _, run := range sarif.Runs {
		for _, result := range run.Results {
			for _, location := range result.Locations {
				uri := location.PhysicalLocation.ArtifactLocation.Uri
				line := location.PhysicalLocation.Region.StartLine
				column := location.PhysicalLocation.Region.StartColumn

				code, err := readSpecificLine(uri, line)

				if err != nil {
					return fmt.Errorf("failed to read file: %w", err)
				}
				fmt.Printf("%s:%d:%d: %s\n", uri, line, column, red.Sprint(result.Message.Text))
				fmt.Printf("  %d: %s\n", line, code)
				fmt.Println(strings.Repeat(" ", column) + yellow.Sprint("^"))
			}
		}
	}
	return nil
}

func readSpecificLine(filePath string, targetLine int) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	currentLine := 1

	for scanner.Scan() {
		if currentLine == targetLine {
			return scanner.Text(), nil
		}
		currentLine++
	}

	if err := scanner.Err(); err != nil {
		return "", err
	}

	return "", fmt.Errorf("file has less than %d lines", targetLine)
}
