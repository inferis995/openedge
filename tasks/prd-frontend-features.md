# PRD: Frontend Features - Industrial Edge Middleware

## Introduction

Completamento della frontend UI per il sistema Industrial Edge Middleware. Questo documento descrive le funzionalità che devono essere implementate nella cartella `frontend/` dopo l'eliminazione di `services/web-ui/`.

## Goals

- Implementare tutti i dialoghi per creazione entità (Organization, Site, Area, Gateway, Tag)
- Aggiungere polling real-time per i valori dei tag nel tree view
- Implementare pagina Trend/Historian con grafici multi-tag
- Implementare pagina Alarms con gestione completa
- Aggiungere notifiche real-time via MQTT
- Creare container Docker per produzione

## User Stories

### US-100: Implement organization creation dialog

**Description:** As a user, I need to create organizations so that I can set up new tenants.

**Acceptance Criteria:**
- [ ] Create `src/components/dialogs/CreateOrgDialog.tsx`
- [ ] Use shadcn Dialog component with proper styling
- [ ] Render Input field for "Organization Name"
- [ ] Add "Create" button that validates input (not empty, min 2 chars)
- [ ] On submit: call `api.createOrganization()` with name
- [ ] On success: close dialog, refresh hierarchy, show success toast
- [ ] On error: show error toast with message from API
- [ ] Add keyboard shortcut (Enter to submit, Escape to close)
- [ ] Typecheck passes
- [ ] Verify in browser: dialog opens/closes, creation works

---

### US-101: Implement site creation dialog

**Description:** As a user, I need to create sites within organizations so that I can organize locations.

**Acceptance Criteria:**
- [ ] Create `src/components/dialogs/CreateSiteDialog.tsx`
- [ ] Include Select dropdown for parent organization (fetched from API)
- [ ] Include Input for "Site Name" with validation
- [ ] Show organization name in dialog title for context
- [ ] On submit: call `api.createSite()` with org_id and name
- [ ] Handle success/error with toast notifications
- [ ] Reset form on close/open
- [ ] Typecheck passes
- [ ] Verify in browser: org dropdown populates, creation works

---

### US-102: Implement area creation dialog

**Description:** As a user, I need to create areas within sites so that I can define physical zones.

**Acceptance Criteria:**
- [ ] Create `src/components/dialogs/CreateAreaDialog.tsx`
- [ ] Include Select for parent site (filtered by current org)
- [ ] Include Input for "Area Name"
- [ ] Show breadcrumb context: Org > Site > New Area
- [ ] Call `api.createArea()` on submit
- [ ] Refresh hierarchy on success
- [ ] Typecheck passes
- [ ] Verify in browser: site dropdown filters correctly, area appears in tree

---

### US-103: Implement gateway creation dialog

**Description:** As a user, I need to create gateways so that I can connect PLC drivers.

**Acceptance Criteria:**
- [ ] Create `src/components/dialogs/CreateGatewayDialog.tsx`
- [ ] Include fields:
  - name (Input text, required)
  - driver_type (Select: S7, MODBUS_TCP, required)
  - area_id (Select dropdown, required)
- [ ] Include dynamic connection_config form based on driver_type:
  - S7 fields: ip (Input), rack (Input number), slot (Input number), port (Input number, default 102)
  - Modbus fields: ip (Input), port (Input number, default 502), slaveId (Input number)
- [ ] Add scan_rate_ms (Input number, default 1000)
- [ ] Add enabled (Switch/Checkbox, default true)
- [ ] Show JSON preview of connection_config
- [ ] Validate all fields before submit (IP format, port ranges)
- [ ] Call `api.createGateway()` on submit
- [ ] Typecheck passes
- [ ] Verify in browser: driver type switches form, JSON validates, creation works

---

### US-104: Implement tag creation dialog

**Description:** As a user, I need to create tags so that I can define data points to read from PLCs.

**Acceptance Criteria:**
- [ ] Create `src/components/dialogs/CreateTagDialog.tsx` with multi-section form
- [ ] Basic section:
  - code (Input text, PLC address, required)
  - alias (Input text, display name, required)
  - data_type (Select: BOOL, INT, REAL, DINT, required)
  - gateway_id (Select dropdown, required)
