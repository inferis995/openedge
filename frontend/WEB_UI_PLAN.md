# Piano di Implementazione: Web UI di Configurazione - Industrial Edge Middleware

## Obiettivo
Creare una Web UI completa con React + TypeScript per configurare e gestire tutto il sistema: Organizzazioni, Sites, Areas, Gateways (S7/Modbus), Tag, Allarmi.

---

## Stack Tecnologico

### Frontend
- **React 18** + TypeScript
- **Vite** (build tool)
- **shadcn/ui** (Radix UI + Tailwind CSS)
- **React Query** (TanStack Query) per data fetching
- **Zustand** per state management globale
- **Axios** per HTTP client
- **React Router v7** per routing
- **React Hook Form + Zod** per validazione form
- **TanStack Table** per tabelle
- **Lucide React** per icone

---

## Modifiche ai Servizi Esistenti

### 1. CORS in core-api

**File da modificare:** `services/core-api/main.go`

Aggiungere import:
```go
import "github.com/gin-contrib/cors"
```

Aggiungere middleware CORS dopo aver creato il router:
```go
// Create Gin router
router := gin.Default()

// Configure CORS
router.Use(cors.New(cors.Config{
    AllowOrigins:     []string{"http://localhost:3000", "http://127.0.0.1:3000"},
    AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
    AllowHeaders:     []string{"Origin", "Content-Type", "Authorization"},
    AllowCredentials: true,
    MaxAge:           12 * time.Hour,
}))
```

Aggiornare `go.mod`:
```bash
go get github.com/gin-contrib/cors
```

---

### 2. Nuovi Endpoint API da Aggiungere

**File da modificare:** `services/core-api/main.go` e handlers

Aggiungere questi endpoint mancanti:

#### Gateways
- `GET /api/gateways/:id` - Dettagli singolo gateway
- `DELETE /api/gateways/:id` - Elimina gateway

#### Tags
- `GET /api/tags/:id` - Dettagli singolo tag
- `DELETE /api/tags/:id` - Elimina tag

#### Alarms (nuovi)
- `GET /api/alarms` - Lista allarmi con filtri (stato, priorità, tag_id)
- `POST /api/alarms/:id/acknowledge` - Acknowledge allarme
- `GET /api/alarms/:id` - Dettagli allarme

#### Health
- `GET /api/health` - Health check con statistiche sistema (totale org, sites, gateways, tag, alarms attivi)

---

### 3. Aggiornamento docker-compose.yml

Aggiungere il nuovo servizio `web-ui`:

```yaml
  web-ui:
    build:
      context: .
      dockerfile: services/web-ui/Dockerfile
    container_name: industrial-web-ui
    environment:
      - VITE_API_URL=http://localhost:8080/api
      # In produzione usare: http://core-api:8080/api
    ports:
      - "3000:3000"
    depends_on:
      - core-api
    networks:
      - industrial-network
```

---

## Struttura del Servizio web-ui

```
services/web-ui/
├── Dockerfile
├── package.json
├── tsconfig.json
├── tsconfig.node.json
├── vite.config.ts
├── tailwind.config.js
├── postcss.config.js
├── index.html
├── .eslintrc.cjs
└── src/
    ├── main.tsx
    ├── App.tsx
    ├── vite-env.d.ts
    ├── api/
    │   ├── client.ts           # Axios configuration
    │   ├── organizations.ts
    │   ├── sites.ts
    │   ├── areas.ts
    │   ├── gateways.ts
    │   ├── tags.ts
    │   └── alarms.ts
    ├── types/
    │   └── index.ts            # TypeScript interfaces
    ├── components/
    │   ├── ui/                 # shadcn/ui components
    │   │   ├── button.tsx
    │   │   ├── input.tsx
    │   │   ├── label.tsx
    │   │   ├── select.tsx
    │   │   ├── dialog.tsx
    │   │   ├── table.tsx
    │   │   ├── badge.tsx
    │   │   ├── card.tsx
    │   │   ├── toast.tsx
    │   │   ├── breadcrumb.tsx
    │   │   └── separator.tsx
    │   ├── layout/
    │   │   ├── Sidebar.tsx
    │   │   ├── Header.tsx
    │   │   └── MainLayout.tsx
    │   ├── organizations/
    │   │   ├── OrganizationsTable.tsx
    │   │   └── OrganizationForm.tsx
    │   ├── sites/
    │   │   ├── SitesTable.tsx
    │   │   └── SiteForm.tsx
    │   ├── areas/
    │   │   ├── AreasTable.tsx
    │   │   └── AreaForm.tsx
    │   ├── gateways/
    │   │   ├── GatewaysTable.tsx
    │   │   ├── GatewayForm.tsx
    │   │   └── DriverConfigForm.tsx
    │   ├── tags/
    │   │   ├── TagsTable.tsx
    │   │   └── TagForm.tsx
    │   └── alarms/
    │       ├── AlarmsTable.tsx
    │       └── AlarmDetailDialog.tsx
    ├── pages/
    │   ├── DashboardPage.tsx
    │   ├── OrganizationsPage.tsx
    │   ├── SitesPage.tsx
    │   ├── AreasPage.tsx
    │   ├── GatewaysPage.tsx
    │   ├── TagsPage.tsx
    │   └── AlarmsPage.tsx
    ├── hooks/
    │   ├── useOrganizations.ts
    │   ├── useSites.ts
    │   ├── useAreas.ts
    │   ├── useGateways.ts
    │   ├── useTags.ts
    │   ├── useAlarms.ts
    │   └── useHealth.ts
    ├── stores/
    │   └── useNavigationStore.ts
    └── lib/
        └── utils.ts
```

---

## Dettaglio File da Creare

### 1. Dockerfile

**File:** `services/web-ui/Dockerfile`

```dockerfile
# Build stage
FROM node:20-alpine AS builder

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY . .
RUN npm run build

# Production stage
FROM nginx:alpine

COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 3000

CMD ["nginx", "-g", "daemon off;"]
```

### 2. nginx.conf

**File:** `services/web-ui/nginx.conf`

```nginx
server {
    listen 3000;
    root /usr/share/nginx/html;
    index index.html;

    location / {
        try_files $uri $uri/ /index.html;
    }

    location /api {
        proxy_pass http://core-api:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    gzip on;
    gzip_types text/plain text/css application/json application/javascript;
}
```

### 3. package.json

**File:** `services/web-ui/package.json`

```json
{
  "name": "industrial-edge-web-ui",
  "private": true,
  "version": "1.0.0",
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "tsc && vite build",
    "preview": "vite preview",
    "lint": "eslint . --ext ts,tsx --report-unused-disable-directives --max-warnings 0"
  },
  "dependencies": {
    "@tanstack/react-query": "^5.17.0",
    "@tanstack/react-table": "^8.11.0",
    "axios": "^1.6.0",
    "class-variance-authority": "^0.7.0",
    "clsx": "^2.0.0",
    "date-fns": "^3.0.0",
    "lucide-react": "^0.303.0",
    "react": "^18.2.0",
    "react-dom": "^18.2.0",
    "react-hook-form": "^7.49.0",
    "react-router-dom": "^7.0.0",
    "tailwind-merge": "^2.2.0",
    "tailwindcss-animate": "^1.0.7",
    "zod": "^3.22.0",
    "zustand": "^4.4.0"
  },
  "devDependencies": {
    "@radix-ui/react-dialog": "^1.0.5",
    "@radix-ui/react-dropdown-menu": "^2.0.6",
    "@radix-ui/react-label": "^2.0.2",
    "@radix-ui/react-select": "^2.0.0",
    "@radix-ui/react-separator": "^1.0.3",
    "@radix-ui/react-slot": "^1.0.2",
    "@radix-ui/react-toast": "^1.1.5",
    "@types/node": "^20.10.0",
    "@types/react": "^18.2.0",
    "@types/react-dom": "^18.2.0",
    "@typescript-eslint/eslint-plugin": "^6.15.0",
    "@typescript-eslint/parser": "^6.15.0",
    "@vitejs/plugin-react": "^4.2.0",
    "autoprefixer": "^10.4.16",
    "eslint": "^8.56.0",
    "eslint-plugin-react-hooks": "^4.6.0",
    "eslint-plugin-react-refresh": "^0.4.5",
    "postcss": "^8.4.32",
    "tailwindcss": "^3.4.0",
    "typescript": "^5.3.0",
    "vite": "^5.0.0"
  }
}
```

### 4. vite.config.ts

**File:** `services/web-ui/vite.config.ts`

```typescript
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      '@': path.resolve(__dirname, './src'),
    },
  },
  server: {
    port: 3000,
    proxy: {
      '/api': {
        target: process.env.VITE_API_URL || 'http://localhost:8080',
        changeOrigin: true,
      },
    },
  },
})
```

### 5. tailwind.config.js

**File:** `services/web-ui/tailwind.config.js`

```javascript
/** @type {import('tailwindcss').Config} */
export default {
  darkMode: ["class"],
  content: [
    './pages/**/*.{ts,tsx}',
    './components/**/*.{ts,tsx}',
    './app/**/*.{ts,tsx}',
    './src/**/*.{ts,tsx}',
  ],
  prefix: "",
  theme: {
    container: {
      center: true,
      padding: "2rem",
      screens: {
        "2xl": "1400px",
      },
    },
    extend: {
      colors: {
        border: "hsl(var(--border))",
        input: "hsl(var(--input))",
        ring: "hsl(var(--ring))",
        background: "hsl(var(--background))",
        foreground: "hsl(var(--foreground))",
        primary: {
          DEFAULT: "hsl(var(--primary))",
          foreground: "hsl(var(--primary-foreground))",
        },
        secondary: {
          DEFAULT: "hsl(var(--secondary))",
          foreground: "hsl(var(--secondary-foreground))",
        },
        destructive: {
          DEFAULT: "hsl(var(--destructive))",
          foreground: "hsl(var(--destructive-foreground))",
        },
        muted: {
          DEFAULT: "hsl(var(--muted))",
          foreground: "hsl(var(--muted-foreground))",
        },
        accent: {
          DEFAULT: "hsl(var(--accent))",
          foreground: "hsl(var(--accent-foreground))",
        },
        popover: {
          DEFAULT: "hsl(var(--popover))",
          foreground: "hsl(var(--popover-foreground))",
        },
        card: {
          DEFAULT: "hsl(var(--card))",
          foreground: "hsl(var(--card-foreground))",
        },
      },
      borderRadius: {
        lg: "var(--radius)",
        md: "calc(var(--radius) - 2px)",
        sm: "calc(var(--radius) - 4px)",
      },
      keyframes: {
        "accordion-down": {
          from: { height: "0" },
          to: { height: "var(--radix-accordion-content-height)" },
        },
        "accordion-up": {
          from: { height: "var(--radix-accordion-content-height)" },
          to: { height: "0" },
        },
      },
      animation: {
        "accordion-down": "accordion-down 0.2s ease-out",
        "accordion-up": "accordion-up 0.2s ease-out",
      },
    },
  },
  plugins: [require("tailwindcss-animate")],
}
```

### 6. postcss.config.js

**File:** `services/web-ui/postcss.config.js`

```javascript
export default {
  plugins: {
    tailwindcss: {},
    autoprefixer: {},
  },
}
```

### 7. tsconfig.json

**File:** `services/web-ui/tsconfig.json`

```json
{
  "compilerOptions": {
    "target": "ES2020",
    "useDefineForClassFields": true,
    "lib": ["ES2020", "DOM", "DOM.Iterable"],
    "module": "ESNext",
    "skipLibCheck": true,
    "moduleResolution": "bundler",
    "allowImportingTsExtensions": true,
    "resolveJsonModule": true,
    "isolatedModules": true,
    "noEmit": true,
    "jsx": "react-jsx",
    "strict": true,
    "noUnusedLocals": true,
    "noUnusedParameters": true,
    "noFallthroughCasesInSwitch": true,
    "baseUrl": ".",
    "paths": {
      "@/*": ["./src/*"]
    }
  },
  "include": ["src"],
  "references": [{ "path": "./tsconfig.node.json" }]
}
```

### 8. tsconfig.node.json

**File:** `services/web-ui/tsconfig.node.json`

```json
{
  "compilerOptions": {
    "composite": true,
    "skipLibCheck": true,
    "module": "ESNext",
    "moduleResolution": "bundler",
    "allowSyntheticDefaultImports": true
  },
  "include": ["vite.config.ts"]
}
```

### 9. index.html

