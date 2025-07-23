package updateclient

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/foundriesio/fiotuf/events"
	"github.com/foundriesio/fiotuf/targets"
	"github.com/foundriesio/fiotuf/tuf"
	"github.com/spf13/cobra"
	"github.com/theupdateframework/go-tuf/v2/metadata"

	"github.com/foundriesio/composeapp/pkg/compose"
	v1 "github.com/foundriesio/composeapp/pkg/compose/v1"
	"github.com/foundriesio/composeapp/pkg/update"
	"github.com/foundriesio/fioconfig/sotatoml"
	"github.com/foundriesio/fioconfig/transport"
	_ "modernc.org/sqlite"
)

// GetAppsList returns a list of apps from the target
// Example custom content:
// "custom": {
//           "arch": "x86_64",
//           "containers-sha": "884983bf4e01031e2610b1a8511513e991e5dad7",
//           "createdAt": "2024-11-14T18:28:32Z",
//           "docker_compose_apps": {
//             "shellhttpd_base_10000": {
//               "uri": "hub.foundries.io/detsch-temp-test-2/shellhttpd_base_10000@sha256:7e4d461ddfbfd846a24c662395298159de1664b24a94cb5bd58965360d1bc34d"
//             },
//             "shellhttpd_base_20000": {
//               "uri": "hub.foundries.io/detsch-temp-test-2/shellhttpd_base_20000@sha256:692487f3943b90bac059585731f1dff91473bbbd106d940a894a1682e3cbc3cd"
//             },
//             "shellhttpd_base_30000": {
//               "uri": "hub.foundries.io/detsch-temp-test-2/shellhttpd_base_30000@sha256:5636e38e170fc90a878149b2e474263c1f6909943117516cca892759f93ecc92"
//             }
//           },
//           "fetched-apps": {
//             "shortlist": "",
//             "uri": "https://api.foundries.io/projects/detsch-temp-test-2/lmp/builds/98/runs/publish-compose-apps/intel-corei7-64-lmp-98.apps.tar"
//           },
//           "hardwareIds": [
//             "intel-corei7-64"
//           ],
//           "image-file": "lmp-factory-image-intel-corei7-64.wic.gz",
//           "lmp-manifest-sha": "df8901a91a5eb3c8ae46ed863e1821856122aa4b",
//           "lmp-ver": "4.0.20-2-94.1",
//           "meta-subscriber-overrides-sha": "7d2677eaff43c25b05fc1e44409e080e91636846",
//           "name": "intel-corei7-64-lmp",
//           "origUriApps": "https://ci.foundries.io/projects/detsch-temp-test-2/lmp/builds/2",
//           "tags": [
//             "e2e-test-11"
//           ],
//           "targetFormat": "OSTREE",
//           "updatedAt": "2024-11-14T18:28:32Z",
//           "version": "99"
//         },
// return the list of "docker_compose_apps" uris

// Atributes of the UpdateContext instance are gradually set during the update process
type (
	UpdateContext struct {
		DbFilePath string

		Target          *metadata.TargetFiles
		CurrentTarget   *metadata.TargetFiles
		Reason          string
		RequiredApps    []string
		AppsToUninstall []string
		InstalledApps   []string
		ConfiguredApps  []string

		Context       context.Context
		ComposeConfig *compose.Config
		Runner        update.Runner
		Resuming      bool
		CorrelationId string
	}
)

func InitializeDatabase(dbFilePath string) error {
	err := targets.CreateTargetsTable(dbFilePath)
	if err != nil {
		return fmt.Errorf("failed to create targets table %v", err)
	}

	err = events.CreateEventsTable(dbFilePath)
	if err != nil {
		return fmt.Errorf("failed to create events table %v", err)
	}

	// TODO: When using aklite as docker credentials agent, additional tables are required: version and tls_creds
	return nil
}

func doUpdateClient(cmd *cobra.Command, args []string) {
	RunUpdateClient(srcDir, configPaths)
}

