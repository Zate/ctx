package view

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
	viewpkg "github.com/zate/ctx/internal/view"
)

var (
	composeQuery    string
	composeBudget   int
	composeIDs      string
	composeEdges    bool
	composeTemplate string
	composeSeed     string
	composeDepth    int
	composeProject  string
)

var composeCmd = &cobra.Command{
	Use:   "compose",
	Short: "Compose context from query or node IDs",
	RunE:  runCompose,
}

func init() {
	defaultBudget := 50000
	if envBudget := os.Getenv("CTX_DEFAULT_BUDGET"); envBudget != "" {
		if n, err := strconv.Atoi(envBudget); err == nil {
			defaultBudget = n
		}
	}
	composeCmd.Flags().StringVar(&composeQuery, "query", "", "Query expression")
	composeCmd.Flags().IntVar(&composeBudget, "budget", defaultBudget, "Token budget")
	composeCmd.Flags().StringVar(&composeIDs, "ids", "", "Comma-separated node IDs to compose (supports short prefixes)")
	composeCmd.Flags().BoolVar(&composeEdges, "edges", false, "Include relationships between composed nodes")
	composeCmd.Flags().StringVar(&composeTemplate, "template", "", "Render using template: default, document")
	composeCmd.Flags().StringVar(&composeSeed, "seed", "", "Seed node ID for graph traversal")
	composeCmd.Flags().IntVar(&composeDepth, "depth", 1, "Traversal depth for seed mode")
	composeCmd.Flags().StringVar(&composeProject, "project", "", "Project scope for filtering")
	register(composeCmd)
}

func runCompose(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	opts := viewpkg.ComposeOptions{
		Query:        composeQuery,
		Budget:       composeBudget,
		IncludeEdges: composeEdges,
		SeedID:       composeSeed,
		Depth:        composeDepth,
		Agent:        cmdutil.Agent(cmd),
		Project:      composeProject,
	}

	if composeIDs != "" {
		ids := strings.Split(composeIDs, ",")
		for i := range ids {
			ids[i] = strings.TrimSpace(ids[i])
		}
		opts.IDs = ids
	}

	result, err := viewpkg.Compose(d, opts)
	if err != nil {
		return err
	}

	composeCtx := "compose"
	switch {
	case composeIDs != "":
		composeCtx = "compose:ids"
	case composeSeed != "":
		composeCtx = "compose:seed:" + composeSeed
	case composeQuery != "":
		composeCtx = "compose:" + composeQuery
	}
	cmdutil.LogAccessNodes(cmd, d, result.Nodes, "explicit_query", composeCtx)

	// If a template is specified, use template rendering
	if composeTemplate != "" {
		fmt.Print(viewpkg.RenderTemplate(result, composeTemplate))
		return nil
	}

	if cmdutil.AgentOut(cmd) {
		fmt.Fprintf(os.Stdout, "ok compose nodes=%d tokens=%d\n", len(result.Nodes), result.TotalTokens)
		fmt.Fprint(os.Stdout, viewpkg.RenderText(result))
		return nil
	}

	switch cmdutil.Format(cmd) {
	case "json":
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
	case "markdown":
		fmt.Print(viewpkg.RenderMarkdown(result))
	default:
		fmt.Print(viewpkg.RenderText(result))
	}

	return nil
}