**File:** `services/web-ui/index.html`

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <link rel="icon" type="image/svg+xml" href="/vite.svg" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
    <title>Industrial Edge Middleware - Configuration UI</title>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

---

## File Sorgente Principali

### 10. main.tsx

**File:** `services/web-ui/src/main.tsx`

```typescript
import React from 'react'
import ReactDOM from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import App from './App'
import './index.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: false,
      retry: 1,
      staleTime: 5000,
    },
  },
})

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
    </QueryClientProvider>
  </React.StrictMode>,
)
```

### 11. index.css

**File:** `services/web-ui/src/index.css`

```css
@tailwind base;
@tailwind components;
@tailwind utilities;

@layer base {
  :root {
    --background: 0 0% 100%;
    --foreground: 222.2 84% 4.9%;
    --card: 0 0% 100%;
    --card-foreground: 222.2 84% 4.9%;
    --popover: 0 0% 100%;
    --popover-foreground: 222.2 84% 4.9%;
    --primary: 221.2 83.2% 53.3%;
    --primary-foreground: 210 40% 98%;
    --secondary: 210 40% 96.1%;
    --secondary-foreground: 222.2 47.4% 11.2%;
    --muted: 210 40% 96.1%;
    --muted-foreground: 215.4 16.3% 46.9%;
    --accent: 210 40% 96.1%;
    --accent-foreground: 222.2 47.4% 11.2%;
    --destructive: 0 84.2% 60.2%;
    --destructive-foreground: 210 40% 98%;
    --border: 214.3 31.8% 91.4%;
    --input: 214.3 31.8% 91.4%;
    --ring: 221.2 83.2% 53.3%;
    --radius: 0.5rem;
  }

  .dark {
    --background: 222.2 84% 4.9%;
    --foreground: 210 40% 98%;
    --card: 222.2 84% 4.9%;
    --card-foreground: 210 40% 98%;
    --popover: 222.2 84% 4.9%;
    --popover-foreground: 210 40% 98%;
    --primary: 217.2 91.2% 59.8%;
    --primary-foreground: 222.2 47.4% 11.2%;
    --secondary: 217.2 32.6% 17.5%;
    --secondary-foreground: 210 40% 98%;
    --muted: 217.2 32.6% 17.5%;
    --muted-foreground: 215 20.2% 65.1%;
    --accent: 217.2 32.6% 17.5%;
    --accent-foreground: 210 40% 98%;
    --destructive: 0 62.8% 30.6%;
    --destructive-foreground: 210 40% 98%;
    --border: 217.2 32.6% 17.5%;
    --input: 217.2 32.6% 17.5%;
    --ring: 224.3 76.3% 48%;
  }
}

@layer base {
  * {
    @apply border-border;
  }
  body {
    @apply bg-background text-foreground;
  }
}
```

### 12. App.tsx

**File:** `services/web-ui/src/App.tsx`

```typescript
import { BrowserRouter, Routes, Route } from 'react-router-dom'
import { MainLayout } from './components/layout/MainLayout'
import { DashboardPage } from './pages/DashboardPage'
import { OrganizationsPage } from './pages/OrganizationsPage'
import { SitesPage } from './pages/SitesPage'
import { AreasPage } from './pages/AreasPage'
import { GatewaysPage } from './pages/GatewaysPage'
import { TagsPage } from './pages/TagsPage'
import { AlarmsPage } from './pages/AlarmsPage'

function App() {
  return (
    <BrowserRouter>
      <Routes>
        <Route path="/" element={<MainLayout />}>
          <Route index element={<DashboardPage />} />
          <Route path="organizations" element={<OrganizationsPage />} />
          <Route path="sites" element={<SitesPage />} />
          <Route path="areas" element={<AreasPage />} />
          <Route path="gateways" element={<GatewaysPage />} />
          <Route path="tags" element={<TagsPage />} />
          <Route path="alarms" element={<AlarmsPage />} />
        </Route>
      </Routes>
    </BrowserRouter>
  )
}

export default App
```

---

## API Client e Types

### 13. types/index.ts

**File:** `services/web-ui/src/types/index.ts`

```typescript
export interface Organization {
  id: number
  name: string
  created_at: string
}

export interface Site {
  id: number
  org_id: number
  name: string
  created_at: string
  organization?: Organization
}

export interface Area {
  id: number
  site_id: number
  name: string
  created_at: string
  site?: Site
}

export type DriverType = 'S7' | 'MODBUS_TCP'

export interface ConnectionConfig {
  ip: string
  rack?: number  // S7 only
  slot?: number  // S7 only
  slave_id?: number  // Modbus only
  port?: number  // Modbus only
}

export interface Gateway {
  id: number
  area_id: number
  name: string
  driver_type: DriverType
  connection_config: ConnectionConfig
  scan_rate_ms: number
  enabled: boolean
  created_at: string
  area?: Area
  tags?: Tag[]
}

export type DataType = 'INT' | 'REAL' | 'BOOL' | 'DINT'
export type AlarmOperator = '>' | '<' | '='
export type AlarmPriority = 1 | 2 | 3 | 4 | 5

export interface Tag {
  id: number
  gateway_id: number
  code: string
  alias: string
  data_type: DataType
  historize: boolean
  historize_deadband: number
  alarm_enabled: boolean
  alarm_threshold: number
  alarm_operator: AlarmOperator
  alarm_priority: AlarmPriority
  created_at: string
  gateway?: Gateway
}

export type AlarmState = 'ACTIVE' | 'RTN' | 'ACKNOWLEDGED' | 'CLEAR'

export interface Alarm {
  id: number
  tag_id: number
  state: AlarmState
  message: string
  triggered_at: string
  acknowledged_at?: string
  cleared_at?: string
  tag?: Tag
}

export interface HealthStats {
  organizations_count: number
  sites_count: number
  areas_count: number
  gateways_count: number
  tags_count: number
  active_alarms_count: number
  online_gateways_count: number
}

// Create DTOs
export interface CreateOrganizationDto {
  name: string
}

export interface CreateSiteDto {
  org_id: number
  name: string
}

export interface CreateAreaDto {
  site_id: number
  name: string
}

export interface CreateGatewayDto {
  area_id: number
  name: string
  driver_type: DriverType
  connection_config: ConnectionConfig
  scan_rate_ms: number
  enabled: boolean
}

export interface CreateTagDto {
  gateway_id: number
  code: string
  alias: string
  data_type: DataType
  historize: boolean
  historize_deadband: number
  alarm_enabled: boolean
  alarm_threshold: number
  alarm_operator: AlarmOperator
  alarm_priority: AlarmPriority
}

export interface UpdateGatewayDto {
  name?: string
  connection_config?: ConnectionConfig
  scan_rate_ms?: number
  enabled?: boolean
}

export interface UpdateTagDto {
  code?: string
  alias?: string
  data_type?: DataType
  historize?: boolean
  historize_deadband?: number
  alarm_enabled?: boolean
  alarm_threshold?: number
  alarm_operator?: AlarmOperator
  alarm_priority?: AlarmPriority
}
```

### 14. api/client.ts

**File:** `services/web-ui/src/api/client.ts`

```typescript
import axios from 'axios'

const API_URL = import.meta.env.VITE_API_URL || 'http://localhost:8080/api'

export const apiClient = axios.create({
  baseURL: API_URL,
  headers: {
    'Content-Type': 'application/json',
  },
})

// Request interceptor
apiClient.interceptors.request.use(
  (config) => {
    return config
  },
  (error) => {
    return Promise.reject(error)
  }
)

// Response interceptor
apiClient.interceptors.response.use(
  (response) => response,
  (error) => {
    console.error('API Error:', error.response?.data || error.message)
    return Promise.reject(error)
  }
)
```

### 15. api/organizations.ts

**File:** `services/web-ui/src/api/organizations.ts`

```typescript
import { apiClient } from './client'
import type { Organization, CreateOrganizationDto } from '@/types'

export const organizationsApi = {
  list: async (): Promise<Organization[]> => {
    const response = await apiClient.get<Organization[]>('/organizations')
    return response.data
  },

  create: async (dto: CreateOrganizationDto): Promise<Organization> => {
    const response = await apiClient.post<Organization>('/organizations', dto)
    return response.data
  },

  delete: async (id: number): Promise<void> => {
    await apiClient.delete(`/organizations/${id}`)
  },
}
```

### 16. api/sites.ts

**File:** `services/web-ui/src/api/sites.ts`

```typescript
import { apiClient } from './client'
import type { Site, CreateSiteDto } from '@/types'

export const sitesApi = {
  list: async (orgId?: number): Promise<Site[]> => {
    const params = orgId ? { org_id: orgId } : {}
    const response = await apiClient.get<Site[]>('/sites', { params })
    return response.data
  },

  create: async (dto: CreateSiteDto): Promise<Site> => {
    const response = await apiClient.post<Site>('/sites', dto)
    return response.data
  },

  delete: async (id: number): Promise<void> => {
    await apiClient.delete(`/sites/${id}`)
  },
}
```

### 17. api/areas.ts

**File:** `services/web-ui/src/api/areas.ts`

```typescript
import { apiClient } from './client'
import type { Area, CreateAreaDto } from '@/types'

export const areasApi = {
  list: async (siteId?: number): Promise<Area[]> => {
    const params = siteId ? { site_id: siteId } : {}
    const response = await apiClient.get<Area[]>('/areas', { params })
    return response.data
  },

  create: async (dto: CreateAreaDto): Promise<Area> => {
    const response = await apiClient.post<Area>('/areas', dto)
    return response.data
  },

  delete: async (id: number): Promise<void> => {
    await apiClient.delete(`/areas/${id}`)
  },
}
```

### 18. api/gateways.ts

**File:** `services/web-ui/src/api/gateways.ts`

```typescript
import { apiClient } from './client'
import type { Gateway, CreateGatewayDto, UpdateGatewayDto } from '@/types'

export const gatewaysApi = {
  list: async (areaId?: number): Promise<Gateway[]> => {
    const params = areaId ? { area_id: areaId } : {}
    const response = await apiClient.get<Gateway[]>('/gateways', { params })
    return response.data
  },

  getById: async (id: number): Promise<Gateway> => {
    const response = await apiClient.get<Gateway>(`/gateways/${id}`)
    return response.data
  },

  create: async (dto: CreateGatewayDto): Promise<Gateway> => {
    const response = await apiClient.post<Gateway>('/gateways', dto)
    return response.data
  },

  update: async (id: number, dto: UpdateGatewayDto): Promise<Gateway> => {
    const response = await apiClient.put<Gateway>(`/gateways/${id}`, dto)
    return response.data
  },

  delete: async (id: number): Promise<void> => {
    await apiClient.delete(`/gateways/${id}`)
  },
}
```

### 19. api/tags.ts

**File:** `services/web-ui/src/api/tags.ts`

```typescript
import { apiClient } from './client'
import type { Tag, CreateTagDto, UpdateTagDto } from '@/types'

export const tagsApi = {
  list: async (gatewayId?: number): Promise<Tag[]> => {
    const params = gatewayId ? { gateway_id: gatewayId } : {}
    const response = await apiClient.get<Tag[]>('/tags', { params })
    return response.data
  },

  getById: async (id: number): Promise<Tag> => {
    const response = await apiClient.get<Tag>(`/tags/${id}`)
    return response.data
  },

  create: async (dto: CreateTagDto): Promise<Tag> => {
    const response = await apiClient.post<Tag>('/tags', dto)
    return response.data
  },

  update: async (id: number, dto: UpdateTagDto): Promise<Tag> => {
    const response = await apiClient.put<Tag>(`/tags/${id}`, dto)
    return response.data
  },

  delete: async (id: number): Promise<void> => {
    await apiClient.delete(`/tags/${id}`)
  },
}
```

### 20. api/alarms.ts

**File:** `services/web-ui/src/api/alarms.ts`

```typescript
import { apiClient } from './client'
import type { Alarm } from '@/types'

export const alarmsApi = {
  list: async (params?: {
    state?: string
    priority?: number
    tag_id?: number
  }): Promise<Alarm[]> => {
    const response = await apiClient.get<Alarm[]>('/alarms', { params })
    return response.data
  },

  getById: async (id: number): Promise<Alarm> => {
    const response = await apiClient.get<Alarm>(`/alarms/${id}`)
    return response.data
  },

  acknowledge: async (id: number): Promise<void> => {
    await apiClient.post(`/alarms/${id}/acknowledge`)
  },
}
```