- [ ] Historization section:
  - historize (Switch/Checkbox)
  - historize_deadband (Input number, conditional on historize)
- [ ] Alarm section:
  - alarm_enabled (Switch/Checkbox)
  - alarm_threshold (Input number, conditional on alarm_enabled)
  - alarm_operator (Select: >, <, =, !=, conditional)
  - alarm_priority (Input number 1-5, conditional)
- [ ] Validate required fields (code, alias, data_type, gateway_id)
- [ ] Show help text for each field
- [ ] Use Tabs or Card to separate sections
- [ ] Call `api.createTag()` on submit
- [ ] Typecheck passes
- [ ] Verify in browser: all sections render, conditional fields show/hide, validation works

---

### US-105: Add real-time value polling to tree view

**Description:** As a user, I need to see current tag values in the tree so that I can monitor live data.

**Acceptance Criteria:**
- [ ] Add polling state to ConfigPage (isPolling, pollingInterval)
- [ ] Add useEffect that polls `api.getCurrentTagValue()` every 5 seconds
- [ ] Only poll for tags that are currently visible/expanded in tree
- [ ] Display value next to tag name in tree: "Temperature (23.5 °C)"
- [ ] Show quality indicator:
  - Green dot for quality=0 (good)
  - Red dot for quality=1 (bad)
  - Gray dot for no data
- [ ] Add timestamp tooltip: "Last update: 2024-01-24 10:30:45"
- [ ] Add toggle button in header: "Enable Live" / "Live" with RefreshCw icon
- [ ] Stop polling when leaving the page
- [ ] Handle errors gracefully (show warning toast, continue polling)
- [ ] Typecheck passes
- [ ] Verify in browser: values update every 5s, quality indicator works, toggle works

---

### US-106: Implement gateway enabled/disabled toggle

**Description:** As a user, I need to enable/disable gateways so that I can control which drivers are active.

**Acceptance Criteria:**
- [ ] Add Switch component in DetailPanel when gateway node is selected
- [ ] Show current enabled state from gateway data
- [ ] On toggle change: call `api.updateGateway()` with enabled: true/false
- [ ] Show loading state during API call (spinner on switch)
- [ ] Update tree to show enabled/disabled status:
  - Grayed out opacity when disabled
  - Disabled icon indicator
- [ ] Show success toast: "Gateway {name} enabled/disabled"
- [ ] Disable toggle during API call to prevent duplicate requests
- [ ] Typecheck passes
- [ ] Verify in browser: toggle updates gateway status, tree updates

---

### US-107: Create trend/historian page with multi-tag support

**Description:** As a user, I need to view historical data for multiple tags so that I can compare trends.

**Acceptance Criteria:**
- [ ] Create `/trend` route with `src/pages/TrendPage.tsx`
- [ ] Install chart library: `npm install recharts`
- [ ] Add multi-select dropdown for tag selection (filter from all tags)
- [ ] Add date range picker:
  - Start datetime-local input
  - End datetime-local input
  - Quick range buttons: 1h, 6h, 24h, 7d, 30d
- [ ] Set default range: last 24 hours
- [ ] Add aggregation controls:
  - agg (Select: mean, max, min, sum, count)
  - interval (Select: 1m, 5m, 15m, 1h, 1d)
- [ ] Add "Query" button to fetch data
- [ ] Call `api.getHistory()` for each selected tag
- [ ] Display line chart using recharts:
  - Each tag gets different color (8-color palette)
  - Legend showing tag names
  - X-axis: timestamp (formatted)
  - Y-axis: value with auto-scaling
  - Tooltip showing values on hover
- [ ] Add "Export CSV" button that downloads data as CSV file
- [ ] Show loading spinner during fetch
- [ ] Show error message if no data found
- [ ] Add empty state when no tags selected
- [ ] Typecheck passes
- [ ] Verify in browser: multi-tag chart renders, export works, time range works

---

