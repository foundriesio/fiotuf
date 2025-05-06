package updateclient

import (
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/foundriesio/fiotuf/events"
	"github.com/foundriesio/fiotuf/targets"

	_ "modernc.org/sqlite"

	"github.com/foundriesio/composeapp/pkg/compose"
	"github.com/foundriesio/composeapp/pkg/update"
	"github.com/schollz/progressbar/v3"
)

func InitUpdate(updateContext *UpdateContext) error {
	updateRunner, err := update.GetCurrentUpdate(updateContext.ComposeConfig)
	var correlationId string
	if !errors.Is(err, update.ErrUpdateNotFound) {
		updateStatus := updateRunner.Status()
		log.Println("Current update:", updateStatus)

		clientRef := updateStatus.ClientRef
		clientRefSplit := strings.Split(clientRef, "|")
		if (clientRefSplit == nil) || (len(clientRefSplit) != 2) {
			log.Printf("Invalid clientRef: %s\n", clientRef)
			err = updateRunner.Cancel(updateContext.Context)
			if err != nil {
				return fmt.Errorf("error cancelling update: %v", err)
			}
		}

		targetName := clientRefSplit[0]
		correlationId = clientRefSplit[1]

		if updateStatus.State == update.StateStarted {
			updateRunner.Complete(updateContext.Context)
		}

		updateStatus = updateRunner.Status()
		if updateStatus.State != update.StateCompleted {
			if updateStatus.State != update.StateInitializing && updateStatus.State != update.StateCanceled && updateStatus.State != update.StateCancelling && targetName == updateContext.Target.Path && appsListMatch(updateContext.RequiredApps, updateStatus.URIs) {
				log.Printf("Proceeding with previous update of %s (%s)\n", updateStatus.URIs, targetName)
				updateContext.Resuming = true
			} else {
				fmt.Printf("Cancelling current update: %s\n", updateStatus.ID)
				correlationId = ""
				err = updateRunner.Cancel(updateContext.Context)
				if err != nil {
					return fmt.Errorf("error cancelling update: %v", err)
				}
			}
		}
	}

	if !updateContext.Resuming {
		version, err := GetVersion(updateContext.Target)
		if err != nil {
			return fmt.Errorf("error getting version: %v", err)
		}
		correlationId = fmt.Sprintf("%d-%d", version, time.Now().Unix())

		if len(updateContext.RequiredApps) == 0 {
			// Do not invoke composeapp update if there are no apps to install. updateRunner.Init does not accept an empty apps list
			updateRunner = nil
		} else {
			updateRunner, err = update.NewUpdate(updateContext.ComposeConfig, updateContext.Target.Path+"|"+correlationId)
			if err != nil {
				return err
			}

			// Progress bar
			bar := progressbar.DefaultBytes(int64(len(updateContext.RequiredApps)))
			initOptions := []update.InitOption{
				update.WithInitProgress(func(status *update.InitProgress) {
					if status.Current == 0 {
						return
					}
					if status.State == update.UpdateInitStateLoadingTree {
						if err := bar.Set(status.Current); err != nil {
							log.Printf("Error setting progress bar: %s\n", err.Error())
						}
					} else {
						if bar == nil {
							bar = progressbar.Default(int64(status.Total))
						}
						if err := bar.Set(status.Current); err != nil {
							log.Printf("Error setting progress bar: %s\n", err.Error())
						}
					}
				})}

			err = updateRunner.Init(updateContext.Context, updateContext.RequiredApps, initOptions...)
			if err != nil {
				return err
			}
		}
	}
	updateContext.Runner = updateRunner
	updateContext.CorrelationId = correlationId
	return nil
}

