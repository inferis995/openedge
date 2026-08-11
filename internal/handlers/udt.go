package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lib/pq"

	"github.com/ralph/industrial-edge-middleware/internal/middleware"
)

// User-defined types.
//
// Tags in this platform are flat: gateway, address, alias, data type. Fifty
// identical motors mean fifty times N tags entered by hand, and moving one
// alarm threshold means fifty edits, of which one gets missed — and the one
// that gets missed is discovered by an alarm that does not fire.
//
// A type declares the shape once. Every instance is GENERATED from it and
// stays bound to it, so the type is the single place the truth lives. That
// binding is the whole point and also the whole difficulty: editing a type has
// to reach every instance, and one of those edits can destroy data.
//
// ── The constraint that shapes everything here ──────────────────────────────
//
// tag_history.tag_id is ON DELETE CASCADE. Deleting a tag deletes its history.
// So removing a member from a type — one click on one screen — would delete
// the historian data for that member across every instance of the type. On a
// plant with fifty motors and a year of trends that is irreversible and
// silent, and it would look like a successful edit.
//
// Removals are therefore refused unless the caller says so explicitly, and the
// refusal reports how many tags and how many recorded rows are at stake.
// Everything else about the reconciler follows from wanting that one path to
// be impossible to walk into by accident.

type UDTHandler struct {
	db *sql.DB
}

func NewUDTHandler(db *sql.DB) *UDTHandler { return &UDTHandler{db: db} }

// ── Wire types ──────────────────────────────────────────────────────────────

type udtMember struct {
	ID                int     `json:"id,omitempty"`
	Name              string  `json:"name"`
	AddressSuffix     string  `json:"address_suffix"`
	DataType          string  `json:"data_type"`
	Historize         bool    `json:"historize"`
	HistorizeDeadband float64 `json:"historize_deadband"`

	ScalingEnabled bool    `json:"scaling_enabled"`
	ScalingRawMin  float64 `json:"scaling_raw_min"`
	ScalingRawMax  float64 `json:"scaling_raw_max"`
	ScalingEuMin   float64 `json:"scaling_eu_min"`
	ScalingEuMax   float64 `json:"scaling_eu_max"`
	ScalingClamp   bool    `json:"scaling_clamp"`
	EuUnit         string  `json:"eu_unit"`
	EuDecimals     int     `json:"eu_decimals"`
	Invert         bool    `json:"invert"`

	SortOrder int        `json:"sort_order"`
	Alarms    []udtAlarm `json:"alarms"`
}

type udtAlarm struct {
	ID           int      `json:"id,omitempty"`
	AlarmType    string   `json:"alarm_type"`
	Threshold    *float64 `json:"threshold"`
	Deadband     float64  `json:"deadband"`
	DelaySeconds int      `json:"delay_seconds"`
	Severity     string   `json:"severity"`
	Message      string   `json:"message"`
	Enabled      bool     `json:"enabled"`
}