### US-108: Add loading states and error handling components

**Description:** As a user, I need clear feedback during loading and errors so that I understand what's happening.

**Acceptance Criteria:**
- [ ] Create `src/components/ui/skeleton.tsx` with animate-pulse effect
- [ ] Create `src/components/ui/async-wrapper.tsx` component with:
  - Loading state (skeleton or spinner)
  - Error state with message and retry button
  - Empty state with icon and message
- [ ] Create `src/lib/api-error-handler.ts` with utilities:
  - `formatApiError(error)`: converts error to user-friendly message
  - `showApiError(error, toast)`: displays error toast
  - `showApiSuccess(message, toast)`: displays success toast
- [ ] Update API client interceptors:
  - Log slow requests (>3s)
  - Add detailed error logging for 403/401/timeout/network errors
- [ ] Add loading skeletons to:
  - Tree view while loading hierarchy
  - Detail panel while loading entity data
  - Tags table while loading
- [ ] Update all error handling to use new utilities
- [ ] Typecheck passes
- [ ] Verify in browser: skeletons show, errors display with retry button

---

### US-109: Create frontend Docker container

**Description:** As a developer, I need to containerize the frontend for production deployment.

**Acceptance Criteria:**
- [ ] Create `frontend/Dockerfile` with multi-stage build
- [ ] Stage 1: FROM node:20-alpine
  - WORKDIR /app
  - COPY package*.json ./
  - RUN npm ci
  - COPY . .
  - RUN npm run build
- [ ] Stage 2: FROM nginx:alpine
  - COPY --from=1 /app/dist /usr/share/nginx/html
  - Create nginx.conf with SPA routing (try_files $uri /index.html)
  - Configure API proxy to core-api:8080 at /api
  - EXPOSE 80
  - CMD ["nginx", "-g", "daemon off;"]
- [ ] Update docker-compose.yml:
  - Add `web-ui` service
  - Build from `frontend/Dockerfile`
  - Port mapping: 3004:80
  - Depends on core-api with healthcheck
  - Environment: VITE_API_BASE_URL=http://core-api:8080
  - Vite proxy config for dev mode
- [ ] Build image successfully: `docker-compose build web-ui`
- [ ] Container serves app on http://localhost:3004
- [ ] Verify SPA routing works (refresh on /trend loads correctly)

---

### US-110: Create alarms page

**Description:** As an operator, I need to view and manage alarms so that I can monitor system alerts.

**Acceptance Criteria:**
- [ ] Create `/alarms` route with `src/pages/AlarmsPage.tsx`
- [ ] Add navigation link in sidebar to Alarms page (AlertTriangle icon)
- [ ] Fetch alarms on mount: `api.getAlarms()` with X-Organization-ID header
- [ ] Display alarms in Table with columns:
  - Tag (show tag alias and code)
  - State (badge with color coding)
  - Message
  - Priority (badge 1-5)
  - Triggered At (formatted datetime)
  - Actions (Acknowledge button)
- [ ] State badge colors:
  - ACTIVE (red bg-red-100 text-red-800)
  - RTN (yellow bg-yellow-100 text-yellow-800)
  - ACKNOWLEDGED (blue bg-blue-100 text-blue-800)
  - CLEAR (gray bg-gray-100 text-gray-800)
- [ ] Add filter controls:
  - State dropdown (All, ACTIVE, RTN, ACKNOWLEDGED, CLEAR)
  - Tag dropdown (populated from fetched tags)
  - Date range picker (start/end)
  - Apply Filters button
  - Clear Filters button
- [ ] Store filter state in URL search params for shareability
- [ ] Show count of filtered alarms
- [ ] Add 'Acknowledge' button for ACTIVE alarms:
  - Calls `api.acknowledgeAlarm()`
  - Shows success toast
  - Refreshes alarms list
- [ ] Add auto-refresh every 10 seconds
- [ ] Add pause/resume toggle for auto-refresh
- [ ] Add loading spinner during fetch
- [ ] Add empty state: "No alarms found"
- [ ] Typecheck passes
- [ ] Verify in browser: alarms table renders, acknowledge works, auto-refresh functions

