package tag

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/zate/ctx/cmd/internal/cmdutil"
)

var tagsPrefix string

var tagsCmd = &cobra.Command{
	Use:   "tags",
	Short: "List all tags",
	RunE:  runTags,
}

func init() {
	tagsCmd.Flags().StringVar(&tagsPrefix, "prefix", "", "Filter by prefix")
	register(tagsCmd)
}

func runTags(cmd *cobra.Command, args []string) error {
	d, err := cmdutil.OpenDB(cmd)
	if err != nil {
		return err
	}
	defer d.Close()

	var tags []string
	if tagsPrefix != "" {
		tags, err = d.ListTagsByPrefix(tagsPrefix)
	} else {
		tags, err = d.ListAllTags()
	}
	if err != nil {
		return err
	}

	switch cmdutil.Format(cmd) {
	case "json":
		data, _ := json.MarshalIndent(tags, "", "  ")
		fmt.Println(string(data))
	default:
		if len(tags) == 0 {
			fmt.Println("No tags found.")
			return nil
		}
		for _, t := range tags {
			fmt.Println(t)
		}
	}

	return nil
}
