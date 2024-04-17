package cmd

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"whynoipv6/internal/core"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// disableCmd represents the command for disabling domains
var disableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disables service domains",
	Long:  "Disables service domains from a file",
	Run: func(cmd *cobra.Command, args []string) {
		domainService = *core.NewDomainService(db)
		disableDomains()
	},
}

func init() {
	rootCmd.AddCommand(disableCmd)
}

// ServiceDomainYAML represents a service domain in YAML format
type ServiceDomainYAML struct {
	Domains []string `yaml:"domains"`
}

// disableDomains disables domains from a file.
// It reads and processes the YAML files in the campaign folder and disables the domains.
func disableDomains() {
	ctx := context.Background()
	log.Println("Banning service domains")

	// Read and unmarshal the YAML file
	yamlData, err := unmarshalDomainFile("service_domains.yml")
	if err != nil {
		log.Println(err)
		return
	}

	// Disable the domains
	for _, domain := range yamlData.Domains {
		log.Println("Disabling domain:", domain)
		domainService.DisableDomain(ctx, domain)
	}
}

// unmarshalYAMLFile reads and unmarshals the YAML data from the given file and returns the unmarshalled data as CampaignYAML.
func unmarshalDomainFile(serviceFile string) (*ServiceDomainYAML, error) {
	data, err := os.ReadFile(serviceFile)
	if err != nil {
		return nil, fmt.Errorf("error reading file %s: %v", serviceFile, err)
	}
	log.Println("Processing file:", filepath.Base(serviceFile))

	// Unmarshal the YAML data
	var yamlData ServiceDomainYAML
	err = yaml.Unmarshal(data, &yamlData)
	if err != nil {
		return nil, fmt.Errorf("error unmarshalling YAML data from %s: %v", serviceFile, err)
	}

	return &yamlData, nil
}
