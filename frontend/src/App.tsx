// Design System: See tasks/design-system.md
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { LoginPage } from '@/pages/LoginPage'
import { ConfigPage } from '@/pages/ConfigPage'
import { Layout } from '@/components/Layout'
import { Toaster } from '@/components/ui/toaster'

// Placeholder pages for routes that will be implemented in future user stories
function TrendPage() {
  return (
    <div className="flex items-center justify-center h-full">
      <div className="text-center">
        <h2 className="text-2xl font-bold text-gray-900 mb-2">Trend/Historian</h2>
        <p className="text-gray-600">Coming soon...</p>
      </div>
    </div>
  )
}

function AlarmsPage() {
  return (
    <div className="flex items-center justify-center h-full">
      <div className="text-center">
        <h2 className="text-2xl font-bold text-gray-900 mb-2">Alarms</h2>
        <p className="text-gray-600">Coming soon...</p>
      </div>
    </div>
  )
}

// Route guard component that checks for organization selection
function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const orgId = localStorage.getItem('ralph_org_id')
  if (!orgId) {
    return <Navigate to="/login" replace />
  }
  return <>{children}</>
}

function App() {
  return (
    <BrowserRouter>
      <Routes>
        {/* Login/Organization Selection Page - No Layout */}
        <Route path="/login" element={<LoginPage />} />

        {/* Protected Routes with Layout */}
        <Route
          path="/*"
          element={
            <ProtectedRoute>
              <Layout>
                <Routes>
                  {/* Configuration Page */}
                  <Route path="/config" element={<ConfigPage />} />

                  {/* Trend/Historian Page */}
                  <Route path="/trend" element={<TrendPage />} />

                  {/* Alarms Page */}
                  <Route path="/alarms" element={<AlarmsPage />} />

                  {/* Default redirect to config */}
                  <Route path="/" element={<Navigate to="/config" replace />} />
                </Routes>
              </Layout>
            </ProtectedRoute>
          }
        />
      </Routes>
      <Toaster />
    </BrowserRouter>
  )
}

export default App
