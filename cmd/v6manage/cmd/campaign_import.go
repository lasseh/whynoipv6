package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"whynoipv6/internal/core"
	"whynoipv6/internal/toolbox"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// campaignImportCmd represents the command for importing domains to a campaign
var campaignImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Import list of domains to a campaign",
	Long:  "Import list of domains to a campaign from a file",
	Run: func(cmd *cobra.Command, args []string) {
		campaignService = *core.NewCampaignService(db)
		toolboxService = *toolbox.NewToolboxService(cfg.GeoIPPath, cfg.Nameserver)
		importDomainsToCampaignNew()
	},
}

func init() {
	campaignCmd.AddCommand(campaignImportCmd)
}

// CampaignYAML represents a campaign in YAML format
type CampaignYAML struct {
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	UUID        string   `yaml:"uuid"`
	DomainNames []string `yaml:"domains"`
}

// importDomainsToCampaign imports domains from a file to the specified campaign.
// It reads and processes the YAML files in the campaign folder and imports the domains into the campaign.
// The function also handles updating the campaign's title and description if needed,
// as well as adding and removing domains from the campaign.
func importDomainsToCampaignNew() {
	ctx := context.Background()

	// Initialize DNS client.
	_, err := toolboxService.NewResolver()
	if err != nil {
		log.Printf("Could not initialize DNS resolver: %s\n", err)
		os.Exit(1)
	}

	log.Println("Starting Campaign Import from", cfg.CampaignPath)

	// Read all the files in the campaign folder
	err = filepath.Walk(cfg.CampaignPath, func(campaignFile string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Check if the file is a YAML file
		if !info.IsDir() && strings.EqualFold(filepath.Ext(campaignFile), ".yml") {
			// Unmarshal YAML data from the file
			yamlData, err := unmarshalYAMLFile(campaignFile)
			if err != nil {
				log.Println(err)
				return nil
			}

			// Create or update campaign
			err = createOrUpdateCampaign(ctx, campaignFile, yamlData)
			if err != nil {
				log.Println(err)
				return nil
			}

			// Update campaign title and description if needed
			// err = updateCampaignInfo(ctx, campaignFile, yamlData)
			// if err != nil {
			// 	log.Println(err)
			// 	return nil
			// }

			// Sync domains with the database
			err = syncDomainsWithDatabase(ctx, yamlData)
			if err != nil {
				log.Println(err)
				return nil
			}
		}
		return nil
	})

	if err != nil {
		fmt.Printf("Error walking through directory %s: %v\n", cfg.CampaignPath, err)
	}
}

// unmarshalYAMLFile reads and unmarshals the YAML data from the given file and returns the unmarshalled data as CampaignYAML.
func unmarshalYAMLFile(campaignFile string) (*CampaignYAML, error) {
	data, err := os.ReadFile(campaignFile)
	if err != nil {
		return nil, fmt.Errorf("error reading file %s: %v", campaignFile, err)
	}
	fmt.Println("Processing file:", filepath.Base(campaignFile))

	// Unmarshal the YAML data
	var yamlData CampaignYAML
	err = yaml.Unmarshal(data, &yamlData)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling YAML data from %s: %v", campaignFile, err)
	}

	return &yamlData, nil
}

// createOrUpdateCampaign creates a new campaign if the UUID is empty, otherwise it updates the campaign.
func createOrUpdateCampaign(ctx context.Context, campaignFile string, yamlData *CampaignYAML) error {
	// Validate Campaign title and description
	if yamlData.Title == "" || yamlData.Description == "" {
		return fmt.Errorf("error: Campaign title or description is empty in %s", campaignFile)
	}

	// Check if the UUID is empty, if so, create a new campaign and update the YAML file
	if yamlData.UUID == "" {
		newUUID, err := updateCampaignUUID(campaignFile, yamlData)
		if err != nil {
			return fmt.Errorf("error updating campaign UUID: %v", err)
		}
		yamlData.UUID = newUUID
	}

	newCampaign := core.CampaignModel{
		UUID:        uuid.MustParse(yamlData.UUID),
		Name:        yamlData.Title,
		Description: yamlData.Description,
	}

	// Update the campaign
	_, err := campaignService.CreateOrUpdateCampaign(ctx, newCampaign)
	if err != nil {
		return fmt.Errorf("error creating or updating campaign: %v", err)
	}

	// log.Println("Campaign created or updated:", campaign.UUID)
	return nil

}

