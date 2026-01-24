# PRD: Industrial Edge Middleware - Frontend Web UI

## Introduction

Create a modern web-based user interface for the Industrial Edge Middleware system. The frontend enables system integrators to configure the entire multi-tenant hierarchy (organizations, sites, areas, gateways, tags) and visualize historical data trends without using REST APIs directly. The UI targets technical users who need efficient configuration workflows and powerful data analysis capabilities.

**Authentication Note:** The frontend uses Organization ID selection (X-Organization-ID header) for multi-tenant isolation, matching the backend API design. No API key authentication is implemented.

## Goals

- Provide complete CRUD interface for all system entities (Organizations → Sites → Areas → Gateways → Tags)
- Enable historical data visualization with multi-tag comparison and export capabilities
- Offer intuitive split-view interface for efficient navigation and editing
- Reduce configuration time through hierarchical tree navigation and inline editing
- Support organization-based data isolation using X-Organization-ID header

## User Stories

### US-001: Create React + Vite project with TypeScript

**Description:** As a developer, I need to set up the frontend project structure so that we have a modern, type-safe development environment.

**Acceptance Criteria:**

- [ ] Create frontend/ directory with `npm create vite@latest frontend -- --template react-ts`
- [ ] Install Tailwind CSS: `npm install -D tailwindcss postcss autoprefixer` and configure
- [ ] Create folder structure: `src/components`, `src/pages`, `src/lib`, `src/services`, `src/types`
- [ ] Configure Vite proxy to forward /api requests to localhost:8080
- [ ] Verify `npm run dev` starts dev server on port 5173
- [ ] Verify `npm run build` creates production build without errors

### US-002: Install and configure shadcn/ui

**Description:** As a developer, I need to install shadcn/ui components so that we have pre-built, accessible UI components.

**Acceptance Criteria:**

- [ ] Install shadcn/ui CLI: `npm install -D @shadcn/vite-plugin`
- [ ] Configure `components.json` with proper paths for components and utils
- [ ] Install essential components: Button, Input, Card, Table, Dialog, Select, Tabs, Tree, Separator, Badge, Alert, Toast
- [ ] Verify components render in test page
- [ ] Typecheck passes with `npm run type-check`

### US-003: Implement API client with axios

**Description:** As a developer, I need to create an API client service so that all HTTP requests include proper headers and error handling.

**Acceptance Criteria:**

- [ ] Install axios: `npm install axios`
- [ ] Create `src/services/api.ts` with axios instance
- [ ] Configure base URL to `/api` (uses Vite proxy in dev)
- [ ] Add request interceptor to include `X-Organization-ID` header from localStorage
- [ ] Add response interceptor to handle 403/404 (show error) and 500 errors
- [ ] Export typed API functions for each endpoint: getOrganizations, createOrganization, getSites, createSite, getAreas, createArea, getGateways, createGateway, updateGateway, getTags, createTag, updateTag, getTagCurrentValue, getHistory
- [ ] Add TypeScript types matching backend API responses

### US-004: Create organization selection page

**Description:** As a user, I need to select my organization so that I can access the application data.

**Acceptance Criteria:**

- [ ] Create `/login` route with centered card layout
- [ ] On mount: call GET /api/organizations to fetch available organizations
- [ ] Render Select dropdown with organization list (show name and ID)
- [ ] Add "Enter" button that stores selected organization ID in localStorage as `ralph_org_id`
- [ ] On success: redirect to `/config`
- [ ] On error: show error toast message if organizations list is empty
- [ ] Add useEffect to check for existing org ID and redirect if already selected
- [ ] Typecheck passes
- [ ] Verify in browser: org selection works, redirect happens

### US-005: Create main layout with header and navigation

**Description:** As a user, I need a consistent layout with navigation so that I can move between pages easily.

**Acceptance Criteria:**

- [ ] Create `src/components/Layout.tsx` with header and main content area
- [ ] Header shows: application title, current organization name (fetched from API using stored org ID), "Change Organization" button
- [ ] Sidebar shows navigation links: "Configuration" (/config), "Trend/Historian" (/trend)
- [ ] Use React Router with BrowserRouter for client-side routing
- [ ] Add route guard to redirect to /login if no organization selected in localStorage
- [ ] Apply consistent dark/light theme with Tailwind classes
- [ ] Typecheck passes
- [ ] Verify in browser: navigation works, layout persists