func PullTarget(updateContext *UpdateContext) error {
	log.Println("Pulling target", updateContext.Target)

	var updateStatus update.Update
	invokeComposeUpdate := updateContext.Runner != nil
	if invokeComposeUpdate {
		updateStatus = updateContext.Runner.Status()
		if updateStatus.State != update.StateInitialized && updateStatus.State != update.StateFetching {
			log.Printf("update has already been fetched. Update state: %s\n", updateStatus.State)
			if updateContext.Resuming {
				return nil
			}
			// If we are not resuming an update, still generate events
			invokeComposeUpdate = false
		}
	}

	err := GenAndSaveEvent(updateContext, events.DownloadStarted, updateContext.Reason, nil)
	if err != nil {
		return fmt.Errorf("error on GenAndSaveEvent: %v", err)
	}

	// Progress bar
	if invokeComposeUpdate {
		bar := progressbar.DefaultBytes(updateStatus.TotalBlobDownloadSize)
		fetchOptions := []update.FetchOption{
			update.WithFetchProgress(func(status *update.FetchProgress) {
				if err := bar.Set64(status.Current); err != nil {
					log.Printf("Error setting progress bar: %s\n", err.Error())
				}
			}),
			update.WithProgressPollInterval(200)}

		err = updateContext.Runner.Fetch(updateContext.Context, fetchOptions...)
		if err != nil {
			err := GenAndSaveEvent(updateContext, events.DownloadCompleted, err.Error(), targets.BoolPointer(false))
			return fmt.Errorf("error pulling target: %v", err)
		}

		updateStatus = updateContext.Runner.Status()
		if updateStatus.State != update.StateFetched {
			log.Println("update not fetched")
		}
		if updateStatus.Progress != 100 {
			log.Printf("update is not fetched for 100%%: %d\n", updateStatus.Progress)
		}
	}

	err = GenAndSaveEvent(updateContext, events.DownloadCompleted, "", targets.BoolPointer(true))
	if err != nil {
		return fmt.Errorf("error on GenAndSaveEvent: %v", err)
	}

	return nil
}

type progressRendererCtx struct {
	bar        *progressbar.ProgressBar
	curImageID string
	curLayerID string
}

func getProgressRenderer() compose.InstallProgressFunc {
	ctx := &progressRendererCtx{}

	return func(p *compose.InstallProgress) {
		switch p.AppInstallState {
		case compose.AppInstallStateComposeInstalling:
			{
				log.Printf("Installing app %s\n", p.AppID)
			}
		case compose.AppInstallStateComposeChecking:
			{
			}
		case compose.AppInstallStateImagesLoading:
			{
				renderImageLoadingProgress(ctx, p)
			}
		}
	}
}

func renderImageLoadingProgress(ctx *progressRendererCtx, p *compose.InstallProgress) {
	switch p.ImageLoadState {
	case compose.ImageLoadStateLayerLoading:
		{
			if ctx.curImageID != p.ImageID {
				log.Printf("  Loading image %s\n", p.ImageID)
				ctx.curImageID = p.ImageID
				ctx.curLayerID = ""
			}
			if ctx.curLayerID != p.ID {
				ctx.bar = progressbar.DefaultBytes(p.Total)
				ctx.bar.Describe(fmt.Sprintf("    %s", p.ID))
				ctx.curLayerID = p.ID
			}
			if err := ctx.bar.Set64(p.Current); err != nil {
				log.Printf("Error setting progress bar: %s\n", err.Error())
			}
		}
	case compose.ImageLoadStateLayerSyncing:
		{
			// TODO: render layer syncing progress
			//fmt.Print(".")
		}
	case compose.ImageLoadStateLayerLoaded:
		{
			//fmt.Println("ok")
			ctx.curLayerID = ""
			ctx.bar.Close()
			ctx.bar = nil
		}
	case compose.ImageLoadStateImageLoaded:
		{
			log.Printf("  Image loaded: %s\n", p.ImageID)
		}
	case compose.ImageLoadStateImageExist:
		{
			log.Printf("  Already exists: %s\n", p.ImageID)
		}
	default:
		log.Printf("  Unknown state %s\n", p.ImageLoadState)
	}
}