type udtType struct {
	ID          int         `json:"id"`
	OrgID       int         `json:"org_id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Members     []udtMember `json:"members"`
	Instances   int         `json:"instance_count"`
}

type udtInstance struct {
	ID          int    `json:"id"`
	TypeID      int    `json:"type_id"`
	TypeName    string `json:"type_name,omitempty"`
	GatewayID   int    `json:"gateway_id"`
	Name        string `json:"name"`
	BaseAddress string `json:"base_address"`
	TagCount    int    `json:"tag_count,omitempty"`
}

// ── Validation ──────────────────────────────────────────────────────────────

var validDataTypes = map[string]bool{
	"INT": true, "REAL": true, "BOOL": true, "DINT": true, "STRING": true,
}

// memberNamePattern is what an instance's tag alias is built from, so it has
// to survive being embedded in an MQTT topic. The drivers slugify aliases, and
// a member called "Speed / RPM" would produce two different strings on the two
// sides of that — which is exactly how tags stop being historised without
// anybody noticing.
func validMemberName(s string) error {
	if s == "" {
		return errors.New("member name is required")
	}
	if len(s) > 64 {
		return errors.New("member name is longer than 64 characters")
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_', r == '-':
		default:
			return fmt.Errorf("member name %q contains %q; use letters, digits, hyphen or underscore "+
				"— the name becomes part of an MQTT topic", s, string(r))
		}
	}
	return nil
}

func validateMembers(members []udtMember) error {
	if len(members) == 0 {
		return errors.New("a type needs at least one member")
	}
	seen := map[string]bool{}
	for i := range members {
		m := &members[i]
		if err := validMemberName(m.Name); err != nil {
			return err
		}
		if seen[strings.ToLower(m.Name)] {
			return fmt.Errorf("duplicate member name %q", m.Name)
		}
		seen[strings.ToLower(m.Name)] = true

		if !validDataTypes[m.DataType] {
			return fmt.Errorf("member %q has data_type %q; want INT, REAL, BOOL, DINT or STRING",
				m.Name, m.DataType)
		}
		if m.ScalingEnabled && m.ScalingRawMax == m.ScalingRawMin {
			return fmt.Errorf("member %q has scaling enabled with an empty raw span", m.Name)
		}
	}
	return nil
}

// ── Reads ───────────────────────────────────────────────────────────────────

func (h *UDTHandler) loadMembers(ctx context.Context, typeID int) ([]udtMember, error) {
	rows, err := h.db.QueryContext(ctx, `
		SELECT id, name, address_suffix, data_type, historize, historize_deadband,
		       scaling_enabled, scaling_raw_min, scaling_raw_max, scaling_eu_min,
		       scaling_eu_max, scaling_clamp, eu_unit, eu_decimals, invert, sort_order
		FROM udt_members WHERE type_id = $1 ORDER BY sort_order, id`, typeID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	members := []udtMember{}
	for rows.Next() {
		var m udtMember
		if err := rows.Scan(&m.ID, &m.Name, &m.AddressSuffix, &m.DataType, &m.Historize,
			&m.HistorizeDeadband, &m.ScalingEnabled, &m.ScalingRawMin, &m.ScalingRawMax,
			&m.ScalingEuMin, &m.ScalingEuMax, &m.ScalingClamp, &m.EuUnit, &m.EuDecimals,
			&m.Invert, &m.SortOrder); err != nil {
			return nil, err
		}
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range members {
		alarms, err := h.loadMemberAlarms(ctx, members[i].ID)
		if err != nil {
			return nil, err
		}
		members[i].Alarms = alarms
	}
	return members, nil
}

func (h *UDTHandler) loadMemberAlarms(ctx context.Context, memberID int) ([]udtAlarm, error) {
	rows, err := h.db.QueryContext(ctx, `
		SELECT id, alarm_type, threshold, deadband, delay_seconds, severity, message, enabled
		FROM udt_member_alarms WHERE member_id = $1 ORDER BY id`, memberID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	alarms := []udtAlarm{}
	for rows.Next() {
		var a udtAlarm
		if err := rows.Scan(&a.ID, &a.AlarmType, &a.Threshold, &a.Deadband,
			&a.DelaySeconds, &a.Severity, &a.Message, &a.Enabled); err != nil {
			return nil, err
		}
		alarms = append(alarms, a)
	}
	return alarms, rows.Err()
}

// ListTypes returns every type in the caller's organization.
func (h *UDTHandler) ListTypes(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}

	rows, err := h.db.QueryContext(c.Request.Context(), `
		SELECT t.id, t.org_id, t.name, t.description,
		       (SELECT COUNT(*) FROM udt_instances i WHERE i.type_id = t.id)
		FROM udt_types t WHERE t.org_id = $1 ORDER BY t.name`, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer func() { _ = rows.Close() }()

	types := []udtType{}
	for rows.Next() {
		var t udtType
		if err := rows.Scan(&t.ID, &t.OrgID, &t.Name, &t.Description, &t.Instances); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		types = append(types, t)
	}
	c.JSON(http.StatusOK, gin.H{"items": types, "total": len(types)})
}

// GetType returns one type with its members and alarms.
func (h *UDTHandler) GetType(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var t udtType
	err = h.db.QueryRowContext(c.Request.Context(), `
		SELECT t.id, t.org_id, t.name, t.description,
		       (SELECT COUNT(*) FROM udt_instances i WHERE i.type_id = t.id)
		FROM udt_types t WHERE t.id = $1 AND t.org_id = $2`, id, orgID).
		Scan(&t.ID, &t.OrgID, &t.Name, &t.Description, &t.Instances)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "type not found"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if t.Members, err = h.loadMembers(c.Request.Context(), t.ID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, t)
}

// ── Writes ──────────────────────────────────────────────────────────────────

type udtTypeRequest struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Members     []udtMember `json:"members"`

	// ConfirmDataLoss must be set to remove a member that instances already
	// carry, because doing so deletes those tags and, by way of the cascade on
	// tag_history, everything ever recorded for them.
	ConfirmDataLoss bool `json:"confirm_data_loss"`
}

// CreateType defines a new type. It has no instances yet, so nothing to
// reconcile.
func (h *UDTHandler) CreateType(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}
	var req udtTypeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validMemberName(req.Name); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type " + err.Error()})
		return
	}
	if err := validateMembers(req.Members); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	tx, err := h.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer func() { _ = tx.Rollback() }()

	var typeID int
	if err := tx.QueryRowContext(c.Request.Context(), `
		INSERT INTO udt_types (org_id, name, description, created_by)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		orgID, req.Name, req.Description, userIDFromCtx(c)).Scan(&typeID); err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			c.JSON(http.StatusConflict, gin.H{"error": "a type with that name already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if err := insertMembers(c.Request.Context(), tx, typeID, req.Members); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": typeID})
}

