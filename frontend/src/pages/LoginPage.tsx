// Design System: See tasks/design-system.md
import { useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { useToast } from '@/components/ui/use-toast'
import { Toaster } from '@/components/ui/toaster'
import { getOrganizations } from '@/services/api'
import type { Organization } from '@/types/api'

export function LoginPage() {
  const [organizations, setOrganizations] = useState<Organization[]>([])
  const [selectedOrgId, setSelectedOrgId] = useState<string>('')
  const [loading, setLoading] = useState(true)
  const [submitting, setSubmitting] = useState(false)
  const navigate = useNavigate()
  const { toast } = useToast()

  // Check for existing organization ID and redirect if already selected
  useEffect(() => {
    const existingOrgId = localStorage.getItem('ralph_org_id')
    if (existingOrgId) {
      navigate('/config')
    }
  }, [navigate])

  // Fetch organizations on mount
  useEffect(() => {
    const fetchOrgs = async () => {
      try {
        const orgs = await getOrganizations()
        setOrganizations(orgs)
      } catch (error) {
        toast({
          variant: 'destructive',
          title: 'Error',
          description: 'Failed to load organizations. Please check if the backend is running.',
        })
      } finally {
        setLoading(false)
      }
    }
    fetchOrgs()
  }, [toast])

  const handleEnter = () => {
    if (!selectedOrgId) {
      toast({
        variant: 'destructive',
        title: 'No organization selected',
        description: 'Please select an organization to continue.',
      })
      return
    }

    setSubmitting(true)
    try {
      // Store selected organization ID in localStorage
      localStorage.setItem('ralph_org_id', selectedOrgId)

      toast({
        title: 'Welcome!',
        description: `You have entered ${organizations.find(org => org.id === parseInt(selectedOrgId))?.name || 'the organization'}.`,
      })

      // Redirect to /config page
      setTimeout(() => {
        navigate('/config')
      }, 500)
    } catch (error) {
      toast({
        variant: 'destructive',
        title: 'Error',
        description: 'Failed to select organization. Please try again.',
      })
    } finally {
      setSubmitting(false)
    }
  }

  if (loading) {
    return (
      <div className="min-h-screen bg-gray-50 flex items-center justify-center">
        <div className="text-center">
          <div className="animate-spin rounded-full h-12 w-12 border-b-2 border-violet-600 mx-auto mb-4"></div>
          <p className="text-gray-600">Loading organizations...</p>
        </div>
        <Toaster />
      </div>
    )
  }

  return (
    <div className="min-h-screen bg-gray-50 flex items-center justify-center p-4">
      <Toaster />
      <Card className="w-full max-w-md">
        <CardHeader className="text-center">
          <CardTitle className="text-3xl font-bold text-gray-900">
            Industrial Edge Middleware
          </CardTitle>
          <CardDescription className="text-base mt-2">
            Select your organization to access the system
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-6">
          {organizations.length === 0 ? (
            <Alert variant="destructive">
              <AlertTitle>No organizations available</AlertTitle>
              <AlertDescription>
                Please create an organization in the backend first.
              </AlertDescription>
            </Alert>
          ) : (
            <>
              <div className="space-y-2">
                <label htmlFor="org-select" className="text-sm font-medium text-gray-700">
                  Organization
                </label>
                <Select value={selectedOrgId} onValueChange={setSelectedOrgId}>
                  <SelectTrigger id="org-select" className="w-full">
                    <SelectValue placeholder="Select an organization" />
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

              <Button
                onClick={handleEnter}
                disabled={!selectedOrgId || submitting}
                className="w-full bg-violet-600 hover:bg-violet-700"
              >
                {submitting ? 'Entering...' : 'Enter'}
              </Button>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
