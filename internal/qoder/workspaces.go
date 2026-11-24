package qoder

import (
	"encoding/json"
	"fmt"
)

type Workspace struct {
	Path string
	Name string
}

// ListWorkspaces reads registered local coding roots from the desktop catalog.
func ListWorkspaces(database string) ([]Workspace, error) {
	db, err := openTaskCatalog(database)
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query(`
		SELECT COALESCE(name, ''), root_paths_json
		FROM workspaces
		WHERE workspace_source = 'local' AND product_mode = 'coding'
		  AND archived = 0 AND deleted_at IS NULL
		ORDER BY updated_at DESC, workspace_id`)
	if err != nil {
		return nil, fmt.Errorf("read Qoder workspaces: %w", err)
	}
	defer func() { _ = rows.Close() }()
	result := []Workspace{}
	for rows.Next() {
		var name, raw string
		if err := rows.Scan(&name, &raw); err != nil {
			return nil, err
		}
		var roots []string
		if err := json.Unmarshal([]byte(raw), &roots); err != nil {
			continue
		}
		for _, root := range roots {
			result = append(result, Workspace{Path: root, Name: name})
		}
	}
	return result, rows.Err()
}
