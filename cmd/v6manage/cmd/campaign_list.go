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
		listCampaign()
	},
}

func init() {
	campaignCmd.AddCommand(listCmd)
}

// listCampaign displays a table of campaigns with their UUID, name, and domain count.
func listCampaign() {
	ctx := context.Background()

	// Create a new table with simpletable package.
	table := simpletable.New()

	// Fetch campaigns using the campaignService.
	campaigns, err := campaignService.ListCampaign(ctx)
	if err != nil {
		fmt.Printf("Error fetching campaigns: %v\n", err)
		return
	}

	// Set table header.
	table.Header = &simpletable.Header{
		Cells: []*simpletable.Cell{
			{Align: simpletable.AlignCenter, Text: "UUID"},
			{Align: simpletable.AlignCenter, Text: "Name"},
			{Align: simpletable.AlignCenter, Text: "Domain Count"},
		},
	}

	// Initialize a variable to store the total domain count.
	var totalDomainCount = 0

	// Iterate through the campaigns and add rows to the table.
	for _, campaign := range campaigns {
		row := []*simpletable.Cell{
			{Text: campaign.UUID.String()},
			{Text: campaign.Name},
			{Align: simpletable.AlignRight, Text: fmt.Sprintf("%d", campaign.Count)},
		}

		table.Body.Cells = append(table.Body.Cells, row)

		totalDomainCount += int(campaign.Count)
	}

	// Set table footer with the total domain count.
	table.Footer = &simpletable.Footer{
		Cells: []*simpletable.Cell{
			{},
			{Align: simpletable.AlignRight, Text: "Total"},
			{Align: simpletable.AlignRight, Text: fmt.Sprintf("%d", totalDomainCount)},
		},
	}

	// Set table style and print it.
	table.SetStyle(simpletable.StyleDefault)
	fmt.Println(table.String())
}
