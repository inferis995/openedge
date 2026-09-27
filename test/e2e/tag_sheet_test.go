//go:build e2e

package e2e

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"testing"
	"time"

	"github.com/ralph/industrial-edge-middleware/internal/tagsheet"
)

// A signal list kept in Excel imports as tags: previewed first, applied only
// when every row is right, and exported back in the same shape.
func TestATagSheetIsPreviewedAppliedAndExported(t *testing.T) {
	admin, _ := adminSession(t)
	db := openDB(t)

	suffix := uniqueSuffix()
	org := createOrg(t, admin, "sheet-"+suffix)
	gw := seedInventoryGateway(t, db, org.ID, "sheet-plc-"+suffix)
	orgAdmin := createOrgAdmin(t, admin, org.ID, "sheet-"+suffix, "e2e-Password-"+suffix)

	sheet := "Nome;Indirizzo;Tipo;Storicizza;Banda morta;Unità;Scalatura;Grezzo min;Grezzo max;Min EU;Max EU\n" +
		"Temperatura;DB1.DBD0;REAL;sì;0,5;°C;no;;;;\n" +
		"Pressione;DB1.DBW4;INT;sì;;bar;sì;0;27648;0;10\n"

	// Preview: nothing written.
	var res struct {
		Rows []struct {
			Line    int      `json:"line"`
			Action  string   `json:"action"`
			Changes []string `json:"changes"`
		} `json:"rows"`
		Created, Updated, Unchanged, Invalid int
		Applied                             bool
	}
	uploadSheet(t, orgAdmin, gw, sheet, false, &res)
	if res.Created != 2 || res.Applied {
		t.Fatalf("preview: %+v", res)
	}
	if n := countTags(t, db, gw); n != 0 {
		t.Fatalf("a preview wrote %d tags", n)
	}

	// Apply.
	uploadSheet(t, orgAdmin, gw, sheet, true, &res)
	if !res.Applied || res.Created != 2 {
		t.Fatalf("apply: %+v", res)
	}
	var unit string
	var rawMax, deadband float64
	var scaling bool
	if err := db.QueryRow(`SELECT eu_unit, scaling_enabled, scaling_raw_max FROM tags WHERE gateway_id = $1 AND code = 'DB1.DBW4'`,
		gw).Scan(&unit, &scaling, &rawMax); err != nil {
		t.Fatalf("reading the imported tag: %v", err)
	}
	if unit != "bar" || !scaling || rawMax != 27648 {
		t.Errorf("imported pressure tag: unit=%q scaling=%v raw max=%v", unit, scaling, rawMax)
	}
	if err := db.QueryRow(`SELECT historize_deadband FROM tags WHERE gateway_id = $1 AND code = 'DB1.DBD0'`,
		gw).Scan(&deadband); err != nil || deadband != 0.5 {
		t.Errorf("the Italian decimal comma was not read: deadband=%v err=%v", deadband, err)
	}

	// The same sheet again: nothing to do. A rename: one update, and a sheet
	// without the scaling columns leaves the scaling alone.
	uploadSheet(t, orgAdmin, gw, sheet, false, &res)
	if res.Unchanged != 2 || res.Created+res.Updated != 0 {
		t.Errorf("re-importing the same sheet: %+v", res)
	}
	uploadSheet(t, orgAdmin, gw, "Name,Address,Type\nLine pressure,DB1.DBW4,INT\n", true, &res)
	if res.Updated != 1 || !res.Applied {
		t.Fatalf("rename: %+v", res)
	}
	if err := db.QueryRow(`SELECT scaling_enabled FROM tags WHERE gateway_id = $1 AND code = 'DB1.DBW4'`,
		gw).Scan(&scaling); err != nil || !scaling {
		t.Errorf("a sheet without scaling columns switched the scaling off (err %v)", err)
	}

	// One bad row: nothing at all is written.
	uploadSheet(t, orgAdmin, gw, "Name,Address,Type\nGood,DB1.DBD20,REAL\nBad,DB1.DBD24,WORD\n", true, &res)
	if res.Applied || res.Invalid != 1 {
		t.Fatalf("a sheet with an invalid row: %+v", res)
	}
	if n := countTags(t, db, gw); n != 2 {
		t.Errorf("a rejected sheet still wrote tags: %d on the gateway, want 2", n)
	}

	// Export as Excel and read it back. The global admin sends no
	// organization header: the export used to refuse that with a 400.
	status, body := rawGet(t, admin, fmt.Sprintf("/api/tags/export?gateway_id=%d&format=xlsx&lang=it", gw))
	if status != http.StatusOK {
		t.Fatalf("export returned %d: %s", status, truncate(body))
	}
	back := tagsheet.Parse(body)
	if back.Blocking() || len(back.Rows) != 2 {
		t.Fatalf("the exported workbook does not read back: %d rows, %+v", len(back.Rows), back.Problems)
	}

	// The template carries this gateway's address format.
	status, body = rawGet(t, orgAdmin, fmt.Sprintf("/api/tags/import/template?gateway_id=%d&format=csv&lang=en", gw))
	if status != http.StatusOK || !bytes.Contains(body, []byte("Address")) {
		t.Errorf("template returned %d: %s", status, truncate(body))
	}
}

func uploadSheet(t *testing.T, c *apiClient, gatewayID int, content string, apply bool, out interface{}) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("gateway_id", fmt.Sprint(gatewayID))
	_ = w.WriteField("apply", fmt.Sprint(apply))
	fw, _ := w.CreateFormFile("file", "tags.csv")
	_, _ = fw.Write([]byte(content))
	_ = w.Close()

	req, _ := http.NewRequest(http.MethodPost, apiBase()+"/api/tags/import/file", bytes.NewReader(buf.Bytes()))
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.token)
	if c.orgID != "" {
		req.Header.Set("X-Organization-ID", c.orgID)
	}
	resp, err := httpClient(30 * time.Second).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import returned %d: %s", resp.StatusCode, truncate(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		t.Fatalf("import response: %v: %s", err, truncate(body))
	}
}

func rawGet(t *testing.T, c *apiClient, path string) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, apiBase()+path, nil)
	req.Header.Set("Authorization", "Bearer "+c.token)
	if c.orgID != "" {
		req.Header.Set("X-Organization-ID", c.orgID)
	}
	resp, err := httpClient(30 * time.Second).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

func countTags(t *testing.T, db *sql.DB, gatewayID int) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM tags WHERE gateway_id = $1`, gatewayID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