### 21. api/health.ts

**File:** `services/web-ui/src/api/health.ts`

```typescript
import { apiClient } from './client'
import type { HealthStats } from '@/types'

export const healthApi = {
  getStats: async (): Promise<HealthStats> => {
    const response = await apiClient.get<HealthStats>('/health')
    return response.data
  },
}
```

---

## Componenti UI Base (shadcn/ui)

### 22. lib/utils.ts

**File:** `services/web-ui/src/lib/utils.ts`

```typescript
import { type ClassValue, clsx } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}
```

### 23. components/ui/button.tsx

**File:** `services/web-ui/src/components/ui/button.tsx`

```typescript
import * as React from "react"
import { Slot } from "@radix-ui/react-slot"
import { cva, type VariantProps } from "class-variance-authority"
import { cn } from "@/lib/utils"

const buttonVariants = cva(
  "inline-flex items-center justify-center whitespace-nowrap rounded-md text-sm font-medium ring-offset-background transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:pointer-events-none disabled:opacity-50",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground hover:bg-primary/90",
        destructive:
          "bg-destructive text-destructive-foreground hover:bg-destructive/90",
        outline:
          "border border-input bg-background hover:bg-accent hover:text-accent-foreground",
        secondary:
          "bg-secondary text-secondary-foreground hover:bg-secondary/80",
        ghost: "hover:bg-accent hover:text-accent-foreground",
        link: "text-primary underline-offset-4 hover:underline",
      },
      size: {
        default: "h-10 px-4 py-2",
        sm: "h-9 rounded-md px-3",
        lg: "h-11 rounded-md px-8",
        icon: "h-10 w-10",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
)

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : "button"
    return (
      <Comp
        className={cn(buttonVariants({ variant, size, className }))}
        ref={ref}
        {...props}
      />
    )
  }
)
Button.displayName = "Button"

export { Button, buttonVariants }
```

### 24. components/ui/input.tsx

**File:** `services/web-ui/src/components/ui/input.tsx`

```typescript
import * as React from "react"
import { cn } from "@/lib/utils"

export interface InputProps
  extends React.InputHTMLAttributes<HTMLInputElement> {}

const Input = React.forwardRef<HTMLInputElement, InputProps>(
  ({ className, type, ...props }, ref) => {
    return (
      <input
        type={type}
        className={cn(
          "flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background file:border-0 file:bg-transparent file:text-sm file:font-medium placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50",
          className
        )}
        ref={ref}
        {...props}
      />
    )
  }
)
Input.displayName = "Input"

export { Input }
```

### 25. components/ui/label.tsx

**File:** `services/web-ui/src/components/ui/label.tsx`

```typescript
import * as React from "react"
import * as LabelPrimitive from "@radix-ui/react-label"
import { cva, type VariantProps } from "class-variance-authority"
import { cn } from "@/lib/utils"

const labelVariants = cva(
  "text-sm font-medium leading-none peer-disabled:cursor-not-allowed peer-disabled:opacity-70"
)

const Label = React.forwardRef<
  React.ElementRef<typeof LabelPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof LabelPrimitive.Root> &
    VariantProps<typeof labelVariants>
>(({ className, ...props }, ref) => (
  <LabelPrimitive.Root
    ref={ref}
    className={cn(labelVariants(), className)}
    {...props}
  />
))
Label.displayName = LabelPrimitive.Root.displayName

export { Label }
```

### 26. components/ui/select.tsx

**File:** `services/web-ui/src/components/ui/select.tsx`

```typescript
import * as React from "react"
import * as SelectPrimitive from "@radix-ui/react-select"
import { Check, ChevronDown, ChevronUp } from "lucide-react"
import { cn } from "@/lib/utils"

const Select = SelectPrimitive.Root

const SelectGroup = SelectPrimitive.Group

const SelectValue = SelectPrimitive.Value

const SelectTrigger = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Trigger>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.Trigger>
>(({ className, children, ...props }, ref) => (
  <SelectPrimitive.Trigger
    ref={ref}
    className={cn(
      "flex h-10 w-full items-center justify-between rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 [&>span]:line-clamp-1",
      className
    )}
    {...props}
  >
    {children}
    <SelectPrimitive.Icon asChild>
      <ChevronDown className="h-4 w-4 opacity-50" />
    </SelectPrimitive.Icon>
  </SelectPrimitive.Trigger>
))
SelectTrigger.displayName = SelectPrimitive.Trigger.displayName

const SelectScrollUpButton = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.ScrollUpButton>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.ScrollUpButton>
>(({ className, ...props }, ref) => (
  <SelectPrimitive.ScrollUpButton
    ref={ref}
    className={cn(
      "flex cursor-default items-center justify-center py-1",
      className
    )}
    {...props}
  >
    <ChevronUp className="h-4 w-4" />
  </SelectPrimitive.ScrollUpButton>
))
SelectScrollUpButton.displayName = SelectPrimitive.ScrollUpButton.displayName

const SelectScrollDownButton = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.ScrollDownButton>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.ScrollDownButton>
>(({ className, ...props }, ref) => (
  <SelectPrimitive.ScrollDownButton
    ref={ref}
    className={cn(
      "flex cursor-default items-center justify-center py-1",
      className
    )}
    {...props}
  >
    <ChevronDown className="h-4 w-4" />
  </SelectPrimitive.ScrollDownButton>
))
SelectScrollDownButton.displayName =
  SelectPrimitive.ScrollDownButton.displayName

const SelectContent = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.Content>
>(({ className, children, position = "popper", ...props }, ref) => (
  <SelectPrimitive.Portal>
    <SelectPrimitive.Content
      ref={ref}
      className={cn(
        "relative z-50 max-h-96 min-w-[8rem] overflow-hidden rounded-md border bg-popover text-popover-foreground shadow-md data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 data-[side=bottom]:slide-in-from-top-2 data-[side=left]:slide-in-from-right-2 data-[side=right]:slide-in-from-left-2 data-[side=top]:slide-in-from-bottom-2",
        position === "popper" &&
          "data-[side=bottom]:translate-y-1 data-[side=left]:-translate-x-1 data-[side=right]:translate-x-1 data-[side=top]:-translate-y-1",
        className
      )}
      position={position}
      {...props}
    >
      <SelectScrollUpButton />
      <SelectPrimitive.Viewport
        className={cn(
          "p-1",
          position === "popper" &&
            "h-[var(--radix-select-trigger-height)] w-full min-w-[var(--radix-select-trigger-width)]"
        )}
      >
        {children}
      </SelectPrimitive.Viewport>
      <SelectScrollDownButton />
    </SelectPrimitive.Content>
  </SelectPrimitive.Portal>
))
SelectContent.displayName = SelectPrimitive.Content.displayName

const SelectLabel = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Label>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.Label>
>(({ className, ...props }, ref) => (
  <SelectPrimitive.Label
    ref={ref}
    className={cn("py-1.5 pl-8 pr-2 text-sm font-semibold", className)}
    {...props}
  />
))
SelectLabel.displayName = SelectPrimitive.Label.displayName

const SelectItem = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Item>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.Item>
>(({ className, children, ...props }, ref) => (
  <SelectPrimitive.Item
    ref={ref}
    className={cn(
      "relative flex w-full cursor-default select-none items-center rounded-sm py-1.5 pl-8 pr-2 text-sm outline-none focus:bg-accent focus:text-accent-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
      className
    )}
    {...props}
  >
    <span className="absolute left-2 flex h-3.5 w-3.5 items-center justify-center">
      <SelectPrimitive.ItemIndicator>
        <Check className="h-4 w-4" />
      </SelectPrimitive.ItemIndicator>
    </span>

    <SelectPrimitive.ItemText>{children}</SelectPrimitive.ItemText>
  </SelectPrimitive.Item>
))
SelectItem.displayName = SelectPrimitive.Item.displayName

const SelectSeparator = React.forwardRef<
  React.ElementRef<typeof SelectPrimitive.Separator>,
  React.ComponentPropsWithoutRef<typeof SelectPrimitive.Separator>
>(({ className, ...props }, ref) => (
  <SelectPrimitive.Separator
    ref={ref}
    className={cn("-mx-1 my-1 h-px bg-muted", className)}
    {...props}
  />
))
SelectSeparator.displayName = SelectPrimitive.Separator.displayName

export {
  Select,
  SelectGroup,
  SelectValue,
  SelectTrigger,
  SelectContent,
  SelectLabel,
  SelectItem,
  SelectSeparator,
  SelectScrollUpButton,
  SelectScrollDownButton,
}
```

### 27. components/ui/dialog.tsx

**File:** `services/web-ui/src/components/ui/dialog.tsx`

```typescript
import * as React from "react"
import * as DialogPrimitive from "@radix-ui/react-dialog"
import { X } from "lucide-react"
import { cn } from "@/lib/utils"

const Dialog = DialogPrimitive.Root

const DialogTrigger = DialogPrimitive.Trigger

const DialogPortal = DialogPrimitive.Portal

const DialogClose = DialogPrimitive.Close

const DialogOverlay = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Overlay>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Overlay>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Overlay
    ref={ref}
    className={cn(
      "fixed inset-0 z-50 bg-black/80  data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0",
      className
    )}
    {...props}
  />
))
DialogOverlay.displayName = DialogPrimitive.Overlay.displayName

const DialogContent = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Content>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Content>
>(({ className, children, ...props }, ref) => (
  <DialogPortal>
    <DialogOverlay />
    <DialogPrimitive.Content
      ref={ref}
      className={cn(
        "fixed left-[50%] top-[50%] z-50 grid w-full max-w-lg translate-x-[-50%] translate-y-[-50%] gap-4 border bg-background p-6 shadow-lg duration-200 data-[state=open]:animate-in data-[state=closed]:animate-out data-[state=closed]:fade-out-0 data-[state=open]:fade-in-0 data-[state=closed]:zoom-out-95 data-[state=open]:zoom-in-95 data-[state=closed]:slide-out-to-left-1/2 data-[state=closed]:slide-out-to-top-[48%] data-[state=open]:slide-in-from-left-1/2 data-[state=open]:slide-in-from-top-[48%] sm:rounded-lg",
        className
      )}
      {...props}
    >
      {children}
      <DialogPrimitive.Close className="absolute right-4 top-4 rounded-sm opacity-70 ring-offset-background transition-opacity hover:opacity-100 focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 disabled:pointer-events-none data-[state=open]:bg-accent data-[state=open]:text-muted-foreground">
        <X className="h-4 w-4" />
        <span className="sr-only">Close</span>
      </DialogPrimitive.Close>
    </DialogPrimitive.Content>
  </DialogPortal>
))
DialogContent.displayName = DialogPrimitive.Content.displayName

const DialogHeader = ({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) => (
  <div
    className={cn(
      "flex flex-col space-y-1.5 text-center sm:text-left",
      className
    )}
    {...props}
  />
)
DialogHeader.displayName = "DialogHeader"

const DialogFooter = ({
  className,
  ...props
}: React.HTMLAttributes<HTMLDivElement>) => (
  <div
    className={cn(
      "flex flex-col-reverse sm:flex-row sm:justify-end sm:space-x-2",
      className
    )}
    {...props}
  />
)
DialogFooter.displayName = "DialogFooter"

const DialogTitle = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Title>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Title>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Title
    ref={ref}
    className={cn(
      "text-lg font-semibold leading-none tracking-tight",
      className
    )}
    {...props}
  />
))
DialogTitle.displayName = DialogPrimitive.Title.displayName

const DialogDescription = React.forwardRef<
  React.ElementRef<typeof DialogPrimitive.Description>,
  React.ComponentPropsWithoutRef<typeof DialogPrimitive.Description>
>(({ className, ...props }, ref) => (
  <DialogPrimitive.Description
    ref={ref}
    className={cn("text-sm text-muted-foreground", className)}
    {...props}
  />
))
DialogDescription.displayName = DialogPrimitive.Description.displayName

export {
  Dialog,
  DialogPortal,
  DialogOverlay,
  DialogClose,
  DialogTrigger,
  DialogContent,
  DialogHeader,
  DialogFooter,
  DialogTitle,
  DialogDescription,
}
```

### 28. components/ui/badge.tsx

**File:** `services/web-ui/src/components/ui/badge.tsx`