### US-006: Create API types from OpenAPI specification

**Description:** As a developer, I need TypeScript types matching the backend API so that we have type safety across the frontend.

**Acceptance Criteria:**

- [ ] Create `src/types/api.ts` with interfaces for all entities
- [ ] Types: Organization, Site, Area, Gateway (with connection_status), Tag, TagValue, HistoryPoint, Alarm
- [ ] Export types for API request/response bodies
- [ ] Ensure types match backend OpenAPI spec at /api/swagger.json
- [ ] Typecheck passes

### US-007: Create hierarchical data service

**Description:** As a developer, I need a service to fetch and transform the hierarchical data so that the UI can display the complete tree structure.

**Acceptance Criteria:**

- [ ] Create `src/services/hierarchy.ts` with fetchHierarchy(orgId) function
- [ ] Fetch sites for organization: GET /api/sites with X-Organization-ID header
- [ ] For each site, fetch areas: GET /api/areas?site_id={id} with X-Organization-ID header
- [ ] For each area, fetch gateways: GET /api/gateways?area_id={id} with X-Organization-ID header
- [ ] For each gateway, fetch tags: GET /api/tags?gateway_id={id} with X-Organization-ID header
- [ ] Transform flat API responses into nested tree structure: TreeNode[] with children
- [ ] Add caching with 30-second TTL to avoid excessive API calls
- [ ] Handle loading and error states
- [ ] Return typed TreeNode[] matching Organization > Site > Area > Gateway > Tag hierarchy

### US-008: Create tree view component for navigation

**Description:** As a user, I need a hierarchical tree view so that I can navigate the organization structure efficiently.

**Acceptance Criteria:**

- [ ] Create `src/components/TreeView.tsx` using shadcn Tree component
- [ ] Render nested tree nodes with icons for each entity type (org, site, area, gateway, tag)
- [ ] Support expand/collapse for nodes with children
- [ ] Highlight selected node
- [ ] Show online/offline status indicator for gateways (green/gray dot)
- [ ] Handle click events to emit node selection
- [ ] Typecheck passes
- [ ] Verify in browser: tree renders correctly, expand/collapse works

### US-009: Create detail panel for entity display and editing

**Description:** As a user, I need a detail panel so that I can view and edit entity properties in a split view.

**Acceptance Criteria:**

- [ ] Create `src/components/DetailPanel.tsx` with two states: view and edit
- [ ] View mode: display all entity fields in a read-only card
- [ ] Edit mode: show form with inputs for editable fields
- [ ] Add "Edit" and "Cancel" buttons for mode switching
- [ ] Add "Save" button that calls appropriate API endpoint (PUT)
- [ ] Add "Delete" button with confirmation dialog
- [ ] Show success/error toast after save/delete operations
- [ ] Typecheck passes
- [ ] Verify in browser: panel updates, edit mode works

### US-010: Create configuration page with split view

**Description:** As a user, I need a configuration page with tree on left and details on right so that I can navigate and edit efficiently.

**Acceptance Criteria:**

- [ ] Create `/config` page route with `src/pages/ConfigPage.tsx`
- [ ] Layout: 30% width tree view on left, 70% detail panel on right
- [ ] On mount: fetch hierarchy data using stored organization ID and populate tree
- [ ] When tree node selected: display entity details in panel
- [ ] Show empty state when no node selected
- [ ] Add "Create" buttons for each entity type in the tree
- [ ] Responsive: stack vertically on mobile (< 768px)
- [ ] Typecheck passes
- [ ] Verify in browser: split view works, selection updates panel

### US-011: Implement organization creation dialog

**Description:** As a user, I need to create organizations so that I can set up new tenants.

**Acceptance Criteria:**

- [ ] Create `src/components/CreateOrgDialog.tsx` using shadcn Dialog
- [ ] Render Input field for "Organization Name"
- [ ] Add "Create" button that validates input (not empty)
- [ ] On submit: call POST /api/organizations with {name} body
- [ ] On success: close dialog, refresh organization list, show success toast
- [ ] On error: show error toast with message from API
- [ ] Typecheck passes
- [ ] Verify in browser: dialog opens/closes, creation works

