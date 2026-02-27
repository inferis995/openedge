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
	"time"

	"github.com/gin-gonic/gin"
)

type BackupHandler struct {
	db *sql.DB
}

func NewBackupHandler(db *sql.DB) *BackupHandler {
	return &BackupHandler{
		db: db,
	}
}

// ExportBackup creates a full system backup (PostgreSQL config + historian) and streams it as a ZIP download
func (h *BackupHandler) ExportBackup(c *gin.Context) {
	// Create a temporary directory for backup artifacts
	tempDir, err := os.MkdirTemp("", "backup-*")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create temp directory"})
		return
	}
	defer os.RemoveAll(tempDir) // Clean up

	// Files to add to zip
	filesToZip := []string{}

	// PostgreSQL Backup (includes config + tag_history + system_events)
	pgDumpFile := filepath.Join(tempDir, "full_backup.sql")
	// Use environment variables or hardcoded assumptions based on docker-compose
	pgHost := os.Getenv("DB_HOST")
	pgUser := os.Getenv("DB_USER")
	pgPass := os.Getenv("DB_PASSWORD") // Should be passed via PGPASSWORD env var
	pgDB := os.Getenv("DB_NAME")

	pgCmd := exec.Command("pg_dump", "-h", pgHost, "-U", pgUser, "-d", pgDB, "-F", "p", "--clean", "--if-exists", "-f", pgDumpFile)
	pgCmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", pgPass))

	if output, err := pgCmd.CombinedOutput(); err != nil {
		log.Printf("pg_dump error: %s", string(output))
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Database backup failed: %v", err)})
		return
	}
	filesToZip = append(filesToZip, pgDumpFile)

	// Create ZIP and stream
	buf := new(bytes.Buffer)
	zipWriter := zip.NewWriter(buf)

	for _, file := range filesToZip {
		// Determine relative path in zip
		relPath, err := filepath.Rel(tempDir, file)
		if err != nil {
			relPath = filepath.Base(file)
		}

		f, err := os.Open(file)
		if err != nil {
			continue
		}

		w, err := zipWriter.Create(relPath)
		if err != nil {
			f.Close()
			continue
		}

		if _, err := io.Copy(w, f); err != nil {
			f.Close()
			continue
		}
		f.Close()
	}

	if err := zipWriter.Close(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to finalize zip"})
		return
	}

	// Send Response
	timestamp := time.Now().Format("20060102-150405")
	filename := fmt.Sprintf("system-backup-%s.zip", timestamp)

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

	// Restore PostgreSQL (config + historian)
	if configFound {
		// Try full_backup.sql first, then fall back to config_dump.sql
		pgDumpFile := filepath.Join(tempDir, "full_backup.sql")
		if _, err := os.Stat(pgDumpFile); os.IsNotExist(err) {
			pgDumpFile = filepath.Join(tempDir, "config_dump.sql")
		}

		pgHost := os.Getenv("DB_HOST")
		pgUser := os.Getenv("DB_USER")
		pgPass := os.Getenv("DB_PASSWORD")
		pgDB := os.Getenv("DB_NAME")

		// Force schema wipe for clean restore
		// Crucial for backups without --clean flag and to avoid conflicts
		log.Println("Wiping schema public...")
		if _, err := h.db.Exec("DROP SCHEMA IF EXISTS public CASCADE; CREATE SCHEMA public; GRANT ALL ON SCHEMA public TO public;"); err != nil {
			log.Printf("Schema wipe warning: %v", err)
			messages = append(messages, fmt.Sprintf("Schema wipe warning: %v", err))
		} else {
			messages = append(messages, "Schema public wiped")
		}

		pgCmd := exec.Command("psql", "-h", pgHost, "-U", pgUser, "-d", pgDB, "-f", pgDumpFile)
		pgCmd.Env = append(os.Environ(), fmt.Sprintf("PGPASSWORD=%s", pgPass))

		if output, err := pgCmd.CombinedOutput(); err != nil {
			messages = append(messages, fmt.Sprintf("Restore failed: %s", string(output)))
		} else {
			messages = append(messages, "Database restored successfully (config + historian)")
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Restore process completed",
		"details": messages,
	})
}
