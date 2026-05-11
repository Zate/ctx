package node

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
	"github.com/zate/ctx/internal/query"
)

var recallInject bool

var recallCmd = &cobra.Command{
	Use:   "recall <query>",
	Short: "Run a memory query and optionally inject results into next prompt",
	Args:  cobra.ExactArgs(1),
	RunE:  runRecall,
}

func init() {
	recallCmd.Flags().BoolVar(&recallInject, "inject", false, "Store results in pending for injection at next prompt-submit")
	register(recallCmd)
}

func runRecall(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	queryStr := args[0]
	nodes, err := query.ExecuteQuery(d, queryStr, false)
	if err != nil {
		return fmt.Errorf("recall query failed: %w", err)
	}

	// Filter by agent partition
	nodes = cmdutil.FilterNodesByAgent(cmd, nodes)

	// Log access
	cmdutil.LogAccessNodes(cmd, d, nodes, "explicit_query", "recall:"+queryStr)

	// Optionally store in pending for prompt-submit injection
	if recallInject {
		if err := d.SetPending("recall_query", queryStr); err != nil {
			return fmt.Errorf("failed to set pending recall: %w", err)
		}
	}

	if cmdutil.AgentOut(cmd) {
		fmt.Fprintf(os.Stdout, "ok recall count=%d query=%s injected=%v\n",
			len(nodes), cmdutil.AOFQuote(queryStr), recallInject)
		if len(nodes) > 0 {
			fmt.Fprintln(os.Stdout, "@ id type tags summary")
			for _, n := range nodes {
				tags := "_"
				if len(n.Tags) > 0 {
					tags = strings.Join(n.Tags, "|")
				}
				content := n.Content
				if len(content) > 120 {
					content = content[:120] + "…"
				}
				fmt.Fprintf(os.Stdout, "- %s %s %s %s\n", n.ID, n.Type, tags, cmdutil.AOFQuote(content))
			}
		}
		return nil
	}

	switch cmdutil.Format(cmd) {
	case "json":
		fmt.Printf("{\"query\":%q,\"count\":%d,\"injected\":%v,\"nodes\":", queryStr, len(nodes), recallInject)
		for i, n := range nodes {
			if i == 0 {
				fmt.Print("[")
			}
			fmt.Printf("{\"id\":%q,\"type\":%q,\"content\":%q}", n.ID, n.Type, n.Content)
			if i < len(nodes)-1 {
				fmt.Print(",")
			}
		}
		if len(nodes) == 0 {
			fmt.Print("[")
		}
		fmt.Println("]}")
	default:
		if len(nodes) == 0 {
			fmt.Println("No results found.")
			return nil
		}
		fmt.Printf("Recall: %d result(s) for %q\n", len(nodes), queryStr)
		if recallInject {
			fmt.Println("(injected into next prompt)")
		}
		for _, n := range nodes {
			preview := n.Content
			if len(preview) > 80 {
				preview = preview[:80] + "..."
			}
			tags := ""
			if len(n.Tags) > 0 {
				tags = " [" + strings.Join(n.Tags, ", ") + "]"
			}
			fmt.Printf("[%s] %s: %s%s\n", n.ID, n.Type, preview, tags)
		}
	}

	return nil
}
