package internal

import (
	"fmt"
	"log"
	"net/http"
	"strconv"

	"github.com/detsch/fiotuf/tuf"
	"github.com/foundriesio/fioconfig/sotatoml"
	"github.com/gin-gonic/gin"
)

var (
	globalFioTuf *tuf.FioTuf
)

var Commit string

func getTargetsHttp(c *gin.Context) {
	// ret := []string{}
	targets := globalFioTuf.GetTargets()
	// for name := range targets {
	// 	t, _ := targets[name].MarshalJSON()
	// 	ret = append(ret, string(t))
	// }
	c.IndentedJSON(http.StatusOK, targets)
}

func getRootHttp(c *gin.Context) {
	c.JSON(http.StatusOK, globalFioTuf.GetRoot())
	// c.IndentedJSON(http.StatusOK, fioUpdater.GetTrustedMetadataSet().Root)
}

type tufError struct {
	s string
}

func (f tufError) Error() string {
	return "TUF error"
}

func refreshTufHttp(c *gin.Context) {
	err := globalFioTuf.RefreshTuf(c.Query("localTufRepo"))
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

func StartTufAgent(config *sotatoml.AppConfig) error {
	fiotuf, err := tuf.NewFioTuf(config)
	if err != nil {
		log.Println("Error creating fiotuf: ", err)
		return err
	}

	globalFioTuf = fiotuf
	startHttpServer()
	return nil
}
