/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package certificates

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"time"
)

// GenerateCerts generates a CA certificate, a server certificate signed by the CA, and the server's private key.
// Ref: https://github.com/morvencao/kube-sidecar-injector/blob/master/cmd/cert.go
func GenerateCerts(dnsNames []string) (caCertPEM string, serverCertPEM string, serverKeyPEM string, err error) {
	// Generate private key for the CA
	caPrivKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return "", "", "", err
	}

	// Create the CA certificate template
	caTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Self Signed Company, INC."},
			Country:      []string{"ES"},
			Province:     []string{"Canary Islands"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0), // Valid for 10 years
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	// Self-sign the CA certificate
	caBytes, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caPrivKey.PublicKey, caPrivKey)
	if err != nil {
		return "", "", "", err
	}

	// Encode the CA certificate to PEM
	caPEMBuffer := new(bytes.Buffer)
	pem.Encode(caPEMBuffer, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: caBytes,
	})

	// Generate private key for the server
	serverPrivKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return "", "", "", err
	}

	// Create the server certificate template
	serverTemplate := &x509.Certificate{
		DNSNames:     dnsNames,
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization: []string{"Self Signed Company, INC."},
		},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(9, 0, 0), // Valid for 9 year
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	// Sign the server certificate with the CA's private key
	serverCertBytes, err := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverPrivKey.PublicKey, caPrivKey)
	if err != nil {
		return "", "", "", err
	}

	// Encode the server certificate to PEM
	serverCertPEMBuffer := new(bytes.Buffer)
	pem.Encode(serverCertPEMBuffer, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: serverCertBytes,
	})

	// Encode the server's private key to PEM
	serverKeyPEMBuffer := new(bytes.Buffer)
	pem.Encode(serverKeyPEMBuffer, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(serverPrivKey),
	})

	// Return the PEM-encoded certificates and private key as strings
	return caPEMBuffer.String(), serverCertPEMBuffer.String(), serverKeyPEMBuffer.String(), nil
}