func insertMembers(ctx context.Context, tx *sql.Tx, typeID int, members []udtMember) error {
	for i := range members {
		m := &members[i]
		var memberID int
		if err := tx.QueryRowContext(ctx, `
			INSERT INTO udt_members (type_id, name, address_suffix, data_type, historize,
				historize_deadband, scaling_enabled, scaling_raw_min, scaling_raw_max,
				scaling_eu_min, scaling_eu_max, scaling_clamp, eu_unit, eu_decimals, invert, sort_order)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING id`,
			typeID, m.Name, m.AddressSuffix, m.DataType, m.Historize, m.HistorizeDeadband,
			m.ScalingEnabled, m.ScalingRawMin, m.ScalingRawMax, m.ScalingEuMin, m.ScalingEuMax,
			m.ScalingClamp, m.EuUnit, m.EuDecimals, m.Invert, i).Scan(&memberID); err != nil {
			return err
		}
		for _, a := range m.Alarms {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO udt_member_alarms (member_id, alarm_type, threshold, deadband,
					delay_seconds, severity, message, enabled)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
				memberID, a.AlarmType, a.Threshold, a.Deadband, a.DelaySeconds,
				a.Severity, a.Message, a.Enabled); err != nil {
				return err
			}
		}
	}
	return nil
}

func userIDFromCtx(c *gin.Context) interface{} {
	if v, ok := c.Get("user_id"); ok {
		return v
	}
	return nil
}

// ── Reconciliation ──────────────────────────────────────────────────────────

// tagCodeFor joins an instance's base address to a member's suffix.
//
// Deliberately a plain concatenation. The type does not know which protocol it
// will land on: a Modbus instance is base "40001" with suffix "+2", an S7 one
// is base "DB10" with suffix ".DBX0.1", and inventing a syntax here would mean
// inventing one that is wrong for some driver. The suffix carries whatever
// separator the address language needs.
func tagCodeFor(baseAddress, suffix string) string {
	return baseAddress + suffix
}

// tagAliasFor names the generated tag. The instance name prefixes the member
// name so two motors do not collide, and so an operator reading an alarm knows
// which machine it came from without opening anything.
func tagAliasFor(instanceName, memberName string) string {
	return instanceName + "_" + memberName
}

// removalImpact reports what deleting the tags for a set of members would
// destroy, so the refusal can quote it.
type removalImpact struct {
	Members     []string `json:"members"`
	Tags        int      `json:"tags"`
	HistoryRows int64    `json:"history_rows"`
}

