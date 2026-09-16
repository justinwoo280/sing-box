package main

import (
	"os"

	sewp "github.com/justinwoo280/sing-ewp"
	"github.com/sagernet/sing-box/log"
	"github.com/spf13/cobra"
)

func init() {
	commandGenerate.AddCommand(commandGenerateEWPKeyPair)
}

var commandGenerateEWPKeyPair = &cobra.Command{
	Use:   "ewp-keypair",
	Short: "Generate EWP/v2.3 server signing identity key pair",
	Args:  cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		if err := generateEWPKeyPair(); err != nil {
			log.Fatal(err)
		}
	},
}

func generateEWPKeyPair() error {
	privateKey, publicKey, err := sewp.GenerateSigningIdentity()
	if err != nil {
		return err
	}
	_, _ = os.Stdout.WriteString("PrivateKey: " + privateKey + "\n")
	_, _ = os.Stdout.WriteString("PublicKey: " + publicKey + "\n")
	return nil
}
