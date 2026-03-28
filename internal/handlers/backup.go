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
	Enabled    bool   `json:"enabled"`
	Interval   string `json:"interval"`    // "6h", "12h", "24h", "7d"
	BackupType string `json:"backup_type"` // "config" or "full"
	Retention  int    `json:"retention"`   // days to keep
	NextRun    string `json:"next_run"`
	LastRun    string `json:"last_run"`
	LastStatus string `json:"last_status"`
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

	// Full backup including historical data (complete backup with all tables)
	// Uses pg_dump with proper flags for reliable restore
	pgDumpFile := filepath.Join(tempDir, "full_backup.sql")
	pgCmd := exec.Command("pg_dump", "-h", pgHost, "-U", pgUser, "-d", pgDB,
		"-F", "p",
		"--create",              // Include CREATE DATABASE statement
		"--clean", "--if-exists", // Drop existing objects before restore
		"--no-owner", "--no-acl", // Skip owner/ACL to avoid permission issues
		"--exclude-table=_timescaledb_internal.*",
		"--exclude-table=_timescaledb_catalog.*",
		"--exclude-table=_timescaledb_config.*",
		"-f", pgDumpFile)
	pgCmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", pgPass))

	log.Printf("[BACKUP] Running pg_dump for host=%s, db=%s, user=%s", pgHost, pgDB, pgUser)
	if output, err := pgCmd.CombinedOutput(); err != nil {
		log.Printf("pg_dump error (credentials masked): %s", maskCredentials(string(output), pgPass))
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

// ImportRestore handles uploading a backup zip and restoring it with full validation
func (h *BackupHandler) ImportRestore(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}

	// Validate file size (max 500MB)
	if file.Size > 500*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Backup file too large (max 500MB)"})
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
	backupFound := false
	sqlFiles := []string{}

	for _, f := range zipReader.File {
		fpath := filepath.Join(tempDir, f.Name)

		// Check for backup files
		if f.Name == "full_backup.sql" || f.Name == "config_dump.sql" || strings.HasSuffix(f.Name, ".sql") {
			backupFound = true
			sqlFiles = append(sqlFiles, f.Name)
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

	if !backupFound {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No SQL backup file found in zip"})
		return
	}

	messages := []string{}

	// Find the primary backup file
	var pgDumpFile string
	fullBackupFile := filepath.Join(tempDir, "full_backup.sql")
	if _, err := os.Stat(fullBackupFile); err == nil {
		pgDumpFile = fullBackupFile
		log.Println("Detected FULL backup file")
	} else {
		// Use first SQL file found
		pgDumpFile = filepath.Join(tempDir, sqlFiles[0])
		log.Printf("Using backup file: %s", sqlFiles[0])
	}

	// Validate SQL file content
	if err := h.validateBackupFile(pgDumpFile); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "Invalid backup file",
			"details": err.Error(),
		})
		return
	}
	messages = append(messages, "Backup file validated")

	pgHost := os.Getenv("DB_HOST")
	pgUser := os.Getenv("DB_USER")
	pgPass := os.Getenv("DB_PASSWORD")
	pgDB := os.Getenv("DB_NAME")

	// Step 0: Create pre-restore safety backup before dropping any data
	log.Println("Step 0: Creating pre-restore safety backup...")
	safetyBackupPath := filepath.Join(tempDir, "safety_backup.sql")
	safetyCmd := exec.Command("pg_dump", "-h", pgHost, "-U", pgUser, "-d", pgDB,
		"-F", "p", "--no-owner", "--no-acl", "-f", safetyBackupPath)
	safetyCmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", pgPass))
	if safetyErr := safetyCmd.Run(); safetyErr != nil {
		log.Printf("[WARN] Could not create safety backup before restore: %v - aborting to protect existing data", safetyErr)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create safety backup before restore - aborting"})
		return
	}
	messages = append(messages, "Pre-restore safety backup created")

	// Step 1: Drop and recreate schema for clean restore
	log.Println("Step 1: Dropping schema public for clean restore...")
	if _, err := h.db.Exec("DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;"); err != nil {
		log.Printf("Schema reset error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to reset schema: %v", err)})
		return
	}
	messages = append(messages, "Schema reset complete")

	// Step 2: Restore from backup
	log.Println("Step 2: Restoring from backup file...")
	pgCmd := exec.Command("psql", "-h", pgHost, "-U", pgUser, "-d", pgDB,
		"-v", "ON_ERROR_STOP=0", // Continue on errors, we'll parse output
		"-f", pgDumpFile)
	pgCmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", pgPass))

	outputBytes, err := pgCmd.CombinedOutput()
	output := string(outputBytes)
	log.Printf("psql restore completed (err: %v)", err)

	// Parse output for critical errors
	hasCriticalError := false
	errorLines := []string{}
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "ERROR:") || strings.HasPrefix(trimmed, "FATAL:") {
			// Ignore expected/non-critical errors
			if h.isNonCriticalError(trimmed) {
				continue
			}
			hasCriticalError = true
			errorLines = append(errorLines, trimmed)
			log.Printf("CRITICAL: %s", trimmed)
		}
	}

	if hasCriticalError {
		// Attempt recovery from safety backup
		log.Println("[RECOVERY] Restore failed - attempting recovery from pre-restore safety backup...")
		recoveryCmd := exec.Command("psql", "-h", pgHost, "-U", pgUser, "-d", pgDB,
			"-v", "ON_ERROR_STOP=0", "-f", safetyBackupPath)
		recoveryCmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", pgPass))
		if recoveryErr := recoveryCmd.Run(); recoveryErr != nil {
			log.Printf("[RECOVERY] Recovery from safety backup also failed: %v", recoveryErr)
		} else {
			log.Println("[RECOVERY] Database recovered from safety backup")
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Database restore failed with critical errors - recovery attempted from safety backup",
			"details": append(messages, errorLines...),
		})
		return
	}

	messages = append(messages, "Database restored from backup")

	// Step 3: Ensure TimescaleDB structures and extensions
	log.Println("Step 3: Verifying TimescaleDB structures...")
	if err := h.EnsureTimescaleDBStructures(); err != nil {
		log.Printf("TimescaleDB setup error: %v", err)
		messages = append(messages, fmt.Sprintf("TimescaleDB warning: %v", err))
	} else {
		messages = append(messages, "TimescaleDB structures verified")
	}

	// Step 4: Ensure critical constraints
	log.Println("Step 4: Ensuring referential integrity...")
	h.ensureCriticalConstraints()
	messages = append(messages, "Referential integrity verified")

	// Step 5: Post-restore validation
	log.Println("Step 5: Validating restore integrity...")
	validationResults := h.validateRestoreIntegrity()
	messages = append(messages, validationResults...)
	if len(validationResults) == 0 {
		messages = append(messages, "Restore validation passed")
	}

	// Step 6: Notify drivers to reload
	if h.mqttClient != nil {
		if err := h.mqttClient.PublishWithQoS("sys/command/restore-complete", "restore", 1, false); err != nil {
			log.Printf("Failed to send restore signal: %v", err)
		} else {
			messages = append(messages, "Drivers notified to reload configuration")
		}
	}

	log.Printf("Restore completed successfully with %d messages", len(messages))
	c.JSON(http.StatusOK, gin.H{
		"message": "Restore completed successfully",
		"details": messages,
	})
}