func (h *UDTHandler) impactOfRemoving(ctx context.Context, memberIDs []int) (removalImpact, error) {
	var imp removalImpact
	if len(memberIDs) == 0 {
		return imp, nil
	}

	rows, err := h.db.QueryContext(ctx, `
		SELECT m.name, COUNT(t.id)
		FROM udt_members m LEFT JOIN tags t ON t.udt_member_id = m.id
		WHERE m.id = ANY($1) GROUP BY m.name`, pqIntArray(memberIDs))
	if err != nil {
		return imp, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		var n int
		if err := rows.Scan(&name, &n); err != nil {
			return imp, err
		}
		imp.Members = append(imp.Members, name)
		imp.Tags += n
	}
	if err := rows.Err(); err != nil {
		return imp, err
	}

	// The number that makes the warning real. Counting rows on a hypertable is
	// not free, so it is only done on the path that is about to refuse.
	if err := h.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM tag_history
		WHERE tag_id IN (SELECT id FROM tags WHERE udt_member_id = ANY($1))`,
		pqIntArray(memberIDs)).Scan(&imp.HistoryRows); err != nil {
		// A failure to count is not a reason to allow the deletion; report it
		// as unknown and let the caller still have to confirm.
		imp.HistoryRows = -1
	}
	return imp, nil
}

// reconcileType brings every instance of a type in line with the type.
//
// Runs inside the caller's transaction so a partial reconciliation cannot be
// left behind: half the motors carrying a new member and half not is worse
// than the edit failing, because nothing shows it happened.
func reconcileType(ctx context.Context, tx *sql.Tx, typeID int) (created, updated, deleted int, err error) {
	instances, err := loadInstancesForType(ctx, tx, typeID)
	if err != nil || len(instances) == 0 {
		return 0, 0, 0, err
	}
	members, err := loadMembersForType(ctx, tx, typeID)
	if err != nil {
		return 0, 0, 0, err
	}

	for i := range instances {
		in := &instances[i]
		for j := range members {
			m := &members[j]
			code := tagCodeFor(in.baseAddress, m.AddressSuffix)
			alias := tagAliasFor(in.name, m.Name)

			// Matched by (instance, member) rather than by alias: renaming an
			// instance must MOVE its tags, not orphan them and mint new ones,
			// which would strand the history behind an alias nobody reads.
			var tagID int
			lookupErr := tx.QueryRowContext(ctx,
				`SELECT id FROM tags WHERE udt_instance_id = $1 AND udt_member_id = $2`,
				in.id, m.ID).Scan(&tagID)

			switch {
			case errors.Is(lookupErr, sql.ErrNoRows):
				newID, insErr := insertGeneratedTag(ctx, tx, in.gatewayID, in.id, m.ID, code, alias, m)
				if insErr != nil {
					return 0, 0, 0, fmt.Errorf("creating tag %s: %w", alias, insErr)
				}
				tagID = newID
				created++

			case lookupErr != nil:
				return 0, 0, 0, lookupErr

			default:
				if updErr := updateGeneratedTag(ctx, tx, tagID, in.gatewayID, code, alias, m); updErr != nil {
					return 0, 0, 0, fmt.Errorf("updating tag %s: %w", alias, updErr)
				}
				updated++
			}

			if alErr := reconcileMemberAlarms(ctx, tx, m.ID, tagID); alErr != nil {
				return 0, 0, 0, fmt.Errorf("alarms for %s: %w", alias, alErr)
			}
		}

		// Tags whose member no longer exists. udt_member_id is SET NULL on
		// member deletion precisely so they survive to be found here rather
		// than vanishing with the member — by this point the caller has had to
		// confirm the loss.
		res, delErr := tx.ExecContext(ctx,
			`DELETE FROM tags WHERE udt_instance_id = $1 AND udt_member_id IS NULL`, in.id)
		if delErr != nil {
			return 0, 0, 0, delErr
		}
		n, _ := res.RowsAffected()
		deleted += int(n)
	}

	return created, updated, deleted, nil
}

type reconcileInstance struct {
	id, gatewayID     int
	name, baseAddress string
}

func loadInstancesForType(ctx context.Context, tx *sql.Tx, typeID int) ([]reconcileInstance, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT id, gateway_id, name, base_address FROM udt_instances WHERE type_id = $1`, typeID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []reconcileInstance{}
	for rows.Next() {
		var i reconcileInstance
		if err := rows.Scan(&i.id, &i.gatewayID, &i.name, &i.baseAddress); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func loadMembersForType(ctx context.Context, tx *sql.Tx, typeID int) ([]udtMember, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT id, name, address_suffix, data_type, historize, historize_deadband,
		       scaling_enabled, scaling_raw_min, scaling_raw_max, scaling_eu_min,
		       scaling_eu_max, scaling_clamp, eu_unit, eu_decimals, invert
		FROM udt_members WHERE type_id = $1 ORDER BY sort_order, id`, typeID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []udtMember{}
	for rows.Next() {
		var m udtMember
		if err := rows.Scan(&m.ID, &m.Name, &m.AddressSuffix, &m.DataType, &m.Historize,
			&m.HistorizeDeadband, &m.ScalingEnabled, &m.ScalingRawMin, &m.ScalingRawMax,
			&m.ScalingEuMin, &m.ScalingEuMax, &m.ScalingClamp, &m.EuUnit, &m.EuDecimals,
			&m.Invert); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// insertGeneratedTag creates the tag for one member of one instance.
func insertGeneratedTag(ctx context.Context, tx *sql.Tx, gatewayID, instanceID, memberID int,
	code, alias string, m *udtMember) (int, error) {
	var id int
	err := tx.QueryRowContext(ctx, `
		INSERT INTO tags (gateway_id, code, alias, data_type, historize, historize_deadband,
			scaling_enabled, scaling_raw_min, scaling_raw_max, scaling_eu_min, scaling_eu_max,
			scaling_clamp, eu_unit, eu_decimals, invert, udt_instance_id, udt_member_id, sort_order)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,
			(SELECT COALESCE(MAX(sort_order),0)+1 FROM tags WHERE gateway_id = $1))
		RETURNING id`,
		gatewayID, code, alias, m.DataType, m.Historize, m.HistorizeDeadband,
		m.ScalingEnabled, m.ScalingRawMin, m.ScalingRawMax, m.ScalingEuMin, m.ScalingEuMax,
		m.ScalingClamp, m.EuUnit, m.EuDecimals, m.Invert, instanceID, memberID).Scan(&id)
	return id, err
}

// updateGeneratedTag rewrites an existing tag from its member definition.
func updateGeneratedTag(ctx context.Context, tx *sql.Tx, tagID, gatewayID int,
	code, alias string, m *udtMember) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE tags SET code = $1, alias = $2, data_type = $3, historize = $4,
			historize_deadband = $5, scaling_enabled = $6, scaling_raw_min = $7,
			scaling_raw_max = $8, scaling_eu_min = $9, scaling_eu_max = $10,
			scaling_clamp = $11, eu_unit = $12, eu_decimals = $13, invert = $14,
			gateway_id = $15
		WHERE id = $16`,
		code, alias, m.DataType, m.Historize, m.HistorizeDeadband,
		m.ScalingEnabled, m.ScalingRawMin, m.ScalingRawMax, m.ScalingEuMin,
		m.ScalingEuMax, m.ScalingClamp, m.EuUnit, m.EuDecimals, m.Invert,
		gatewayID, tagID)
	return err
}

