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
	Short: "Generate EWP/v2.1 server static identity key pair",
	Long: `Generate a long-term X25519 key pair for an EWP/v2.1 server.

The PrivateKey goes into the server inbound's "server_static_private_key"
field; the PublicKey goes into every client outbound's
"server_static_public_key" field. EWP/v2.1 binds the handshake KDF to
this identity, closing audit findings S1, S2, and H2.

Both values are base64-encoded 32-byte X25519 scalars.`,
	Args: cobra.NoArgs,
	Run: func(cmd *cobra.Command, args []string) {
		err := generateEWPKeyPair()
		if err != nil {
			log.Fatal(err)
		}
	},
}

func generateEWPKeyPair() error {
	privB64, pubB64, err := sewp.GenerateServerStaticKeypair()
	if err != nil {
		return err
	}
	os.Stdout.WriteString("PrivateKey: " + privB64 + "\n")
	os.Stdout.WriteString("PublicKey: " + pubB64 + "\n")
	return nil
}
