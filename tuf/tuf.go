package tuf

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/foundriesio/fioconfig/sotatoml"
	"github.com/foundriesio/fioconfig/transport"
	"github.com/go-logr/stdr"
	"github.com/theupdateframework/go-tuf/v2/metadata"
	"github.com/theupdateframework/go-tuf/v2/metadata/config"
	"github.com/theupdateframework/go-tuf/v2/metadata/updater"
)

type FioFetcher struct {
	client  *http.Client
	tag     string
	repoUrl string
}

type FioTuf struct {
	config     *sotatoml.AppConfig
	client     *http.Client
	fioUpdater *updater.Updater
}

func NewFioTuf(config *sotatoml.AppConfig) (*FioTuf, error) {
	client := transport.CreateClient(config)

	up, err := newFioUpdater(config, client, "")
	if err != nil {
		return nil, err
	}

	ret := FioTuf{
		config:     config,
		client:     client,
		fioUpdater: up,
	}

	return &ret, nil
}

func (fiotuf *FioTuf) RefreshTuf(localRepoPath string) error {
	metadata.SetLogger(stdr.New(log.New(os.Stdout, "", log.LstdFlags)))

	up, err := newFioUpdater(fiotuf.config, fiotuf.client, "")
	if err != nil {
		return err
	}
	fiotuf.fioUpdater = up

	// try to build the top-level metadata
	err = up.Refresh()
	if err != nil {
		log.Println("failed to refresh trusted metadata: ", err)
		return err
	}
	log.Println("TUF refresh successful")
	// for name := range up.GetTopLevelTargets() {
	// 	log.Println("target name " + name)
	// }
	return nil
}

func (fiotuf *FioTuf) GetTargets() map[string]*metadata.TargetFiles {
	return fiotuf.fioUpdater.GetTopLevelTargets()
}

func (fiotuf *FioTuf) GetRoot() *metadata.Metadata[metadata.RootType] {
	return fiotuf.fioUpdater.GetTrustedMetadataSet().Root
}

// DownloadFile downloads a file from urlPath, errors out if it failed,
// its length is larger than maxLength or the timeout is reached.
func (d *FioFetcher) DownloadFile(urlPath string, maxLength int64, timeout time.Duration) ([]byte, error) {
	if strings.HasPrefix(urlPath, "file://") {
		return readLocalFile(urlPath[len("file://"):])
	} else {
		return readRemoteFile(d, urlPath, maxLength)
	}
}

func readLocalFile(filePath string) ([]byte, error) {
	log.Println("Reading local file:", filePath)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, &metadata.ErrDownloadHTTP{StatusCode: 404, URL: "file://" + filePath}
	}
	return data, nil
}

func readRemoteFile(d *FioFetcher, urlPath string, maxLength int64) ([]byte, error) {
	log.Println("Fetching remote file: " + urlPath)
	headers := make(map[string]string)
	headers["x-ats-tags"] = d.tag
	res, err := transport.HttpGet(d.client, urlPath, headers)

	if err != nil {
		return nil, err
	}

	if res.StatusCode != http.StatusOK {
		return nil, &metadata.ErrDownloadHTTP{StatusCode: res.StatusCode, URL: urlPath}
	}

	var length int64
	// Get content length from header (might not be accurate, -1 or not set).
	if header := res.Header.Get("Content-Length"); header != "" {
		length, err = strconv.ParseInt(header, 10, 0)
		if err != nil {
			return nil, err
		}
		// Error if the reported size is greater than what is expected.
		if length > maxLength {
			return nil, &metadata.ErrDownloadLengthMismatch{Msg: fmt.Sprintf("download failed for %s, length %d is larger than expected %d", urlPath, length, maxLength)}
		}
	}
	// // Although the size has been checked above, use a LimitReader in case
	// // the reported size is inaccurate, or size is -1 which indicates an
	// // unknown length. We read maxLength + 1 in order to check if the read data
	// // surpassed our set limit.
	// data, err := io.ReadAll(io.LimitReader(res.Body, maxLength+1))
	// if err != nil {
	// 	return nil, err
	// }
	// Error if the reported size is greater than what is expected.
	length = int64(len(res.Body))
	if length > maxLength {
		return nil, &metadata.ErrDownloadLengthMismatch{Msg: fmt.Sprintf("download failed for %s, length %d is larger than expected %d", urlPath, length, maxLength)}
	}

	return res.Body, nil
}

func getTufCfg(client *http.Client, repoUrl string, tag string) (*config.UpdaterConfig, error) {
	// TODO: do not hardcode path:
	localMetadataDir := "/var/sota/tuf/"
	provPath := "/usr/lib/sota/tuf/"

	rootPath := filepath.Join(localMetadataDir, "root.json")
	if _, err := os.Stat(rootPath); errors.Is(err, os.ErrNotExist) {
		log.Printf("%s does not exist. Trying to import initial root metadata\n", rootPath)
		deviceType := "ci" // or "prod"
		existingRootPath := ""
		for i := 1; i < 100; i++ {
			importRootPath := filepath.Join(provPath, deviceType, strconv.Itoa(i)+".root.json")
			if _, err := os.Stat(importRootPath); errors.Is(err, os.ErrNotExist) {
				log.Printf("%s does not exist\n", importRootPath)
				break
			} else {
				existingRootPath = importRootPath
			}
		}

		if existingRootPath != "" {
			log.Printf("Importing %s", existingRootPath)
			rootPath = existingRootPath
		} else {
			msg := "unable to find initial root metadata"
			log.Println(msg)
			return nil, errors.New(msg)
		}
	}

	rootBytes, err := os.ReadFile(rootPath)
	if err != nil {
		log.Println("os.ReadFile error")
		return nil, err
	}

	// create updater configuration
	cfg, err := config.New(repoUrl, rootBytes) // default config
	if err != nil {
		log.Println("config.New(repoUrl, error")
		return nil, err
	}
	cfg.LocalMetadataDir = localMetadataDir
	cfg.LocalTargetsDir = filepath.Join(localMetadataDir, "download")
	cfg.RemoteTargetsURL = repoUrl
	cfg.PrefixTargetsWithHash = true
	cfg.Fetcher = &FioFetcher{
		client:  client,
		tag:     tag,
		repoUrl: repoUrl,
	}
	return cfg, nil
}

func newFioUpdater(config *sotatoml.AppConfig, client *http.Client, localRepoPath string) (*updater.Updater, error) {
	repoUrl := ""
	if localRepoPath == "" {
		repoUrl = config.GetDefault("tls.server", "https://ota-lite.foundries.io:8443") + "/repo"
	} else {
		if strings.HasPrefix(localRepoPath, "file://") {
			repoUrl = localRepoPath
		} else {
			repoUrl = "file://" + localRepoPath
		}
	}

	if localRepoPath == "" {
		log.Println("Refreshing TUF metadata from device gateway")
	} else {
		log.Println("Refreshing TUF metadata from", repoUrl)
	}

	tag := config.Get("pacman.tags")
	tufCfg, err := getTufCfg(client, repoUrl, tag)
	if err != nil {
		log.Println("failed to create Config instance: ", err)
		return nil, err
	}

	// create a new Updater instance
	up, err := updater.New(tufCfg)
	if err != nil {
		log.Println("failed to create Updater instance: ", err)
		return nil, err
	}
	return up, err
}