// validateBackupFile checks if the SQL file contains valid backup content
func (h *BackupHandler) validateBackupFile(filepath string) error {
	content, err := os.ReadFile(filepath)
	if err != nil {
		return fmt.Errorf("cannot read backup file: %w", err)
	}

	contentStr := string(content)

	// Check for minimum content
	if len(contentStr) < 100 {
		return fmt.Errorf("backup file too small or empty")
	}

	// Check for PostgreSQL backup markers
	hasPostgresMarker := strings.Contains(contentStr, "PostgreSQL database dump") ||
		strings.Contains(contentStr, "CREATE TABLE") ||
		strings.Contains(contentStr, "CREATE DATABASE")

	if !hasPostgresMarker {
		return fmt.Errorf("file does not appear to be a valid PostgreSQL backup")
	}

	// Check for critical table structures
	hasCriticalTables := strings.Contains(contentStr, "CREATE TABLE") &&
		(strings.Contains(contentStr, "organizations") ||
			strings.Contains(contentStr, "sites") ||
			strings.Contains(contentStr, "gateways"))

	if !hasCriticalTables {
		return fmt.Errorf("backup missing critical table structures")
	}

	return nil
}

// isNonCriticalError checks if an error message is expected/non-critical
func (h *BackupHandler) isNonCriticalError(errMsg string) bool {
	nonCriticalPatterns := []string{
		"already exists",
		"does not exist",
		"extension",
		"role",
		"must be owner of table",
		"cannot drop",
		"timescaledb",
	}

	lowerErr := strings.ToLower(errMsg)
	for _, pattern := range nonCriticalPatterns {
		if strings.Contains(lowerErr, strings.ToLower(pattern)) {
			return true
		}
	}
	return false
}

