package cmd

import (
	"context"
	"fmt"
	"whynoipv6/internal/core"

	"github.com/alexeyco/simpletable"

	"github.com/spf13/cobra"
)

// listCmd represents the list command
var listCmd = &cobra.Command{
	Use:   "list",
	Short: "Lists all campaigns",
	Long:  `Lists all campaigns`,
	Run: func(cmd *cobra.Command, args []string) {
		campaignService = *core.NewCampaignService(db)
		listCampain()
	},
}

func init() {
	campaignCmd.AddCommand(listCmd)
}

func listCampain() {
	ctx := context.Background()
	table := simpletable.New()

	campaigns, err := campaignService.ListCampaign(ctx)
	if err != nil {
		fmt.Println(err)
	}

	table.Header = &simpletable.Header{
		Cells: []*simpletable.Cell{
			{Align: simpletable.AlignCenter, Text: "UUID"},
			{Align: simpletable.AlignCenter, Text: "Name"},
			{Align: simpletable.AlignCenter, Text: "Domain Count"},
		},
	}

	var total = 0

	for _, row := range campaigns {
		r := []*simpletable.Cell{
			{Text: row.UUID.String()},
			{Text: row.Name},
			{Align: simpletable.AlignRight, Text: fmt.Sprintf("%d", row.Count)},
		}

		table.Body.Cells = append(table.Body.Cells, r)

		total += int(row.Count)
	}
	table.Footer = &simpletable.Footer{
		Cells: []*simpletable.Cell{
			{},
			{Align: simpletable.AlignRight, Text: "Total"},
			{Align: simpletable.AlignRight, Text: fmt.Sprintf("%d", total)},
		},
	}

	table.SetStyle(simpletable.StyleDefault)
	fmt.Println(table.String())

}