```typescript
import * as React from "react"
import { cva, type VariantProps } from "class-variance-authority"
import { cn } from "@/lib/utils"

const badgeVariants = cva(
  "inline-flex items-center rounded-full border px-2.5 py-0.5 text-xs font-semibold transition-colors focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2",
  {
    variants: {
      variant: {
        default:
          "border-transparent bg-primary text-primary-foreground hover:bg-primary/80",
        secondary:
          "border-transparent bg-secondary text-secondary-foreground hover:bg-secondary/80",
        destructive:
          "border-transparent bg-destructive text-destructive-foreground hover:bg-destructive/80",
        outline: "text-foreground",
        success:
          "border-transparent bg-green-500 text-white hover:bg-green-600",
        warning:
          "border-transparent bg-yellow-500 text-white hover:bg-yellow-600",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

export interface BadgeProps
  extends React.HTMLAttributes<HTMLDivElement>,
    VariantProps<typeof badgeVariants> {}

function Badge({ className, variant, ...props }: BadgeProps) {
  return (
    <div className={cn(badgeVariants({ variant }), className)} {...props} />
  )
}

export { Badge, badgeVariants }
```

### 29. components/ui/card.tsx

**File:** `services/web-ui/src/components/ui/card.tsx`

```typescript
import * as React from "react"
import { cn } from "@/lib/utils"

const Card = React.forwardRef<
  HTMLDivElement,
  React.HTMLAttributes<HTMLDivElement>
>(({ className, ...props }, ref) => (
  <div
    ref={ref}
    className={cn(
      "rounded-lg border bg-card text-card-foreground shadow-sm",
      className
    )}
    {...props}
  />
))
Card.displayName = "Card"

const CardHeader = React.forwardRef<
  HTMLDivElement,
  React.HTMLAttributes<HTMLDivElement>
>(({ className, ...props }, ref) => (
  <div
    ref={ref}
    className={cn("flex flex-col space-y-1.5 p-6", className)}
    {...props}
  />
))
CardHeader.displayName = "CardHeader"

const CardTitle = React.forwardRef<
  HTMLParagraphElement,
  React.HTMLAttributes<HTMLHeadingElement>
>(({ className, ...props }, ref) => (
  <h3
    ref={ref}
    className={cn(
      "text-2xl font-semibold leading-none tracking-tight",
      className
    )}
    {...props}
  />
))
CardTitle.displayName = "CardTitle"

const CardDescription = React.forwardRef<
  HTMLParagraphElement,
  React.HTMLAttributes<HTMLParagraphElement>
>(({ className, ...props }, ref) => (
  <p
    ref={ref}
    className={cn("text-sm text-muted-foreground", className)}
    {...props}
  />
))
CardDescription.displayName = "CardDescription"

const CardContent = React.forwardRef<
  HTMLDivElement,
  React.HTMLAttributes<HTMLDivElement>
>(({ className, ...props }, ref) => (
  <div ref={ref} className={cn("p-6 pt-0", className)} {...props} />
))
CardContent.displayName = "CardContent"

const CardFooter = React.forwardRef<
  HTMLDivElement,
  React.HTMLAttributes<HTMLDivElement>
>(({ className, ...props }, ref) => (
  <div
    ref={ref}
    className={cn("flex items-center p-6 pt-0", className)}
    {...props}
  />
))
CardFooter.displayName = "CardFooter"

export { Card, CardHeader, CardFooter, CardTitle, CardDescription, CardContent }
```

### 30. components/ui/table.tsx

**File:** `services/web-ui/src/components/ui/table.tsx`

```typescript
import * as React from "react"
import { cn } from "@/lib/utils"

const Table = React.forwardRef<
  HTMLTableElement,
  React.HTMLAttributes<HTMLTableElement>
>(({ className, ...props }, ref) => (
  <div className="relative w-full overflow-auto">
    <table
      ref={ref}
      className={cn("w-full caption-bottom text-sm", className)}
      {...props}
    />
  </div>
))
Table.displayName = "Table"

const TableHeader = React.forwardRef<
  HTMLTableSectionElement,
  React.HTMLAttributes<HTMLTableSectionElement>
>(({ className, ...props }, ref) => (
  <thead ref={ref} className={cn("[&_tr]:border-b", className)} {...props} />
))
TableHeader.displayName = "TableHeader"

const TableBody = React.forwardRef<
  HTMLTableSectionElement,
  React.HTMLAttributes<HTMLTableSectionElement>
>(({ className, ...props }, ref) => (
  <tbody
    ref={ref}
    className={cn("[&_tr:last-child]:border-0", className)}
    {...props}
  />
))
TableBody.displayName = "TableBody"

const TableFooter = React.forwardRef<
  HTMLTableSectionElement,
  React.HTMLAttributes<HTMLTableSectionElement>
>(({ className, ...props }, ref) => (
  <tfoot
    ref={ref}
    className={cn(
      "border-t bg-muted/50 font-medium [&>tr]:last:border-b-0",
      className
    )}
    {...props}
  />
))
TableFooter.displayName = "TableFooter"

const TableRow = React.forwardRef<
  HTMLTableRowElement,
  React.HTMLAttributes<HTMLTableRowElement>
>(({ className, ...props }, ref) => (
  <tr
    ref={ref}
    className={cn(
      "border-b transition-colors hover:bg-muted/50 data-[state=selected]:bg-muted",
      className
    )}
    {...props}
  />
))
TableRow.displayName = "TableRow"

const TableHead = React.forwardRef<
  HTMLTableCellElement,
  React.ThHTMLAttributes<HTMLTableCellElement>
>(({ className, ...props }, ref) => (
  <th
    ref={ref}
    className={cn(
      "h-12 px-4 text-left align-middle font-medium text-muted-foreground [&:has([role=checkbox])]:pr-0",
      className
    )}
    {...props}
  />
))
TableHead.displayName = "TableHead"

const TableCell = React.forwardRef<
  HTMLTableCellElement,
  React.TdHTMLAttributes<HTMLTableCellElement>
>(({ className, ...props }, ref) => (
  <td
    ref={ref}
    className={cn("p-4 align-middle [&:has([role=checkbox])]:pr-0", className)}
    {...props}
  />
))
TableCell.displayName = "TableCell"

const TableCaption = React.forwardRef<
  HTMLTableCaptionElement,
  React.HTMLAttributes<HTMLTableCaptionElement>
>(({ className, ...props }, ref) => (
  <caption
    ref={ref}
    className={cn("mt-4 text-sm text-muted-foreground", className)}
    {...props}
  />
))
TableCaption.displayName = "TableCaption"

export {
  Table,
  TableHeader,
  TableBody,
  TableFooter,
  TableHead,
  TableRow,
  TableCell,
  TableCaption,
}
```

### 31. components/ui/toast.tsx

**File:** `services/web-ui/src/components/ui/toast.tsx`

```typescript
import * as React from "react"
import * as ToastPrimitives from "@radix-ui/react-toast"
import { cva, type VariantProps } from "class-variance-authority"
import { X } from "lucide-react"
import { cn } from "@/lib/utils"

const ToastProvider = ToastPrimitives.Provider

const ToastViewport = React.forwardRef<
  React.ElementRef<typeof ToastPrimitives.Viewport>,
  React.ComponentPropsWithoutRef<typeof ToastPrimitives.Viewport>
>(({ className, ...props }, ref) => (
  <ToastPrimitives.Viewport
    ref={ref}
    className={cn(
      "fixed top-0 z-[100] flex max-h-screen w-full flex-col-reverse p-4 sm:bottom-0 sm:right-0 sm:top-auto sm:flex-col md:max-w-[420px]",
      className
    )}
    {...props}
  />
))
ToastViewport.displayName = ToastPrimitives.Viewport.displayName

const toastVariants = cva(
  "group pointer-events-auto relative flex w-full items-center justify-between space-x-4 overflow-hidden rounded-md border p-6 pr-8 shadow-lg transition-all data-[swipe=cancel]:translate-x-0 data-[swipe=end]:translate-x-[var(--radix-toast-swipe-end-x)] data-[swipe=move]:translate-x-[var(--radix-toast-swipe-move-x)] data-[swipe=move]:transition-none data-[state=open]:animate-in data-[state=closed]:animate-out data-[swipe=end]:animate-out data-[state=closed]:fade-out-80 data-[state=closed]:slide-out-to-right-full data-[state=open]:slide-in-from-top-full data-[state=open]:sm:slide-in-from-bottom-full",
  {
    variants: {
      variant: {
        default: "border bg-background text-foreground",
        destructive:
          "destructive group border-destructive bg-destructive text-destructive-foreground",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

const Toast = React.forwardRef<
  React.ElementRef<typeof ToastPrimitives.Root>,
  React.ComponentPropsWithoutRef<typeof ToastPrimitives.Root> &
    VariantProps<typeof toastVariants>
>(({ className, variant, ...props }, ref) => {
  return (
    <ToastPrimitives.Root
      ref={ref}
      className={cn(toastVariants({ variant }), className)}
      {...props}
    />
  )
})
Toast.displayName = ToastPrimitives.Root.displayName

const ToastAction = React.forwardRef<
  React.ElementRef<typeof ToastPrimitives.Action>,
  React.ComponentPropsWithoutRef<typeof ToastPrimitives.Action>
>(({ className, ...props }, ref) => (
  <ToastPrimitives.Action
    ref={ref}
    className={cn(
      "inline-flex h-8 shrink-0 items-center justify-center rounded-md border bg-transparent px-3 text-sm font-medium ring-offset-background transition-colors hover:bg-secondary focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 disabled:pointer-events-none disabled:opacity-50 group-[.destructive]:border-muted/40 group-[.destructive]:hover:border-destructive/30 group-[.destructive]:hover:bg-destructive group-[.destructive]:hover:text-destructive-foreground group-[.destructive]:focus:ring-destructive",
      className
    )}
    {...props}
  />
))
ToastAction.displayName = ToastPrimitives.Action.displayName

const ToastClose = React.forwardRef<
  React.ElementRef<typeof ToastPrimitives.Close>,
  React.ComponentPropsWithoutRef<typeof ToastPrimitives.Close>
>(({ className, ...props }, ref) => (
  <ToastPrimitives.Close
    ref={ref}
    className={cn(
      "absolute right-2 top-2 rounded-md p-1 text-foreground/50 opacity-0 transition-opacity hover:text-foreground focus:opacity-100 focus:outline-none focus:ring-2 group-hover:opacity-100 group-[.destructive]:text-red-300 group-[.destructive]:hover:text-red-50 group-[.destructive]:focus:ring-red-400 group-[.destructive]:focus:ring-offset-red-600",
      className
    )}
    toast-close=""
    {...props}
  >
    <X className="h-4 w-4" />
  </ToastPrimitives.Close>
))
ToastClose.displayName = ToastPrimitives.Close.displayName

const ToastTitle = React.forwardRef<
  React.ElementRef<typeof ToastPrimitives.Title>,
  React.ComponentPropsWithoutRef<typeof ToastPrimitives.Title>
>(({ className, ...props }, ref) => (
  <ToastPrimitives.Title
    ref={ref}
    className={cn("text-sm font-semibold", className)}
    {...props}
  />
))
ToastTitle.displayName = ToastPrimitives.Title.displayName

const ToastDescription = React.forwardRef<
  React.ElementRef<typeof ToastPrimitives.Description>,
  React.ComponentPropsWithoutRef<typeof ToastPrimitives.Description>
>(({ className, ...props }, ref) => (
  <ToastPrimitives.Description
    ref={ref}
    className={cn("text-sm opacity-90", className)}
    {...props}
  />
))
ToastDescription.displayName = ToastPrimitives.Description.displayName

type ToastProps = React.ComponentPropsWithoutRef<typeof Toast>

type ToastActionElement = React.ReactElement<typeof ToastAction>

export {
  type ToastProps,
  type ToastActionElement,
  ToastProvider,
  ToastViewport,
  Toast,
  ToastTitle,
  ToastDescription,
  ToastClose,
  ToastAction,
}
```

### 32. components/ui/toaster.tsx

**File:** `services/web-ui/src/components/ui/toaster.tsx`

```typescript
"use client"

import {
  Toast,
  ToastClose,
  ToastDescription,
  ToastProvider,
  ToastTitle,
  ToastViewport,
} from "@/components/ui/toast"
import { useToast } from "@/hooks/use-toast"

export function Toaster() {
  const { toasts } = useToast()

  return (
    <ToastProvider>
      {toasts.map(function ({ id, title, description, action, ...props }) {
        return (
          <Toast key={id} {...props}>
            <div className="grid gap-1">
              {title && <ToastTitle>{title}</ToastTitle>}
              {description && (
                <ToastDescription>{description}</ToastDescription>
              )}
            </div>
            {action}
            <ToastClose />
          </Toast>
        )
      })}
      <ToastViewport />
    </ToastProvider>
  )
}
```