### US-012: Implement site creation dialog

**Description:** As a user, I need to create sites within organizations so that I can organize locations.

**Acceptance Criteria:**

- [ ] Create `src/components/CreateSiteDialog.tsx`
- [ ] Include Input for "Site Name"
- [ ] On submit: call POST /api/sites with {name} body and X-Organization-ID header
- [ ] Handle success/error with toast notifications
- [ ] After success, refresh hierarchy to show new site
- [ ] Typecheck passes
- [ ] Verify in browser: creation works, site appears in tree

### US-013: Implement area creation dialog

**Description:** As a user, I need to create areas within sites so that I can define physical zones.

**Acceptance Criteria:**

- [ ] Create `src/components/CreateAreaDialog.tsx`
- [ ] Include Select dropdown for parent site (populated from fetched sites)
- [ ] Include Input for "Area Name"
- [ ] On submit: call POST /api/areas with {site_id, name} body and X-Organization-ID header
- [ ] Refresh hierarchy on success
- [ ] Typecheck passes
- [ ] Verify in browser: dialog works, area appears in tree

### US-014: Implement gateway creation dialog

**Description:** As a user, I need to create gateways so that I can connect PLC drivers.

**Acceptance Criteria:**

- [ ] Create `src/components/CreateGatewayDialog.tsx`
- [ ] Include fields: name (Input), driver_type (Select: S7, MODBUS_TCP), area_id (Select)
- [ ] Include JSON editor for connection_config (textarea with validation)
- [ ] Add helper text showing expected JSON structure based on driver_type
- [ ] S7 example: `{"ip": "192.168.1.100", "rack": 0, "slot": 1, "port": 102}`
- [ ] Modbus example: `{"ip": "192.168.1.101", "port": 502, "slaveId": 1, "timeout": 5000}`
- [ ] Validate JSON before submit
- [ ] Call POST /api/gateways with X-Organization-ID header
- [ ] Typecheck passes
- [ ] Verify in browser: JSON editor validates, creation works

### US-015: Implement tag creation dialog

**Description:** As a user, I need to create tags so that I can define data points to read from PLCs.

**Acceptance Criteria:**

- [ ] Create `src/components/CreateTagDialog.tsx` with multi-section form
- [ ] Basic fields: code (Input), alias (Input), data_type (Select: INT, REAL, BOOL, DINT), gateway_id (Select)
- [ ] Historization section: historize (Checkbox), historize_deadband (Input number)
- [ ] Alarm section: alarm_enabled (Checkbox), alarm_threshold (Input), alarm_operator (Select: >, <, =), alarm_priority (Input 1-5)
- [ ] Validate required fields (code, alias, data_type, gateway_id)
- [ ] Call POST /api/tags with X-Organization-ID header
- [ ] Typecheck passes
- [ ] Verify in browser: all sections render, validation works

### US-016: Add real-time value polling to tree view

**Description:** As a user, I need to see current tag values so that I can monitor live data.

**Acceptance Criteria:**

- [ ] Add useEffect in ConfigPage that polls GET /api/tags/{id}/current every 5 seconds
- [ ] Only poll for visible/expanded tags in the tree
- [ ] Display value next to tag name in tree: "Temperature (23.5°C)"
- [ ] Show quality indicator: green dot for q=0 (good), red for q=1 (bad)
- [ ] Add timestamp tooltip: "Last update: 2024-01-24 10:30:45"
- [ ] Add toggle button to enable/disable polling
- [ ] Typecheck passes
- [ ] Verify in browser: values update every 5s, quality indicator works

### US-017: Implement gateway enabled/disabled toggle

**Description:** As a user, I need to enable/disable gateways so that I can control which drivers are active.

**Acceptance Criteria:**

- [ ] Add toggle switch in detail panel when gateway is selected
- [ ] Show current enabled state from gateway data
- [ ] On toggle: call PUT /api/gateways/{id} with {enabled: true/false} body and X-Organization-ID header
- [ ] Update tree to show enabled/disabled status (grayed out when disabled)
- [ ] Show loading state during API call
- [ ] Typecheck passes
- [ ] Verify in browser: toggle updates gateway status

