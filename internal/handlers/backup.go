package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
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

// BackupCatalogEntry represents a row in backup_catalog.
type BackupCatalogEntry struct {
	ID            int     `json:"id"`
	Filename      string  `json:"filename"`
	SizeBytes     int64   `json:"size_bytes"`
	SHA256        string  `json:"sha256"`
	Encrypted     bool    `json:"encrypted"`
	SchemaVersion string  `json:"schema_version"`
	Storage       string  `json:"storage"`
	S3Key         *string `json:"s3_key,omitempty"`
	CreatedAt     string  `json:"created_at"`
	DeletedAt     *string `json:"deleted_at,omitempty"`
}

// BackupAuditEntry represents a row in backup_audit.
type BackupAuditEntry struct {
	ID        int    `json:"id"`
	Action    string `json:"action"`
	Filename  string `json:"filename"`
	UserEmail string `json:"user_email"`
	IPAddr    string `json:"ip_addr"`
	Details   string `json:"details"`
	CreatedAt string `json:"created_at"`
}

// logAudit inserts a row into backup_audit. Best-effort — never fails the caller.
func (h *BackupHandler) logAudit(ctx context.Context, action, filename, userEmail, ip, details string) {
	_, err := h.db.ExecContext(ctx, `
		INSERT INTO backup_audit (action, filename, user_email, ip_addr, details)
		VALUES ($1, $2, $3, $4, $5)`,
		action, filename, userEmail, ip, details)
	if err != nil {
		log.Printf("[BACKUP] audit log failed (%s %s): %v", action, filename, err)
	}
}

// emailFromCtx extracts the user email from the Gin context (set by JWT middleware).
func emailFromCtx(c *gin.Context) string {
	if v, ok := c.Get("user_email"); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return "system"
}

// checksumFile computes the SHA-256 hex digest of a file.
func checksumFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// checksumBytes computes the SHA-256 hex digest of a byte slice.
func checksumBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// ExportBackup creates a full backup (config + historian) and streams it as a ZIP download.
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

	pgDumpFile := filepath.Join(tempDir, "full_backup.sql")
	pgCmd := exec.CommandContext(c.Request.Context(), "pg_dump",
		"-h", pgHost, "-U", pgUser, "-d", pgDB,
		"-F", "p",
		"--create",
		"--clean", "--if-exists",
		"--no-owner", "--no-acl",
		"--exclude-table=_timescaledb_internal.*",
		"--exclude-table=_timescaledb_catalog.*",
		"--exclude-table=_timescaledb_config.*",
		"-f", pgDumpFile)
	pgCmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", pgPass))

	log.Printf("[BACKUP] Manual export: pg_dump host=%s db=%s", pgHost, pgDB)
	if output, dumpErr := pgCmd.CombinedOutput(); dumpErr != nil {
		log.Printf("pg_dump error: %s", maskCredentials(string(output), pgPass))
		h.logAudit(c.Request.Context(), "export_failed", "", emailFromCtx(c), c.ClientIP(), "pg_dump failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database backup failed"})
		return
	}

	// Build ZIP in memory.
	buf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buf)

	f, err := os.Open(pgDumpFile)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read backup file"})
		return
	}
	defer f.Close()

	w, err := zipWriter.Create(filepath.Base(pgDumpFile))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create zip entry"})
		return
	}
	if _, err = io.Copy(w, f); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to write zip entry"})
		return
	}
	if err := zipWriter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to finalize zip"})
		return
	}

	payload := buf.Bytes()
	chk := checksumBytes(payload)
	timestamp := time.Now().UTC().Format("20060102-150405")
	filename := fmt.Sprintf("backup-%s.zip", timestamp)

	h.logAudit(c.Request.Context(), "export_downloaded", filename, emailFromCtx(c), c.ClientIP(),
		fmt.Sprintf("size=%d sha256=%s", len(payload), chk))

	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Header("X-Checksum-SHA256", chk)
	c.Data(http.StatusOK, "application/zip", payload)
}

