package handlers

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type BackupHandler struct {
	db         *sql.DB
	mqttClient MQTTClient
}

func NewBackupHandler(db *sql.DB, mqttClient MQTTClient) *BackupHandler {
	return &BackupHandler{
		db:         db,
		mqttClient: mqttClient,
	}
}

// BackupSettings represents the automatic backup configuration
type BackupSettings struct {
	Enabled     bool   `json:"enabled"`
	Interval    string `json:"interval"`    // "6h", "12h", "24h", "7d"
	BackupType  string `json:"backup_type"` // "config" or "full"
	Retention   int    `json:"retention"`   // days to keep
	NextRun     string `json:"next_run"`
	LastRun     string `json:"last_run"`
	LastStatus  string `json:"last_status"`
}

// ExportBackup creates a full backup (config + historian)
func (h *BackupHandler) ExportBackup(c *gin.Context) {
	tempDir, err := os.MkdirTemp("", "backup-*")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create temp directory"})
		return
	}
	defer os.RemoveAll(tempDir)

	pgHost := os.Getenv("DB_HOST")
	pgUser := os.Getenv("DB_USER")
	pgPass := os.Getenv("DB_PASSWORD")
	pgDB := os.Getenv("DB_NAME")

	// Full backup: everything except TimescaleDB internal tables
	pgDumpFile := filepath.Join(tempDir, "full_backup.sql")
	pgCmd := exec.Command("pg_dump", "-h", pgHost, "-U", pgUser, "-d", pgDB,
		"-F", "p",
		"--exclude-table=_timescaledb_internal.*",
		"--exclude-table=_timescaledb_catalog.*",
		"--exclude-table=_timescaledb_config.*",
		"--exclude-table=tag_history_1m",
		"--exclude-table=tag_history_1h",
		"--exclude-table=tag_history_1d",
		"-f", pgDumpFile)
	pgCmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", pgPass))

	if output, err := pgCmd.CombinedOutput(); err != nil {
		log.Printf("pg_dump error: %s", string(output))
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Database backup failed: %v", err)})
		return
	}

	// Create ZIP
	buf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buf)

	// Add the SQL dump to the zip
	relPath := filepath.Base(pgDumpFile)
	f, err := os.Open(pgDumpFile)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read backup file"})
		return
	}
	defer f.Close()

	w, err := zipWriter.Create(relPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create zip entry"})
		return
	}
	io.Copy(w, f)

	if err := zipWriter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to finalize zip"})
		return
	}

	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("backup-%s.zip", timestamp)

	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Data(http.StatusOK, "application/zip", buf.Bytes())
}

