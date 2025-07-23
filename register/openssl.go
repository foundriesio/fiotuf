package register

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
)

// Generate a CSR using external keys (not created with Go)
func OpenSSLGenCSR(opt LmpOptions, pub, priv interface{}) (string, error) {
	subj := pkix.Name{
		CommonName:   opt.UUID,
		Organization: []string{opt.Factory},
	}
	if opt.Production {
		subj.ExtraNames = append(subj.ExtraNames, pkix.AttributeTypeAndValue{
			Type:  []int{2, 5, 4, 15}, // businessCategory OID
			Value: "production",
		})
	}

	template := x509.CertificateRequest{
		Subject:            subj,
		SignatureAlgorithm: x509.ECDSAWithSHA256,
	}

	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &template, priv)
	if err != nil {
		return "", err
	}

	csrPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})
	return string(csrPEM), nil
}

// Create a new EC key and CSR
func OpenSSLCreateCSR(opt LmpOptions) (key string, csr string, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}

	subj := pkix.Name{
		CommonName:         opt.UUID,
		OrganizationalUnit: []string{opt.Factory},
	}
	if opt.Production {
		subj.ExtraNames = append(subj.ExtraNames, pkix.AttributeTypeAndValue{
			Type:  []int{2, 5, 4, 15}, // businessCategory OID
			Value: "production",
		})
	}

	template := x509.CertificateRequest{
		Subject:            subj,
		SignatureAlgorithm: x509.ECDSAWithSHA256,
	}

	fmt.Println("Generating CSR for Factory:", opt.Factory)
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, &template, priv)
	if err != nil {
		return "", "", err
	}
	csrPEMBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER})

	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return "", "", err
	}
	privPEMBytes := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes})

	return string(privPEMBytes), string(csrPEMBytes), nil
}