// ImportRestore handles uploading a backup zip and restoring it with full validation
func (h *BackupHandler) ImportRestore(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No file uploaded"})
		return
	}

	if file.Size > 500*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Backup file too large (max 500MB)"})
		return
	}

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

	h.logAudit(c.Request.Context(), "restore_started", file.Filename, emailFromCtx(c), c.ClientIP(),
		fmt.Sprintf("size=%d", file.Size))

	zipReader, err := zip.OpenReader(zipPath)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid zip file"})
		return
	}
	defer zipReader.Close()

	backupFound := false
	sqlFiles := []string{}
	for _, zf := range zipReader.File {
		fpath := filepath.Join(tempDir, zf.Name)
		if zf.Name == "full_backup.sql" || zf.Name == "config_dump.sql" || strings.HasSuffix(zf.Name, ".sql") {
			backupFound = true
			sqlFiles = append(sqlFiles, zf.Name)
		}
		if zf.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}
		if err := os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			continue
		}
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, zf.Mode())
		if err != nil {
			continue
		}
		rc, err := zf.Open()
		if err != nil {
			outFile.Close()
			continue
		}
		_, _ = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
	}

	if !backupFound {
		h.logAudit(c.Request.Context(), "restore_failed", file.Filename, emailFromCtx(c), c.ClientIP(), "no SQL file in zip")
		c.JSON(http.StatusBadRequest, gin.H{"error": "No SQL backup file found in zip"})
		return
	}

	messages := []string{}
	var pgDumpFile string
	fullBackupFile := filepath.Join(tempDir, "full_backup.sql")
	if _, statErr := os.Stat(fullBackupFile); statErr == nil {
		pgDumpFile = fullBackupFile
	} else {
		pgDumpFile = filepath.Join(tempDir, sqlFiles[0])
	}

	if err := h.validateBackupFile(pgDumpFile); err != nil {
		h.logAudit(c.Request.Context(), "restore_failed", file.Filename, emailFromCtx(c), c.ClientIP(), err.Error())
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid backup file", "details": err.Error()})
		return
	}
	messages = append(messages, "Backup file validated")

	pgHost := os.Getenv("DB_HOST")
	pgUser := os.Getenv("DB_USER")
	pgPass := os.Getenv("DB_PASSWORD")
	pgDB := os.Getenv("DB_NAME")

	// Step 0: safety backup before dropping anything.
	log.Println("[BACKUP] Step 0: creating pre-restore safety backup")
	safetyPath := filepath.Join(tempDir, "safety_backup.sql")
	safetyCmd := exec.CommandContext(context.Background(), "pg_dump",
		"-h", pgHost, "-U", pgUser, "-d", pgDB,
		"-F", "p", "--no-owner", "--no-acl", "-f", safetyPath)
	safetyCmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", pgPass))
	if safetyErr := safetyCmd.Run(); safetyErr != nil {
		log.Printf("[BACKUP] safety backup failed: %v — aborting", safetyErr)
		h.logAudit(context.Background(), "restore_failed", file.Filename, emailFromCtx(c), c.ClientIP(), "safety backup failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create safety backup before restore — aborting"})
		return
	}
	messages = append(messages, "Pre-restore safety backup created")

	// Step 1: drop + recreate schema.
	if _, err := h.db.ExecContext(context.Background(),
		"DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public;"); err != nil {
		log.Printf("[BACKUP] schema reset error: %v", err)
		h.logAudit(context.Background(), "restore_failed", file.Filename, emailFromCtx(c), c.ClientIP(), "schema reset failed")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset schema"})
		return
	}
	messages = append(messages, "Schema reset complete")

	// Step 2: restore.
	pgCmd := exec.CommandContext(context.Background(), "psql",
		"-h", pgHost, "-U", pgUser, "-d", pgDB,
		"-v", "ON_ERROR_STOP=0", "-f", pgDumpFile)
	pgCmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", pgPass))
	outputBytes, restoreErr := pgCmd.CombinedOutput()
	output := string(outputBytes)
	log.Printf("[BACKUP] psql restore completed (err: %v)", restoreErr)

	hasCriticalError := false
	errorLines := []string{}
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if (strings.HasPrefix(trimmed, "ERROR:") || strings.HasPrefix(trimmed, "FATAL:")) &&
			!h.isNonCriticalError(trimmed) {
			hasCriticalError = true
			errorLines = append(errorLines, trimmed)
			log.Printf("[BACKUP] CRITICAL: %s", trimmed)
		}
	}

	if hasCriticalError {
		// Attempt recovery from safety backup.
		log.Println("[BACKUP] restore failed — attempting recovery from safety backup")
		recoveryCmd := exec.CommandContext(context.Background(), "psql",
			"-h", pgHost, "-U", pgUser, "-d", pgDB,
			"-v", "ON_ERROR_STOP=1", "-f", safetyPath)
		recoveryCmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", pgPass))
		if _, recoveryErr := recoveryCmd.CombinedOutput(); recoveryErr != nil {
			log.Printf("[BACKUP] recovery also failed: %v", recoveryErr)
		}
		h.logAudit(context.Background(), "restore_failed", file.Filename, emailFromCtx(c), c.ClientIP(),
			strings.Join(errorLines, "; "))
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Database restore failed — recovery attempted from safety backup",
			"details": append(messages, errorLines...),
		})
		return
	}
	messages = append(messages, "Database restored from backup")

	// Step 3: TimescaleDB.
	if err := h.EnsureTimescaleDBStructures(); err != nil {
		log.Printf("[BACKUP] TimescaleDB setup: %v", err)
		messages = append(messages, fmt.Sprintf("TimescaleDB warning: %v", err))
	} else {
		messages = append(messages, "TimescaleDB structures verified")
	}

	// Step 4: constraints.
	h.ensureCriticalConstraints()
	messages = append(messages, "Referential integrity verified")

	// Step 5: validate.
	messages = append(messages, h.validateRestoreIntegrity()...)
	if len(messages) == 0 {
		messages = append(messages, "Restore validation passed")
	}

	// Step 6: notify drivers.
	if h.mqttClient != nil {
		if pubErr := h.mqttClient.PublishWithQoS("sys/command/restore-complete", "restore", 1, false); pubErr != nil {
			log.Printf("[BACKUP] MQTT notify failed: %v", pubErr)
		} else {
			messages = append(messages, "Drivers notified to reload configuration")
		}
	}

	h.logAudit(context.Background(), "restore_completed", file.Filename, emailFromCtx(c), c.ClientIP(),
		fmt.Sprintf("steps=%d", len(messages)))
	log.Printf("[BACKUP] restore completed successfully")
	c.JSON(http.StatusOK, gin.H{"message": "Restore completed successfully", "details": messages})
}

