package cmd

import (
	"net/http"
	"strings"

	"github.com/kong/deck/konnect"

	"github.com/kong/deck/file"
	"github.com/kong/deck/utils"
	"github.com/pkg/errors"

	"github.com/spf13/cobra"
)

var (
	konnectDumpIncludeConsumers bool
)

// konnectDumpCmd represents the dump2 command
var konnectDumpCmd = &cobra.Command{
	Use:   "dump",
	Short: "Export configuration from Konnect",
	Long: `Dump command reads all entities present in Konnect and exports them to
a file on disk. The file can then be read using the Sync o Diff command to again
configure Konnect.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		httpClient := http.DefaultClient

		// get Konnect client
		konnectClient, err := utils.GetKonnectClient(httpClient, konnectConfig.Debug)
		if err != nil {
			return err
		}

		// authenticate with konnect
		_, err = konnectClient.Auth.Login(cmd.Context(),
			konnectConfig.Email,
			konnectConfig.Password)
		if err != nil {
			return errors.Wrap(err, "authenticating with Konnect")
		}

		// get kong control plane ID
		kongCPID, err := fetchKongControlPlaneID(cmd.Context(), konnectClient)
		if err != nil {
			return err
		}

		// initialize kong client
		kongClient, err := utils.GetKongClient(utils.KongClientConfig{
			Address:    konnect.BaseURL() + "/api/control_planes/" + kongCPID,
			HTTPClient: httpClient,
			Debug:      konnectConfig.Debug,
		})
		if err != nil {
			return err
		}

		ks, err := getKonnectState(cmd.Context(), kongClient, konnectClient, kongCPID,
			!konnectDumpIncludeConsumers)
		if err != nil {
			return err
		}

		if err := file.KonnectStateToFile(ks, file.WriteConfig{
			Filename:   dumpCmdKongStateFile,
			FileFormat: file.Format(strings.ToUpper(dumpCmdStateFormat)),
			WithID:     dumpWithID,
		}); err != nil {
			return err
		}
		return nil
	},
}

func init() {
	konnectCmd.AddCommand(konnectDumpCmd)
	konnectDumpCmd.Flags().StringVarP(&dumpCmdKongStateFile, "output-file", "o",
		"kong", "file to which to write Kong's configuration."+
			"Use '-' to write to stdout.")
	konnectDumpCmd.Flags().StringVar(&dumpCmdStateFormat, "format",
		"yaml", "output file format: json or yaml")
	konnectDumpCmd.Flags().BoolVar(&dumpWithID, "with-id",
		false, "write ID of all entities in the output")
	konnectDumpCmd.Flags().BoolVar(&konnectDumpIncludeConsumers, "include-consumers",
		false, "export consumers, associated credentials and any plugins associated "+
			"with consumers")
}