### 33. components/ui/separator.tsx

**File:** `services/web-ui/src/components/ui/separator.tsx`

```typescript
import * as React from "react"
import * as SeparatorPrimitive from "@radix-ui/react-separator"
import { cn } from "@/lib/utils"

const Separator = React.forwardRef<
  React.ElementRef<typeof SeparatorPrimitive.Root>,
  React.ComponentPropsWithoutRef<typeof SeparatorPrimitive.Root>
>(
  (
    { className, orientation = "horizontal", decorative = true, ...props },
    ref
  ) => (
    <SeparatorPrimitive.Root
      ref={ref}
      decorative={decorative}
      orientation={orientation}
      className={cn(
        "shrink-0 bg-border",
        orientation === "horizontal" ? "h-[1px] w-full" : "h-full w-[1px]",
        className
      )}
      {...props}
    />
  )
)
Separator.displayName = SeparatorPrimitive.Root.displayName

export { Separator }
```

### 34. components/ui/breadcrumb.tsx

**File:** `services/web-ui/src/components/ui/breadcrumb.tsx`

```typescript
import * as React from "react"
import { ChevronRight, MoreHorizontal } from "lucide-react"
import { Slot } from "@radix-ui/react-slot"
import { cn } from "@/lib/utils"

const Breadcrumb = React.forwardRef<
  HTMLElement,
  React.ComponentPropsWithoutRef<"nav"> & {
    separator?: React.ReactNode
  }
>(({ ...props }, ref) => <nav ref={ref} aria-label="breadcrumb" {...props} />)
Breadcrumb.displayName = "Breadcrumb"

const BreadcrumbList = React.forwardRef<
  HTMLOListElement,
  React.ComponentPropsWithoutRef<"ol">
>(({ className, ...props }, ref) => (
  <ol
    ref={ref}
    className={cn(
      "flex flex-wrap items-center gap-1.5 break-words text-sm text-muted-foreground sm:gap-2.5",
      className
    )}
    {...props}
  />
))
BreadcrumbList.displayName = "BreadcrumbList"

const BreadcrumbItem = React.forwardRef<
  HTMLLIElement,
  React.ComponentPropsWithoutRef<"li">
>(({ className, ...props }, ref) => (
  <li
    ref={ref}
    className={cn("inline-flex items-center gap-1.5", className)}
    {...props}
  />
))
BreadcrumbItem.displayName = "BreadcrumbItem"

const BreadcrumbLink = React.forwardRef<
  HTMLAnchorElement,
  React.ComponentPropsWithoutRef<"a"> & {
    asChild?: boolean
  }
>(({ asChild, className, ...props }, ref) => {
  const Comp = asChild ? Slot : "a"

  return (
    <Comp
      ref={ref}
      className={cn(
        "transition-colors hover:text-foreground",
        className
      )}
      {...props}
    />
  )
})
BreadcrumbLink.displayName = "BreadcrumbLink"

const BreadcrumbPage = React.forwardRef<
  HTMLSpanElement,
  React.ComponentPropsWithoutRef<"span">
>(({ className, ...props }, ref) => (
  <span
    ref={ref}
    role="link"
    aria-disabled="true"
    aria-current="page"
    className={cn("font-normal text-foreground", className)}
    {...props}
  />
))
BreadcrumbPage.displayName = "BreadcrumbPage"

const BreadcrumbSeparator = ({
  children,
  className,
  ...props
}: React.ComponentProps<"li">) => (
  <li
    role="presentation"
    aria-hidden="true"
    className={cn("[&>svg]:size-3.5", className)}
    {...props}
  >
    {children ?? <ChevronRight />}
  </li>
)
BreadcrumbSeparator.displayName = "BreadcrumbSeparator"

const BreadcrumbEllipsis = ({
  className,
  ...props
}: React.ComponentProps<"span">) => (
  <span
    role="presentation"
    aria-hidden="true"
    className={cn("flex h-9 w-9 items-center justify-center", className)}
    {...props}
  >
    <MoreHorizontal className="h-4 w-4" />
    <span className="sr-only">More</span>
  </span>
)
BreadcrumbEllipsis.displayName = "BreadcrumbEllipsis"

export {
  Breadcrumb,
  BreadcrumbList,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbPage,
  BreadcrumbSeparator,
  BreadcrumbEllipsis,
}
```

### 35. hooks/use-toast.ts

**File:** `services/web-ui/src/hooks/use-toast.ts`

```typescript
import * as React from "react"

const TOAST_LIMIT = 1
const TOAST_REMOVE_DELAY = 1000000

type ToasterToast = {
  id: string
  title?: string
  description?: string
  action?: React.ReactNode
}

const actionTypes = {
  ADD_TOAST: "ADD_TOAST",
  UPDATE_TOAST: "UPDATE_TOAST",
  DISMISS_TOAST: "DISMISS_TOAST",
  REMOVE_TOAST: "REMOVE_TOAST",
} as const

let count = 0

function genId() {
  count = (count + 1) % Number.MAX_SAFE_INTEGER
  return count.toString()
}

type ActionType = typeof actionTypes

type Action =
  | {
      type: ActionType["ADD_TOAST"]
      toast: ToasterToast
    }
  | {
      type: ActionType["UPDATE_TOAST"]
      toast: Partial<ToasterToast>
    }
  | {
      type: ActionType["DISMISS_TOAST"]
      toastId?: string
    }
  | {
      type: ActionType["REMOVE_TOAST"]
      toastId?: string
    }

interface State {
  toasts: ToasterToast[]
}

const toastTimeouts = new Map<string, ReturnType<typeof setTimeout>>()

const addToRemoveQueue = (toastId: string) => {
  if (toastTimeouts.has(toastId)) {
    return
  }

  const timeout = setTimeout(() => {
    toastTimeouts.delete(toastId)
    dispatch({
      type: "REMOVE_TOAST",
      toastId: toastId,
    })
  }, TOAST_REMOVE_DELAY)

  toastTimeouts.set(toastId, timeout)
}

export const reducer = (state: State, action: Action): State => {
  switch (action.type) {
    case "ADD_TOAST":
      return {
        ...state,
        toasts: [action.toast, ...state.toasts].slice(0, TOAST_LIMIT),
      }

    case "UPDATE_TOAST":
      return {
        ...state,
        toasts: state.toasts.map((t) =>
          t.id === action.toast.id ? { ...t, ...action.toast } : t
        ),
      }

    case "DISMISS_TOAST": {
      const { toastId } = action

      if (toastId) {
        addToRemoveQueue(toastId)
      } else {
        state.toasts.forEach((toast) => {
          addToRemoveQueue(toast.id)
        })
      }

      return {
        ...state,
        toasts: state.toasts.map((t) =>
          t.id === toastId || toastId === undefined
            ? {
                ...t,
                open: false,
              }
            : t
        ),
      }
    }
    case "REMOVE_TOAST":
      if (action.toastId === undefined) {
        return {
          ...state,
          toasts: [],
        }
      }
      return {
        ...state,
        toasts: state.toasts.filter((t) => t.id !== action.toastId),
      }
  }
}

const listeners: Array<(state: State) => void> = []

let memoryState: State = { toasts: [] }

function dispatch(action: Action) {
  memoryState = reducer(memoryState, action)
  listeners.forEach((listener) => {
    listener(memoryState)
  })
}

type Toast = Omit<ToasterToast, "id">

function toast({ ...props }: Toast) {
  const id = genId()

  const update = (props: ToasterToast) =>
    dispatch({
      type: "UPDATE_TOAST",
      toast: { ...props, id },
    })
  const dismiss = () => dispatch({ type: "DISMISS_TOAST", toastId: id })

  dispatch({
    type: "ADD_TOAST",
    toast: {
      ...props,
      id,
      open: true,
      onOpenChange: (open) => {
        if (!open) dismiss()
      },
    },
  })

  return {
    id: id,
    dismiss,
    update,
  }
}

function useToast() {
  const [state, setState] = React.useState<State>(memoryState)

  React.useEffect(() => {
    listeners.push(setState)
    return () => {
      const index = listeners.indexOf(setState)
      if (index > -1) {
        listeners.splice(index, 1)
      }
    }
  }, [state])

  return {
    ...state,
    toast,
    dismiss: (toastId?: string) => dispatch({ type: "DISMISS_TOAST", toastId }),
  }
}

export { useToast, toast }
```

---

## Componenti Layout

### 36. components/layout/Sidebar.tsx

**File:** `services/web-ui/src/components/layout/Sidebar.tsx`

```typescript
import { NavLink } from 'react-router-dom'
import { Layout, Dashboard, Building2, Map, Server, Tag, AlertTriangle } from 'lucide-react'
import { cn } from '@/lib/utils'

const navItems = [
  { path: '/', label: 'Dashboard', icon: Dashboard },
  { path: '/organizations', label: 'Organizations', icon: Building2 },
  { path: '/sites', label: 'Sites', icon: Map },
  { path: '/areas', label: 'Areas', icon: Layout },
  { path: '/gateways', label: 'Gateways', icon: Server },
  { path: '/tags', label: 'Tags', icon: Tag },
  { path: '/alarms', label: 'Alarms', icon: AlertTriangle },
]

export function Sidebar() {
  return (
    <aside className="flex h-full w-64 flex-col border-r bg-muted/40">
      <div className="flex h-16 items-center border-b px-6">
        <Server className="mr-2 h-6 w-6" />
        <span className="text-lg font-semibold">Industrial Edge</span>
      </div>

      <nav className="flex-1 space-y-1 p-4">
        {navItems.map((item) => (
          <NavLink
            key={item.path}
            to={item.path}
            className={({ isActive }) =>
              cn(
                'flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors',
                isActive
                  ? 'bg-primary text-primary-foreground'
                  : 'text-muted-foreground hover:bg-muted hover:text-foreground'
              )
            }
          >
            <item.icon className="h-5 w-5" />
            {item.label}
          </NavLink>
        ))}
      </nav>
    </aside>
  )
}
```

### 37. components/layout/Header.tsx

**File:** `services/web-ui/src/components/layout/Header.tsx`

```typescript
import { useNavigationStore } from '@/stores/useNavigationStore'

export function Header() {
  const { selectedOrg, selectedSite, selectedArea } = useNavigationStore()

  return (
    <header className="flex h-16 items-center justify-between border-b bg-background px-6">
      <div className="flex items-center gap-2 text-sm text-muted-foreground">
        {selectedOrg && <span>Org: {selectedOrg.name}</span>}
        {selectedSite && <span>Site: {selectedSite.name}</span>}
        {selectedArea && <span>Area: {selectedArea.name}</span>}
      </div>
      <div className="flex items-center gap-4">
        <span className="text-sm text-muted-foreground">Configuration UI</span>
      </div>
    </header>
  )
}
```

### 38. components/layout/MainLayout.tsx

**File:** `services/web-ui/src/components/layout/MainLayout.tsx`

```typescript
import { Outlet } from 'react-router-dom'
import { Sidebar } from './Sidebar'
import { Header } from './Header'

export function MainLayout() {
  return (
    <div className="flex h-screen overflow-hidden">
      <Sidebar />
      <div className="flex flex-1 flex-col overflow-hidden">
        <Header />
        <main className="flex-1 overflow-auto p-6">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
```

---

## Store per Navigazione

### 39. stores/useNavigationStore.ts

**File:** `services/web-ui/src/stores/useNavigationStore.ts`

```typescript
import { create } from 'zustand'
import type { Organization, Site, Area } from '@/types'

interface NavigationState {
  selectedOrg: Organization | null
  selectedSite: Site | null
  selectedArea: Area | null
  setSelectedOrg: (org: Organization | null) => void
  setSelectedSite: (site: Site | null) => void
  setSelectedArea: (area: Area | null) => void
  clearSelection: () => void
}

export const useNavigationStore = create<NavigationState>((set) => ({
  selectedOrg: null,
  selectedSite: null,
  selectedArea: null,
  setSelectedOrg: (org) => set({ selectedOrg: org, selectedSite: null, selectedArea: null }),
  setSelectedSite: (site) => set({ selectedSite: site, selectedArea: null }),
  setSelectedArea: (area) => set({ selectedArea: area }),
  clearSelection: () => set({ selectedOrg: null, selectedSite: null, selectedArea: null }),
}))
```

