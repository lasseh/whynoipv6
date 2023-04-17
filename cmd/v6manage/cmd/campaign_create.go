package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"whynoipv6/internal/core"

	"github.com/spf13/cobra"
)

// createCmd represents the create command
var createCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new campaign",
	Long:  `Create a new campaign`,
	Run: func(cmd *cobra.Command, args []string) {
		campaignService = *core.NewCampaignService(db)
		createCampaign()
	},
}

func init() {
	campaignCmd.AddCommand(createCmd)
}

// createCampaign reads campaign name and description from stdin and creates a new campaign.
func createCampaign() {
	ctx := context.Background()

	// Create a new buffered reader for reading input from stdin.
	reader := bufio.NewReader(os.Stdin)

	// Read campaign name from stdin.
	fmt.Print("Campaign Name: ")
	name, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("Error reading campaign name: %v\n", err)
		return
	}

	// Read campaign description from stdin.
	fmt.Print("Campaign Description: ")
	description, err := reader.ReadString('\n')
	if err != nil {
		fmt.Printf("Error reading campaign description: %v\n", err)
		return
	}

	// Create a new campaign using the campaignService.
	newCampaign, err := campaignService.CreateCampaign(ctx, strings.TrimSpace(name), strings.TrimSpace(description))
	if err != nil {
		fmt.Printf("Error creating campaign: %v\n", err)
		return
	}

	// Display the created campaign's details.
	fmt.Println("")
	fmt.Println("Campaign created successfully.")
	fmt.Println("UUID: ", newCampaign.UUID)
	fmt.Println("Name: ", newCampaign.Name)
}
