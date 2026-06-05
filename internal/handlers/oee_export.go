package handlers

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type OEEExportHandler struct {
	db       *sql.DB
	oee      *OEEHandler
	history  *OEEHistoryHandler
	losses   *LossesHandler
}

func NewOEEExportHandler(db *sql.DB, oee *OEEHandler, history *OEEHistoryHandler, losses *LossesHandler) *OEEExportHandler {
	return &OEEExportHandler{db: db, oee: oee, history: history, losses: losses}
}

func (h *OEEExportHandler) ExportHistoryCSV(c *gin.Context) {
	from, to, err := h.parseRange(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	profileFilter := strings.TrimSpace(c.Query("profile_id"))

	var rows *sql.Rows
	if profileFilter == "" || profileFilter == "0" {
		rows, err = h.db.Query(`
			SELECT p.name, h.bucket_start, h.bucket_size,
				h.oee, h.availability, h.performance, h.quality,
				h.planned_min, h.downtime_min,
				h.pieces_produced, h.pieces_good,
				COALESCE(s.name,'')
			FROM oee_history h
			LEFT JOIN oee_profiles p ON p.id = h.profile_id
			LEFT JOIN shifts s ON s.id = h.shift_id
			WHERE h.bucket_start >= $1 AND h.bucket_start < $2
			ORDER BY h.bucket_start ASC`,
			from, to,
		)
	} else {
		rows, err = h.db.Query(`
			SELECT p.name, h.bucket_start, h.bucket_size,
				h.oee, h.availability, h.performance, h.quality,
				h.planned_min, h.downtime_min,
				h.pieces_produced, h.pieces_good,
				COALESCE(s.name,'')
			FROM oee_history h
			LEFT JOIN oee_profiles p ON p.id = h.profile_id
			LEFT JOIN shifts s ON s.id = h.shift_id
			WHERE (h.profile_id::text = $1 OR ($1 = '' AND h.profile_id IS NULL))
			  AND h.bucket_start >= $2 AND h.bucket_start < $3
			ORDER BY h.bucket_start ASC`,
			profileFilter, from, to,
		)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	h.writeCSV(c, "oee_history.csv", func(w *csvWriter) {
		w.row("Profilo", "Data/Ora", "Bucket", "OEE%", "Availability%", "Performance%", "Quality%",
			"Planned Min", "Downtime Min", "Pezzi Prodotti", "Pezzi Buoni", "Turno")
		for rows.Next() {
			var profileName sql.NullString
			var bucketStart time.Time
			var bucketSize string
			var oee, avail, perf, qual, planned, downtime, produced, good float64
			var shiftName string
			if rows.Scan(&profileName, &bucketStart, &bucketSize,
				&oee, &avail, &perf, &qual,
				&planned, &downtime, &produced, &good,
				&shiftName) != nil {
				continue
			}
			pName := "OEE Overall"
			if profileName.Valid && profileName.String != "" {
				pName = profileName.String
			}
			w.row(pName,
				bucketStart.Format("2006-01-02 15:04"),
				bucketSize,
				fmt.Sprintf("%.1f", oee),
				fmt.Sprintf("%.1f", avail),
				fmt.Sprintf("%.1f", perf),
				fmt.Sprintf("%.1f", qual),
				fmt.Sprintf("%.0f", planned),
				fmt.Sprintf("%.1f", downtime),
				fmt.Sprintf("%.0f", produced),
				fmt.Sprintf("%.0f", good),
				shiftName,
			)
		}
	})
}

func (h *OEEExportHandler) ExportByShiftCSV(c *gin.Context) {
	from, to, err := h.parseRange(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	profileFilter := strings.TrimSpace(c.Query("profile_id"))

	baseSQL := `
		SELECT p.name, h.shift_id, s.name,
			to_char(date_trunc('day', h.bucket_start), 'YYYY-MM-DD') AS date,
			AVG(h.oee), AVG(h.availability), AVG(h.performance), AVG(h.quality),
			COUNT(*) AS hours
		FROM oee_history h
		JOIN shifts s ON s.id = h.shift_id
		LEFT JOIN oee_profiles p ON p.id = h.profile_id
		WHERE h.bucket_size = 'hour'
		  AND h.bucket_start >= $1 AND h.bucket_start < $2
		  AND h.shift_id IS NOT NULL`

	var rows *sql.Rows
	if profileFilter == "" || profileFilter == "0" {
		rows, err = h.db.Query(baseSQL+`
			  AND h.profile_id IS NULL
			GROUP BY p.name, h.shift_id, s.name, date
			ORDER BY date, h.shift_id`,
			from, to,
		)
	} else {
		rows, err = h.db.Query(baseSQL+`
			  AND h.profile_id = $3
			GROUP BY p.name, h.shift_id, s.name, date
			ORDER BY date, h.shift_id`,
			from, to, profileFilter,
		)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	h.writeCSV(c, "oee_by_shift.csv", func(w *csvWriter) {
		w.row("Profilo", "Data", "Turno", "OEE%", "Availability%", "Performance%", "Quality%", "Ore")
		for rows.Next() {
			var profileName sql.NullString
			var shiftID int
			var shiftName, date string
			var oee, avail, perf, qual float64
			var hours int
			if rows.Scan(&profileName, &shiftID, &shiftName, &date,
				&oee, &avail, &perf, &qual, &hours) != nil {
				continue
			}
			pName := "OEE Overall"
			if profileName.Valid && profileName.String != "" {
				pName = profileName.String
			}
			w.row(pName, date, shiftName,
				fmt.Sprintf("%.1f", oee),
				fmt.Sprintf("%.1f", avail),
				fmt.Sprintf("%.1f", perf),
				fmt.Sprintf("%.1f", qual),
				fmt.Sprintf("%d", hours),
			)
		}
	})
}

func (h *OEEExportHandler) ExportLossTreeCSV(c *gin.Context) {
	pid := strings.TrimSpace(c.Query("profile_id"))
	if pid == "" || pid == "0" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "profile_id required"})
		return
	}
	from, to, err := h.parseRange(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	rows, err := h.db.Query(`
		SELECT c.code, c.loss_pillar, c.display_label,
			e.start_at, e.end_at, e.duration_min, e.source, COALESCE(e.notes,'')
		FROM oee_loss_events e
		JOIN oee_loss_categories c ON c.id = e.category_id
		WHERE e.profile_id = $1
		  AND e.start_at >= $2 AND e.start_at < $3
		ORDER BY e.start_at ASC`,
		pid, from, to,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()

	h.writeCSV(c, "oee_losses.csv", func(w *csvWriter) {
		w.row("Categoria", "Pillar", "Label", "Inizio", "Fine", "Durata Min", "Fonte", "Note")
		for rows.Next() {
			var code, pillar, label, source, notes string
			var startAt, endAt time.Time
			var duration float64
			if rows.Scan(&code, &pillar, &label,
				&startAt, &endAt, &duration, &source, &notes) != nil {
				continue
			}
			w.row(code, pillar, label,
				startAt.Format("2006-01-02 15:04"),
				endAt.Format("2006-01-02 15:04"),
				fmt.Sprintf("%.1f", duration),
				source, notes,
			)
		}
	})
}

func (h *OEEExportHandler) ExportProfilesCSV(c *gin.Context) {
	profiles := h.oee.loadEnabledProfiles()

	h.writeCSV(c, "oee_profiles.csv", func(w *csvWriter) {
		w.row("ID", "Nome", "Descrizione", "Area", "Abilitato",
			"Window Min", "Target OEE%", "Target Pz/H",
			"Rispetta Turni", "Rispetta Manutenzione",
			"Tag Running", "Tag Produced", "Tag Good")
		for _, p := range profiles {
			enabled := "Si"
			if !p.Enabled {
				enabled = "No"
			}
			respectShifts := "Si"
			if !p.RespectShifts {
				respectShifts = "No"
			}
			respectMaint := "Si"
			if !p.RespectMaintenance {
				respectMaint = "No"
			}
			w.row(
				fmt.Sprintf("%d", p.ID),
				p.Name,
				p.Description,
				p.AreaName,
				enabled,
				fmt.Sprintf("%d", p.WindowMinutes),
				fmt.Sprintf("%.0f", p.TargetOEE),
				fmt.Sprintf("%.0f", p.TargetPiecesPerHour),
				respectShifts,
				respectMaint,
				tagIDOrEmpty(p.RunTimeTagID),
				tagIDOrEmpty(p.ProducedTagID),
				tagIDOrEmpty(p.GoodTagID),
			)
		}
	})
}

func tagIDOrEmpty(id *int) string {
	if id == nil {
		return ""
	}
	return fmt.Sprintf("%d", *id)
}

func (h *OEEExportHandler) parseRange(c *gin.Context) (time.Time, time.Time, error) {
	fromStr := c.Query("from")
	toStr := c.Query("to")
	now := time.Now().UTC()
	var from, to time.Time
	var err error

	if fromStr != "" {
		from, err = parseHistoryTime(fromStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid 'from': %s", err.Error())
		}
	} else {
		from = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Add(-30 * 24 * time.Hour)
	}
	if toStr != "" {
		to, err = parseHistoryTime(toStr)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("invalid 'to': %s", err.Error())
		}
	} else {
		to = now
	}
	return from, to, nil
}

type csvWriter struct {
	buf []string
}

func (w *csvWriter) row(fields ...string) {
	escaped := make([]string, len(fields))
	for i, f := range fields {
		if strings.Contains(f, ",") || strings.Contains(f, "\"") || strings.Contains(f, "\n") {
			escaped[i] = "\"" + strings.ReplaceAll(f, "\"", "\"\"") + "\""
		} else {
			escaped[i] = f
		}
	}
	w.buf = append(w.buf, strings.Join(escaped, ","))
}

func (h *OEEExportHandler) writeCSV(c *gin.Context, filename string, fill func(w *csvWriter)) {
	w := &csvWriter{}
	fill(w)
	bom := "\xEF\xBB\xBF"
	body := bom + strings.Join(w.buf, "\n") + "\n"

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	c.String(http.StatusOK, body)
}