---

### US-111: Add alarm filtering enhancements

**Description:** As an operator, I need enhanced filtering so that I can focus on specific alarms.

**Acceptance Criteria:**
- [ ] Add URL search params sync for all filters
- [ ] Show active filter count badge on "Apply Filters" button
- [ ] Add "Clear Filters" button that resets all filters and URL params
- [ ] Add filter persistence (filters restored on page reload)
- [ ] Add visual indicator for active filters (badge count in header)
- [ ] Update `api.getAlarms()` to accept filter parameters
- [ ] Handle edge cases:
  - No alarms match filters (show empty state)
  - Invalid date range (show error)
- [ ] Typecheck passes
- [ ] Verify in browser: filters persist, URL params update, badge count works

---

### US-112: Add alarm detail dialog

**Description:** As an operator, I need to see detailed alarm information so that I can understand the context.

**Acceptance Criteria:**
- [ ] Create `src/components/dialogs/AlarmDetailDialog.tsx` using shadcn Dialog
- [ ] Show when clicking on an alarm row in the table (cursor-pointer, hover effect)
- [ ] Display full alarm details:
  - Tag info (alias, code, gateway, area, site, org)
  - Current state (badge)
  - Message
  - Priority (badge with color: 1=low green, 5=critical red)
  - Triggered At (formatted datetime)
  - Acknowledged At (if applicable)
  - Cleared At (if applicable)
- [ ] Show 'Acknowledge' button if state is ACTIVE
- [ ] Add 'Close' button to dismiss dialog
- [ ] Add link to navigate to tag configuration: "View Tag Configuration"
- [ ] Add note: "State history tracking coming soon"
- [ ] Add dialog title with alarm icon
- [ ] Typecheck passes
- [ ] Verify in browser: dialog opens with correct alarm details, link works

---

### US-113: Add real-time alarm notifications via MQTT

**Description:** As an operator, I need to receive real-time notifications for new alarms so that I can respond quickly.