// Runs check + update (if needed) once. May become a loop in the future
func RunUpdateClient(srcDir string, cfgDirs []string) error {
	var configPaths []string
	if len(cfgDirs) > 0 {
		configPaths = cfgDirs
	} else {
		configPaths = sotatoml.DEF_CONFIG_ORDER
	}
	config, err := sotatoml.NewAppConfig(configPaths)
	if err != nil {
		log.Println("ERROR - unable to decode sota.toml:", err)
		os.Exit(1)
	}

	updateContext := &UpdateContext{
		DbFilePath: path.Join(config.GetDefault("storage.path", "/var/sota"), config.GetDefault("storage.sqldb_path", "sql.db")),
	}
	err = InitializeDatabase(updateContext.DbFilePath)
	if err != nil {
		log.Println("Error initializing database", err)
		return err
	}

	client := transport.CreateClient(config)
	fiotuf, err := tuf.NewFioTuf(config, client)
	if err != nil {
		log.Println("Error creating fiotuf instance", err)
		return err
	}

	var localRepoPath string
	if srcDir == "" {
		localRepoPath = ""
	} else {
		localRepoPath = path.Join(srcDir, "repo")
	}
	err = fiotuf.RefreshTuf(localRepoPath)
	if err != nil {
		log.Println("Error refreshing TUF", err)
		return err
	}

	tufTargets := fiotuf.GetTargets()
	err = GetTargetToInstall(updateContext, config, tufTargets)
	if err != nil {
		return fmt.Errorf("error getting target to install %v", err)
	}

	// log.Println("GetTargetToInstall", updateContext.Target, updateContext.AppsToInstall, updateContext.AppsToUninstall)
	if updateContext != nil {
		_, err := PerformUpdate(updateContext)
		// if doRollback {
		// 	log.Println("Rolling back", err)
		// 	err = Rollback(updateContext)
		// 	if err != nil {
		// 		log.Println("Error rolling back", err)
		// 		return err
		// 	}
		// }
		if err != nil {
			log.Println("Error updating to target:", err)
		}
	}

	ReportAppsStates(config, client, updateContext)

	eventsUrl := config.GetDefault("tls.server", "https://ota-lite.foundries.io:8443") + "/events"
	log.Println("Flushing events")
	events.FlushEvents(updateContext.DbFilePath, client, eventsUrl)
	return err
}

