package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"whynoipv6/internal/core"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

// campaignImportCmd represents the command for importing domains to a campaign
var campaignImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Import list of domains to a campaign",
	Long:  "Import list of domains to a campaign from a file",
	Run: func(cmd *cobra.Command, args []string) {
		campaignService = *core.NewCampaignService(db)
		importDomainsToCampaign()
	},
}

func init() {
	campaignCmd.AddCommand(campaignImportCmd)
}

// importDomainsToCampaign imports domains from a file to the specified campaign
func importDomainsToCampaign() {
	ctx := context.Background()

	fmt.Println("Importing domains to campaign, please provide the UUID of the campaign.")
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Campaign UUID: ")
	campaignUUIDStr, _ := reader.ReadString('\n')
	campaignUUIDStr = strings.TrimSpace(campaignUUIDStr)

	// Convert string to UUID
	campaignUUID, err := uuid.Parse(campaignUUIDStr)
	if err != nil {
		fmt.Println("Error parsing UUID:", err)
		return
	}

	// Open file and loop through each line
	filePath := fmt.Sprintf("tmp/%s.txt", campaignUUIDStr)
	file, err := os.Open(filePath)
	if err != nil {
		fmt.Println("Error opening file:", err)
		return
	}
	defer file.Close()

	var importedDomainsCount = 0

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		domain := scanner.Text()
		if err := campaignService.InsertCampaignDomain(ctx, campaignUUID, domain); err != nil {
			fmt.Println("Error inserting domain:", err)
			continue
		}
		importedDomainsCount++
	}

	if err := scanner.Err(); err != nil {
		fmt.Println("Error reading file:", err)
	}

	fmt.Printf("Imported %d domains to campaign\n", importedDomainsCount)
}