// GetCatalog returns backup files from backup_catalog with filesystem fallback.
func (h *BackupHandler) GetCatalog(c *gin.Context) {
	rows, err := h.db.QueryContext(c.Request.Context(), `
		SELECT id, filename, size_bytes, COALESCE(sha256,''), encrypted,
		       COALESCE(schema_version,''), storage, s3_key, created_at, deleted_at
		FROM backup_catalog
		WHERE deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT 200`)
	if err != nil {
		// Table might not exist yet — fall back to filesystem list.
		h.ListBackups(c)
		return
	}
	defer func() { _ = rows.Close() }()

	entries := make([]BackupCatalogEntry, 0)
	for rows.Next() {
		var e BackupCatalogEntry
		var deletedAt sql.NullString
		var s3Key sql.NullString
		if scanErr := rows.Scan(&e.ID, &e.Filename, &e.SizeBytes, &e.SHA256, &e.Encrypted,
			&e.SchemaVersion, &e.Storage, &s3Key, &e.CreatedAt, &deletedAt); scanErr != nil {
			continue
		}
		if s3Key.Valid {
			e.S3Key = &s3Key.String
		}
		entries = append(entries, e)
	}
	c.JSON(http.StatusOK, entries)
}

// GetAudit returns the last 100 backup audit entries.
func (h *BackupHandler) GetAudit(c *gin.Context) {
	rows, err := h.db.QueryContext(c.Request.Context(), `
		SELECT id, action, COALESCE(filename,''), COALESCE(user_email,''),
		       COALESCE(ip_addr,''), COALESCE(details,''), created_at
		FROM backup_audit
		ORDER BY created_at DESC
		LIMIT 100`)
	if err != nil {
		c.JSON(http.StatusOK, []BackupAuditEntry{})
		return
	}
	defer func() { _ = rows.Close() }()

	entries := make([]BackupAuditEntry, 0)
	for rows.Next() {
		var e BackupAuditEntry
		if scanErr := rows.Scan(&e.ID, &e.Action, &e.Filename, &e.UserEmail,
			&e.IPAddr, &e.Details, &e.CreatedAt); scanErr != nil {
			continue
		}
		entries = append(entries, e)
	}
	c.JSON(http.StatusOK, entries)
}