func ReportAppsStates(config *sotatoml.AppConfig, client *http.Client, updateContext *UpdateContext) error {
	log.Println("Reporting apps state (stub)")

	states, err := compose.CheckAppsStatus(updateContext.Context, updateContext.ComposeConfig, nil)
	if err != nil {
		log.Println("Error checking apps status", err)
		return err
	}

	currentTime := time.Now()
	utcTime := currentTime.UTC()
	rfc3339Time := utcTime.Format(time.RFC3339)

	// {
	//         "apps" :
	//         {
	//                 "shellhttpd_base_10000" :
	//                 {
	//                         "bundle_errors" : null,
	//                         "in_store" : true,
	//                         "missing_images" : null,
	//                         "name" : "shellhttpd_base_10000",
	//                         "services" :
	//                         [
	//                                 {
	//                                         "ctr-id" : "96c8f184b7a8441239fc62b3c0578afefb14d224a405ca153776910af3ba19b4",
	//                                         "hash" : "87d9fa5844ec9e68677c06df8ab04e2f06170c9508ac47368e723badf36fb602",
	//                                         "image" : "hub.foundries.io/detsch-temp-test-1/shellhttpd_base_10000@sha256:98e4fcf4df5e67ec7dcb4efb83d4d5b3dc8ca85a1fc792d4a4806e92e26228c4",
	//                                         "name" : "httpd_1",
	//                                         "state" : "running",
	//                                         "status" : "Up 10 hours"
	//                                 },
	//                                 {
	//                                         "ctr-id" : "fb41f07a31e88f54baf9f4d1a460cf430539f830ad6f5ba8337d3965aaa89ec1",
	//                                         "hash" : "61af703d24a6401c491bbe7ddc177944a3869ba6ada7aba4b32dcb352573f941",
	//                                         "image" : "hub.foundries.io/detsch-temp-test-1/shellhttpd_base_10000@sha256:98e4fcf4df5e67ec7dcb4efb83d4d5b3dc8ca85a1fc792d4a4806e92e26228c4",
	//                                         "name" : "httpd_4",
	//                                         "state" : "running",
	//                                         "status" : "Up 10 hours"
	//                                 },
	//                                 {
	//                                         "ctr-id" : "cc852a089f6324ad656591cea83a6fa7bab18f5923db71e4e3f0237ba5a8e33a",
	//                                         "hash" : "9519bdad5105d7af7d7cee424b15016ef4aab48670dbcc2a29bc52361df589b9",
	//                                         "image" : "hub.foundries.io/detsch-temp-test-1/shellhttpd_base_10000@sha256:98e4fcf4df5e67ec7dcb4efb83d4d5b3dc8ca85a1fc792d4a4806e92e26228c4",
	//                                         "name" : "httpd_2",
	//                                         "state" : "running",
	//                                         "status" : "Up 10 hours"
	//                                 },
	//                                 {
	//                                         "ctr-id" : "3e9bebc47b40adf32a5c21aab736b23d80fbcfc107ea4aa99aed1c86b6c63281",
	//                                         "hash" : "c2db3a7238a7ce539692eb4822ae9b4336bb691cd5117dce32e0f4112e70158a",
	//                                         "image" : "hub.foundries.io/detsch-temp-test-1/shellhttpd_base_10000@sha256:98e4fcf4df5e67ec7dcb4efb83d4d5b3dc8ca85a1fc792d4a4806e92e26228c4",
	//                                         "name" : "httpd_5",
	//                                         "state" : "running",
	//                                         "status" : "Up 10 hours"
	//                                 },
	//                                 {
	//                                         "ctr-id" : "bae97284c81a4c3b54e6a64b45b326f83f9da2bb3434b97aa7591e11b4d1710d",
	//                                         "hash" : "c601a495a5cb4809e3c3604cad08ead965902f4a9ed28a8133a997cb14dbdd71",
	//                                         "image" : "hub.foundries.io/detsch-temp-test-1/shellhttpd_base_10000@sha256:98e4fcf4df5e67ec7dcb4efb83d4d5b3dc8ca85a1fc792d4a4806e92e26228c4",
	//                                         "name" : "httpd_3",
	//                                         "state" : "running",
	//                                         "status" : "Up 10 hours"
	//                                 }
	//                         ],
	//                         "state" : "healthy",
	//                         "uri" : "hub.foundries.io/detsch-temp-test-1/shellhttpd_base_10000@sha256:643ec5ca445dcccba14f3ce704f6b7d4ad80756047dbcee4fd1ab938a10a5a41"
	//                 },
	//                 "shellhttpd_base_20000" :
	//                 {
	//                         "bundle_errors" : null,
	//                         "in_store" : true,
	//                         "missing_images" : null,
	//                         "name" : "shellhttpd_base_20000",
	//                         "services" :
	//                         [
	//                                 {
	//                                         "ctr-id" : "4b3afad8a22bc35c80936af2daf8ba5ddf884869d78a34a0275e2daa9692d21b",
	//                                         "hash" : "4e23ce3445a554c071b604bc52e88d34e675c65e2d85f57b8a7080a1ff616684",
	//                                         "image" : "hub.foundries.io/detsch-temp-test-1/shellhttpd_base_20000@sha256:93257af580de925e5373711539af1ec339b771d33757560a607b9169c9124f58",
	//                                         "name" : "httpd_1",
	//                                         "state" : "running",
	//                                         "status" : "Up 10 hours"
	//                                 }
	//                         ],
	//                         "state" : "healthy",
	//                         "uri" : "hub.foundries.io/detsch-temp-test-1/shellhttpd_base_20000@sha256:b15e4c02148e10283423938d97f1a105810c6670e81e2e79d6c8040533ff9183"
	//                 },
	//                 "shellhttpd_base_30000" :
	//                 {
	//                         "bundle_errors" : null,
	//                         "in_store" : true,
	//                         "missing_images" : null,
	//                         "name" : "shellhttpd_base_30000",
	//                         "services" :
	//                         [
	//                                 {
	//                                         "ctr-id" : "9452b5770cea99400fd4c2e459b1589d8f922cdbf8f49eabb3212cc7ee8730d4",
	//                                         "hash" : "59ac6764c15175110678c5ca5b7f3d1e7f538d06608c9141479e7a163d77c430",
	//                                         "image" : "hub.foundries.io/detsch-temp-test-1/shellhttpd_base_10000@sha256:98e4fcf4df5e67ec7dcb4efb83d4d5b3dc8ca85a1fc792d4a4806e92e26228c4",
	//                                         "name" : "httpd_2",
	//                                         "state" : "running",
	//                                         "status" : "Up 10 hours"
	//                                 },
	//                                 {
	//                                         "ctr-id" : "4c1162f8e0519c76ada7d868a6bb120513711bf09e646ba41e3cf642f9d7861b",
	//                                         "hash" : "ac7d05924baf50f63216bfa9987d6dad00020503e3d7e803ab84204ec4182a65",
	//                                         "image" : "hub.foundries.io/detsch-temp-test-1/shellhttpd_base_10000@sha256:98e4fcf4df5e67ec7dcb4efb83d4d5b3dc8ca85a1fc792d4a4806e92e26228c4",
	//                                         "name" : "httpd_1",
	//                                         "state" : "running",
	//                                         "status" : "Up 10 hours"
	//                                 }
	//                         ],
	//                         "state" : "healthy",
	//                         "uri" : "hub.foundries.io/detsch-temp-test-1/shellhttpd_base_30000@sha256:6b43c228e0a9867f368f4ca5f3c0c9f4037060a457741ddc0ed5bcb3046d74ed"
	//                 }
	//         },
	//         "deviceTime" : "2025-07-14T23:12:55Z",
	//         "ostree" : "2aed97e4925f7949c9762cacbba82ba947ee554f56aa4697aa9e5b3cee43d875"
	// }

	data := map[string]interface{}{
		"deviceTime": rfc3339Time,
		"ostree":     "8509e5bda0c762d2bac7f90d79c2f9bf560f0cdac2c4a2d6361a041a5a677566",
	}

	dataBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}

	log.Printf("Apps states: %s\n", string(dataBytes))
	appsStatesUrl := config.GetDefault("tls.server", "https://ota-lite.foundries.io:8443") + "/apps-states"

	res, err := transport.HttpPost(client, appsStatesUrl, data)
	if err != nil {
		log.Printf("Unable to send apps-state: %s", err)
	} else if res.StatusCode < 200 || res.StatusCode > 204 {
		log.Printf("Server could not process apps-states (%s): HTTP_%d - %s", interface{}(states), res.StatusCode, res.String())
	}
	return err
}