// reconcileMemberAlarms rewrites a tag's alarm definitions from its member's.
//
// Replace rather than merge: the type is the single source of truth, and a
// definition left behind from a previous shape of the type is an alarm nobody
// declared and nobody expects.
func reconcileMemberAlarms(ctx context.Context, tx *sql.Tx, memberID, tagID int) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM alarm_definitions WHERE tag_id = $1`, tagID); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT alarm_type, threshold, deadband, delay_seconds, severity, message, enabled
		FROM udt_member_alarms WHERE member_id = $1 ORDER BY id`, memberID)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	type def struct {
		alarmType         string
		threshold         *float64
		deadband          float64
		delay             int
		severity, message string
		enabled           bool
	}
	defs := []def{}
	for rows.Next() {
		var d def
		if err := rows.Scan(&d.alarmType, &d.threshold, &d.deadband, &d.delay,
			&d.severity, &d.message, &d.enabled); err != nil {
			return err
		}
		defs = append(defs, d)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for k := range defs {
		d := &defs[k]
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO alarm_definitions (tag_id, alarm_type, threshold, deadband,
				delay_seconds, severity, message, enabled)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			tagID, d.alarmType, d.threshold, d.deadband, d.delay,
			d.severity, d.message, d.enabled); err != nil {
			return err
		}
	}
	return nil
}

// pqIntArray adapts a slice of ids for `= ANY($1)`.
func pqIntArray(ids []int) interface{} {
	arr := make([]int64, len(ids))
	for i, v := range ids {
		arr[i] = int64(v)
	}
	return pq.Array(arr)
}

// UpdateType replaces a type's members and reconciles every instance.
//
// The members are sent whole rather than patched one at a time. A type is a
// shape, and applying a shape change member-by-member would mean the instances
// pass through states the engineer never asked for — a motor briefly without
// its Fault bit is a motor whose alarm briefly cannot fire.
func (h *UDTHandler) UpdateType(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}
	id, idErr := strconv.Atoi(c.Param("id"))
	if idErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req udtTypeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validMemberName(req.Name); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "type " + err.Error()})
		return
	}
	if err := validateMembers(req.Members); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var owned bool
	if err := h.db.QueryRowContext(c.Request.Context(),
		`SELECT EXISTS (SELECT 1 FROM udt_types WHERE id = $1 AND org_id = $2)`,
		id, orgID).Scan(&owned); err != nil || !owned {
		c.JSON(http.StatusNotFound, gin.H{"error": "type not found"})
		return
	}

	existing, removing, err := h.diffMembers(c.Request.Context(), id, req.Members)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// The refusal this whole feature is built around. tag_history cascades
	// from tags, so dropping a member takes every recorded value for it on
	// every instance — and an edit screen gives no hint that is what happened.
	if len(removing) > 0 && !req.ConfirmDataLoss {
		refused, impErr := h.refuseDataLoss(c.Request.Context(), removing)
		if impErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": impErr.Error()})
			return
		}
		if refused != nil {
			c.JSON(http.StatusConflict, refused)
			return
		}
	}

	created, updated, deleted, err := h.applyTypeUpdate(c.Request.Context(), id, &req, existing)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "updated",
		"reconciled": gin.H{
			"tags_created": created,
			"tags_updated": updated,
			"tags_deleted": deleted,
		},
	})
}