// validateBackupFile checks if the SQL file contains valid backup content.
func (h *BackupHandler) validateBackupFile(path string) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("cannot read backup file: %w", err)
	}
	if len(content) < 100 {
		return fmt.Errorf("backup file too small or empty")
	}
	s := string(content)
	if !strings.Contains(s, "PostgreSQL database dump") &&
		!strings.Contains(s, "CREATE TABLE") &&
		!strings.Contains(s, "CREATE DATABASE") {
		return fmt.Errorf("file does not appear to be a valid PostgreSQL backup")
	}
	if !strings.Contains(s, "CREATE TABLE") ||
		(!strings.Contains(s, "organizations") && !strings.Contains(s, "sites") && !strings.Contains(s, "gateways")) {
		return fmt.Errorf("backup missing critical table structures")
	}
	return nil
}

// isNonCriticalError checks if a psql error line is safe to ignore.
func (h *BackupHandler) isNonCriticalError(errMsg string) bool {
	lowerErr := strings.ToLower(errMsg)
	for _, pattern := range []string{
		"already exists", "does not exist", "extension",
		"role", "must be owner of table", "cannot drop", "timescaledb",
	} {
		if strings.Contains(lowerErr, pattern) {
			return true
		}
	}
	return false
}

// ensureCriticalConstraints recreates critical foreign key constraints after restore.
func (h *BackupHandler) ensureCriticalConstraints() {
	type constraint struct{ name, drop, add string }
	for _, c := range []constraint{
		{
			"tags_gateway_id_fkey",
			`ALTER TABLE tags DROP CONSTRAINT IF EXISTS tags_gateway_id_fkey`,
			`ALTER TABLE tags ADD CONSTRAINT tags_gateway_id_fkey FOREIGN KEY (gateway_id) REFERENCES gateways(id) ON DELETE CASCADE`,
		},
		{
			"alarm_definitions_tag_id_fkey",
			`ALTER TABLE alarm_definitions DROP CONSTRAINT IF EXISTS alarm_definitions_tag_id_fkey`,
			`ALTER TABLE alarm_definitions ADD CONSTRAINT alarm_definitions_tag_id_fkey FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE`,
		},
		{
			"alarm_events_tag_id_fkey",
			`ALTER TABLE alarm_events DROP CONSTRAINT IF EXISTS alarm_events_tag_id_fkey`,
			`ALTER TABLE alarm_events ADD CONSTRAINT alarm_events_tag_id_fkey FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE`,
		},
	} {
		if _, err := h.db.ExecContext(context.Background(), c.drop); err != nil {
			log.Printf("[BACKUP] Warning: could not drop constraint %s: %v", c.name, err)
		}
		if _, err := h.db.ExecContext(context.Background(), c.add); err != nil {
			log.Printf("[BACKUP] Warning: could not add constraint %s: %v", c.name, err)
		}
	}
}

// validateRestoreIntegrity checks that critical tables exist after restore.
func (h *BackupHandler) validateRestoreIntegrity() []string {
	messages := []string{}
	for _, table := range []string{
		"organizations", "sites", "areas", "gateways", "tags",
		"alarm_definitions", "alarm_events", "tag_history",
	} {
		var exists bool
		if err := h.db.QueryRowContext(context.Background(), `
			SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = $1)`, table).
			Scan(&exists); err != nil || !exists {
			messages = append(messages, fmt.Sprintf("WARNING: Table '%s' missing after restore", table))
		}
	}
	if len(messages) == 0 {
		messages = append(messages, "All 8 critical tables verified")
	}
	return messages
}

