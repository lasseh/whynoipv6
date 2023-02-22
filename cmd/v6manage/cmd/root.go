package cmd

import (
	"fmt"
	"log"
	"os"
	"whynoipv6/internal/config"
	"whynoipv6/internal/postgres"

	cc "github.com/ivanpirog/coloredcobra"

	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/spf13/cobra"
)

var (
	cfg     *config.Config
	db      *pgxpool.Pool
	verbose bool
	err     error
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "v6manage",
	Short: "IPv6 Magic!",
	Long:  `Does all the magic behind https://whynoipv6.com`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	cc.Init(&cc.Config{
		RootCmd:         rootCmd,
		Headings:        cc.HiCyan + cc.Bold + cc.Underline,
		Commands:        cc.HiRed + cc.Bold,
		CmdShortDescr:   cc.HiCyan,
		Example:         cc.Italic,
		ExecName:        cc.Bold,
		Flags:           cc.Red,
		FlagsDescr:      cc.HiCyan,
		NoExtraNewlines: true,
		NoBottomNewline: true,
	})

	rootCmd.CompletionOptions.DisableDefaultCmd = true
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")

	// Read config
	cfg, err = config.Read()
	if err != nil {
		log.Fatal(err.Error())
	}

	// Connect to the database
	db, err = postgres.NewPostgreSQL(cfg.DatabaseSource)
	if err != nil {
		fmt.Println("Error connecting to database", err)
	}

}
