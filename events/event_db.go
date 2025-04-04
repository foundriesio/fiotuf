package events

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"

	_ "github.com/foundriesio/composeapp/pkg/compose"
	_ "github.com/foundriesio/composeapp/pkg/update"
)

func SaveEvent(dbFilePath string, event *DgUpdateEvent) error {
	db, err := sql.Open("sqlite", dbFilePath)
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer db.Close()

	eventJSON, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event to JSON: %v", err)
	}

	_, err = db.Exec("INSERT INTO report_events (json_string) VALUES (?);", string(eventJSON))
	if err != nil {
		return fmt.Errorf("failed to insert event into report_events: %v", err)
	}

	return nil
}

func DeleteEvents(dbFilePath string, maxId int) error {
	db, err := sql.Open("sqlite", dbFilePath)
	if err != nil {
		return fmt.Errorf("failed to open database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec("DELETE FROM report_events WHERE id <= ?;", maxId)
	if err != nil {
		return fmt.Errorf("failed to delete event from report_events: %v", err)
	}

	return nil
}

func GetEvents(dbFilePath string) ([]DgUpdateEvent, int, error) {
	db, err := sql.Open("sqlite", dbFilePath)
	if err != nil {
		return nil, -1, fmt.Errorf("failed to open database: %v", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT id, json_string FROM report_events;")
	if err != nil {
		return nil, -1, fmt.Errorf("failed to select events: %v", err)
	}
	defer rows.Close()

	maxId := -1
	var eventsList []DgUpdateEvent
	for rows.Next() {
		var eventData string
		var id int
		if err := rows.Scan(&id, &eventData); err != nil {
			return nil, -1, fmt.Errorf("failed to scan event data: %v", err)
		}

		var event DgUpdateEvent
		if err := json.Unmarshal([]byte(eventData), &event); err != nil {
			return nil, -1, fmt.Errorf("failed to unmarshal event data: %v", err)
		}

		if maxId < id {
			maxId = id
		}
		eventsList = append(eventsList, event)
	}

	if err := rows.Err(); err != nil {
		return nil, -1, fmt.Errorf("error iterating over rows: %v", err)
	}

	return eventsList, maxId, nil
}