// ImportRestore handles uploading a backup zip and restoring it
func (h *BackupHandler) ImportRestore(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}

	// Save zip to temp
	tempDir, err := os.MkdirTemp("", "restore-*")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create temp dir"})
		return
	}
	defer os.RemoveAll(tempDir)

	zipPath := filepath.Join(tempDir, "restore.zip")
	if err := c.SaveUploadedFile(file, zipPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save upload"})
		return
	}

	// Unzip
	zipReader, err := zip.OpenReader(zipPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid zip file"})
		return
	}
	defer zipReader.Close()

	// Extract everything
	configFound := false

	for _, f := range zipReader.File {
		fpath := filepath.Join(tempDir, f.Name)

		// Check for specific files
		if f.Name == "full_backup.sql" || f.Name == "config_dump.sql" {
			configFound = true
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			continue
		}

		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			outFile.Close()
			continue
		}

		_, err = io.Copy(outFile, rc)

		outFile.Close()
		rc.Close()
	}

	messages := []string{}

	// Restore PostgreSQL (always full restore)
	if configFound {
		// Detect backup file: full_backup.sql vs config_backup.sql
		fullBackupFile := filepath.Join(tempDir, "full_backup.sql")
		configBackupFile := filepath.Join(tempDir, "config_backup.sql")

		var pgDumpFile string

		if _, err := os.Stat(fullBackupFile); err == nil {
			pgDumpFile = fullBackupFile
			log.Println("Detected FULL backup")
		} else if _, err := os.Stat(configBackupFile); err == nil {
			pgDumpFile = configBackupFile
			log.Println("Detected backup file (config_backup.sql)")
		} else {
			// Fallback to old naming
			pgDumpFile = filepath.Join(tempDir, "config_dump.sql")
		}

		pgHost := os.Getenv("DB_HOST")
		pgUser := os.Getenv("DB_USER")
		pgPass := os.Getenv("DB_PASSWORD")
		pgDB := os.Getenv("DB_NAME")

		// ALWAYS do a full restore - wipe everything and restore from backup
		log.Println("Wiping schema public (full restore)...")
		if _, err := h.db.Exec("DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public; GRANT ALL ON SCHEMA public TO public;"); err != nil {
			log.Printf("Schema wipe warning: %v", err)
			messages = append(messages, fmt.Sprintf("Schema wipe warning: %v", err))
		} else {
			messages = append(messages, "Schema public wiped")
		}

		pgCmd := exec.Command("psql", "-h", pgHost, "-U", pgUser, "-d", pgDB, "-v", "ON_ERROR_STOP=0", "-f", pgDumpFile)
		pgCmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", pgPass))

		log.Printf("Running psql restore from %s...", pgDumpFile)
		output, err := pgCmd.CombinedOutput()
		log.Printf("psql output: %s", string(output))

		if err != nil {
			messages = append(messages, fmt.Sprintf("Restore completed with warnings: %v", err))
		} else {
			messages = append(messages, "Database restored successfully")
		}

		// Always try to ensure structures exist after restore
		log.Println("Ensuring TimescaleDB structures exist...")
		if err := h.ensureTimescaleDBStructures(); err != nil {
			log.Printf("TimescaleDB setup warning: %v", err)
			messages = append(messages, fmt.Sprintf("TimescaleDB warning: %v", err))
		} else {
			messages = append(messages, "TimescaleDB structures verified")
		}

		// Re-apply CASCADE constraint for tags
		h.db.Exec(`ALTER TABLE tags DROP CONSTRAINT IF EXISTS tags_gateway_id_fkey`)
		h.db.Exec(`ALTER TABLE tags ADD CONSTRAINT tags_gateway_id_fkey FOREIGN KEY (gateway_id) REFERENCES gateways(id) ON DELETE CASCADE`)

		// Send MQTT signal to driver-manager to reload after restore
		if h.mqttClient != nil {
			if err := h.mqttClient.PublishWithQoS("sys/command/restore-complete", "restore", 1, false); err != nil {
				log.Printf("Failed to send restore signal: %v", err)
			} else {
				log.Println("Sent restore-complete signal to driver-manager")
				messages = append(messages, "Driver manager notified to reload")
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Restore process completed",
		"details": messages,
	})
}

// BackupFileInfo represents a backup file on disk
type BackupFileInfo struct {
	Filename  string `json:"filename"`
	Size      int64  `json:"size"`
	CreatedAt string `json:"created_at"`
	Type      string `json:"type"` // "config" or "full"
}

// GetBackupSettings returns the current automatic backup configuration
func (h *BackupHandler) GetBackupSettings(c *gin.Context) {
	settings := BackupSettings{
		Enabled:    false,
		Interval:   "24h",
		BackupType: "config",
		Retention:  7,
	}

	// Load from database
	var enabled bool
	var interval, backupType string
	var retention int
	var lastRun, lastStatus sql.NullString

	err := h.db.QueryRow(`
		SELECT enabled, interval_hours, backup_type, retention_days, last_run, last_status
		FROM backup_settings WHERE id = 1
	`).Scan(&enabled, &interval, &backupType, &retention, &lastRun, &lastStatus)

	if err == nil {
		settings.Enabled = enabled
		settings.Interval = interval
		settings.BackupType = backupType
		settings.Retention = retention
		if lastRun.Valid {
			settings.LastRun = lastRun.String
		}
		if lastStatus.Valid {
			settings.LastStatus = lastStatus.String
		}
	}

	// Calculate next run
	if settings.Enabled {
		if lastRun.Valid {
			last, err := time.Parse(time.RFC3339, lastRun.String)
			if err == nil {
				next := last.Add(parseBackupInterval(settings.Interval))
				settings.NextRun = next.Format(time.RFC3339)
			}
		} else {
			settings.NextRun = time.Now().Add(parseBackupInterval(settings.Interval)).Format(time.RFC3339)
		}
	}

	c.JSON(http.StatusOK, settings)
}

// UpdateBackupSettings updates the automatic backup configuration
func (h *BackupHandler) UpdateBackupSettings(c *gin.Context) {
	var req BackupSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Upsert settings
	_, err := h.db.Exec(`
		INSERT INTO backup_settings (id, enabled, interval_hours, backup_type, retention_days)
		VALUES (1, $1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE SET
			enabled = $1,
			interval_hours = $2,
			backup_type = $3,
			retention_days = $4
	`, req.Enabled, req.Interval, req.BackupType, req.Retention)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Backup settings updated"})
}

