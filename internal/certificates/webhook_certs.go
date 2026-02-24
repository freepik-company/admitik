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
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	apimacherrors "k8s.io/apimachinery/pkg/api/errors"
	apimachv1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/freepik-company/admitik/internal/globals"
)

type WebhookCertPaths struct {
	CAPath         string
	CertPath       string
	PrivateKeyPath string
}

type WebhookCertOptions struct {
	CAPath              string
	CertPath            string
	PrivateKeyPath      string
	SecretName          string
	AutogenerateCerts   bool
	ClientHostname      string
	Namespace           string
}

func EnsureWebhookCerts(ctx context.Context, opts WebhookCertOptions) (*WebhookCertPaths, error) {
	if (opts.CAPath != "" || opts.CertPath != "" || opts.PrivateKeyPath != "") && opts.SecretName != "" {
		return nil, fmt.Errorf("getting certificates from files and from Secret objects are mutually exclusive")
	}

	if opts.SecretName == "" {
		return &WebhookCertPaths{
			CAPath:         opts.CAPath,
			CertPath:       opts.CertPath,
			PrivateKeyPath: opts.PrivateKeyPath,
		}, nil
	}

	var ca, cert, privKey string

	var lastErr error
	succeeded := false
	for try := 0; try < 3; try++ {
		secretObj, err := globals.Application.KubeRawCoreClient.CoreV1().Secrets(opts.Namespace).
			Get(ctx, opts.SecretName, apimachv1.GetOptions{})

		if err != nil {
			if !apimacherrors.IsNotFound(err) {
				lastErr = fmt.Errorf("unable to get secret with certificates: %w", err)
				continue
			}

			if !opts.AutogenerateCerts {
				lastErr = fmt.Errorf("unable to get secret and autogeneration is disabled: %w", err)
				continue
			}

			dnsNames := []string{"localhost", opts.ClientHostname}
			if strings.HasSuffix(opts.ClientHostname, ".cluster.local") {
				dnsNames = append(dnsNames, strings.TrimSuffix(opts.ClientHostname, ".cluster.local"))
			}
			if strings.HasSuffix(opts.ClientHostname, ".svc") {
				dnsNames = append(dnsNames, opts.ClientHostname+".cluster.local")
			}
			ca, cert, privKey, err = GenerateCerts(dnsNames)
			if err != nil {
				lastErr = fmt.Errorf("unable to generate self-signed certificates: %w", err)
				continue
			}

			secretObj.StringData = map[string]string{
				"ca.crt":  ca,
				"tls.crt": cert,
				"tls.key": privKey,
			}
			secretObj.Name = opts.SecretName
			_, err = globals.Application.KubeRawCoreClient.CoreV1().Secrets(opts.Namespace).
				Create(ctx, secretObj, apimachv1.CreateOptions{})
			if err != nil {
				lastErr = fmt.Errorf("unable to create secret with self-signed certificates: %w", err)
				continue
			}

			succeeded = true
			break
		}

		caBytes, ok := secretObj.Data["ca.crt"]
		if !ok {
			return nil, fmt.Errorf("unable to get ca.crt from defined secret")
		}
		ca = string(caBytes)

		certBytes, ok := secretObj.Data["tls.crt"]
		if !ok {
			return nil, fmt.Errorf("unable to get tls.crt from defined secret")
		}
		cert = string(certBytes)

		privKeyBytes, ok := secretObj.Data["tls.key"]
		if !ok {
			return nil, fmt.Errorf("unable to get tls.key from defined secret")
		}
		privKey = string(privKeyBytes)

		succeeded = true
		break
	}

	if !succeeded {
		return nil, fmt.Errorf("unable to get self-signed certificates after retries: %w", lastErr)
	}

	tempDir := os.TempDir()
	paths := &WebhookCertPaths{
		CAPath:         filepath.Join(tempDir, "ca.crt"),
		CertPath:       filepath.Join(tempDir, "tls.crt"),
		PrivateKeyPath: filepath.Join(tempDir, "tls.key"),
	}

	if err := os.WriteFile(paths.CAPath, []byte(ca), 0600); err != nil {
		return nil, fmt.Errorf("unable to write CA file: %w", err)
	}
	if err := os.WriteFile(paths.CertPath, []byte(cert), 0600); err != nil {
		return nil, fmt.Errorf("unable to write certificate file: %w", err)
	}
	if err := os.WriteFile(paths.PrivateKeyPath, []byte(privKey), 0600); err != nil {
		return nil, fmt.Errorf("unable to write private key file: %w", err)
	}

	return paths, nil
}