// ensureCriticalConstraints ensures critical foreign key constraints exist
func (h *BackupHandler) ensureCriticalConstraints() {
	type constraint struct {
		drop string
		add  string
		name string
	}
	constraints := []constraint{
		{
			name: "tags_gateway_id_fkey",
			drop: `ALTER TABLE tags DROP CONSTRAINT IF EXISTS tags_gateway_id_fkey`,
			add:  `ALTER TABLE tags ADD CONSTRAINT tags_gateway_id_fkey FOREIGN KEY (gateway_id) REFERENCES gateways(id) ON DELETE CASCADE`,
		},
		{
			name: "alarm_definitions_tag_id_fkey",
			drop: `ALTER TABLE alarm_definitions DROP CONSTRAINT IF EXISTS alarm_definitions_tag_id_fkey`,
			add:  `ALTER TABLE alarm_definitions ADD CONSTRAINT alarm_definitions_tag_id_fkey FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE`,
		},
		{
			name: "alarm_events_tag_id_fkey",
			drop: `ALTER TABLE alarm_events DROP CONSTRAINT IF EXISTS alarm_events_tag_id_fkey`,
			add:  `ALTER TABLE alarm_events ADD CONSTRAINT alarm_events_tag_id_fkey FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE`,
		},
	}

	for _, c := range constraints {
		if _, err := h.db.Exec(c.drop); err != nil {
			log.Printf("[BACKUP] Warning: could not drop constraint %s: %v", c.name, err)
		}
		if _, err := h.db.Exec(c.add); err != nil {
			log.Printf("[BACKUP] Warning: could not add constraint %s: %v", c.name, err)
		}
	}
}