---

## Custom Hooks per Data Fetching

### 40. hooks/useOrganizations.ts

**File:** `services/web-ui/src/hooks/useOrganizations.ts`

```typescript
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { organizationsApi } from '@/api/organizations'
import type { Organization, CreateOrganizationDto } from '@/types'

export function useOrganizations() {
  const queryClient = useQueryClient()

  const query = useQuery({
    queryKey: ['organizations'],
    queryFn: organizationsApi.list,
  })

  const createMutation = useMutation({
    mutationFn: (dto: CreateOrganizationDto) => organizationsApi.create(dto),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['organizations'] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => organizationsApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['organizations'] })
    },
  })

  return {
    organizations: query.data || [],
    isLoading: query.isLoading,
    error: query.error,
    createOrganization: createMutation.mutate,
    deleteOrganization: deleteMutation.mutate,
    isCreating: createMutation.isPending,
    isDeleting: deleteMutation.isPending,
  }
}
```

### 41. hooks/useSites.ts

**File:** `services/web-ui/src/hooks/useSites.ts`

```typescript
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { sitesApi } from '@/api/sites'
import type { Site, CreateSiteDto } from '@/types'

export function useSites(orgId?: number) {
  const queryClient = useQueryClient()

  const query = useQuery({
    queryKey: ['sites', orgId],
    queryFn: () => sitesApi.list(orgId),
  })

  const createMutation = useMutation({
    mutationFn: (dto: CreateSiteDto) => sitesApi.create(dto),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['sites'] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => sitesApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['sites'] })
    },
  })

  return {
    sites: query.data || [],
    isLoading: query.isLoading,
    error: query.error,
    createSite: createMutation.mutate,
    deleteSite: deleteMutation.mutate,
    isCreating: createMutation.isPending,
    isDeleting: deleteMutation.isPending,
  }
}
```

### 42. hooks/useAreas.ts

**File:** `services/web-ui/src/hooks/useAreas.ts`

```typescript
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { areasApi } from '@/api/areas'
import type { Area, CreateAreaDto } from '@/types'

export function useAreas(siteId?: number) {
  const queryClient = useQueryClient()

  const query = useQuery({
    queryKey: ['areas', siteId],
    queryFn: () => areasApi.list(siteId),
  })

  const createMutation = useMutation({
    mutationFn: (dto: CreateAreaDto) => areasApi.create(dto),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['areas'] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => areasApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['areas'] })
    },
  })

  return {
    areas: query.data || [],
    isLoading: query.isLoading,
    error: query.error,
    createArea: createMutation.mutate,
    deleteArea: deleteMutation.mutate,
    isCreating: createMutation.isPending,
    isDeleting: deleteMutation.isPending,
  }
}
```

### 43. hooks/useGateways.ts

**File:** `services/web-ui/src/hooks/useGateways.ts`

```typescript
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { gatewaysApi } from '@/api/gateways'
import type { Gateway, CreateGatewayDto, UpdateGatewayDto } from '@/types'

export function useGateways(areaId?: number) {
  const queryClient = useQueryClient()

  const query = useQuery({
    queryKey: ['gateways', areaId],
    queryFn: () => gatewaysApi.list(areaId),
  })

  const createMutation = useMutation({
    mutationFn: (dto: CreateGatewayDto) => gatewaysApi.create(dto),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['gateways'] })
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, dto }: { id: number; dto: UpdateGatewayDto }) =>
      gatewaysApi.update(id, dto),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['gateways'] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => gatewaysApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['gateways'] })
    },
  })

  return {
    gateways: query.data || [],
    isLoading: query.isLoading,
    error: query.error,
    createGateway: createMutation.mutate,
    updateGateway: updateMutation.mutate,
    deleteGateway: deleteMutation.mutate,
    isCreating: createMutation.isPending,
    isUpdating: updateMutation.isPending,
    isDeleting: deleteMutation.isPending,
  }
}
```

### 44. hooks/useTags.ts

**File:** `services/web-ui/src/hooks/useTags.ts`

```typescript
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { tagsApi } from '@/api/tags'
import type { Tag, CreateTagDto, UpdateTagDto } from '@/types'

export function useTags(gatewayId?: number) {
  const queryClient = useQueryClient()

  const query = useQuery({
    queryKey: ['tags', gatewayId],
    queryFn: () => tagsApi.list(gatewayId),
  })

  const createMutation = useMutation({
    mutationFn: (dto: CreateTagDto) => tagsApi.create(dto),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tags'] })
    },
  })

  const updateMutation = useMutation({
    mutationFn: ({ id, dto }: { id: number; dto: UpdateTagDto }) =>
      tagsApi.update(id, dto),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tags'] })
    },
  })

  const deleteMutation = useMutation({
    mutationFn: (id: number) => tagsApi.delete(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['tags'] })
    },
  })

  return {
    tags: query.data || [],
    isLoading: query.isLoading,
    error: query.error,
    createTag: createMutation.mutate,
    updateTag: updateMutation.mutate,
    deleteTag: deleteMutation.mutate,
    isCreating: createMutation.isPending,
    isUpdating: updateMutation.isPending,
    isDeleting: deleteMutation.isPending,
  }
}
```

### 45. hooks/useAlarms.ts

**File:** `services/web-ui/src/hooks/useAlarms.ts`

```typescript
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { alarmsApi } from '@/api/alarms'
import type { Alarm } from '@/types'

export function useAlarms(params?: { state?: string; priority?: number; tag_id?: number }) {
  const queryClient = useQueryClient()

  const query = useQuery({
    queryKey: ['alarms', params],
    queryFn: () => alarmsApi.list(params),
    refetchInterval: 5000, // Poll every 5 seconds
  })

  const acknowledgeMutation = useMutation({
    mutationFn: (id: number) => alarmsApi.acknowledge(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['alarms'] })
    },
  })

  return {
    alarms: query.data || [],
    isLoading: query.isLoading,
    error: query.error,
    acknowledgeAlarm: acknowledgeMutation.mutate,
    isAcknowledging: acknowledgeMutation.isPending,
  }
}
```

### 46. hooks/useHealth.ts

**File:** `services/web-ui/src/hooks/useHealth.ts`

```typescript
import { useQuery } from '@tanstack/react-query'
import { healthApi } from '@/api/health'
import type { HealthStats } from '@/types'

export function useHealth() {
  const query = useQuery({
    queryKey: ['health'],
    queryFn: healthApi.getStats,
    refetchInterval: 10000, // Poll every 10 seconds
  })

  return {
    stats: query.data,
    isLoading: query.isLoading,
    error: query.error,
  }
}
```

---

## Pagine

### 47. pages/DashboardPage.tsx

**File:** `services/web-ui/src/pages/DashboardPage.tsx`

```typescript
import { useHealth } from '@/hooks/useHealth'
import { useOrganizations } from '@/hooks/useOrganizations'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Building2, Map, Server, Tag, AlertTriangle, Activity } from 'lucide-react'

export function DashboardPage() {
  const { stats, isLoading: healthLoading } = useHealth()
  const { organizations } = useOrganizations()

  if (healthLoading) {
    return <div>Loading...</div>
  }

  const statCards = [
    {
      title: 'Organizations',
      value: stats?.organizations_count || 0,
      icon: Building2,
      color: 'text-blue-500',
    },
    {
      title: 'Sites',
      value: stats?.sites_count || 0,
      icon: Map,
      color: 'text-green-500',
    },
    {
      title: 'Gateways',
      value: stats?.gateways_count || 0,
      icon: Server,
      color: 'text-purple-500',
    },
    {
      title: 'Online Gateways',
      value: stats?.online_gateways_count || 0,
      icon: Activity,
      color: 'text-emerald-500',
    },
    {
      title: 'Tags',
      value: stats?.tags_count || 0,
      icon: Tag,
      color: 'text-orange-500',
    },
    {
      title: 'Active Alarms',
      value: stats?.active_alarms_count || 0,
      icon: AlertTriangle,
      color: 'text-red-500',
    },
  ]

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-3xl font-bold">Dashboard</h1>
        <p className="text-muted-foreground">System overview and statistics</p>
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
        {statCards.map((stat) => (
          <Card key={stat.title}>
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">{stat.title}</CardTitle>
              <stat.icon className={`h-4 w-4 ${stat.color}`} />
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{stat.value}</div>
            </CardContent>
          </Card>
        ))}
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Recent Organizations</CardTitle>
        </CardHeader>
        <CardContent>
          {organizations.length === 0 ? (
            <p className="text-sm text-muted-foreground">No organizations found</p>
          ) : (
            <div className="space-y-2">
              {organizations.slice(0, 5).map((org) => (
                <div key={org.id} className="flex items-center justify-between">
                  <span className="text-sm font-medium">{org.name}</span>
                  <Badge variant="outline">ID: {org.id}</Badge>
                </div>
              ))}
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
```

### 48. pages/OrganizationsPage.tsx

**File:** `services/web-ui/src/pages/OrganizationsPage.tsx`

```typescript
import { useState } from 'react'
import { useOrganizations } from '@/hooks/useOrganizations'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Plus, Trash2 } from 'lucide-react'

export function OrganizationsPage() {
  const { organizations, isLoading, createOrganization, deleteOrganization, isCreating, isDeleting } = useOrganizations()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    createOrganization({ name })
    setName('')
    setOpen(false)
  }

  const handleDelete = (id: number) => {
    if (confirm('Are you sure you want to delete this organization?')) {
      deleteOrganization(id)
    }
  }

  if (isLoading) {
    return <div>Loading...</div>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Organizations</h1>
          <p className="text-muted-foreground">Manage your organizations</p>
        </div>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              New Organization
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Create Organization</DialogTitle>
              <DialogDescription>
                Enter the name for the new organization
              </DialogDescription>
            </DialogHeader>
            <form onSubmit={handleSubmit}>
              <div className="space-y-4 py-4">
                <div className="space-y-2">
                  <Label htmlFor="name">Name</Label>
                  <Input
                    id="name"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    required
                  />
                </div>
              </div>
              <DialogFooter>
                <Button type="submit" disabled={isCreating}>
                  Create
                </Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>
      </div>

      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>ID</TableHead>
              <TableHead>Name</TableHead>
              <TableHead>Created At</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {organizations.length === 0 ? (
              <TableRow>
                <TableCell colSpan={4} className="text-center">
                  No organizations found
                </TableCell>
              </TableRow>
            ) : (
              organizations.map((org) => (
                <TableRow key={org.id}>
                  <TableCell>{org.id}</TableCell>
                  <TableCell className="font-medium">{org.name}</TableCell>
                  <TableCell>{new Date(org.created_at).toLocaleString()}</TableCell>
                  <TableCell className="text-right">
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => handleDelete(org.id)}
                      disabled={isDeleting}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}
```

### 49. pages/SitesPage.tsx

**File:** `services/web-ui/src/pages/SitesPage.tsx`

```typescript
import { useState } from 'react'
import { useSites, useOrganizations } from '@/hooks'
import { useNavigationStore } from '@/stores/useNavigationStore'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Plus, Trash2 } from 'lucide-react'

export function SitesPage() {
  const { selectedOrg } = useNavigationStore()
  const { organizations } = useOrganizations()
  const { sites, isLoading, createSite, deleteSite } = useSites(selectedOrg?.id)
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [orgId, setOrgId] = useState<number | null>(null)

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (orgId) {
      createSite({ org_id: orgId, name })
      setName('')
      setOrgId(null)
      setOpen(false)
    }
  }

  const handleDelete = (id: number) => {
    if (confirm('Are you sure you want to delete this site?')) {
      deleteSite(id)
    }
  }

  if (isLoading) {
    return <div>Loading...</div>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Sites</h1>
          <p className="text-muted-foreground">Manage your sites</p>
        </div>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              New Site
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Create Site</DialogTitle>
              <DialogDescription>
                Create a new site for an organization
              </DialogDescription>
            </DialogHeader>
            <form onSubmit={handleSubmit}>
              <div className="space-y-4 py-4">
                <div className="space-y-2">
                  <Label htmlFor="org">Organization</Label>
                  <Select value={orgId?.toString()} onValueChange={(v) => setOrgId(parseInt(v))}>
                    <SelectTrigger>
                      <SelectValue placeholder="Select organization" />
                    </SelectTrigger>
                    <SelectContent>
                      {organizations.map((org) => (
                        <SelectItem key={org.id} value={org.id.toString()}>
                          {org.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="name">Name</Label>
                  <Input
                    id="name"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    required
                  />
                </div>
              </div>
              <DialogFooter>
                <Button type="submit" disabled={!orgId}>
                  Create
                </Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>
      </div>

      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>ID</TableHead>
              <TableHead>Organization</TableHead>
              <TableHead>Name</TableHead>
              <TableHead>Created At</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {sites.length === 0 ? (
              <TableRow>
                <TableCell colSpan={5} className="text-center">
                  No sites found
                </TableCell>
              </TableRow>
            ) : (
              sites.map((site) => (
                <TableRow key={site.id}>
                  <TableCell>{site.id}</TableCell>
                  <TableCell>{site.organization?.name || `Org ${site.org_id}`}</TableCell>
                  <TableCell className="font-medium">{site.name}</TableCell>
                  <TableCell>{new Date(site.created_at).toLocaleString()}</TableCell>
                  <TableCell className="text-right">
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => handleDelete(site.id)}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}
```

