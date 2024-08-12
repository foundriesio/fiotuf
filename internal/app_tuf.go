package internal

import (
	"errors"
	"fmt"
	"log"
	stdlog "log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/detsch/go-tuf/v2/metadata"
	"github.com/detsch/go-tuf/v2/metadata/config"
	"github.com/detsch/go-tuf/v2/metadata/updater"
	"github.com/go-logr/stdr"
)

var (
	globalApp *App
)

type FioFetcher struct {
	client  *http.Client
	tag     string
	repoUrl string
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
	res, err := httpGet(d.client, urlPath, headers)

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
	// // surpased our set limit.
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

// DownloadFile downloads a file from urlPath, errors out if it failed,
// its length is larger than maxLength or the timeout is reached.
func (d *FioFetcher) DownloadFile(urlPath string, maxLength int64, timeout time.Duration) ([]byte, error) {
	if strings.HasPrefix(urlPath, "file://") {
		return readLocalFile(urlPath[len("file://"):])
	} else {
		return readRemoteFile(d, urlPath, maxLength)
	}
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

var (
	fioUpdater *updater.Updater
	fioClient  *http.Client
)

func refreshTuf(client *http.Client, repoUrl string, tag string) error {
	if repoUrl == "" {
		log.Println("Refreshing TUF metadata from device gateway")
	} else {
		log.Println("Refreshing TUF metadata from", repoUrl)
	}

	cfg, err := getTufCfg(client, repoUrl, tag)
	if err != nil {
		log.Println("failed to create Config instance: ", err)
		return err
	}

	// create a new Updater instance
	up, err := updater.New(cfg)
	if err != nil {
		log.Println("failed to create Updater instance: ", err)
		return err
	}

	// fioUpdater is used to read the current targets data
	fioUpdater = up

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

func (a *App) refreshTufApp(client *http.Client, localRepoPath string) error {
	metadata.SetLogger(stdr.New(stdlog.New(os.Stdout, "", stdlog.LstdFlags)))
	fioClient = client
	var repoUrl string
	tag := a.sota.Get("pacman.tags")
	if localRepoPath == "" {
		repoUrl = strings.Replace(a.configUrl, "/config", "/repo", -1)
	} else {
		if strings.HasPrefix(localRepoPath, "file://") {
			repoUrl = localRepoPath
		} else {
			repoUrl = "file://" + localRepoPath
		}
	}
	ret := refreshTuf(client, repoUrl, tag)
	return ret
}

func DieNotNil(err error, message ...string) {
	if err != nil {
		parts := []interface{}{"ERROR:"}
		for _, p := range message {
			parts = append(parts, p)
		}
		parts = append(parts, err)
		fmt.Println(parts...)
		os.Exit(1)
	}
}

func getTargetsHttp(c *gin.Context) {
	// ret := []string{}
	targets := fioUpdater.GetTopLevelTargets()
	// for name := range targets {
	// 	t, _ := targets[name].MarshalJSON()
	// 	ret = append(ret, string(t))
	// }
	c.IndentedJSON(http.StatusOK, targets)
}

func getRootHttp(c *gin.Context) {
	c.JSON(http.StatusOK, fioUpdater.GetTrustedMetadataSet().Root)
	// c.IndentedJSON(http.StatusOK, fioUpdater.GetTrustedMetadataSet().Root)
}

type tufError struct {
	s string
}

func (f tufError) Error() string {
	return "TUF error"
}

func refreshTufHttp(c *gin.Context) {
	err := globalApp.refreshTufApp(fioClient, c.Query("localTufRepo"))
	if err != nil {
		errAbort := c.AbortWithError(http.StatusBadRequest, tufError{fmt.Sprintf("failed to create Config instance: %v", err)})
		if errAbort != nil {
			log.Println("Error aborting TUF refresh operation request %w", errAbort)
		}

	}
	c.Done()
}

func startHttpServer() {
	// TODO: make port configurable
	port := 9080
	router := gin.Default()
	err := router.SetTrustedProxies([]string{"127.0.0.1"})
	if err != nil {
		log.Println("Error setting gin router trusted proxies: ", err)
		return
	}
	router.GET("/targets", getTargetsHttp)
	router.GET("/root", getRootHttp)
	router.POST("/targets/update/", refreshTufHttp)
	log.Println("Starting TUF agent http server at port", port)
	err = router.Run(":" + strconv.Itoa(port))
	if err != nil {
		log.Println("Error starting gin router: ", err)
	}
}

func (a *App) StartTufAgent() error {
	globalApp = a
	client, crypto := createClient(a.sota)
	// defer crypto.Close()
	a.callInitFunctions(client, crypto)

	err := a.refreshTufApp(client, "")
	if err != nil {
		return err
	}

	startHttpServer()
	return nil
}
