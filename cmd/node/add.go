package node

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
	"github.com/zate/ctx/internal/db"
)

var addCmd = &cobra.Command{
	Use:   "add [content]",
	Short: "Add a new node",
	RunE:  runAdd,
}

var (
	addType  string
	addTags  []string
	addMeta  []string
	addStdin bool
)

func init() {
	addCmd.Flags().StringVar(&addType, "type", "", "Node type (required)")
	_ = addCmd.MarkFlagRequired("type")
	addCmd.Flags().StringArrayVar(&addTags, "tag", nil, "Tags (repeatable)")
	addCmd.Flags().StringArrayVar(&addMeta, "meta", nil, "Metadata key=value (repeatable)")
	addCmd.Flags().BoolVar(&addStdin, "stdin", false, "Read content from stdin")
	register(addCmd)
}

func runAdd(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	var content string
	if addStdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read stdin: %w", err)
		}
		content = strings.TrimSpace(string(data))
	} else if len(args) > 0 {
		content = strings.Join(args, " ")
	} else {
		return fmt.Errorf("content is required (provide as argument or use --stdin)")
	}

	metadata := "{}"
	if len(addMeta) > 0 {
		m := make(map[string]string)
		for _, kv := range addMeta {
			parts := strings.SplitN(kv, "=", 2)
			if len(parts) == 2 {
				m[parts[0]] = parts[1]
			}
		}
		data, _ := json.Marshal(m)
		metadata = string(data)
	}

	// Auto-add agent tag if --agent is set
	if at := cmdutil.AgentTag(cmd); at != "" {
		addTags = append(addTags, at)
	}

	node, err := d.CreateNode(db.CreateNodeInput{
		Type:     addType,
		Content:  content,
		Metadata: metadata,
		Tags:     addTags,
	})
	if err != nil {
		return err
	}

	switch cmdutil.Format(cmd) {
	case "json":
		data, _ := json.MarshalIndent(node, "", "  ")
		fmt.Println(string(data))
	default:
		fmt.Printf("Added: %s\n", node.ID)
	}

	return nil
}