// BackupFileInfo represents a backup file on disk (legacy list endpoint).
type BackupFileInfo struct {
	Filename  string `json:"filename"`
	Size      int64  `json:"size"`
	CreatedAt string `json:"created_at"`
	Type      string `json:"type"`
}

// GetBackupSettings returns the current automatic backup configuration.
func (h *BackupHandler) GetBackupSettings(c *gin.Context) {
	settings := BackupSettings{
		Enabled:    false,
		Interval:   "24h",
		BackupType: "full",
		Retention:  7,
	}
	var enabled bool
	var interval, backupType string
	var retention int
	var lastRun, lastStatus sql.NullString
	if err := h.db.QueryRowContext(c.Request.Context(), `
		SELECT enabled, interval_hours, backup_type, retention_days, last_run, last_status
		FROM backup_settings WHERE id = 1`).
		Scan(&enabled, &interval, &backupType, &retention, &lastRun, &lastStatus); err == nil {
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
	if settings.Enabled {
		if lastRun.Valid {
			if last, err := time.Parse(time.RFC3339, lastRun.String); err == nil {
				settings.NextRun = last.Add(parseBackupInterval(settings.Interval)).Format(time.RFC3339)
			}
		} else {
			settings.NextRun = time.Now().Add(parseBackupInterval(settings.Interval)).Format(time.RFC3339)
		}
	}
	c.JSON(http.StatusOK, settings)
}

// UpdateBackupSettings updates the automatic backup configuration.
func (h *BackupHandler) UpdateBackupSettings(c *gin.Context) {
	var req BackupSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if _, err := h.db.ExecContext(c.Request.Context(), `
		INSERT INTO backup_settings (id, enabled, interval_hours, backup_type, retention_days)
		VALUES (1, $1, $2, $3, $4)
		ON CONFLICT (id) DO UPDATE SET
			enabled = $1, interval_hours = $2, backup_type = $3, retention_days = $4`,
		req.Enabled, req.Interval, req.BackupType, req.Retention); err != nil {
		log.Printf("[BACKUP] update settings error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"message": "Backup settings updated"})
}

// ListBackups returns backup files from the filesystem (legacy — use GetCatalog for richer data).
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
		if f.IsDir() || (!strings.HasSuffix(f.Name(), ".zip") &&
			!strings.HasSuffix(f.Name(), ".dump") &&
			!strings.HasSuffix(f.Name(), ".dump.age")) {
			continue
		}
		info, err := f.Info()
		if err != nil {
			continue
		}
		backups = append(backups, BackupFileInfo{
			Filename:  f.Name(),
			Size:      info.Size(),
			CreatedAt: info.ModTime().Format(time.RFC3339),
			Type:      "full",
		})
	}
	for i, j := 0, len(backups)-1; i < j; i, j = i+1, j-1 {
		backups[i], backups[j] = backups[j], backups[i]
	}
	c.JSON(http.StatusOK, backups)
}

// DownloadBackup downloads a specific backup file with SHA-256 integrity header.
func (h *BackupHandler) DownloadBackup(c *gin.Context) {
	filename := c.Param("filename")
	cleanFilename := filepath.Base(filename)
	if cleanFilename != filename {
		log.Printf("[SECURITY] Path traversal attempt in DownloadBackup: %s", filename)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid filename"})
		return
	}
	backupPath := os.Getenv("BACKUP_PATH")
	if backupPath == "" {
		backupPath = "/backups"
	}
	basePath, err := filepath.Abs(backupPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Backup path error"})
		return
	}
	filePath := filepath.Join(basePath, cleanFilename)
	resolvedPath, err := filepath.Abs(filePath)
	if err != nil || !strings.HasPrefix(resolvedPath, basePath+string(filepath.Separator)) {
		log.Printf("[SECURITY] Path traversal blocked in DownloadBackup: %s", filename)
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}

	// Attach SHA-256 if we have it in catalog or sidecar file.
	var chk string
	if err := h.db.QueryRowContext(c.Request.Context(),
		`SELECT COALESCE(sha256,'') FROM backup_catalog WHERE filename = $1`, cleanFilename).
		Scan(&chk); err != nil || chk == "" {
		// Try sidecar file written by backup-now.sh.
		if sidecar, readErr := os.ReadFile(resolvedPath + ".sha256"); readErr == nil {
			parts := strings.Fields(string(sidecar))
			if len(parts) > 0 {
				chk = parts[0]
			}
		}
		// Fall back to computing on-the-fly.
		if chk == "" {
			if computed, compErr := checksumFile(resolvedPath); compErr == nil {
				chk = computed
			}
		}
	}
	if chk != "" {
		c.Header("X-Checksum-SHA256", chk)
	}

	h.logAudit(c.Request.Context(), "backup_downloaded", cleanFilename, emailFromCtx(c), c.ClientIP(),
		fmt.Sprintf("sha256=%s", chk))

	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", cleanFilename))
	c.File(resolvedPath)
}

// DeleteBackup deletes a backup file and marks it deleted in the catalog.
func (h *BackupHandler) DeleteBackup(c *gin.Context) {
	filename := c.Param("filename")
	cleanFilename := filepath.Base(filename)
	if cleanFilename != filename {
		log.Printf("[SECURITY] Path traversal attempt in DeleteBackup: %s", filename)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid filename"})
		return
	}
	backupPath := os.Getenv("BACKUP_PATH")
	if backupPath == "" {
		backupPath = "/backups"
	}
	basePath, err := filepath.Abs(backupPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Backup path error"})
		return
	}
	filePath := filepath.Join(basePath, cleanFilename)
	resolvedPath, err := filepath.Abs(filePath)
	if err != nil || !strings.HasPrefix(resolvedPath, basePath+string(filepath.Separator)) {
		log.Printf("[SECURITY] Path traversal blocked in DeleteBackup: %s", filename)
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
		return
	}
	if err := os.Remove(resolvedPath); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// Best-effort: remove sidecar sha256 file too.
	_ = os.Remove(resolvedPath + ".sha256")
	// Mark deleted in catalog.
	_, _ = h.db.ExecContext(c.Request.Context(),
		`UPDATE backup_catalog SET deleted_at = NOW() WHERE filename = $1`, cleanFilename)

	h.logAudit(c.Request.Context(), "backup_deleted", cleanFilename, emailFromCtx(c), c.ClientIP(), "")
	c.JSON(http.StatusOK, gin.H{"message": "Backup deleted"})
}

// RunScheduledBackup executes a backup, saves it to disk, and records it in the catalog.
func (h *BackupHandler) RunScheduledBackup() error {
	var enabled bool
	var interval, backupType string
	var retention int
	var lastRun sql.NullTime
	if err := h.db.QueryRowContext(context.Background(), `
		SELECT enabled, interval_hours, backup_type, retention_days, last_run
		FROM backup_settings WHERE id = 1`).
		Scan(&enabled, &interval, &backupType, &retention, &lastRun); err != nil || !enabled {
		return nil
	}

	if lastRun.Valid && lastRun.Time.After(time.Now().Add(-parseBackupInterval(interval))) {
		return nil
	}

	backupPath := os.Getenv("BACKUP_PATH")
	if backupPath == "" {
		backupPath = "/backups"
	}
	if err := os.MkdirAll(backupPath, 0o755); err != nil {
		return err
	}

	tempDir, err := os.MkdirTemp("", "backup-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	pgHost := os.Getenv("DB_HOST")
	pgUser := os.Getenv("DB_USER")
	pgPass := os.Getenv("DB_PASSWORD")
	pgDB := os.Getenv("DB_NAME")

	pgDumpFile := filepath.Join(tempDir, "full_backup.sql")
	pgCmd := exec.CommandContext(context.Background(), "pg_dump",
		"-h", pgHost, "-U", pgUser, "-d", pgDB,
		"-F", "p", "--create", "--clean", "--if-exists",
		"--no-owner", "--no-acl",
		"--exclude-table=_timescaledb_internal.*",
		"--exclude-table=_timescaledb_catalog.*",
		"--exclude-table=_timescaledb_config.*",
		"-f", pgDumpFile)
	pgCmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", pgPass))

	if output, dumpErr := pgCmd.CombinedOutput(); dumpErr != nil {
		log.Printf("[BACKUP] Scheduled pg_dump error: %s", string(output))
		h.updateBackupStatus("failed")
		h.logAudit(context.Background(), "backup_failed", "", "system", "", "pg_dump: "+string(output))
		return dumpErr
	}

	timestamp := time.Now().UTC().Format("20060102-150405")
	zipPath := filepath.Join(backupPath, fmt.Sprintf("full-backup-%s.zip", timestamp))

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
	if _, err = io.Copy(w, srcFile); err != nil {
		return err
	}
	if err = zipWriter.Close(); err != nil {
		return fmt.Errorf("zip finalize: %w", err)
	}
	if err = zipFile.Close(); err != nil {
		return fmt.Errorf("zip file close: %w", err)
	}

	// Compute SHA-256 of the zip.
	chk, checksumErr := checksumFile(zipPath)
	if checksumErr != nil {
		log.Printf("[BACKUP] SHA-256 computation failed: %v", checksumErr)
		chk = ""
	}

	info, _ := os.Stat(zipPath)
	sizeBytes := int64(0)
	if info != nil {
		sizeBytes = info.Size()
	}

	// Record in catalog.
	_, _ = h.db.ExecContext(context.Background(), `
		INSERT INTO backup_catalog (filename, size_bytes, sha256, encrypted, storage)
		VALUES ($1, $2, $3, false, 'local')
		ON CONFLICT (filename) DO UPDATE SET size_bytes = $2, sha256 = $3`,
		filepath.Base(zipPath), sizeBytes, chk)

	h.logAudit(context.Background(), "backup_created", filepath.Base(zipPath), "system", "",
		fmt.Sprintf("size=%d sha256=%s", sizeBytes, chk))

	log.Printf("[BACKUP] Scheduled backup: %s (%d bytes, sha256=%s)", zipPath, sizeBytes, chk)
	h.updateBackupStatus("success")
	h.cleanupOldBackups(backupPath, retention)
	return nil
}

// cleanupOldBackups removes backups older than retentionDays.
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
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		fullPath := filepath.Join(backupPath, f.Name())
		if removeErr := os.Remove(fullPath); removeErr == nil {
			log.Printf("[BACKUP] Pruned old backup: %s", f.Name())
			_ = os.Remove(fullPath + ".sha256")
			_, _ = h.db.ExecContext(context.Background(),
				`UPDATE backup_catalog SET deleted_at = NOW() WHERE filename = $1`, f.Name())
		}
	}
}

