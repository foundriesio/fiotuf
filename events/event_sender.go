package events

import (
	"fmt"
	"log"
	"net/http"

	"strconv"
	"time"

	"github.com/foundriesio/fioconfig/transport"

	"github.com/google/uuid"
)

type EventTypeValue string

const (
	DownloadStarted       EventTypeValue = "EcuDownloadStarted"
	DownloadCompleted     EventTypeValue = "EcuDownloadCompleted"
	InstallationStarted   EventTypeValue = "EcuInstallationStarted"
	InstallationApplied   EventTypeValue = "EcuInstallationApplied"
	InstallationCompleted EventTypeValue = "EcuInstallationCompleted"
)

type DgEvent struct {
	CorrelationId string `json:"correlationId"`
	Success       *bool  `json:"success"`
	TargetName    string `json:"targetName"`
	Version       string `json:"version"`
	Details       string `json:"details,omitempty"`
}
type DgEventType struct {
	Id      EventTypeValue `json:"id"`
	Version int            `json:"version"`
}
type DgUpdateEvent struct {
	Id         string      `json:"id"`
	DeviceTime string      `json:"deviceTime"`
	Event      DgEvent     `json:"event"`
	EventType  DgEventType `json:"eventType"`
}

func NewEvent(eventType EventTypeValue, details string, success *bool, correlationId string, targetName string, version int) []DgUpdateEvent {
	evt := []DgUpdateEvent{
		{
			Id:         uuid.New().String(),
			DeviceTime: time.Now().Format(time.RFC3339),
			Event: DgEvent{
				CorrelationId: correlationId,
				Success:       success,
				TargetName:    targetName,
				Version:       strconv.Itoa(version),
				Details:       details,
			},
			EventType: DgEventType{
				Id:      eventType,
				Version: 0,
			},
		},
	}
	return evt
}

func SendEvent(client *http.Client, urlPath string, event []DgUpdateEvent) error {
	res, err := transport.HttpPost(client, urlPath, event)
	if err != nil {
		log.Printf("Unable to send event: %s", err)
	} else if res.StatusCode < 200 || res.StatusCode > 204 {
		log.Printf("Server could not process event(%s): HTTP_%d - %s", interface{}(event), res.StatusCode, res.String())
	}
	return err
}

func FlushEvents(dbFilePath string, client *http.Client, urlPath string) error {
	evts, maxId, err := GetEvents(dbFilePath)
	if err != nil {
		return fmt.Errorf("error getting events: %v", err)
	}

	if len(evts) == 0 {
		log.Println("No events to send")
		return nil
	}

	err = SendEvent(client, urlPath, evts)
	if err != nil {
		return fmt.Errorf("error sending events: %v", err)
	}
	log.Println("Events:", evts)
	log.Println("maxId:", maxId)

	err = DeleteEvents(dbFilePath, maxId)
	if err != nil {
		return fmt.Errorf("error deleting events: %v", err)
	}
	return nil
}