func InstallTarget(updateContext *UpdateContext) error {
	log.Println("Installing target", updateContext.Target)

	invokeComposeUpdate := updateContext.Runner != nil
	if invokeComposeUpdate {
		updateStatus := updateContext.Runner.Status()
		if updateStatus.State != update.StateFetched && updateStatus.State != update.StateInstalling {
			log.Printf("update was already installed. Update state: %s\n", updateStatus.State)
			if updateContext.Resuming {
				return nil
			}
			// If we are not resuming an update, still generate events
			invokeComposeUpdate = false
		}
	}

	targets.RegisterInstallationStarted(updateContext.DbFilePath, updateContext.Target, updateContext.CorrelationId)
	err := GenAndSaveEvent(updateContext, events.InstallationStarted, updateContext.Reason, nil)
	if err != nil {
		log.Println("error on GenAndSaveEvent", err)
	}

	if invokeComposeUpdate {
		installOptions := []compose.InstallOption{
			compose.WithInstallProgress(getProgressRenderer())}

		compose.StopApps(updateContext.Context, updateContext.ComposeConfig, updateContext.AppsToUninstall)
		err = updateContext.Runner.Install(updateContext.Context, installOptions...)
	}
	if err != nil {
		err := GenAndSaveEvent(updateContext, events.DownloadCompleted, err.Error(), targets.BoolPointer(false))
		return fmt.Errorf("error installing target: %v", err)
	}

	if invokeComposeUpdate {
		updateStatus := updateContext.Runner.Status()
		if updateStatus.State != update.StateInstalled {
			log.Println("update not installed")
		}
		if updateStatus.Progress != 100 {
			log.Printf("update is not installed for 100%%: %d\n", updateStatus.Progress)
		}
	}

	err = GenAndSaveEvent(updateContext, events.InstallationApplied, "", targets.BoolPointer(true))
	if err != nil {
		log.Println("error on GenAndSaveEvent", err)
	}
	return nil
}

func StartTarget(updateContext *UpdateContext) (bool, error) {
	log.Println("Running target", updateContext.Target)

	var err error
	invokeComposeUpdate := updateContext.Runner != nil
	if invokeComposeUpdate {
		updateStatus := updateContext.Runner.Status()
		if updateStatus.State != update.StateInstalled && updateStatus.State != update.StateStarting {
			log.Printf("Skipping start target operation because state is: %s\n", updateStatus.State)
			if updateContext.Resuming {
				return false, nil
			}
			invokeComposeUpdate = false
		}
	}

	compose.StopApps(updateContext.Context, updateContext.ComposeConfig, updateContext.AppsToUninstall)
	// StopAndRemoveApps(updateContext) // No need to uninstall explicitly if CompleteWithPruning() is in use when completing

	if invokeComposeUpdate {
		err = updateContext.Runner.Start(updateContext.Context)
		if err != nil {
			log.Println("error on starting target", err)
			err := GenAndSaveEvent(updateContext, events.InstallationCompleted, err.Error(), targets.BoolPointer(false))
			if err != nil {
				log.Println("error on GenAndSaveEvent", err)
			}
			targets.RegisterInstallationFailed(updateContext.DbFilePath, updateContext.Target, updateContext.CorrelationId)

			rollback(updateContext)

			return false, fmt.Errorf("rolled back to previous target")
		}

		if updateContext.Runner.Status().State != update.StateStarted {
			log.Println("update not started")
		}

		updateStatus := updateContext.Runner.Status()
		if updateStatus.Progress != 100 {
			log.Printf("update is not started for 100%%: %d\n", updateStatus.Progress)
		}
	}

	err = GenAndSaveEvent(updateContext, events.InstallationCompleted, "", targets.BoolPointer(true))
	if err != nil {
		log.Println("error on GenAndSaveEvent", err)
	}
	targets.RegisterInstallationSuceeded(updateContext.DbFilePath, updateContext.Target, updateContext.CorrelationId)

	if invokeComposeUpdate {
		err = updateContext.Runner.Complete(updateContext.Context, update.CompleteWithPruning())
		if err != nil {
			log.Println("error completing update:", err)
		}
	}

	return false, nil
}

