package register

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/docker/distribution/uuid"
	"github.com/spf13/cobra"
	ini "gopkg.in/ini.v1"
)

type LmpOptions struct {
	UseServer  bool
	Production bool
	// StartDaemon bool
	SotaDir     string
	DeviceGroup string
	Factory     string
	// Mlock          bool
	Vuuid      bool
	Hwid       string
	PacmanTags string
	ApiToken   string
	HsmModule  string
	HsmPin     string
	HsmSoPin   string
	UUID       string

	Name           string
	ApiTokenHeader string
	Force          bool
	Apps           string
	RestorableApps string
}

const (
	HARDWARE_ID    = "unknown"
	LMP_OS_STR     = "/etc/os-release"
	OS_FACTORY_TAG = "LMP_FACTORY_TAG"
	OS_FACTORY     = "LMP_FACTORY"
	GIT_COMMIT     = "unknown"

	// Environment Variables
	ENV_DEVICE_FACTORY = "DEVICE_FACTORY"
	ENV_PRODUCTION     = "PRODUCTION"
	ENV_OAUTH_BASE     = "OAUTH_BASE"
	ENV_DEVICE_API     = "DEVICE_API"

	// HSM definitions
	// HSM_TOKEN_STR   = "aktualizr"
	// HSM_TLS_STR     = "tls"
	// HSM_TLS_ID_STR  = "01"
	// HSM_CRT_STR     = "client"
	// HSM_CRT_ID      = 3
	// HSM_CRT_ID_STR  = "03"

	// Files
	AKLITE_LOCK = "/var/lock/aklite.lock"
	SOTA_DIR    = "/var/sota"
	SOTA_PEM    = "/client.pem"
	SOTA_SQL    = "/sql.db"

	SOTA_CLIENT = "aktualizr-lite"
)

var (
	opt LmpOptions
)

func getFactoryTagsInfo(osRelease string) (factory, fsrc, tag, tsrc string) {
	if env := os.Getenv(ENV_DEVICE_FACTORY); env != "" {
		factory = env
		fsrc = "environment"
	}
	if _, err := os.Stat(osRelease); err != nil {
		return
	}
	cfg, err := ini.Load(osRelease)
	if err != nil {
		fmt.Printf("Can't parse file %s\n", osRelease)
		return
	}
	tag = cfg.Section("").Key(OS_FACTORY_TAG).String()
	tag = strings.ReplaceAll(tag, "\"", "")
	if tag != "" {
		tsrc = osRelease
	}
	if factory != "" {
		return
	}
	factory = cfg.Section("").Key(OS_FACTORY).String()
	factory = strings.ReplaceAll(factory, "\"", "")
	if factory != "" {
		fsrc = osRelease
	}
	return
}

