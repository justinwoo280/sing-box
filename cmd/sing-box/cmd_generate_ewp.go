package main

import (
	"os"

	"github.com/sagernet/sing-box/log"

	sewp "github.com/justinwoo280/sing-ewp"
	"github.com/spf13/cobra"
)

func init() {
	commandGenerate.AddCommand(commandGenerateEWPKeyPair)
}

var commandGenerateEWPKeyPair = &cobra.Command{
	Use:   "ewp-keypair",
	Short: "Generate EWP/v2.3 server signing identity key pair",
	Long: `Generate a long-term Ed25519 signing identity for an EWP/v2.3 server.

The PrivateKey goes into the server inbound's "signing_private_key"
field; the PublicKey goes into every client outbound's
"server_public_key" field. EWP/v2.3 pins this identity and signs every
short-term outer key and ServerHello transcript with it.

Both values are base64-encoded (64-byte private key, 32-byte public key).`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		err := generateEWPKeyPair()
		if err != nil {
			log.Fatal(err)
		}
	},
}

func generateEWPKeyPair() error {
	privB64, pubB64, err := sewp.GenerateSigningIdentity()
	if err != nil {
		return err
	}
	os.Stdout.WriteString("PrivateKey: " + privB64 + "\n")
	os.Stdout.WriteString("PublicKey: " + pubB64 + "\n")
	return nil
}