// ListBackups returns list of backup files in the backup directory
func (h *BackupHandler) ListBackups(c *gin.Context) {
	backupPath := os.Getenv("BACKUP_PATH")
	if backupPath == "" {
		backupPath = "/backups"
	}

	files, err := os.ReadDir(backupPath)
	if err != nil {
		c.JSON(http.StatusOK, []BackupFileInfo{})
		return
	}

	var backups []BackupFileInfo
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".zip") {
			continue
		}

		info, err := f.Info()
		if err != nil {
			continue
		}

		backupType := "config"
		if strings.HasPrefix(f.Name(), "full-") {
			backupType = "full"
		}

		backups = append(backups, BackupFileInfo{
			Filename:  f.Name(),
			Size:      info.Size(),
			CreatedAt: info.ModTime().Format(time.RFC3339),
			Type:      backupType,
		})
	}

	// Sort by date descending (newest first)
	for i, j := 0, len(backups)-1; i < j; i, j = i+1, j-1 {
		backups[i], backups[j] = backups[j], backups[i]
	}

	c.JSON(http.StatusOK, backups)
}

// DownloadBackup downloads a specific backup file
func (h *BackupHandler) DownloadBackup(c *gin.Context) {
	filename := c.Param("filename")

	// Security: prevent path traversal
	if strings.Contains(filename, "..") || strings.ContainsAny(filename, "/\\") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid filename"})
		return
	}

	backupPath := os.Getenv("BACKUP_PATH")
	if backupPath == "" {
		backupPath = "/backups"
	}

	filePath := filepath.Join(backupPath, filename)
	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.File(filePath)
}

// DeleteBackup deletes a specific backup file
func (h *BackupHandler) DeleteBackup(c *gin.Context) {
	filename := c.Param("filename")

	// Security: prevent path traversal
	if strings.Contains(filename, "..") || strings.ContainsAny(filename, "/\\") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid filename"})
		return
	}

	backupPath := os.Getenv("BACKUP_PATH")
	if backupPath == "" {
		backupPath = "/backups"
	}

	filePath := filepath.Join(backupPath, filename)
	if err := os.Remove(filePath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Backup deleted"})
}

// RunScheduledBackup executes a backup and saves to disk (called by scheduler)
func (h *BackupHandler) RunScheduledBackup() error {
	// Load settings
	var enabled bool
	var interval, backupType string
	var retention int

	err := h.db.QueryRow(`
		SELECT enabled, interval_hours, backup_type, retention_days
		FROM backup_settings WHERE id = 1
	`).Scan(&enabled, &interval, &backupType, &retention)

	if err != nil || !enabled {
		return nil // Not enabled or no settings
	}

	backupPath := os.Getenv("BACKUP_PATH")
	if backupPath == "" {
		backupPath = "/backups"
	}

	// Ensure backup directory exists
	os.MkdirAll(backupPath, 0755)

	// Create backup
	tempDir, err := os.MkdirTemp("", "backup-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	pgHost := os.Getenv("DB_HOST")
	pgUser := os.Getenv("DB_USER")
	pgPass := os.Getenv("DB_PASSWORD")
	pgDB := os.Getenv("DB_NAME")

	var pgDumpFile string
	var pgCmd *exec.Cmd

	if backupType == "full" {
		pgDumpFile = filepath.Join(tempDir, "full_backup.sql")
		pgCmd = exec.Command("pg_dump", "-h", pgHost, "-U", pgUser, "-d", pgDB,
			"-F", "p", "--clean", "--if-exists", "-f", pgDumpFile)
	} else {
		pgDumpFile = filepath.Join(tempDir, "config_backup.sql")
		pgCmd = exec.Command("pg_dump", "-h", pgHost, "-U", pgUser, "-d", pgDB,
			"-F", "p", "--clean", "--if-exists",
			"--exclude-table=tag_data",
			"--exclude-table=system_events",
			"-f", pgDumpFile)
	}
	pgCmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", pgPass))

	if output, err := pgCmd.CombinedOutput(); err != nil {
		log.Printf("Scheduled backup pg_dump error: %s", string(output))
		h.updateBackupStatus("failed")
		return err
	}

	// Create ZIP
	timestamp := time.Now().Format("20060102-150405")
	prefix := "config"
	if backupType == "full" {
		prefix = "full"
	}
	zipPath := filepath.Join(backupPath, fmt.Sprintf("%s-backup-%s.zip", prefix, timestamp))

	zipFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	srcFile, err := os.Open(pgDumpFile)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	w, _ := zipWriter.Create(filepath.Base(pgDumpFile))
	io.Copy(w, srcFile)
	zipWriter.Close()

	log.Printf("Scheduled backup created: %s", zipPath)
	h.updateBackupStatus("success")

	// Cleanup old backups
	h.cleanupOldBackups(backupPath, retention)

	return nil
}

// cleanupOldBackups removes backups older than retention days
func (h *BackupHandler) cleanupOldBackups(backupPath string, retentionDays int) {
	files, err := os.ReadDir(backupPath)
	if err != nil {
		return
	}

	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		info, err := f.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			filePath := filepath.Join(backupPath, f.Name())
			os.Remove(filePath)
			log.Printf("Deleted old backup: %s", f.Name())
		}
	}
}