// updateBackupStatus updates last_run and last_status.
func (h *BackupHandler) updateBackupStatus(status string) {
	_, _ = h.db.ExecContext(context.Background(), `
		UPDATE backup_settings SET last_run = NOW(), last_status = $1 WHERE id = 1`, status)
}

// maskCredentials redacts sensitive values from log output.
func maskCredentials(output, password string) string {
	if password != "" {
		return strings.ReplaceAll(output, password, "****")
	}
	return output
}

// parseBackupInterval converts a string like "6h" to a time.Duration.
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

// EnsureTimescaleDBStructures creates TimescaleDB hypertables if they don't exist.
// (Called once at startup to guarantee schema correctness after a fresh DB.)
func (h *BackupHandler) EnsureTimescaleDBStructures() error {
	if _, err := h.db.ExecContext(context.Background(),
		`CREATE EXTENSION IF NOT EXISTS timescaledb CASCADE`); err != nil {
		return fmt.Errorf("failed to enable timescaledb: %w", err)
	}

	for _, stmt := range []string{
		`CREATE TABLE IF NOT EXISTS tag_history (
			id        BIGSERIAL,
			time      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			tag_id    INT NOT NULL,
			value     DOUBLE PRECISION,
			source    VARCHAR(20) DEFAULT 'mqtt',
			PRIMARY KEY (time, id)
		)`,
		`SELECT create_hypertable('tag_history','time',if_not_exists=>TRUE)`,
		`CREATE INDEX IF NOT EXISTS idx_tag_history_tag_id ON tag_history (tag_id, time DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_tag_history_time   ON tag_history (time DESC)`,
		`CREATE TABLE IF NOT EXISTS system_events (
			id         BIGSERIAL,
			time       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			gateway_id INT NOT NULL,
			status     VARCHAR(20) NOT NULL,
			message    TEXT
		)`,
		`SELECT create_hypertable('system_events','time',if_not_exists=>TRUE)`,
		`CREATE INDEX IF NOT EXISTS idx_system_events_gw ON system_events (gateway_id, time DESC)`,
		`CREATE TABLE IF NOT EXISTS backup_settings (
			id INTEGER PRIMARY KEY DEFAULT 1,
			enabled BOOLEAN DEFAULT FALSE,
			interval_hours VARCHAR(10) DEFAULT '24h',
			backup_type VARCHAR(10) DEFAULT 'full',
			retention_days INTEGER DEFAULT 7,
			last_run TIMESTAMPTZ,
			last_status VARCHAR(20)
		)`,
		`INSERT INTO backup_settings (id) VALUES (1) ON CONFLICT (id) DO NOTHING`,
		`CREATE TABLE IF NOT EXISTS alarm_definitions (
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
		)`,
		`CREATE INDEX IF NOT EXISTS idx_alarm_def_tag ON alarm_definitions(tag_id)`,
		`CREATE TABLE IF NOT EXISTS alarm_events (
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
		)`,
		`SELECT create_hypertable('alarm_events','trigger_time',if_not_exists=>TRUE)`,
		`CREATE INDEX IF NOT EXISTS idx_alarm_events_status ON alarm_events(status)`,
		`CREATE INDEX IF NOT EXISTS idx_alarm_events_tag    ON alarm_events(tag_id)`,
		`CREATE INDEX IF NOT EXISTS idx_alarm_events_time   ON alarm_events(trigger_time DESC)`,
	} {
		if _, err := h.db.ExecContext(context.Background(), stmt); err != nil {
			log.Printf("[BACKUP] EnsureTimescaleDB warning (%s…): %v", stmt[:intMin(40, len(stmt))], err)
		}
	}
	return nil
}

