package simulator

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/pterm/pterm"
)

type Console struct{}

func NewConsole() *Console {
	return &Console{}
}

func (c *Console) PrintHeader(name string, duration time.Duration, nodeCount int, seed int64, failureRate float64, cascading bool) {
	pterm.DefaultHeader.WithBackgroundStyle(pterm.NewStyle(pterm.BgDarkGray)).
		WithTextStyle(pterm.NewStyle(pterm.FgLightCyan, pterm.Bold)).
		Println("STRESS TEST: " + name)

	fmt.Println()

	configPanel := pterm.DefaultBox.WithTitle("Configuration").WithTitleTopCenter()
	configContent := fmt.Sprintf(
		"Duration: %s\nNodes: %d\nSeed: %d",
		duration.String(), nodeCount, seed,
	)
	if failureRate > 0 {
		configContent += fmt.Sprintf("\nFailure Rate: %.1f/min/1000", failureRate)
		configContent += fmt.Sprintf("\nCascading: %v", cascading)
	}
	configPanel.Println(configContent)
	fmt.Println()
}

func (c *Console) PrintProgress(pct float64, elapsed, remaining time.Duration, healthyNodes int64, failures, cascading, recoveries int64) {
	status := fmt.Sprintf("[%5.1f%%] %s elapsed, %s remaining | Nodes: %d healthy | Failures: %d (cascade: %d) | Recoveries: %d",
		pct,
		elapsed.Round(time.Second),
		remaining.Round(time.Second),
		healthyNodes,
		failures,
		cascading,
		recoveries)
	fmt.Printf("\r%-120s", status)
}

func (c *Console) ClearProgress() {
	fmt.Printf("\r%120s\r", "")
}

func (c *Console) PrintResults(results *StressResults) {
	pterm.DefaultHeader.WithBackgroundStyle(pterm.NewStyle(pterm.BgDarkGray)).
		WithTextStyle(pterm.NewStyle(pterm.FgLightGreen, pterm.Bold)).
		Println("STRESS TEST RESULTS")

	fmt.Println()
	pterm.Info.Printfln("Duration: %s", results.Duration.Round(time.Millisecond))
	fmt.Println()

	nodeData := pterm.TableData{
		{"Metric", "Value"},
		{"Started", fmt.Sprintf("%d", results.NodesStarted)},
		{"Failed to Start", fmt.Sprintf("%d", results.NodesFailed)},
		{"Healthy", fmt.Sprintf("%d", results.NodesHealthy)},
		{"Unhealthy", fmt.Sprintf("%d", results.NodesUnhealthy)},
		{"Degraded", fmt.Sprintf("%d", results.NodesDegraded)},
	}

	pterm.DefaultSection.Println("Nodes")
	pterm.DefaultTable.WithHasHeader().WithBoxed().WithData(nodeData).Render()
	fmt.Println()

	failureData := pterm.TableData{
		{"Metric", "Value"},
		{"Total Failures", fmt.Sprintf("%d", results.TotalFailures)},
		{"Cascading", fmt.Sprintf("%d", results.CascadingFailures)},
		{"Recoveries", fmt.Sprintf("%d", results.Recoveries)},
	}
	pterm.DefaultSection.Println("Failures")
	pterm.DefaultTable.WithHasHeader().WithBoxed().WithData(failureData).Render()
	fmt.Println()

	if len(results.FailuresByType) > 0 {
		typeData := pterm.TableData{{"Type", "Count"}}
		for ftype, count := range results.FailuresByType {
			typeData = append(typeData, []string{ftype, fmt.Sprintf("%d", count)})
		}
		pterm.DefaultSection.Println("Failure Types")
		pterm.DefaultTable.WithHasHeader().WithBoxed().WithData(typeData).Render()
		fmt.Println()
	}

	if len(results.FailuresByXID) > 0 {
		type xidEntry struct {
			code  int
			count int64
		}
		var entries []xidEntry
		for code, count := range results.FailuresByXID {
			entries = append(entries, xidEntry{code, count})
		}
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].count > entries[j].count
		})

		xidData := pterm.TableData{{"XID Code", "Name", "Count", "Severity"}}
		for i := 0; i < len(entries) && i < 5; i++ {
			info, known := XIDCodes[entries[i].code]
			name := "Unknown"
			severity := "Unknown"
			if known {
				name = info.Name
				if info.Fatal {
					severity = pterm.Red("FATAL")
				} else {
					severity = pterm.Green("Recoverable")
				}
			}
			if len(name) > 30 {
				name = name[:27] + "..."
			}
			xidData = append(xidData, []string{
				fmt.Sprintf("%d", entries[i].code),
				name,
				fmt.Sprintf("%d", entries[i].count),
				severity,
			})
		}

		pterm.DefaultSection.Println("Top XID Errors")
		pterm.DefaultTable.WithHasHeader().WithBoxed().WithData(xidData).Render()
		fmt.Println()
	}
}

func (c *Console) PrintReports(files []string) {
	if len(files) == 0 {
		return
	}

	pterm.DefaultSection.Println("Reports Generated")
	for _, f := range files {
		pterm.Success.Println(f)
	}

	var htmlPath string
	for _, f := range files {
		if strings.Contains(f, "(HTML)") {
			htmlPath = strings.TrimSuffix(f, " (HTML)")
			break
		}
		// Check if this is a run directory (contains report.html)
		if !strings.Contains(f, "(") {
			possibleHTML := filepath.Join(f, "report.html")
			if _, err := os.Stat(possibleHTML); err == nil {
				htmlPath = possibleHTML
				break
			}
		}
	}

	if htmlPath != "" {
		absPath, err := filepath.Abs(htmlPath)
		if err == nil {
			fmt.Println()
			pterm.Info.Println("View HTML report in browser:")
			fmt.Printf("  file://%s\n", absPath)
		}
	}
}

func (c *Console) PrintSuccess(msg string) {
	fmt.Println()
	pterm.Success.Println(msg)
}

func (c *Console) PrintError(msg string) {
	pterm.Error.Println(msg)
}

func (c *Console) PrintWebView(addr string) {
	pterm.Info.Printfln("Live web view: %s", addr)
}

func (c *Console) PrintRunning(duration time.Duration) {
	pterm.Info.Printfln("Running stress test for %s...", duration)
	fmt.Println()
}

type StressResults struct {
	Duration          time.Duration
	NodesStarted      int64
	NodesFailed       int64
	NodesHealthy      int64
	NodesUnhealthy    int64
	NodesDegraded     int64
	TotalFailures     int64
	CascadingFailures int64
	Recoveries        int64
	FailuresByType    map[string]int64
	FailuresByXID     map[int]int64
}
