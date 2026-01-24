// Design System: See tasks/design-system.md
import { BrowserRouter, Routes, Route, Navigate } from 'react-router-dom'
import { LoginPage } from '@/pages/LoginPage'
import { ConfigPage } from '@/pages/ConfigPage'
import { Toaster } from '@/components/ui/toaster'

function App() {
  return (
    <BrowserRouter>
      <Routes>
        {/* Login/Organization Selection Page */}
        <Route path="/login" element={<LoginPage />} />

        {/* Configuration Page */}
        <Route path="/config" element={<ConfigPage />} />

        {/* Default redirect to login */}
        <Route path="/" element={<Navigate to="/login" replace />} />

        {/* Catch all - redirect to login */}
        <Route path="*" element={<Navigate to="/login" replace />} />
      </Routes>
      <Toaster />
    </BrowserRouter>
  )
}

export default App