func FillAppsList(updateContext *UpdateContext) error {
	targetApps, err := GetAppsUris(updateContext.Target)
	if err != nil {
		log.Println("Error getting apps uris", err)
		return fmt.Errorf("error getting apps uris: %v", err)
	}

	requiredApps := []string{}
	for _, app := range targetApps {
		if updateContext.ConfiguredApps == nil || slices.Contains(updateContext.RequiredApps, app) {
			requiredApps = append(requiredApps, app)
		}
	}
	updateContext.RequiredApps = requiredApps

	installedApps, err := getInstalledApps(updateContext)
	log.Println("targetApps:", targetApps)
	log.Println("installedApps:", installedApps)
	if err != nil {
		log.Println("Error getting running apps", err)
		return fmt.Errorf("error getting running apps: %v", err)
	}
	appsToUninstall := []string{}
	for _, app := range installedApps {
		if !slices.Contains(updateContext.RequiredApps, app) {
			appsToUninstall = append(appsToUninstall, app)
		}
	}
	updateContext.AppsToUninstall = appsToUninstall
	updateContext.InstalledApps = installedApps
	return nil
}

func FillAndCheckAppsList(updateContext *UpdateContext) error {
	err := FillAppsList(updateContext)
	if err != nil {
		log.Println("Error filling apps list", err)
		return fmt.Errorf("error filling apps list: %v", err)
	}

	log.Println("Checking if current target is running", updateContext.Target.Path)
	isRunning, err := IsTargetRunning(updateContext)
	if err != nil {
		return fmt.Errorf("error checking target: %v", err)
	}

	if isRunning {
		log.Println("Target is running")
		updateContext.Target = nil
		updateContext.RequiredApps = nil
		if len(updateContext.AppsToUninstall) == 0 {
			log.Println("No apps to uninstall")
		} else {
			log.Println("Apps to uninstall:", updateContext.AppsToUninstall)
		}
	}
	return nil
}