// updateBackupStatus updates the last_run and last_status in database
func (h *BackupHandler) updateBackupStatus(status string) {
	now := time.Now().Format(time.RFC3339)
	h.db.Exec(`
		UPDATE backup_settings
		SET last_run = $1, last_status = $2
		WHERE id = 1
	`, now, status)
}

// parseBackupInterval converts interval string to duration
func parseBackupInterval(s string) time.Duration {
	switch s {
	case "6h":
		return 6 * time.Hour
	case "12h":
		return 12 * time.Hour
	case "24h":
		return 24 * time.Hour
	case "7d":
		return 7 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// ensureTimescaleDBStructures creates TimescaleDB hypertables if they don't exist
func (h *BackupHandler) ensureTimescaleDBStructures() error {
	// Enable TimescaleDB extension
	if _, err := h.db.Exec(`CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE`); err != nil {
		return fmt.Errorf("failed to enable timescaledb: %w", err)
	}

	// Check if tag_data table exists
	var tagDataExists bool
	err := h.db.QueryRow(`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'tag_data')`).Scan(&tagDataExists)
	if err != nil {
		return err
	}

	if !tagDataExists {
		// Create tag_data table
		_, err = h.db.Exec(`
			CREATE TABLE IF NOT EXISTS tag_data (
				time        TIMESTAMPTZ NOT NULL,
				tag_id      INT NOT NULL,
				value       DOUBLE PRECISION,
				quality     INT DEFAULT 0,
				PRIMARY KEY (time, tag_id)
			)
		`)
		if err != nil {
			return fmt.Errorf("failed to create tag_data: %w", err)
		}

		// Convert to hypertable
		_, err = h.db.Exec(`SELECT create_hypertable('tag_data', 'time', if_not_exists => TRUE)`)
		if err != nil {
			return fmt.Errorf("failed to create hypertable: %w", err)
		}

		// Create index
		_, err = h.db.Exec(`CREATE INDEX IF NOT EXISTS idx_tag_data_tag_id ON tag_data (tag_id, time DESC)`)
		if err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	// Check if system_events table exists
	var systemEventsExists bool
	err = h.db.QueryRow(`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'system_events')`).Scan(&systemEventsExists)
	if err != nil {
		return err
	}

	if !systemEventsExists {
		// Create system_events table
		_, err = h.db.Exec(`
			CREATE TABLE IF NOT EXISTS system_events (
				time        TIMESTAMPTZ NOT NULL,
				event_type  VARCHAR(50) NOT NULL,
				source      VARCHAR(100),
				message     TEXT,
				severity    VARCHAR(20) DEFAULT 'info',
				PRIMARY KEY (time, event_type)
			)
		`)
		if err != nil {
			return fmt.Errorf("failed to create system_events: %w", err)
		}

		// Convert to hypertable
		_, err = h.db.Exec(`SELECT create_hypertable('system_events', 'time', if_not_exists => TRUE)`)
		if err != nil {
			return fmt.Errorf("failed to create system_events hypertable: %w", err)
		}
	}

	// Ensure backup_settings table exists
	var backupSettingsExists bool
	err = h.db.QueryRow(`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'backup_settings')`).Scan(&backupSettingsExists)
	if err != nil {
		return err
	}

	if !backupSettingsExists {
		_, err = h.db.Exec(`
			CREATE TABLE IF NOT EXISTS backup_settings (
				id INTEGER PRIMARY KEY DEFAULT 1,
				enabled BOOLEAN DEFAULT FALSE,
				interval_hours VARCHAR(10) DEFAULT '24h',
				backup_type VARCHAR(10) DEFAULT 'config',
				retention_days INTEGER DEFAULT 7,
				last_run TIMESTAMPTZ,
				last_status VARCHAR(20)
			);
			INSERT INTO backup_settings (id, enabled, interval_hours, backup_type, retention_days)
			VALUES (1, FALSE, '24h', 'config', 7)
			ON CONFLICT (id) DO NOTHING
		`)
		if err != nil {
			return fmt.Errorf("failed to create backup_settings: %w", err)
		}
	}

	return nil
}
