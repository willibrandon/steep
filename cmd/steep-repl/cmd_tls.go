package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/willibrandon/steep/internal/repl/grpc/certs"
)

// newInitTLSCmd creates the init-tls subcommand for generating mTLS certificates.
func newInitTLSCmd() *cobra.Command {
	var (
		outputDir string
		nodeName  string
		hosts     []string
		validDays int
	)

	cmd := &cobra.Command{
		Use:   "init-tls",
		Short: "Generate mTLS certificates for secure node communication",
		Long: `Generate a CA and server/client certificates for mTLS.

This creates all certificates needed for secure gRPC communication:
  - ca.crt, ca.key       CA certificate and key
  - server.crt, server.key   Server certificate for this node
  - client.crt, client.key   Client certificate for connecting to other nodes

Example:
  steep-repl init-tls
  steep-repl init-tls --hosts localhost,192.168.1.10,node1.example.com
  steep-repl init-tls --output ~/.config/steep/certs --days 365`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Default output directory
			if outputDir == "" {
				home, err := os.UserHomeDir()
				if err != nil {
					return fmt.Errorf("get home dir: %w", err)
				}
				outputDir = filepath.Join(home, ".config", "steep", "certs")
			}

			// Default hosts
			if len(hosts) == 0 {
				hosts = []string{"localhost", "127.0.0.1"}
				// Try to add hostname
				if h, err := os.Hostname(); err == nil {
					hosts = append(hosts, h)
				}
			}

			cfg := certs.Config{
				OutputDir: outputDir,
				NodeName:  nodeName,
				Hosts:     hosts,
				ValidDays: validDays,
			}

			fmt.Printf("Generating mTLS certificates in %s...\n", outputDir)
			result, err := certs.Generate(cfg)
			if err != nil {
				return err
			}

			fmt.Println("\nGenerated files:")
			fmt.Printf("  CA:     %s, %s\n", result.CACert, result.CAKey)
			fmt.Printf("  Server: %s, %s\n", result.ServerCert, result.ServerKey)
			fmt.Printf("  Client: %s, %s\n", result.ClientCert, result.ClientKey)

			fmt.Println("\nAdd this to your config.yaml:")
			fmt.Println("─────────────────────────────────")
			fmt.Print(certs.ConfigSnippet(result))
			fmt.Println("─────────────────────────────────")

			fmt.Println("\nFor multi-node setup:")
			fmt.Println("  1. Copy ca.crt to all nodes")
			fmt.Println("  2. Run 'steep-repl init-tls' on each node with appropriate --hosts")
			fmt.Println("  3. Each node uses its own server.crt/key and the shared ca.crt")

			return nil
		},
	}

	cmd.Flags().StringVarP(&outputDir, "output", "o", "", "output directory (default ~/.config/steep/certs)")
	cmd.Flags().StringVarP(&nodeName, "name", "n", "", "node name for certificate CN (default steep-repl)")
	cmd.Flags().StringSliceVar(&hosts, "hosts", nil, "hostnames and IPs for certificate SANs (default localhost,127.0.0.1)")
	cmd.Flags().IntVar(&validDays, "days", 365, "certificate validity in days")

	return cmd
}
