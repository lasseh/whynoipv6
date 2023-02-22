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

// importsCmd represents the imports command
var campaignImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Import list of domains to a campaign",
	Long:  `Import list of domains to a campaign`,
	Run: func(cmd *cobra.Command, args []string) {
		campaignService = *core.NewCampaignService(db)
		importCampaign()
	},
}

func init() {
	campaignCmd.AddCommand(campaignImportCmd)
}

func importCampaign() {
	ctx := context.Background()

	fmt.Println("Importing domains to campaign, please provide the uuid of the campaign.")
	reader := bufio.NewReader(os.Stdin)
	fmt.Print("Campaign UUID: ")
	campaignid, _ := reader.ReadString('\n')
	campaignid = strings.TrimSpace(campaignid)
	fmt.Println(campaignid)

	// Convert string to UUID
	u, err := uuid.Parse(campaignid)
	if err != nil {
		fmt.Println(err)
	}

	// Open file and loop through each line
	file, err := os.Open(fmt.Sprintf("tmp/%s.txt", campaignid))
	if err != nil {
		fmt.Println(err)
	}
	defer file.Close()

	var count = 0

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		domain := scanner.Text()
		if err := campaignService.InsertCampaignDomain(ctx, u, domain); err != nil {
			fmt.Println(err)
		}
		count++
	}

	fmt.Printf("Imported %d domains to campaign\n", count)

}
