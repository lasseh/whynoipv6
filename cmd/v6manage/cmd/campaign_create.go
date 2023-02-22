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

func createCampaign() {
	ctx := context.Background()

	// Read input from stdin
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Campaign Name: ")
	name, _ := reader.ReadString('\n')
	fmt.Print("Campaign Description: ")
	description, _ := reader.ReadString('\n')

	// Create campaign
	n, err := campaignService.CreateCampaign(ctx, strings.TrimSpace(name), strings.TrimSpace(description))
	if err != nil {
		fmt.Println(err)
	}

	fmt.Println("")
	fmt.Println("Campaign created successfully.")
	fmt.Println("UUID: ", n.UUID)
	fmt.Println("Name: ", n.Name)
}
