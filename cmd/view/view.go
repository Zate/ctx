package view

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
	viewpkg "github.com/zate/ctx/internal/view"
)

var viewCmd = &cobra.Command{
	Use:   "view",
	Short: "Manage named views",
}

var viewCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a named view",
	Args:  cobra.ExactArgs(1),
	RunE:  runViewCreate,
}

var viewListCmd = &cobra.Command{
	Use:   "list",
	Short: "List views",
	RunE:  runViewList,
}

var viewRenderCmd = &cobra.Command{
	Use:   "render <name>",
	Short: "Render a named view",
	Args:  cobra.ExactArgs(1),
	RunE:  runViewRender,
}

var viewDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a view",
	Args:  cobra.ExactArgs(1),
	RunE:  runViewDelete,
}

var (
	viewQuery  string
	viewBudget int
)

func init() {
	defaultBudget := 50000
	if envBudget := os.Getenv("CTX_DEFAULT_BUDGET"); envBudget != "" {
		if n, err := strconv.Atoi(envBudget); err == nil {
			defaultBudget = n
		}
	}
	viewCreateCmd.Flags().StringVar(&viewQuery, "query", "", "Query expression")
	_ = viewCreateCmd.MarkFlagRequired("query")
	viewCreateCmd.Flags().IntVar(&viewBudget, "budget", defaultBudget, "Token budget")

	viewRenderCmd.Flags().IntVar(&viewBudget, "budget", 0, "Override budget")

	viewCmd.AddCommand(viewCreateCmd, viewListCmd, viewRenderCmd, viewDeleteCmd)
	register(viewCmd)
}

func runViewCreate(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = d.Exec(`INSERT OR REPLACE INTO views (name, query, budget, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		args[0], viewQuery, viewBudget, now, now)
	if err != nil {
		return fmt.Errorf("failed to create view: %w", err)
	}

	fmt.Printf("Created view: %s\n", args[0])
	return nil
}

func runViewList(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	rows, err := d.Query("SELECT name, query, budget FROM views ORDER BY name")
	if err != nil {
		return err
	}
	defer rows.Close()

	type viewInfo struct {
		Name   string `json:"name"`
		Query  string `json:"query"`
		Budget int    `json:"budget"`
	}

	var views []viewInfo
	for rows.Next() {
		var v viewInfo
		_ = rows.Scan(&v.Name, &v.Query, &v.Budget)
		views = append(views, v)
	}

	switch cmdutil.Format(cmd) {
	case "json":
		data, _ := json.MarshalIndent(views, "", "  ")
		fmt.Println(string(data))
	default:
		for _, v := range views {
			fmt.Printf("%s: %s (budget: %d)\n", v.Name, v.Query, v.Budget)
		}
	}

	return nil
}

func runViewRender(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	var q string
	var budget int
	err = d.QueryRow("SELECT query, budget FROM views WHERE name = ?", args[0]).Scan(&q, &budget)
	if err != nil {
		return fmt.Errorf("view not found: %s", args[0])
	}

	if viewBudget > 0 {
		budget = viewBudget
	}

	result, err := viewpkg.Compose(d, viewpkg.ComposeOptions{
		Query:  q,
		Budget: budget,
	})
	if err != nil {
		return err
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

func runViewDelete(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	_, err = d.Exec("DELETE FROM views WHERE name = ?", args[0])
	if err != nil {
		return err
	}
	fmt.Printf("Deleted view: %s\n", args[0])
	return nil
}