// updateCampaignInfo updates the campaign's title and description if they are different from the current values.
// If the UUID is empty, it creates a new campaign and updates the YAML file with the new UUID.
// func updateCampaignInfo(ctx context.Context, campaignFile string, yamlData *CampaignYAML) error {
// 	// Validate Campaign title and description
// 	if yamlData.Title == "" || yamlData.Description == "" {
// 		return fmt.Errorf("error: Campaign title or description is empty in %s", campaignFile)
// 	}

// 	// Check if the UUID is empty, if so, create a new campaign and update the YAML file
// 	if yamlData.UUID == "" {
// 		newUUID, err := updateCampaignUUID(campaignFile, yamlData)
// 		if err != nil {
// 			return fmt.Errorf("error updating campaign UUID: %v", err)
// 		}
// 		yamlData.UUID = newUUID
// 	}

// 	// Update the campaign if the title or description is different
// 	campaign, err := campaignService.GetCampaign(ctx, uuid.MustParse(yamlData.UUID))
// 	if err != nil {
// 		return fmt.Errorf("error getting campaign with UUID %s: %v", yamlData.UUID, err)
// 	}
// 	if campaign.Name != yamlData.Title || campaign.Description != yamlData.Description {
// 		err = campaignService.UpdateCampaign(ctx, core.CampaignModel{
// 			UUID:        campaign.UUID,
// 			Name:        yamlData.Title,
// 			Description: yamlData.Description,
// 		})
// 		if err != nil {
// 			return fmt.Errorf("error updating campaign with UUID %s: %v", yamlData.UUID, err)
// 		}
// 	}

// 	return nil
// }

// updateCampaignUUID updates the campaign's UUID in the given YAML file and stores the updated content.
// It creates a new campaign using campaignService, extracts the UUID and updates the YAML file with the new UUID.
// And returns the new UUID.
func updateCampaignUUID(yamlFilePath string, campaignData *CampaignYAML) (string, error) {
	ctx := context.Background()

	// Create a new campaign using the provided YAML data.
	newCampaign, err := campaignService.CreateCampaign(ctx, campaignData.Title, campaignData.Description)
	if err != nil {
		return "", fmt.Errorf("failed to create campaign: %w", err)
	}
	fmt.Printf("Created new campaign with UUID %s\n", newCampaign.UUID)

	// Update the campaign data with the new UUID.
	campaignData.UUID = newCampaign.UUID.String()

	// Marshal the updated campaign data back into YAML format.
	updatedYAMLData, err := yaml.Marshal(campaignData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal updated campaign data: %w", err)
	}

	// Write the updated YAML data to the file.
	err = os.WriteFile(yamlFilePath, updatedYAMLData, 0644)

	if err != nil {
		return "", fmt.Errorf("failed to write updated YAML data to file: %w", err)
	}

	fmt.Println("Updated YAML file with new UUID")

	return newCampaign.UUID.String(), nil
}

// syncDomainsWithDatabase syncs domains from the yamlData with the database, inserting new domains and
// removing the ones that are not present in the yamlData anymore.
func syncDomainsWithDatabase(ctx context.Context, yamlData *CampaignYAML) error {
	campaignUUID := uuid.MustParse(yamlData.UUID)

	// Loop over domains
	for _, domain := range yamlData.DomainNames {
		// Validate domain
		if err := toolboxService.ValidateDomain(domain); err != nil {
			log.Printf("error validating domain %s: %v", domain, err.Error())
			continue
		}

		// Insert domain into campaign
		if err := campaignService.InsertCampaignDomain(ctx, campaignUUID, domain); err != nil {
			log.Printf("error inserting domain: %v", err)
			continue
		}
	}

	// Check if domains in the database are in the YAML file, if not, remove them from the campaign table
	domains, err := campaignService.ListCampaignDomain(ctx, campaignUUID, 0, 10000)
	if err != nil {
		return fmt.Errorf("error listing domains for campaign with UUID %s: %v", yamlData.UUID, err)
	}

	// Use a map to store the domains from yamlData.DomainNames for faster lookup.
	domainMap := make(map[string]bool)
	for _, domain := range yamlData.DomainNames {
		domainMap[domain] = true
	}

	// Iterate over all domains in the database.
	for _, domainRecord := range domains {
		// Check if the domain is not present in the domainMap.
		if !domainMap[domainRecord.Site] {
			fmt.Printf("Removing domain %s from campaign '%s'\n", domainRecord.Site, yamlData.Title)
			// Delete the domain from the campaign.
			err := campaignService.DeleteCampaignDomain(ctx, campaignUUID, domainRecord.Site)
			if err != nil {
				return fmt.Errorf("error deleting domain %s from campaign with UUID %s: %v", domainRecord.Site, yamlData.UUID, err)
			}
		}
	}

	return nil
}