func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register device with Foundries.io",
		Run:   doRegister,
	}
	cmd.Flags().BoolVar(&opt.UseServer, "use-ostree-server", true, "Use OSTree Proxy server instead of the Device Gateway to pull the ostree repo.")
	cmd.Flags().BoolVar(&opt.Production, "production", false, "Mark the device as a production device.")
	// cmd.Flags().BoolVar(&opt.StartDaemon, "start-daemon", true, "Start the "+SOTA_CLIENT+" systemd service after registration.")
	cmd.Flags().StringVar(&opt.SotaDir, "sota-dir", SOTA_DIR, "The directory to install to keys and configuration to.")
	cmd.Flags().StringVar(&opt.DeviceGroup, "device-group", "", "Assign this device to a device group.")
	cmd.Flags().StringVar(&opt.Factory, "factory", "", "The factory name to subscribe to.")
	// cmd.Flags().StringVar(&opt.HsmSoPin, "hsm-so-pin", "", "The PKCS#11 security officer pin - HSM only.")
	// cmd.Flags().BoolVar(&opt.Mlock, "mlock-all", true, "Avoid paging out the process memory during execution.")
	cmd.Flags().BoolVar(&opt.Vuuid, "validate-uuid", true, "Validate the UUID before calling the remote service.")
	cmd.Flags().StringVar(&opt.Hwid, "hwid", HARDWARE_ID, "The hardware identifier for the device type.")
	cmd.Flags().StringVar(&opt.PacmanTags, "tags", "", "Configure "+SOTA_CLIENT+" to only apply updates from Targets with these tags.")
	cmd.Flags().StringVar(&opt.ApiToken, "api-token", "", "API token for authentication. If not provided, oauth2 will be used instead.")
	// cmd.Flags().StringVar(&opt.HsmModule, "hsm-module", "", "The PKCS#11 implementation (.so library) - HSM only.")
	// cmd.Flags().StringVar(&opt.HsmPin, "hsm-pin", "", "The PKCS#11 pin - HSM only.")
	cmd.Flags().StringVar(&opt.UUID, "uuid", "", "A per-device UUID. If not provided, one will be generated.")
	cmd.Flags().StringVar(&opt.Name, "name", "", "The name of the device as it should appear in the dashboard. When not specified, the device's UUID will be used instead.")
	cmd.Flags().StringVar(&opt.ApiTokenHeader, "api-token-header", "OSF-TOKEN", "The HTTP header to use for authentication.")
	cmd.Flags().BoolVar(&opt.Force, "force", false, "Force registration, removing data from previous execution.")

	return cmd
}

func validateUUID(opt *LmpOptions) error {
	_, err := uuid.Parse(opt.UUID)
	if err == nil {
		return nil
	}
	msg := fmt.Sprintf("Invalid UUID: %s", opt.UUID)
	if opt.Vuuid {
		return errors.New(msg + ", aborting")
	}
	fmt.Println(msg + ", please consider using a valid format")
	return nil
}

// func validateHSM(opt *LmpOptions) error {
// 	if opt.HsmModule == "" {
// 		if opt.HsmSoPin != "" || opt.HsmPin != "" {
// 			return errors.New("HSM incorrectly configured")
// 		}
// 		return nil
// 	}
// 	if opt.HsmSoPin == "" || opt.HsmPin == "" /* || pkcs11CheckHSM(opt) */ {
// 		return errors.New("HSM incorrectly configured")
// 	}
// 	return nil
// }

func getUUID(opt *LmpOptions) error {
	if opt.UUID != "" {
		return validateUUID(opt)
	}
	if opt.UUID == "" {
		opt.UUID = uuid.Generate().String()
		fmt.Println("UUID:", opt.UUID, "[Random]")
	}
	return validateUUID(opt)
}

func UpdateOptions(args []string, opt *LmpOptions) error {
	factory, fsrc, tags, tsrc := getFactoryTagsInfo(LMP_OS_STR)
	if opt.Factory == "" || opt.Factory == "lmp" {
		return errors.New("missing factory definition")
	}
	if opt.PacmanTags == "" {
		return errors.New("missing tag definition")
	}
	if factory != opt.Factory {
		fmt.Println("Factory read from command line")
	} else {
		fmt.Println("Factory read from", fsrc)
	}
	if tags != opt.PacmanTags {
		fmt.Println("Tags read from command line")
	} else {
		fmt.Println("Tags read from", tsrc)
	}
	log.Println("Parsing command line options 5")
	// if err := validateHSM(opt); err != nil {
	// 	return err
	// }
	if os.Getenv(ENV_PRODUCTION) != "" {
		opt.Production = true
	}
	if err := getUUID(opt); err != nil {
		return err
	}
	if opt.Name == "" {
		fmt.Println("Setting device name to UUID")
		opt.Name = opt.UUID
	}
	// if opt.Mlock {
	// 	if err := syscall.Mlockall(syscall.MCL_CURRENT | syscall.MCL_FUTURE); err != nil {
	// 		fmt.Println("Error locking memory")
	// 		return err
	// 	}
	// }
	// fmt.Println("PID memory", map[bool]string{true: "locked", false: "unlocked"}[opt.Mlock])
	return nil
}