**Acceptance Criteria:**
- [ ] Install MQTT client: `npm install mqtt`
- [ ] Create `src/lib/mqtt-client.ts` with MQTTClientService:
  - Connect to WebSocket MQTT (ws://localhost:9001/mqtt or via VITE_MQTT_WS_URL)
  - Auto-reconnect on disconnect
  - Subscribe to topic: `events/alarms/#`
  - Parse alarm event payload: `{tag, tag_code, state, message, timestamp, priority}`
  - Emit events via callback or event emitter
- [ ] Create `src/stores/useAlarmStore.ts` with zustand:
  - State: activeAlarmCount, lastAlarmEvent, isMqttConnected
  - Actions: connectMqtt(), disconnectMqtt(), incrementAlarmCount()
  - Persist connection state to localStorage
- [ ] When new ACTIVE alarm received:
  - Show toast notification with alarm details
  - Play notification sound (optional, toggleable)
  - Update alarm badge count in header
- [ ] Add 'Alarms' badge in header navigation:
  - Shows red dot when active alarms exist
  - Shows count number (99+ for large counts)
  - Clicking navigates to /alarms page
- [ ] Add MQTT connection status indicator:
  - Green dot when connected
  - Red dot when disconnected
  - Tooltip: "MQTT Connected" / "MQTT Disconnected"
- [ ] Auto-refresh alarms list when new alarm event received
- [ ] Add notification sound toggle in localStorage (enabled by default)
- [ ] Add environment variables:
  - VITE_MQTT_WS_URL (default: ws://localhost:9001/mqtt)
  - VITE_MQTT_TOPIC (default: events/alarms/#)
- [ ] Typecheck passes
- [ ] Verify in browser: toast appears for new MQTT alarm events, badge updates, status indicator works

---

### US-114: Add notification settings panel

**Description:** As a user, I need to configure notification preferences so that I can control how I receive alerts.

**Acceptance Criteria:**
- [ ] Create `/settings` route with `src/pages/SettingsPage.tsx`
- [ ] Add navigation link in sidebar: "Settings" (Settings icon)
- [ ] Create settings sections using Card components:
  - **Notifications section**:
    - Sound notifications toggle (Switch)
    - Test sound button (plays notification sound)
    - Auto-refresh interval (Select: 5s, 10s, 30s, off)
  - **Display section**:
    - Theme toggle (light/dark - for future implementation)
    - Density (comfortable/compact)
- [ ] Persist all settings to localStorage
- [ ] Load settings on mount
- [ ] Add "Reset to Defaults" button
- [ ] Show success toast when settings saved
- [ ] Typecheck passes
- [ ] Verify in browser: settings persist, sound test works, toggle works

---

## Functional Requirements

### CRUD Dialogs
- FR-1: All create dialogs must use shadcn Dialog component with consistent styling
- FR-2: All dialogs must validate inputs before submission
- FR-3: All dialogs must show loading state during API call
- FR-4: All dialogs must refresh hierarchy/data on success
- FR-5: All dialogs must show success/error toast notifications

### Real-time Features
- FR-6: Tree view must poll tag values every 5 seconds when enabled
- FR-7: Polling must only fetch visible/expanded tags
- FR-8: Quality indicator must show green/red/gray based on quality value
- FR-9: Gateway toggle must call update API and reflect changes immediately

### Trend/Historian
- FR-10: Trend page must support selecting up to 8 tags simultaneously
- FR-11: Chart must use different colors for each tag
- FR-12: Export CSV must download data with proper formatting
- FR-13: Date range must default to last 24 hours

### Alarms
- FR-14: Alarms page must auto-refresh every 10 seconds by default
- FR-15: Alarms must be filterable by state, tag, and date range
- FR-16: Filter state must persist in URL search params
- FR-17: Active alarms must be acknowledgeable from the table
- FR-18: Clicking an alarm row must open detail dialog

### MQTT Notifications
- FR-19: Frontend must connect to MQTT via WebSocket on app mount
- FR-20: Frontend must subscribe to events/alarms/# topic
- FR-21: New ACTIVE alarms must trigger toast notification
- FR-22: Header must show active alarm count badge
- FR-23: Notification sound must be toggleable

### Docker
- FR-24: Frontend must containerize with multi-stage Dockerfile
- FR-25: Nginx must serve SPA with proper routing fallback
- FR-26: Docker must expose port 80 for internal routing
- FR-27: docker-compose must map to port 3004 externally

## Non-Goals (Out of Scope)

- WebSocket implementation for real-time updates (using MQTT over WebSocket)
- Advanced chart features (annotations, multiple Y-axes, zooming)
- Alarm history/audit trail (state changes log)
- User authentication (organization-based only)
- Multi-language support
- Dark mode theme (design system supports but not implementing)

## Technical Considerations

- **Libraries to install**:
  - `recharts` for charts
  - `mqtt` for MQTT client
  - `zustand` for state management (alarm store)
- **API endpoints available**:
  - All CRUD endpoints implemented in backend
  - History endpoint: GET /api/history
  - Alarms endpoint: GET /api/alarms
  - Acknowledge endpoint: POST /api/alarms/{id}/acknowledge
- **MQTT configuration**:
  - WebSocket URL: ws://localhost:9001/mqtt (Mosquitto default)
  - Topic pattern: events/alarms/{org_id}/{tag_id}
- **Design system**: Follow tasks/design-system.md (violet theme, Inter font, shadcn components)

## Success Metrics

- All CRUD operations work within 3 seconds
- Real-time values update within 5 seconds of polling
- Trend charts render within 2 seconds for 24h data
- Alarms page shows new alarms within 10 seconds
- MQTT connection persists across page refreshes
- Docker container builds under 5 minutes
- Total bundle size under 2MB (gzipped)

## Open Questions

- Should polling interval be configurable per user? (Defaulting to 5s)
- Should chart support live-updating mode? (Not in MVP)
- How many historical data points max for chart performance? (Limit to 1000 points per series)
- Should MQTT support username/password authentication? (Not in MVP, Mosquitto allows anonymous)