func rollback(updateContext *UpdateContext) error {
	log.Println("Rolling back to target", updateContext.CurrentTarget.Path)

	if updateContext.Runner != nil {

		updateStatus := updateContext.Runner.Status()
		if updateStatus.State == update.StateStarted {
			err := updateContext.Runner.Complete(updateContext.Context)
			if err != nil {
				log.Println("Rollback: Error updateContext.Runner.Complete", err)
			}
		} else {
			err := updateContext.Runner.Cancel(updateContext.Context)
			if err != nil {
				log.Println("Rollback: Error updateContext.Runner.Cancel", err)
				return err
			}
		}

		updateContext.Runner = nil
		updateContext.Resuming = false
	} else {
		log.Println("Rollback: No installation to cancel")
	}

	updateContext.Reason = "Rolling back to " + updateContext.CurrentTarget.Path
	updateContext.Target = updateContext.CurrentTarget
	updateRunner, err := update.NewUpdate(updateContext.ComposeConfig, updateContext.Target.Path+"|"+updateContext.CorrelationId)
	if err != nil {
		log.Println("Rollback: Error calling update.NewUpdate", err)
		return err
	}

	err = FillAndCheckAppsList(updateContext)
	if err != nil {
		log.Println("Rollback: Error calling FillAndCheckAppsList", err)
		return err
	}

	if updateContext.Target == nil {
		// Target is already running
		log.Println("Rollback: Target is already running", updateContext.Target)
		return nil
	}

	if len(updateContext.RequiredApps) > 0 {
		err = updateRunner.Init(updateContext.Context, updateContext.RequiredApps)
		if err != nil {
			log.Println("rollback init error", err)
			return err
		}
	}

	updateStatus := updateRunner.Status()
	// Must be in fetched state
	if updateStatus.State != update.StateFetched && updateStatus.State != update.StateInstalled {
		log.Println("rollback wrong state error", updateStatus.State)
		return fmt.Errorf("rollback update was not fetched %s", updateStatus.State)
	}

	log.Println("Proceeding with rollback. Current update runner state is", updateStatus.State)

	updateContext.Runner = updateRunner
	err = InstallTarget(updateContext)
	if err != nil {
		log.Println("rollback error installing target", err)
		return err
	}
	_, err = StartTarget(updateContext)
	if err != nil {
		log.Println("rollback error starting target", err)
		return err
	}

	log.Println("rollback done", err)
	return nil
}

func IsTargetRunning(updateContext *UpdateContext) (bool, error) {
	log.Println("Checking target", updateContext.Target)
	if updateContext.Target.Path != updateContext.CurrentTarget.Path {
		log.Println("IsTargetRunning: Running name target is different than candidate name target", updateContext.CurrentTarget.Path, updateContext.Target.Path)
		return false, nil
	}

	// updateStatus, err := update.GetLastSuccessfulUpdate(updateContext.ComposeConfig)
	// if err != nil {
	// 	log.Println("error getting last update", err)
	// 	return false, err
	// }
	if isSublist(updateContext.InstalledApps, updateContext.RequiredApps) {
		log.Println("Installed applications match selected target apps")
		err := compose.CheckRunning(updateContext.Context, updateContext.ComposeConfig, updateContext.RequiredApps)
		if err != nil {
			log.Println("Required applications are not running", err)
			return false, nil
		} else {
			log.Println("Required applications are are running")
			return true, nil
		}
	} else {
		log.Println("Installed applications list do not contain all target apps")
		return false, nil
	}
}

func isSublist[S ~[]E, E comparable](mainList, sublist S) bool {
	if len(sublist) > len(mainList) {
		return false
	}
	for _, subElem := range sublist {
		if !slices.Contains(mainList, subElem) {
			return false
		}
	}
	return true
}

func appsListMatch(appsList1 []string, appsList2 []string) bool {
	if len(appsList1) != len(appsList2) {
		return false
	}

	for _, app1 := range appsList1 {
		found := false
		for _, app2 := range appsList2 {
			if app1 == app2 {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// func progress(status *update.InitProgress) {
// 	if status.State == update.UpdateInitStateLoadingTree {
// 	} else {
// 	}
// }
