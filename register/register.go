package register

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/spf13/cobra"
)

func sotaCleanup(opt LmpOptions) error {
	crt := opt.SotaDir + SOTA_PEM
	sql := opt.SotaDir + SOTA_SQL

	fmt.Println("Cleaning up SOTA files")

	if fileExists(sql) {
		fmt.Println("Removing", sql)
		if err := os.Remove(sql); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: unable to remove %s: %v\n", sql, err)
			return err
		}
	}

	if fileExists(crt) {
		fmt.Println("Removing", crt)
		if err := os.Remove(crt); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: unable to remove %s: %v\n", crt, err)
			return err
		}
	}

	return nil
}

func checkSotaFiles(opt LmpOptions) error {
	crt := opt.SotaDir + SOTA_PEM
	sql := opt.SotaDir + SOTA_SQL

	crtMissing := !fileExists(crt)
	sqlMissing := !fileExists(sql)

	if crtMissing && sqlMissing {
		return nil
	}

	if !opt.Force {
		fmt.Fprintf(os.Stderr, "ERROR: Device already registered in %s\n", opt.SotaDir)
		fmt.Fprintln(os.Stderr, "Re-run with --force 1 to remove existing registration data")
		return fmt.Errorf("device already registered")
	}

	return sotaCleanup(opt)
}

func checkAkliteNotRunning() error {
	aklock := AKLITE_LOCK

	if !fileExists(aklock) {
		return nil
	}

	lock, err := os.OpenFile(aklock, os.O_RDONLY, 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: is %s running?\n", SOTA_CLIENT)
		return fmt.Errorf("unable to open aklite lock file: %w", err)
	}
	defer lock.Close()

	// Try to acquire a shared lock (non-blocking)
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_SH|syscall.LOCK_NB); err == nil {
		// Lock acquired, so aklite is not running
		syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		return nil
	} else {
		fmt.Fprintf(os.Stderr, "ERROR: %s already running\n", SOTA_CLIENT)
		return fmt.Errorf("%s already running", SOTA_CLIENT)
	}
}

// Helper function to check if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func checkDeviceStatus(opt LmpOptions) error {
	tmp := opt.SotaDir + "/.tmp"

	// Check directory is writable
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to write to %s\n", opt.SotaDir)
		return err
	}
	f.Close()
	os.Remove(tmp)

	// Aklite must not be running
	if err := checkAkliteNotRunning(); err != nil {
		return err
	}

	// Check device was not registered
	if err := checkSotaFiles(opt); err != nil {
		return err
	}

	return nil
}

// func RegisterDevice(opt LmpOptions) error {
// 	if err := checkDeviceStatus(opt); err != nil {
// 		return err
// 	}

// 	fmt.Println("Registering device with the server...")

// 	// Here you would typically call the registration logic, e.g., sending a request to the server
// 	// For now, we just simulate a successful registration
// 	fmt.Println("Device registered successfully!")

// 	// After successful registration, you might want to create necessary directories or files
// 	if err := os.MkdirAll(opt.SotaDir, 0755); err != nil {
// 		return fmt.Errorf("failed to create sota directory: %v", err)
// 	}

// 	return nil
// }

// putHSMInfo adds HSM-related overrides to the device info map if HSM is enabled.
func putHSMInfo(opt LmpOptions, dev map[string]interface{}) {
	if opt.HsmModule == "" {
		return
	}
	dev["overrides.tls.pkey_source"] = "\"pkcs11\""
	dev["overrides.tls.cert_source"] = "\"pkcs11\""
	dev["overrides.storage.tls_pkey_path"] = ""
	dev["overrides.storage.tls_clientcert_path"] = ""
	dev["overrides.import.tls_pkey_path"] = ""
	dev["overrides.import.tls_clientcert_path"] = ""
}

// getDeviceInfo fills the device info map with registration data.
func getDeviceInfo(opt LmpOptions, csr string, dev map[string]interface{}) {
	dev["use-ostree-server"] = strconv.FormatBool(opt.UseServer)
	dev["sota-config-dir"] = opt.SotaDir
	dev["hardware-id"] = opt.Hwid
	dev["name"] = opt.Name
	dev["uuid"] = opt.UUID
	dev["csr"] = csr

	putHSMInfo(opt, dev)
	// putComposeAppInfo(opt, dev) // Implement as needed

	if opt.DeviceGroup != "" {
		dev["group"] = opt.DeviceGroup
	}
	if opt.PacmanTags != "" {
		dev["overrides"] = map[string]interface{}{
			"pacman": map[string]interface{}{
				"tags": fmt.Sprintf("\"%s\"", opt.PacmanTags),
			},
		}
	}
}

// writeSafely writes content to a file atomically.
func writeSafely(name, content string) error {
	tmp := name + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("unable to open %s for writing: %w", tmp, err)
	}
	defer f.Close()
	if _, err := io.WriteString(f, content); err != nil {
		return fmt.Errorf("unable to write to %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("unable to fsync %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, name); err != nil {
		return fmt.Errorf("unable to create %s: %w", name, err)
	}
	return nil
}