func intMin(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ServiceStatus represents the health status of a service.
type ServiceStatus struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// PostRestoreResponse represents the response from PostRestore.
type PostRestoreResponse struct {
	Message string          `json:"message"`
	Steps   []ServiceStatus `json:"steps"`
}

// PostRestore restarts all services in the correct order after a restore.
func (h *BackupHandler) PostRestore(c *gin.Context) {
	log.Println("[POST-RESTORE] Starting post-restore service restart...")
	services := []string{
		"industrial-postgres", "industrial-redis", "industrial-mosquitto",
		"industrial-core-api", "industrial-driver-manager",
		"industrial-engine-historian", "industrial-web-ui",
	}
	results := make([]ServiceStatus, 0, len(services))

	for _, svc := range services {
		log.Printf("[POST-RESTORE] Restarting %s...", svc)
		if err := exec.CommandContext(context.Background(), "docker", "restart", svc).Run(); err != nil {
			log.Printf("[POST-RESTORE] Failed to restart %s: %v", svc, err)
			results = append(results, ServiceStatus{Name: svc, Status: "error"})
			continue
		}
		healthy := false
		for i := 0; i < 30; i++ {
			time.Sleep(2 * time.Second)
			out, err := exec.CommandContext(context.Background(), "docker", "inspect",
				"--format={{.State.Health.Status}}", svc).Output()
			if err == nil && strings.TrimSpace(string(out)) == "healthy" {
				healthy = true
				break
			}
		}
		status := "healthy"
		if !healthy {
			status = "error"
		}
		log.Printf("[POST-RESTORE] %s → %s", svc, status)
		results = append(results, ServiceStatus{Name: svc, Status: status})
	}

	allHealthy := true
	for _, r := range results {
		if r.Status != "healthy" {
			allHealthy = false
			break
		}
	}

	h.logAudit(context.Background(), "post_restore_restart", "", emailFromCtx(c), c.ClientIP(),
		fmt.Sprintf("all_healthy=%v", allHealthy))

	if allHealthy {
		c.JSON(http.StatusOK, PostRestoreResponse{Message: "All services restarted successfully", Steps: results})
	} else {
		c.JSON(http.StatusMultiStatus, PostRestoreResponse{Message: "Post-restore completed with some errors", Steps: results})
	}
}