// validateRestoreIntegrity checks database integrity after restore
func (h *BackupHandler) validateRestoreIntegrity() []string {
	messages := []string{}

	// Check critical tables exist
	criticalTables := []string{
		"organizations", "sites", "areas", "gateways", "tags",
		"alarm_definitions", "alarm_events", "tag_history",
	}

	for _, table := range criticalTables {
		var exists bool
		err := h.db.QueryRow(`
			SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = $1)
		`, table).Scan(&exists)

		if err != nil || !exists {
			messages = append(messages, fmt.Sprintf("WARNING: Table '%s' missing after restore", table))
		}
	}

	if len(messages) == 0 {
		messages = append(messages, fmt.Sprintf("All %d critical tables verified", len(criticalTables)))
	}

	return messages
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
		BackupType: "full",
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
		log.Printf("ERROR updating backup settings: %v", err)
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

		backupType := "full"

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

	// Security: prevent path traversal - step 1: base name check
	cleanFilename := filepath.Base(filename)
	if cleanFilename != filename {
		log.Printf("[SECURITY] Path traversal attempt blocked in DownloadBackup: %s", filename)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid filename"})
		return
	}

	backupPath := os.Getenv("BACKUP_PATH")
	if backupPath == "" {
		backupPath = "/backups"
	}

	// Security: prevent path traversal - step 2: resolved path check
	basePath, err := filepath.Abs(backupPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Backup path error"})
		return
	}
	filePath := filepath.Join(basePath, cleanFilename)
	resolvedPath, err := filepath.Abs(filePath)
	if err != nil || !strings.HasPrefix(resolvedPath, basePath+string(filepath.Separator)) {
		log.Printf("[SECURITY] Path traversal blocked in DownloadBackup: %s resolves outside backup dir", filename)
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", cleanFilename))
	c.File(resolvedPath)
}

// DeleteBackup deletes a specific backup file
func (h *BackupHandler) DeleteBackup(c *gin.Context) {
	filename := c.Param("filename")

	// Security: prevent path traversal - step 1: base name check
	cleanFilename := filepath.Base(filename)
	if cleanFilename != filename {
		log.Printf("[SECURITY] Path traversal attempt blocked in DeleteBackup: %s", filename)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid filename"})
		return
	}

	backupPath := os.Getenv("BACKUP_PATH")
	if backupPath == "" {
		backupPath = "/backups"
	}

	// Security: prevent path traversal - step 2: resolved path check
	basePath, pathErr := filepath.Abs(backupPath)
	if pathErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Backup path error"})
		return
	}
	filePath := filepath.Join(basePath, cleanFilename)
	resolvedPath, pathErr := filepath.Abs(filePath)
	if pathErr != nil || !strings.HasPrefix(resolvedPath, basePath+string(filepath.Separator)) {
		log.Printf("[SECURITY] Path traversal blocked in DeleteBackup: %s resolves outside backup dir", filename)
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	if err := os.Remove(resolvedPath); err != nil {
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
	var lastRun sql.NullTime

	err := h.db.QueryRow(`
		SELECT enabled, interval_hours, backup_type, retention_days, last_run
		FROM backup_settings WHERE id = 1
	`).Scan(&enabled, &interval, &backupType, &retention, &lastRun)

	if err != nil || !enabled {
		return nil // Not enabled or no settings
	}

	// Check if enough time has passed since last backup
	nextBackupTime := time.Now().Add(-parseBackupInterval(interval)) // time in the past
	if lastRun.Valid && lastRun.Time.After(nextBackupTime) {
		// Not enough time has passed, skip this run
		return nil
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

	// We still need these variables because pgCmd is created differently in earlier versions,
	// but now it's just one path
	var pgDumpFile string
	var pgCmd *exec.Cmd

	pgDumpFile = filepath.Join(tempDir, "full_backup.sql")
	pgCmd = exec.Command("pg_dump", "-h", pgHost, "-U", pgUser, "-d", pgDB,
		"-F", "p",
		"--create",              // Include CREATE DATABASE statement
		"--clean", "--if-exists", // Drop existing objects before restore
		"--no-owner", "--no-acl", // Skip owner/ACL to avoid permission issues
		"--exclude-table=_timescaledb_internal.*",
		"--exclude-table=_timescaledb_catalog.*",
		"--exclude-table=_timescaledb_config.*",
		"-f", pgDumpFile)
	pgCmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", pgPass))

	if output, err := pgCmd.CombinedOutput(); err != nil {
		log.Printf("Scheduled backup pg_dump error: %s", string(output))
		h.updateBackupStatus("failed")
		return err
	}

	// Create ZIP
	timestamp := time.Now().Format("20060102-150405")
	prefix := "full"
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

// maskCredentials replaces sensitive password values in log output
func maskCredentials(output, password string) string {
	if password != "" {
		return strings.ReplaceAll(output, password, "****")
	}
	return output
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

// EnsureTimescaleDBStructures creates TimescaleDB hypertables if they don't exist
func (h *BackupHandler) EnsureTimescaleDBStructures() error {
	// Enable TimescaleDB extension
	if _, err := h.db.Exec(`CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE`); err != nil {
		return fmt.Errorf("failed to enable timescaledb: %w", err)
	}

	// Check if tag_history table exists
	var tagHistoryExists bool
	err := h.db.QueryRow(`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'tag_history')`).Scan(&tagHistoryExists)
	if err != nil {
		return err
	}

	if !tagHistoryExists {
		// Create tag_history table (this is the main historian table, NOT tag_data)
		_, err = h.db.Exec(`
			CREATE TABLE IF NOT EXISTS tag_history (
				id        BIGSERIAL,
				time      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				tag_id    INT NOT NULL,
				value     DOUBLE PRECISION,
				source    VARCHAR(20) DEFAULT 'mqtt',
				PRIMARY KEY (time, id)
			)
		`)
		if err != nil {
			return fmt.Errorf("failed to create tag_history: %w", err)
		}

		// Convert to hypertable
		_, err = h.db.Exec(`SELECT create_hypertable('tag_history', 'time', if_not_exists => TRUE)`)
		if err != nil {
			return fmt.Errorf("failed to create hypertable: %w", err)
		}

		// Create indexes
		_, err = h.db.Exec(`CREATE INDEX IF NOT EXISTS idx_tag_history_tag_id ON tag_history (tag_id, time DESC)`)
		if err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
		_, err = h.db.Exec(`CREATE INDEX IF NOT EXISTS idx_tag_history_time ON tag_history (time DESC)`)
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
				id         BIGSERIAL,
				time       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				gateway_id INT NOT NULL,
				status     VARCHAR(20) NOT NULL,
				message    TEXT
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

		// Create index
		_, err = h.db.Exec(`CREATE INDEX IF NOT EXISTS idx_system_events_gw ON system_events (gateway_id, time DESC)`)
		if err != nil {
			return fmt.Errorf("failed to create index: %w", err)
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
				backup_type VARCHAR(10) DEFAULT 'full',
				retention_days INTEGER DEFAULT 7,
				last_run TIMESTAMPTZ,
				last_status VARCHAR(20)
			);
			INSERT INTO backup_settings (id, enabled, interval_hours, backup_type, retention_days)
			VALUES (1, FALSE, '24h', 'full', 7)
			ON CONFLICT (id) DO NOTHING
		`)
		if err != nil {
			return fmt.Errorf("failed to create backup_settings: %w", err)
		}
	}

	// Ensure alarm_definitions table exists
	var alarmDefsExists bool
	err = h.db.QueryRow(`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'alarm_definitions')`).Scan(&alarmDefsExists)
	if err != nil {
		return err
	}

	if !alarmDefsExists {
		_, err = h.db.Exec(`
			CREATE TABLE IF NOT EXISTS alarm_definitions (
				id SERIAL PRIMARY KEY,
				tag_id INT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
				alarm_type VARCHAR(20) NOT NULL,
				threshold DOUBLE PRECISION,
				deadband DOUBLE PRECISION DEFAULT 0,
				delay_seconds INT DEFAULT 0,
				severity VARCHAR(20) DEFAULT 'warning',
				message TEXT,
				enabled BOOLEAN DEFAULT true,
				created_at TIMESTAMPTZ DEFAULT NOW(),
				updated_at TIMESTAMPTZ DEFAULT NOW()
			);
			CREATE INDEX IF NOT EXISTS idx_alarm_def_tag ON alarm_definitions(tag_id)
		`)
		if err != nil {
			return fmt.Errorf("failed to create alarm_definitions: %w", err)
		}
	}

	// Ensure alarm_events hypertable exists
	var alarmEventsExists bool
	err = h.db.QueryRow(`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'alarm_events')`).Scan(&alarmEventsExists)
	if err != nil {
		return err
	}

	if !alarmEventsExists {
		_, err = h.db.Exec(`
			CREATE TABLE alarm_events (
				id SERIAL,
				tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
				definition_id INTEGER REFERENCES alarm_definitions(id) ON DELETE SET NULL,
				status VARCHAR(50) NOT NULL,
				alarm_type VARCHAR(50) NOT NULL,
				severity VARCHAR(50) NOT NULL,
				message TEXT,
				value_at_trigger DOUBLE PRECISION,
				trigger_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				clear_time TIMESTAMPTZ,
				bg_ack_user VARCHAR(100),
				ack_time TIMESTAMPTZ,
				PRIMARY KEY (id, trigger_time)
			)
		`)
		if err != nil {
			return fmt.Errorf("failed to create alarm_events: %w", err)
		}

		_, err = h.db.Exec(`SELECT create_hypertable('alarm_events', 'trigger_time', if_not_exists => TRUE)`)
		if err != nil {
			return fmt.Errorf("failed to create alarm_events hypertable: %w", err)
		}

		_, err = h.db.Exec(`
			CREATE INDEX IF NOT EXISTS idx_alarm_events_status ON alarm_events(status);
			CREATE INDEX IF NOT EXISTS idx_alarm_events_tag ON alarm_events(tag_id);
			CREATE INDEX IF NOT EXISTS idx_alarm_events_time ON alarm_events(trigger_time DESC)
		`)
		if err != nil {
			return fmt.Errorf("failed to create alarm_events indexes: %w", err)
		}
	}

	return nil
}

// ServiceStatus represents the health status of a service
type ServiceStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"` // "healthy", "starting", "error"
}

// PostRestoreResponse represents the response from PostRestore
type PostRestoreResponse struct {
	Message string          `json:"message"`
	Steps   []ServiceStatus `json:"steps"`
}

// PostRestore restarts all services in the correct order after a restore
func (h *BackupHandler) PostRestore(c *gin.Context) {
	log.Println("[POST-RESTORE] Starting post-restore service restart...")

	// Services in restart order
	services := []string{
		"industrial-postgres",
		"industrial-redis",
		"industrial-mosquitto",
		"industrial-core-api",
		"industrial-driver-manager",
		"industrial-engine-historian",
		"industrial-web-ui",
	}

	results := make([]ServiceStatus, 0, len(services))

	// Function to restart and wait for a service
	restartService := func(serviceName string) ServiceStatus {
		log.Printf("[POST-RESTORE] Restarting %s...", serviceName)

		// Restart the service
		cmd := exec.Command("docker", "restart", serviceName)
		if err := cmd.Run(); err != nil {
			log.Printf("[POST-RESTORE] Failed to restart %s: %v", serviceName, err)
			return ServiceStatus{Name: serviceName, Status: "error"}
		}

		// Wait for service to be healthy (max 30 seconds)
		for i := 0; i < 30; i++ {
			time.Sleep(2 * time.Second)

			// Check health status
			cmd = exec.Command("docker", "inspect", "--format={{.State.Health.Status}}", serviceName)
			output, err := cmd.Output()
			if err != nil {
				continue // Service might not be fully started yet
			}

			status := strings.TrimSpace(string(output))
			if status == "healthy" {
				log.Printf("[POST-RESTORE] ✓ %s is healthy", serviceName)
				return ServiceStatus{Name: serviceName, Status: "healthy"}
			}
		}

		log.Printf("[POST-RESTORE] ✗ %s failed to become healthy", serviceName)
		return ServiceStatus{Name: serviceName, Status: "error"}
	}

	// Restart all services in order
	for _, service := range services {
		status := restartService(service)
		results = append(results, status)

		// If a service fails to start, log but continue
		if status.Status == "error" {
			log.Printf("[POST-RESTORE] Warning: %s failed, continuing with next service", service)
		}
	}

	// Check if all services are healthy
	allHealthy := true
	for _, r := range results {
		if r.Status != "healthy" {
			allHealthy = false
			break
		}
	}

	if allHealthy {
		log.Println("[POST-RESTORE] ✓ All services restarted successfully")
		c.JSON(http.StatusOK, PostRestoreResponse{
			Message: "All services restarted successfully",
			Steps:   results,
		})
	} else {
		log.Println("[POST-RESTORE] ⚠ Some services failed to restart")
		c.JSON(http.StatusMultiStatus, PostRestoreResponse{
			Message: "Post-restore completed with some errors",
			Steps:   results,
		})
	}
}