### US-018: Create trend/historian page with multi-tag support

**Description:** As a user, I need to view historical data for multiple tags so that I can compare trends.

**Acceptance Criteria:**

- [ ] Create `/trend` page route with `src/pages/TrendPage.tsx`
- [ ] Add multi-select dropdown for tag selection (populated from fetched tags)
- [ ] Add date range picker with start and end datetime-local inputs
- [ ] Set default range: last 24 hours
- [ ] Add "Query" button to fetch data
- [ ] Add aggregation controls: agg (Select: mean, max, min, sum), interval (Input: 1m, 5m, 1h, 1d)
- [ ] Call GET /api/history?tag_id={id}&start={iso}&end={iso}&agg={agg}&interval={interval} for each selected tag with X-Organization-ID header
- [ ] Display line chart using recharts library (npm install recharts)
- [ ] Each tag gets different color with legend
- [ ] X-axis: timestamp, Y-axis: value
- [ ] Add "Export CSV" button that downloads data as CSV file
- [ ] Show error message if no data found
- [ ] Typecheck passes
- [ ] Verify in browser: multi-tag chart renders, export works

### US-019: Add loading states and error handling

**Description:** As a user, I need clear feedback during loading and errors so that I understand what's happening.

**Acceptance Criteria:**

- [ ] Add loading spinner component using shadcn Spinner
- [ ] Show spinner during all async operations (API calls, data fetching)
- [ ] Add skeleton loaders for tree view and detail panel using shadcn Skeleton
- [ ] Show error messages with retry button for failed API calls
- [ ] Add toast notifications for success feedback using shadcn Toast
- [ ] Handle 403 by showing "Access denied - check your organization selection" error
- [ ] Handle 404 with "Resource not found" error
- [ ] Handle 500 with "Server error, please try again" message
- [ ] If organization ID is missing from localStorage, redirect to /login
- [ ] Typecheck passes
- [ ] Verify in browser: loading states show, errors display correctly

### US-020: Create frontend Docker container

**Description:** As a developer, I need to containerize the frontend for production deployment.

**Acceptance Criteria:**

- [ ] Create `frontend/Dockerfile` with multi-stage build
- [ ] Stage 1: FROM node:18-alpine, RUN npm ci, RUN npm run build
- [ ] Stage 2: FROM nginx:alpine, COPY build to /usr/share/nginx/html
- [ ] Configure nginx.conf to handle SPA routing (try_files $uri /index.html)
- [ ] Expose port 80
- [ ] Add frontend service to docker-compose.yml
- [ ] Set API base URL to core-api service name
- [ ] Build image successfully: `docker-compose build frontend`
- [ ] Container serves app on http://localhost:3000

## Functional Requirements

- FR-1: Frontend uses React 18+ with TypeScript and Vite build system
- FR-2: All API requests include X-Organization-ID header from localStorage
- FR-3: Users select organization on first visit, stored in localStorage
- FR-4: Unselected users are redirected to /login page
- FR-5: Configuration page displays hierarchical tree of all entities
- FR-6: Tree view supports expand/collapse for navigation
- FR-7: Detail panel shows entity properties and enables editing
- FR-8: All CRUD operations (Create, Read, Update, Delete) work for all entities
- FR-9: Gateway connection_config edited as JSON with validation
- FR-10: Tag values poll every 5 seconds when polling is enabled
- FR-11: Trend page supports multi-tag selection and comparison
- FR-12: Trend chart data exportable as CSV
- FR-13: All API errors display user-friendly messages
- FR-14: Frontend containerized with nginx and deployable via docker-compose

## Non-Goals (Out of Scope)

- **No dashboard home page** - no KPIs, statistics, or overview widgets
- **No user authentication** - organization selection only, no login/password
- **No user management or role-based access control**
- **No alarm visualization or management interface**
- **No real-time WebSocket connections** (uses polling instead)
- **No multi-language support**
- **No dark mode toggle** (uses system default)
- **No mobile app** or responsive design beyond basic CSS media queries
- **No data export beyond CSV** for trend charts
- **No advanced chart features** (annotations, custom ranges, compare modes)

