package updateclient

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"slices"
	"strconv"

	"github.com/foundriesio/fiotuf/events"
	"github.com/foundriesio/fiotuf/targets"
	"github.com/foundriesio/fiotuf/tuf"
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
		Reason          string
		RequiredApps    []string
		AppsToUninstall []string

		Context       context.Context
		ComposeConfig *compose.Config
		Runner        update.Runner
		Resuming      bool
		CorrelationId string
	}
)

// Runs check + update (if needed) once. May become a loop in the future
func RunUpdateClient(srcDir string) error {
	config, err := sotatoml.NewAppConfig(sotatoml.DEF_CONFIG_ORDER)
	if err != nil {
		log.Println("ERROR - unable to decode sota.toml:", err)
		os.Exit(1)
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

	allTargets := fiotuf.GetTargets()

	updateContext, err := GetTargetToInstall(config, allTargets)
	if err != nil {
		return fmt.Errorf("error getting target to install %v", err)
	}

	// log.Println("GetTargetToInstall", updateContext.Target, updateContext.AppsToInstall, updateContext.AppsToUninstall)
	if updateContext != nil {
		doRollback, err := PerformUpdate(updateContext)
		if doRollback {
			log.Println("Rolling back", err)
			err = Rollback(updateContext)
			if err != nil {
				log.Println("Error rolling back", err)
				return err
			}
		}
		if err != nil {
			log.Println("Error updating to target:", err)
			return err
		}
	}

	eventsUrl := config.GetDefault("tls.server", "https://ota-lite.foundries.io:8443") + "/events"
	log.Println("Flushing events")
	events.FlushEvents(updateContext.DbFilePath, client, eventsUrl)
	return nil
}

// Returns information about the apps to install and to remove, as long as the corresponding target
// No update operation is performed at this point. Not even apps stopping
func GetTargetToInstall(config *sotatoml.AppConfig, allTargets map[string]*metadata.TargetFiles) (*UpdateContext, error) {
	updateContext := &UpdateContext{
		DbFilePath: path.Join(config.GetDefault("storage.path", "/var/sota"), config.GetDefault("storage.sqldb_path", "sql.db")),
	}
	var err error

	updateContext.ComposeConfig, err = getComposeConfig(config)
	if err != nil {
		return nil, err
	}

	currentTarget, err := targets.GetCurrentTarget(updateContext.DbFilePath)
	if err != nil {
		log.Println("Error getting current target", err)
	}

	candidateTarget, _ := getLatestTarget(allTargets)
	log.Println("Latest hash:", candidateTarget.Hashes["sha256"])

	// Check if target is marked as failing
	failing, _ := targets.IsFailingTarget(updateContext.DbFilePath, candidateTarget.Path)
	if failing {
		log.Println("Skipping failing target", candidateTarget.Path+" using "+currentTarget.Path+" instead")
		candidateTarget = currentTarget
	}

	updateContext.Target = candidateTarget
	targetApps, err := GetAppsUris(candidateTarget)
	if err != nil {
		log.Println("Error getting apps uris", err)
		return nil, fmt.Errorf("error getting apps uris: %v", err)
	}
	updateContext.RequiredApps = targetApps
	updateContext.Context = context.Background()

	installedApps, err := getInstalledApps(updateContext)
	log.Println("targetApps", targetApps)
	log.Println("runningApps", installedApps)
	if err != nil {
		log.Println("Error getting running apps", err)
		return nil, fmt.Errorf("error getting running apps: %v", err)
	}
	appsToUninstall := []string{}
	for _, app := range installedApps {
		if !slices.Contains(updateContext.RequiredApps, app) {
			appsToUninstall = append(appsToUninstall, app)
		}
	}
	updateContext.AppsToUninstall = appsToUninstall

	var currentTargetName string
	if currentTarget == nil {
		currentTargetName = "Initial Target"
	} else {
		currentTargetName = currentTarget.Path
	}

	if updateContext.Target.Path == currentTargetName {
		log.Println("Checking if current target is running", currentTargetName)
		isRunning, err := IsTargetRunning(updateContext, installedApps)
		if err != nil {
			return nil, fmt.Errorf("error checking target: %v", err)
		}

		if isRunning {
			log.Println("Target is running")
			updateContext.Target = nil
			updateContext.RequiredApps = nil
			return updateContext, nil
		} else {
			log.Println("Target is not running. Proceeding with an apps sync update")
		}
	}

	if updateContext.Target != nil && currentTargetName != updateContext.Target.Path {
		updateContext.Reason = "Updating from " + currentTargetName + " to " + updateContext.Target.Path
	} else {
		updateContext.Reason = "Syncing Active Target Apps"
	}

	return updateContext, nil
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
	err := compose.StopApps(updateContext.Context, updateContext.ComposeConfig, updateContext.AppsToUninstall)
	if err != nil {
		log.Println("Error stopping apps", err)
		return fmt.Errorf("error stopping apps: %v", err)
	}

	err = compose.RemoveApps(updateContext.Context, updateContext.ComposeConfig, updateContext.AppsToUninstall)
	if err != nil {
		log.Println("Error removing apps", err)
		return fmt.Errorf("error removing apps: %v", err)
	}

	err = compose.UninstallApps(updateContext.Context, updateContext.ComposeConfig, updateContext.AppsToUninstall)
	if err != nil {
		log.Println("Error uninstalling apps", err)
		return fmt.Errorf("error uninstalling apps: %v", err)
	}

	return nil
}

func Rollback(updateContext *UpdateContext) error {
	log.Println("Rolling back")
	// TODO: implement me
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

func getLatestTarget(allTargets map[string]*metadata.TargetFiles) (*metadata.TargetFiles, error) {
	latest := -1
	var latestTarget *metadata.TargetFiles
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
		if v > latest {
			latestTarget = allTargets[name]
			latest = v
		}
		log.Println(name, tc.Version)

	}
	return latestTarget, nil
}

func getInstalledApps(updateContext *UpdateContext) ([]string, error) {
	return compose.ListApps(updateContext.Context, updateContext.ComposeConfig)
}

func getComposeConfig(config *sotatoml.AppConfig) (*compose.Config, error) {
	cfg, err := v1.NewDefaultConfig()
	if err != nil {
		return nil, err
	}

	cfg.StoreRoot = config.GetDefault("pacman.reset_apps_root", "/var/sota/reset-apps")
	cfg.DBFilePath = path.Join(config.GetDefault("storage.path", "/var/sota"), "updates.db")
	return cfg, nil
}