// fillP11EngineInfo writes the [p11] section for HSM to the TOML buffer.
func fillP11EngineInfo(opt LmpOptions, toml *string) {
	*toml += "[p11]\n"
	*toml += fmt.Sprintf("module = \"%s\"\n", opt.HsmModule)
	*toml += fmt.Sprintf("pass = \"%s\"\n", opt.HsmPin)
	*toml += "tls_pkey_id = \"01\"\n"
	*toml += "tls_clientcert_id = \"03\"\n\n"
}

// populateSotaDir writes registration files to the SOTA directory.
func populateSotaDir(opt LmpOptions, resp map[string]interface{}, pkey string) error {
	fmt.Println("Populate sota directory.")

	if opt.HsmModule == "" {
		// Write the private key
		if err := writeSafely(filepath.Join(opt.SotaDir, "pkey.pem"), pkey); err != nil {
			return err
		}
	}

	var sotaToml string
	for name, data := range resp {
		strData := fmt.Sprintf("%v", data)
		fullName := filepath.Join(opt.SotaDir, name)
		if filepath.Base(fullName) == "sota.toml" {
			sotaToml += strData + "\n"
			if opt.HsmModule != "" {
				fillP11EngineInfo(opt, &sotaToml)
			}
			continue
		}
		if err := writeSafely(fullName, strData); err != nil {
			goto errorHandler
		}
		if filepath.Ext(fullName) != ".pem" {
			continue
		}
		// Import the certificate to PKCS#11 if HSM is enabled
		if opt.HsmModule != "" {
			crt, err := readX509FromFile(fullName)
			if err != nil {
				goto errorHandler
			}
			if err := pkcs11StoreCert(opt, crt); err != nil {
				goto errorHandler
			}
		}
	}
	if err := writeSafely(filepath.Join(opt.SotaDir, "sota.toml"), sotaToml); err != nil {
		goto errorHandler
	}
	return nil
errorHandler:
	sotaCleanup(opt)
	return errors.New("failed to populate sota directory")
}

// readX509FromFile reads an X509 certificate from a PEM file.
func readX509FromFile(filename string) (*x509.Certificate, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("failed to decode PEM")
	}
	return x509.ParseCertificate(block.Bytes)
}

func pkcs11StoreCert(opt LmpOptions, crt *x509.Certificate) error {
	return nil
}

// createCSR creates a certificate signing request, using HSM if enabled.
func createCSR(opt LmpOptions) (key string, csr string, err error) {
	if opt.HsmModule == "" {
		return OpenSSLCreateCSR(opt)
	}
	return pkcs11CreateCSR(opt)
}

func pkcs11CreateCSR(opt LmpOptions) (string, string, error) {
	return "", "", nil
}

// cleanup cleans up partial registration.
func cleanup(opt LmpOptions) {
	fmt.Println("Cleaning up partial registration before leaving")
	sotaCleanup(opt)
	pkcs11Cleanup(opt)
}

func pkcs11Cleanup(opt LmpOptions) {
}

// signalHandler handles signals for cleanup.
func signalHandler(opt LmpOptions) func(os.Signal) {
	return func(sig os.Signal) {
		fmt.Printf("\nHandling %s signal\n", sig)
		cleanup(opt)
		os.Exit(1)
	}
}

// setSignals sets up signal handlers for cleanup.
func setSignals(opt LmpOptions) func() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGSEGV)
	done := make(chan struct{})
	go func() {
		sig := <-sigs
		signalHandler(opt)(sig)
		close(done)
	}()
	return func() { signal.Stop(sigs); close(done) }
}

// mainRegisterDevice is the main registration logic.
func doRegister(cmd *cobra.Command, args []string) {
	err := RegisterDevice()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error during registration: %v\n", err)
		os.Exit(1)
	}
}

func RegisterDevice() error {
	// Parse options as needed

	err := UpdateOptions(os.Args, &opt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing options: %v\n", err)
		return err
	}

	// Check if this device can be registered
	if err := checkDeviceStatus(opt); err != nil {
		return err
	}

	headers, err := AuthGetHttpHeaders(opt)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting HTTP headers: %v\n", err)
		return err
	}

	// Check server reachability
	if err := AuthPingServer(); err != nil {
		return err
	}

	// Register signal handler for cleanup
	unsetSignals := setSignals(opt)
	defer unsetSignals()

	// Create the key pair and the certificate request
	key, csr, err := createCSR(opt)
	if err != nil {
		cleanup(opt)
		return err
	}

	// Get the device information
	info := make(map[string]interface{})
	getDeviceInfo(opt, csr, info)

	// Register the device with the factory
	fmt.Printf("Registering device %s with factory %s\n", opt.Name, opt.Factory)
	resp, err := AuthRegisterDevice(headers, info)
	if err != nil {
		cleanup(opt)
		return err
	}

	// Store the login details
	if err := populateSotaDir(opt, resp, key); err != nil {
		cleanup(opt)
		return err
	}

	fmt.Println("Device is now registered.")
	// if opt.StartDaemon {
	// 	fmt.Printf("Starting %s daemon\n", SOTA_CLIENT)
	// 	spawn("systemctl", "start", SOTA_CLIENT)
	// }
	return nil
}