// diffMembers reports the current members by name, and which of them the
// submitted shape drops.
//
// Matched by NAME, not id. Name is the identity an engineer works with: they
// retype the shape, they do not carry ids around. Matching on id would treat
// every edit as a wholesale replacement and destroy the history of every
// member on every save, which is the opposite of what a type is for.
func (h *UDTHandler) diffMembers(ctx context.Context, typeID int, submitted []udtMember) (
	existing map[string]int, removing []int, err error) {

	keep := map[string]bool{}
	for i := range submitted {
		keep[strings.ToLower(submitted[i].Name)] = true
	}

	rows, err := h.db.QueryContext(ctx, `SELECT id, name FROM udt_members WHERE type_id = $1`, typeID)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = rows.Close() }()

	existing = map[string]int{}
	for rows.Next() {
		var mid int
		var name string
		if scanErr := rows.Scan(&mid, &name); scanErr != nil {
			return nil, nil, scanErr
		}
		existing[strings.ToLower(name)] = mid
		if !keep[strings.ToLower(name)] {
			removing = append(removing, mid)
		}
	}
	return existing, removing, rows.Err()
}

// refuseDataLoss returns the body to answer with, or nil when nothing would be
// destroyed and the edit may proceed.
func (h *UDTHandler) refuseDataLoss(ctx context.Context, removing []int) (gin.H, error) {
	imp, err := h.impactOfRemoving(ctx, removing)
	if err != nil {
		return nil, err
	}
	if imp.Tags == 0 {
		return nil, nil
	}
	return gin.H{
		"error": fmt.Sprintf(
			"removing %s would delete %d tag(s) across the instances of this type, "+
				"and with them every value ever recorded for those tags (%s). "+
				"Re-send with confirm_data_loss=true if that is intended.",
			strings.Join(imp.Members, ", "), imp.Tags, historyRowsPhrase(imp.HistoryRows)),
		"impact": imp,
	}, nil
}

