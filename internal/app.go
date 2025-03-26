package internal

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/foundriesio/fioconfig/sotatoml"
)

var NotModifiedError = errors.New("Config unchanged on server")

// Functions to be called when the daemon is initialized
var initFunctions = map[string]func(app *App, client *http.Client, crypto CryptoHandler) error{}

type CryptoHandler interface {
	Decrypt(value string) ([]byte, error)
	Close()
}

type App struct {
	StorageDir      string
	EncryptedConfig string
	SecretsDir      string

	configUrl      string
	configPaths    []string
	unsafeHandlers bool
	sota           *sotatoml.AppConfig

	exitFunc func(int)
}

func tomlAssertVal(cfg *sotatoml.AppConfig, key string, allowed []string) string {
	val := cfg.GetOrDie(key)
	for _, v := range allowed {
		if val == v {
			return val
		}
	}
	fmt.Println("ERROR: Invalid value", val, "in sota.toml for", key)
	return val
}

// TODO: Re-enable code for hsm / pkcs11 support
// // sota.toml has slot id's as "01". We need to turn that into []byte{1}
// func idToBytes(id string) []byte {
// 	bytes := []byte(id)
// 	start := -1
// 	for idx, char := range bytes {
// 		bytes[idx] = char - byte('0')
// 		if bytes[idx] != 0 && start == -1 {
// 			start = idx
// 		}
// 	}
// 	//strip off leading 0's
// 	return bytes[start:]
// }

func createClientPkcs11(sota *sotatoml.AppConfig) (*http.Client, CryptoHandler) {
	return nil, nil
	// TODO: Re-enable code for hsm / pkcs11 support
	// module := sota.GetOrDie("p11.module")
	// pin := sota.GetOrDie("p11.pass")
	// pkeyId := sota.GetOrDie("p11.tls_pkey_id")
	// certId := sota.GetOrDie("p11.tls_clientcert_id")
	// caFile := sota.GetOrDie("import.tls_cacert_path")

	// cfg := crypto11.Config{
	// 	Path:        module,
	// 	TokenLabel:  sota.GetDefault("p11.label", "aktualizr"),
	// 	Pin:         pin,
	// 	MaxSessions: 2,
	// }

	// ctx, err := crypto11.Configure(&cfg)
	// if err != nil {
	// 	log.Fatal(err)
	// }

	// privKey, err := ctx.FindKeyPair(idToBytes(pkeyId), nil)
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// cert, err := ctx.FindCertificate(idToBytes(certId), nil, nil)
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// if cert == nil || privKey == nil {
	// 	log.Fatal("Unable to load pkcs11 client cert and/or private key")
	// }

	// caCert, err := os.ReadFile(caFile)
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// caCertPool := x509.NewCertPool()
	// caCertPool.AppendCertsFromPEM(caCert)

	// tlsConfig := &tls.Config{
	// 	Certificates: []tls.Certificate{
	// 		{
	// 			Certificate: [][]byte{cert.Raw},
	// 			PrivateKey:  privKey,
	// 		},
	// 	},
	// 	RootCAs: caCertPool,
	// }
	// transport := &http.Transport{TLSClientConfig: tlsConfig}
	// client := &http.Client{Timeout: time.Second * 30, Transport: transport}
	// return client, nil
}

func createClientLocal(sota *sotatoml.AppConfig) (*http.Client, CryptoHandler) {
	certFile := sota.GetOrDie("import.tls_clientcert_path")
	keyFile := sota.GetOrDie("import.tls_pkey_path")
	caFile := sota.GetOrDie("import.tls_cacert_path")

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		log.Fatal(err)
	}

	caCert, err := os.ReadFile(caFile)
	if err != nil {
		log.Fatal(err)
	}
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(caCert)

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caCertPool,
	}
	transport := &http.Transport{TLSClientConfig: tlsConfig}
	client := &http.Client{Timeout: time.Second * 30, Transport: transport}

	return client, nil
	// if handler := NewEciesLocalHandler(cert.PrivateKey); handler != nil {
	// 	return client, handler
	// }
	// panic("Unsupported private key")
}

func createClient(sota *sotatoml.AppConfig) (*http.Client, CryptoHandler) {
	_ = tomlAssertVal(sota, "tls.ca_source", []string{"file"})
	source := tomlAssertVal(sota, "tls.pkey_source", []string{"file", "pkcs11"})
	_ = tomlAssertVal(sota, "tls.cert_source", []string{source})
	if source == "file" {
		return createClientLocal(sota)
	}
	return createClientPkcs11(sota)
}

func NewApp(configPaths []string, secrets_dir string, unsafeHandlers, testing bool) (*App, error) {
	if len(configPaths) == 0 {
		configPaths = sotatoml.DEF_CONFIG_ORDER
	}
	sota, err := sotatoml.NewAppConfig(configPaths)
	if err != nil {
		fmt.Println("ERROR - unable to decode sota.toml:", err)
		os.Exit(1)
	}
	// Assert we have a sane configuration
	createClient(sota)
	// crypto.Close()

	url := os.Getenv("CONFIG_URL")
	if len(url) == 0 {
		url = sota.GetDefault("tls.server", "https://ota-lite.foundries.io:8443")
		url += "/config"
	}

	storagePath := sota.GetOrDie("storage.path")

	app := App{
		StorageDir:      storagePath,
		EncryptedConfig: filepath.Join(storagePath, "config.encrypted"),
		SecretsDir:      secrets_dir,
		configUrl:       url,
		configPaths:     configPaths,
		sota:            sota,
		unsafeHandlers:  unsafeHandlers,
		exitFunc:        os.Exit,
	}

	return &app, nil
}

func (a *App) CallInitFunctions() {
	client, crypto := createClient(a.sota)
	defer crypto.Close()
	a.callInitFunctions(client, crypto)
}

func (a *App) callInitFunctions(client *http.Client, crypto CryptoHandler) {
	for name, cb := range initFunctions {
		log.Printf("Running %s initialization", name)
		if err := cb(a, client, crypto); err != nil {
			log.Println("ERROR:", err)
		} else {
			delete(initFunctions, name)
		}
	}
}
