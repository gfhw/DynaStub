package main

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"os"
)

func main() {
	// Read the certificate file
	data, err := os.ReadFile("build/certs/tls.crt")
	if err != nil {
		fmt.Printf("Error reading file: %v\n", err)
		return
	}

	// Decode PEM
	block, _ := pem.Decode(data)
	if block == nil {
		fmt.Println("Failed to decode PEM")
		return
	}

	// Parse certificate
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		fmt.Printf("Error parsing certificate: %v\n", err)
		return
	}

	fmt.Printf("Subject: %s\n", cert.Subject)
	fmt.Printf("Issuer: %s\n", cert.Issuer)
	fmt.Printf("DNS Names: %v\n", cert.DNSNames)
	fmt.Printf("Is CA: %v\n", cert.IsCA)
	fmt.Printf("Subject CommonName: %s\n", cert.Subject.CommonName)

	// Check if it's a CA certificate
	if cert.IsCA {
		fmt.Println("\n⚠️  WARNING: This is a CA certificate, not a server certificate!")
		fmt.Println("The certificate needs to be a server certificate with DNS SANs.")
	}

	// Check DNS names
	if len(cert.DNSNames) == 0 {
		fmt.Println("\n⚠️  WARNING: No DNS SANs found in the certificate!")
		fmt.Println("The certificate needs DNS SANs for the webhook service.")
	}

	// Print base64 for reference
	fmt.Printf("\nBase64 encoded:\n%s\n", base64.StdEncoding.EncodeToString(data))
}