### 50. pages/AreasPage.tsx

**File:** `services/web-ui/src/pages/AreasPage.tsx`

```typescript
import { useState } from 'react'
import { useAreas, useSites } from '@/hooks'
import { useNavigationStore } from '@/stores/useNavigationStore'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Plus, Trash2 } from 'lucide-react'

export function AreasPage() {
  const { selectedSite } = useNavigationStore()
  const { sites } = useSites()
  const { areas, isLoading, createArea, deleteArea } = useAreas(selectedSite?.id)
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [siteId, setSiteId] = useState<number | null>(null)

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (siteId) {
      createArea({ site_id: siteId, name })
      setName('')
      setSiteId(null)
      setOpen(false)
    }
  }

  const handleDelete = (id: number) => {
    if (confirm('Are you sure you want to delete this area?')) {
      deleteArea(id)
    }
  }

  if (isLoading) {
    return <div>Loading...</div>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Areas</h1>
          <p className="text-muted-foreground">Manage your areas</p>
        </div>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              New Area
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Create Area</DialogTitle>
              <DialogDescription>
                Create a new area for a site
              </DialogDescription>
            </DialogHeader>
            <form onSubmit={handleSubmit}>
              <div className="space-y-4 py-4">
                <div className="space-y-2">
                  <Label htmlFor="site">Site</Label>
                  <Select value={siteId?.toString()} onValueChange={(v) => setSiteId(parseInt(v))}>
                    <SelectTrigger>
                      <SelectValue placeholder="Select site" />
                    </SelectTrigger>
                    <SelectContent>
                      {sites.map((site) => (
                        <SelectItem key={site.id} value={site.id.toString()}>
                          {site.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="name">Name</Label>
                  <Input
                    id="name"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    required
                  />
                </div>
              </div>
              <DialogFooter>
                <Button type="submit" disabled={!siteId}>
                  Create
                </Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>
      </div>

      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>ID</TableHead>
              <TableHead>Site</TableHead>
              <TableHead>Name</TableHead>
              <TableHead>Created At</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {areas.length === 0 ? (
              <TableRow>
                <TableCell colSpan={5} className="text-center">
                  No areas found
                </TableCell>
              </TableRow>
            ) : (
              areas.map((area) => (
                <TableRow key={area.id}>
                  <TableCell>{area.id}</TableCell>
                  <TableCell>{area.site?.name || `Site ${area.site_id}`}</TableCell>
                  <TableCell className="font-medium">{area.name}</TableCell>
                  <TableCell>{new Date(area.created_at).toLocaleString()}</TableCell>
                  <TableCell className="text-right">
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => handleDelete(area.id)}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}
```

### 51. pages/GatewaysPage.tsx

**File:** `services/web-ui/src/pages/GatewaysPage.tsx`

```typescript
import { useState } from 'react'
import { useGateways, useAreas } from '@/hooks'
import { useNavigationStore } from '@/stores/useNavigationStore'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Badge } from '@/components/ui/badge'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Plus, Trash2, Power } from 'lucide-react'
import type { DriverType, ConnectionConfig } from '@/types'

export function GatewaysPage() {
  const { selectedArea } = useNavigationStore()
  const { areas } = useAreas()
  const { gateways, isLoading, createGateway, updateGateway, deleteGateway } = useGateways(selectedArea?.id)
  const [open, setOpen] = useState(false)
  const [name, setName] = useState('')
  const [areaId, setAreaId] = useState<number | null>(null)
  const [driverType, setDriverType] = useState<DriverType>('S7')
  const [scanRate, setScanRate] = useState(1000)
  const [enabled, setEnabled] = useState(true)
  const [ip, setIp] = useState('')
  const [rack, setRack] = useState(0)
  const [slot, setSlot] = useState(0)
  const [slaveId, setSlaveId] = useState(1)
  const [port, setPort] = useState(502)

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (areaId) {
      const connectionConfig: ConnectionConfig = { ip }
      if (driverType === 'S7') {
        connectionConfig.rack = rack
        connectionConfig.slot = slot
      } else {
        connectionConfig.slave_id = slaveId
        connectionConfig.port = port
      }

      createGateway({
        area_id: areaId,
        name,
        driver_type: driverType,
        connection_config: connectionConfig,
        scan_rate_ms: scanRate,
        enabled,
      })
      resetForm()
      setOpen(false)
    }
  }

  const resetForm = () => {
    setName('')
    setAreaId(null)
    setDriverType('S7')
    setScanRate(1000)
    setEnabled(true)
    setIp('')
    setRack(0)
    setSlot(0)
    setSlaveId(1)
    setPort(502)
  }

  const handleToggle = (gateway: typeof gateways[0]) => {
    updateGateway({
      id: gateway.id,
      dto: { enabled: !gateway.enabled },
    })
  }

  const handleDelete = (id: number) => {
    if (confirm('Are you sure you want to delete this gateway?')) {
      deleteGateway(id)
    }
  }

  if (isLoading) {
    return <div>Loading...</div>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Gateways</h1>
          <p className="text-muted-foreground">Manage your gateways</p>
        </div>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              New Gateway
            </Button>
          </DialogTrigger>
          <DialogContent className="max-w-md">
            <DialogHeader>
              <DialogTitle>Create Gateway</DialogTitle>
              <DialogDescription>
                Configure a new gateway with PLC connection
              </DialogDescription>
            </DialogHeader>
            <form onSubmit={handleSubmit}>
              <div className="space-y-4 py-4 max-h-[60vh] overflow-y-auto">
                <div className="space-y-2">
                  <Label htmlFor="area">Area</Label>
                  <Select value={areaId?.toString()} onValueChange={(v) => setAreaId(parseInt(v))}>
                    <SelectTrigger>
                      <SelectValue placeholder="Select area" />
                    </SelectTrigger>
                    <SelectContent>
                      {areas.map((area) => (
                        <SelectItem key={area.id} value={area.id.toString()}>
                          {area.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="name">Name</Label>
                  <Input
                    id="name"
                    value={name}
                    onChange={(e) => setName(e.target.value)}
                    required
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="driver">Driver Type</Label>
                  <Select value={driverType} onValueChange={(v: DriverType) => setDriverType(v)}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="S7">Siemens S7</SelectItem>
                      <SelectItem value="MODBUS_TCP">Modbus TCP</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="ip">IP Address</Label>
                  <Input
                    id="ip"
                    value={ip}
                    onChange={(e) => setIp(e.target.value)}
                    placeholder="192.168.1.100"
                    required
                  />
                </div>
                {driverType === 'S7' && (
                  <>
                    <div className="space-y-2">
                      <Label htmlFor="rack">Rack</Label>
                      <Input
                        id="rack"
                        type="number"
                        value={rack}
                        onChange={(e) => setRack(parseInt(e.target.value))}
                      />
                    </div>
                    <div className="space-y-2">
                      <Label htmlFor="slot">Slot</Label>
                      <Input
                        id="slot"
                        type="number"
                        value={slot}
                        onChange={(e) => setSlot(parseInt(e.target.value))}
                      />
                    </div>
                  </>
                )}
                {driverType === 'MODBUS_TCP' && (
                  <>
                    <div className="space-y-2">
                      <Label htmlFor="slave">Slave ID</Label>
                      <Input
                        id="slave"
                        type="number"
                        value={slaveId}
                        onChange={(e) => setSlaveId(parseInt(e.target.value))}
                      />
                    </div>
                    <div className="space-y-2">
                      <Label htmlFor="port">Port</Label>
                      <Input
                        id="port"
                        type="number"
                        value={port}
                        onChange={(e) => setPort(parseInt(e.target.value))}
                      />
                    </div>
                  </>
                )}
                <div className="space-y-2">
                  <Label htmlFor="scanRate">Scan Rate (ms)</Label>
                  <Input
                    id="scanRate"
                    type="number"
                    value={scanRate}
                    onChange={(e) => setScanRate(parseInt(e.target.value))}
                  />
                </div>
              </div>
              <DialogFooter>
                <Button type="submit" disabled={!areaId}>
                  Create
                </Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>
      </div>

      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>ID</TableHead>
              <TableHead>Area</TableHead>
              <TableHead>Name</TableHead>
              <TableHead>Driver</TableHead>
              <TableHead>IP</TableHead>
              <TableHead>Scan Rate</TableHead>
              <TableHead>Status</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {gateways.length === 0 ? (
              <TableRow>
                <TableCell colSpan={8} className="text-center">
                  No gateways found
                </TableCell>
              </TableRow>
            ) : (
              gateways.map((gateway) => (
                <TableRow key={gateway.id}>
                  <TableCell>{gateway.id}</TableCell>
                  <TableCell>{gateway.area?.name || `Area ${gateway.area_id}`}</TableCell>
                  <TableCell className="font-medium">{gateway.name}</TableCell>
                  <TableCell>
                    <Badge variant="outline">{gateway.driver_type}</Badge>
                  </TableCell>
                  <TableCell>{gateway.connection_config.ip}</TableCell>
                  <TableCell>{gateway.scan_rate_ms}ms</TableCell>
                  <TableCell>
                    <Badge variant={gateway.enabled ? 'success' : 'destructive'}>
                      {gateway.enabled ? 'Enabled' : 'Disabled'}
                    </Badge>
                  </TableCell>
                  <TableCell className="text-right">
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => handleToggle(gateway)}
                    >
                      <Power className="h-4 w-4" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => handleDelete(gateway.id)}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}
```

### 52. pages/TagsPage.tsx

**File:** `services/web-ui/src/pages/TagsPage.tsx`