// Returns information about the apps to install and to remove, as long as the corresponding target
// No update operation is performed at this point. Not even apps stopping
func GetTargetToInstall(updateContext *UpdateContext, config *sotatoml.AppConfig, tufTargets map[string]*metadata.TargetFiles) error {
	var err error

	updateContext.ComposeConfig, err = getComposeConfig(config)
	if err != nil {
		return err
	}

	currentTarget, err := targets.GetCurrentTarget(updateContext.DbFilePath)
	if err != nil {
		log.Println("Error getting current target", err)
	}

	versionInt := -1
	version := os.Getenv("UPDATE_TO_VERSION")
	if version != "" {
		versionInt, _ = strconv.Atoi(version)
		fmt.Println("Version set to", versionInt)
	}

	candidateTarget, _ := selectTarget(tufTargets, versionInt)
	if candidateTarget == nil {
		log.Println("No target found for version", versionInt)
		return fmt.Errorf("no target found for version %d", versionInt)
	}

	log.Println("Latest hash:", candidateTarget.Hashes["sha256"])

	// Check if target is marked as failing
	failing, _ := targets.IsFailingTarget(updateContext.DbFilePath, candidateTarget.Path)
	if failing {
		log.Println("Skipping failing target", candidateTarget.Path+" using "+currentTarget.Path+" instead")
		candidateTarget = currentTarget
	}

	updateContext.Target = candidateTarget
	updateContext.CurrentTarget = currentTarget
	updateContext.Context = context.Background()

	apps := config.GetDefault("pacman.compose_apps", "-")
	if apps != "-" {
		updateContext.ConfiguredApps = strings.Split(apps, ",")
		log.Println("pacman.compose_apps=", updateContext.ConfiguredApps)
	}

	err = FillAndCheckAppsList(updateContext)
	if err != nil {
		log.Println("FillAndCheckAppsList error", err)
		return err
	}

	// No update required
	if updateContext.Target == nil {
		log.Println("No update required")
		return nil
	}

	if updateContext.CurrentTarget.Path != updateContext.Target.Path {
		updateContext.Reason = "Updating from " + updateContext.CurrentTarget.Path + " to " + updateContext.Target.Path
	} else {
		updateContext.Reason = "Syncing Active Target Apps"
	}
	log.Println(updateContext.Reason)
	return nil
}

// Perform the actual update based on information collected before
func PerformUpdate(updateContext *UpdateContext) (bool, error) {
	// Valid cases:
	// If updateContext.Target is set, it is either an apps sync or version update. Events will be generated. updateContext.AppsToUninstall will be explicitly handled
	//   - If updateContext.AppsToInstall is empty, we will not initiate a composeapp update.
	//   - If updateContext.AppsToInstall is set, we will initiate a composeapp update.
	// If updateContext.Target is not set, updateContext.AppsToInstall shouldn't be set, and only handle updateContext.AppsToUninstall

	if updateContext.Target == nil {
		return false, StopAndRemoveApps(updateContext)
	} else {
		return UpdateToTarget(updateContext)
	}
}

func UpdateToTarget(updateContext *UpdateContext) (bool, error) {
	log.Println("Updating to target", updateContext.Target.Path)
	// updateContext.Target must be set
	// updateContext.AppsToInstall might be empty. In this case, we will not initiate a composeapp update, just remove the required apps and geenerate the events

	err := InitUpdate(updateContext)
	if err != nil {
		return false, fmt.Errorf("error initializing update for target: %v", err)
	}

	// Pull
	err = PullTarget(updateContext)
	if err != nil {
		return false, fmt.Errorf("error pulling target: %v", err)
	}

	// Install
	err = InstallTarget(updateContext)
	if err != nil {
		return false, fmt.Errorf("error installing target: %v", err)
	}

	// Run
	doRollback, err := StartTarget(updateContext)
	if err != nil {
		return doRollback, fmt.Errorf("error running target: %v", err)
	}

	return false, nil
}

