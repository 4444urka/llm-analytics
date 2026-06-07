package db

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct {
	conn *sql.DB
}

type Session struct {
	ID           string
	DatasetName  string
	Summary      string
	Instructions string
	Report       string
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Chart struct {
	ID        int
	SessionID string
	Name      string
	Data      []byte
}

func Open(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	conn.SetMaxOpenConns(1)
	conn.SetConnMaxLifetime(0)

	if err := migrate(conn); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return &DB{conn: conn}, nil
}

func migrate(conn *sql.DB) error {
	_, err := conn.Exec(`
		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			dataset_name TEXT NOT NULL,
			summary TEXT NOT NULL DEFAULT '',
			instructions TEXT NOT NULL DEFAULT '',
			report TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'created',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS datasets (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			filename TEXT NOT NULL,
			data BLOB NOT NULL,
			FOREIGN KEY (session_id) REFERENCES sessions(id)
		);

		CREATE TABLE IF NOT EXISTS charts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT NOT NULL,
			name TEXT NOT NULL,
			data BLOB NOT NULL,
			FOREIGN KEY (session_id) REFERENCES sessions(id)
		);

		CREATE INDEX IF NOT EXISTS idx_datasets_session ON datasets(session_id);
		CREATE INDEX IF NOT EXISTS idx_charts_session ON charts(session_id);
	`)
	return err
}

func (d *DB) Close() error {
	return d.conn.Close()
}

func (d *DB) CreateSession(datasetName string, csvData []byte, summary, instructions string) (*Session, error) {
	id, err := generateID()
	if err != nil {
		return nil, err
	}

	tx, err := d.conn.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := time.Now()
	_, err = tx.Exec(
		`INSERT INTO sessions (id, dataset_name, summary, instructions, status, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'created', ?, ?)`,
		id, datasetName, summary, instructions, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert session: %w", err)
	}

	_, err = tx.Exec(
		`INSERT INTO datasets (session_id, filename, data) VALUES (?, ?, ?)`,
		id, datasetName, csvData,
	)
	if err != nil {
		return nil, fmt.Errorf("insert dataset: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &Session{
		ID:           id,
		DatasetName:  datasetName,
		Summary:      summary,
		Instructions: instructions,
		Status:       "created",
		CreatedAt:    now,
		UpdatedAt:    now,
	}, nil
}

func (d *DB) GetSession(id string) (*Session, error) {
	var s Session
	err := d.conn.QueryRow(
		`SELECT id, dataset_name, summary, instructions, report, status, created_at, updated_at
		 FROM sessions WHERE id = ?`, id,
	).Scan(&s.ID, &s.DatasetName, &s.Summary, &s.Instructions, &s.Report, &s.Status, &s.CreatedAt, &s.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return &s, nil
}

func (d *DB) GetDatasetData(sessionID string) ([]byte, string, error) {
	var data []byte
	var filename string
	err := d.conn.QueryRow(
		`SELECT data, filename FROM datasets WHERE session_id = ?`, sessionID,
	).Scan(&data, &filename)
	if err != nil {
		return nil, "", err
	}
	return data, filename, nil
}

func (d *DB) UpdateSessionStatus(id, status string) error {
	_, err := d.conn.Exec(
		`UPDATE sessions SET status = ?, updated_at = ? WHERE id = ?`,
		status, time.Now(), id,
	)
	return err
}

func (d *DB) SaveResults(id, report string) error {
	_, err := d.conn.Exec(
		`UPDATE sessions SET report = ?, status = 'completed', updated_at = ? WHERE id = ?`,
		report, time.Now(), id,
	)
	return err
}

func (d *DB) SaveChart(sessionID, name string, data []byte) error {
	_, err := d.conn.Exec(
		`INSERT INTO charts (session_id, name, data) VALUES (?, ?, ?)`,
		sessionID, name, data,
	)
	return err
}

func (d *DB) GetCharts(sessionID string) ([]Chart, error) {
	rows, err := d.conn.Query(
		`SELECT id, session_id, name, data FROM charts WHERE session_id = ?`, sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var charts []Chart
	for rows.Next() {
		var c Chart
		if err := rows.Scan(&c.ID, &c.SessionID, &c.Name, &c.Data); err != nil {
			return nil, err
		}
		charts = append(charts, c)
	}
	return charts, nil
}

func (d *DB) GetChartData(sessionID, name string) ([]byte, error) {
	var data []byte
	err := d.conn.QueryRow(
		`SELECT data FROM charts WHERE session_id = ? AND name = ?`, sessionID, name,
	).Scan(&data)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func generateID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