## Design Considerations

### UI/UX Requirements

- Use shadcn/ui components for consistent design system
- Follow shadcn/ui default theme with Tailwind CSS
- Typography: Inter or system font
- Color scheme: Professional industrial aesthetic (blues, grays)
- Icon library: Lucide React (included with shadcn/ui)

### Authentication Flow

```
1. User opens app → Check localStorage for organization ID
2. If not found → Redirect to /login
3. /login page:
   - Fetch organizations list: GET /api/organizations
   - Show dropdown with organizations
   - User selects org → Store org_id in localStorage
   - Redirect to /config
4. All API calls include: X-Organization-ID: <stored_org_id> header
```

### Screen Layouts

**Login Page:**
- Centered card on gray background
- Logo/title at top
- Organization dropdown (fetched from API)
- Enter button

**Configuration Page (Split View):**
```
+------------------+---------------------------------------+
| Tree View        | Detail Panel                          |
| (30% width)      | (70% width)                           |
|                  |                                       |
| ▶ Org A          | Gateway: PLC-1                        |
|   ▶ Site 1       | Driver: S7                            |
|     ▶ Area 1     | Status: Online ●                      |
|       ▶ GW-1     | IP: 192.168.1.100                    |
|         Tag-1    | Scan Rate: 1000ms                    |
|         Tag-2     | [Edit] [Delete]                       |
|       ▶ GW-2     +---------------------------------------+
|                  | + Create Gateway                      |
| ▶ Org B          | + Create Site                         |
+------------------+
```

**Trend Page:**
```
+-----------------------------------------------------------+
| Tag Select: [Tag-1, Tag-2, Tag-3 ▼]  Date: [01/20] to [01/24] |
|                                                           |
| Aggregation: [mean ▼] Interval: [1m]    [Query] [Export CSV]|
|                                                           |
|  50 ┤                                    ╭──── Tag-1      |
|  40 ┤                         ╭─── Tag-2 ╯              |
|  30 ┤            Tag-3 ╭────╯                            |
|  20 ┤        ╭────╯                                    |
|  10 ┤   ╭───╯                                          |
|   0 ┼───┴────────────────────────────────────────────>  |
|      10:00    11:00    12:00    13:00    14:00          |
+-----------------------------------------------------------+
```

### Component Reuse

- Reuse shadcn/ui Button, Input, Select, Dialog, Table components
- Create custom TreeView component (extend shadcn Tree)
- Create custom DetailPanel component for entity editing
- Create custom JsonEditor component for connection_config

## Technical Considerations

### Dependencies

```json
{
  "react": "^18.2.0",
  "react-dom": "^18.2.0",
  "react-router-dom": "^6.20.0",
  "axios": "^1.6.0",
  "recharts": "^2.10.0",
  "tailwindcss": "^3.3.0",
  "@radix-ui/react-*": "latest (shadcn/ui deps)",
  "lucide-react": "^0.294.0"
}
```

### State Management

- Use React useState/useContext for local state
- No global state management library (Redux/Zustand) required
- Store organization ID in localStorage
- API data cached in service layer with simple TTL

### Performance

- Tree virtualization if entity count exceeds 1000
- Debounce search/filter inputs (300ms)
- Pagination for tag lists if count exceeds 100
- Lazy load chart data (only fetch when Query clicked)

### Security

- Organization ID stored in localStorage
- No sensitive data in URL params
- All API calls over HTTPS in production
- Multi-tenant isolation via X-Organization-ID header
- XSS protection via React automatic escaping

## Success Metrics

- Time to select organization and load hierarchy < 3 seconds
- Time to create complete org hierarchy (1 org, 1 site, 1 area, 1 gateway, 5 tags) < 5 minutes
- Time to query and display trend chart for 3 tags < 10 seconds
- Zero console errors in production build
- Lighthouse score > 90 for Performance and Accessibility
- 100% TypeScript coverage (no any types)

## Open Questions

- Should organization ID be stored in httpOnly cookie instead of localStorage for better security?
- Should we add WebSocket support for real-time tag value updates instead of polling?
- Should the trend chart support more advanced features like zooming, panning, annotations?
- Should we add a dark mode toggle?