func StopAndRemoveApps(updateContext *UpdateContext) error {
	if len(updateContext.AppsToUninstall) == 0 {
		log.Println("No apps to uninstall")
		return nil
	}

	log.Printf("StopApps apps %v\n", updateContext.AppsToUninstall)
	err := compose.StopApps(updateContext.Context, updateContext.ComposeConfig, updateContext.AppsToUninstall)
	if err != nil {
		log.Println("Error stopping apps", err)
		// return fmt.Errorf("error stopping apps: %v", err)
	}

	log.Printf("Uninstall apps %v\n", updateContext.AppsToUninstall)
	err = compose.UninstallApps(updateContext.Context, updateContext.ComposeConfig, updateContext.AppsToUninstall)
	if err != nil {
		log.Println("Error uninstalling apps", err)
		// return fmt.Errorf("error uninstalling apps: %v", err)
	}

	log.Printf("Remove apps %v\n", updateContext.AppsToUninstall)
	err = compose.RemoveApps(updateContext.Context, updateContext.ComposeConfig, updateContext.AppsToUninstall)
	if err != nil {
		log.Println("Error removing apps", err)
		return fmt.Errorf("error removing apps: %v", err)
	}

	return nil
}

func GenAndSaveEvent(updateContext *UpdateContext, eventType events.EventTypeValue, details string, success *bool) error {
	version, _ := GetVersion(updateContext.Target)
	targetName := updateContext.Target.Path
	evt := events.NewEvent(eventType, details, success, updateContext.CorrelationId, targetName, version)
	return events.SaveEvent(updateContext.DbFilePath, &evt[0])
}

func GetAppsUris(target *metadata.TargetFiles) ([]string, error) {
	var tc targets.TargetCustom
	var b []byte
	b, _ = (*target.Custom).MarshalJSON()
	err := json.Unmarshal(b, &tc)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling event: %v", err)
	}

	var appsUris []string
	var dockerComposeApps map[string]interface{}
	err = json.Unmarshal(b, &dockerComposeApps)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling docker_compose_apps: %v", err)
	}

	if apps, ok := dockerComposeApps["docker_compose_apps"].(map[string]interface{}); ok {
		for _, app := range apps {
			if appDetails, ok := app.(map[string]interface{}); ok {
				if uri, ok := appDetails["uri"].(string); ok {
					appsUris = append(appsUris, uri)
				}
			}
		}
	} else {
		appsUris = []string{}
		// return nil, fmt.Errorf("docker_compose_apps field is missing or invalid")
	}

	return appsUris, nil
}

func GetVersion(target *metadata.TargetFiles) (int, error) {
	var tc targets.TargetCustom
	var b []byte
	b, _ = (*target.Custom).MarshalJSON()
	err := json.Unmarshal(b, &tc)
	if err != nil {
		return -1, fmt.Errorf("error unmarshaling event: %v", err)
	}
	version, err := strconv.Atoi(tc.Version)
	if err != nil {
		return -1, fmt.Errorf("error converting version to int: %v", err)
	}
	return version, nil
}

func selectTarget(allTargets map[string]*metadata.TargetFiles, version int) (*metadata.TargetFiles, error) {
	latest := -1
	var selectedTarget *metadata.TargetFiles
	for name := range allTargets {
		var tc targets.TargetCustom
		var b []byte
		b, _ = (*allTargets[name].Custom).MarshalJSON()
		err := json.Unmarshal(b, &tc)
		if err != nil {
			continue
		}

		v, err := strconv.Atoi(tc.Version)
		if err != nil {
			continue
		}
		if (version > 0 && version == v) || (version <= 0 && v > latest) {
			selectedTarget = allTargets[name]
			latest = v
		}
		log.Println(name, tc.Version)

	}
	log.Println(latest)
	return selectedTarget, nil
}

func getInstalledApps(updateContext *UpdateContext) ([]string, error) {
	ret := []string{}
	apps, err := compose.ListApps(updateContext.Context, updateContext.ComposeConfig)
	if err != nil {
		log.Println("Error listing apps", err)
		return nil, fmt.Errorf("error listing apps: %v", err)
	}
	for _, app := range apps {
		if app.Name() != "" {
			ret = append(ret, app.Ref().Spec.Locator+"@"+app.Ref().Digest.String())
		}
	}
	return ret, nil
}

func getComposeConfig(config *sotatoml.AppConfig) (*compose.Config, error) {
	cfg, err := v1.NewDefaultConfig(
		v1.WithStoreRoot(config.GetDefault("pacman.reset_apps_root", "/var/sota/reset-apps")),
		v1.WithComposeRoot(config.GetDefault("pacman.compose_apps_root", "/var/sota/compose-apps")),
		v1.WithUpdateDB(path.Join(config.GetDefault("storage.path", "/var/sota"), "updates.db")),
	)
	if err != nil {
		return nil, err
	}

	return cfg, nil
}
