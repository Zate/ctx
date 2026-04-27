package cmd

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/internal/db"
)

var (
	listType  string
	listTags  []string
	listSince string
	listLimit int
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List nodes",
	RunE:  runList,
}

func init() {
	listCmd.Flags().StringVar(&listType, "type", "", "Filter by type")
	listCmd.Flags().StringArrayVar(&listTags, "tag", nil, "Filter by tag (repeatable, AND logic)")
	listCmd.Flags().StringVar(&listSince, "since", "", "Filter by creation time (e.g. 1h, 24h, 7d)")
	listCmd.Flags().IntVar(&listLimit, "limit", 0, "Limit results")
	rootCmd.AddCommand(listCmd)
}

func runList(cmd *cobra.Command, args []string) error {
	d, err := openDB()
	if err != nil {
		return err
	}
	defer d.Close()

	opts := db.ListOptions{
		Type:  listType,
		Tags:  listTags,
		Limit: listLimit,
	}

	if listSince != "" {
		since, err := parseDuration(listSince)
		if err != nil {
			return fmt.Errorf("invalid since value: %w", err)
		}
		t := time.Now().Add(-since)
		opts.Since = &t
	}

	nodes, err := d.ListMemoryNodes(opts)
	if err != nil {
		return err
	}

	// Filter by agent partition
	nodes = filterNodesByAgent(nodes)

	logAccessNodes(d, nodes, "explicit_query", "list")

	switch format {
	case "json":
		data, _ := json.MarshalIndent(nodes, "", "  ")
		fmt.Println(string(data))
	default:
		if len(nodes) == 0 {
			fmt.Println("No nodes found.")
			return nil
		}
		for _, n := range nodes {
			preview := n.Content
			if len(preview) > 80 {
				preview = preview[:80] + "..."
			}
			tags := ""
			if len(n.Tags) > 0 {
				tags = " [" + joinStrings(n.Tags, ", ") + "]"
			}
			fmt.Printf("[%s] %s: %s%s\n", n.ID, n.Type, preview, tags)
		}
	}

	return nil
}

func parseDuration(s string) (time.Duration, error) {
	if len(s) == 0 {
		return 0, fmt.Errorf("empty duration")
	}

	last := s[len(s)-1]
	numStr := s[:len(s)-1]

	switch last {
	case 'h':
		return time.ParseDuration(numStr + "h")
	case 'd':
		hours, err := time.ParseDuration(numStr + "h")
		if err != nil {
			return 0, err
		}
		return hours * 24, nil
	case 'w':
		hours, err := time.ParseDuration(numStr + "h")
		if err != nil {
			return 0, err
		}
		return hours * 24 * 7, nil
	default:
		return time.ParseDuration(s)
	}
}