// applyTypeUpdate rewrites the members and reconciles, in one transaction.
//
// One transaction on purpose: half the motors carrying a new member and half
// not is worse than the edit failing, because nothing shows it happened.
func (h *UDTHandler) applyTypeUpdate(ctx context.Context, typeID int, req *udtTypeRequest,
	existing map[string]int) (created, updated, deleted int, err error) {

	tx, err := h.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, 0, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx,
		`UPDATE udt_types SET name = $1, description = $2, updated_at = NOW() WHERE id = $3`,
		req.Name, req.Description, typeID); err != nil {
		return 0, 0, 0, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM udt_members WHERE type_id = $1`, typeID); err != nil {
		return 0, 0, 0, err
	}
	if err = insertMembersPreservingIdentity(ctx, tx, typeID, req.Members, existing); err != nil {
		return 0, 0, 0, err
	}
	if created, updated, deleted, err = reconcileType(ctx, tx, typeID); err != nil {
		return 0, 0, 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, 0, 0, err
	}
	return created, updated, deleted, nil
}

func historyRowsPhrase(n int64) string {
	switch {
	case n < 0:
		return "the number of recorded rows could not be counted"
	case n == 0:
		return "no rows recorded yet"
	default:
		return fmt.Sprintf("%d recorded rows", n)
	}
}

// insertMembersPreservingIdentity re-creates members, reusing the previous row
// id where the name is unchanged.
//
// Without this the DELETE-then-INSERT above would hand every member a new id,
// the reconciler would find no tag matching (instance, member), and it would
// create a second set of tags while the first set — carrying all the history —
// was left with a NULL member and then deleted as orphaned. Every save of a
// type would silently wipe the historian.
func insertMembersPreservingIdentity(ctx context.Context, tx *sql.Tx, typeID int,
	members []udtMember, previous map[string]int) error {
	for i := range members {
		m := &members[i]
		var memberID int
		if prev, ok := previous[strings.ToLower(m.Name)]; ok {
			// Re-insert with the original id so existing tags stay attached.
			if err := tx.QueryRowContext(ctx, `
				INSERT INTO udt_members (id, type_id, name, address_suffix, data_type, historize,
					historize_deadband, scaling_enabled, scaling_raw_min, scaling_raw_max,
					scaling_eu_min, scaling_eu_max, scaling_clamp, eu_unit, eu_decimals, invert, sort_order)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17) RETURNING id`,
				prev, typeID, m.Name, m.AddressSuffix, m.DataType, m.Historize, m.HistorizeDeadband,
				m.ScalingEnabled, m.ScalingRawMin, m.ScalingRawMax, m.ScalingEuMin, m.ScalingEuMax,
				m.ScalingClamp, m.EuUnit, m.EuDecimals, m.Invert, i).Scan(&memberID); err != nil {
				return err
			}
			// Re-point the tags whose member was just deleted (SET NULL) back
			// at the member they belong to.
			if _, err := tx.ExecContext(ctx,
				`UPDATE tags SET udt_member_id = $1
				 WHERE udt_member_id IS NULL AND udt_instance_id IN
				       (SELECT id FROM udt_instances WHERE type_id = $2)
				   AND alias LIKE '%' || $3`,
				memberID, typeID, "_"+m.Name); err != nil {
				return err
			}
		} else {
			if err := tx.QueryRowContext(ctx, `
				INSERT INTO udt_members (type_id, name, address_suffix, data_type, historize,
					historize_deadband, scaling_enabled, scaling_raw_min, scaling_raw_max,
					scaling_eu_min, scaling_eu_max, scaling_clamp, eu_unit, eu_decimals, invert, sort_order)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) RETURNING id`,
				typeID, m.Name, m.AddressSuffix, m.DataType, m.Historize, m.HistorizeDeadband,
				m.ScalingEnabled, m.ScalingRawMin, m.ScalingRawMax, m.ScalingEuMin, m.ScalingEuMax,
				m.ScalingClamp, m.EuUnit, m.EuDecimals, m.Invert, i).Scan(&memberID); err != nil {
				return err
			}
		}

		for _, a := range m.Alarms {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO udt_member_alarms (member_id, alarm_type, threshold, deadband,
					delay_seconds, severity, message, enabled)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
				memberID, a.AlarmType, a.Threshold, a.Deadband, a.DelaySeconds,
				a.Severity, a.Message, a.Enabled); err != nil {
				return err
			}
		}
	}

	// Keep the sequence ahead of the ids reinserted by hand, or the next
	// member created normally collides with one of them.
	if _, err := tx.ExecContext(ctx,
		`SELECT setval(pg_get_serial_sequence('udt_members','id'),
		                GREATEST((SELECT COALESCE(MAX(id),0) FROM udt_members), 1))`); err != nil {
		return err
	}
	return nil
}

// ── Instances ───────────────────────────────────────────────────────────────

type udtInstanceRequest struct {
	TypeID      int    `json:"type_id"`
	GatewayID   int    `json:"gateway_id"`
	Name        string `json:"name"`
	BaseAddress string `json:"base_address"`
}

// CreateInstance stamps a type onto a gateway and generates its tags.
func (h *UDTHandler) CreateInstance(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}
	var req udtInstanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validMemberName(req.Name); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "instance " + err.Error()})
		return
	}

	// Both the type and the gateway must belong to the caller's organization.
	// Checked in one query: a type from tenant A stamped onto a gateway in
	// tenant B would generate tags that belong to neither consistently.
	var typeOK, gatewayOK bool
	if err := h.db.QueryRowContext(c.Request.Context(), `
		SELECT
			EXISTS (SELECT 1 FROM udt_types WHERE id = $1 AND org_id = $3),
			EXISTS (SELECT 1 FROM gateways g
			        JOIN areas a ON g.area_id = a.id
			        JOIN sites s ON a.site_id = s.id
			        WHERE g.id = $2 AND s.org_id = $3)`,
		req.TypeID, req.GatewayID, orgID).Scan(&typeOK, &gatewayOK); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !typeOK {
		c.JSON(http.StatusNotFound, gin.H{"error": "type not found in this organization"})
		return
	}
	if !gatewayOK {
		c.JSON(http.StatusNotFound, gin.H{"error": "gateway not found in this organization"})
		return
	}

	tx, txErr := h.db.BeginTx(c.Request.Context(), nil)
	if txErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": txErr.Error()})
		return
	}
	defer func() { _ = tx.Rollback() }()

	var instanceID int
	if err := tx.QueryRowContext(c.Request.Context(), `
		INSERT INTO udt_instances (type_id, gateway_id, name, base_address)
		VALUES ($1,$2,$3,$4) RETURNING id`,
		req.TypeID, req.GatewayID, req.Name, req.BaseAddress).Scan(&instanceID); err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			c.JSON(http.StatusConflict, gin.H{"error": "an instance with that name already exists on this gateway"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	created, _, _, err := reconcileType(c.Request.Context(), tx, req.TypeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"id": instanceID, "tags_created": created})
}

// ListInstances returns the instances of the caller's organization,
// optionally narrowed to one type.
func (h *UDTHandler) ListInstances(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}

	query := `
		SELECT i.id, i.type_id, t.name, i.gateway_id, i.name, i.base_address,
		       (SELECT COUNT(*) FROM tags tg WHERE tg.udt_instance_id = i.id)
		FROM udt_instances i
		JOIN udt_types t ON t.id = i.type_id
		WHERE t.org_id = $1`
	args := []interface{}{orgID}
	if raw := c.Query("type_id"); raw != "" {
		if tid, err := strconv.Atoi(raw); err == nil {
			query += ` AND i.type_id = $2`
			args = append(args, tid)
		}
	}
	query += ` ORDER BY t.name, i.name`

	rows, err := h.db.QueryContext(c.Request.Context(), query, args...)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer func() { _ = rows.Close() }()

	items := []udtInstance{}
	for rows.Next() {
		var in udtInstance
		if err := rows.Scan(&in.ID, &in.TypeID, &in.TypeName, &in.GatewayID,
			&in.Name, &in.BaseAddress, &in.TagCount); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		items = append(items, in)
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": len(items)})
}

// UpdateInstance renames an instance or moves its base address, then
// reconciles so its tags follow.
func (h *UDTHandler) UpdateInstance(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}
	id, idErr := strconv.Atoi(c.Param("id"))
	if idErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}
	var req udtInstanceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validMemberName(req.Name); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "instance " + err.Error()})
		return
	}

	var typeID int
	if err := h.db.QueryRowContext(c.Request.Context(), `
		SELECT i.type_id FROM udt_instances i
		JOIN udt_types t ON t.id = i.type_id
		WHERE i.id = $1 AND t.org_id = $2`, id, orgID).Scan(&typeID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
		return
	}

	tx, txErr := h.db.BeginTx(c.Request.Context(), nil)
	if txErr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": txErr.Error()})
		return
	}
	defer func() { _ = tx.Rollback() }()

	// Renaming moves the tags rather than replacing them — they are matched by
	// (instance, member), so the history follows the equipment.
	if _, err := tx.ExecContext(c.Request.Context(), `
		UPDATE udt_instances SET name = $1, base_address = $2, updated_at = NOW()
		WHERE id = $3`, req.Name, req.BaseAddress, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	_, updated, _, err := reconcileType(c.Request.Context(), tx, typeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "updated", "tags_updated": updated})
}

// DeleteInstance removes an instance and its generated tags.
//
// Deleting a named piece of equipment is an explicit act, so this does not ask
// for the confirmation a member removal does — but it says what it destroyed,
// because the history goes with the tags either way.
func (h *UDTHandler) DeleteInstance(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var tagCount int
	if err := h.db.QueryRowContext(c.Request.Context(), `
		SELECT COUNT(tg.id) FROM udt_instances i
		JOIN udt_types t ON t.id = i.type_id
		LEFT JOIN tags tg ON tg.udt_instance_id = i.id
		WHERE i.id = $1 AND t.org_id = $2
		GROUP BY i.id`, id, orgID).Scan(&tagCount); errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, gin.H{"error": "instance not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if _, err := h.db.ExecContext(c.Request.Context(),
		`DELETE FROM udt_instances WHERE id = $1`, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted", "tags_deleted": tagCount})
}

// DeleteType removes a type. Refused while instances exist: the instances are
// real equipment, and the ON DELETE RESTRICT on udt_instances.type_id makes
// the database agree.
func (h *UDTHandler) DeleteType(c *gin.Context) {
	orgID, ok := middleware.GetOrganizationID(c)
	if !ok {
		c.JSON(http.StatusForbidden, gin.H{"error": "organization context required"})
		return
	}
	id, idErr := strconv.Atoi(c.Param("id"))
	if idErr != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
		return
	}

	var instances int
	if err := h.db.QueryRowContext(c.Request.Context(), `
		SELECT COUNT(*) FROM udt_instances i
		JOIN udt_types t ON t.id = i.type_id
		WHERE i.type_id = $1 AND t.org_id = $2`, id, orgID).Scan(&instances); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if instances > 0 {
		c.JSON(http.StatusConflict, gin.H{
			"error": fmt.Sprintf("%d instance(s) still use this type; delete them first", instances),
		})
		return
	}

	res, err := h.db.ExecContext(c.Request.Context(),
		`DELETE FROM udt_types WHERE id = $1 AND org_id = $2`, id, orgID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "type not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "deleted"})
}