```typescript
import { useState } from 'react'
import { useTags, useGateways } from '@/hooks'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch' // Da aggiungere nei componenti UI
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from '@/components/ui/dialog'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Plus, Trash2, Settings } from 'lucide-react'
import type { DataType, AlarmOperator, AlarmPriority } from '@/types'

export function TagsPage() {
  const { gateways } = useGateways()
  const { tags, isLoading, createTag, deleteTag } = useTags()
  const [open, setOpen] = useState(false)
  const [gatewayId, setGatewayId] = useState<number | null>(null)
  const [code, setCode] = useState('')
  const [alias, setAlias] = useState('')
  const [dataType, setDataType] = useState<DataType>('INT')
  const [historize, setHistorize] = useState(false)
  const [historizeDeadband, setHistorizeDeadband] = useState(0)
  const [alarmEnabled, setAlarmEnabled] = useState(false)
  const [alarmThreshold, setAlarmThreshold] = useState(0)
  const [alarmOperator, setAlarmOperator] = useState<AlarmOperator>('>')
  const [alarmPriority, setAlarmPriority] = useState<AlarmPriority>(3)

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault()
    if (gatewayId) {
      createTag({
        gateway_id: gatewayId,
        code,
        alias,
        data_type: dataType,
        historize,
        historize_deadband: historizeDeadband,
        alarm_enabled: alarmEnabled,
        alarm_threshold: alarmThreshold,
        alarm_operator: alarmOperator,
        alarm_priority: alarmPriority,
      })
      resetForm()
      setOpen(false)
    }
  }

  const resetForm = () => {
    setGatewayId(null)
    setCode('')
    setAlias('')
    setDataType('INT')
    setHistorize(false)
    setHistorizeDeadband(0)
    setAlarmEnabled(false)
    setAlarmThreshold(0)
    setAlarmOperator('>')
    setAlarmPriority(3)
  }

  const handleDelete = (id: number) => {
    if (confirm('Are you sure you want to delete this tag?')) {
      deleteTag(id)
    }
  }

  if (isLoading) {
    return <div>Loading...</div>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Tags</h1>
          <p className="text-muted-foreground">Manage your PLC tags</p>
        </div>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger asChild>
            <Button>
              <Plus className="mr-2 h-4 w-4" />
              New Tag
            </Button>
          </DialogTrigger>
          <DialogContent className="max-w-md">
            <DialogHeader>
              <DialogTitle>Create Tag</DialogTitle>
              <DialogDescription>
                Configure a new PLC tag with optional alarm
              </DialogDescription>
            </DialogHeader>
            <form onSubmit={handleSubmit}>
              <div className="space-y-4 py-4 max-h-[60vh] overflow-y-auto">
                <div className="space-y-2">
                  <Label htmlFor="gateway">Gateway</Label>
                  <Select value={gatewayId?.toString()} onValueChange={(v) => setGatewayId(parseInt(v))}>
                    <SelectTrigger>
                      <SelectValue placeholder="Select gateway" />
                    </SelectTrigger>
                    <SelectContent>
                      {gateways.map((gw) => (
                        <SelectItem key={gw.id} value={gw.id.toString()}>
                          {gw.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label htmlFor="code">Code (PLC Address)</Label>
                  <Input
                    id="code"
                    value={code}
                    onChange={(e) => setCode(e.target.value)}
                    placeholder="DB100.DBD0"
                    required
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="alias">Alias</Label>
                  <Input
                    id="alias"
                    value={alias}
                    onChange={(e) => setAlias(e.target.value)}
                    placeholder="Temperature_1"
                    required
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="dataType">Data Type</Label>
                  <Select value={dataType} onValueChange={(v: DataType) => setDataType(v)}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="INT">INT</SelectItem>
                      <SelectItem value="REAL">REAL</SelectItem>
                      <SelectItem value="BOOL">BOOL</SelectItem>
                      <SelectItem value="DINT">DINT</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="flex items-center justify-between">
                  <Label htmlFor="historize">Historize</Label>
                  <Switch
                    id="historize"
                    checked={historize}
                    onCheckedChange={setHistorize}
                  />
                </div>
                {historize && (
                  <div className="space-y-2">
                    <Label htmlFor="deadband">Deadband</Label>
                    <Input
                      id="deadband"
                      type="number"
                      value={historizeDeadband}
                      onChange={(e) => setHistorizeDeadband(parseFloat(e.target.value))}
                    />
                  </div>
                )}
                <div className="flex items-center justify-between">
                  <Label htmlFor="alarm">Enable Alarm</Label>
                  <Switch
                    id="alarm"
                    checked={alarmEnabled}
                    onCheckedChange={setAlarmEnabled}
                  />
                </div>
                {alarmEnabled && (
                  <>
                    <div className="space-y-2">
                      <Label htmlFor="threshold">Threshold</Label>
                      <Input
                        id="threshold"
                        type="number"
                        value={alarmThreshold}
                        onChange={(e) => setAlarmThreshold(parseFloat(e.target.value))}
                      />
                    </div>
                    <div className="space-y-2">
                      <Label htmlFor="operator">Operator</Label>
                      <Select value={alarmOperator} onValueChange={(v: AlarmOperator) => setAlarmOperator(v)}>
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value=">">Greater than (&gt;)</SelectItem>
                          <SelectItem value="<">Less than (&lt;)</SelectItem>
                          <SelectItem value="=">Equal (=)</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="space-y-2">
                      <Label htmlFor="priority">Priority (1-5)</Label>
                      <Input
                        id="priority"
                        type="number"
                        min={1}
                        max={5}
                        value={alarmPriority}
                        onChange={(e) => setAlarmPriority(parseInt(e.target.value) as AlarmPriority)}
                      />
                    </div>
                  </>
                )}
              </div>
              <DialogFooter>
                <Button type="submit" disabled={!gatewayId}>
                  Create
                </Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>
      </div>

      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Code</TableHead>
              <TableHead>Alias</TableHead>
              <TableHead>Gateway</TableHead>
              <TableHead>Type</TableHead>
              <TableHead>Historize</TableHead>
              <TableHead>Alarm</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {tags.length === 0 ? (
              <TableRow>
                <TableCell colSpan={7} className="text-center">
                  No tags found
                </TableCell>
              </TableRow>
            ) : (
              tags.map((tag) => (
                <TableRow key={tag.id}>
                  <TableCell className="font-mono text-sm">{tag.code}</TableCell>
                  <TableCell className="font-medium">{tag.alias}</TableCell>
                  <TableCell>{tag.gateway?.name || `GW ${tag.gateway_id}`}</TableCell>
                  <TableCell>
                    <Badge variant="outline">{tag.data_type}</Badge>
                  </TableCell>
                  <TableCell>
                    {tag.historize ? (
                      <Badge variant="success">Yes ({tag.historize_deadband})</Badge>
                    ) : (
                      <Badge variant="outline">No</Badge>
                    )}
                  </TableCell>
                  <TableCell>
                    {tag.alarm_enabled ? (
                      <Badge variant="warning">P{tag.alarm_priority}</Badge>
                    ) : (
                      <Badge variant="outline">None</Badge>
                    )}
                  </TableCell>
                  <TableCell className="text-right">
                    <Button
                      variant="ghost"
                      size="icon"
                      onClick={() => handleDelete(tag.id)}
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}
```

### 53. pages/AlarmsPage.tsx

**File:** `services/web-ui/src/pages/AlarmsPage.tsx`

```typescript
import { useState } from 'react'
import { useAlarms } from '@/hooks/useAlarms'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { Check, AlertTriangle, Clock } from 'lucide-react'
import type { AlarmState } from '@/types'

export function AlarmsPage() {
  const [stateFilter, setStateFilter] = useState<string>('')
  const { alarms, isLoading, acknowledgeAlarm } = useAlarms(
    stateFilter ? { state: stateFilter } : undefined
  )

  const handleAcknowledge = (id: number) => {
    acknowledgeAlarm(id)
  }

  const getStateBadge = (state: AlarmState) => {
    switch (state) {
      case 'ACTIVE':
        return <Badge variant="destructive">Active</Badge>
      case 'RTN':
        return <Badge variant="warning">RTN</Badge>
      case 'ACKNOWLEDGED':
        return <Badge variant="default">Acknowledged</Badge>
      case 'CLEAR':
        return <Badge variant="outline">Clear</Badge>
      default:
        return <Badge variant="outline">{state}</Badge>
    }
  }

  if (isLoading) {
    return <div>Loading...</div>
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-3xl font-bold">Alarms</h1>
          <p className="text-muted-foreground">View and manage system alarms</p>
        </div>
        <Select value={stateFilter} onValueChange={setStateFilter}>
          <SelectTrigger className="w-[180px]">
            <SelectValue placeholder="Filter by state" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="">All Alarms</SelectItem>
            <SelectItem value="ACTIVE">Active</SelectItem>
            <SelectItem value="RTN">RTN</SelectItem>
            <SelectItem value="ACKNOWLEDGED">Acknowledged</SelectItem>
            <SelectItem value="CLEAR">Clear</SelectItem>
          </SelectContent>
        </Select>
      </div>

      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>ID</TableHead>
              <TableHead>State</TableHead>
              <TableHead>Message</TableHead>
              <TableHead>Tag</TableHead>
              <TableHead>Triggered At</TableHead>
              <TableHead>Acknowledged At</TableHead>
              <TableHead className="text-right">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {alarms.length === 0 ? (
              <TableRow>
                <TableCell colSpan={7} className="text-center">
                  No alarms found
                </TableCell>
              </TableRow>
            ) : (
              alarms.map((alarm) => (
                <TableRow key={alarm.id}>
                  <TableCell>{alarm.id}</TableCell>
                  <TableCell>{getStateBadge(alarm.state)}</TableCell>
                  <TableCell className="font-medium">{alarm.message}</TableCell>
                  <TableCell>
                    {alarm.tag ? (
                      <span className="font-mono text-sm">{alarm.tag.alias}</span>
                    ) : (
                      `Tag ${alarm.tag_id}`
                    )}
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center gap-1">
                      <Clock className="h-3 w-3" />
                      {new Date(alarm.triggered_at).toLocaleString()}
                    </div>
                  </TableCell>
                  <TableCell>
                    {alarm.acknowledged_at ? (
                      <div className="flex items-center gap-1">
                        <Check className="h-3 w-3" />
                        {new Date(alarm.acknowledged_at).toLocaleString()}
                      </div>
                    ) : (
                      '-'
                    )}
                  </TableCell>
                  <TableCell className="text-right">
                    {alarm.state === 'ACTIVE' || alarm.state === 'RTN' ? (
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={() => handleAcknowledge(alarm.id)}
                      >
                        <Check className="mr-2 h-4 w-4" />
                        Acknowledge
                      </Button>
                    ) : null}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}
```

### 54. components/ui/switch.tsx (aggiunta per Tags)

**File:** `services/web-ui/src/components/ui/switch.tsx`

```typescript
import * as React from "react"
import * as SwitchPrimitives from "@radix-ui/react-switch"
import { cn } from "@/lib/utils"

const Switch = React.forwardRef<
  React.ElementRef<typeof SwitchPrimitives.Root>,
  React.ComponentPropsWithoutRef<typeof SwitchPrimitives.Root>
>(({ className, ...props }, ref) => (
  <SwitchPrimitives.Root
    className={cn(
      "peer inline-flex h-6 w-11 shrink-0 cursor-pointer items-center rounded-full border-2 border-transparent transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:cursor-not-allowed disabled:opacity-50 data-[state=checked]:bg-primary data-[state=unchecked]:bg-input",
      className
    )}
    {...props}
    ref={ref}
  >
    <SwitchPrimitives.Thumb
      className={cn(
        "pointer-events-none block h-5 w-5 rounded-full bg-background shadow-lg ring-0 transition-transform data-[state=checked]:translate-x-5 data-[state=unchecked]:translate-x-0"
      )}
    />
  </SwitchPrimitives.Root>
))
Switch.displayName = SwitchPrimitives.Root.displayName

export { Switch }
```

---

## Componente da Aggiungere a App.tsx

Aggiungere il Toaster per i toast notification:

```typescript
import { Toaster } from '@/components/ui/toaster'

// Nel return di App, prima della chiusura del BrowserRouter
<Toaster />
```

---

## Comandi per il Setup

```bash
# Creare la directory
mkdir -p services/web-ui

# Navigare nella directory
cd services/web-ui

# Inizializzare il progetto
npm init -y

# Installare le dipendenze (usare il package.json fornito sopra)
npm install

# In modalità sviluppo
npm run dev

# Build per produzione
npm run build

# Con Docker
docker-compose build web-ui
docker-compose up -d web-ui
```

---

## Note Importanti

1. **Dipendenze aggiuntive da installare**: Aggiungere `@radix-ui/react-switch` tra le dipendenze nel package.json

2. **Variabile d'ambiente**: Assicurarsi che `VITE_API_URL` sia configurata correttamente:
   - In sviluppo: `http://localhost:8080/api`
   - In produzione: `http://core-api:8080/api`

3. **Proxy Vite**: In modalità sviluppo, Vite gestisce il proxy tramite la configurazione in `vite.config.ts`

4. **Componenti UI shadcn**: I componenti forniti sono basati su shadcn/ui. Alcuni componenti potrebbero需要 ulteriori adattamenti

5. **Polling**: Le query per alarms e health usano refetchInterval per polling automatico

6. **Validazione**: I form usano controlli HTML5 base. Per validazione più complessa, integrare React Hook Form + Zod completamente

---

## Checklist Implementazione

- [ ] Creare struttura directory
- [ ] Creare Dockerfile e nginx.conf
- [ ] Creare package.json e installare dipendenze
- [ ] Creare file di configurazione (vite, tailwind, tsconfig)
- [ ] Creare componenti UI base
- [ ] Creare componenti layout
- [ ] Creare store per navigazione
- [ ] Creare API client
- [ ] Creare custom hooks
- [ ] Creare pagine
- [ ] Aggiornare docker-compose.yml
- [ ] Testare flusso completo

---

## Riferimenti

- **Vite**: https://vitejs.dev/
- **shadcn/ui**: https://ui.shadcn.com/
- **React Query**: https://tanstack.com/query/latest
- **Zustand**: https://zustand-demo.pmnd.rs/
- **React Router**: https://reactrouter.com/
- **TanStack Table**: https://tanstack.com/table/latest
