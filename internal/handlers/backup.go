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
	db          *sql.DB
	influxURL   string
	influxToken string
	influxOrg   string
}

func NewBackupHandler(db *sql.DB, influxURL, influxToken, influxOrg string) *BackupHandler {
	return &BackupHandler{
		db:          db,
		influxURL:   influxURL,
		influxToken: influxToken,
		influxOrg:   influxOrg,
	}
}

// ExportBackup creates a full system backup (Postgres + InfluxDB) and streams it as a ZIP download
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

	// 1. PostgreSQL Backup
	pgDumpFile := filepath.Join(tempDir, "config_dump.sql")
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

	// 2. InfluxDB Backup (if configured)
	if h.influxURL != "" && h.influxToken != "" {
		influxDir := filepath.Join(tempDir, "history_backup")
		// influx backup relies on influx CLI being available
		// Command: influx backup <dir> --host <url> -t <token>
		influxCmd := exec.Command("influx", "backup", influxDir, "--host", h.influxURL, "-t", h.influxToken, "--org", h.influxOrg)

		if output, err := influxCmd.CombinedOutput(); err != nil {
			log.Printf("influx backup warning: %s - continuing without history", string(output))
			// We don't fail the whole backup if metrics fail, usually config is more critical
		} else {
			// Add recursively
			err = filepath.Walk(influxDir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return err
				}
				if !info.IsDir() {
					filesToZip = append(filesToZip, path)
				}
				return nil
			})
			if err != nil {
				log.Printf("Error walking influx backup: %v", err)
			}
		}
	}

	// 3. Create ZIP and stream
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
	historyFound := false

	for _, f := range zipReader.File {
		fpath := filepath.Join(tempDir, f.Name)

		// Check for specific files
		if f.Name == "config_dump.sql" {
			configFound = true
		}
		// Check for history folder structure (starts with history_backup/)
		if len(f.Name) > 15 && f.Name[:14] == "history_backup" {
			historyFound = true
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

	// Restore Config (Postgres)
	if configFound {
		pgDumpFile := filepath.Join(tempDir, "config_dump.sql")
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
			messages = append(messages, fmt.Sprintf("Config restore failed: %s", string(output)))
		} else {
			messages = append(messages, "Config restored successfully")
		}
	}

	// Restore History (InfluxDB)
	if historyFound && h.influxURL != "" {
		influxDir := filepath.Join(tempDir, "history_backup")
		// influx restore <dir>
		influxCmd := exec.Command("influx", "restore", influxDir, "--host", h.influxURL, "-t", h.influxToken)

		if output, err := influxCmd.CombinedOutput(); err != nil {
			messages = append(messages, fmt.Sprintf("History restore warning: %s", string(output)))
		} else {
			messages = append(messages, "History restored successfully")
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Restore process completed",
		"details": messages,
	})
}
